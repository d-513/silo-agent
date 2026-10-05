package discord

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// --- fakes ---

// fakeHost plays the Control Plane for an adapter.
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

// request is one REST call the adapter made.
type request struct {
	Method, Path, Query, ContentType string
	Body                             []byte
}

// transport answers Discord's REST API from a handler and records every call.
type transport struct {
	mu     sync.Mutex
	reqs   []request
	handle func(r request) (int, string)
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	r := request{Method: req.Method, Path: strings.TrimPrefix(req.URL.Path, "/api/v"+discordgo.APIVersion), Query: req.URL.RawQuery,
		ContentType: req.Header.Get("Content-Type"), Body: body}
	t.mu.Lock()
	t.reqs = append(t.reqs, r)
	t.mu.Unlock()
	status, payload := 404, `{"code":10003,"message":"Unknown Channel"}`
	if t.handle != nil {
		status, payload = t.handle(r)
	}
	return &http.Response{
		StatusCode: status, Request: req, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(payload)),
	}, nil
}

func (t *transport) calls() []request {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.reqs)
}

const okMessage = `{"id":"m1","channel_id":"c1","content":"ok"}`

// session is a discordgo session that never touches the network: REST goes to
// tr, and the state already knows who the bot is.
func session(t *testing.T, tr *transport) *discordgo.Session {
	t.Helper()
	s, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatal(err)
	}
	s.Client = &http.Client{Transport: tr}
	s.State.User = &discordgo.User{ID: "BOT", Username: "silo"}
	return s
}

func newChannel(external string) *db.Channel {
	return &db.Channel{ID: "ch1", BotID: "bot1", Adapter: "discord", ExternalID: external, Enabled: true, Inbound: true}
}

func user(id, name string) *discordgo.User { return &discordgo.User{ID: id, Username: name} }

// --- descriptor ---

func TestDescriptor(t *testing.T) {
	d := New().Descriptor()
	if d.Slug != "discord" || !d.RequiresTarget || d.Logo == "" || d.Guide == "" {
		t.Fatalf("descriptor: %+v", d)
	}
	var token *channels.Field
	for i := range d.Fields {
		if d.Fields[i].Key == "bot_token" {
			token = &d.Fields[i]
		}
	}
	if token == nil || token.Type != channels.FieldSecret || !token.Required {
		t.Fatalf("bot_token must be a required secret: %+v", token)
	}
	keys := []string{}
	for _, a := range d.Actions {
		keys = append(keys, a.Key)
	}
	for _, want := range []string{"list_channels", "invite", "refresh"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("missing action %q in %v", want, keys)
		}
	}
}

func TestValidateNeedsAToken(t *testing.T) {
	a := New()
	if _, err := a.Validate(context.Background(), nil, channels.Config{Values: map[string]string{}}); err == nil {
		t.Fatal("empty token accepted")
	}
	if _, err := a.Validate(context.Background(), nil, channels.Config{Values: map[string]string{"bot_token": " x "}}); err != nil {
		t.Fatal(err)
	}
}

// --- target ---

func TestTargetID(t *testing.T) {
	for in, want := range map[string]string{
		"123456789012345678":                                   "123456789012345678",
		"  123456789012345678 ":                                "123456789012345678",
		"<#123456789012345678>":                                "123456789012345678",
		"https://discord.com/channels/111111111/222222222":     "222222222",
		"https://discord.com/channels/111111111/222222222/333": "222222222",
		"https://discord.com/channels/@me/444444444":           "444444444",
		"https://discord.com/channels/111111111":               "",
		"https://example.com/channels/1/2222222":               "2222222",
		"general":                                              "",
		"@alice":                                               "",
		"":                                                     "",
	} {
		if got := targetID(in); got != want {
			t.Errorf("targetID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInviteURL(t *testing.T) {
	u := inviteURL("42")
	for _, want := range []string{"https://discord.com/oauth2/authorize?", "client_id=42", "scope=bot", "permissions="} {
		if !strings.Contains(u, want) {
			t.Errorf("%q missing from %s", want, u)
		}
	}
	// View Channel and Send Messages are the floor.
	if int64(invitePermissions)&(discordgo.PermissionViewChannel|discordgo.PermissionSendMessages) == 0 {
		t.Fatal("invite permissions lack view/send")
	}
}

func TestStartupErrorsAreActionable(t *testing.T) {
	if !isDisallowedIntent(errors.New("websocket: close 4014: Disallowed intent(s).")) || isDisallowedIntent(nil) {
		t.Fatal("4014 not recognised")
	}
	if got := friendlyOpen(errors.New("websocket: close 4004: Authentication failed.")).Error(); !strings.Contains(got, "token") {
		t.Fatalf("4004 message: %s", got)
	}
	if got := friendlyOpen(errors.New("dial tcp: no route")).Error(); !strings.Contains(got, "no route") {
		t.Fatalf("other errors must keep their cause: %s", got)
	}
}

// --- inbound ---

func guildMsg(id, text string, mentions ...*discordgo.User) *discordgo.Message {
	return &discordgo.Message{ID: id, ChannelID: "100100100100", GuildID: "G1", Content: text, Author: user("U1", "alice"), Mentions: mentions}
}

func TestInboundFilters(t *testing.T) {
	bot := user("BOT", "silo")
	reply := guildMsg("r1", "yes", nil...)
	reply.Type = discordgo.MessageTypeReply
	reply.ReferencedMessage = &discordgo.Message{ID: "p", Author: bot}

	botAuthor := guildMsg("b1", "hi", bot)
	botAuthor.Author = &discordgo.User{ID: "U2", Username: "otherbot", Bot: true}
	webhook := guildMsg("w1", "hi", bot)
	webhook.WebhookID = "hook"
	system := guildMsg("s1", "joined", bot)
	system.Type = discordgo.MessageTypeGuildMemberJoin
	own := guildMsg("o1", "my own words", bot)
	own.Author = bot
	elsewhere := guildMsg("e1", "hi <@BOT>", bot)
	elsewhere.ChannelID = "100200200200"
	empty := guildMsg("n1", "", bot)

	for _, tc := range []struct {
		name  string
		mode  string
		msg   *discordgo.Message
		wants int
	}{
		{"mention in mentions mode", "mentions", guildMsg("1", "hi <@BOT>", bot), 1},
		{"no mention in mentions mode", "mentions", guildMsg("2", "hello there"), 0},
		{"default mode needs a mention", "", guildMsg("3", "hello there"), 0},
		{"no mention in always mode", "always", guildMsg("4", "hello there"), 1},
		{"reply to the bot without a ping", "mentions", reply, 1},
		{"another bot", "always", botAuthor, 0},
		{"webhook", "always", webhook, 0},
		{"system message", "always", system, 0},
		{"the bot's own message", "always", own, 0},
		{"another channel", "always", elsewhere, 0},
		{"nothing to read", "always", empty, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, h := New(), &fakeHost{}
			cfg := channels.Config{Values: map[string]string{"reply_mode": tc.mode}}
			if err := a.onMessage(context.Background(), newChannel("100100100100"), cfg, h, session(t, &transport{}), tc.msg); err != nil {
				t.Fatal(err)
			}
			if got := len(h.inbound()); got != tc.wants {
				t.Fatalf("delivered %d, want %d", got, tc.wants)
			}
		})
	}
}

func TestInboundReadsTheMessage(t *testing.T) {
	a, h := New(), &fakeHost{}
	s := session(t, &transport{})
	s.State.GuildAdd(&discordgo.Guild{ID: "G1", Name: "My Server", Channels: []*discordgo.Channel{
		{ID: "100100100100", GuildID: "G1", Name: "general", Type: discordgo.ChannelTypeGuildText},
	}})
	m := guildMsg("1", "look <@BOT> at this", user("BOT", "silo"))
	m.Member = &discordgo.Member{Nick: "Ali"}
	m.Attachments = []*discordgo.MessageAttachment{{Filename: "plan.pdf", ContentType: "application/pdf", URL: "https://cdn.example/plan.pdf"}}
	if err := a.onMessage(context.Background(), newChannel("100100100100"), channels.Config{}, h, s, m); err != nil {
		t.Fatal(err)
	}
	got := h.inbound()
	if len(got) != 1 {
		t.Fatalf("delivered %d", len(got))
	}
	in := got[0]
	if in.ExternalID != "100100100100" || in.Author != "Ali" || in.Title != "#general · My Server" {
		t.Fatalf("inbound = %+v", in)
	}
	if in.Text != "look @silo at this\n[attachment: plan.pdf (application/pdf) https://cdn.example/plan.pdf]" {
		t.Fatalf("text = %q", in.Text)
	}
}

func TestInboundIgnoresARepeat(t *testing.T) {
	a, h := New(), &fakeHost{}
	cfg := channels.Config{Values: map[string]string{"reply_mode": "always"}}
	s := session(t, &transport{})
	for range 2 {
		if err := a.onMessage(context.Background(), newChannel("100100100100"), cfg, h, s, guildMsg("same", "hi")); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(h.inbound()); got != 1 {
		t.Fatalf("a redelivered message ran %d times", got)
	}
}

func TestInboundFollowsATargetPickedAfterStart(t *testing.T) {
	a := New()
	h := &fakeHost{current: newChannel("100100100100")}
	cfg := channels.Config{Values: map[string]string{"reply_mode": "always"}}
	// The adapter started before anyone picked a channel.
	if err := a.onMessage(context.Background(), newChannel(""), cfg, h, session(t, &transport{}), guildMsg("1", "hi")); err != nil {
		t.Fatal(err)
	}
	if len(h.inbound()) != 1 {
		t.Fatal("the freshly picked channel was ignored")
	}
}

func TestDMsAnswerWithoutAMentionAndBecomePickable(t *testing.T) {
	a, h := New(), &fakeHost{}
	dm := &discordgo.Message{ID: "d1", ChannelID: "200100100100", Content: "hello", Author: &discordgo.User{ID: "U1", Username: "alice", GlobalName: "Alice A"}}
	// Not bound to the DM yet: it is learned, not answered.
	if err := a.onMessage(context.Background(), newChannel("100100100100"), channels.Config{}, h, session(t, &transport{}), dm); err != nil {
		t.Fatal(err)
	}
	if len(h.inbound()) != 0 {
		t.Fatal("answered a DM the channel is not bound to")
	}
	if len(h.states) != 1 || h.states[0].Values["dm:200100100100"] != "DM · Alice A" {
		t.Fatalf("the DM was not published for persistence: %+v", h.states)
	}
	// Bound to it: answered with no mention, whatever the reply mode.
	dm2 := *dm
	dm2.ID = "d2"
	if err := a.onMessage(context.Background(), newChannel("200100100100"), channels.Config{}, h, session(t, &transport{}), &dm2); err != nil {
		t.Fatal(err)
	}
	got := h.inbound()
	if len(got) != 1 || got[0].Title != "DM · Alice A" {
		t.Fatalf("delivered %+v", got)
	}
	if len(h.states) != 1 {
		t.Fatal("a known DM was published again")
	}
}

func TestDMsSurviveARestart(t *testing.T) {
	a := New()
	a.putDM("ch1", "200100100100", "DM · alice")
	state, _ := json.Marshal(channels.State{Kind: channels.StateInfo, Values: a.dmState("ch1")})

	b := New()
	b.loadDMs(&db.Channel{ID: "ch1", StateJSON: string(state)})
	if got := b.dmState("ch1"); got["dm:200100100100"] != "DM · alice" {
		t.Fatalf("restored = %v", got)
	}
	b.loadDMs(&db.Channel{ID: "ch2", StateJSON: "not json"}) // must not panic
}

// --- outbound ---

func TestSendChunksAndNeverPingsEveryone(t *testing.T) {
	tr := &transport{handle: func(request) (int, string) { return 200, okMessage }}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})

	long := strings.Repeat("sentence number one. ", 250) // ~5000 runes
	err := a.Send(context.Background(), newChannel("111111111"), channels.Config{}, channels.Outbound{Text: long + " @everyone", ReplyTo: "777"})
	if err != nil {
		t.Fatal(err)
	}
	calls := tr.calls()
	if len(calls) < 3 {
		t.Fatalf("expected the text in several messages, got %d", len(calls))
	}
	for i, c := range calls {
		if c.Method != "POST" || c.Path != "/channels/111111111/messages" {
			t.Fatalf("call %d: %s %s", i, c.Method, c.Path)
		}
		var body struct {
			Content string `json:"content"`
			Allowed struct {
				Parse []string `json:"parse"`
			} `json:"allowed_mentions"`
			Reference *struct {
				MessageID string `json:"message_id"`
			} `json:"message_reference"`
		}
		if err := json.Unmarshal(c.Body, &body); err != nil {
			t.Fatal(err)
		}
		if n := utf8.RuneCountInString(body.Content); n == 0 || n > 2000 {
			t.Fatalf("call %d has %d runes", i, n)
		}
		if !slices.Equal(body.Allowed.Parse, []string{"users"}) {
			t.Fatalf("call %d allows %v: a model-written @everyone must not ping", i, body.Allowed.Parse)
		}
		if (i == 0) != (body.Reference != nil) {
			t.Fatalf("call %d: the reply reference belongs on the first message only", i)
		}
		if body.Reference != nil && body.Reference.MessageID != "777" {
			t.Fatalf("reply to %q", body.Reference.MessageID)
		}
	}
}

func TestSendAttachesFilesToTheLastMessage(t *testing.T) {
	tr := &transport{handle: func(request) (int, string) { return 200, okMessage }}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})

	var files []channels.Attachment
	for _, n := range []string{"a.txt", "b.txt", "", "d.txt"} {
		files = append(files, channels.Attachment{Name: n, Mime: "text/plain", Data: []byte("data-" + n)})
	}
	files = append(files, channels.Attachment{Name: "empty.txt"}) // skipped
	if err := a.Send(context.Background(), newChannel("111111111"), channels.Config{}, channels.Outbound{Text: "here you go", Files: files}); err != nil {
		t.Fatal(err)
	}
	calls := tr.calls()
	if len(calls) != 1 {
		t.Fatalf("got %d messages, want one carrying the text and files", len(calls))
	}
	mediaType, params, err := mime.ParseMediaType(calls[0].ContentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type %q", calls[0].ContentType)
	}
	mr := multipart.NewReader(strings.NewReader(string(calls[0].Body)), params["boundary"])
	var names []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FileName() != "" {
			names = append(names, p.FileName())
		}
	}
	if !slices.Equal(names, []string{"a.txt", "b.txt", "file", "d.txt"}) {
		t.Fatalf("uploaded %v", names)
	}
}

func TestSendNothingIsNotAnError(t *testing.T) {
	tr := &transport{handle: func(request) (int, string) { return 200, okMessage }}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})
	if err := a.Send(context.Background(), newChannel("111111111"), channels.Config{}, channels.Outbound{Text: "  "}); err != nil {
		t.Fatal(err)
	}
	if len(tr.calls()) != 0 {
		t.Fatal("sent an empty message")
	}
}

func TestSendNeedsAConnectionAndATarget(t *testing.T) {
	a := New()
	if err := a.Send(context.Background(), newChannel("111111111"), channels.Config{}, channels.Outbound{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("err = %v", err)
	}
	a.putConn("ch1", &conn{sess: session(t, &transport{})})
	if err := a.Send(context.Background(), newChannel("general"), channels.Config{}, channels.Outbound{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "channel id") {
		t.Fatalf("err = %v", err)
	}
	// The message's own conversation wins over the channel's target.
	tr := &transport{handle: func(request) (int, string) { return 200, okMessage }}
	a.putConn("ch1", &conn{sess: session(t, tr)})
	if err := a.Send(context.Background(), newChannel("111111111"), channels.Config{}, channels.Outbound{Text: "hi", ExternalID: "999999999"}); err != nil {
		t.Fatal(err)
	}
	if c := tr.calls(); len(c) != 1 || c[0].Path != "/channels/999999999/messages" {
		t.Fatalf("calls = %+v", c)
	}
}

func TestSendExplainsMissingPermissions(t *testing.T) {
	tr := &transport{handle: func(request) (int, string) {
		return 403, `{"code":50013,"message":"Missing Permissions"}`
	}}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})
	err := a.Send(context.Background(), newChannel("111111111"), channels.Config{}, channels.Outbound{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "Send Messages") {
		t.Fatalf("err = %v", err)
	}
}

// --- history ---

func TestHistoryIsOldestFirstAndMarksTheBot(t *testing.T) {
	tr := &transport{handle: func(r request) (int, string) {
		if r.Path != "/channels/111111111/messages" || !strings.Contains(r.Query, "limit=3") {
			t.Errorf("unexpected request %s?%s", r.Path, r.Query)
		}
		// Discord answers newest first.
		return 200, `[
			{"id":"3","channel_id":"c","content":"third","timestamp":"2026-01-01T10:02:00Z","author":{"id":"BOT","username":"silo"}},
			{"id":"2","channel_id":"c","content":"second","timestamp":"2026-01-01T10:01:00Z","author":{"id":"U1","username":"alice","global_name":"Alice"}},
			{"id":"1","channel_id":"c","content":"","timestamp":"2026-01-01T10:00:00Z","author":{"id":"U1","username":"alice"},
			 "attachments":[{"id":"a","filename":"x.png","content_type":"image/png","url":"https://cdn/x.png"}]}
		]`
	}}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})
	got, err := a.History(context.Background(), newChannel("111111111"), channels.Config{}, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d messages", len(got))
	}
	if !strings.HasPrefix(got[0].Text, "[attachment: x.png") || got[0].Author != "alice" || got[0].Out {
		t.Fatalf("oldest = %+v", got[0])
	}
	if got[1].Text != "second" || got[1].Author != "Alice" {
		t.Fatalf("middle = %+v", got[1])
	}
	if got[2].Text != "third" || got[2].Author != "Bot" || !got[2].Out {
		t.Fatalf("newest = %+v", got[2])
	}
	if !got[0].At.Before(got[2].At) || got[2].At != time.Date(2026, 1, 1, 10, 2, 0, 0, time.UTC) {
		t.Fatalf("timestamps = %v .. %v", got[0].At, got[2].At)
	}
}

func TestHistoryCapsTheLimit(t *testing.T) {
	tr := &transport{handle: func(r request) (int, string) {
		if !strings.Contains(r.Query, "limit=100") {
			t.Errorf("limit not capped: %s", r.Query)
		}
		return 200, `[]`
	}}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})
	if _, err := a.History(context.Background(), newChannel("111111111"), channels.Config{}, "", 5000); err != nil {
		t.Fatal(err)
	}
}

// --- setup actions ---

func TestListChannelsOffersWritableTextChannelsAndDMs(t *testing.T) {
	tr := &transport{handle: func(r request) (int, string) {
		switch r.Path {
		case "/users/@me/guilds":
			return 200, `[{"id":"G2","name":"Zed"},{"id":"G1","name":"Acme"}]`
		case "/guilds/G1/channels":
			return 200, `[
				{"id":"1","guild_id":"G1","name":"general","type":0},
				{"id":"2","guild_id":"G1","name":"voice","type":2},
				{"id":"3","guild_id":"G1","name":"Text","type":4},
				{"id":"4","guild_id":"G1","name":"news","type":5}]`
		case "/guilds/G2/channels":
			return 200, `[{"id":"5","guild_id":"G2","name":"lobby","type":0}]`
		}
		t.Errorf("unexpected request %s", r.Path)
		return 404, `{}`
	}}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})
	a.putDM("ch1", "200100100100", "DM · alice")

	st, err := a.Action(context.Background(), newChannel(""), channels.Config{}, "list_channels", nil)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, o := range st.Options {
		labels = append(labels, o.Label)
	}
	want := []string{"Acme › #general", "Acme › #news", "DM · alice", "Zed › #lobby"}
	if st.Kind != channels.StateSelect || !slices.Equal(labels, want) {
		t.Fatalf("state %s, options %v, want %v", st.Kind, labels, want)
	}
	if st.Values["dm:200100100100"] == "" {
		t.Fatal("known DMs must be re-published so they survive a restart")
	}
}

func TestListChannelsExplainsAnEmptyBot(t *testing.T) {
	tr := &transport{handle: func(request) (int, string) { return 200, `[]` }}
	a := New()
	a.putConn("ch1", &conn{sess: session(t, tr)})
	st, err := a.Action(context.Background(), newChannel(""), channels.Config{}, "list_channels", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != channels.StateSelect || len(st.Options) != 0 || !strings.Contains(st.Message, "Invite link") {
		t.Fatalf("state = %+v", st)
	}
}

func TestOtherActions(t *testing.T) {
	a := New()
	if st, _ := a.Action(context.Background(), newChannel(""), channels.Config{}, "refresh", nil); st.Kind != channels.StateError {
		t.Fatalf("disconnected refresh = %+v", st)
	}
	a.putConn("ch1", &conn{sess: session(t, &transport{}), limited: true})

	st, _ := a.Action(context.Background(), newChannel(""), channels.Config{}, "refresh", nil)
	if st.Kind != channels.StateInfo || !strings.Contains(st.Message, "Connected as @silo") || !strings.Contains(st.Message, "Message Content") {
		t.Fatalf("refresh = %+v", st)
	}
	st, _ = a.Action(context.Background(), newChannel(""), channels.Config{}, "invite", nil)
	if !strings.Contains(st.Message, "client_id=BOT") {
		t.Fatalf("invite = %+v", st)
	}
	if _, err := a.Action(context.Background(), newChannel(""), channels.Config{}, "nope", nil); err == nil {
		t.Fatal("unknown action accepted")
	}
}
