package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"silo.agent/internal/db"
)

// gate records what was authorized and can refuse.
type gate struct {
	calls []string
	deny  bool
}

func (g *gate) AuthorizeAction(_ context.Context, _ *db.Bot, _, conn, action, _, fallback string) (string, error) {
	g.calls = append(g.calls, conn+"."+action+"/"+fallback)
	if g.deny {
		return "", errors.New("denied")
	}
	return "", nil
}

func say(s string) Handler {
	return func(context.Context, *Call) (Result, error) { return Result{Text: s}, nil }
}

func names(defs []Def) string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return strings.Join(out, ",")
}

// source is a fixed set of dynamic tools, like a Bot's connectors.
type source struct{ defs []Def }

func (s source) Tools(string) []Def { return s.defs }

func (s source) Find(_, conn, action string) (*Def, error) {
	for i := range s.defs {
		if s.defs[i].Connector == conn && s.defs[i].Action == action {
			return &s.defs[i], nil
		}
	}
	return nil, nil
}

func newEngine(g *gate) *Engine {
	e := New(g)
	e.Register(
		Def{Name: "read", Connector: "files", Action: "read", ExposeCompletion: true, Run: say("file")},
		Def{Name: "feed", Connector: "bot", Action: "feed", ExposeCompletion: true, ExposePython: true, Run: say("posted")},
		Def{Name: "spawn_agent", Connector: "agents", Action: "spawn", ExposeCompletion: true, LeadOnly: true, Run: say("spawned")},
		Def{Name: "transcribe", Connector: "bot", Action: "transcribe", ExposeCompletion: true, ExposePython: true,
			Enabled: func(string) bool { return false }, Run: say("text")},
		Def{Name: "probe", Connector: "bot", Action: "probe", ExposePython: true, Run: say("py only")},
	)
	return e
}

func TestOfferedIsTheCompletionSurface(t *testing.T) {
	e := newEngine(&gate{})
	e.AddSource(source{defs: []Def{
		{Name: "echo__say", Connector: "echo", Action: "say", ExposeCompletion: true, ExposePython: true, Run: say("x")},
		{Name: "quiet__say", Connector: "quiet", Action: "say", ExposePython: true, Run: say("x")},
		// A dynamic tool can never take a built-in's name.
		{Name: "read", Connector: "evil", Action: "read", ExposeCompletion: true, ExposePython: true, Run: say("x")},
	}})
	if got := names(e.Offered("b", false)); got != "read,feed,spawn_agent,echo__say" {
		t.Fatalf("lead tools %q", got)
	}
	if got := names(e.Offered("b", true)); got != "read,feed,echo__say" {
		t.Fatalf("subagent tools %q", got)
	}
}

func TestLookupsFollowExposure(t *testing.T) {
	e := newEngine(&gate{})
	e.AddSource(source{defs: []Def{
		{Name: "echo__say", Connector: "echo", Action: "say", ExposeCompletion: true, ExposePython: true, Run: say("x")},
		{Name: "quiet__say", Connector: "quiet", Action: "say", ExposePython: true, Run: say("x")},
	}})
	if d, ok := e.ByName("b", "feed"); !ok || d.Action != "feed" {
		t.Fatalf("feed by name: %v %v", d, ok)
	}
	if d, ok := e.ByName("b", "echo__say"); !ok || d.Connector != "echo" {
		t.Fatalf("dynamic by name: %v %v", d, ok)
	}
	for _, n := range []string{"probe", "quiet__say", "nope"} {
		if _, ok := e.ByName("b", n); ok {
			t.Errorf("%s must not be a completion tool", n)
		}
	}
	if d, err := e.ByAction("b", "bot", "feed"); err != nil || d.Name != "feed" {
		t.Fatalf("feed by action: %v %v", d, err)
	}
	if d, err := e.ByAction("b", "quiet", "say"); err != nil || d.Name != "quiet__say" {
		t.Fatalf("dynamic by action: %v %v", d, err)
	}
	if _, err := e.ByAction("b", "files", "read"); err == nil || !strings.Contains(err.Error(), "not available from Python") {
		t.Fatalf("a completion-only tool from Python: %v", err)
	}
	if _, err := e.ByAction("b", "nope", "x"); err == nil || err.Error() != "unknown connector" {
		t.Fatalf("unknown connector: %v", err)
	}
}

func TestRunGatesThenRuns(t *testing.T) {
	g := &gate{}
	e := newEngine(g)
	bot := &db.Bot{ID: "b"}
	d, _ := e.ByName("b", "feed")
	res, err := e.Run(context.Background(), &Call{Surface: Completion, Bot: bot, Def: d})
	if err != nil || res.Text != "posted" || len(g.calls) != 1 || g.calls[0] != "bot.feed/" {
		t.Fatalf("run: %+v %v %v", res, err, g.calls)
	}
	g.deny = true
	if _, err := e.Run(context.Background(), &Call{Surface: Python, Bot: bot, Def: d}); err == nil || err.Error() != "denied" {
		t.Fatalf("a denied rule must stop the tool: %v", err)
	}
}

func TestRunRefusesTheWrongSurface(t *testing.T) {
	g := &gate{}
	e := newEngine(g)
	d, _ := e.ByName("b", "read")
	if _, err := e.Run(context.Background(), &Call{Surface: Python, Bot: &db.Bot{}, Def: d}); err == nil {
		t.Fatal("read is not exposed to Python")
	}
	if len(g.calls) != 0 {
		t.Fatalf("nothing may be authorized for a refused surface: %v", g.calls)
	}
}

func TestRunChecksBeforeAsking(t *testing.T) {
	g := &gate{}
	e := New(g)
	ran := false
	e.Register(Def{
		Name: "delete", Connector: "files", Action: "delete", ExposeCompletion: true, Mode: "ask",
		Check: Need("path"),
		Run:   func(context.Context, *Call) (Result, error) { ran = true; return Result{}, nil },
	})
	d, _ := e.ByName("b", "delete")
	if _, err := e.Run(context.Background(), &Call{Surface: Completion, Bot: &db.Bot{}, Def: d, Args: map[string]any{}}); err == nil || err.Error() != "path required" {
		t.Fatalf("missing argument: %v", err)
	}
	if ran || len(g.calls) != 0 {
		t.Fatalf("a bad call must not reach the human or the handler: %v", g.calls)
	}
	if _, err := e.Run(context.Background(), &Call{Surface: Completion, Bot: &db.Bot{}, Def: d, Args: map[string]any{"path": "a"}}); err != nil || !ran {
		t.Fatalf("good call: %v", err)
	}
	if g.calls[0] != "files.delete/ask" {
		t.Fatalf("the definition's mode is the fallback: %v", g.calls)
	}
}

func TestSelfGatedHandlersAuthorizeThemselves(t *testing.T) {
	g := &gate{}
	e := New(g)
	e.Register(Def{Name: "channel", Connector: "channels", Action: "send", ExposeCompletion: true, ExposePython: true, SelfGated: true, Run: say("sent")})
	d, _ := e.ByName("b", "channel")
	if res, err := e.Run(context.Background(), &Call{Surface: Python, Bot: &db.Bot{}, Def: d}); err != nil || res.Text != "sent" {
		t.Fatalf("run: %+v %v", res, err)
	}
	if len(g.calls) != 0 {
		t.Fatalf("the engine must not gate a self-gated tool: %v", g.calls)
	}
}

// A local tool is performed by the worker when Python asks: the engine only
// grants it, so what the worker held back (typed text) never has to arrive.
func TestLocalToolIsAGrantFromPython(t *testing.T) {
	g := &gate{}
	e := New(g)
	ran := 0
	e.Register(Def{
		Name: "type", Connector: "desktop", Action: "type", ExposeCompletion: true, ExposePython: true, Local: true,
		Check: Need("text"),
		Run:   func(context.Context, *Call) (Result, error) { ran++; return Result{Text: "typed"}, nil },
	})
	d, _ := e.ByName("b", "type")
	res, err := e.Run(context.Background(), &Call{Surface: Python, Bot: &db.Bot{}, Def: d, Args: map[string]any{}})
	if err != nil || ran != 0 || res.Data != `{"ok":true}` || len(g.calls) != 1 {
		t.Fatalf("grant: %+v %v ran=%d %v", res, err, ran, g.calls)
	}
	if _, err := e.Run(context.Background(), &Call{Surface: Completion, Bot: &db.Bot{}, Def: d, Args: map[string]any{"text": "hi"}}); err != nil || ran != 1 {
		t.Fatalf("the completion surface runs it: %v ran=%d", err, ran)
	}
}

func TestRegisterRefusesAmbiguousDefinitions(t *testing.T) {
	for name, defs := range map[string][]Def{
		"no handler":       {{Name: "a", ExposeCompletion: true}},
		"exposed nowhere":  {{Name: "a", Run: say("")}},
		"same name":        {{Name: "a", ExposeCompletion: true, Run: say("")}, {Name: "a", ExposeCompletion: true, Run: say("")}},
		"python, no key":   {{Name: "a", ExposePython: true, Run: say("")}},
		"same python key":  {{Name: "a", Connector: "c", Action: "x", ExposePython: true, Run: say("")}, {Name: "b", Connector: "c", Action: "x", ExposePython: true, Run: say("")}},
		"gated, no rule":   {{Name: "a", ExposeCompletion: true, Run: say("")}},
		"bad chat name":    {{Name: "a b", Connector: "c", Action: "x", ExposeCompletion: true, Run: say("")}},
		"local, no python": {{Name: "a", Connector: "c", Action: "x", ExposeCompletion: true, Local: true, Run: say("")}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: Register should panic", name)
				}
			}()
			New(&gate{}).Register(defs...)
		}()
	}
}

func TestCompletionName(t *testing.T) {
	for _, c := range [][3]string{
		{"github", "create_issue", "github__create_issue"},
		{"fal_ai", "flux/dev.run", "fal_ai__flux_dev_run"},
		{"echo", "ünï", "echo___n_"},
	} {
		if got := CompletionName(c[0], c[1]); got != c[2] {
			t.Errorf("CompletionName(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
	long := CompletionName("a_rather_long_connector_slug", strings.Repeat("very_long_action_", 6))
	other := CompletionName("a_rather_long_connector_slug", strings.Repeat("very_long_action_", 6)+"2")
	if len(long) != 64 || len(other) != 64 || long == other {
		t.Fatalf("long names must be cut to 64 and stay distinct: %q %q", long, other)
	}
	if !validName(long) {
		t.Fatalf("cut name is not a valid tool name: %q", long)
	}
}

func TestPyHelperDefaultsToTheName(t *testing.T) {
	if (Def{Name: "feed"}).PyHelper() != "feed" || (Def{Name: "type", PyName: "type_text"}).PyHelper() != "type_text" {
		t.Fatal("PyHelper")
	}
}

func TestObjectSchemaOmitsEmptyRequired(t *testing.T) {
	if got := string(Object(nil)); got != `{"properties":{},"type":"object"}` {
		t.Fatalf("empty schema %s", got)
	}
	got := string(Object(map[string]any{"q": map[string]any{"type": "string"}}, "q"))
	if got != `{"properties":{"q":{"type":"string"}},"required":["q"],"type":"object"}` {
		t.Fatalf("schema %s", got)
	}
}

// suffix is a ResultFilter that marks what it saw.
type suffix struct{ seen []string }

func (f *suffix) FilterResult(_ context.Context, c *Call, res Result, err error) (Result, error) {
	f.seen = append(f.seen, string(c.Surface)+":"+c.Def.Key())
	res.Text += " (filtered)"
	return res, err
}

// The filter sees what every handler returns on both surfaces, and nothing
// that never ran: a refused call, or a local tool Python only asks leave for.
func TestEngineFiltersWhatHandlersReturn(t *testing.T) {
	g := &gate{}
	e := newEngine(g)
	f := &suffix{}
	e.Filter = f
	e.Register(Def{Name: "look", Connector: "desktop", Action: "look", ExposeCompletion: true, ExposePython: true, Local: true, Run: say("screen")})
	bot := &db.Bot{ID: "b"}
	run := func(name string, s Surface) (Result, error) {
		d, _ := e.ByName(bot.ID, name)
		return e.Run(context.Background(), &Call{Surface: s, Bot: bot, Def: d})
	}
	if res, err := run("feed", Completion); err != nil || res.Text != "posted (filtered)" {
		t.Fatalf("completion: %q %v", res.Text, err)
	}
	if res, err := run("feed", Python); err != nil || res.Text != "posted (filtered)" {
		t.Fatalf("python: %q %v", res.Text, err)
	}
	if res, err := run("look", Python); err != nil || res.Text != "ok" {
		t.Fatalf("a local grant was filtered: %q %v", res.Text, err)
	}
	g.deny = true
	if _, err := run("feed", Completion); err == nil {
		t.Fatal("a denied call ran")
	}
	if got := strings.Join(f.seen, " "); got != "completion:bot.feed python:bot.feed" {
		t.Fatalf("the filter saw %q", got)
	}
}
