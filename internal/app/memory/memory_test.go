package memory

import (
	"strings"
	"testing"
)

func TestFormatRecalled(t *testing.T) {
	out := formatRecalled([]Recalled{{ID: "1", Content: "retry with login"}, {ID: "2", Content: "likes tea"}})
	if !strings.Contains(out, "- [1] (0001-01-01) retry with login") || !strings.Contains(out, "- [2] (0001-01-01) likes tea") {
		t.Fatalf("recall format:\n%s", out)
	}
}
