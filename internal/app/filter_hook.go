package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/app/filter"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// outputPolicyMax caps a Bot's output policy.
const outputPolicyMax = 20000

// filteredKind is the thread's record that an output rule did something to a
// tool result. It is for the owner: replay never reads it, and the memory
// collector skips it, so a withheld result stays out of every model's sight.
const filteredKind = "filtered"

// filteredMax caps how much of a withheld result the owner is shown.
const filteredMax = 4000

// filteredBody is the body of a filtered event.
type filteredBody struct {
	Verdict string `json:"verdict"`
	// Mode is the output rule that led to it: flag, check or withhold.
	Mode string `json:"mode"`
	// Original is the result the Bot did not get, for a withheld one.
	Original string `json:"original,omitempty"`
}

// heldNotices is embedded in App: the warnings waiting for a tool result.
type heldNotices struct {
	noticeMu   sync.Mutex
	notices    map[string][]string    // by run
	botNotices map[string][]botNotice // by Bot, for calls that named no run
}

// FilterResult applies the action's output rule to what a tool returned. It
// is the tool engine's tool.ResultFilter, so it sees the result of every
// tool on both surfaces before anyone reads it.
func (a *App) FilterResult(ctx context.Context, c *tool.Call, res tool.Result, err error) (tool.Result, error) {
	d := c.Def
	if c.Surface == tool.Completion {
		// Whatever the rule, the Bot never reads a result that speaks in the
		// filter's voice: see filter.Defang.
		res.Text = filter.Defang(res.Text)
		if err != nil {
			if msg := filter.Defang(err.Error()); msg != err.Error() {
				err = errors.New(msg)
			}
		}
	}
	if d.Connector == "" || d.Action == "" || !a.cfg().Filter.Enabled || ctx.Err() != nil {
		return res, err
	}
	mode := a.ruleOutput(c.Bot.ID, d.Connector, d.Action, d.OutMode)
	if mode == security.OutPass {
		return res, err
	}
	content := res.Text
	switch {
	case err != nil:
		// The gate's own refusals are not something a tool answered.
		if msg := err.Error(); msg == "denied" || msg == "canceled" {
			return res, err
		}
		content = err.Error()
	case c.Surface == tool.Python && res.Data != "":
		content = res.Data
	}
	// With no text there is nothing for a model to judge. Flag and Withhold
	// ask no model and still apply: a result can be pixels alone.
	if strings.TrimSpace(content) == "" && (mode == security.OutCheck || res.Image == "") {
		return res, err
	}
	content = a.Mask(c.Bot.ID).Apply(content)

	verdict := filter.Flagged
	switch mode {
	case security.OutWithhold:
		verdict = filter.Withheld
	case security.OutCheck:
		verdict = a.Filter.Check(ctx, filter.Call{
			Bot: c.Bot, ChatID: c.ChatID, Connector: d.Connector, Action: d.Action, ArgsJSON: c.ArgsJSON,
		}, content)
	}
	if verdict == filter.Clean {
		return res, err
	}
	key := d.Key()
	a.audit(c.Bot, c.RunID, "filter", key, "output_"+string(verdict))
	body := filteredBody{Verdict: string(verdict), Mode: mode}
	if verdict == filter.Withheld {
		body.Original = textx.Cap(content, filteredMax)
	}
	raw, _ := json.Marshal(body)
	a.Emit(c.Bot.ID, c.ChatID, c.RunID, filteredKind, string(raw), key)

	if c.Surface == tool.Python {
		// Code received this, not the Bot: what it prints of it, and whether
		// it lets an error show, is the code's business. The warning waits
		// for the result of the tool call the code is running in.
		a.holdNotice(c.Bot.ID, c.RunID, filter.CellNotice(verdict, mode, key))
		switch {
		case verdict == filter.Withheld:
			// A script must not carry on as if it had the data.
			return tool.Result{}, errors.New(filter.Placeholder(key))
		case err != nil:
			return tool.Result{}, errors.New(filter.Frame(verdict, mode, key, err.Error()))
		}
		return res, nil
	}
	if err != nil {
		return tool.Result{}, errors.New(filter.Frame(verdict, mode, key, err.Error()))
	}
	if verdict == filter.Withheld {
		return tool.Result{Text: filter.Placeholder(key)}, nil
	}
	res.Text = filter.Frame(verdict, mode, key, res.Text)
	return res, nil
}

// noticeFor is how long a warning held for a Bot (not a run) waits for a tool
// result: a tool call's code runs for five minutes at most.
const noticeFor = 6 * time.Minute

type botNotice struct {
	line string
	at   time.Time
}

// holdNotice keeps a warning about a result that code received until the
// tool call that code runs in returns (takeNotices). Code that did not say
// which run it belongs to (an old silo_runtime, a raw call, a process a cell
// left behind) is held for the Bot instead, and the next tool result on that
// Bot carries it: a warning that lands one call wide of its mark is better
// than one that is lost.
func (a *App) holdNotice(botID, runID, line string) {
	if line == "" {
		return
	}
	// A run that ended while its code's call was being judged has dropped
	// its notices already: held for it, this one would never be read.
	if runID != "" {
		a.mu.Lock()
		_, live := a.runs[runID]
		a.mu.Unlock()
		if !live {
			runID = ""
		}
	}
	a.noticeMu.Lock()
	defer a.noticeMu.Unlock()
	if runID == "" {
		if a.botNotices == nil {
			a.botNotices = map[string][]botNotice{}
		}
		held := slices.DeleteFunc(a.botNotices[botID], func(n botNotice) bool { return time.Since(n.at) > noticeFor })
		for _, n := range held {
			if n.line == line {
				return
			}
		}
		if len(held) < 20 {
			a.botNotices[botID] = append(held, botNotice{line, time.Now()})
		}
		return
	}
	if a.notices == nil {
		a.notices = map[string][]string{}
	}
	if !slices.Contains(a.notices[runID], line) {
		a.notices[runID] = append(a.notices[runID], line)
	}
}

// takeNotices is the warnings held for a run and for its Bot, as the head of
// the tool result they belong to, and forgets them. A run's tool calls are
// one at a time, so what is held when one returns was received by the code
// it ran.
func (a *App) takeNotices(botID, runID string) string {
	a.noticeMu.Lock()
	lines := a.notices[runID]
	delete(a.notices, runID)
	for _, n := range a.botNotices[botID] {
		if time.Since(n.at) <= noticeFor && !slices.Contains(lines, n.line) {
			lines = append(lines, n.line)
		}
	}
	delete(a.botNotices, botID)
	a.noticeMu.Unlock()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// dropNotices forgets what is still held for a run that ended.
func (a *App) dropNotices(runID string) {
	a.noticeMu.Lock()
	delete(a.notices, runID)
	a.noticeMu.Unlock()
}
