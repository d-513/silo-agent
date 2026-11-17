// Package runlog is what a Bot did, for its managers to review: every run,
// in numbers, with the model calls made for it beside its turns, and the
// model calls made for no run at all.
//
// The split is by what the work can do. A run can change the Bot and reach
// its machine, so it has a history in the database. A model call beside the
// turns of a run (a judge, a compaction, a title) explains that run and is
// kept in its log. One made for no run changes nothing a history would have
// to explain, and is a line in memory (Background).
package runlog

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// SideKind is the run event of a model call made for a run beside its turns:
// Tool is what it was for, Body a side as JSON. Only this package reads it:
// it is never published, and no model's context has a case for it.
const SideKind = "side"

const (
	// pageSize is how many ended runs a page lists; liveMax how many live
	// ones head the newest page.
	pageSize = 50
	liveMax  = 50
	errMax   = 300
)

var errNoRun = errors.New("no such run")

// Service owns the review of a Bot's work.
type Service struct {
	db *gorm.DB
	// Background is the calls made for no run.
	Background *Background
}

func New(gdb *gorm.DB) *Service {
	return &Service{db: gdb, Background: newBackground()}
}

// Seen files one model call (models.Service.Seen). A run's own turn is its
// log already; a call made for a run is an event of that run; any other is a
// background line.
func (s *Service) Seen(c models.Call) {
	switch {
	case c.Turn:
	case c.RunID != "":
		body, _ := json.Marshal(side{
			Provider: c.Provider, Model: c.Model, In: c.In, Out: c.Out, Ms: c.Duration.Milliseconds(),
			Verdict: c.Verdict, Err: textx.CapRunes(c.Err, errMax),
		})
		s.db.Create(&db.RunEvent{ID: ids.New(), RunID: c.RunID, Kind: SideKind, Body: string(body), Tool: c.Label, CreatedAt: time.Now()})
	default:
		c.Err = textx.CapRunes(c.Err, errMax)
		s.Background.Note(c)
	}
}

// Drop forgets a deleted Bot's background lines; its runs go with its chats.
func (s *Service) Drop(botID string) { s.Background.Drop(botID) }

// kindScope narrows a query over runs to the named kinds.
func (s *Service) kindScope(q *gorm.DB, names []string) *gorm.DB {
	if len(names) == 0 {
		return q
	}
	cond := s.db.Where("1 = 0")
	for _, name := range names {
		k := run.KindOf(name, "")
		switch name {
		case run.KindJob:
			cond = cond.Or("job <> ''")
		default:
			for _, known := range run.Kinds {
				if known.Name == name {
					k = known
				}
			}
			cond = cond.Or("origin = ? AND job = ''", k.Origin)
		}
	}
	return q.Where(cond)
}

func (s *Service) ListRuns(ctx context.Context, req *connect.Request[v1.ListRunsRequest]) (*connect.Response[v1.ListRunsResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	scope := func() *gorm.DB {
		return s.kindScope(s.db.Where("bot_id = ?", b.ID), req.Msg.GetKinds())
	}
	var rows []db.Run
	before := time.Time{}
	if c := req.Msg.GetBefore(); c != "" {
		if before, err = time.Parse(time.RFC3339Nano, c); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	} else {
		// What is at work now leads, however long ago it started.
		scope().Where("status = ?", "running").Order("created_at desc").Limit(liveMax).Find(&rows)
	}
	q := scope().Where("status <> ?", "running")
	if !before.IsZero() {
		q = q.Where("created_at < ?", before)
	}
	var ended []db.Run
	q.Order("created_at desc").Limit(pageSize + 1).Find(&ended)
	out := &v1.ListRunsResponse{}
	if len(ended) > pageSize {
		ended = ended[:pageSize]
		out.Next = ended[pageSize-1].CreatedAt.Format(time.RFC3339Nano)
	}
	rows = append(rows, ended...)
	where := s.places(rows)
	for i := range rows {
		out.Runs = append(out.Runs, s.row(&rows[i], where[rows[i].ChatID]))
	}
	return connect.NewResponse(out), nil
}

// place is where a run ran.
type place struct {
	kind, name                 string
	automation, subagent, lead string
}

// places finds the conversations of a page of runs in a query per table.
func (s *Service) places(rows []db.Run) map[string]place {
	chatIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		chatIDs = append(chatIDs, r.ChatID)
	}
	var cs []db.Chat
	s.db.Select("id", "title", "main", "automation_id", "channel_id", "subagent_id").Where("id IN ?", chatIDs).Find(&cs)
	var autos, chans, subs []string
	for _, c := range cs {
		switch {
		case c.AutomationID != "":
			autos = append(autos, c.AutomationID)
		case c.ChannelID != "":
			chans = append(chans, c.ChannelID)
		case c.SubagentID != "":
			subs = append(subs, c.SubagentID)
		}
	}
	names := map[string]string{}
	var aus []db.Automation
	s.db.Select("id", "name").Where("id IN ?", autos).Find(&aus)
	for _, a := range aus {
		names[a.ID] = a.Name
	}
	var chs []db.Channel
	s.db.Select("id", "name").Where("id IN ?", chans).Find(&chs)
	for _, c := range chs {
		names[c.ID] = c.Name
	}
	leads := map[string]string{}
	var sas []db.Subagent
	s.db.Select("id", "parent_chat_id").Where("id IN ?", subs).Find(&sas)
	for _, sa := range sas {
		leads[sa.ID] = sa.ParentChatID
	}
	out := map[string]place{}
	for _, c := range cs {
		switch {
		case c.AutomationID != "":
			out[c.ID] = place{kind: "automation", name: names[c.AutomationID], automation: c.AutomationID}
		case c.ChannelID != "":
			out[c.ID] = place{kind: "channel", name: names[c.ChannelID]}
		case c.SubagentID != "":
			out[c.ID] = place{kind: "subagent", name: c.Title, subagent: c.SubagentID, lead: leads[c.SubagentID]}
		case c.Main:
			out[c.ID] = place{kind: "main"}
		default:
			title := c.Title
			if chats.Untitled(title) {
				title = chats.NewTitle
			}
			out[c.ID] = place{kind: "chat", name: title}
		}
	}
	return out
}

func (s *Service) row(r *db.Run, at place) *v1.RunRow {
	out := &v1.RunRow{
		Id: r.ID, Kind: run.KindOf(r.Origin, r.Job).Name, Job: r.Job, Trigger: r.Trigger, Status: r.Status,
		Title: r.Title, Model: r.Model, CreatedAt: r.CreatedAt.Format(time.RFC3339), DurationMs: r.DurationMs,
		ChatId: r.ChatID, SourceKind: at.kind, SourceName: at.name,
		AutomationId: at.automation, SubagentId: at.subagent, LeadChatId: at.lead,
	}
	if r.UserID != "" {
		out.Person = access.Email(s.db, r.UserID)
	}
	if r.Status == "running" {
		return out
	}
	var sum Summary
	if r.Summary == "" || json.Unmarshal([]byte(r.Summary), &sum) != nil {
		// A run from before summaries were kept: worked out once, now.
		sum = s.Keep(r.ID)
	}
	out.Summary = sum.proto()
	return out
}

func (s *Service) GetRunCalls(ctx context.Context, req *connect.Request[v1.GetRunCallsRequest]) (*connect.Response[v1.GetRunCallsResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	var r db.Run
	if s.db.Where("id = ? AND bot_id = ?", req.Msg.GetRunId(), b.ID).Limit(1).Find(&r).RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errNoRun)
	}
	out := &v1.GetRunCallsResponse{}
	var evs []db.RunEvent
	s.db.Where("run_id = ? AND kind = ?", r.ID, SideKind).Order("seq").Find(&evs)
	for _, ev := range evs {
		var c side
		_ = json.Unmarshal([]byte(ev.Body), &c)
		out.Calls = append(out.Calls, &v1.ModelCall{
			At: ev.CreatedAt.Format(time.RFC3339), Label: ev.Tool, Provider: c.Provider, Model: c.Model,
			Input: int64(c.In), Output: int64(c.Out), DurationMs: c.Ms, Verdict: c.Verdict, Error: c.Err, Count: 1,
		})
	}
	var aps []db.Approval
	s.db.Where("run_id = ?", r.ID).Order("created_at").Find(&aps)
	for _, a := range aps {
		// The title names the action; the arguments stay on the slip.
		out.Approvals = append(out.Approvals, &v1.RunApproval{
			Action: security.Key(a.Connector, a.Action), Title: security.Describe(a.Connector, a.Action, "").Title,
			Status: a.Status, Auto: a.Auto, CreatedAt: a.CreatedAt.Format(time.RFC3339),
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ListBackground(ctx context.Context, req *connect.Request[v1.ListBackgroundRequest]) (*connect.Response[v1.ListBackgroundResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	out := &v1.ListBackgroundResponse{Since: s.Background.since.Format(time.RFC3339)}
	for _, l := range s.Background.List(b.ID) {
		out.Calls = append(out.Calls, &v1.ModelCall{
			At: l.At.Format(time.RFC3339), Label: l.Label, Provider: l.Provider, Model: l.Model,
			Input: int64(l.In), Output: int64(l.Out), DurationMs: l.Duration.Milliseconds(),
			Verdict: l.Verdict, Error: l.Err, Count: int32(l.Count),
		})
	}
	return connect.NewResponse(out), nil
}
