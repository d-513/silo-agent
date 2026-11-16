package llm

import (
	"slices"
	"strings"
	"testing"
)

func TestFastModelIDs(t *testing.T) {
	if base, fast := SplitFast("claude-opus-5-5:fast"); base != "claude-opus-5-5" || !fast {
		t.Fatalf("SplitFast = %q %v", base, fast)
	}
	if base, fast := SplitFast("openai/gpt-5.6-luna:nitro"); fast || base != "openai/gpt-5.6-luna:nitro" {
		t.Fatalf("another suffix is part of the model: %q %v", base, fast)
	}
	if _, m, err := Parse("anthropic/claude-opus-5-5:fast"); err != nil || m != "claude-opus-5-5:fast" {
		t.Fatalf("a fast Anthropic model: %q %v", m, err)
	}
	for _, id := range []string{"groq/llama-4:fast", "local/qwen:fast"} {
		if _, _, err := Parse(id); err == nil || !strings.Contains(err.Error(), "no fast mode") {
			t.Fatalf("%s: a provider with no fast mode must refuse it, got %v", id, err)
		}
	}
	if _, _, err := Parse("openai/:fast"); err == nil {
		t.Fatal("a bare suffix names no model")
	}
}

func TestClaudeFastLaunch(t *testing.T) {
	if args := (claudeLaunch{model: "opus", fast: true}).args(); !slices.Contains(args, `{"fastMode":true}`) {
		t.Fatalf("a fast launch does not opt in to fast mode: %v", args)
	}
	if args := (claudeLaunch{model: "opus"}).args(); slices.Contains(args, "--settings") {
		t.Fatalf("a plain launch sets fast mode: %v", args)
	}
	err := claudeFastError("opus", "off", "extra_usage_disabled")
	if !strings.Contains(err.Error(), "extra usage") || !strings.Contains(err.Error(), "opus:fast") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	if err := claudeFastError("opus", "cooldown", ""); !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("a cooldown is not worded: %v", err)
	}
}
