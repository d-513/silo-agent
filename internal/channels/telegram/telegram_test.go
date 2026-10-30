package telegram

import (
	"strings"
	"testing"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"

	"silo.agent/internal/channels"
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

func TestAsPhoto(t *testing.T) {
	for _, tc := range []struct {
		mime string
		size int
		want bool
	}{
		{"image/png", 100, true},
		{"image/jpeg", maxPhotoBytes, true},
		{"IMAGE/WEBP", 100, true},
		{"image/png", maxPhotoBytes + 1, false},
		{"image/svg+xml", 100, false},
		{"image/gif", 100, false}, // a photo would lose the animation
		{"application/pdf", 100, false},
		{"", 100, false},
	} {
		if got := asPhoto(channels.Attachment{Mime: tc.mime, Data: make([]byte, tc.size)}); got != tc.want {
			t.Errorf("asPhoto(%s, %d) = %v", tc.mime, tc.size, got)
		}
	}
}

func TestMediaOf(t *testing.T) {
	a := &Adapter{clients: map[string]*telegram.Client{}}
	photo := &tg.Photo{ID: 1, Sizes: []tg.PhotoSizeClass{
		&tg.PhotoStrippedSize{Type: "i"},
		&tg.PhotoSize{Type: "s", W: 90, H: 90, Size: 100},
		&tg.PhotoSizeProgressive{Type: "y", W: 1280, H: 720, Sizes: []int{500, 9000}},
		&tg.PhotoSize{Type: "m", W: 320, H: 320, Size: 2000},
	}}
	got := a.mediaOf("c", &tg.MessageMediaPhoto{Photo: photo})
	if len(got) != 1 || got[0].Size != 9000 || got[0].Mime != "image/jpeg" || got[0].Voice {
		t.Fatalf("photo = %+v", got)
	}

	voice := &tg.Document{ID: 2, MimeType: "audio/ogg", Size: 44, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAudio{Voice: true}}}
	got = a.mediaOf("c", &tg.MessageMediaDocument{Document: voice})
	if len(got) != 1 || !got[0].Voice || got[0].Name != "voice.ogg" || got[0].Mime != "audio/ogg" {
		t.Fatalf("voice = %+v", got)
	}

	song := &tg.Document{ID: 3, MimeType: "audio/mpeg", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAudio{}, &tg.DocumentAttributeFilename{FileName: "song.mp3"}}}
	got = a.mediaOf("c", &tg.MessageMediaDocument{Document: song})
	if len(got) != 1 || got[0].Voice || got[0].Name != "song.mp3" {
		t.Fatalf("audio file = %+v", got)
	}

	sticker := &tg.Document{ID: 4, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{}}}
	if got := a.mediaOf("c", &tg.MessageMediaDocument{Document: sticker}); got != nil {
		t.Fatalf("sticker = %+v", got)
	}
	if got := a.mediaOf("c", &tg.MessageMediaGeo{}); got != nil {
		t.Fatalf("geo = %+v", got)
	}
	if _, err := a.mediaOf("gone", &tg.MessageMediaDocument{Document: voice})[0].Fetch(t.Context()); err == nil {
		t.Fatal("a download with no client passed")
	}
}
