package telegram

import (
	"strings"
	"testing"
)

func TestSplitMessage(t *testing.T) {
	long := strings.Repeat("word ", 3000) // 15000 chars
	parts := splitMessage(long, 4000)
	if len(parts) < 2 {
		t.Fatalf("expected split, got %d part(s)", len(parts))
	}
	total := 0
	for i, p := range parts {
		if len(p) > 4000 {
			t.Fatalf("part %d too long: %d", i, len(p))
		}
		total += len(strings.Fields(p))
	}
	if total != len(strings.Fields(long)) {
		t.Fatalf("words lost: %d != %d", total, len(strings.Fields(long)))
	}
}

func TestSplitMessageShort(t *testing.T) {
	parts := splitMessage("hello", 4000)
	if len(parts) != 1 || parts[0] != "hello" {
		t.Fatalf("got %v", parts)
	}
}

func TestPeerRefRoundTrip(t *testing.T) {
	ref := peerRef{Kind: "channel", ID: 42, AccessHash: 99, Title: "Team"}
	if ref.externalID() != "channel:42" {
		t.Fatalf("external id %q", ref.externalID())
	}
	ip := ref.inputPeer()
	if ip == nil {
		t.Fatal("nil input peer")
	}
}
