package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// --- fakes ---

const (
	botID    = "@silo:example.org"
	token    = "syt_secret_token"
	group    = "!group:example.org"
	direct   = "!dm:example.org"
	secret   = "!secret:example.org"
	lobbyID  = "!lobby:example.org"
	lobbyAka = "#lobby:example.org"
)

// fakeHost plays the Control Plane for the adapter.
type fakeHost struct {
	mu        sync.Mutex
	current   *db.Channel
	delivered []channels.Inbound
	states    []channels.State
}

func (h *fakeHost) GetSecret(_, _ string) (string, error) { return "", errors.New("unused") }
func (h *fakeHost) DeliverInbound(_ context.Context, _ *db.Channel, in channels.Inbound) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = append(h.delivered, in)
	return nil
}
func (h *fakeHost) PublishState(_ string, st channels.State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.states = append(h.states, st)
}
func (h *fakeHost) CurrentChannel(string) (*db.Channel, bool) { return h.current, h.current != nil }
func (h *fakeHost) DataDir() string                           { return "" }
func (h *fakeHost) DatabaseURL() string                       { return "" }

func (h *fakeHost) inbound() []channels.Inbound {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.delivered)
}

func (h *fakeHost) lastState() channels.State {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.states) == 0 {
		return channels.State{}
	}
	return h.states[len(h.states)-1]
}

// call is one request the adapter made.
type call struct {
	Method string
	Path   string
	Query  url.Values
	Auth   string
	Type   string
	Body   []byte
}

func (c call) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("%s %s: body %q: %v", c.Method, c.Path, c.Body, err)
	}
	return m
}

type handler func(c call) (int, any)

// fakeServer is a homeserver: per-route handlers, a record of every call, and
// a sync that answers the first request from initial and the rest from a queue.
type fakeServer struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	calls   []call
	routes  map[string]handler // "METHOD path-prefix"
	initial map[string]any
	syncs   chan map[string]any
	members map[string]map[string]string // room -> MXID -> display name
}

func matrixError(code, msg string) map[string]any {
	return map[string]any{"errcode": code, "error": msg}
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{
		t:       t,
		routes:  map[string]handler{},
		initial: map[string]any{"next_batch": "s0"},
		syncs:   make(chan map[string]any, 16),
		members: map[string]map[string]string{
			group:  {botID: "Silo", "@alice:example.org": "Alice", "@bob:example.org": "Bob"},
			direct: {botID: "Silo", "@alice:example.org": "Alice"},
		},
	}
	f.on("GET /_matrix/client/v3/account/whoami", func(call) (int, any) {
		return 200, map[string]any{"user_id": botID}
	})
	f.on("GET /_matrix/client/v3/profile/", func(call) (int, any) {
		return 200, map[string]any{"displayname": "Silo"}
	})
	f.on("GET /_matrix/client/v3/sync", func(c call) (int, any) {
		if c.Query.Get("since") == "" {
			return 200, f.initial
		}
		select {
		case resp := <-f.syncs:
			return 200, resp
		case <-time.After(50 * time.Millisecond):
			return 200, map[string]any{"next_batch": c.Query.Get("since")}
		}
	})
	f.on("GET /_matrix/client/v3/rooms/", func(c call) (int, any) {
		rest := strings.TrimPrefix(c.Path, "/_matrix/client/v3/rooms/")
		room, tail, _ := strings.Cut(rest, "/")
		switch tail {
		case "joined_members":
			joined := map[string]any{}
			for id, name := range f.members[room] {
				joined[id] = map[string]any{"display_name": name}
			}
			return 200, map[string]any{"joined": joined}
		case "state/m.room.encryption":
			if room == secret {
				return 200, map[string]any{"algorithm": "m.megolm.v1.aes-sha2"}
			}
		}
		return 404, matrixError("M_NOT_FOUND", "not found")
	})
	f.on("PUT /_matrix/client/v3/rooms/", func(c call) (int, any) {
		return 200, map[string]any{"event_id": "$sent" + c.Path[len(c.Path)-6:]}
	})
	f.on("POST /_matrix/media/v3/upload", func(call) (int, any) {
		return 200, map[string]any{"content_uri": "mxc://example.org/up1"}
	})
	f.on("GET /_matrix/client/v3/directory/room/", func(c call) (int, any) {
		if strings.HasSuffix(c.Path, "/"+lobbyAka) {
			return 200, map[string]any{"room_id": lobbyID}
		}
		return 404, matrixError("M_NOT_FOUND", "no such alias")
	})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := call{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Auth: r.Header.Get("Authorization"), Type: r.Header.Get("Content-Type"), Body: body}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		var h handler
		best := ""
		for route, rh := range f.routes {
			if strings.HasPrefix(r.Method+" "+r.URL.Path, route) && len(route) > len(best) {
				best, h = route, rh
			}
		}
		f.mu.Unlock()
		status, out := 404, any(matrixError("M_UNRECOGNIZED", "unknown endpoint"))
		if h != nil {
			status, out = h(c)
		}
		if raw, ok := out.([]byte); ok {
			w.WriteHeader(status)
			_, _ = w.Write(raw)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) on(route string, h handler) {
	f.mu.Lock()
	f.routes[route] = h
	f.mu.Unlock()
}

// made lists the calls whose "METHOD path" starts with route.
func (f *fakeServer) made(route string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if strings.HasPrefix(c.Method+" "+c.Path, route) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeServer) config(extra ...string) channels.Config {
	cfg := channels.Config{Values: map[string]string{"homeserver_url": f.srv.URL, "access_token": token}}
	for i := 0; i+1 < len(extra); i += 2 {
		cfg.Values[extra[i]] = extra[i+1]
	}
	return cfg
}

// connected signs a fresh adapter in against the fake, as Start would.
func connected(t *testing.T, f *fakeServer, ch *db.Channel) (*Adapter, *conn) {
	t.Helper()
	a := New()
	c, err := login(context.Background(), f.config())
	if err != nil {
		t.Fatal(err)
	}
	a.putConn(ch.ID, c)
	return a, c
}

func channel(target string) *db.Channel {
	return &db.Channel{ID: "ch1", BotID: "b1", Adapter: "matrix", Name: "Matrix", ExternalID: target, Enabled: true, Inbound: true}
}

// message is an m.room.message event; extra is merged into its content.
func message(id, sender, body string, extra map[string]any) map[string]any {
	content := map[string]any{"msgtype": "m.text", "body": body}
	for k, v := range extra {
		content[k] = v
	}
	return map[string]any{"type": "m.room.message", "event_id": id, "sender": sender, "origin_server_ts": 1700000000000, "content": content}
}

func asEvent(t *testing.T, m map[string]any) event {
	t.Helper()
	b, _ := json.Marshal(m)
	var ev event
	if err := json.Unmarshal(b, &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

func syncOf(next string, rooms map[string][]map[string]any) map[string]any {
	join := map[string]any{}
	for id, events := range rooms {
		join[id] = map[string]any{"timeline": map[string]any{"events": events}}
	}
	return map[string]any{"next_batch": next, "rooms": map[string]any{"join": join}}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// start runs a real Start until the test ends.
func start(t *testing.T, a *Adapter, ch *db.Channel, cfg channels.Config, h *fakeHost) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Start(ctx, ch, cfg, h) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Start ended with %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Start did not stop")
		}
	})
}

// --- start ---

func TestStartDeliversOnlyNewMessagesOfTheBoundRoom(t *testing.T) {
	f := newFakeServer(t)
	// What was said before the channel started is never delivered.
	f.initial = syncOf("s0", map[string][]map[string]any{direct: {message("$old", "@alice:example.org", "said before", nil)}})
	f.initial["rooms"].(map[string]any)["invite"] = map[string]any{"!invited:example.org": map[string]any{}}
	f.on("POST /_matrix/client/v3/join/", func(call) (int, any) { return 200, map[string]any{"room_id": "!invited:example.org"} })
	ch := channel(direct)
	h := &fakeHost{current: ch}
	start(t, New(), ch, f.config(), h)

	waitFor(t, "the connected state", func() bool { return h.lastState().Message == "Connected as "+botID })
	f.syncs <- syncOf("s1", map[string][]map[string]any{
		direct: {
			message("$1", "@alice:example.org", "hello there", nil),
			message("$own", botID, "my own words", nil),
			message("$notice", "@other-bot:example.org", "beep", map[string]any{"msgtype": "m.notice"}),
		},
		group: {message("$2", "@bob:example.org", "Silo, elsewhere", nil)},
	})
	waitFor(t, "a delivery", func() bool { return len(h.inbound()) > 0 })
	// The same event in a later sync is not a second message.
	f.syncs <- syncOf("s2", map[string][]map[string]any{direct: {message("$1", "@alice:example.org", "hello there", nil)}})
	waitFor(t, "the next sync", func() bool {
		return slices.ContainsFunc(f.made("GET /_matrix/client/v3/sync"), func(c call) bool { return c.Query.Get("since") == "s2" })
	})

	got := h.inbound()
	if len(got) != 1 {
		t.Fatalf("delivered %+v", got)
	}
	in := got[0]
	if in.ExternalID != direct || in.Text != "hello there" || in.Author != "Alice" || in.AuthorID != "@alice:example.org" || in.Title != "DM · Alice" || in.ReplyTo != "" {
		t.Fatalf("inbound %+v", in)
	}
	syncs := f.made("GET /_matrix/client/v3/sync")
	if first := syncs[0]; first.Query.Get("timeout") != "0" || first.Query.Get("since") != "" || first.Auth != "Bearer "+token {
		t.Fatalf("first sync %+v", first)
	}
	if next := syncs[1]; next.Query.Get("since") != "s0" || next.Query.Get("timeout") != "30000" {
		t.Fatalf("second sync %+v", next.Query)
	}
	if joins := f.made("POST /_matrix/client/v3/join/!invited:example.org"); len(joins) != 1 {
		t.Fatalf("the invitation was accepted %d times", len(joins))
	}
}

func TestStartRetriesAnOutageAndStopsOnARejectedToken(t *testing.T) {
	f := newFakeServer(t)
	ch := channel(direct)
	h := &fakeHost{current: ch}
	var mu sync.Mutex
	mode := "down"
	f.on("GET /_matrix/client/v3/sync", func(c call) (int, any) {
		mu.Lock()
		defer mu.Unlock()
		switch mode {
		case "down":
			return 502, []byte("<html>bad gateway</html>")
		case "revoked":
			return 401, matrixError("M_UNKNOWN_TOKEN", "Invalid access token passed.")
		}
		return 200, map[string]any{"next_batch": "s1"}
	})
	a := New()
	a.retryMin = 5 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- a.Start(context.Background(), ch, f.config(), h) }()

	waitFor(t, "the outage to show", func() bool {
		st := h.lastState()
		return st.Kind == channels.StateError && strings.Contains(st.Message, "trying again")
	})
	mu.Lock()
	mode = "up"
	mu.Unlock()
	waitFor(t, "the connection to come back", func() bool { return h.lastState().Kind == channels.StateInfo })
	mu.Lock()
	mode = "revoked"
	mu.Unlock()
	select {
	case err := <-done:
		if !channels.IsPermanent(err) || strings.Contains(err.Error(), token) {
			t.Fatalf("Start ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start went on with a revoked token")
	}
	if st := h.lastState(); st.Kind != channels.StateError || !strings.Contains(st.Message, "access token") {
		t.Fatalf("state %+v", st)
	}
}

func TestRejectedTokenIsPermanent(t *testing.T) {
	f := newFakeServer(t)
	f.on("GET /_matrix/client/v3/account/whoami", func(call) (int, any) {
		return 401, matrixError("M_UNKNOWN_TOKEN", "Invalid access token passed.")
	})
	ch := channel(direct)
	h := &fakeHost{current: ch}
	a := New()
	err := a.Start(context.Background(), ch, f.config(), h)
	if !channels.IsPermanent(err) || !strings.Contains(err.Error(), "access token") || strings.Contains(err.Error(), token) {
		t.Fatalf("Start: %v", err)
	}
	if st := h.lastState(); st.Kind != channels.StateError {
		t.Fatalf("state %+v", st)
	}
	st, err := a.Validate(context.Background(), ch, f.config())
	if err == nil || st.Kind != channels.StateError || strings.Contains(st.Message, token) {
		t.Fatalf("Validate: %+v, %v", st, err)
	}
	if len(f.made("GET /_matrix/client/v3/sync")) != 0 {
		t.Fatal("synced with a rejected token")
	}
}

func TestValidateSaysWhoIsConnected(t *testing.T) {
	f := newFakeServer(t)
	st, err := New().Validate(context.Background(), channel(""), f.config())
	if err != nil || st.Kind != channels.StateInfo || st.Message != "Connected as "+botID {
		t.Fatalf("%+v, %v", st, err)
	}
	if _, err := New().Validate(context.Background(), channel(""), channels.Config{Values: map[string]string{"access_token": token}}); err == nil {
		t.Fatal("no homeserver URL was accepted")
	}
}

func TestLoginFindsTheHomeserverBehindAServerName(t *testing.T) {
	f := newFakeServer(t)
	// A website that is not the homeserver, but says where it is.
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" && r.URL.Path == "/.well-known/matrix/client" {
			t.Error("the token was sent with the discovery request")
		}
		if r.URL.Path == "/.well-known/matrix/client" {
			_ = json.NewEncoder(w).Encode(map[string]any{"m.homeserver": map[string]any{"base_url": f.srv.URL + "/"}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(site.Close)
	c, err := login(context.Background(), channels.Config{Values: map[string]string{"homeserver_url": site.URL, "access_token": token}})
	if err != nil || c.self != botID || c.api.base != f.srv.URL {
		t.Fatalf("%+v, %v", c, err)
	}
}

func TestEncryptedRoomIsSaidNotSilent(t *testing.T) {
	f := newFakeServer(t)
	ch := channel(direct)
	h := &fakeHost{current: ch}
	start(t, New(), ch, f.config(), h)
	waitFor(t, "the connected state", func() bool { return h.lastState().Kind == channels.StateInfo })

	f.syncs <- syncOf("s1", map[string][]map[string]any{direct: {{
		"type": "m.room.encrypted", "event_id": "$e", "sender": "@alice:example.org",
		"content": map[string]any{"algorithm": "m.megolm.v1.aes-sha2", "ciphertext": "…"},
	}}})
	waitFor(t, "the encryption warning", func() bool {
		st := h.lastState()
		return st.Kind == channels.StateError && strings.Contains(st.Message, "This room is encrypted; Silo cannot read it")
	})
	if n := len(h.inbound()); n != 0 {
		t.Fatalf("an encrypted event was delivered: %+v", h.inbound())
	}
}

// --- inbound ---

func TestMentionGatingInAGroupAndNotInADM(t *testing.T) {
	f := newFakeServer(t)
	ch := channel(group)
	a, c := connected(t, f, ch)
	h := &fakeHost{current: ch}
	ctx := context.Background()
	cfg := f.config()
	hear := func(room string, cfg channels.Config, m map[string]any) int {
		t.Helper()
		before := len(h.inbound())
		if err := a.onMessage(ctx, ch, cfg, h, c, room, asEvent(t, m)); err != nil {
			t.Fatal(err)
		}
		return len(h.inbound()) - before
	}
	alice := "@alice:example.org"

	if n := hear(group, cfg, message("$1", alice, "lunch? I like silos", nil)); n != 0 {
		t.Fatal("an unaddressed message was delivered")
	}
	if n := hear(group, cfg, message("$2", alice, "what now", map[string]any{"m.mentions": map[string]any{"user_ids": []string{botID}}})); n != 1 {
		t.Fatal("an m.mentions mention was not delivered")
	}
	if n := hear(group, cfg, message("$3", alice, "hey silo, what now", nil)); n != 1 {
		t.Fatal("a mention by name was not delivered")
	}
	if n := hear(group, cfg, message("$4", alice, "ping", map[string]any{"formatted_body": `<a href="https://matrix.to/#/` + botID + `">bot</a> ping`})); n != 1 {
		t.Fatal("a mention by link was not delivered")
	}
	// The quoted original at the head of a reply names the bot; the reply does not.
	reply := message("$5", alice, "> <"+botID+"> earlier answer\n\nthanks all", map[string]any{
		"m.relates_to": map[string]any{"m.in_reply_to": map[string]any{"event_id": "$sent"}},
	})
	if n := hear(group, cfg, reply); n != 0 {
		t.Fatal("a reply's quoted original counted as a mention")
	}
	if n := hear(group, cfg, message("$6", alice, "edited", map[string]any{
		"m.mentions":   map[string]any{"user_ids": []string{botID}},
		"m.relates_to": map[string]any{"rel_type": "m.replace", "event_id": "$2"},
	})); n != 0 {
		t.Fatal("an edit was delivered as a new message")
	}

	// A thread: silent until the bot is named there, then part of it.
	inThread := func(id, body string) map[string]any {
		return message(id, alice, body, map[string]any{"m.relates_to": map[string]any{"rel_type": "m.thread", "event_id": "$root"}})
	}
	if n := hear(group, cfg, inThread("$7", "in a thread")); n != 0 {
		t.Fatal("a thread the bot is not in was delivered")
	}
	if n := hear(group, cfg, inThread("$8", "Silo: join us")); n != 1 {
		t.Fatal("a mention in a thread was not delivered")
	}
	if n := hear(group, cfg, inThread("$9", "and tomorrow?")); n != 1 {
		t.Fatal("a follow-up in the bot's thread needed a mention")
	}
	if last := h.inbound()[len(h.inbound())-1]; last.ReplyTo != "$root" || last.Author != "Alice" {
		t.Fatalf("thread inbound %+v", last)
	}

	if n := hear(group, f.config("reply_mode", "always"), message("$10", alice, "anything", nil)); n != 1 {
		t.Fatal("reply_mode always still needed a mention")
	}
	// A room of two is a direct chat: every message is to the bot.
	if n := hear(direct, cfg, message("$11", alice, "no mention here", nil)); n != 1 {
		t.Fatal("a direct chat needed a mention")
	}
}

func TestInboundFileIsFetchedThroughAuthenticatedMedia(t *testing.T) {
	f := newFakeServer(t)
	f.on("GET /_matrix/client/v1/media/download/example.org/abc", func(call) (int, any) { return 200, []byte("picture bytes") })
	// A homeserver from before authenticated media knows only the old path.
	f.on("GET /_matrix/media/v3/download/old.org/memo", func(call) (int, any) { return 200, []byte("voice bytes") })
	ch := channel(direct)
	a, c := connected(t, f, ch)
	h := &fakeHost{current: ch}
	ctx := context.Background()

	image := message("$1", "@alice:example.org", "look at this", map[string]any{
		"msgtype": "m.image", "filename": "cat.png", "url": "mxc://example.org/abc",
		"info": map[string]any{"mimetype": "image/png", "size": 13},
	})
	voice := message("$2", "@alice:example.org", "Voice message.ogg", map[string]any{
		"msgtype": "m.audio", "url": "mxc://old.org/memo", "info": map[string]any{"mimetype": "audio/ogg"},
		"org.matrix.msc3245.voice": map[string]any{},
	})
	for _, m := range []map[string]any{image, voice} {
		if err := a.onMessage(ctx, ch, f.config(), h, c, direct, asEvent(t, m)); err != nil {
			t.Fatal(err)
		}
	}
	got := h.inbound()
	if len(got) != 2 || len(got[0].Files) != 1 || len(got[1].Files) != 1 {
		t.Fatalf("delivered %+v", got)
	}
	pic := got[0].Files[0]
	if got[0].Text != "look at this" || pic.Name != "cat.png" || pic.Mime != "image/png" || pic.Size != 13 || pic.Voice {
		t.Fatalf("image %+v, caption %q", pic, got[0].Text)
	}
	data, err := pic.Fetch(ctx)
	if err != nil || string(data) != "picture bytes" {
		t.Fatalf("fetch: %q, %v", data, err)
	}
	if dl := f.made("GET /_matrix/client/v1/media/download/example.org/abc"); len(dl) != 1 || dl[0].Auth != "Bearer "+token {
		t.Fatalf("download calls %+v", dl)
	}
	memo := got[1].Files[0]
	if got[1].Text != "" || memo.Name != "Voice message.ogg" || !memo.Voice {
		t.Fatalf("voice %+v, text %q", memo, got[1].Text)
	}
	data, err = memo.Fetch(ctx)
	if err != nil || string(data) != "voice bytes" {
		t.Fatalf("legacy fetch: %q, %v", data, err)
	}
}

// --- outbound ---

func TestSendFormatsThreadsAndUploads(t *testing.T) {
	f := newFakeServer(t)
	ch := channel(group)
	a, _ := connected(t, f, ch)
	ctx := context.Background()

	err := a.Send(ctx, ch, f.config(), channels.Outbound{Text: "**Done.** See `x` and @room"})
	if err != nil {
		t.Fatal(err)
	}
	sends := f.made("PUT /_matrix/client/v3/rooms/" + group + "/send/m.room.message/")
	if len(sends) != 1 {
		t.Fatalf("sends %+v", sends)
	}
	body := sends[0].json(t)
	if body["msgtype"] != "m.text" || body["body"] != "**Done.** See `x` and @room" || body["format"] != "org.matrix.custom.html" {
		t.Fatalf("text event %+v", body)
	}
	if html := body["formatted_body"].(string); html != "<p><strong>Done.</strong> See <code>x</code> and @room</p>" {
		t.Fatalf("formatted_body %q", html)
	}
	// Present and empty, so a written @room pages nobody.
	if m, ok := body["m.mentions"].(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("m.mentions %+v", body["m.mentions"])
	}
	if _, has := body["m.relates_to"]; has || sends[0].Auth != "Bearer "+token {
		t.Fatalf("event %+v", body)
	}

	// A thread reply with a file: both go in the thread, the file after an upload.
	err = a.Send(ctx, ch, f.config(), channels.Outbound{
		Text: "here it is", ReplyTo: "$root",
		Files: []channels.Attachment{{Name: "chart.png", Mime: "image/png", Data: []byte("png bytes")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sends = f.made("PUT /_matrix/client/v3/rooms/" + group + "/send/m.room.message/")
	if len(sends) != 3 || sends[1].Path == sends[2].Path {
		t.Fatalf("sends %+v", sends)
	}
	text, file := sends[1].json(t), sends[2].json(t)
	rel := text["m.relates_to"].(map[string]any)
	if rel["rel_type"] != "m.thread" || rel["event_id"] != "$root" || rel["is_falling_back"] != true ||
		rel["m.in_reply_to"].(map[string]any)["event_id"] != "$root" {
		t.Fatalf("thread relation %+v", rel)
	}
	ups := f.made("POST /_matrix/media/v3/upload")
	if len(ups) != 1 || string(ups[0].Body) != "png bytes" || ups[0].Type != "image/png" || ups[0].Query.Get("filename") != "chart.png" {
		t.Fatalf("uploads %+v", ups)
	}
	if file["msgtype"] != "m.image" || file["url"] != "mxc://example.org/up1" || file["body"] != "chart.png" ||
		file["info"].(map[string]any)["mimetype"] != "image/png" {
		t.Fatalf("file event %+v", file)
	}
	// The file follows the text in the thread, as a reply to it.
	frel := file["m.relates_to"].(map[string]any)
	if frel["event_id"] != "$root" || !strings.HasPrefix(frel["m.in_reply_to"].(map[string]any)["event_id"].(string), "$sent") {
		t.Fatalf("file relation %+v", frel)
	}
	// A thread the bot wrote in is one it takes part in.
	if !a.joined.Has(threadKey(ch.ID, group, "$root")) {
		t.Fatal("the bot's own thread is not remembered")
	}
}

func TestSendChunksLongTextAndWaitsOutARateLimit(t *testing.T) {
	f := newFakeServer(t)
	ch := channel(group)
	a, _ := connected(t, f, ch)
	var mu sync.Mutex
	tries := 0
	f.on("PUT /_matrix/client/v3/rooms/", func(c call) (int, any) {
		mu.Lock()
		defer mu.Unlock()
		if tries++; tries == 1 {
			return 429, map[string]any{"errcode": "M_LIMIT_EXCEEDED", "error": "Too Many Requests", "retry_after_ms": 20}
		}
		return 200, map[string]any{"event_id": "$ok"}
	})
	long := strings.Repeat("word ", 1500) // 7500 runes: two messages
	if err := a.Send(context.Background(), ch, f.config(), channels.Outbound{Text: long}); err != nil {
		t.Fatal(err)
	}
	sends := f.made("PUT /_matrix/client/v3/rooms/")
	if len(sends) != 3 {
		t.Fatalf("%d requests, want a refused one, its retry and the second part", len(sends))
	}
	if sends[0].Path != sends[1].Path {
		t.Fatal("the retry is not the same transaction")
	}
}

func TestSendRefusesAnEncryptedRoom(t *testing.T) {
	f := newFakeServer(t)
	ch := channel(secret)
	a, _ := connected(t, f, ch)
	err := a.Send(context.Background(), ch, f.config(), channels.Outbound{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("Send: %v", err)
	}
	if n := len(f.made("PUT ")); n != 0 {
		t.Fatalf("%d events were sent into an encrypted room", n)
	}
}

func TestTypingShowsAndClears(t *testing.T) {
	f := newFakeServer(t)
	ch := channel(direct)
	a, _ := connected(t, f, ch)
	for _, on := range []bool{true, false} {
		if err := a.Typing(context.Background(), ch, direct, on); err != nil {
			t.Fatal(err)
		}
	}
	calls := f.made("PUT /_matrix/client/v3/rooms/" + direct + "/typing/" + botID)
	if len(calls) != 2 || calls[0].json(t)["typing"] != true || calls[1].json(t)["typing"] != false {
		t.Fatalf("typing calls %+v", calls)
	}
}

// --- history, picker, target ---

func TestHistoryIsOldestFirst(t *testing.T) {
	f := newFakeServer(t)
	f.on("GET /_matrix/client/v3/rooms/"+direct+"/messages", func(c call) (int, any) {
		if c.Query.Get("dir") != "b" || c.Query.Get("limit") != "10" {
			t.Errorf("history query %v", c.Query)
		}
		return 200, map[string]any{"chunk": []map[string]any{
			message("$3", botID, "the answer", nil),
			message("$2", "@alice:example.org", "notes.pdf", map[string]any{"msgtype": "m.file", "url": "mxc://example.org/n", "info": map[string]any{"mimetype": "application/pdf"}}),
			message("$1", "@alice:example.org", "a question", nil),
		}}
	})
	ch := channel(direct)
	a, _ := connected(t, f, ch)
	got, err := a.History(context.Background(), ch, f.config(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("history %+v", got)
	}
	if got[0].Text != "a question" || got[0].Author != "Alice" || got[0].Out || got[0].At.IsZero() {
		t.Fatalf("first %+v", got[0])
	}
	if got[1].Text != "[attachment: notes.pdf (application/pdf)]" {
		t.Fatalf("file %+v", got[1])
	}
	if got[2].Text != "the answer" || !got[2].Out {
		t.Fatalf("last %+v", got[2])
	}
}

func TestPickerNamesRoomsAndMarksEncryptedOnes(t *testing.T) {
	f := newFakeServer(t)
	state := func(kind string, content map[string]any) map[string]any {
		return map[string]any{"type": kind, "state_key": "", "event_id": "$s" + kind, "sender": "@alice:example.org", "content": content}
	}
	f.initial = map[string]any{"next_batch": "s0", "rooms": map[string]any{"join": map[string]any{
		group:   map[string]any{"state": map[string]any{"events": []any{state("m.room.name", map[string]any{"name": "Team"})}}},
		lobbyID: map[string]any{"state": map[string]any{"events": []any{state("m.room.canonical_alias", map[string]any{"alias": lobbyAka})}}},
		direct:  map[string]any{},
		secret: map[string]any{"state": map[string]any{"events": []any{
			state("m.room.name", map[string]any{"name": "Vault"}),
			state("m.room.encryption", map[string]any{"algorithm": "m.megolm.v1.aes-sha2"}),
		}}},
	}}}
	ch := channel("")
	a := New()
	st, err := a.Action(context.Background(), ch, f.config(), "list_rooms", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []channels.Option{
		{Value: lobbyID, Label: lobbyAka},
		{Value: direct, Label: "DM · Alice"},
		{Value: group, Label: "Team"},
		{Value: secret, Label: "Vault (encrypted: Silo cannot read it)"},
	}
	if st.Kind != channels.StateSelect || !slices.Equal(st.Options, want) {
		t.Fatalf("picker %+v", st)
	}

	// Check connection on a channel bound to an encrypted room says so.
	st, err = a.Action(context.Background(), channel(secret), f.config(), "refresh", nil)
	if err != nil || st.Kind != channels.StateError || !strings.Contains(st.Message, "encrypted") {
		t.Fatalf("refresh %+v, %v", st, err)
	}
	st, err = a.Action(context.Background(), channel(direct), f.config(), "refresh", nil)
	if err != nil || st.Kind != channels.StateInfo || st.Message != "Connected as "+botID {
		t.Fatalf("refresh %+v, %v", st, err)
	}
}

func TestTargetReadsWhatTheOwnerTypes(t *testing.T) {
	f := newFakeServer(t)
	_, c := connected(t, f, channel(""))
	ctx := context.Background()
	cases := map[string]string{
		direct:                            direct,
		" " + group + " ":                 group,
		lobbyAka:                          lobbyID,
		"https://matrix.to/#/" + lobbyAka: lobbyID,
		"https://matrix.to/#/%23lobby:example.org?via=example.org": lobbyID,
		"https://matrix.to/#/" + group + "/$event?via=example.org": group,
	}
	for in, want := range cases {
		if got, err := c.target(ctx, in); err != nil || got != want {
			t.Errorf("target(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "lobby", "@alice:example.org", "#nowhere:example.org"} {
		if got, err := c.target(ctx, in); err == nil {
			t.Errorf("target(%q) = %q, want an error", in, got)
		}
	}
	// An alias is asked for once.
	if n := len(f.made("GET /_matrix/client/v3/directory/room/" + lobbyAka)); n != 1 {
		t.Errorf("the alias was resolved %d times", n)
	}
	// A channel bound to an alias sends to the room it names.
	a := New()
	if err := a.Send(ctx, channel(lobbyAka), f.config(), channels.Outbound{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.made("PUT /_matrix/client/v3/rooms/" + lobbyID + "/send/")); n != 1 {
		t.Fatalf("%d sends to the aliased room", n)
	}
}

// --- words ---

func TestNamedMatchesWholeWords(t *testing.T) {
	cases := []struct {
		text, name string
		want       bool
	}{
		{"hey Silo, hi", "silo", true},
		{"SILO", "silo", true},
		{"silo: do it", "Silo", true},
		{"the silos are full", "silo", false},
		{"isilo", "silo", false},
		{"silos and silo", "silo", true},
		{"anything", "", false},
		{"ask Silo Bot now", "silo bot", true},
	}
	for _, c := range cases {
		if got := named(c.text, c.name); got != c.want {
			t.Errorf("named(%q, %q) = %v", c.text, c.name, got)
		}
	}
}

func TestToHTMLRendersWhatModelsWrite(t *testing.T) {
	cases := map[string]string{
		"plain":                        "<p>plain</p>",
		"one\ntwo":                     "<p>one<br>\ntwo</p>",
		"~~gone~~ [a](https://x.test)": `<p><del>gone</del> <a href="https://x.test">a</a></p>`,
		"```go\nx := 1\n```":           "<pre><code class=\"language-go\">x := 1\n</code></pre>",
		"<script>alert(1)</script> hi": "<p>&lt;script&gt;alert(1)&lt;/script&gt; hi</p>",
		"use <name> here":              "<p>use &lt;name&gt; here</p>",
	}
	for in, want := range cases {
		if got := toHTML(in); got != want {
			t.Errorf("toHTML(%q) = %q, want %q", in, got, want)
		}
	}
	if got := toHTML("| a | b |\n|---|---|\n| 1 | 2 |"); !strings.Contains(got, "<table>") || !strings.Contains(got, "<td>1</td>") {
		t.Errorf("table: %q", got)
	}
}

func TestHomeserverReadsWhatTheOwnerTypes(t *testing.T) {
	cases := map[string]string{
		"https://matrix.example.org":                 "https://matrix.example.org",
		" matrix.example.org/ ":                      "https://matrix.example.org",
		"http://10.0.0.5:8008":                       "http://10.0.0.5:8008",
		"https://example.org/_matrix/client/v3/sync": "https://example.org",
		"https://example.org/synapse/":               "https://example.org/synapse",
		"":                                           "",
		"ftp://example.org":                          "",
		"https://user:pass@example.org":              "",
	}
	for in, want := range cases {
		got, err := homeserver(in)
		if got != want || (want == "") != (err != nil) {
			t.Errorf("homeserver(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
