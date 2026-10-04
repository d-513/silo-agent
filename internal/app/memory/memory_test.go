package memory

import (
	"strings"
	"testing"
)

func TestFormatRecalledMarksLessons(t *testing.T) {
	out := formatRecalled([]Recalled{{ID: "1", Kind: Lesson, Content: "retry with login"}, {ID: "2", Kind: Fact, Content: "likes tea"}})
	if !strings.Contains(out, "lesson: retry with login") || strings.Contains(out, "lesson: likes tea") {
		t.Fatalf("recall format:\n%s", out)
	}
}
