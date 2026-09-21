package app

import (
	"strings"
	"testing"

	"silo.agent/internal/llm"
	"silo.agent/internal/security"
)

func TestParseVerdict(t *testing.T) {
	cases := map[string]string{
		"approve":         security.Allow,
		"Approve.\n":      security.Allow,
		"allow":           security.Allow,
		"deny":            security.Deny,
		"Denied":          security.Deny,
		"ask":             security.Ask,
		"human review":    security.Ask,
		"":                security.Ask,
		"whatever":        security.Ask,
		"yes, it is safe": security.Allow,
	}
	for in, want := range cases {
		if got := parseVerdict(in); got != want {
			t.Fatalf("parseVerdict(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTidyTextCollapsesBlankRuns(t *testing.T) {
	got := tidyText("a\n\n\n\n\nb  \n\nc\n")
	if got != "a\n\nb\n\nc" {
		t.Fatalf("tidyText = %q", got)
	}
}

func TestFormatLLMRequestIncludesSystemAndMessages(t *testing.T) {
	rec := llm.Record{
		Provider: "openrouter",
		Model:    "openai/gpt",
		System:   []llm.SystemBlock{{Text: "BASE"}},
		Messages: []llm.Message{
			{Role: llm.RoleUser, Text: "hello"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Name: "read", Arguments: `{"path":"x"}`}}},
		},
		Tools: []llm.Tool{{Name: "read", Description: "read a file"}},
	}
	out := formatLLMRequest(rec)
	for _, want := range []string{"openrouter/openai/gpt", "BASE", "hello", "read", "read a file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatLLMRequest missing %q:\n%s", want, out)
		}
	}
}
