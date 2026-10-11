package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/db"
	"silo.agent/internal/desktop"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
)

// newTools builds the tool engine: the built-in definitions plus the Bot's
// connectors as a per-Bot source. Both ways a tool is reached — a chat tool
// call (execTool) and Python through the worker (CallTool) — run through it.
func (a *App) newTools() *tool.Engine {
	e := tool.New(a)
	e.Expose = func() []string { return a.cfg().Tools.ExposeCompletion }
	e.Register(a.toolDefs()...)
	if a.Connectors != nil {
		e.AddSource(a.Connectors)
	}
	return e
}

// toolsFor is the completion tool list for a run: every tool exposed to the
// model and enabled for this Bot, minus the lead-only ones for a subagent.
func (a *App) toolsFor(botID string, origin *run.Origin) []llm.Tool {
	return tool.LLM(a.Tools.Offered(botID, origin != nil && origin.Subagent != nil))
}

// execTool runs one tool call the model made: the completion surface. def is
// the definition its name resolved to (toolDef), nil when there is none.
func (a *App) execTool(ctx context.Context, def *tool.Def, botID, chatID, runID, name, argsJSON string) (string, string, error) {
	if def == nil {
		return "", "", fmt.Errorf("unknown tool %s", name)
	}
	var bot db.Bot
	if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
		return "", "", errors.New("unknown bot")
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	res, err := a.Tools.Run(ctx, &tool.Call{
		Surface: tool.Completion, Bot: &bot, ChatID: chatID, RunID: runID,
		Def: def, Args: args, ArgsJSON: argsJSON,
	})
	return res.Text, res.Image, err
}

// toolDef is the definition behind a completion tool name for one Bot. Nil
// when the model made the name up.
func (a *App) toolDef(botID, name string) *tool.Def {
	d, _ := a.Tools.ByName(botID, name)
	return d
}

// --- handler helpers ---

// text adapts a handler that only returns text.
func text(fn func(ctx context.Context, c *tool.Call) (string, error)) tool.Handler {
	return func(ctx context.Context, c *tool.Call) (tool.Result, error) {
		out, err := fn(ctx, c)
		return tool.Result{Text: out}, err
	}
}

// onWorker is a tool that is one command on the Bot's machine. done shapes the
// worker's answer; nil passes its output (and any screenshot it took) through.
func (a *App) onWorker(build func(c *tool.Call) *v1.Cmd, done func(c *tool.Call, out hub.Result) tool.Result) tool.Handler {
	return func(ctx context.Context, c *tool.Call) (tool.Result, error) {
		cmd := build(c)
		cmd.Id, cmd.RunId = ids.New(), c.RunID
		log.Printf("exec %s bot=%s run=%s", c.Def.Name, c.Bot.ID, c.RunID)
		if !a.Hub.WaitConnected(ctx, c.Bot.ID, 90*time.Second) {
			return tool.Result{}, errors.New("the Bot machine is not running yet")
		}
		// The run's first tool that can change files takes the snapshot its
		// changes are diffed against.
		if c.Def.Writes {
			a.Changes.Begin(ctx, c.Bot.ID, c.RunID)
		}
		a.mu.Lock()
		a.cmdRun[cmd.Id] = c.RunID
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			delete(a.cmdRun, cmd.Id)
			a.mu.Unlock()
		}()
		out, err := a.Hub.ExecResult(ctx, c.Bot.ID, cmd)
		if err != nil {
			return tool.Result{}, err
		}
		if done != nil {
			return done(c, out), nil
		}
		// Python/terminal commands may have taken a desktop look; the worker
		// reports the fresh screenshot as a data: URL on the done event.
		return tool.Result{Text: out.Out, Image: out.Image}, nil
	}
}

// withChrome opens the desktop Chromium first when the code is about to drive
// it through Playwright.
func (a *App) withChrome(next tool.Handler) tool.Handler {
	return func(ctx context.Context, c *tool.Call) (tool.Result, error) {
		if desktop.WantsChromium(c.Str("code")) {
			if !a.Hub.WaitConnected(ctx, c.Bot.ID, 90*time.Second) {
				return tool.Result{}, errors.New("the Bot machine is not running yet")
			}
			if err := a.ensureChrome(ctx, c.Bot.ID, c.RunID); err != nil {
				return tool.Result{}, err
			}
		}
		return next(ctx, c)
	}
}

// pySession is the persistent Python interpreter a run's exec_python calls
// share; empty (a throwaway interpreter) for a call no live run made.
func (a *App) pySession(runID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if lr := a.runs[runID]; lr != nil {
		return lr.pySession
	}
	return ""
}

// fileChanged tells Knowledge a workspace file was written or removed.
func (a *App) fileChanged(c *tool.Call, out hub.Result) tool.Result {
	a.Knowledge.Dirty(c.Bot.ID, relPath(c))
	return tool.Result{Text: out.Out, Image: out.Image}
}

// artifactTool shows a workspace path as a card in the run's thread.
func (a *App) artifactTool(ctx context.Context, c *tool.Call) (tool.Result, error) {
	if c.RunID == "" {
		// An orphan call has no thread to hold the card.
		return tool.Result{}, errors.New("artifact needs a live chat run; this process is not attached to one")
	}
	out, err := a.Artifacts.Tool(ctx, c.Bot, c.RunID, c.ArgsJSON)
	if err != nil {
		return tool.Result{}, err
	}
	res := tool.Result{Text: out}
	if c.Structured() {
		res.Data = `{"ok":true}`
	}
	return res, nil
}

// recallTool searches long-term memory: lines for the model, rows for Python.
func (a *App) recallTool(ctx context.Context, c *tool.Call) (string, error) {
	if !c.Structured() {
		return a.Memory.RecallTool(ctx, c.Bot.ID, c.Str("query"), c.Int("limit"))
	}
	rows, err := a.Memory.Recall(ctx, c.Bot.ID, c.Str("query"), c.Int("limit"), 0)
	if err != nil {
		return "", err
	}
	type memory struct {
		ID        string  `json:"id"`
		Content   string  `json:"content"`
		CreatedAt string  `json:"created_at"`
		Distance  float64 `json:"distance"`
	}
	out := struct {
		Memories []memory `json:"memories"`
	}{Memories: []memory{}}
	for _, r := range rows {
		out.Memories = append(out.Memories, memory{r.ID, r.Content, r.CreatedAt.Format(time.RFC3339), r.Distance})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// --- argument checks and titles ---

// relPath is the path argument relative to /workspace.
func relPath(c *tool.Call) string { return workspace.Rel(c.Str("path")) }

func needPath(c *tool.Call) error {
	if relPath(c) == "" {
		return errors.New("path required")
	}
	return nil
}

func needKey(c *tool.Call) error {
	if c.Str("name") == "" {
		return errors.New("key required")
	}
	return nil
}

func checkPoint(c *tool.Call) error {
	return desktop.CheckPoint(c.Int("x"), c.Int("y"))
}

func checkScroll(c *tool.Call) error {
	if err := checkPoint(c); err != nil {
		return err
	}
	if c.Int("dy") == 0 {
		return errors.New("dy required")
	}
	return nil
}

// grepHits is the grep result cap: what was asked for, else 80.
func grepHits(c *tool.Call) int {
	if n := c.Int("max_hits"); n > 0 {
		return n
	}
	return 80
}

func desktopTitle(c *tool.Call) string { return callTitle("Desktop", c.Def.Action) }

func channelTitle(c *tool.Call) string {
	if name := strings.TrimSpace(c.Str("channel")); name != "" {
		return "Send to " + name
	}
	return "Send to channel"
}

func jsonLooksComplete(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}
