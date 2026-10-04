package models

import (
	"strings"
	"testing"

	"silo.agent/internal/llm"
)

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

// TestFormatLLMRequestToolOrderMatchesProviderRender guards the prompt order the
// providers actually cache: tools render first, then system, then messages. A
// debug view that puts tools after the user turn misrepresents the cached
// prefix and hides tool/schema changes that invalidate it.
func TestFormatLLMRequestToolOrderMatchesProviderRender(t *testing.T) {
	rec := llm.Record{
		Provider: "openrouter",
		Model:    "openai/gpt",
		System:   []llm.SystemBlock{{Text: "BASE"}},
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "hello"}},
		Tools:    []llm.Tool{{Name: "read", Description: "read a file"}},
	}
	out := formatLLMRequest(rec)
	tools := strings.Index(out, "## TOOLS")
	system := strings.Index(out, "## SYSTEM")
	messages := strings.Index(out, "## MESSAGES")
	if tools < 0 || system < 0 || messages < 0 {
		t.Fatalf("formatLLMRequest missing a section:\n%s", out)
	}
	if !(tools < system && system < messages) {
		t.Fatalf("sections out of render order tools=%d system=%d messages=%d:\n%s", tools, system, messages, out)
	}
}
