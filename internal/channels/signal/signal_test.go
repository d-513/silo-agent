package signal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	account = "+491700000001"
	alice   = "+491700000002"
	aliceID = "a11ce000-0000-4000-8000-000000000002"
	selfID  = "b0700000-0000-4000-8000-000000000001"
)

// groupID is a group's id as signal-cli gives it: 32 bytes of base64.
var groupID = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

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

func (h *fakeHost) sawState(kind channels.StateKind, part string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.ContainsFunc(h.states, func(s channels.State) bool {
		return s.Kind == kind && strings.Contains(s.Message, part)
	})
}

// call is one JSON-RPC request the adapter made.
type call struct {
	Method string
	Params map[string]any
}

// fakeDaemon is a signal-cli daemon: the check, the RPC answered from
// per-method handlers (a method with none answers an empty result), and an
// event stream served by the next of streams, one per connection.
type fakeDaemon struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	calls   []call
	handle  map[string]func(c call) (any, *rpcError)
	streams chan func(w io.Writer, flush func(), r *http.Request)
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	f := &fakeDaemon{
		t:       t,
		handle:  map[string]func(call) (any, *rpcError){},
		streams: make(chan func(io.Writer, func(), *http.Request), 8),
	}
	f.on("listAccounts", func(call) (any, *rpcError) {
		return []map[string]any{{"number": account, "aci": selfID}}, nil
	})
	f.on("listContacts", func(c call) (any, *rpcError) {
		all := []map[string]any{
			{"number": alice, "uuid": aliceID, "name": "Alice"},
			{"number": account, "uuid": selfID, "profile": map[string]any{"givenName": "Silo", "familyName": "Bot"}},
		}
		who, _ := c.Params["recipient"].([]any)
		if len(who) == 0 {
			return all, nil
		}
		var out []map[string]any
		for _, p := range all {
			if p["number"] == who[0] || p["uuid"] == who[0] {
				out = append(out, p)
			}
		}
		return out, nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/check", func(w http.ResponseWriter, _ *http.Request) {})
	mux.HandleFunc("/api/v1/rpc", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		c := call{Method: req.Method, Params: req.Params}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		h := f.handle[req.Method]
		f.mu.Unlock()
		ans := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		var result any = map[string]any{}
		var fail *rpcError
		if h != nil {
			result, fail = h(c)
		}
		if fail != nil {
			ans["error"] = fail
		} else {
			ans["result"] = result
		}
		_ = json.NewEncoder(w).Encode(ans)
	})
	mux.HandleFunc("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("account") != account {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flush := w.(http.Flusher).Flush
		flush()
		select {
		case serve := <-f.streams:
			serve(w, flush, r)
		case <-r.Context().Done():
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDaemon) on(method string, h func(c call) (any, *rpcError)) {
	f.mu.Lock()
	f.handle[method] = h
	f.mu.Unlock()
}

func (f *fakeDaemon) called(method string) []call {
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

func (f *fakeDaemon) config(mode string) channels.Config {
	return channels.Config{Values: map[string]string{"daemon_url": f.srv.URL + "/", "account": "+49 170 0000001", "reply_mode": mode}}
}

// conn is a session as Start makes one, for tests that feed events by hand.
func (f *fakeDaemon) conn(t *testing.T) *conn {
	d, err := newDaemon(f.config(""))
	if err != nil {
		t.Fatal(err)
	}
	return &conn{d: d, since: time.Now().Add(-queueSlack).UnixMilli(), people: map[string]person{}}
}

func channel(target string) *db.Channel {
	return &db.Channel{ID: "ch1", BotID: "bot1", Adapter: "signal", ExternalID: target}
}

func nowMS() int64 { return time.Now().UnixMilli() }

// received is one event's data as the daemon writes it.
func received(envelope map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"account": account, "envelope": envelope})
	return b
}

func fromAlice(ts int64, data map[string]any) map[string]any {
	return map[string]any{
		"source": alice, "sourceNumber": alice, "sourceUuid": aliceID, "sourceName": "Alice",
		"timestamp": ts, "dataMessage": data,
	}
}

func inGroup(data map[string]any) map[string]any {
	data["groupInfo"] = map[string]any{"groupId": groupID, "groupName": "Family", "type": "DELIVER"}
	return data
}

func sse(w io.Writer, flush func(), id string, data []byte) {
	fmt.Fprintf(w, "id:%s\nevent:receive\ndata:%s\n\n", id, data)
	flush()
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

// feed hands one event to the adapter as the read loop would.
func feed(t *testing.T, a *Adapter, f *fakeDaemon, h *fakeHost, c *conn, mode string, env map[string]any) {
	t.Helper()
	if err := a.onEvent(context.Background(), h.current, f.config(mode), h, c, event{Name: "receive", Data: received(env)}); err != nil {
		t.Fatal(err)
	}
}

// --- tests ---

func TestStartReadsTheStreamAndDeliversOnlyTheBoundConversation(t *testing.T) {
	f := newFakeDaemon(t)
	ts := nowMS()
	f.streams <- func(w io.Writer, flush func(), r *http.Request) {
		fmt.Fprint(w, ":\n\n") // a keep-alive
		// The account's own send from another device.
		sse(w, flush, "1", received(map[string]any{"sourceNumber": account, "timestamp": ts, "syncMessage": map[string]any{
			"sentMessage": map[string]any{"destinationNumber": alice, "message": "from my phone", "timestamp": ts}}}))
		sse(w, flush, "2", received(map[string]any{"sourceNumber": alice, "timestamp": ts, "receiptMessage": map[string]any{"isRead": true, "timestamps": []int64{1}}}))
		sse(w, flush, "3", received(map[string]any{"sourceNumber": alice, "timestamp": ts, "typingMessage": map[string]any{"action": "STARTED"}}))
		// Queued before the start.
		sse(w, flush, "4", received(fromAlice(ts-time.Hour.Milliseconds(), map[string]any{"message": "old"})))
		// Another conversation: a group, and another person.
		sse(w, flush, "5", received(fromAlice(ts+1, inGroup(map[string]any{"message": "in the group"}))))
		sse(w, flush, "6", received(map[string]any{"sourceNumber": "+491700000009", "sourceUuid": "c0000000-0000-4000-8000-000000000009", "timestamp": ts + 2, "dataMessage": map[string]any{"message": "a stranger"}}))
		sse(w, flush, "7", received(fromAlice(ts+3, map[string]any{"message": "hello Bot"})))
		// The same message again, as a reconnect replays it.
		sse(w, flush, "8", received(fromAlice(ts+3, map[string]any{"message": "hello Bot"})))
		<-r.Context().Done()
	}
	ch := channel("+49 170 0000002")
	h := &fakeHost{current: ch}
	a := New()
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan error, 1)
	go func() { ended <- a.Start(ctx, ch, f.config("mentions"), h) }()

	waitFor(t, "the read receipt", func() bool { return len(f.called("sendReceipt")) == 1 })
	got := h.inbound()
	if len(got) != 1 {
		t.Fatalf("delivered %+v, want one message", got)
	}
	want := channels.Inbound{ExternalID: alice, Title: "Signal · Alice", Author: "Alice", AuthorID: alice, Text: "hello Bot"}
	if g := got[0]; g.ExternalID != want.ExternalID || g.Title != want.Title || g.Author != want.Author || g.AuthorID != want.AuthorID || g.Text != want.Text {
		t.Fatalf("delivered %+v, want %+v", g, want)
	}
	rc := f.called("sendReceipt")[0].Params
	if rc["recipient"] != alice || rc["type"] != "read" || fmt.Sprint(rc["targetTimestamp"]) != fmt.Sprint([]any{float64(ts + 3)}) || rc["account"] != account {
		t.Fatalf("receipt %+v", rc)
	}
	if !h.sawState(channels.StateInfo, "Connected as "+account) {
		t.Fatalf("states %+v", h.states)
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
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not stop")
	}
	if a.conn(ch.ID) != nil {
		t.Fatal("session kept after stop")
	}
}

func TestStartReconnectsWhenTheStreamDrops(t *testing.T) {
	f := newFakeDaemon(t)
	ts := nowMS()
	f.streams <- func(w io.Writer, flush func(), _ *http.Request) {
		sse(w, flush, "41", received(fromAlice(ts, map[string]any{"message": "one"})))
		// returning closes the stream under the adapter
	}
	resumed := make(chan string, 1)
	f.streams <- func(w io.Writer, flush func(), r *http.Request) {
		resumed <- r.Header.Get("Last-Event-ID")
		sse(w, flush, "42", received(fromAlice(ts+1, map[string]any{"message": "two"})))
		<-r.Context().Done()
	}
	ch := channel(alice)
	h := &fakeHost{current: ch}
	a := New()
	a.retryMin, a.retryMax = 10*time.Millisecond, 20*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- a.Start(ctx, ch, f.config(""), h) }()

	waitFor(t, "both messages", func() bool { return len(h.inbound()) == 2 })
	if got := h.inbound(); got[0].Text != "one" || got[1].Text != "two" {
		t.Fatalf("delivered %+v", got)
	}
	if id := <-resumed; id != "41" {
		t.Fatalf("reconnected with Last-Event-ID %q, want 41", id)
	}
	if !h.sawState(channels.StateError, "trying again") {
		t.Fatalf("the drop was not shown: %+v", h.states)
	}
	select {
	case err := <-ended:
		t.Fatalf("a dropped stream ended Start: %v", err)
	default:
	}
}

func TestStartStopsOnAnAccountTheDaemonLacks(t *testing.T) {
	f := newFakeDaemon(t)
	f.on("listAccounts", func(call) (any, *rpcError) { return []map[string]any{{"number": "+15550001111"}}, nil })
	ch := channel(alice)
	h := &fakeHost{current: ch}
	err := New().Start(context.Background(), ch, f.config(""), h)
	if !channels.IsPermanent(err) || !strings.Contains(err.Error(), "has no account "+account) {
		t.Fatalf("Start ended with %v", err)
	}
	if !h.sawState(channels.StateError, "has no account") {
		t.Fatalf("states %+v", h.states)
	}

	// A single-account daemon cannot list accounts; what it receives says
	// which number it runs.
	f = newFakeDaemon(t)
	f.on("listAccounts", func(call) (any, *rpcError) {
		return nil, &rpcError{Code: codeMethodNotFound, Message: "Method not implemented"}
	})
	f.streams <- func(w io.Writer, flush func(), r *http.Request) {
		b, _ := json.Marshal(map[string]any{"account": "+15550001111", "envelope": fromAlice(nowMS(), map[string]any{"message": "hi"})})
		sse(w, flush, "1", b)
		<-r.Context().Done()
	}
	h = &fakeHost{current: ch}
	err = New().Start(context.Background(), ch, f.config(""), h)
	if !channels.IsPermanent(err) || !strings.Contains(err.Error(), "runs the account +15550001111") {
		t.Fatalf("Start ended with %v", err)
	}
	if len(h.inbound()) != 0 {
		t.Fatalf("delivered %+v from another account", h.inbound())
	}
}

func TestValidateChecksTheDaemonAndTheAccount(t *testing.T) {
	f := newFakeDaemon(t)
	st, err := New().Validate(context.Background(), channel(""), f.config(""))
	if err != nil || st.Kind != channels.StateInfo || st.Message != "Connected as "+account {
		t.Fatalf("state %+v, err %v", st, err)
	}

	f.on("listAccounts", func(call) (any, *rpcError) {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Specified account does not exist"}
	})
	if _, err = New().Validate(context.Background(), channel(""), f.config("")); err == nil || !strings.Contains(err.Error(), "has no account") {
		t.Fatalf("unknown account: %v", err)
	}

	f.srv.Close()
	if _, err = New().Validate(context.Background(), channel(""), f.config("")); err == nil || !strings.Contains(err.Error(), "cannot reach the signal-cli daemon") {
		t.Fatalf("daemon down: %v", err)
	}

	for _, cfg := range []map[string]string{
		{"daemon_url": "", "account": account},
		{"daemon_url": "ftp://x", "account": account},
		{"daemon_url": "http://127.0.0.1:1", "account": "alice"},
		{"daemon_url": "http://127.0.0.1:1", "account": ""},
	} {
		if _, err := New().Validate(context.Background(), channel(""), channels.Config{Values: cfg}); err == nil {
			t.Fatalf("%v passed", cfg)
		}
	}
}

func TestGroupAnswersMentionsAndADirectChatAlways(t *testing.T) {
	f := newFakeDaemon(t)
	a := New()
	c := f.conn(t)
	ts := nowMS()

	group := &fakeHost{current: channel("group:" + groupID)}
	feed(t, a, f, group, c, "mentions", fromAlice(ts, inGroup(map[string]any{"message": "just chatting"})))
	feed(t, a, f, group, c, "mentions", fromAlice(ts+1, inGroup(map[string]any{
		"message":  "￼ and ￼, lunch?",
		"mentions": []map[string]any{{"uuid": selfID, "start": 0, "length": 1}, {"number": alice, "uuid": aliceID, "start": 6, "length": 1}},
	})))
	// Someone else is mentioned, not the account.
	feed(t, a, f, group, c, "mentions", fromAlice(ts+2, inGroup(map[string]any{
		"message": "￼?", "mentions": []map[string]any{{"number": alice, "uuid": aliceID, "start": 0, "length": 1}},
	})))
	// A change to the group is not a message.
	feed(t, a, f, group, c, "always", fromAlice(ts+3, map[string]any{"message": "", "groupInfo": map[string]any{"groupId": groupID, "type": "UPDATE"}}))
	// A direct message is another conversation.
	feed(t, a, f, group, c, "always", fromAlice(ts+4, map[string]any{"message": "psst"}))
	got := group.inbound()
	if len(got) != 1 || got[0].Text != "@Silo Bot and @Alice, lunch?" || got[0].ExternalID != "group:"+groupID || got[0].Title != "Group · Family" {
		t.Fatalf("group delivered %+v", got)
	}

	// The same group, typed as a bare id, answering everything.
	always := &fakeHost{current: channel(groupID)}
	feed(t, a, f, always, c, "always", fromAlice(ts+5, inGroup(map[string]any{"message": "just chatting"})))
	if got := always.inbound(); len(got) != 1 || got[0].Text != "just chatting" {
		t.Fatalf("always delivered %+v", got)
	}

	// A direct chat needs no mention; bound by uuid, the sender named by
	// number alone is still them.
	direct := &fakeHost{current: channel(strings.ToUpper(aliceID))}
	feed(t, a, f, direct, c, "mentions", map[string]any{"sourceNumber": alice, "sourceName": "Alice", "timestamp": ts + 6, "dataMessage": map[string]any{"message": "hi"}})
	feed(t, a, f, direct, c, "mentions", map[string]any{"sourceNumber": "+491700000009", "timestamp": ts + 7, "dataMessage": map[string]any{"message": "not her"}})
	if got := direct.inbound(); len(got) != 1 || got[0].Text != "hi" || got[0].ExternalID != aliceID || got[0].AuthorID != alice {
		t.Fatalf("direct delivered %+v", got)
	}
}

func TestAttachmentsAreFetchedThroughTheDaemon(t *testing.T) {
	f := newFakeDaemon(t)
	f.on("getAttachment", func(c call) (any, *rpcError) {
		if c.Params["id"] != "att1.ogg" {
			return nil, &rpcError{Code: -1, Message: "Could not find attachment with ID: " + fmt.Sprint(c.Params["id"])}
		}
		return map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("OggS-voice"))}, nil
	})
	a := New()
	c := f.conn(t)
	h := &fakeHost{current: channel(alice)}
	feed(t, a, f, h, c, "", fromAlice(nowMS(), map[string]any{
		"message": nil,
		"attachments": []map[string]any{
			{"id": "att1.ogg", "contentType": "audio/ogg; codecs=opus", "filename": nil, "size": 10, "isVoiceNote": true},
			{"id": "att2.pdf", "contentType": "application/pdf", "filename": "plan.pdf", "size": 2048, "isVoiceNote": false},
		},
	}))
	got := h.inbound()
	if len(got) != 1 || len(got[0].Files) != 2 || got[0].Text != "" {
		t.Fatalf("delivered %+v", got)
	}
	voice, doc := got[0].Files[0], got[0].Files[1]
	if !voice.Voice || voice.Name != "voice-message.ogg" || voice.Mime != "audio/ogg" || voice.Size != 10 {
		t.Fatalf("voice %+v", voice)
	}
	if doc.Voice || doc.Name != "plan.pdf" || doc.Mime != "application/pdf" {
		t.Fatalf("doc %+v", doc)
	}
	if len(f.called("getAttachment")) != 0 {
		t.Fatal("fetched before anyone asked")
	}
	data, err := voice.Fetch(context.Background())
	if err != nil || string(data) != "OggS-voice" {
		t.Fatalf("fetch: %q, %v", data, err)
	}
	if p := f.called("getAttachment")[0].Params; p["id"] != "att1.ogg" || p["recipient"] != alice || p["account"] != account {
		t.Fatalf("getAttachment %+v", p)
	}
	if _, err := doc.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "Could not find attachment") {
		t.Fatalf("a missing attachment: %v", err)
	}
}

func TestSendToAGroupAndToAPersonWithAFile(t *testing.T) {
	f := newFakeDaemon(t)
	a := New()

	err := a.Send(context.Background(), channel("group:"+groupID), f.config(""), channels.Outbound{Text: "**Done.** See `plan.pdf`"})
	if err != nil {
		t.Fatal(err)
	}
	p := f.called("send")[0].Params
	if fmt.Sprint(p["groupId"]) != fmt.Sprint([]any{groupID}) || p["recipient"] != nil || p["message"] != "Done. See plan.pdf" || p["account"] != account {
		t.Fatalf("group send %+v", p)
	}
	if fmt.Sprint(p["textStyle"]) != fmt.Sprint([]any{"0:5:BOLD", "10:8:MONOSPACE"}) || p["attachments"] != nil {
		t.Fatalf("group send styles %+v", p)
	}

	long := strings.Repeat("word ", 700) // two messages
	err = a.Send(context.Background(), channel(alice), f.config(""), channels.Outbound{
		Text:  long,
		Files: []channels.Attachment{{Name: "my plan; v2.pdf", Mime: "application/pdf", Data: []byte("%PDF")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sends := f.called("send")[1:]
	if len(sends) != 2 {
		t.Fatalf("%d sends, want the text in two", len(sends))
	}
	if sends[0].Params["attachments"] != nil || fmt.Sprint(sends[0].Params["recipient"]) != fmt.Sprint([]any{alice}) || sends[0].Params["groupId"] != nil {
		t.Fatalf("first send %+v", sends[0].Params)
	}
	want := "data:application/pdf;filename=my_plan__v2.pdf;base64," + base64.StdEncoding.EncodeToString([]byte("%PDF"))
	if fmt.Sprint(sends[1].Params["attachments"]) != fmt.Sprint([]any{want}) {
		t.Fatalf("attachments %v", sends[1].Params["attachments"])
	}

	// A file alone goes without a message.
	before := len(f.called("send"))
	if err := a.Send(context.Background(), channel(alice), f.config(""), channels.Outbound{Files: []channels.Attachment{{Name: "a.png", Data: []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	only := f.called("send")[before].Params
	if _, has := only["message"]; has || !strings.HasPrefix(fmt.Sprint(only["attachments"]), "[data:application/octet-stream;filename=a.png;base64,") {
		t.Fatalf("file-only send %+v", only)
	}

	f.on("send", func(call) (any, *rpcError) { return nil, &rpcError{Code: -1, Message: "Unregistered user"} })
	if err := a.Send(context.Background(), channel(alice), f.config(""), channels.Outbound{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "Unregistered user") {
		t.Fatalf("a refusal: %v", err)
	}
	if err := a.Send(context.Background(), channel("nobody"), f.config(""), channels.Outbound{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "pick it again") {
		t.Fatalf("a bad target: %v", err)
	}
}

func TestTypingStartsAndStops(t *testing.T) {
	f := newFakeDaemon(t)
	a := New()
	ch := channel("group:" + groupID)
	if err := a.Typing(context.Background(), ch, ch.ExternalID, true); err == nil {
		t.Fatal("typing without a session")
	}
	a.putConn(ch.ID, f.conn(t))
	if err := a.Typing(context.Background(), ch, ch.ExternalID, true); err != nil {
		t.Fatal(err)
	}
	if err := a.Typing(context.Background(), ch, alice, false); err != nil {
		t.Fatal(err)
	}
	got := f.called("sendTyping")
	if fmt.Sprint(got[0].Params["groupId"]) != fmt.Sprint([]any{groupID}) || got[0].Params["stop"] != nil {
		t.Fatalf("on %+v", got[0].Params)
	}
	if fmt.Sprint(got[1].Params["recipient"]) != fmt.Sprint([]any{alice}) || got[1].Params["stop"] != true {
		t.Fatalf("off %+v", got[1].Params)
	}
}

func TestPickerListsGroupsAndContacts(t *testing.T) {
	f := newFakeDaemon(t)
	f.on("listGroups", func(call) (any, *rpcError) {
		return []map[string]any{
			{"id": groupID, "name": "Family", "isMember": true},
			{"id": "bGVmdA==", "name": "Left long ago", "isMember": false},
		}, nil
	})
	st, err := New().Action(context.Background(), channel(""), f.config(""), "list_chats", nil)
	if err != nil || st.Kind != channels.StateSelect {
		t.Fatalf("state %+v, err %v", st, err)
	}
	want := []channels.Option{{Value: "group:" + groupID, Label: "Group · Family"}, {Value: alice, Label: "Alice · " + alice}}
	if !slices.Equal(st.Options, want) {
		t.Fatalf("options %+v, want %+v", st.Options, want)
	}

	st, err = New().Action(context.Background(), channel(""), f.config(""), "refresh", nil)
	if err != nil || st.Kind != channels.StateInfo || st.Message != "Connected as "+account {
		t.Fatalf("refresh %+v, %v", st, err)
	}
	if _, err := New().History(context.Background(), channel(alice), f.config(""), alice, 10); err == nil {
		t.Fatal("history answered")
	}
}

func TestParseTargetReadsWhatTheOwnerTypes(t *testing.T) {
	cases := map[string]string{
		"group:" + groupID:       "group:" + groupID,
		"GROUP: " + groupID:      "group:" + groupID,
		groupID:                  "group:" + groupID,
		"+49 170 000-0002":       alice,
		"0049 (170) 0000002":     alice,
		strings.ToUpper(aliceID): aliceID,
		"491700000002":           "",
		"alice":                  "",
		"group:":                 "",
		"":                       "",
	}
	for in, want := range cases {
		got, ok := parseTarget(in)
		if ok != (want != "") || (ok && got.String() != want) {
			t.Errorf("parseTarget(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestFormatWritesSignalStyles(t *testing.T) {
	cases := []struct {
		in, text string
		styles   []string
	}{
		{"plain", "plain", nil},
		{"**bold** and *it* and _it_ and ~~gone~~ and ||secret||", "bold and it and it and gone and secret",
			[]string{"0:4:BOLD", "9:2:ITALIC", "16:2:ITALIC", "23:4:STRIKETHROUGH", "32:6:SPOILER"}},
		{"snake_case_name and 2 * 3 * 4", "snake_case_name and 2 * 3 * 4", nil},
		{"# Title\n- one\n- **two**", "Title\n• one\n• two", []string{"0:5:BOLD", "14:3:BOLD"}},
		{"see [the docs](https://x.test/a) or [https://y.test](https://y.test)", "see the docs (https://x.test/a) or https://y.test", nil},
		{"run:\n```sh\nls -la\n```\ndone", "run:\nls -la\ndone", []string{"5:6:MONOSPACE"}},
		// Ranges count UTF-16 units: the emoji is two.
		{"😀 **b**", "😀 b", []string{"3:1:BOLD"}},
		{`\*not\* it`, "*not* it", nil},
	}
	for _, c := range cases {
		text, styles := format(c.in)
		if text != c.text || !slices.Equal(styles, c.styles) {
			t.Errorf("format(%q) = %q %v; want %q %v", c.in, text, styles, c.text, c.styles)
		}
	}
}
