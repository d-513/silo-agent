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
	"silo.agent/internal/app/connector"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
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
	client, provider, model, err := a.Models.Observed(cfg.ApprovalModel(), bot.ID, "approval")
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
		Cache:     models.CachePolicy(cfg.ProviderSettings(provider), bot.ID),
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
