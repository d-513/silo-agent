package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/prompts"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

func (a *App) CallTool(ctx context.Context, req *connect.Request[v1.ToolReq]) (*connect.Response[v1.ToolRes], error) {
	bot := currentBot(ctx)
	slug := req.Msg.GetConnector()
	action := req.Msg.GetAction()
	runID := a.callRun(bot.ID, req.Msg.GetRunId())
	if security.Reserved(slug) {
		return a.callBuiltin(ctx, bot, slug, action, req.Msg.GetArgsJson(), runID)
	}
	bc, c, err := a.findBotConnector(bot.ID, slug)
	if err != nil {
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	if c.Auth == authOAuth && bc.AuthStatus != statusOK {
		return connect.NewResponse(&v1.ToolRes{Error: "connector is not authorized"}), nil
	}
	if hdr, err := mcpx.HeadersFromJSON(a.resolveConnector(c).HeadersJSON); err == nil {
		for _, v := range hdr {
			a.Mask(bot.ID).Add(v)
		}
	}
	tool := slug + "." + action
	a.emit(bot.ID, a.ChatOfRun(runID), runID, "call", callTitle(c.Name, action), tool)
	if _, err := a.AuthorizeAction(ctx, bot, runID, slug, action, req.Msg.GetArgsJson(), builtinMode(c, action)); err != nil {
		a.emitCallDone(bot.ID, runID, tool, err.Error())
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	if c.Transport == transportBuiltin {
		out, err := a.runBuiltin(ctx, bot, c, action, req.Msg.GetArgsJson())
		if err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		a.emitCallDone(bot.ID, runID, tool, textx.Cap(out, callResultMax))
		return connect.NewResponse(&v1.ToolRes{ResultJson: out}), nil
	}
	sess, err := a.mcpSession(ctx, bc, c)
	if err != nil {
		a.emitCallDone(bot.ID, runID, tool, err.Error())
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	args := map[string]any{}
	if raw := strings.TrimSpace(req.Msg.GetArgsJson()); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			a.emitCallDone(bot.ID, runID, tool, "invalid args")
			return connect.NewResponse(&v1.ToolRes{Error: "invalid args"}), nil
		}
	}
	for k, v := range args {
		if v == nil {
			delete(args, k)
		}
	}
	out, err := mcpx.Call(ctx, sess, action, args)
	if err != nil && errors.Is(err, mcpx.ErrSessionGone) {
		a.dropMCP(bc.ID)
		sess, err = a.mcpSession(ctx, bc, c)
		if err == nil {
			out, err = mcpx.Call(ctx, sess, action, args)
		}
	}
	if err != nil {
		a.emitCallDone(bot.ID, runID, tool, err.Error())
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	out = a.Mask(bot.ID).Apply(out)
	a.emitCallDone(bot.ID, runID, tool, textx.Cap(out, callResultMax))
	return connect.NewResponse(&v1.ToolRes{ResultJson: out}), nil
}

func (a *App) callBuiltin(ctx context.Context, bot *db.Bot, slug, action, argsJSON, runID string) (*connect.Response[v1.ToolRes], error) {
	switch slug {
	case security.Desktop:
		tool := security.Key(slug, action)
		a.emit(bot.ID, a.ChatOfRun(runID), runID, "call", callTitle("Desktop", action), tool)
		if _, err := a.AuthorizeAction(ctx, bot, runID, slug, action, argsJSON, ""); err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		a.emitCallDone(bot.ID, runID, tool, "ok")
		return connect.NewResponse(&v1.ToolRes{ResultJson: `{"ok":true}`}), nil
	case security.Web:
		if action != "search" {
			return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
		}
		tool := security.Key(slug, action)
		title := security.Describe(slug, action, argsJSON).Title
		a.emit(bot.ID, a.ChatOfRun(runID), runID, "call", title, tool)
		if _, err := a.AuthorizeAction(ctx, bot, runID, slug, action, argsJSON, ""); err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		out, err := a.runWebSearch(ctx, argsJSON)
		if err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		out = a.Mask(bot.ID).Apply(out)
		a.emitCallDone(bot.ID, runID, tool, textx.Cap(out, callResultMax))
		return connect.NewResponse(&v1.ToolRes{ResultJson: out}), nil
	case security.Channels:
		if action != "send" {
			return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
		}
		args := map[string]any{}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		name, _ := args["channel"].(string)
		title := "Send to channel"
		if strings.TrimSpace(name) != "" {
			title = "Send to " + name
		}
		tool := security.Key(slug, action)
		a.emit(bot.ID, a.ChatOfRun(runID), runID, "call", title, tool)
		out, err := a.channelSendTool(ctx, bot.ID, runID, args)
		if err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		a.emitCallDone(bot.ID, runID, tool, out)
		return connect.NewResponse(&v1.ToolRes{ResultJson: jsonResult(out)}), nil
	case security.Chats:
		if action != "read" {
			return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
		}
		args := map[string]any{}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		tool := security.Key(slug, action)
		a.emit(bot.ID, a.ChatOfRun(runID), runID, "call", "Read chats", tool)
		out, err := a.chatsReadTool(ctx, bot.ID, runID, args)
		if err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		a.emitCallDone(bot.ID, runID, tool, textx.Cap(out, callResultMax))
		return connect.NewResponse(&v1.ToolRes{ResultJson: jsonResult(out)}), nil
	case security.Bot, security.Automations, security.Model, security.Tasks:
		// Actions shared with a chat tool run the same code (runShared); the
		// rest of these connectors (soul, core_memory, switch_model) are chat-only.
		name, ok := sharedToolName(slug, action)
		if !ok {
			return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
		}
		args := map[string]any{}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		tool := security.Key(slug, action)
		a.emit(bot.ID, a.ChatOfRun(runID), runID, "call", security.Describe(slug, action, argsJSON).Title, tool)
		out, err := a.runShared(ctx, bot, runID, name, args, true)
		if err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		a.emitCallDone(bot.ID, runID, tool, textx.Cap(out, callResultMax))
		if strings.HasPrefix(out, "{") && json.Valid([]byte(out)) {
			return connect.NewResponse(&v1.ToolRes{ResultJson: out}), nil
		}
		return connect.NewResponse(&v1.ToolRes{ResultJson: jsonResult(out)}), nil
	case security.Artifact:
		if action != "emit" {
			return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
		}
		if runID == "" {
			// An orphan call has no thread to hold the card.
			return connect.NewResponse(&v1.ToolRes{Error: "artifact needs a live chat run; this process is not attached to one"}), nil
		}
		tool := security.Key(slug, action)
		title := security.Describe(slug, action, argsJSON).Title
		a.emit(bot.ID, a.ChatOfRun(runID), runID, "call", title, tool)
		if _, err := a.AuthorizeAction(ctx, bot, runID, slug, action, argsJSON, ""); err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		out, err := a.artifact(ctx, bot, runID, argsJSON)
		if err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		a.emitCallDone(bot.ID, runID, tool, out)
		return connect.NewResponse(&v1.ToolRes{ResultJson: `{"ok":true}`}), nil
	default:
		return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
	}
}

func callTitle(name, action string) string {
	a := action
	if i := strings.LastIndex(action, "__"); i >= 0 {
		a = action[i+2:]
	} else if i := strings.LastIndex(action, "."); i >= 0 {
		a = action[i+1:]
	}
	a = strings.ReplaceAll(a, "_", " ")
	if name == "" {
		return a
	}
	return name + " · " + a
}

// callResultMax caps a connector call's result on the thread's call row.
const callResultMax = 2000

// jsonResult wraps a plain string as a JSON object for the Python bus.
func jsonResult(s string) string {
	b, err := json.Marshal(map[string]string{"result": s})
	if err != nil {
		return `{"result":""}`
	}
	return string(b)
}

func (a *App) AuthorizeAction(ctx context.Context, bot *db.Bot, runID, conn, action, argsJSON, fallback string) (string, error) {
	decision := security.Rule(a.ruleDecision(bot.ID, conn, action, fallback))
	if decision == security.Auto {
		switch a.autoDecision(ctx, bot, conn, action, argsJSON) {
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
			Connector: conn, Action: action, ArgsJSON: security.Redact(conn, action, argsJSON), Status: "pending", CreatedAt: time.Now(),
		}
		a.DB.Create(&ap)
		ch := make(chan string, 1)
		a.mu.Lock()
		a.approvals[ap.ID] = &waiter{ch: ch, botID: bot.ID, runID: runID}
		a.mu.Unlock()
		a.setBotStatus(bot.ID, "needs_you")
		a.emit(bot.ID, a.ChatOfRun(runID), runID, "approval", ap.ID, conn+"."+action)
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

// autoDecision asks the approval model whether an action covered by an "auto"
// rule may run. It sends only a small prompt — Silo's base gate instructions
// plus the Bot's own policy and a redacted action summary. Anything other than
// a clear approve/deny falls back to a human (ask).
func (a *App) autoDecision(ctx context.Context, bot *db.Bot, conn, action, argsJSON string) string {
	policy := strings.TrimSpace(bot.AutoApprove)
	if policy == "" {
		return security.Ask
	}
	cfg := a.cfg()
	client, provider, model, err := a.modelClient(cfg.ApprovalModel(), bot.ID, "approval")
	if err != nil {
		log.Printf("auto-approval model: %v", err)
		return security.Ask
	}
	p := security.Describe(conn, action, security.Redact(conn, action, argsJSON))
	var b strings.Builder
	fmt.Fprintf(&b, "Action: %s\n", p.Title)
	fmt.Fprintf(&b, "%s\n", p.Summary)
	for _, f := range p.Fields {
		fmt.Fprintf(&b, "%s: %s\n", f.Label, f.Value)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := client.Complete(cctx, llm.Request{
		Model: model,
		System: []llm.SystemBlock{
			{Text: prompts.Approval},
			{Text: "The operator's auto-approval policy:\n" + policy},
		},
		Messages:  []llm.Message{{Role: llm.RoleUser, Text: b.String()}},
		Cache:     cachePolicy(cfg.ProviderSettings(provider), bot.ID),
		MaxTokens: 8,
	})
	if err != nil {
		log.Printf("auto-approval model: %v", err)
		return security.Ask
	}
	return parseVerdict(res.Text)
}

// parseVerdict reads the model's one-word decision from the first word of the
// reply. Anything else is "ask" — an unrecognized or negated answer must never
// auto-allow.
func parseVerdict(s string) string {
	words := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(s)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	if len(words) == 0 {
		return security.Ask
	}
	switch words[0] {
	case "approve", "approved", "allow", "allowed", "yes":
		return security.Allow
	case "deny", "denied", "no", "forbid", "forbidden", "block":
		return security.Deny
	case "ask", "human", "review", "unsure", "maybe":
		return security.Ask
	default:
		return security.Ask
	}
}
