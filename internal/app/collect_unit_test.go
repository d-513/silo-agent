package app

import (
	"strings"
	"testing"

	"silo.agent/internal/llm"
)

func TestChunkEntriesKeepsNewestAndCaps(t *testing.T) {
	entry := func(tag string, runes int) string { return tag + strings.Repeat("x", runes) }
	// Each entry is ~300 tokens; a 1000-token budget fits three per chunk.
	var entries []string
	for i := 0; i < 20; i++ {
		entries = append(entries, entry(string(rune('a'+i)), 1050))
	}
	chunks, dropped := chunkEntries(entries, 1000, 2)
	if len(chunks) != 2 {
		t.Fatalf("chunks %d", len(chunks))
	}
	if dropped == 0 {
		t.Fatal("an over-budget delta must drop its oldest entries")
	}
	if !strings.HasPrefix(chunks[len(chunks)-1][strings.LastIndex(chunks[len(chunks)-1], "\n\n")+2:], "t") {
		t.Fatal("the newest entry must be read")
	}
	for _, c := range chunks {
		if runeTokens(c) > 1100 {
			t.Fatalf("chunk of %d tokens over budget", runeTokens(c))
		}
	}

	one, _ := chunkEntries([]string{entry("z", 50000)}, 1000, 3)
	if len(one) != 1 || len([]rune(one[0])) > 3100 {
		t.Fatalf("a huge entry must be truncated, got %d runes", len([]rune(one[0])))
	}
}

func TestParseCollectReplyTolerant(t *testing.T) {
	r, ok := parseCollectReply("Sure!\n```json\n{\"save\":[{\"kind\":\"lesson\",\"text\":\"x\"}]}\n```")
	if !ok || len(r.Save) != 1 || r.Save[0].Kind != "lesson" {
		t.Fatalf("fenced reply: %+v %v", r, ok)
	}
	if _, ok := parseCollectReply("nothing to add"); ok {
		t.Fatal("prose must not parse")
	}
	if r, ok := parseCollectReply("{}"); !ok || len(r.Save) != 0 {
		t.Fatal("{} is a valid empty reply")
	}
}

func TestCollectCapsKeepMoreOfFailures(t *testing.T) {
	long := strings.Repeat("y", 2000)
	msgs := []llm.Message{
		{Role: llm.RoleTool, Text: "ok " + long},
		{Role: llm.RoleTool, Text: "Traceback (most recent call last): " + long},
	}
	entries, _ := transcriptEntries(msgs, collectCaps)
	okLen, errLen := len([]rune(entries[0])), len([]rune(entries[1]))
	if okLen > 450 || errLen < 800 || errLen > 850 {
		t.Fatalf("result caps: ok %d, error %d", okLen, errLen)
	}
}

func TestFormatRecalledMarksLessons(t *testing.T) {
	out := formatRecalled([]recalled{{ID: "1", Kind: memoryLesson, Content: "retry with login"}, {ID: "2", Kind: memoryFact, Content: "likes tea"}})
	if !strings.Contains(out, "lesson: retry with login") || strings.Contains(out, "lesson: likes tea") {
		t.Fatalf("recall format:\n%s", out)
	}
}
