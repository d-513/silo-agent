package teams

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
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
	appID    = "11111111-2222-3333-4444-555555555555"
	tenantID = "contoso-tenant"
	secret   = "s3cret-value"
	hookURL  = "https://silo.example.com/channels/hook/ch1"
	botID    = "28:" + appID
	chatID   = "a:1personal"
	teamConv = "19:general@thread.tacv2"
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

// PublishState keeps the values in the row, as the host does.
func (h *fakeHost) PublishState(_ string, st channels.State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.states = append(h.states, st)
	if h.current == nil || len(st.Values) == 0 {
		return
	}
	var kept channels.State
	_ = json.Unmarshal([]byte(h.current.StateJSON), &kept)
	if kept.Values == nil {
		kept.Values = map[string]string{}
	}
	for k, v := range st.Values {
		kept.Values[k] = v
	}
	b, _ := json.Marshal(kept)
	h.current.StateJSON = string(b)
}
func (h *fakeHost) CurrentChannel(string) (*db.Channel, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current == nil {
		return nil, false
	}
	row := *h.current
	return &row, true
}
func (h *fakeHost) DataDir() string     { return "" }
func (h *fakeHost) DatabaseURL() string { return "" }

func (h *fakeHost) inbound() []channels.Inbound {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.delivered)
}

// posted is one activity the adapter sent to the connector.
type posted struct {
	Conversation string
	Auth         string
	Activity     outgoing
}

// fakeMicrosoft is all of Microsoft the adapter talks to, on one https
// server: the Bot Framework's signing keys, the sign-in service that hands the
// bot its token, and the connector.
type fakeMicrosoft struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu     sync.Mutex
	tokens int // tokens handed out
	posts  []posted
	// refuse makes the sign-in service answer as Microsoft does to a bad secret.
	refuse bool
	// file is what the download address serves, and fileAuth what it was asked with.
	file     []byte
	fileAuth []string
}

func newFakeMicrosoft(t *testing.T) *fakeMicrosoft {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeMicrosoft{t: t, key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/openid", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"issuer": tokenIssuer, "jwks_uri": f.srv.URL + "/keys"})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		pub := key.PublicKey
		writeJSON(w, 200, map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/login/"+tenantID+"/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.refuse || r.Form.Get("client_secret") != secret || r.Form.Get("client_id") != appID ||
			r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("scope") != connectorScope {
			writeJSON(w, 401, map[string]any{
				"error":             "invalid_client",
				"error_description": "AADSTS7000215: Invalid client secret provided.\nTrace ID: abc",
			})
			return
		}
		f.tokens++
		writeJSON(w, 200, map[string]any{"token_type": "Bearer", "expires_in": 3600, "access_token": "bot-token"})
	})
	mux.HandleFunc("/v3/conversations/", func(w http.ResponseWriter, r *http.Request) {
		conv := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v3/conversations/"), "/activities")
		var act outgoing
		_ = json.NewDecoder(r.Body).Decode(&act)
		f.mu.Lock()
		f.posts = append(f.posts, posted{Conversation: conv, Auth: r.Header.Get("Authorization"), Activity: act})
		f.mu.Unlock()
		writeJSON(w, 201, map[string]any{"id": "sent"})
	})
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.fileAuth = append(f.fileAuth, r.Header.Get("Authorization"))
		data := f.file
		f.mu.Unlock()
		_, _ = w.Write(data)
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)

	oldMeta, oldLogin := openIDMetadataURL, loginURL
	openIDMetadataURL, loginURL = f.srv.URL+"/openid", f.srv.URL+"/login"
	t.Cleanup(func() { openIDMetadataURL, loginURL = oldMeta, oldLogin })
	return f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// sign makes a token as the Bot Framework does, with the claims changed by edit.
func (f *fakeMicrosoft) sign(edit func(claims map[string]any)) string {
	claims := map[string]any{
		"iss":        tokenIssuer,
		"aud":        appID,
		"serviceurl": f.srv.URL,
		"nbf":        time.Now().Add(-time.Minute).Unix(),
		"exp":        time.Now().Add(10 * time.Minute).Unix(),
	}
	if edit != nil {
		edit(claims)
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signed := enc(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "k1"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fakeMicrosoft) sent() []posted {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.posts)
}

// rig is an adapter, its channel row and the fakes around it.
type rig struct {
	t    *testing.T
	ms   *fakeMicrosoft
	a    *Adapter
	host *fakeHost
	cfg  channels.Config
}

func newRig(t *testing.T, bound string, values map[string]string) *rig {
	ms := newFakeMicrosoft(t)
	a := New()
	a.client = ms.srv.Client()
	cfg := channels.Config{HookURL: hookURL, Values: map[string]string{
		"app_id": appID, "app_password": secret, "tenant_id": tenantID,
	}}
	for k, v := range values {
		cfg.Values[k] = v
	}
	host := &fakeHost{current: &db.Channel{ID: "ch1", BotID: "bot1", Adapter: "teams", ExternalID: bound}}
	return &rig{t: t, ms: ms, a: a, host: host, cfg: cfg}
}

func (r *rig) row() *db.Channel {
	row, _ := r.host.CurrentChannel("ch1")
	return row
}

// message is a message activity as Teams sends one; edit changes it.
func (r *rig) message(conv, kind, text string, edit func(act map[string]any)) map[string]any {
	act := map[string]any{
		"type":         "message",
		"id":           "act-" + text,
		"channelId":    "msteams",
		"serviceUrl":   r.ms.srv.URL,
		"from":         map[string]any{"id": "29:alice", "name": "Alice", "aadObjectId": "aad-alice"},
		"recipient":    map[string]any{"id": botID, "name": "Silo"},
		"conversation": map[string]any{"id": conv, "conversationType": kind},
		"text":         text,
	}
	if edit != nil {
		edit(act)
	}
	return act
}

// hook delivers an activity with the given Authorization header and answers
// the status the adapter gave.
func (r *rig) hook(auth string, act map[string]any) int {
	body, _ := json.Marshal(act)
	req := httptest.NewRequest(http.MethodPost, hookURL, bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	r.a.Hook(rec, req, r.row(), r.cfg, r.host)
	return rec.Code
}

// deliver is hook with a good token, which must be accepted.
func (r *rig) deliver(act map[string]any) {
	r.t.Helper()
	if code := r.hook("Bearer "+r.ms.sign(nil), act); code != http.StatusOK {
		r.t.Fatalf("hook answered %d, want 200", code)
	}
}

func mention(act map[string]any, id, name string) {
	act["entities"] = []map[string]any{{
		"type": "mention", "text": "<at>" + name + "</at>",
		"mentioned": map[string]any{"id": id, "name": name},
	}}
}

// --- tests ---

func TestDescriptor(t *testing.T) {
	d := New().Descriptor()
	if d.Slug != "teams" || d.Name != "Microsoft Teams" || !d.RequiresTarget {
		t.Fatalf("descriptor = %+v", d)
	}
	if !strings.HasPrefix(d.Logo, "data:image/svg+xml;base64,") || !strings.Contains(d.Guide, "messaging endpoint") {
		t.Fatal("the logo or the guide is not embedded")
	}
	var _ channels.Webhook = New()
	var _ channels.Typer = New()
}

func TestDeliversOnlyTheBoundConversation(t *testing.T) {
	r := newRig(t, chatID, nil)
	r.deliver(r.message("a:2other", "personal", "elsewhere", nil))
	r.deliver(r.message(chatID, "personal", "hello &amp; welcome&nbsp;", nil))
	// Teams sends an activity again when the answer was slow.
	r.deliver(r.message(chatID, "personal", "hello &amp; welcome&nbsp;", nil))
	// Not messages, and a bot's own.
	r.deliver(r.message(chatID, "personal", "typing", func(act map[string]any) { act["type"] = "typing" }))
	r.deliver(r.message(chatID, "personal", "from a bot", func(act map[string]any) {
		act["from"] = map[string]any{"id": "28:other", "name": "Other", "role": "bot"}
	}))
	// Another Bot Service channel on the same endpoint.
	r.deliver(r.message(chatID, "personal", "web chat", func(act map[string]any) { act["channelId"] = "webchat" }))

	got := r.host.inbound()
	if len(got) != 1 {
		t.Fatalf("delivered %d messages, want 1: %+v", len(got), got)
	}
	in := got[0]
	if in.ExternalID != chatID || in.Text != "hello & welcome" || in.Author != "Alice" || in.AuthorID != "aad-alice" || in.ReplyTo != "" {
		t.Fatalf("inbound = %+v", in)
	}
}

func TestUnsignedRequestsAreRefused(t *testing.T) {
	r := newRig(t, chatID, nil)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	stranger := &fakeMicrosoft{t: t, key: other, srv: r.ms.srv}
	cases := map[string]string{
		"no token":       "",
		"not a bearer":   "Basic " + r.ms.sign(nil),
		"wrong audience": "Bearer " + r.ms.sign(func(c map[string]any) { c["aud"] = "someone-else" }),
		"wrong issuer":   "Bearer " + r.ms.sign(func(c map[string]any) { c["iss"] = "https://evil.example" }),
		"expired":        "Bearer " + r.ms.sign(func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }),
		"wrong service":  "Bearer " + r.ms.sign(func(c map[string]any) { c["serviceurl"] = "https://evil.example" }),
		"no service":     "Bearer " + r.ms.sign(func(c map[string]any) { delete(c, "serviceurl") }),
		"another's key":  "Bearer " + stranger.sign(nil),
		"not a token":    "Bearer nonsense",
	}
	for name, auth := range cases {
		if code := r.hook(auth, r.message(chatID, "personal", name, nil)); code != http.StatusUnauthorized {
			t.Errorf("%s: answered %d, want 401", name, code)
		}
	}
	if got := r.host.inbound(); len(got) != 0 {
		t.Fatalf("delivered from an unsigned request: %+v", got)
	}
	if strings.Contains(r.row().StateJSON, chatID) {
		t.Fatal("an unsigned request was remembered")
	}

	req := httptest.NewRequest(http.MethodGet, hookURL, nil)
	req.Header.Set("Authorization", "Bearer "+r.ms.sign(nil))
	rec := httptest.NewRecorder()
	r.a.Hook(rec, req, r.row(), r.cfg, r.host)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET answered %d, want 405", rec.Code)
	}
}

func TestServiceAddressMustBeHTTPS(t *testing.T) {
	r := newRig(t, chatID, nil)
	plain := "http://connector.example"
	act := r.message(chatID, "personal", "hi", func(act map[string]any) { act["serviceUrl"] = plain })
	code := r.hook("Bearer "+r.ms.sign(func(c map[string]any) { c["serviceurl"] = plain }), act)
	if code != http.StatusBadRequest || len(r.host.inbound()) != 0 {
		t.Fatalf("a plain http service address: answered %d, delivered %d", code, len(r.host.inbound()))
	}
}

func TestMentionGating(t *testing.T) {
	r := newRig(t, teamConv, nil)
	conv := teamConv + ";messageid=1700000000001"

	r.deliver(r.message(conv, "channel", "nobody asked", nil))
	r.deliver(r.message(conv, "channel", "<at>Bob</at> look", func(act map[string]any) { mention(act, "29:bob", "Bob") }))
	if got := r.host.inbound(); len(got) != 0 {
		t.Fatalf("answered without a mention: %+v", got)
	}

	r.deliver(r.message(conv, "channel", "<at>Silo</at> ask <at>Bob</at> &lt;now&gt;", func(act map[string]any) {
		act["entities"] = []map[string]any{
			{"type": "mention", "text": "<at>Silo</at>", "mentioned": map[string]any{"id": botID, "name": "Silo"}},
			{"type": "mention", "text": "<at>Bob</at>", "mentioned": map[string]any{"id": "29:bob", "name": "Bob"}},
		}
	}))
	got := r.host.inbound()
	if len(got) != 1 || got[0].Text != "ask @Bob <now>" {
		t.Fatalf("inbound = %+v", got)
	}
	if got[0].ExternalID != teamConv || got[0].ReplyTo != "1700000000001" {
		t.Fatalf("thread: external %q reply to %q", got[0].ExternalID, got[0].ReplyTo)
	}

	// A personal chat needs no mention; a channel set to every message neither.
	personal := newRig(t, chatID, nil)
	personal.deliver(personal.message(chatID, "personal", "plain", nil))
	if len(personal.host.inbound()) != 1 {
		t.Fatal("a personal chat was not answered")
	}
	always := newRig(t, teamConv, map[string]string{"reply_mode": "always"})
	always.deliver(always.message(conv, "channel", "nobody asked", nil))
	if len(always.host.inbound()) != 1 {
		t.Fatal("reply_mode always did not answer")
	}
}

func TestSendAnswersInTheThread(t *testing.T) {
	r := newRig(t, teamConv, map[string]string{"reply_mode": "always"})
	r.deliver(r.message(teamConv+";messageid=42", "channel", "question", nil))
	in := r.host.inbound()[0]

	ctx := context.Background()
	if err := r.a.Send(ctx, r.row(), r.cfg, channels.Outbound{ExternalID: in.ExternalID, ReplyTo: in.ReplyTo, Text: "**answer** @everyone"}); err != nil {
		t.Fatal(err)
	}
	if err := r.a.Send(ctx, r.row(), r.cfg, channels.Outbound{ExternalID: in.ExternalID, Text: "to the channel"}); err != nil {
		t.Fatal(err)
	}
	if err := r.a.Typing(ctx, r.row(), in.ExternalID, true); err != nil {
		t.Fatal(err)
	}
	if err := r.a.Typing(ctx, r.row(), in.ExternalID, false); err != nil {
		t.Fatal(err)
	}

	sent := r.ms.sent()
	if len(sent) != 3 {
		t.Fatalf("sent %d activities, want 3: %+v", len(sent), sent)
	}
	first := sent[0]
	if first.Conversation != teamConv+";messageid=42" || first.Auth != "Bearer bot-token" {
		t.Fatalf("first send = %+v", first)
	}
	if a := first.Activity; a.Type != "message" || a.TextFormat != "markdown" || a.Text != "**answer** @everyone" || a.From == nil || a.From.ID != botID {
		t.Fatalf("first activity = %+v", a)
	}
	// No entities are ever sent, so nothing written can notify anyone.
	raw, _ := json.Marshal(first.Activity)
	if strings.Contains(string(raw), "entities") {
		t.Fatalf("an activity carries entities: %s", raw)
	}
	if sent[1].Conversation != teamConv {
		t.Fatalf("a send with no thread went to %q", sent[1].Conversation)
	}
	if sent[2].Activity.Type != "typing" || sent[2].Conversation != teamConv+";messageid=42" {
		t.Fatalf("typing = %+v", sent[2])
	}
	// One token served them all.
	if r.ms.tokens != 1 {
		t.Fatalf("asked for %d tokens, want 1", r.ms.tokens)
	}
}

func TestLongTextIsChunked(t *testing.T) {
	r := newRig(t, chatID, nil)
	r.deliver(r.message(chatID, "personal", "hi", nil))
	long := strings.Repeat("word ", 2*maxMessageRunes/5+10)
	if err := r.a.Send(context.Background(), r.row(), r.cfg, channels.Outbound{ExternalID: chatID, Text: long}); err != nil {
		t.Fatal(err)
	}
	sent := r.ms.sent()
	if len(sent) < 2 {
		t.Fatalf("sent %d messages, want several", len(sent))
	}
	for _, p := range sent {
		if n := len([]rune(p.Activity.Text)); n > maxMessageRunes {
			t.Fatalf("a message of %d runes", n)
		}
	}
}

func TestConversationsAreRememberedAndPicked(t *testing.T) {
	r := newRig(t, "", nil)
	st, err := r.a.Action(context.Background(), r.row(), r.cfg, "list_conversations", nil)
	if err != nil || st.Kind != channels.StateSelect || len(st.Options) != 0 || !strings.Contains(st.Message, "Message the bot in Teams first") {
		t.Fatalf("empty picker = %+v, %v", st, err)
	}

	r.deliver(r.message(chatID, "personal", "hi", nil))
	r.deliver(r.message(teamConv+";messageid=7", "channel", "added", func(act map[string]any) {
		act["type"] = "conversationUpdate"
		act["channelData"] = map[string]any{"team": map[string]any{"name": "Platform"}, "channel": map[string]any{"name": "General"}}
	}))
	// A later message there names no team: the title stays.
	r.deliver(r.message(teamConv+";messageid=8", "channel", "later", nil))
	if got := r.host.inbound(); len(got) != 0 {
		t.Fatalf("delivered with no conversation picked: %+v", got)
	}

	var kept channels.State
	if err := json.Unmarshal([]byte(r.row().StateJSON), &kept); err != nil {
		t.Fatal(err)
	}
	var convs []conversation
	if err := json.Unmarshal([]byte(kept.Values[stateKey]), &convs); err != nil || len(convs) != 2 {
		t.Fatalf("state values = %v (%v)", kept.Values, err)
	}
	if convs[0].ID != chatID || convs[0].ServiceURL != r.ms.srv.URL || convs[0].Bot != botID {
		t.Fatalf("remembered = %+v", convs[0])
	}

	st, err = r.a.Action(context.Background(), r.row(), r.cfg, "list_conversations", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []channels.Option{{Value: chatID, Label: "Alice"}, {Value: teamConv, Label: "Platform / General"}}
	if !slices.Equal(st.Options, want) {
		t.Fatalf("options = %+v, want %+v", st.Options, want)
	}

	// After a restart the row is all there is: a new adapter still sends.
	fresh := New()
	fresh.client = r.ms.srv.Client()
	if err := fresh.Send(context.Background(), r.row(), r.cfg, channels.Outbound{ExternalID: chatID, Text: "back"}); err != nil {
		t.Fatal(err)
	}
	if sent := r.ms.sent(); len(sent) != 1 || sent[0].Conversation != chatID {
		t.Fatalf("sent after restart = %+v", sent)
	}
	if err := fresh.Send(context.Background(), r.row(), r.cfg, channels.Outbound{ExternalID: "a:never", Text: "x"}); err == nil {
		t.Fatal("sent to a conversation Teams never named")
	}
}

func TestConversationListIsBounded(t *testing.T) {
	a := New()
	for i := range maxConversations + 5 {
		id := "c" + string(rune('A'+i%26)) + strings.Repeat("x", i)
		a.remember("ch1", conversation{ID: id, ServiceURL: "https://s.example"}, "cA")
	}
	convs := a.conversations("ch1")
	if len(convs) != maxConversations {
		t.Fatalf("kept %d conversations, want %d", len(convs), maxConversations)
	}
	if convs[0].ID != "cA" {
		t.Fatalf("the bound conversation was dropped; first is %q", convs[0].ID)
	}
}

func TestFiles(t *testing.T) {
	r := newRig(t, chatID, nil)
	r.deliver(r.message(chatID, "personal", "hi", nil))
	ctx := context.Background()

	err := r.a.Send(ctx, r.row(), r.cfg, channels.Outbound{ExternalID: chatID, Text: "the report", Files: []channels.Attachment{
		{Name: "report.pdf", Mime: "application/pdf", Data: []byte("%PDF")},
	}})
	if err == nil || !strings.Contains(err.Error(), "report.pdf") {
		t.Fatalf("a PDF: err = %v", err)
	}
	err = r.a.Send(ctx, r.row(), r.cfg, channels.Outbound{ExternalID: chatID, Files: []channels.Attachment{
		{Name: "big.png", Mime: "image/png", Data: make([]byte, maxInlineImage+1)},
	}})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("a large picture: err = %v", err)
	}
	if sent := r.ms.sent(); len(sent) != 0 {
		t.Fatalf("something was sent with a refused file: %+v", sent)
	}

	err = r.a.Send(ctx, r.row(), r.cfg, channels.Outbound{ExternalID: chatID, Text: "a chart", Files: []channels.Attachment{
		{Name: "chart.png", Mime: "image/png", Data: []byte("png-bytes")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sent := r.ms.sent()
	if len(sent) != 1 || len(sent[0].Activity.Attachments) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	att := sent[0].Activity.Attachments[0]
	if att.ContentType != "image/png" || att.ContentURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("png-bytes")) {
		t.Fatalf("attachment = %+v", att)
	}
}

func TestInboundFiles(t *testing.T) {
	r := newRig(t, chatID, nil)
	r.ms.file = []byte("file-bytes")
	r.deliver(r.message(chatID, "personal", "see these", func(act map[string]any) {
		act["attachments"] = []map[string]any{
			{"contentType": "text/html", "content": "<p>see these</p>"},
			{"contentType": fileDownload, "name": "notes.txt", "content": map[string]any{
				"downloadUrl": r.ms.srv.URL + "/file?tempauth=abc", "fileType": "txt",
			}},
			{"contentType": "image/png", "contentUrl": r.ms.srv.URL + "/file", "name": "shot.png"},
			{"contentType": "image/png", "contentUrl": "http://plain.example/x.png"},
			{"contentType": fileDownload, "name": "bad.txt", "content": map[string]any{"downloadUrl": "http://plain.example/f"}},
		}
	}))
	got := r.host.inbound()
	if len(got) != 1 || len(got[0].Files) != 2 {
		t.Fatalf("inbound = %+v", got)
	}
	doc, shot := got[0].Files[0], got[0].Files[1]
	if doc.Name != "notes.txt" || !strings.HasPrefix(doc.Mime, "text/plain") || shot.Name != "shot.png" || shot.Mime != "image/png" {
		t.Fatalf("files = %+v", got[0].Files)
	}
	for _, f := range got[0].Files {
		data, err := f.Fetch(context.Background())
		if err != nil || string(data) != "file-bytes" {
			t.Fatalf("%s: %q, %v", f.Name, data, err)
		}
	}
	// The shared file's address carries its own permission; only the picture
	// the connector serves is asked for with the bot's token.
	if want := []string{"", "Bearer bot-token"}; !slices.Equal(r.ms.fileAuth, want) {
		t.Fatalf("downloads were authorised as %q, want %q", r.ms.fileAuth, want)
	}
}

func TestValidate(t *testing.T) {
	r := newRig(t, "", nil)
	ctx := context.Background()
	st, err := r.a.Validate(ctx, r.row(), r.cfg)
	if err != nil || st.Kind != channels.StateInfo || !strings.Contains(st.Message, hookURL) {
		t.Fatalf("validate = %+v, %v", st, err)
	}

	for _, hook := range []string{"", "http://silo.example.com/channels/hook/ch1"} {
		cfg := r.cfg
		cfg.HookURL = hook
		st, err := r.a.Validate(ctx, r.row(), cfg)
		if err == nil || st.Kind != channels.StateError || !strings.Contains(st.Message, "public_url") {
			t.Fatalf("hook %q: validate = %+v, %v", hook, st, err)
		}
	}

	bad := newRig(t, "", map[string]string{"app_password": "zz-not-it-zz"})
	st, err = bad.a.Validate(ctx, bad.row(), bad.cfg)
	if err == nil || st.Kind != channels.StateError || !strings.Contains(st.Message, "client secret") {
		t.Fatalf("a wrong secret: validate = %+v, %v", st, err)
	}
	if strings.Contains(st.Message, "zz-not-it-zz") {
		t.Fatalf("the secret is in the error: %s", st.Message)
	}

	missing := newRig(t, "", map[string]string{"tenant_id": ""})
	if _, err := missing.a.Validate(ctx, missing.row(), missing.cfg); err == nil || !strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("no tenant: err = %v", err)
	}
}

func TestStartShowsTheEndpointAndWaits(t *testing.T) {
	r := newRig(t, "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.a.Start(ctx, r.row(), r.cfg, r.host) }()
	deadline := time.After(5 * time.Second)
	for {
		r.host.mu.Lock()
		n := len(r.host.states)
		r.host.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Start published no state")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if st := r.host.states[0]; st.Kind != channels.StateInfo || !strings.Contains(st.Message, hookURL) {
		t.Fatalf("state = %+v", st)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Start ended with %v", err)
	}

	cfg := r.cfg
	cfg.HookURL = ""
	if err := r.a.Start(context.Background(), r.row(), cfg, r.host); !channels.IsPermanent(err) {
		t.Fatalf("Start with no public address: %v", err)
	}
}

func TestHistoryIsNotAvailable(t *testing.T) {
	if _, err := New().History(context.Background(), &db.Channel{}, channels.Config{}, chatID, 10); err == nil {
		t.Fatal("History answered")
	}
}

func TestBodyIsCapped(t *testing.T) {
	r := newRig(t, chatID, nil)
	req := httptest.NewRequest(http.MethodPost, hookURL, io.LimitReader(neverEnding('x'), maxBody+10))
	req.Header.Set("Authorization", "Bearer "+r.ms.sign(nil))
	rec := httptest.NewRecorder()
	r.a.Hook(rec, req, r.row(), r.cfg, r.host)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized body answered %d", rec.Code)
	}
}

type neverEnding byte

func (b neverEnding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
