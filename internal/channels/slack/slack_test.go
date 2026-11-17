package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// --- fakes ---

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

// call is one Web API request the adapter made.
type call struct {
	Method string
	Form   url.Values
	Auth   string
	Body   []byte
}

// fakeSlack answers Slack's Web API from per-method handlers and records every
// call. A method with no handler answers ok.
type fakeSlack struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	calls  []call
	handle map[string]func(c call) (int, any)
	// sock serves the Socket Mode websocket, when a test needs one.
	sock func(ctx context.Context, ws *websocket.Conn)
}

func newFakeSlack(t *testing.T) *fakeSlack {
	f := &fakeSlack{t: t, handle: map[string]func(call) (int, any){}}
	f.handle["auth.test"] = func(call) (int, any) {
		return 200, map[string]any{"ok": true, "user_id": "UBOT", "bot_id": "BBOT", "user": "silo", "team": "Acme"}
	}
	f.handle["users.info"] = func(c call) (int, any) {
		names := map[string]string{"UALICE": "Alice", "UBOB": "Bob", "UBOT": "silo"}
		id := c.Form.Get("user")
		return 200, map[string]any{"ok": true, "user": map[string]any{"id": id, "name": strings.ToLower(id), "profile": map[string]any{"display_name": names[id]}}}
	}
	f.handle["conversations.info"] = func(c call) (int, any) {
		id := c.Form.Get("channel")
		switch {
		case strings.HasPrefix(id, "D"):
			return 200, map[string]any{"ok": true, "channel": map[string]any{"id": id, "is_im": true, "user": "UALICE"}}
		default:
			return 200, map[string]any{"ok": true, "channel": map[string]any{"id": id, "name": "general", "is_channel": true}}
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		form := url.Values{}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			form, _ = url.ParseQuery(string(body))
		}
		for k, v := range r.URL.Query() {
			form[k] = v
		}
		c := call{Method: method, Form: form, Auth: r.Header.Get("Authorization"), Body: body}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		h := f.handle[method]
		f.mu.Unlock()
		status, out := 200, any(map[string]any{"ok": true})
		if h != nil {
			status, out = h(c)
		}
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "1")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/upload/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, call{Method: "upload " + strings.TrimPrefix(r.URL.Path, "/upload/"), Auth: r.Header.Get("Authorization"), Body: body})
		f.mu.Unlock()
		w.WriteHeader(200)
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, call{Method: "download " + r.URL.Path, Auth: r.Header.Get("Authorization")})
		f.mu.Unlock()
		_, _ = w.Write([]byte("file bytes"))
	})
	mux.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		// Socket Mode sends Origin: https://api.slack.com.
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.CloseNow()
		if f.sock != nil {
			f.sock(r.Context(), ws)
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) api() string { return f.srv.URL + "/api/" }

func (f *fakeSlack) on(method string, h func(c call) (int, any)) {
	f.mu.Lock()
	f.handle[method] = h
	f.mu.Unlock()
}

func (f *fakeSlack) made(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeSlack) adapter() *Adapter {
	a := New()
	a.apiURL = f.api()
	return a
}

func goodConfig(extra ...string) channels.Config {
	cfg := channels.Config{Values: map[string]string{"bot_token": "xoxb-1-abc", "app_token": "xapp-1-def"}}
	for i := 0; i+1 < len(extra); i += 2 {
		cfg.Values[extra[i]] = extra[i+1]
	}
	return cfg
}

// connected logs a fresh adapter in against the fake, as Start would.
func connected(t *testing.T, f *fakeSlack, ch *db.Channel, cfg channels.Config) (*Adapter, *conn) {
	t.Helper()
	a := f.adapter()
	c, err := a.login(context.Background(), a.client(cfg))
	if err != nil {
		t.Fatal(err)
	}
	c.mark = cfg.Get("working") != "off"
	a.putConn(ch.ID, c)
	return a, c
}

func channel(target string) *db.Channel {
	return &db.Channel{ID: "ch1", BotID: "b1", Adapter: "slack", Name: "Slack", ExternalID: target, Enabled: true, Inbound: true}
}

// --- tokens and targets ---

func TestCheckTokensCatchesMixUps(t *testing.T) {
	cases := []struct {
		bot, app, want string
	}{
		{"xoxb-1", "xapp-1", ""},
		{"", "xapp-1", "bot_token is required"},
		{"xapp-1", "xoxb-1", "app-level token"},
		{"xoxp-1", "xapp-1", "user token"},
		{"abc", "xapp-1", "starts with xoxb-"},
		{"xoxb-1", "", "app_token is required"},
		{"xoxb-1", "xoxb-2", "starts with xapp-"},
	}
	for _, c := range cases {
		err := checkTokens(channels.Config{Values: map[string]string{"bot_token": c.bot, "app_token": c.app}})
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%q/%q: %v", c.bot, c.app, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%q/%q: got %v, want %q", c.bot, c.app, err, c.want)
		}
	}
}

func TestTargetIDReadsWhatTheOwnerPastes(t *testing.T) {
	cases := map[string]string{
		"C0123ABCD":            "C0123ABCD",
		" G0123ABCD ":          "G0123ABCD",
		"D0123ABCD":            "D0123ABCD",
		"<#C0123ABCD|general>": "C0123ABCD",
		"<#C0123ABCD>":         "C0123ABCD",
		"https://acme.slack.com/archives/C0123ABCD/p1700000000000100": "C0123ABCD",
		"https://acme.slack.com/archives/C0123ABCD":                   "C0123ABCD",
		"https://app.slack.com/client/T0123ABCD/C0123ABCD/thread/x":   "C0123ABCD",
		"slack://channel?team=T0123ABCD&id=C0123ABCD":                 "C0123ABCD",
		"https://slack.com/app_redirect?channel=C0123ABCD":            "C0123ABCD",
		"general":        "",
		"#general":       "",
		"c0123abcd":      "",
		"U0123ABCD":      "",
		"https://x.test": "",
	}
	for in, want := range cases {
		if got := targetID(in); got != want {
			t.Errorf("targetID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFriendlyMarksWhatRetryingCannotFix(t *testing.T) {
	for _, code := range []string{"invalid_auth", "token_revoked", "not_allowed_token_type", "account_inactive", "app_uninstalled"} {
		if err := friendly(slackapi.SlackErrorResponse{Err: code}); !channels.IsPermanent(err) {
			t.Errorf("%s: not permanent: %v", code, err)
		}
	}
	// Socket Mode reports its refusals as bare codes.
	if err := friendly(errors.New("invalid_auth")); !channels.IsPermanent(err) {
		t.Errorf("bare invalid_auth: %v", err)
	}
	err := friendly(slackapi.SlackErrorResponse{Err: "missing_scope", Needed: "reactions:write"})
	if channels.IsPermanent(err) || !strings.Contains(err.Error(), "reactions:write") {
		t.Errorf("missing_scope: %v", err)
	}
	if err := friendly(slackapi.SlackErrorResponse{Err: "not_in_channel"}); !strings.Contains(err.Error(), "/invite") {
		t.Errorf("not_in_channel: %v", err)
	}
}

// --- inbound ---

func message(channel, user, text, ts string) inbound {
	return inbound{user: user, text: text, ts: ts, channel: channel, kind: "channel"}
}

func TestInboundAnswersMentionsInAThreadUnderTheMessage(t *testing.T) {
	f := newFakeSlack(t)
	ch := channel("C0123ABCD")
	cfg := goodConfig()
	a, c := connected(t, f, ch, cfg)
	h := &fakeHost{current: ch}
	ctx := context.Background()

	// Not addressed: ignored in the default reply mode.
	if err := a.onMessage(ctx, ch, cfg, h, c, message("C0123ABCD", "UALICE", "lunch?", "1.1")); err != nil {
		t.Fatal(err)
	}
	if n := len(h.inbound()); n != 0 {
		t.Fatalf("an unaddressed message was delivered: %+v", h.inbound())
	}

	// Mentioned: delivered, names read out, answered in a thread under it.
	if err := a.onMessage(ctx, ch, cfg, h, c, message("C0123ABCD", "UALICE", "<@UBOT> ask <@UBOB> about <https://x.test/a?b=1&amp;c=2|the doc> &lt;3", "1.2")); err != nil {
		t.Fatal(err)
	}
	got := h.inbound()
	if len(got) != 1 {
		t.Fatalf("delivered %+v", got)
	}
	in := got[0]
	if in.ExternalID != "C0123ABCD" || in.Author != "Alice" || in.Title != "#general · Acme" || in.ReplyTo != "1.2" {
		t.Fatalf("inbound %+v", in)
	}
	if want := "@silo ask @Bob about the doc (https://x.test/a?b=1&c=2) <3"; in.Text != want {
		t.Fatalf("text %q, want %q", in.Text, want)
	}

	// The app_mention event of the same message is not a second message.
	dup := message("C0123ABCD", "UALICE", "<@UBOT> ask", "1.2")
	dup.mention = true
	_ = a.onMessage(ctx, ch, cfg, h, c, dup)
	if n := len(h.inbound()); n != 1 {
		t.Fatalf("the same message was delivered %d times", n)
	}

	// A follow-up in the thread it answered in needs no mention.
	follow := message("C0123ABCD", "UBOB", "and tomorrow?", "1.3")
	follow.thread = "1.2"
	_ = a.onMessage(ctx, ch, cfg, h, c, follow)
	got = h.inbound()
	if len(got) != 2 || got[1].ReplyTo != "1.2" || got[1].Author != "Bob" {
		t.Fatalf("thread follow-up: %+v", got)
	}

	// Someone else's thread is not.
	other := message("C0123ABCD", "UBOB", "unrelated", "1.5")
	other.thread = "1.4"
	_ = a.onMessage(ctx, ch, cfg, h, c, other)
	if n := len(h.inbound()); n != 2 {
		t.Fatalf("a message in an unrelated thread was delivered")
	}
}

func TestInboundIgnoresAppsEditsAndOtherConversations(t *testing.T) {
	f := newFakeSlack(t)
	ch := channel("C0123ABCD")
	cfg := goodConfig("reply_mode", "always")
	a, c := connected(t, f, ch, cfg)
	h := &fakeHost{current: ch}
	ctx := context.Background()

	self := message("C0123ABCD", "UBOT", "my own words", "2.1")
	bot := message("C0123ABCD", "UOTHER", "another app", "2.2")
	bot.bot = "BOTHER"
	edit := message("C0123ABCD", "UALICE", "edited", "2.3")
	edit.subtype = "message_changed"
	join := message("C0123ABCD", "UALICE", "has joined", "2.4")
	join.subtype = "channel_join"
	elsewhere := message("C9999ZZZZ", "UALICE", "<@UBOT> hi", "2.5")
	empty := message("C0123ABCD", "UALICE", "  ", "2.6")
	reminder := message("C0123ABCD", "USLACKBOT", "Reminder: standup", "2.9")
	for _, m := range []inbound{self, bot, edit, join, elsewhere, empty, reminder} {
		if err := a.onMessage(ctx, ch, cfg, h, c, m); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.inbound(); len(got) != 0 {
		t.Fatalf("delivered %+v", got)
	}

	// In "always" mode a plain message is answered, and a broadcast reply is
	// answered in its thread.
	_ = a.onMessage(ctx, ch, cfg, h, c, message("C0123ABCD", "UALICE", "plain", "2.7"))
	bc := message("C0123ABCD", "UALICE", "also to the channel", "2.8")
	bc.subtype, bc.thread = "thread_broadcast", "2.7"
	_ = a.onMessage(ctx, ch, cfg, h, c, bc)
	got := h.inbound()
	if len(got) != 2 || got[0].ReplyTo != "2.7" || got[1].ReplyTo != "2.7" {
		t.Fatalf("delivered %+v", got)
	}
}

func TestInboundDMAndAnswerInTheChannel(t *testing.T) {
	f := newFakeSlack(t)
	ctx := context.Background()

	// A DM is always answered, in place.
	dmCh := channel("D0123ABCD")
	cfg := goodConfig()
	a, c := connected(t, f, dmCh, cfg)
	h := &fakeHost{current: dmCh}
	dm := inbound{user: "UALICE", text: "hi", ts: "3.1", channel: "D0123ABCD", kind: "im"}
	_ = a.onMessage(ctx, dmCh, cfg, h, c, dm)
	got := h.inbound()
	if len(got) != 1 || got[0].ReplyTo != "" || got[0].Title != "DM · Alice" {
		t.Fatalf("dm: %+v", got)
	}

	// Answer in: the channel keeps a top-level answer at the top level, and
	// still answers a threaded message in its thread.
	ch := channel("C0123ABCD")
	cfg = goodConfig("reply_in", "channel")
	a, c = connected(t, f, ch, cfg)
	h = &fakeHost{current: ch}
	_ = a.onMessage(ctx, ch, cfg, h, c, message("C0123ABCD", "UALICE", "<@UBOT> hi", "3.2"))
	threaded := message("C0123ABCD", "UALICE", "<@UBOT> and here", "3.4")
	threaded.thread = "3.3"
	_ = a.onMessage(ctx, ch, cfg, h, c, threaded)
	got = h.inbound()
	if len(got) != 2 || got[0].ReplyTo != "" || got[1].ReplyTo != "3.3" {
		t.Fatalf("reply_in=channel: %+v", got)
	}
}

func TestInboundTargetPickedAfterStartTakesEffect(t *testing.T) {
	f := newFakeSlack(t)
	started := channel("")
	cfg := goodConfig()
	a, c := connected(t, f, started, cfg)
	h := &fakeHost{current: channel("C0123ABCD")}
	_ = a.onMessage(context.Background(), started, cfg, h, c, message("C0123ABCD", "UALICE", "<@UBOT> hi", "4.1"))
	if n := len(h.inbound()); n != 1 {
		t.Fatalf("delivered %d, want the live row's target to count", n)
	}
}

func TestInboundFilesAndVoiceClips(t *testing.T) {
	f := newFakeSlack(t)
	ch := channel("D0123ABCD")
	cfg := goodConfig()
	a, c := connected(t, f, ch, cfg)
	h := &fakeHost{current: ch}

	raw := json.RawMessage(`{"type":"event_callback","event":{"type":"message","subtype":"file_share","channel":"D0123ABCD","channel_type":"im","user":"UALICE","text":"","ts":"5.1",
		"files":[{"id":"F1","name":"audio_message.webm","mimetype":"audio/webm","size":10,"subtype":"slack_audio","url_private_download":"` + f.srv.URL + `/files/F1"},
		         {"id":"F2","name":"report.pdf","mimetype":"application/pdf","size":10,"url_private_download":"` + f.srv.URL + `/files/F2"},
		         {"id":"F3","name":"far.pdf","mimetype":"application/pdf","size":10,"url_private_download":"https://evil.test/F3"},
		         {"id":"F4","name":"old.png","mode":"hidden_by_limit"}]}}`)
	ev, err := slackevents.ParseEvent(raw, slackevents.OptionNoVerifyToken())
	if err != nil {
		t.Fatal(err)
	}
	m, ok := readEvent(ev, raw)
	if !ok {
		t.Fatal("message not read")
	}
	if err := a.onMessage(context.Background(), ch, cfg, h, c, m); err != nil {
		t.Fatal(err)
	}
	got := h.inbound()
	if len(got) != 1 || len(got[0].Files) != 4 {
		t.Fatalf("delivered %+v", got)
	}
	files := got[0].Files
	if !files[0].Voice || files[1].Voice || files[0].Mime != "audio/webm" || files[1].Name != "report.pdf" {
		t.Fatalf("files %+v", files)
	}
	data, err := files[1].Fetch(context.Background())
	if err != nil || string(data) != "file bytes" {
		t.Fatalf("fetch: %q %v", data, err)
	}
	if d := f.made("download /files/F2"); len(d) != 1 || d[0].Auth != "Bearer xoxb-1-abc" {
		t.Fatalf("download %+v", d)
	}
	// The token goes to Slack's hosts only.
	if _, err := files[2].Fetch(context.Background()); err == nil {
		t.Fatal("a file outside Slack was fetched with the token")
	}
	if _, err := files[3].Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("hidden file: %v", err)
	}
}

func TestReadEventTakesMessagesAndMentions(t *testing.T) {
	raw := json.RawMessage(`{"type":"event_callback","event":{"type":"app_mention","channel":"C0123ABCD","user":"UALICE","text":"<@UBOT> hi","ts":"6.1","thread_ts":"6.0"}}`)
	ev, err := slackevents.ParseEvent(raw, slackevents.OptionNoVerifyToken())
	if err != nil {
		t.Fatal(err)
	}
	m, ok := readEvent(ev, raw)
	if !ok || !m.mention || m.thread != "6.0" || m.channel != "C0123ABCD" || m.user != "UALICE" {
		t.Fatalf("app_mention read as %+v", m)
	}
	raw = json.RawMessage(`{"type":"event_callback","event":{"type":"reaction_added","user":"UALICE"}}`)
	ev, _ = slackevents.ParseEvent(raw, slackevents.OptionNoVerifyToken())
	if _, ok := readEvent(ev, raw); ok {
		t.Fatal("a reaction was read as a message")
	}
}

// --- outbound ---

func TestSendWritesSlackMarkupInTheThread(t *testing.T) {
	f := newFakeSlack(t)
	ch := channel("C0123ABCD")
	cfg := goodConfig()
	a := f.adapter() // no live session: sending needs only the bot token

	long := strings.Repeat("word ", 1000) // over one message
	err := a.Send(context.Background(), ch, cfg, channels.Outbound{
		Text: "**Done** — see [the doc](https://x.test/?a=1&b=2) <!channel>\n\n" + long, ReplyTo: "7.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	posts := f.made("chat.postMessage")
	if len(posts) != 2 {
		t.Fatalf("%d posts, want the long text in two", len(posts))
	}
	first := posts[0].Form
	if first.Get("channel") != "C0123ABCD" || first.Get("thread_ts") != "7.1" || first.Get("link_names") != "false" {
		t.Fatalf("post %v", first)
	}
	if want := "*Done* — see <https://x.test/?a=1&amp;b=2|the doc> &lt;!channel&gt;"; !strings.HasPrefix(first.Get("text"), want) {
		t.Fatalf("text %q, want it to start %q", first.Get("text"), want)
	}
	if posts[1].Form.Get("thread_ts") != "7.1" {
		t.Fatal("the second part left the thread")
	}

	// No thread: the conversation itself.
	if err := a.Send(context.Background(), ch, cfg, channels.Outbound{Text: "top"}); err != nil {
		t.Fatal(err)
	}
	if p := f.made("chat.postMessage"); p[len(p)-1].Form.Has("thread_ts") {
		t.Fatal("a send with no anchor went to a thread")
	}
}

func TestSendUploadsFilesIntoTheThread(t *testing.T) {
	f := newFakeSlack(t)
	n := 0
	f.on("files.getUploadURLExternal", func(c call) (int, any) {
		n++
		return 200, map[string]any{"ok": true, "upload_url": fmt.Sprintf("%s/upload/%d", f.srv.URL, n), "file_id": fmt.Sprintf("F%d", n)}
	})
	f.on("files.completeUploadExternal", func(c call) (int, any) {
		return 200, map[string]any{"ok": true, "files": []map[string]any{{"id": "F1"}}}
	})
	a := f.adapter()
	var files []channels.Attachment
	for i := range 11 {
		files = append(files, channels.Attachment{Name: fmt.Sprintf("f%d.txt", i), Mime: "text/plain", Data: []byte("x")})
	}
	files = append(files, channels.Attachment{Name: "empty.txt"})
	err := a.Send(context.Background(), channel("C0123ABCD"), goodConfig(), channels.Outbound{Text: "here", ReplyTo: "8.1", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(f.made("files.getUploadURLExternal")); got != 11 {
		t.Fatalf("%d staged, want 11 (the empty one skipped)", got)
	}
	done := f.made("files.completeUploadExternal")
	if len(done) != 2 {
		t.Fatalf("%d completions, want batches of ten", len(done))
	}
	var batch []map[string]string
	_ = json.Unmarshal([]byte(done[0].Form.Get("files")), &batch)
	if len(batch) != 10 || done[0].Form.Get("channel_id") != "C0123ABCD" || done[0].Form.Get("thread_ts") != "8.1" {
		t.Fatalf("completion %v", done[0].Form)
	}
	if up := f.made("upload 1"); len(up) != 1 || !strings.Contains(string(up[0].Body), "f0.txt") {
		t.Fatalf("upload %+v", up)
	}
}

func TestSendWaitsOutAShortRateLimit(t *testing.T) {
	f := newFakeSlack(t)
	tries := 0
	f.on("chat.postMessage", func(call) (int, any) {
		if tries++; tries == 1 {
			return http.StatusTooManyRequests, map[string]any{"ok": false, "error": "ratelimited"}
		}
		return 200, map[string]any{"ok": true, "ts": "9.1", "channel": "C0123ABCD"}
	})
	start := time.Now()
	if err := f.adapter().Send(context.Background(), channel("C0123ABCD"), goodConfig(), channels.Outbound{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if tries != 2 || time.Since(start) < time.Second {
		t.Fatalf("tries %d after %s", tries, time.Since(start))
	}
}

func TestSendWordsARefusal(t *testing.T) {
	f := newFakeSlack(t)
	f.on("chat.postMessage", func(call) (int, any) { return 200, map[string]any{"ok": false, "error": "not_in_channel"} })
	err := f.adapter().Send(context.Background(), channel("C0123ABCD"), goodConfig(), channels.Outbound{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "/invite") {
		t.Fatalf("got %v", err)
	}
	if err := f.adapter().Send(context.Background(), channel("general"), goodConfig(), channels.Outbound{Text: "hi"}); err == nil {
		t.Fatal("sent to a target that is not a conversation id")
	}
}

func TestTypingMarksTheMessageOnceAndUnmarksIt(t *testing.T) {
	f := newFakeSlack(t)
	ch := channel("C0123ABCD")
	cfg := goodConfig()
	a, c := connected(t, f, ch, cfg)
	h := &fakeHost{current: ch}
	ctx := context.Background()
	_ = a.onMessage(ctx, ch, cfg, h, c, message("C0123ABCD", "UALICE", "<@UBOT> go", "10.1"))

	for range 3 {
		if err := a.Typing(ctx, ch, "C0123ABCD", true); err != nil {
			t.Fatal(err)
		}
	}
	adds := f.made("reactions.add")
	if len(adds) != 1 || adds[0].Form.Get("name") != "eyes" || adds[0].Form.Get("timestamp") != "10.1" || adds[0].Form.Get("channel") != "C0123ABCD" {
		t.Fatalf("adds %+v", adds)
	}
	// A message arriving meanwhile is marked too; off clears both.
	_ = a.onMessage(ctx, ch, cfg, h, c, message("C0123ABCD", "UALICE", "<@UBOT> and", "10.2"))
	_ = a.Typing(ctx, ch, "C0123ABCD", true)
	f.on("reactions.remove", func(c call) (int, any) {
		if c.Form.Get("timestamp") == "10.2" {
			return 200, map[string]any{"ok": false, "error": "no_reaction"} // someone took it off already
		}
		return 200, map[string]any{"ok": true}
	})
	if err := a.Typing(ctx, ch, "C0123ABCD", false); err != nil {
		t.Fatal(err)
	}
	if rm := f.made("reactions.remove"); len(rm) != 2 {
		t.Fatalf("removes %+v", rm)
	}
	if err := a.Typing(ctx, ch, "C0123ABCD", false); err != nil || len(f.made("reactions.remove")) != 2 {
		t.Fatal("a second off removed again")
	}

	// While working: show nothing.
	cfg = goodConfig("working", "off")
	a, c = connected(t, f, ch, cfg)
	_ = a.onMessage(ctx, ch, cfg, h, c, message("C0123ABCD", "UALICE", "<@UBOT> quiet", "10.3"))
	_ = a.Typing(ctx, ch, "C0123ABCD", true)
	if len(f.made("reactions.add")) != 2 {
		t.Fatal("marked a message with the setting off")
	}
}

// --- history and picker ---

func TestHistoryReadsTheConversationAndItsThreads(t *testing.T) {
	f := newFakeSlack(t)
	f.on("conversations.history", func(c call) (int, any) {
		if c.Form.Get("channel") != "C0123ABCD" {
			return 200, map[string]any{"ok": false, "error": "channel_not_found"}
		}
		// newest first, as Slack answers
		return 200, map[string]any{"ok": true, "messages": []map[string]any{
			{"type": "message", "user": "UBOB", "text": "later", "ts": "1700000300.000000"},
			{"type": "message", "subtype": "channel_join", "user": "UBOB", "text": "<@UBOB> has joined", "ts": "1700000250.000000"},
			{"type": "message", "user": "UALICE", "text": "<@UBOT> question", "ts": "1700000200.000000", "reply_count": 2, "thread_ts": "1700000200.000000"},
			{"type": "message", "bot_id": "BOTHER", "username": "Jira", "text": "ticket", "ts": "1700000100.000000", "subtype": "bot_message"},
		}}
	})
	f.on("conversations.replies", func(c call) (int, any) {
		return 200, map[string]any{"ok": true, "messages": []map[string]any{
			{"type": "message", "user": "UALICE", "text": "<@UBOT> question", "ts": "1700000200.000000", "thread_ts": "1700000200.000000"},
			{"type": "message", "user": "UBOT", "bot_id": "BBOT", "text": "answer &amp; more", "ts": "1700000210.000000", "thread_ts": "1700000200.000000"},
			{"type": "message", "user": "UALICE", "text": "thanks", "ts": "1700000220.000000", "thread_ts": "1700000200.000000",
				"files": []map[string]any{{"id": "F1", "name": "a.png", "mimetype": "image/png"}}},
		}}
	})
	a := f.adapter()
	msgs, err := a.History(context.Background(), channel("C0123ABCD"), goodConfig(), "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, m := range msgs {
		lines = append(lines, fmt.Sprintf("%s|%v|%s", m.Author, m.Out, m.Text))
	}
	want := []string{
		"Jira|false|ticket",
		"Alice|false|@silo question",
		"Bot|true|[in thread] answer & more",
		"Alice|false|[in thread] thanks\n[attachment: a.png (image/png)]",
		"Bob|false|later",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("history\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if !msgs[0].At.Equal(time.Unix(1700000100, 0)) {
		t.Fatalf("time %v", msgs[0].At)
	}
	// A limit keeps the newest.
	msgs, _ = a.History(context.Background(), channel("C0123ABCD"), goodConfig(), "C0123ABCD", 2)
	if len(msgs) != 2 || msgs[1].Text != "later" {
		t.Fatalf("limited %+v", msgs)
	}
	if _, err := a.History(context.Background(), channel("C0123ABCD"), goodConfig(), "C9999ZZZZ", 5); err == nil || !strings.Contains(err.Error(), "pick it again") {
		t.Fatalf("unknown conversation: %v", err)
	}
}

func TestPickerListsWhereTheAppCanTalk(t *testing.T) {
	f := newFakeSlack(t)
	pages := 0
	f.on("conversations.list", func(c call) (int, any) {
		pages++
		if c.Form.Get("cursor") == "" {
			return 200, map[string]any{"ok": true, "channels": []map[string]any{
				{"id": "C0000AAAA", "name": "random", "is_channel": true, "is_member": false},
				{"id": "C0000BBBB", "name": "general", "is_channel": true, "is_member": true},
				{"id": "G0000CCCC", "name": "secret", "is_private": true, "is_member": true},
			}, "response_metadata": map[string]any{"next_cursor": "more"}}
		}
		return 200, map[string]any{"ok": true, "channels": []map[string]any{
			{"id": "D0000DDDD", "is_im": true, "user": "UALICE"},
			{"id": "D0000EEEE", "is_im": true, "user": "USLACKBOT"},
			{"id": "G0000FFFF", "name": "mpdm-alice--bob-1", "is_mpim": true, "is_private": true},
		}}
	})
	ch := channel("")
	a, _ := connected(t, f, ch, goodConfig())
	st, err := a.Action(context.Background(), ch, goodConfig(), "list_channels", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range st.Options {
		got = append(got, o.Value+" "+o.Label)
	}
	want := []string{"C0000BBBB #general", "G0000CCCC #secret (private)", "D0000DDDD DM · Alice", "G0000FFFF Group DM · alice, bob"}
	if st.Kind != channels.StateSelect || pages != 2 || !slices.Equal(got, want) {
		t.Fatalf("picker %v (%d pages)\nwant %v", got, pages, want)
	}

	f.on("conversations.list", func(call) (int, any) { return 200, map[string]any{"ok": true, "channels": []any{}} })
	st, _ = a.Action(context.Background(), ch, goodConfig(), "list_channels", nil)
	if len(st.Options) != 0 || !strings.Contains(st.Message, "/invite @silo") {
		t.Fatalf("empty picker %+v", st)
	}
}

// --- the Socket Mode session ---

// envelope is an events_api request as Socket Mode sends it.
func envelope(id, event string) string {
	return `{"envelope_id":"` + id + `","type":"events_api","accepts_response_payload":false,"payload":{"type":"event_callback","team_id":"T1","event":` + event + `}}`
}

func TestStartDeliversOverSocketModeAndAcknowledges(t *testing.T) {
	f := newFakeSlack(t)
	f.on("apps.connections.open", func(c call) (int, any) {
		if c.Auth != "Bearer xapp-1-def" {
			return 200, map[string]any{"ok": false, "error": "not_allowed_token_type"}
		}
		return 200, map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/socket"}
	})
	acks := make(chan string, 4)
	f.sock = func(ctx context.Context, ws *websocket.Conn) {
		_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","num_connections":1,"connection_info":{"app_id":"A1"}}`))
		_ = ws.Write(ctx, websocket.MessageText, []byte(envelope("e1", `{"type":"message","channel":"C0123ABCD","channel_type":"channel","user":"UALICE","text":"<@UBOT> hello","ts":"11.1"}`)))
		_ = ws.Write(ctx, websocket.MessageText, []byte(envelope("e2", `{"type":"app_mention","channel":"C0123ABCD","user":"UALICE","text":"<@UBOT> hello","ts":"11.1"}`)))
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var ack struct {
				EnvelopeID string `json:"envelope_id"`
			}
			if json.Unmarshal(data, &ack) == nil && ack.EnvelopeID != "" {
				acks <- ack.EnvelopeID
			}
		}
	}
	ch := channel("C0123ABCD")
	h := &fakeHost{current: ch}
	a := f.adapter()
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan error, 1)
	go func() { ended <- a.Start(ctx, ch, goodConfig(), h) }()

	for _, want := range []string{"e1", "e2"} {
		select {
		case got := <-acks:
			if got != want {
				t.Fatalf("ack %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no ack for %s; states %+v; calls %+v", want, h.states, f.calls)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.inbound()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := h.inbound()
	if len(got) != 1 || got[0].ReplyTo != "11.1" {
		t.Fatalf("delivered %+v, want the message once", got)
	}
	if st := h.lastState(); st.Kind != channels.StateInfo || st.Message != "Connected as @silo in Acme" {
		t.Fatalf("state %+v", st)
	}
	if a.conn(ch.ID) == nil {
		t.Fatal("no live session while started")
	}
	cancel()
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not end")
	}
	if a.conn(ch.ID) != nil {
		t.Fatal("the session outlived Start")
	}
}

func TestStartStopsOnTokensSlackRefuses(t *testing.T) {
	cases := map[string]string{
		"invalid_auth":           "rejected a token",
		"not_allowed_token_type": "wrong kind",
	}
	for code, want := range cases {
		f := newFakeSlack(t)
		f.on("apps.connections.open", func(call) (int, any) { return 200, map[string]any{"ok": false, "error": code} })
		ch := channel("C0123ABCD")
		h := &fakeHost{current: ch}
		ended := make(chan error, 1)
		go func() { ended <- f.adapter().Start(context.Background(), ch, goodConfig(), h) }()
		select {
		case err := <-ended:
			if !channels.IsPermanent(err) || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: Start ended with %v", code, err)
			}
			if st := h.lastState(); st.Kind != channels.StateError {
				t.Fatalf("%s: state %+v", code, st)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: Start kept retrying", code)
		}
	}

	// A bot token Slack does not know fails before any socket.
	f := newFakeSlack(t)
	f.on("auth.test", func(call) (int, any) { return 200, map[string]any{"ok": false, "error": "invalid_auth"} })
	err := f.adapter().Start(context.Background(), channel("C0123ABCD"), goodConfig(), &fakeHost{})
	if !channels.IsPermanent(err) {
		t.Fatalf("auth.test refusal: %v", err)
	}
	// Swapped tokens never reach Slack.
	err = f.adapter().Start(context.Background(), channel("C0123ABCD"), goodConfig("bot_token", "xapp-1", "app_token", "xoxb-1"), &fakeHost{})
	if !channels.IsPermanent(err) || len(f.made("apps.connections.open")) != 0 {
		t.Fatalf("swapped tokens: %v", err)
	}
}
