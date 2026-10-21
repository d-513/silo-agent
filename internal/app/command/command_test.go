package command

import (
	"context"
	"errors"
	"testing"
)

func registry() *Registry {
	nop := func(context.Context, Call) (Result, error) { return Result{Notice: "ok"}, nil }
	return New([]Def{
		{Name: "clear", Channel: true, Run: nop},
		{Name: "model", Args: "[model]", Run: nop},
	})
}

func TestParse(t *testing.T) {
	r := registry()
	for _, tc := range []struct {
		in, cmd, args, text string
	}{
		{in: "/clear", cmd: "clear"},
		{in: "  /clear  ", cmd: "clear"},
		{in: "/CLEAR", cmd: "clear"},
		{in: "/clear@SiloBot", cmd: "clear"},
		{in: "/model gpt 5", cmd: "model", args: "gpt 5"},
		{in: "/model\nsonnet", cmd: "model", args: "sonnet"},
		{in: "/usr/bin/env is where", text: "/usr/bin/env is where"},
		{in: "/clearly not", text: "/clearly not"},
		{in: "hello /clear", text: "hello /clear"},
		{in: "//clear", text: "/clear"},
		{in: "", text: ""},
	} {
		p := r.Parse(tc.in)
		if tc.cmd != "" {
			if p.Def == nil || p.Def.Name != tc.cmd || p.Args != tc.args {
				t.Errorf("%q: got %+v, want /%s %q", tc.in, p, tc.cmd, tc.args)
			}
			continue
		}
		if p.Def != nil || p.Text != tc.text {
			t.Errorf("%q: got %+v, want message %q", tc.in, p, tc.text)
		}
	}
}

func TestChannelSurface(t *testing.T) {
	r := registry()
	if got := r.List(Channel); len(got) != 1 || got[0].Name != "clear" {
		t.Fatalf("channel list %+v", got)
	}
	if got := r.List(Web); len(got) != 2 {
		t.Fatalf("web list %+v", got)
	}
	_, err := r.Run(context.Background(), r.Find("model"), Call{Surface: Channel})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("channel /model: %v", err)
	}
	if res, err := r.Run(context.Background(), r.Find("clear"), Call{Surface: Channel}); err != nil || res.Notice != "ok" {
		t.Fatalf("channel /clear: %+v %v", res, err)
	}
}
