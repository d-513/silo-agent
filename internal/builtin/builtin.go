// Package builtin is the registry of built-in connectors: connectors whose
// tools, schemas, config inputs, and actions are Go code on the Control Plane
// instead of an MCP server. They still ride the connector engine — a library
// preset, a per-Bot copy, `tools.<slug>` in Python, per-action rules, and the
// system-prompt section — only the dispatch is in-process.
//
// Each connector registers itself from init() and is blank-imported by the
// app, the same way channel adapters are.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

// FieldType controls the generated form input on the Connectors tab. The
// values match channel fields so the UI renders both with one component.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldSecret   FieldType = "secret"
	FieldToggle   FieldType = "toggle"
	FieldSelect   FieldType = "select"
	FieldNumber   FieldType = "number"
	FieldTextarea FieldType = "textarea"
)

// Option is one choice for a select field.
type Option struct {
	Value string
	Label string
}

// Field is one declared config input. Secret values are stored apart from the
// rest and never returned to the UI.
type Field struct {
	Key         string
	Label       string
	Description string
	Type        FieldType
	Required    bool
	Default     string
	Options     []Option
	// Advanced fields are folded away in the form; defaults cover most setups.
	Advanced bool
}

// Tool is one action. Params is a JSON schema object (the shape the chat tool
// definitions use); Mode is the action's default rule when the Bot has no
// override (security.Allow / Ask / Deny). Run returns any JSON-marshalable
// value, which is what Python gets back.
type Tool struct {
	Name        string
	Description string
	Params      map[string]any
	Mode        string
	Run         func(ctx context.Context, env Env, cfg Config, args json.RawMessage) (any, error)
}

// Descriptor is the static shape of a built-in connector. Prompt is extra
// system-prompt guidance, seeded onto the library preset.
type Descriptor struct {
	Key         string
	Name        string
	Description string
	Category    string
	Prompt      string
	Guide       string
	Fields      []Field
	Tools       []Tool
}

// Connector is one built-in. Check verifies a config works (a login), and is
// run whenever the config changes or the human presses Refresh.
type Connector interface {
	Descriptor() Descriptor
	Check(ctx context.Context, cfg Config) error
}

// Env is the Bot-bound host a tool runs against. Paths are relative to
// /workspace.
type Env interface {
	ReadFile(ctx context.Context, rel string) (name string, data []byte, err error)
	WriteFile(ctx context.Context, rel string, data []byte) error
}

// Config is a connector copy's resolved config, secrets included.
type Config struct {
	Values map[string]string
}

func (c Config) Get(key string) string {
	return strings.TrimSpace(c.Values[key])
}

func (c Config) Bool(key string) bool {
	switch strings.ToLower(c.Get(key)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (c Config) Int(key string, def int) int {
	n, err := strconv.Atoi(c.Get(key))
	if err != nil {
		return def
	}
	return n
}

// Resolve merges stored values over each field's default. Keys the
// descriptor does not declare are dropped.
func Resolve(d Descriptor, values map[string]string) Config {
	out := map[string]string{}
	for _, f := range d.Fields {
		v, ok := values[f.Key]
		if !ok || strings.TrimSpace(v) == "" {
			v = f.Default
		}
		out[f.Key] = v
	}
	return Config{Values: out}
}

// Missing returns the first required field with no value, or nil.
func Missing(d Descriptor, cfg Config) *Field {
	for i := range d.Fields {
		if d.Fields[i].Required && cfg.Get(d.Fields[i].Key) == "" {
			return &d.Fields[i]
		}
	}
	return nil
}

// Tool looks up one action.
func (d Descriptor) Tool(name string) (Tool, bool) {
	for _, t := range d.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// Field looks up one config input.
func (d Descriptor) Field(key string) (Field, bool) {
	for _, f := range d.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

var (
	mu       sync.RWMutex
	registry = map[string]Connector{}
)

// Register adds a connector. It panics on a malformed descriptor, which only
// happens at init time.
func Register(c Connector) {
	d := c.Descriptor()
	if err := validate(d); err != nil {
		panic("builtin: " + err.Error())
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[d.Key]; dup {
		panic("builtin: duplicate key " + d.Key)
	}
	registry[d.Key] = c
}

// Unregister removes a connector (tests register throwaway ones).
func Unregister(key string) {
	mu.Lock()
	defer mu.Unlock()
	delete(registry, key)
}

func Lookup(key string) (Connector, bool) {
	mu.RLock()
	defer mu.RUnlock()
	c, ok := registry[key]
	return c, ok
}

// Keys lists the registered keys, sorted.
func Keys() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func validate(d Descriptor) error {
	if strings.TrimSpace(d.Key) == "" || strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("descriptor needs key and name")
	}
	fields := map[string]bool{}
	for _, f := range d.Fields {
		if f.Key == "" || fields[f.Key] {
			return fmt.Errorf("%s: empty or duplicate field %q", d.Key, f.Key)
		}
		fields[f.Key] = true
		if f.Type == FieldSelect && len(f.Options) == 0 {
			return fmt.Errorf("%s: select field %s has no options", d.Key, f.Key)
		}
	}
	names := map[string]bool{}
	py := map[string]bool{}
	for _, t := range d.Tools {
		if t.Name == "" || names[t.Name] {
			return fmt.Errorf("%s: empty or duplicate tool %q", d.Key, t.Name)
		}
		names[t.Name] = true
		p := toolsgen.PyName(t.Name)
		if p != t.Name || py[p] {
			return fmt.Errorf("%s: tool %q must be a snake_case Python name", d.Key, t.Name)
		}
		py[p] = true
		if t.Run == nil {
			return fmt.Errorf("%s: tool %s has no Run", d.Key, t.Name)
		}
		switch t.Mode {
		case security.Allow, security.Ask, security.Deny:
		default:
			return fmt.Errorf("%s: tool %s mode must be allow, ask, or deny", d.Key, t.Name)
		}
		if t.Params == nil || t.Params["type"] != "object" {
			return fmt.Errorf("%s: tool %s params must be a JSON schema object", d.Key, t.Name)
		}
		if _, err := json.Marshal(t.Params); err != nil {
			return fmt.Errorf("%s: tool %s params: %w", d.Key, t.Name, err)
		}
	}
	return nil
}

// Object builds a JSON schema object from properties and required names.
func Object(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// Prop is one schema property: a JSON type and a description.
func Prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// List is an array property of the given item type.
func List(item, desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": item}, "description": desc}
}
