package app

import (
	"encoding/base64"
	"testing"

	"silo.agent/internal/llm"
)

func TestIsLookTool(t *testing.T) {
	for _, name := range []string{"look", "exec_python", "terminal"} {
		if !isLookTool(name) {
			t.Fatalf("%s should be a look tool", name)
		}
	}
	for _, name := range []string{"present", "artifact", "click"} {
		if isLookTool(name) {
			t.Fatalf("%s should not be a look tool", name)
		}
	}
}

func TestPruneLookImagesKeepsLatestAndPresent(t *testing.T) {
	present := llm.Message{Role: llm.RoleUser, Text: "You presented this image.", Images: []llm.Image{{Mime: "image/png"}}}
	look1 := llm.Message{Role: llm.RoleUser, Text: lookCoordLaw, Images: []llm.Image{{Mime: "image/jpeg"}}}
	msgs := []llm.Message{present, look1}

	// runLoop prunes before appending the newest look.
	pruneLookImages(msgs)
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: lookCoordLaw, Images: []llm.Image{{Mime: "image/jpeg"}}})

	if len(msgs[0].Images) != 1 {
		t.Fatal("present image must survive pruning")
	}
	if len(msgs[1].Images) != 0 || msgs[1].Text != "Earlier screenshot omitted — act on the current one." {
		t.Fatalf("old look not pruned: %+v", msgs[1])
	}
	if len(msgs[2].Images) != 1 {
		t.Fatal("newest look must keep its pixels")
	}
}

func TestPresentImageURLDropsTruncated(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("not-a-real-png"))
	full := `{"name":"x.png","data":"` + data + `","truncated":false}`
	if got := presentImageURL("x.png", full); got == "" {
		t.Fatal("valid payload should produce a data URL")
	}
	trunc := `{"name":"x.png","data":"` + data + `","truncated":true}`
	if got := presentImageURL("x.png", trunc); got != "" {
		t.Fatalf("truncated payload must not attach, got %q", got)
	}
}
