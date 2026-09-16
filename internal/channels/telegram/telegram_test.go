package telegram

import (
	"strings"
	"testing"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"

	"silo.agent/internal/db"
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

func TestLoadPeersFromState(t *testing.T) {
	a := &Adapter{
		clients:   map[string]*telegram.Client{},
		selfs:     map[string]*tg.User{},
		peers:     map[string]map[string]peerRef{},
		seen:      map[string]map[int]struct{}{},
		noHistory: map[string]bool{},
	}
	ch := &db.Channel{
		ID:        "ch1",
		StateJSON: `{"Kind":"select","Values":{"peer:user:5":"{\"kind\":\"user\",\"id\":5,\"access_hash\":9,\"username\":\"bob\"}"}}`,
	}
	a.loadPeers(ch)
	ref, ok := a.peer("ch1", "user:5")
	if !ok {
		t.Fatal("peer not restored from state")
	}
	if ref.AccessHash != 9 || ref.Username != "bob" {
		t.Fatalf("peer restored wrong: %+v", ref)
	}
	// Persisting again must reuse the key.
	st := a.peersState("ch1")
	if _, ok := st["peer:user:5"]; !ok {
		t.Fatalf("peer not serialized: %#v", st)
	}
	if a.inputPeerFor("ch1", "user:5") == nil {
		t.Fatal("input peer not rebuilt after restart")
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
