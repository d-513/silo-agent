package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/connector"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// CallTool is the Python surface of the tool engine: silo_runtime.call and the
// generated `tools.<slug>` stubs arrive here through the worker. It resolves
// connector.action to a definition, shows the call on the thread, runs it
// through the same gate and handler a chat tool call would, and answers JSON.
func (a *App) CallTool(ctx context.Context, req *connect.Request[v1.ToolReq]) (*connect.Response[v1.ToolRes], error) {
	bot := currentBot(ctx)
	slug, action, argsJSON := req.Msg.GetConnector(), req.Msg.GetAction(), req.Msg.GetArgsJson()
	runID := a.callRun(bot.ID, req.Msg.GetRunId())
	fail := func(err error) (*connect.Response[v1.ToolRes], error) {
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	def, err := a.Tools.ByAction(bot.ID, slug, action)
	if err != nil {
		return fail(err)
	}
	call := &tool.Call{
		Surface: tool.Python, Bot: bot, ChatID: a.ChatOfRun(runID), RunID: runID,
		Def: def, Args: map[string]any{}, ArgsJSON: argsJSON,
	}
	// A local tool's arguments were held back by the worker, and a connector
	// reads its own from the raw JSON, so a bad payload is theirs to refuse.
	_ = json.Unmarshal([]byte(argsJSON), &call.Args)
	title := security.Describe(slug, action, argsJSON).Title
	if def.Title != nil {
		title = def.Title(call)
	}
	key := security.Key(slug, action)
	a.Emit(bot.ID, call.ChatID, runID, "call", title, key)
	res, err := a.Tools.Run(ctx, call)
	if err != nil {
		a.emitCallDone(bot.ID, runID, key, err.Error())
		return fail(err)
	}
	out := a.Mask(bot.ID).Apply(res.Text)
	a.emitCallDone(bot.ID, runID, key, textx.Cap(out, callResultMax))
	if res.Data != "" {
		return connect.NewResponse(&v1.ToolRes{ResultJson: res.Data}), nil
	}
	return connect.NewResponse(&v1.ToolRes{ResultJson: pythonResult(out)}), nil
}

func callTitle(name, action string) string { return connector.CallTitle(name, action) }

// callResultMax caps a call's result on the thread's call row.
const callResultMax = 2000

// pythonResult is a tool's text as the JSON Python receives: passed through
// when it already is JSON, otherwise wrapped as {"result": text}.
func pythonResult(s string) string {
	if t := strings.TrimSpace(s); t != "" && json.Valid([]byte(t)) {
		return s
	}
	b, err := json.Marshal(map[string]string{"result": s})
	if err != nil {
		return `{"result":""}`
	}
	return string(b)
}

func (a *App) AuthorizeAction(ctx context.Context, bot *db.Bot, runID, conn, action, argsJSON, fallback string) (string, error) {
	decision := security.Rule(a.ruleDecision(bot.ID, conn, action, fallback))
	auto := decision == security.Auto
	if auto {
		switch a.Approval.Judge(ctx, bot, a.ChatOfRun(runID), conn, action, argsJSON) {
		case security.Allow:
			a.audit(bot, "auto", conn+"."+action, "auto_allow")
			return "", nil
		case security.Deny:
			a.audit(bot, "auto", conn+"."+action, "auto_deny")
			return "", errors.New("denied")
		default:
			// The policy did not clearly decide; a human gets the slip.
			decision = security.Ask
		}
	}
	switch decision {
	case security.Deny:
		a.audit(bot, "worker", conn+"."+action, security.Deny)
		return "", errors.New("denied")
	case security.Allow:
		a.audit(bot, "worker", conn+"."+action, security.Allow)
		return "", nil
	default:
		ap := db.Approval{
			ID: ids.New(), BotID: bot.ID, RunID: runID,
			Connector: conn, Action: action, ArgsJSON: security.Redact(conn, action, argsJSON), Status: "pending", Auto: auto, CreatedAt: time.Now(),
		}
		a.DB.Create(&ap)
		ch := make(chan string, 1)
		a.mu.Lock()
		a.approvals[ap.ID] = &waiter{ch: ch, botID: bot.ID, runID: runID}
		a.mu.Unlock()
		a.setBotStatus(bot.ID, "needs_you")
		a.Emit(bot.ID, a.ChatOfRun(runID), runID, "approval", ap.ID, conn+"."+action)
		var dec string
		select {
		case <-ctx.Done():
			a.dropWaiter(ap.ID)
			a.DB.Model(&db.Approval{}).Where("id = ? AND status = ?", ap.ID, "pending").Update("status", "canceled")
			a.recomputeStatus(bot.ID)
			return ap.ID, errors.New("canceled")
		case dec = <-ch:
		}
		if !security.Granted(dec) {
			return ap.ID, errors.New("denied")
		}
		return ap.ID, nil
	}
}
