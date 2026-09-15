package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
)

func TestCleanAttachments(t *testing.T) {
	out := cleanAttachments([]*v1.Attachment{
		{Name: "a.pdf", Path: "tmp/a.pdf", Size: 3},
		{Name: "", Path: "/workspace/tmp/b.png", Size: 4},
		nil,
		{Name: "bad", Path: "", Size: 1},
	})
	if len(out) != 2 {
		t.Fatalf("got %d: %+v", len(out), out)
	}
	if out[0].Path != "tmp/a.pdf" || out[0].Name != "a.pdf" {
		t.Fatalf("%+v", out[0])
	}
	if out[1].Path != "tmp/b.png" || out[1].Name != "b.png" {
		t.Fatalf("%+v", out[1])
	}
}

func TestCleanAttachmentsCap(t *testing.T) {
	in := make([]*v1.Attachment, 0, maxAttachments+5)
	for i := 0; i < maxAttachments+5; i++ {
		in = append(in, &v1.Attachment{Path: "tmp/x"})
	}
	if got := len(cleanAttachments(in)); got != maxAttachments {
		t.Fatalf("got %d", got)
	}
}

func TestHistoryAttachmentsNote(t *testing.T) {
	a := testApp(t, nil)
	if err := a.DB.Create(&db.Run{ID: "r1", ChatID: "c1", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	a.emitUser("b1", "c1", "r1", "read this", []*v1.Attachment{{Name: "a.pdf", Path: "tmp/a.pdf", Size: 10}})
	msgs := a.historyFromDB("c1")
	if len(msgs) != 1 {
		t.Fatalf("msgs %d", len(msgs))
	}
	b, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "read this") || !strings.Contains(string(b), "tmp/a.pdf") {
		t.Fatalf("attachment note missing: %s", b)
	}
}
