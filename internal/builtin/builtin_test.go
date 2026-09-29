package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"silo.agent/internal/security"
)

type fake struct{ d Descriptor }

func (f fake) Descriptor() Descriptor                                { return f.d }
func (f fake) Check(context.Context, Config) error                   { return nil }
func run(context.Context, Env, Config, json.RawMessage) (any, error) { return nil, nil }

func good(key string) Descriptor {
	return Descriptor{
		Key: key, Name: "Fake",
		Fields: []Field{{Key: "host", Required: true}, {Key: "port", Default: "993"}},
		Tools:  []Tool{{Name: "list_things", Mode: security.Allow, Params: Object(nil), Run: run}},
	}
}

func TestRegisterLookupUnregister(t *testing.T) {
	Register(fake{good("test_fake")})
	defer Unregister("test_fake")
	c, ok := Lookup("test_fake")
	if !ok || c.Descriptor().Name != "Fake" {
		t.Fatalf("lookup: %v %v", c, ok)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate key should panic")
		}
	}()
	Register(fake{good("test_fake")})
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Descriptor){
		"camelCase tool": func(d *Descriptor) { d.Tools[0].Name = "listThings" },
		"dup tool":       func(d *Descriptor) { d.Tools = append(d.Tools, d.Tools[0]) },
		"bad mode":       func(d *Descriptor) { d.Tools[0].Mode = "auto" },
		"no run":         func(d *Descriptor) { d.Tools[0].Run = nil },
		"no schema":      func(d *Descriptor) { d.Tools[0].Params = nil },
		"dup field":      func(d *Descriptor) { d.Fields = append(d.Fields, d.Fields[0]) },
		"select no opts": func(d *Descriptor) { d.Fields[0].Type = FieldSelect },
		"missing key":    func(d *Descriptor) { d.Key = "" },
	}
	for name, mut := range cases {
		d := good("x")
		mut(&d)
		if err := validate(d); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if err := validate(good("x")); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAndMissing(t *testing.T) {
	d := good("x")
	cfg := Resolve(d, map[string]string{"host": " ", "junk": "y"})
	if cfg.Get("port") != "993" || cfg.Int("port", 0) != 993 {
		t.Fatalf("default not applied: %v", cfg.Values)
	}
	if _, ok := cfg.Values["junk"]; ok {
		t.Fatal("undeclared key kept")
	}
	if f := Missing(d, cfg); f == nil || f.Key != "host" {
		t.Fatalf("missing = %v", f)
	}
	if f := Missing(d, Resolve(d, map[string]string{"host": "h"})); f != nil {
		t.Fatalf("missing = %v", f)
	}
}

func TestRegisteredDescriptorsAreValid(t *testing.T) {
	for _, k := range Keys() {
		c, _ := Lookup(k)
		d := c.Descriptor()
		if err := validate(d); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(d.Guide) == "" {
			t.Errorf("%s has no guide", k)
		}
	}
}
