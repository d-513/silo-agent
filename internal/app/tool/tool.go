// Package tool is the one engine behind everything a Bot can call. A tool is a
// Def: what the model is told, the rule that gates it, the code that runs it,
// and where it is offered — to the model as a completion tool
// (ExposeCompletion), to code on the Bot's machine through
// `silo_runtime` / `import tools` (ExposePython), or both. The chat loop and
// the worker's CallTool are thin adapters over Engine.Run, so a tool behaves
// the same whichever way it is reached.
//
// Built-in tools are registered once at start. A Source adds tools that differ
// per Bot: its attached connectors.
package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"silo.agent/internal/app/host"
	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// Surface is a way a tool call arrives.
type Surface string

const (
	// Completion is a tool call the model made in a chat turn.
	Completion Surface = "completion"
	// Python is code on the Bot's machine: silo_runtime.call(connector, action)
	// through the worker.
	Python Surface = "python"
)

// Def is one tool.
type Def struct {
	// Name is the completion tool's name, unique across the engine.
	Name        string
	Description string
	// Params is the JSON schema of the arguments (see Object).
	Params json.RawMessage

	// Connector and Action are the tool's address from Python and, unless it
	// is SelfGated, the rule that gates it.
	Connector string
	Action    string

	// ExposeCompletion offers the tool to the model directly.
	ExposeCompletion bool
	// ExposePython makes it callable as silo_runtime.call(Connector, Action).
	ExposePython bool
	// PyName is the silo_runtime helper that wraps it, when that is not Name.
	PyName string

	// Mode is the decision when the Bot has no rule for the action and the
	// rule catalog has no default (a connector's own default mode).
	Mode string
	// SelfGated means Run authorizes on its own: its rule depends on the
	// arguments (one rule per channel, a public tunnel) or it needs none.
	SelfGated bool
	// Local means the worker performs the action itself when Python asks, so
	// the engine only grants it there (the desktop: typed text stays on the
	// machine). The completion surface still runs it.
	Local bool

	// LeadOnly keeps the tool from subagents.
	LeadOnly bool
	// Writes marks a tool that can change files in the workspace.
	Writes bool
	// RepeatGuard marks a read whose third identical call in a run is answered
	// without running it.
	RepeatGuard bool

	// Enabled, when set, decides whether the tool is offered to a Bot's model
	// (a feature that is switched off). Nil is always.
	Enabled func(botID string) bool
	// Check validates the arguments before anybody is asked for permission.
	Check func(c *Call) error
	// Title names the call on the thread's call row. Nil uses the rule's title.
	Title func(c *Call) string

	Run Handler
}

// PyHelper is the silo_runtime function that wraps the tool.
func (d Def) PyHelper() string {
	if d.PyName != "" {
		return d.PyName
	}
	return d.Name
}

// Key is the tool's connector.action address.
func (d Def) Key() string { return d.Connector + "." + d.Action }

func (d Def) exposed(s Surface) bool {
	if s == Python {
		return d.ExposePython
	}
	return d.ExposeCompletion
}

// Call is one invocation of a tool.
type Call struct {
	Surface Surface
	Bot     *db.Bot
	// ChatID and RunID are empty for a call that belongs to no live run.
	ChatID string
	RunID  string
	Def    *Def
	Args   map[string]any
	// ArgsJSON is the arguments as they arrived, for the approval slip.
	ArgsJSON string
}

// Str is a string argument, "" when missing.
func (c *Call) Str(k string) string {
	s, _ := c.Args[k].(string)
	return s
}

// Int is a numeric argument, 0 when missing.
func (c *Call) Int(k string) int { return toolarg.Int(c.Args, k) }

// Structured reports that the caller wants data rather than prose: Python
// gets JSON where the model gets lines to read.
func (c *Call) Structured() bool { return c.Surface == Python }

// Result is what a tool gives back.
type Result struct {
	// Text is the result the model reads, or the JSON a Python caller gets.
	Text string
	// Image is a data: URL of pixels to show the model with the result.
	Image string
	// Data, when set, is what Python receives instead of Text.
	Data string
}

// Handler runs a tool.
type Handler func(ctx context.Context, c *Call) (Result, error)

// Source lists tools that differ per Bot.
type Source interface {
	// Tools is every tool the Bot has from this source, in a stable order.
	Tools(botID string) []Def
	// Find resolves connector.action for the Python surface. A nil Def with a
	// nil error means the connector is not this source's.
	Find(botID, conn, action string) (*Def, error)
}

// Engine holds the definitions and runs calls against them.
type Engine struct {
	auth    host.Authorizer
	defs    []Def
	byName  map[string]int
	byKey   map[string]int
	sources []Source
}

func New(auth host.Authorizer) *Engine {
	return &Engine{auth: auth, byName: map[string]int{}, byKey: map[string]int{}}
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validName(s string) bool { return nameRE.MatchString(s) }

// Register adds built-in tools. A definition that could not be reached or
// would be ambiguous is a programming error and panics at start.
func (e *Engine) Register(defs ...Def) {
	for _, d := range defs {
		switch {
		case !validName(d.Name):
			panic(fmt.Sprintf("tool %q: not a valid tool name", d.Name))
		case d.Run == nil:
			panic(fmt.Sprintf("tool %s: no handler", d.Name))
		case !d.ExposeCompletion && !d.ExposePython:
			panic(fmt.Sprintf("tool %s: exposed nowhere", d.Name))
		case (d.ExposePython || !d.SelfGated) && (d.Connector == "" || d.Action == ""):
			panic(fmt.Sprintf("tool %s: needs a connector.action", d.Name))
		case d.Local && !d.ExposePython:
			panic(fmt.Sprintf("tool %s: local only means something from Python", d.Name))
		}
		if _, dup := e.byName[d.Name]; dup {
			panic(fmt.Sprintf("tool %s: defined twice", d.Name))
		}
		if d.ExposePython {
			if _, dup := e.byKey[d.Key()]; dup {
				panic(fmt.Sprintf("tool %s: %s is already a Python tool", d.Name, d.Key()))
			}
		}
		e.defs = append(e.defs, d)
		e.byName[d.Name] = len(e.defs) - 1
		if d.Connector != "" && d.Action != "" {
			if _, taken := e.byKey[d.Key()]; !taken {
				e.byKey[d.Key()] = len(e.defs) - 1
			}
		}
	}
}

// AddSource adds a per-Bot source of tools.
func (e *Engine) AddSource(s Source) { e.sources = append(e.sources, s) }

// Defs is every built-in definition, in registration order.
func (e *Engine) Defs() []Def { return append([]Def(nil), e.defs...) }

// Offered is the completion surface for one Bot's run: the built-in tools
// that are exposed and enabled, then the sources' — built-ins first so the
// stable part of the list stays a cacheable prefix.
func (e *Engine) Offered(botID string, subagent bool) []Def {
	out := make([]Def, 0, len(e.defs))
	seen := map[string]bool{}
	for _, d := range e.defs {
		seen[d.Name] = true
		if !d.ExposeCompletion || (subagent && d.LeadOnly) || (d.Enabled != nil && !d.Enabled(botID)) {
			continue
		}
		out = append(out, d)
	}
	for _, s := range e.sources {
		for _, d := range s.Tools(botID) {
			if !d.ExposeCompletion || seen[d.Name] || !validName(d.Name) || d.Run == nil {
				continue
			}
			seen[d.Name] = true
			out = append(out, d)
		}
	}
	return out
}

// ByName resolves a completion tool call.
func (e *Engine) ByName(botID, name string) (*Def, bool) {
	if i, ok := e.byName[name]; ok {
		if !e.defs[i].ExposeCompletion {
			return nil, false
		}
		d := e.defs[i]
		return &d, true
	}
	for _, s := range e.sources {
		for _, d := range s.Tools(botID) {
			if d.Name == name && d.ExposeCompletion && d.Run != nil {
				return &d, true
			}
		}
	}
	return nil, false
}

// ByAction resolves a call from Python by its connector.action. A built-in
// address always wins, so a connector can never stand in for one.
func (e *Engine) ByAction(botID, conn, action string) (*Def, error) {
	if i, ok := e.byKey[conn+"."+action]; ok {
		d := e.defs[i]
		if !d.ExposePython {
			return nil, fmt.Errorf("%s is a chat tool (%s); it is not available from Python", d.Key(), d.Name)
		}
		return &d, nil
	}
	for _, s := range e.sources {
		d, err := s.Find(botID, conn, action)
		if err != nil {
			return nil, err
		}
		if d != nil {
			if !d.ExposePython || d.Run == nil {
				return nil, fmt.Errorf("%s is not available from Python", d.Key())
			}
			return d, nil
		}
	}
	return nil, errors.New("unknown connector")
}

// Run performs one call: the surface must be one the tool is exposed on, the
// arguments are checked, the Bot's rule is consulted (which may wait for the
// human), and then the handler runs.
func (e *Engine) Run(ctx context.Context, c *Call) (Result, error) {
	d := c.Def
	if d == nil || d.Run == nil {
		return Result{}, errors.New("unknown tool")
	}
	if !d.exposed(c.Surface) {
		return Result{}, fmt.Errorf("%s is not available here", d.Name)
	}
	if c.Args == nil {
		c.Args = map[string]any{}
	}
	if c.Surface == Python && d.Local {
		if _, err := e.auth.AuthorizeAction(ctx, c.Bot, c.RunID, d.Connector, d.Action, c.ArgsJSON, d.Mode); err != nil {
			return Result{}, err
		}
		return Result{Text: "ok", Data: `{"ok":true}`}, nil
	}
	if d.Check != nil {
		if err := d.Check(c); err != nil {
			return Result{}, err
		}
	}
	if !d.SelfGated {
		if _, err := e.auth.AuthorizeAction(ctx, c.Bot, c.RunID, d.Connector, d.Action, c.ArgsJSON, d.Mode); err != nil {
			return Result{}, err
		}
	}
	return d.Run(ctx, c)
}

// Need is a Check that requires string arguments to be present.
func Need(keys ...string) func(*Call) error {
	return func(c *Call) error {
		for _, k := range keys {
			if c.Str(k) == "" {
				return fmt.Errorf("%s required", k)
			}
		}
		return nil
	}
}

// Object is the JSON schema of a tool's arguments: an object with these
// properties, of which required must be given.
func Object(props map[string]any, required ...string) json.RawMessage {
	if props == nil {
		props = map[string]any{}
	}
	sch := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sch["required"] = required
	}
	raw, _ := json.Marshal(sch)
	return raw
}

// LLM is the definitions as a model request's tool list.
func LLM(defs []Def) []llm.Tool {
	out := make([]llm.Tool, 0, len(defs))
	for _, d := range defs {
		params := d.Params
		if len(params) == 0 {
			params = Object(nil)
		}
		out = append(out, llm.Tool{Name: d.Name, Description: d.Description, Parameters: params})
	}
	return out
}

// CompletionName is the completion tool name of a connector's action:
// `<slug>__<action>`, held to what every provider accepts (letters, digits,
// `_` and `-`, at most 64). A name that had to be cut ends in a hash of the
// whole, so two long actions stay apart.
func CompletionName(slug, action string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
				return r
			}
			return '_'
		}, s)
	}
	name := clean(slug) + "__" + clean(action)
	if len(name) <= 64 {
		return name
	}
	sum := sha256.Sum256([]byte(slug + "\x00" + action))
	return name[:55] + "_" + hex.EncodeToString(sum[:4])
}
