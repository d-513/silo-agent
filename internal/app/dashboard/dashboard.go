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
//
// The model touches the board only in the curator: a pinned automation this
// package registers (Curator), which runs from a fresh context like any
// firing, reads the board, mends and prunes it. The owner's words from the ask
// bar go to that same run log, so the board never adds a chat.
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
	"silo.agent/internal/prompts"
	"silo.agent/internal/textx"
)

// Curator is the pinned automation that tends a Bot's Dashboard.
var Curator = automation.Pinned{
	Kind: "dashboard", Name: "Dashboard", Prompt: prompts.Dashboard,
	// The first prompt had the model rewrite every card on each run.
	Replaces: []string{"Dashboard sweep."},
}

// postsMax is how many unread Feed posts the board shows for a visit.
const postsMax = 20

// Host is what the Dashboard needs from the App around it.
type Host interface {
	host.Authorizer
	host.Runs
	host.Emitter
	run.Engine
	// EndTurn ends the run after its current turn's tool calls.
	EndTurn(runID string)
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
	feed        *feed.Service
	busy        busySet
	// slots bounds how many probes run at once across every Bot.
	slots chan struct{}
}

// New makes the Dashboard and pins its curator on every Bot.
func New(gdb *gorm.DB, h Host, m *memory.Service, au *automation.Service, f *feed.Service) *Service {
	au.Pin(Curator)
	return &Service{db: gdb, host: h, memory: m, automations: au, feed: f, slots: make(chan struct{}, probeSlots)}
}

// Drop forgets a deleted Bot's Dashboard.
func (s *Service) Drop(botID string) {
	s.db.Where("bot_id = ?", botID).Delete(&db.DashboardCard{})
	s.db.Where("bot_id = ?", botID).Delete(&db.DashboardWidget{})
}

// Open is how many asks wait for the owner: the Bot row carries it, so the
// sidebar can say the board needs them without loading the board.
func (s *Service) Open(botID string) int32 {
	var n int64
	s.db.Model(&db.DashboardCard{}).Where("bot_id = ? AND status = 'open'", botID).Count(&n)
	return int32(n)
}

// ListDashboard is the whole page: asks, unread Feed posts, widgets in their
// places, and the Dashboard automation's state for the meta line.
func (s *Service) ListDashboard(ctx context.Context, req *connect.Request[v1.ListDashboardRequest]) (*connect.Response[v1.ListDashboardResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.Bot(ctx, s.db, botID, access.Use); err != nil {
		return nil, err
	}
	out := &v1.ListDashboardResponse{}
	// What is new is new to this person: each has their own last look.
	me := access.User(ctx).ID
	seen := s.seenAt(botID, me)
	for _, c := range s.openAsks(botID) {
		p := protoCard(&c)
		p.Read = seen != nil && !c.UpdatedAt.After(*seen)
		out.Cards = append(out.Cards, p)
	}
	out.Posts = s.feed.UnreadPosts(botID, me, postsMax)
	for _, w := range s.widgets(botID) {
		p := s.protoWidget(&w)
		p.Fresh = w.ChangedAt != nil && (seen == nil || w.ChangedAt.After(*seen))
		out.Widgets = append(out.Widgets, p)
	}
	if au := s.automations.Pinned(botID, Curator.Kind); au != nil {
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
	if _, err := access.Bot(ctx, s.db, botID, access.Use); err != nil {
		return nil, err
	}
	now := time.Now()
	me := access.User(ctx).ID
	s.db.Model(&db.BotAccess{}).Where("bot_id = ? AND user_id = ?", botID, me).Update("dashboard_seen_at", now)
	s.feed.MarkRead(botID, me, now)
	return connect.NewResponse(&v1.MarkDashboardReadResponse{}), nil
}

// seenAt is the person's last look at the Bot's Dashboard.
func (s *Service) seenAt(botID, userID string) *time.Time {
	var a db.BotAccess
	s.db.Select("dashboard_seen_at").Where("bot_id = ? AND user_id = ?", botID, userID).Limit(1).Find(&a)
	return a.DashboardSeenAt
}
