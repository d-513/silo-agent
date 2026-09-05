package app

import (
	"strings"
	"testing"

	"silo.agent/internal/db"
)

func TestApplyMemoryCap(t *testing.T) {
	cur := "HEAD\n" + strings.Repeat("x", memoryMax)
	_, err := applyMemory(cur, "more", "", "")
	if err == nil || !strings.Contains(err.Error(), "Compact") {
		t.Fatalf("got %v", err)
	}
	next, err := applyMemory(cur, "", "HEAD\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(next, "HEAD") || len(next) >= len(cur) {
		t.Fatalf("len %d %q", len(next), next[:20])
	}
}

func TestApplyMemoryAppend(t *testing.T) {
	got, err := applyMemory("a", "b", "", "")
	if err != nil || got != "a\nb" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestApplySoulPatch(t *testing.T) {
	got, err := applySoul("hello world", "", "world", "there")
	if err != nil || got != "hello there" {
		t.Fatalf("%q %v", got, err)
	}
	_, err = applySoul("aa aa", "", "aa", "b")
	if err == nil {
		t.Fatal("expected unique fail")
	}
}

func TestBuildSystemInjects(t *testing.T) {
	s := buildSystem(&db.Bot{Name: "Scout", Description: "Mail", Soul: "Be brief.", Memory: "Inbox is IMAP."})
	for _, want := range []string{"Scout", "Mail", "## SOUL", "Be brief.", "## MEMORY", "Inbox is IMAP."} {
		if !strings.Contains(s, want) {
			t.Fatal(want)
		}
	}
	fat := strings.Repeat("m", memoryMax+10)
	over := buildSystem(&db.Bot{Memory: fat})
	if !strings.Contains(over, "Compact it") {
		t.Fatal(over[len(over)-200:])
	}
}

func TestExecDoc(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", Soul: defaultSoul})
	out, err := a.execDoc("b1", "memory", map[string]any{"append": "prefers tea"})
	if err != nil || !strings.HasPrefix(out, "ok") {
		t.Fatalf("%q %v", out, err)
	}
	var b db.Bot
	a.DB.First(&b, "id = ?", "b1")
	if b.Memory != "prefers tea" {
		t.Fatal(b.Memory)
	}
}
