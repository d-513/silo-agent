package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/quote"
	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

// Tool runs a Dashboard tool, from chat or Python. The engine has already
// gated it on its bot.* rule; code a widget carries is gated again on
// python.run when it is put.
func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any) (string, error) {
	switch name {
	case "list_dashboard":
		return s.listForBot(bot.ID)
	case "ask_owner":
		return s.askOwner(ctx, bot, runID, args)
	case "upsert_dashboard_card":
		c, created, changed, err := s.upsertAsk(bot.ID, s.host.ChatOfRun(runID), runID, askArgs(args))
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(map[string]any{"id": c.ID, "key": c.Key, "title": c.Title, "created": created, "updated": !created && changed})
		return string(out), nil
	case "remove_dashboard_card":
		ref := strings.TrimSpace(toolarg.Str(args, "id"))
		if ref == "" {
			ref = toolarg.Str(args, "key")
		}
		return s.removeAsk(bot.ID, ref)
	case "put_widget":
		return s.putWidget(ctx, bot, runID, args)
	case "set_widget":
		return s.setWidget(bot.ID, args)
	case "remove_widget":
		return s.removeWidget(bot.ID, toolarg.Str(args, "key"))
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func (s *Service) ownWidget(ctx context.Context, botID, id string, need access.Need) (*db.DashboardWidget, error) {
	return access.Row[db.DashboardWidget](ctx, s.db, botID, id, "widget", need)
}

// RefreshWidget runs a widget's probe now, for the owner looking at it.
func (s *Service) RefreshWidget(ctx context.Context, req *connect.Request[v1.RefreshWidgetRequest]) (*connect.Response[v1.DashboardWidget], error) {
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Use)
	if err != nil {
		return nil, err
	}
	// Any other error is on the widget; the page shows it there.
	if err := pressed(s.refresh(ctx, w), "start the Bot to refresh this widget"); err != nil {
		return nil, err
	}
	return connect.NewResponse(s.protoWidget(w)), nil
}

// pressed is the answer to the owner's press when the widget could not take
// it: nothing ran, so nothing on the widget says why. stopped is what a
// stopped machine reads as.
func pressed(err error, stopped string) error {
	switch {
	case errors.Is(err, errNotRunning):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New(stopped))
	case errors.Is(err, errBusy), errors.Is(err, errNoProbe):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return nil
}

// SetWidgetState stores what the owner set in place, such as checked items.
// It runs nothing; the Bot reads it the next time it looks.
func (s *Service) SetWidgetState(ctx context.Context, req *connect.Request[v1.SetWidgetStateRequest]) (*connect.Response[v1.DashboardWidget], error) {
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Use)
	if err != nil {
		return nil, err
	}
	state, err := object(req.Msg.GetStateJson(), "state", stateMax)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	w.StateJSON = state
	s.db.Model(&db.DashboardWidget{}).Where("id = ?", w.ID).UpdateColumn("state_json", state)
	return connect.NewResponse(s.protoWidget(w)), nil
}

// RunWidgetAction is the owner pressing one of a widget's buttons: a run
// action executes the widget's approved code on the box; a hand action opens
// a chat that quotes the widget.
func (s *Service) RunWidgetAction(ctx context.Context, req *connect.Request[v1.RunWidgetActionRequest]) (*connect.Response[v1.RunWidgetActionResponse], error) {
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Use)
	if err != nil {
		return nil, err
	}
	_, acts := parseSpec(w.SpecJSON)
	var a *action
	for i := range acts {
		if acts[i].ID == req.Msg.GetAction() {
			a = &acts[i]
		}
	}
	if a == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("this widget has no action %q", req.Msg.GetAction()))
	}
	args := "{}"
	if strings.TrimSpace(req.Msg.GetArgsJson()) != "" {
		if args, err = object(req.Msg.GetArgsJson(), "args", dataMax); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	out := &v1.RunWidgetActionResponse{}
	if a.Kind == "hand" {
		placed, err := s.quoteChat(w.BotID, w.Title, widgetQuote(w), handTurn(a, args), req.Msg.GetNewChat())
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		out.Chat, out.RunId = chats.Proto(placed.Chat), placed.RunID
	} else if err := pressed(s.runAction(ctx, w, *a, args), "start the Bot to run this"); err != nil {
		// A failure the code met is the receipt on the widget.
		return nil, err
	}
	var fresh db.DashboardWidget
	s.db.Where("id = ?", w.ID).Limit(1).Find(&fresh)
	if fresh.ID != "" {
		w = &fresh
	}
	out.Widget = s.protoWidget(w)
	return connect.NewResponse(out), nil
}

// widgetQuote is the widget as it stands, for the chat it is handed to: the
// model reads its data, the thread draws the widget again without actions.
func widgetQuote(w *db.DashboardWidget) quote.Quote {
	spec, _ := parseSpec(w.SpecJSON)
	delete(spec, "actions")
	specJSON, _ := json.Marshal(spec)
	data := strings.TrimSpace(w.DataJSON)
	body := "(no data yet)"
	if data != "" && data != "{}" {
		body = "```json\n" + textx.ClipRunes(data, 4000) + "\n```"
	}
	at := time.Now()
	if w.DataAt != nil {
		at = *w.DataAt
	}
	return quote.Quote{
		From: quote.FromWidget, Title: w.Title, Body: body, At: at,
		Widget: &quote.Widget{Key: w.Key, Kind: w.Kind, Spec: string(specJSON), Data: textx.ClipRunes(data, 20000), State: w.StateJSON, Source: w.Source},
	}
}

// handTurn is the owner pressing a hand action, in their own voice: the
// quote above it says which widget.
func handTurn(a *action, args string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "I pressed **%s** on it.", a.Label)
	if vals := valuesText(args); vals != "" {
		b.WriteString(" For:\n" + vals)
	}
	b.WriteString("\n\nPlease go ahead.")
	return b.String()
}

// ArrangeDashboard is the owner placing widgets: once placed, a widget keeps
// its place and size whatever the Bot suggests later.
func (s *Service) ArrangeDashboard(ctx context.Context, req *connect.Request[v1.ArrangeDashboardRequest]) (*connect.Response[v1.ArrangeDashboardResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.Bot(ctx, s.db, botID, access.Manage); err != nil {
		return nil, err
	}
	for _, p := range req.Msg.GetPlaces() {
		upd := map[string]any{"position": p.GetPosition(), "owner_placed": true}
		if sz := p.GetSize(); sz == "s" || sz == "m" || sz == "l" {
			upd["size"] = sz
		}
		if r := p.GetRows(); r == 1 || r == 2 {
			upd["rows"] = r
		}
		s.db.Model(&db.DashboardWidget{}).Where("bot_id = ? AND id = ?", botID, p.GetId()).UpdateColumns(upd)
	}
	return connect.NewResponse(&v1.ArrangeDashboardResponse{}), nil
}

// RemoveWidget is the owner taking a widget off the board.
func (s *Service) RemoveWidget(ctx context.Context, req *connect.Request[v1.RemoveWidgetRequest]) (*connect.Response[v1.RemoveWidgetResponse], error) {
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId(), access.Manage)
	if err != nil {
		return nil, err
	}
	s.db.Delete(w)
	return connect.NewResponse(&v1.RemoveWidgetResponse{}), nil
}

// AskDashboard hands the owner's words about the Dashboard to the Bot. They go
// to the Dashboard automation's log, so changing the board never adds a chat;
// a run already curating takes them in.
func (s *Service) AskDashboard(ctx context.Context, req *connect.Request[v1.AskDashboardRequest]) (*connect.Response[v1.AskDashboardResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.Bot(ctx, s.db, botID, access.Use); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(req.Msg.GetText())
	if text == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("say what to change"))
	}
	var b strings.Builder
	b.WriteString("The owner asked from the Dashboard")
	if id := req.Msg.GetWidgetId(); id != "" {
		var w db.DashboardWidget
		if s.db.Where("bot_id = ? AND id = ?", botID, id).Limit(1).Find(&w).RowsAffected == 1 {
			fmt.Fprintf(&b, ", about the widget `%s` (“%s”)", w.Key, w.Title)
		}
	}
	b.WriteString(":\n\n" + text)
	runID, chatID, err := s.automations.Ask(botID, Curator.Kind, b.String())
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.AskDashboardResponse{RunId: runID, ChatId: chatID}), nil
}
