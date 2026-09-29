package llm

import (
	"context"
	"strings"
)

// Thinking levels are the provider-neutral names for how hard a model reasons
// before it answers. Each provider maps them onto its own knob (OpenRouter
// reasoning.effort, OpenAI reasoning_effort, Anthropic output_config.effort or
// a thinking budget). An empty level means "the model's default": nothing is
// sent.
const (
	ThinkingOff     = "off"
	ThinkingMinimal = "minimal"
	ThinkingLow     = "low"
	ThinkingMedium  = "medium"
	ThinkingHigh    = "high"
	ThinkingXHigh   = "xhigh"
	ThinkingMax     = "max"
)

// ThinkingOrder is every level from least to most reasoning.
var ThinkingOrder = []string{ThinkingOff, ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh, ThinkingMax}

// ThinkingRank is a level's position in ThinkingOrder, or -1.
func ThinkingRank(level string) int {
	level = strings.ToLower(strings.TrimSpace(level))
	for i, l := range ThinkingOrder {
		if l == level {
			return i
		}
	}
	return -1
}

// ValidThinking reports whether level is a known level (empty is valid: the
// model default).
func ValidThinking(level string) bool {
	return strings.TrimSpace(level) == "" || ThinkingRank(level) >= 0
}

// SortThinking returns the known levels in ThinkingOrder, without duplicates.
func SortThinking(levels []string) []string {
	seen := map[int]bool{}
	for _, l := range levels {
		if r := ThinkingRank(l); r >= 0 {
			seen[r] = true
		}
	}
	out := make([]string, 0, len(seen))
	for i, l := range ThinkingOrder {
		if seen[i] {
			out = append(out, l)
		}
	}
	return out
}

// NearestThinking fits a chosen level to what a model accepts: the level
// itself when listed, else the closest listed level (ties go to the lower
// one), else "" (send nothing). It lets a chat keep its level across a model
// switch.
func NearestThinking(level string, levels []string) string {
	want := ThinkingRank(level)
	if want < 0 || len(levels) == 0 {
		return ""
	}
	best, bestDist := "", len(ThinkingOrder)+1
	for _, l := range levels {
		r := ThinkingRank(l)
		if r < 0 {
			continue
		}
		d := r - want
		if d < 0 {
			d = -d
		}
		if d < bestDist || (d == bestDist && r < ThinkingRank(best)) {
			best, bestDist = ThinkingOrder[r], d
		}
	}
	return best
}

// Thinker is the optional interface a provider implements when it can report
// which thinking levels a model accepts (from the gateway's model list, the
// Models API, or known model families). An empty list means the model has no
// selectable thinking; an error means the provider could not tell, and callers
// fall back to operator config.
type Thinker interface {
	ThinkingLevels(ctx context.Context, model string) ([]string, error)
}

// ThinkingBlock is a provider-signed reasoning block (Anthropic thinking or
// redacted_thinking). Providers that need their reasoning handed back
// unchanged on the next request of a tool loop emit it; the agent loop
// attaches it to the assistant message and never persists it.
type ThinkingBlock struct {
	Text      string
	Signature string
	// Redacted holds the opaque data of a redacted_thinking block.
	Redacted string
}
