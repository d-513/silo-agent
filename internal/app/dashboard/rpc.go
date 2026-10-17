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
	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// Tool is every Dashboard tool, from chat or Python. Each is gated on its own
// bot.* rule here; code a widget carries is gated again on python.run when it
// is put.
func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any) (string, error) {
	action := map[string]string{
		"list_dashboard":        "dashboard_list",
		"upsert_dashboard_card": "dashboard_upsert",
		"remove_dashboard_card": "dashboard_remove",
		"put_widget":            "widget_put",
		"set_widget":            "widget_set",
		"remove_widget":         "widget_remove",
	}[name]
	if action == "" {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	argsJSON, _ := json.Marshal(args)
	if _, err := s.host.AuthorizeAction(ctx, bot, runID, security.Bot, action, string(argsJSON), ""); err != nil {
		return "", err
	}
	switch name {
	case "list_dashboard":
		return s.listForBot(bot.ID)
	case "upsert_dashboard_card":
		return s.upsertAsk(bot.ID, s.host.ChatOfRun(runID), runID, args)
	case "remove_dashboard_card":
		ref := str(args, "id_or_key")
		if strings.TrimSpace(ref) == "" {
			ref = str(args, "id")
		}
		if strings.TrimSpace(ref) == "" {
			ref = str(args, "key")
		}
		return s.removeAsk(bot.ID, ref)
	case "put_widget":
		return s.putWidget(ctx, bot, runID, args)
	case "set_widget":
		return s.setWidget(bot.ID, args)
	default:
		return s.removeWidget(bot.ID, str(args, "key"))
	}
}

func (s *Service) ownWidget(ctx context.Context, botID, id string) (*db.DashboardWidget, error) {
	return access.OwnBotRow[db.DashboardWidget](ctx, s.db, botID, id, "widget")
}

// RefreshWidget runs a widget's probe now, for the owner looking at it.
func (s *Service) RefreshWidget(ctx context.Context, req *connect.Request[v1.RefreshWidgetRequest]) (*connect.Response[v1.DashboardWidget], error) {
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.refresh(ctx, w); err != nil {
		if errors.Is(err, errNotRunning) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("start the Bot to refresh this widget"))
		}
		// The error is on the widget; the page shows it there.
	}
	return connect.NewResponse(s.protoWidget(w)), nil
}

// SetWidgetState stores what the owner set in place, such as checked items.
// It runs nothing; the Bot reads it the next time it looks.
func (s *Service) SetWidgetState(ctx context.Context, req *connect.Request[v1.SetWidgetStateRequest]) (*connect.Response[v1.DashboardWidget], error) {
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId())
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
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId())
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
		ch, runID, err := s.quoteChat(w.BotID, w.Title, widgetQuote(w), "Dashboard", time.Now(), handTurn(w, a, args))
		if err != nil {
			return nil, err
		}
		out.Chat, out.RunId = chats.Proto(ch), runID
	} else if err := s.runAction(ctx, w, *a, args); errors.Is(err, errNotRunning) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("start the Bot to run this"))
	}
	var fresh db.DashboardWidget
	s.db.Where("id = ?", w.ID).Limit(1).Find(&fresh)
	if fresh.ID != "" {
		w = &fresh
	}
	out.Widget = s.protoWidget(w)
	return connect.NewResponse(out), nil
}

// widgetQuote is the widget as the chat it is handed to quotes it.
func widgetQuote(w *db.DashboardWidget) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** (Dashboard widget `%s`, %s)", w.Title, w.Key, w.Kind)
	if data := strings.TrimSpace(w.DataJSON); data != "" && data != "{}" {
		b.WriteString("\n\n```json\n" + textx.ClipRunes(data, 4000) + "\n```")
	}
	return b.String()
}

func handTurn(w *db.DashboardWidget, a *action, args string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Pressed **%s** on the Dashboard widget **%s**.", a.Label, w.Title)
	if vals := valuesText(args); vals != "" {
		b.WriteString(" For:\n" + vals)
	}
	b.WriteString("\n\nPlease carry this out.")
	return b.String()
}

// ArrangeDashboard is the owner placing widgets: once placed, a widget keeps
// its place and size whatever the Bot suggests later.
func (s *Service) ArrangeDashboard(ctx context.Context, req *connect.Request[v1.ArrangeDashboardRequest]) (*connect.Response[v1.ArrangeDashboardResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
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
	w, err := s.ownWidget(ctx, req.Msg.GetBotId(), req.Msg.GetId())
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
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
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
	runID, chatID, err := s.automations.Ask(botID, automation.KindDashboard, b.String())
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.AskDashboardResponse{RunId: runID, ChatId: chatID}), nil
}
