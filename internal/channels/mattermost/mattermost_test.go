package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// --- fakes ---

// id makes a Mattermost-shaped id (26 lower-case characters) out of a word.
func id(word string) string { return word + strings.Repeat("0", 26-len(word)) }

var (
	botID   = id("bot")
	aliceID = id("alice")
	town    = id("town")
	other   = id("other")
	direct  = id("direct")
)

const token = "s3cret-token"

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

func (h *fakeHost) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, st := range h.states {
		out = append(out, st.Message)
	}
	return out
}

// call is one REST request the adapter made.
type call struct {
	Route string // "GET /users/me"
	Query string
	Auth  string
	Type  string
	Body  []byte
}

// fakeServer answers Mattermost's REST API from per-route handlers, records
// every call, and serves the websocket.
type fakeServer struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	calls  []call
	handle map[string]func(c call) (int, any)
	dials  int
	// sock runs once the bot is signed in on a websocket; n counts dials.
	sock func(ctx context.Context, ws *websocket.Conn, n int)
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{t: t, handle: map[string]func(call) (int, any){}}
	f.handle["GET /users/me"] = func(call) (int, any) {
		return 200, map[string]any{"id": botID, "username": "silo"}
	}
	f.handle["GET /users/"+aliceID] = func(call) (int, any) {
		return 200, map[string]any{"id": aliceID, "username": "alice", "first_name": "Alice", "last_name": "Liddell"}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/websocket", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.CloseNow() }()
		f.mu.Lock()
		f.dials++
		n := f.dials
		sock := f.sock
		f.mu.Unlock()
		// As the real server: a token it does not know is hung up on, a good
		// one is acknowledged and greeted.
		var req struct {
			Seq    int    `json:"seq"`
			Action string `json:"action"`
			Data   struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		_, data, err := ws.Read(r.Context())
		if err != nil || json.Unmarshal(data, &req) != nil || req.Action != "authentication_challenge" || req.Data.Token != token {
			return
		}
		writeJSON(r.Context(), ws, map[string]any{"status": "OK", "seq_reply": req.Seq})
		writeJSON(r.Context(), ws, map[string]any{"event": "hello", "data": map[string]any{"server_version": "10.0.0"}})
		if sock != nil {
			sock(r.Context(), ws, n)
		}
	})
	mux.HandleFunc("/api/v4/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := call{
			Route: r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v4"),
			Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), Type: r.Header.Get("Content-Type"), Body: body,
		}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		h := f.handle[c.Route]
		f.mu.Unlock()
		status, out := 404, any(map[string]any{"message": "no such route in the fake: " + c.Route})
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
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func writeJSON(ctx context.Context, ws *websocket.Conn, v any) {
	data, _ := json.Marshal(v)
	_ = ws.Write(ctx, websocket.MessageText, data)
}

func (f *fakeServer) cfg(values ...string) channels.Config {
	cfg := channels.Config{Values: map[string]string{"server_url": f.srv.URL, "bot_token": token}}
	for i := 0; i+1 < len(values); i += 2 {
		cfg.Values[values[i]] = values[i+1]
	}
	return cfg
}

func (f *fakeServer) made(route string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Route == route {
			out = append(out, c)
		}
	}
	return out
}

// posts decodes every post the adapter made.
func (f *fakeServer) posts() []newPost {
	var out []newPost
	for _, c := range f.made("POST /posts") {
		var p newPost
		if err := json.Unmarshal(c.Body, &p); err != nil {
			f.t.Fatalf("post body: %v", err)
		}
		out = append(out, p)
	}
	return out
}

func newChannel() *db.Channel {
	return &db.Channel{ID: "ch1", BotID: "bot1", Adapter: "mattermost", ExternalID: town}
}

// event is a "posted" websocket event as the server words it: the post and
// the mentions are JSON inside strings.
func event(p post, kind string, mentions ...string) map[string]any {
	raw, _ := json.Marshal(p)
	data := map[string]any{"channel_type": kind, "channel_display_name": "Town Square", "post": string(raw)}
	if len(mentions) > 0 {
		m, _ := json.Marshal(mentions)
		data["mentions"] = string(m)
	}
	return map[string]any{"event": "posted", "data": data, "seq": 2}
}

// rig is an adapter signed in to the fake, for tests that hand it posts
// without a websocket.
type rig struct {
	a    *Adapter
	f    *fakeServer
	host *fakeHost
	ch   *db.Channel
	c    *conn
}

func newRig(t *testing.T) *rig {
	f := newFakeServer(t)
	cl, err := newAPI(f.cfg())
	if err != nil {
		t.Fatal(err)
	}
	c, err := login(context.Background(), cl)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{a: New(), f: f, host: &fakeHost{}, ch: newChannel(), c: c}
	r.a.putConn(r.ch.ID, c)
	return r
}

func (r *rig) post(t *testing.T, cfg channels.Config, p post, kind string, mentions ...string) {
	t.Helper()
	if p.UserID == "" {
		p.UserID = aliceID
	}
	if p.ChannelID == "" {
		p.ChannelID = town
	}
	if err := r.a.onPost(context.Background(), r.ch, cfg, r.host, r.c, posted{post: p, kind: kind, title: "Town Square", mentions: mentions}); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- start ---

func TestStartSignsInOnTheSocketAndDeliversTheBoundChannelOnly(t *testing.T) {
	f := newFakeServer(t)
	f.sock = func(ctx context.Context, ws *websocket.Conn, _ int) {
		for _, ev := range []map[string]any{
			{"event": "status_change", "data": map[string]any{"status": "online", "user_id": aliceID}},
			event(post{ID: id("p1"), UserID: aliceID, ChannelID: other, Message: "@silo elsewhere"}, "O", botID),
			event(post{ID: id("p2"), UserID: botID, ChannelID: town, Message: "my own words"}, "O"),
			event(post{ID: id("p3"), UserID: id("otherbot"), ChannelID: town, Message: "@silo beep", Props: map[string]any{"from_bot": "true"}}, "O", botID),
			event(post{ID: id("p4"), UserID: id("hook"), ChannelID: town, Message: "@silo deploy done", Props: map[string]any{"from_webhook": "true"}}, "O", botID),
			event(post{ID: id("p5"), UserID: aliceID, ChannelID: town, Message: "alice joined the channel.", Type: "system_join_channel"}, "O"),
			event(post{ID: id("p6"), UserID: aliceID, ChannelID: town, Message: "@silo, what is up?"}, "O", botID),
		} {
			writeJSON(ctx, ws, ev)
		}
		<-ctx.Done()
	}
	a, host, ch := New(), &fakeHost{}, newChannel()
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan error, 1)
	go func() { ended <- a.Start(ctx, ch, f.cfg(), host) }()

	waitFor(t, "the post", func() bool { return len(host.inbound()) == 1 })
	time.Sleep(50 * time.Millisecond) // anything wrongly let through would have followed
	got := host.inbound()
	if len(got) != 1 {
		t.Fatalf("delivered %d messages, want only the bound channel's one from a person: %+v", len(got), got)
	}
	in := got[0]
	if in.ExternalID != town || in.Text != "what is up?" || in.Author != "Alice Liddell" || in.AuthorID != aliceID {
		t.Fatalf("inbound = %+v", in)
	}
	if in.ReplyTo != id("p6") {
		t.Fatalf("ReplyTo = %q, want a thread under the post", in.ReplyTo)
	}
	if in.Title != "Town Square" {
		t.Fatalf("Title = %q", in.Title)
	}
	if !slices.Contains(host.messages(), "Connected as @silo") {
		t.Fatalf("states = %q, want Connected as @silo", host.messages())
	}
	if a.conn(ch.ID) == nil {
		t.Fatal("no live session while running")
	}
	cancel()
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
	if a.conn(ch.ID) != nil {
		t.Fatal("session kept after Start returned")
	}
}

func TestStartReconnectsADroppedSocket(t *testing.T) {
	f := newFakeServer(t)
	f.sock = func(ctx context.Context, ws *websocket.Conn, n int) {
		if n == 1 {
			return // hang up right after signing in
		}
		writeJSON(ctx, ws, event(post{ID: id("p1"), UserID: aliceID, ChannelID: town, Message: "back again"}, "D"))
		<-ctx.Done()
	}
	a, host := New(), &fakeHost{}
	a.retry = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- a.Start(ctx, newChannel(), f.cfg(), host) }()
	waitFor(t, "the post after the reconnect", func() bool { return len(host.inbound()) == 1 })
	select {
	case err := <-ended:
		t.Fatalf("a dropped socket ended Start: %v", err)
	default:
	}
	if !slices.ContainsFunc(host.messages(), func(m string) bool { return strings.HasPrefix(m, "Lost the connection") }) {
		t.Fatalf("states = %q, want the lost connection said", host.messages())
	}
}

func TestRejectedTokenIsPermanent(t *testing.T) {
	f := newFakeServer(t)
	f.handle["GET /users/me"] = func(call) (int, any) {
		return 401, map[string]any{"id": "api.context.session_expired.app_error", "message": "Invalid or expired session, please login again.", "status_code": 401}
	}
	host := &fakeHost{}
	err := New().Start(context.Background(), newChannel(), f.cfg(), host)
	if !channels.IsPermanent(err) {
		t.Fatalf("Start = %v, want a permanent error", err)
	}
	if !strings.Contains(err.Error(), "rejected the access token") || strings.Contains(err.Error(), token) {
		t.Fatalf("error = %q", err)
	}
	if st := host.states[len(host.states)-1]; st.Kind != channels.StateError {
		t.Fatalf("state = %+v", st)
	}
	st, err := New().Validate(context.Background(), newChannel(), f.cfg())
	if !channels.IsPermanent(err) || st.Kind != channels.StateError {
		t.Fatalf("Validate = %+v, %v", st, err)
	}
}

// A token revoked while the bot runs: the server hangs up on the socket's
// sign-in without a word, and REST is what says why.
func TestTokenRevokedWhileRunningStopsForGood(t *testing.T) {
	f := newFakeServer(t)
	f.sock = func(context.Context, *websocket.Conn, int) {
		f.mu.Lock()
		f.handle["GET /users/me"] = func(call) (int, any) { return 401, map[string]any{"message": "expired"} }
		f.mu.Unlock()
	}
	a := New()
	a.retry = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Start(ctx, newChannel(), f.cfg(), &fakeHost{}); !channels.IsPermanent(err) {
		t.Fatalf("Start = %v, want a permanent error", err)
	}
}

func TestValidateSaysWhoTheBotIs(t *testing.T) {
	f := newFakeServer(t)
	st, err := New().Validate(context.Background(), newChannel(), f.cfg("server_url", f.srv.URL+"/api/v4/"))
	if err != nil || st.Kind != channels.StateInfo || st.Message != "Connected as @silo" {
		t.Fatalf("Validate = %+v, %v", st, err)
	}
	if got := f.made("GET /users/me"); len(got) != 1 || got[0].Auth != "Bearer "+token {
		t.Fatalf("calls = %+v", got)
	}
	if _, err := New().Validate(context.Background(), newChannel(), channels.Config{Values: map[string]string{"bot_token": token}}); err == nil {
		t.Fatal("no server address was accepted")
	}
}

// --- inbound ---

func TestMentionGatingInAChannelAndADM(t *testing.T) {
	r := newRig(t)
	cfg := r.f.cfg()
	r.post(t, cfg, post{ID: id("a"), Message: "just chatting"}, "O")
	r.post(t, cfg, post{ID: id("b"), Message: "ask @silosaurus instead"}, "P")
	if got := r.host.inbound(); len(got) != 0 {
		t.Fatalf("unmentioned channel posts were delivered: %+v", got)
	}
	r.post(t, cfg, post{ID: id("c"), Message: "hey bot, look"}, "O", aliceID, botID) // named in the event
	r.post(t, cfg, post{ID: id("d"), Message: "what does @Silo. think"}, "P")        // named in the text
	r.post(t, cfg, post{ID: id("e"), Message: "no mention needed here"}, "D")
	r.post(t, cfg, post{ID: id("f"), Message: "nor here"}, "G")
	got := r.host.inbound()
	if len(got) != 4 {
		t.Fatalf("delivered %d, want 4: %+v", len(got), got)
	}
	if got[0].ReplyTo != id("c") || got[1].ReplyTo != id("d") {
		t.Fatalf("channel answers go in a thread under the post: %q %q", got[0].ReplyTo, got[1].ReplyTo)
	}
	if got[2].ReplyTo != "" || got[3].ReplyTo != "" {
		t.Fatalf("a DM is answered in the DM itself: %q %q", got[2].ReplyTo, got[3].ReplyTo)
	}
	if got[2].Title != "DM · Alice Liddell" || got[3].Title != "Group message · Town Square" {
		t.Fatalf("titles = %q, %q", got[2].Title, got[3].Title)
	}

	always := newRig(t)
	always.post(t, always.f.cfg("reply_mode", "always", "reply_in", "channel"), post{ID: id("g"), Message: "just chatting"}, "O")
	if got := always.host.inbound(); len(got) != 1 || got[0].ReplyTo != "" {
		t.Fatalf("reply_mode always, reply_in channel: %+v", got)
	}
}

func TestThreadTheBotAnsweredInNeedsNoMention(t *testing.T) {
	r := newRig(t)
	cfg := r.f.cfg()
	r.post(t, cfg, post{ID: id("x"), RootID: id("strange"), Message: "a thread it is not in"}, "O")
	r.post(t, cfg, post{ID: id("root"), Message: "@silo start"}, "O", botID)
	r.post(t, cfg, post{ID: id("follow"), RootID: id("root"), Message: "and another thing"}, "O")
	r.post(t, cfg, post{ID: id("follow"), RootID: id("root"), Message: "and another thing"}, "O") // delivered twice
	got := r.host.inbound()
	if len(got) != 2 {
		t.Fatalf("delivered %d, want the mention and the follow-up: %+v", len(got), got)
	}
	if got[1].Text != "and another thing" || got[1].ReplyTo != id("root") {
		t.Fatalf("follow-up = %+v", got[1])
	}
}

func TestTargetPickedAfterStartTakesEffect(t *testing.T) {
	r := newRig(t)
	r.ch.ExternalID = ""
	r.post(t, r.f.cfg(), post{ID: id("a"), Message: "hello"}, "D")
	if len(r.host.inbound()) != 0 {
		t.Fatal("delivered with no target")
	}
	r.host.current = newChannel()
	r.post(t, r.f.cfg(), post{ID: id("b"), Message: "hello"}, "D")
	if len(r.host.inbound()) != 1 {
		t.Fatal("the picked target was not read from the live row")
	}
}

func TestInboundFilesAreFetchedWhenAsked(t *testing.T) {
	r := newRig(t)
	r.f.handle["GET /files/"+id("f2")+"/info"] = func(call) (int, any) {
		return 200, map[string]any{"id": id("f2"), "name": "notes.txt", "mime_type": "text/plain", "size": 11}
	}
	r.f.handle["GET /files/"+id("f2")] = func(call) (int, any) { return 200, []byte("hello notes") }
	p := post{ID: id("a"), FileIDs: []string{id("f1"), id("f2")}}
	p.Metadata.Files = []fileInfo{{ID: id("f1"), Name: "cat.png", MimeType: "image/png", Size: 1234}}
	r.post(t, r.f.cfg(), p, "D")
	got := r.host.inbound()
	if len(got) != 1 || len(got[0].Files) != 2 {
		t.Fatalf("inbound = %+v", got)
	}
	cat, notes := got[0].Files[0], got[0].Files[1]
	if cat.Name != "cat.png" || cat.Mime != "image/png" || cat.Size != 1234 {
		t.Fatalf("file from the event = %+v", cat)
	}
	if notes.Name != "notes.txt" || notes.Mime != "text/plain" || notes.Size != 11 {
		t.Fatalf("file asked about = %+v", notes)
	}
	if len(r.f.made("GET /files/"+id("f2"))) != 0 {
		t.Fatal("the file was downloaded before anyone asked")
	}
	data, err := notes.Fetch(context.Background())
	if err != nil || string(data) != "hello notes" {
		t.Fatalf("Fetch = %q, %v", data, err)
	}
	if got := r.f.made("GET /files/" + id("f2")); len(got) != 1 || got[0].Auth != "Bearer "+token {
		t.Fatalf("download calls = %+v", got)
	}
	if _, err := cat.Fetch(context.Background()); err == nil {
		t.Fatal("a file the server does not have fetched without error")
	}
}

func TestMentionsAreReadAsNames(t *testing.T) {
	for text, want := range map[string]int{
		"@silo hi": 0, "hi @SILO": 3, "hi @silo.": 3, "hi @silo-bot": -1, "hi @silo.bot": -1,
		"@silos and @silo": 11, "mail silo@example.com": -1, "": -1,
	} {
		if got := atName(text, "silo"); got != want {
			t.Errorf("atName(%q) = %d, want %d", text, got, want)
		}
	}
	for text, want := range map[string]string{
		"@silo: do it": "do it", "  @Silo do it": "do it", "do it @silo": "do it @silo", "@silo-bot do it": "@silo-bot do it",
	} {
		if got := withoutLeadingMention(text, "silo"); got != want {
			t.Errorf("withoutLeadingMention(%q) = %q, want %q", text, got, want)
		}
	}
}

// --- outbound ---

func TestSendAnswersUnderTheRootPost(t *testing.T) {
	r := newRig(t)
	r.f.handle["POST /posts"] = func(call) (int, any) { return 201, map[string]any{"id": id("new")} }
	err := r.a.Send(context.Background(), r.ch, r.f.cfg(), channels.Outbound{Text: "**done**, see `x`", ReplyTo: id("root")})
	if err != nil {
		t.Fatal(err)
	}
	got := r.f.posts()
	if len(got) != 1 || got[0].ChannelID != town || got[0].RootID != id("root") || got[0].Message != "**done**, see `x`" {
		t.Fatalf("posts = %+v", got)
	}
	if c := r.f.made("POST /posts")[0]; c.Auth != "Bearer "+token || !strings.HasPrefix(c.Type, "application/json") {
		t.Fatalf("call = %+v", c)
	}
	// The channel tool names no thread: the post goes to the channel itself.
	if err := r.a.Send(context.Background(), r.ch, r.f.cfg(), channels.Outbound{Text: "top level"}); err != nil {
		t.Fatal(err)
	}
	if got := r.f.posts(); got[1].RootID != "" {
		t.Fatalf("root_id = %q, want none", got[1].RootID)
	}
}

func TestSendCutsALongTextIntoPosts(t *testing.T) {
	r := newRig(t)
	r.f.handle["POST /posts"] = func(call) (int, any) { return 201, map[string]any{} }
	long := strings.Repeat("word ", 5000) // 25000 runes
	if err := r.a.Send(context.Background(), r.ch, r.f.cfg(), channels.Outbound{Text: long}); err != nil {
		t.Fatal(err)
	}
	got := r.f.posts()
	if len(got) != 2 {
		t.Fatalf("%d posts, want 2", len(got))
	}
	for _, p := range got {
		if n := len([]rune(p.Message)); n > maxMessageRunes {
			t.Fatalf("a post of %d runes", n)
		}
	}
}

func TestSendUploadsFilesFiveToAPost(t *testing.T) {
	r := newRig(t)
	var uploaded []string
	r.f.handle["POST /files"] = func(c call) (int, any) {
		_, params, err := mime.ParseMediaType(c.Type)
		if err != nil {
			t.Errorf("upload content type %q: %v", c.Type, err)
			return 400, map[string]any{}
		}
		form, err := multipart.NewReader(strings.NewReader(string(c.Body)), params["boundary"]).ReadForm(1 << 20)
		if err != nil {
			t.Errorf("upload form: %v", err)
			return 400, map[string]any{}
		}
		if got := form.Value["channel_id"]; len(got) != 1 || got[0] != town {
			t.Errorf("upload channel_id = %v", got)
		}
		fh := form.File["files"]
		if len(fh) != 1 {
			t.Errorf("upload carries %d files", len(fh))
			return 400, map[string]any{}
		}
		file, _ := fh[0].Open()
		data, _ := io.ReadAll(file)
		uploaded = append(uploaded, fh[0].Filename+"="+string(data))
		return 201, map[string]any{"file_infos": []map[string]any{{"id": id("up" + string(rune('a'+len(uploaded)-1)))}}}
	}
	r.f.handle["POST /posts"] = func(call) (int, any) { return 201, map[string]any{} }
	msg := channels.Outbound{Text: "here you go", ReplyTo: id("root")}
	for _, n := range []string{"1.txt", "2.txt", "3.txt", "4.txt", "5.txt", "6.txt"} {
		msg.Files = append(msg.Files, channels.Attachment{Name: n, Mime: "text/plain", Data: []byte("data " + n)})
	}
	msg.Files = append(msg.Files, channels.Attachment{Name: "empty.txt"})
	if err := r.a.Send(context.Background(), r.ch, r.f.cfg(), msg); err != nil {
		t.Fatal(err)
	}
	if len(uploaded) != 6 || uploaded[0] != "1.txt=data 1.txt" || uploaded[5] != "6.txt=data 6.txt" {
		t.Fatalf("uploaded = %q", uploaded)
	}
	got := r.f.posts()
	if len(got) != 3 {
		t.Fatalf("%d posts, want the text and two of files: %+v", len(got), got)
	}
	if got[0].Message != "here you go" || len(got[0].FileIDs) != 0 {
		t.Fatalf("text post = %+v", got[0])
	}
	if len(got[1].FileIDs) != 5 || len(got[2].FileIDs) != 1 || got[1].FileIDs[0] != id("upa") || got[2].FileIDs[0] != id("upf") {
		t.Fatalf("file posts = %+v, %+v", got[1], got[2])
	}
	for _, p := range got {
		if p.RootID != id("root") || p.ChannelID != town {
			t.Fatalf("post outside the thread: %+v", p)
		}
	}
}

func TestSendNeverPagesTheChannel(t *testing.T) {
	const z = "​"
	for in, want := range map[string]string{
		"hi @channel and @all, @here!":       "hi @" + z + "channel and @" + z + "all, @" + z + "here!",
		"@Channel @alice @allison":           "@" + z + "Channel @alice @allison",
		"run `notify @all` now, @all":        "run `notify @all` now, @" + z + "all",
		"```\necho @here\n```\n@here":        "```\necho @here\n```\n@" + z + "here",
		"an unclosed ` backtick @channel":    "an unclosed ` backtick @" + z + "channel",
		"nobody here, all channel, a@b.test": "nobody here, all channel, a@b.test",
	} {
		if got := quiet(in); got != want {
			t.Errorf("quiet(%q) = %q, want %q", in, got, want)
		}
	}
	r := newRig(t)
	r.f.handle["POST /posts"] = func(call) (int, any) { return 201, map[string]any{} }
	if err := r.a.Send(context.Background(), r.ch, r.f.cfg(), channels.Outbound{Text: "wake up @channel"}); err != nil {
		t.Fatal(err)
	}
	if got := r.f.posts(); len(got) != 1 || strings.Contains(got[0].Message, "@channel") || got[0].Message != "wake up @"+z+"channel" {
		t.Fatalf("posts = %+v", got)
	}
}

func TestSendWordsARefusal(t *testing.T) {
	r := newRig(t)
	r.f.handle["POST /posts"] = func(call) (int, any) {
		return 403, map[string]any{"message": "You do not have the appropriate permissions.", "status_code": 403}
	}
	err := r.a.Send(context.Background(), r.ch, r.f.cfg(), channels.Outbound{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "add it to the team and the channel") || channels.IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
	err = r.a.Send(context.Background(), r.ch, r.f.cfg(), channels.Outbound{ExternalID: "Town Square", Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "not a channel id") {
		t.Fatalf("err = %v", err)
	}
}

// Send needs no websocket: a channel whose worker is down still posts.
func TestSendWorksWhileTheWorkerIsDown(t *testing.T) {
	f := newFakeServer(t)
	f.handle["POST /posts"] = func(call) (int, any) { return 201, map[string]any{} }
	if err := New().Send(context.Background(), newChannel(), f.cfg(), channels.Outbound{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if len(f.posts()) != 1 {
		t.Fatal("nothing was posted")
	}
}

func TestTypingShowsInTheThreadBeingAnswered(t *testing.T) {
	r := newRig(t)
	r.f.handle["POST /users/me/typing"] = func(call) (int, any) { return 200, map[string]any{"status": "OK"} }
	r.post(t, r.f.cfg(), post{ID: id("root"), Message: "@silo go"}, "O", botID)
	if err := r.a.Typing(context.Background(), r.ch, town, true); err != nil {
		t.Fatal(err)
	}
	if err := r.a.Typing(context.Background(), r.ch, town, false); err != nil {
		t.Fatal(err)
	}
	got := r.f.made("POST /users/me/typing")
	if len(got) != 1 {
		t.Fatalf("%d typing calls, want one (off sends nothing)", len(got))
	}
	var body map[string]string
	_ = json.Unmarshal(got[0].Body, &body)
	if body["channel_id"] != town || body["parent_id"] != id("root") {
		t.Fatalf("typing body = %v", body)
	}
}

// --- history and setup ---

func TestHistoryReadsOldestFirst(t *testing.T) {
	r := newRig(t)
	r.f.handle["GET /channels/"+town+"/posts"] = func(call) (int, any) {
		return 200, map[string]any{
			"order": []string{id("p4"), id("p3"), id("p2"), id("p1")},
			"posts": map[string]any{
				id("p1"): map[string]any{"id": id("p1"), "user_id": aliceID, "message": "first", "create_at": 1000},
				id("p2"): map[string]any{"id": id("p2"), "user_id": aliceID, "type": "system_join_channel", "message": "joined", "create_at": 2000},
				id("p3"): map[string]any{"id": id("p3"), "user_id": botID, "root_id": id("p1"), "message": "answer", "create_at": 3000},
				id("p4"): map[string]any{"id": id("p4"), "user_id": aliceID, "message": "", "create_at": 4000, "file_ids": []string{id("f1")},
					"metadata": map[string]any{"files": []map[string]any{{"id": id("f1"), "name": "cat.png", "mime_type": "image/png"}}}},
			},
		}
	}
	got, err := r.a.History(context.Background(), r.ch, r.f.cfg(), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("history = %+v", got)
	}
	if got[0].Author != "alice" || got[0].Text != "first" || got[0].Out || !got[0].At.Equal(time.UnixMilli(1000)) {
		t.Fatalf("first = %+v", got[0])
	}
	if !got[1].Out || got[1].Text != "[in thread] answer" {
		t.Fatalf("the bot's = %+v", got[1])
	}
	if got[2].Text != "[attachment: cat.png (image/png)]" {
		t.Fatalf("file post = %+v", got[2])
	}
	if c := r.f.made("GET /channels/" + town + "/posts"); c[0].Query != "per_page=20" {
		t.Fatalf("query = %q", c[0].Query)
	}
	if n := len(r.f.made("GET /users/" + aliceID)); n != 1 {
		t.Fatalf("alice was looked up %d times, want once", n)
	}
}

func TestPickerListsWhereTheBotCanTalk(t *testing.T) {
	f := newFakeServer(t)
	f.handle["GET /users/me/teams"] = func(call) (int, any) {
		return 200, []map[string]any{{"id": id("team1"), "display_name": "Acme"}, {"id": id("team2"), "display_name": "Labs"}}
	}
	dm := map[string]any{"id": direct, "type": "D", "name": aliceID + "__" + botID}
	f.handle["GET /users/me/teams/"+id("team1")+"/channels"] = func(call) (int, any) {
		return 200, []map[string]any{
			{"id": town, "type": "O", "display_name": "Town Square"},
			{"id": id("secret"), "type": "P", "display_name": "Plans"},
			{"id": id("gone"), "type": "O", "display_name": "Archived", "delete_at": 5},
			{"id": id("self"), "type": "D", "name": botID + "__" + botID},
			dm,
		}
	}
	f.handle["GET /users/me/teams/"+id("team2")+"/channels"] = func(call) (int, any) {
		return 200, []map[string]any{
			{"id": id("group"), "type": "G", "display_name": "alice, bob, silo"},
			dm,
		}
	}
	// No worker runs: the picker reads over REST alone.
	st, err := New().Action(context.Background(), newChannel(), f.cfg(), "list_channels", nil)
	if err != nil || st.Kind != channels.StateSelect {
		t.Fatalf("Action = %+v, %v", st, err)
	}
	want := []channels.Option{
		{Value: direct, Label: "DM · Alice Liddell (@alice)"},
		{Value: id("group"), Label: "Group message · alice, bob, silo"},
		{Value: id("secret"), Label: "Plans · Acme (private)"},
		{Value: town, Label: "Town Square · Acme"},
	}
	if !slices.Equal(st.Options, want) {
		t.Fatalf("options = %+v", st.Options)
	}
	st, err = New().Action(context.Background(), newChannel(), f.cfg(), "refresh", nil)
	if err != nil || st.Message != "Connected as @silo" {
		t.Fatalf("refresh = %+v, %v", st, err)
	}
	if _, err := New().Action(context.Background(), newChannel(), f.cfg(), "nope", nil); err == nil {
		t.Fatal("an unknown action was accepted")
	}
}

func TestTargetAndServerAddressAsTyped(t *testing.T) {
	if targetID(" "+town+" ") != town || targetID("Town Square") != "" || targetID(strings.ToUpper(town)) != "" {
		t.Fatal("targetID takes a 26-character id and nothing else")
	}
	for in, want := range map[string]string{
		"chat.example.com":                    "https://chat.example.com",
		"https://chat.example.com/":           "https://chat.example.com",
		"http://10.0.0.5:8065/api/v4":         "http://10.0.0.5:8065",
		"https://example.com/mattermost/":     "https://example.com/mattermost",
		"https://example.com/chat/api/v4/x/y": "https://example.com/chat",
	} {
		if got, err := serverURL(in); err != nil || got != want {
			t.Errorf("serverURL(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ftp://chat.example.com", "https://"} {
		if _, err := serverURL(in); err == nil {
			t.Errorf("serverURL(%q) was accepted", in)
		}
	}
}

func TestDescriptor(t *testing.T) {
	d := New().Descriptor()
	if d.Slug != "mattermost" || !d.RequiresTarget || !strings.HasPrefix(d.Logo, "data:image/svg+xml;base64,") || d.Guide == "" {
		t.Fatalf("descriptor = %+v", d)
	}
	if _, ok := channels.Lookup("mattermost"); !ok {
		t.Fatal("the adapter is not registered")
	}
}
