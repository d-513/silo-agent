// Package dashboard is a Bot's Dashboard: the widgets it keeps for its owner
// and the asks it needs the owner to decide.
//
// A widget is written once by the Bot (kind, title, settings, actions and an
// optional probe) and kept current by code, not by the model: the probe is
// Python the CP runs on the Bot's machine on a schedule (Sweep), and any cell
// can push data with silo_runtime.set_widget. Silo draws every kind itself, so
// a widget looks like the rest of the console; an app widget shows a live
// artifact app in the hatch, on its own origin. The owner uses a widget in
// place: checks are widget state, a run action executes the widget's stored
// code, a hand action opens a chat. Each action leaves a receipt on the widget.
//
// An ask is what the Bot needs decided: a slip with optional form controls
// whose submit opens a chat with the chosen values.
package dashboard

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/feed"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

// Host is what the Dashboard needs from the App around it.
type Host interface {
	host.Authorizer
	host.Runs
	run.Engine
	// Online reports whether the Bot's worker is connected. The Dashboard
	// never starts a box.
	Online(botID string) bool
	// RunPython runs code in a throwaway interpreter on the Bot's machine and
	// returns what it printed (a failing script's traceback included).
	RunPython(ctx context.Context, botID, code string) (string, error)
	// RuleDecision is the Bot's rule for conn.action now, without asking.
	RuleDecision(botID, conn, action string) string
}

// Service owns the Dashboard: widgets, asks, their tools, RPCs and the sweep
// that keeps probes current.
type Service struct {
	db          *gorm.DB
	host        Host
	memory      *memory.Service
	automations *automation.Service
	busy        busySet
	// slots bounds how many probes run at once across every Bot.
	slots chan struct{}
}

func New(gdb *gorm.DB, h Host, m *memory.Service, au *automation.Service) *Service {
	return &Service{db: gdb, host: h, memory: m, automations: au, slots: make(chan struct{}, probeSlots)}
}

// Drop forgets a deleted Bot's Dashboard.
func (s *Service) Drop(botID string) {
	s.db.Where("bot_id = ?", botID).Delete(&db.DashboardCard{})
	s.db.Where("bot_id = ?", botID).Delete(&db.DashboardWidget{})
}

// Open is how many asks wait for the owner.
func (s *Service) Open(botID string) int32 {
	var n int64
	s.db.Model(&db.DashboardCard{}).Where("bot_id = ? AND status = 'open'", botID).Count(&n)
	return int32(n)
}

// ListDashboard is the whole page: asks, unread Feed posts, widgets in their
// places, and the Dashboard automation's state for the meta line.
func (s *Service) ListDashboard(ctx context.Context, req *connect.Request[v1.ListDashboardRequest]) (*connect.Response[v1.ListDashboardResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
		return nil, err
	}
	out := &v1.ListDashboardResponse{}
	var cards []db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'open'", botID).Order("priority asc, updated_at desc").Find(&cards)
	for i := range cards {
		out.Cards = append(out.Cards, protoCard(&cards[i]))
	}
	var posts []db.FeedPost
	s.db.Where("bot_id = ? AND read_at IS NULL", botID).Order("created_at desc").Limit(20).Find(&posts)
	for i := range posts {
		out.Posts = append(out.Posts, feed.Proto(&posts[i]))
	}
	for _, w := range s.widgets(botID) {
		out.Widgets = append(out.Widgets, s.protoWidget(&w))
	}
	if au := s.automations.Pinned(botID, automation.KindDashboard); au != nil {
		out.AutomationId = au.ID
		out.ChatId = au.ChatID
		out.Schedule = au.Schedule
		out.CuratedAt = textx.RFC3339(au.LastRunAt)
		if au.Schedule != "" && au.Enabled {
			out.NextAt = textx.RFC3339(au.NextRunAt)
		}
		out.Curating = au.ChatID != "" && s.host.LiveRunID(botID, au.ChatID) != ""
	}
	return connect.NewResponse(out), nil
}

// MarkDashboardRead is the owner looking: asks, unread posts and changed
// widgets all count as seen.
func (s *Service) MarkDashboardRead(ctx context.Context, req *connect.Request[v1.MarkDashboardReadRequest]) (*connect.Response[v1.MarkDashboardReadResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
		return nil, err
	}
	now := time.Now()
	s.db.Model(&db.DashboardCard{}).Where("bot_id = ? AND status = 'open' AND read_at IS NULL", botID).Update("read_at", now)
	s.db.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", botID).Update("read_at", now)
	s.db.Model(&db.DashboardWidget{}).Where("bot_id = ?", botID).UpdateColumn("seen_at", now)
	return connect.NewResponse(&v1.MarkDashboardReadResponse{}), nil
}
