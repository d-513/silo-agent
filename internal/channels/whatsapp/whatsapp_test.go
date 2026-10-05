package whatsapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

// --- fakes ---

type fakeHost struct {
	mu        sync.Mutex
	current   *db.Channel
	delivered []channels.Inbound
	states    []channels.State
	dsn       string
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
func (h *fakeHost) DatabaseURL() string                       { return h.dsn }

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

type sentMessage struct {
	To  types.JID
	ID  types.MessageID
	Msg *waE2E.Message
}

type upload struct {
	Kind whatsmeow.MediaType
	Size int
}

// fakeAPI records what the adapter asks WhatsApp to do.
type fakeAPI struct {
	mu        sync.Mutex
	sent      []sentMessage
	uploads   []upload
	presence  []types.ChatPresence
	reads     []types.MessageID
	groups    []*types.GroupInfo
	groupsErr error
	uploadErr error
	next      int
}

func (f *fakeAPI) SendMessage(_ context.Context, to types.JID, m *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var id types.MessageID
	if len(extra) > 0 {
		id = extra[0].ID
	}
	f.sent = append(f.sent, sentMessage{To: to, ID: id, Msg: m})
	return whatsmeow.SendResponse{ID: id}, nil
}

func (f *fakeAPI) Upload(_ context.Context, data []byte, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.uploadErr != nil {
		return whatsmeow.UploadResponse{}, f.uploadErr
	}
	f.uploads = append(f.uploads, upload{Kind: kind, Size: len(data)})
	return whatsmeow.UploadResponse{URL: "https://mmg.example/x", DirectPath: "/x", MediaKey: []byte("k"), FileLength: uint64(len(data))}, nil
}

func (f *fakeAPI) SendChatPresence(_ context.Context, _ types.JID, state types.ChatPresence, _ types.ChatPresenceMedia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presence = append(f.presence, state)
	return nil
}

func (f *fakeAPI) MarkRead(_ context.Context, ids []types.MessageID, _ time.Time, _, _ types.JID, _ ...types.ReceiptType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, ids...)
	return nil
}

func (f *fakeAPI) GetJoinedGroups(context.Context) ([]*types.GroupInfo, error) {
	return f.groups, f.groupsErr
}

func (f *fakeAPI) GenerateMessageID() types.MessageID {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	return types.MessageID(fmt.Sprintf("BOT%d", f.next))
}

type fakeDir struct {
	contacts map[types.JID]types.ContactInfo
	lids     map[types.JID]types.JID // lid -> phone number
}

func (d fakeDir) AllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	return d.contacts, nil
}

func (d fakeDir) PhoneForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if pn, ok := d.lids[lid]; ok {
		return pn, nil
	}
	return types.EmptyJID, errors.New("unknown")
}

var (
	me        = types.NewJID("48100200300", types.DefaultUserServer)
	meLID     = types.NewJID("9900990099", types.HiddenUserServer)
	alice     = types.NewJID("48600700800", types.DefaultUserServer)
	aliceLID  = types.NewJID("5511223344", types.HiddenUserServer)
	bob       = types.NewJID("48111222333", types.DefaultUserServer)
	crew      = types.NewJID("120363000000000001", types.GroupServer)
	startedAt = time.Now()
)

// newClient is a paired session: the account is "me", live since a moment ago.
func newClient(api *fakeAPI, dir directory) *client {
	c := &client{
		api: api, dir: dir,
		sent:      channels.NewSeen(64),
		liveSince: startedAt.Add(-time.Minute),
		failed:    make(chan error, 1),
		spawn:     func(f func()) { f() }, // run the ack inline so a test can see it
		names:     map[types.JID]string{},
	}
	c.setSelf(types.NewADJID(me.User, 0, 7), meLID)
	return c
}

func newChannel(external string) *db.Channel {
	return &db.Channel{ID: "ch1", BotID: "bot1", Adapter: "whatsapp", ExternalID: external, Enabled: true, Inbound: true}
}

var msgCounter int

// textFrom builds an inbound text message.
func textFrom(chat, sender types.JID, text string) *events.Message {
	msgCounter++
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: chat.Server == types.GroupServer},
			ID:            fmt.Sprintf("M%d", msgCounter),
			PushName:      "Alice A",
			Timestamp:     startedAt,
		},
		Message: &waE2E.Message{Conversation: proto.String(text)},
	}
}

func cfg(mode string) channels.Config {
	return channels.Config{Values: map[string]string{"reply_mode": mode}}
}

func deliver(t *testing.T, a *Adapter, c *client, h *fakeHost, target string, mode string, e *events.Message) {
	t.Helper()
	if err := a.onMessage(context.Background(), newChannel(target), cfg(mode), h, c, e); err != nil {
		t.Fatal(err)
	}
}

// --- descriptor and parsing ---

func TestDescriptor(t *testing.T) {
	d := New().Descriptor()
	if d.Slug != "whatsapp" || !d.RequiresTarget || d.Logo == "" || d.Guide == "" {
		t.Fatalf("descriptor: %+v", d)
	}
	for _, f := range d.Fields {
		if f.Required || f.Type == channels.FieldSecret {
			t.Fatalf("a QR-linked account has nothing to type in or hide: %+v", f)
		}
	}
	keys := []string{}
	for _, a := range d.Actions {
		keys = append(keys, a.Key)
	}
	for _, want := range []string{"link", "list_chats", "refresh"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("missing action %q in %v", want, keys)
		}
	}
}

func TestNormalizeTarget(t *testing.T) {
	for in, want := range map[string]string{
		"48600700800@s.whatsapp.net":      "48600700800@s.whatsapp.net",
		"48600700800:12@s.whatsapp.net":   "48600700800@s.whatsapp.net", // device suffix dropped
		"5511223344@lid":                  "5511223344@lid",
		"120363000000000001@g.us":         "120363000000000001@g.us",
		"+48 600 700 800":                 "48600700800@s.whatsapp.net",
		"(48) 600-700-800":                "48600700800@s.whatsapp.net",
		"48600700800":                     "48600700800@s.whatsapp.net",
		"https://wa.me/48600700800":       "48600700800@s.whatsapp.net",
		"https://wa.me/48600700800?text=": "48600700800@s.whatsapp.net",
		"":                                "",
		"alice":                           "",
		"12345":                           "", // too short for a phone number
		"1234567890123456":                "", // too long
		"x@example.com":                   "",
		"@s.whatsapp.net":                 "",
	} {
		got, ok := normalizeTarget(in)
		if want == "" {
			if ok {
				t.Errorf("normalizeTarget(%q) = %s, want rejection", in, got)
			}
			continue
		}
		if !ok || got.String() != want {
			t.Errorf("normalizeTarget(%q) = %s (%v), want %s", in, got, ok, want)
		}
	}
}

func TestMessageText(t *testing.T) {
	for name, tc := range map[string]struct {
		msg  *waE2E.Message
		want string
	}{
		"conversation":  {&waE2E.Message{Conversation: proto.String("  hi  ")}, "hi"},
		"extended text": {&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("see this")}}, "see this"},
		"image":         {&waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, "[image]"},
		"image caption": {&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("my cat")}}, "[image]\nmy cat"},
		"video caption": {&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String("clip")}}, "[video]\nclip"},
		"document":      {&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("q3.pdf"), Caption: proto.String("numbers")}}, "[document: q3.pdf]\nnumbers"},
		"voice note":    {&waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true)}}, "[voice message]"},
		"audio":         {&waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}, "[audio]"},
		"sticker":       {&waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}, "[sticker]"},
		"location":      {&waE2E.Message{LocationMessage: &waE2E.LocationMessage{DegreesLatitude: proto.Float64(52.2297), DegreesLongitude: proto.Float64(21.0122), Name: proto.String("Warsaw")}}, "[location: 52.22970, 21.01220]\nWarsaw"},
		"contact":       {&waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Bob")}}, "[contact: Bob]"},
		"reaction":      {&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}}, ""},
		"nothing":       {&waE2E.Message{}, ""},
	} {
		if got := messageText(tc.msg); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
	if messageText(nil) != "" {
		t.Error("nil message")
	}
}

// --- inbound ---

func TestDirectChatIsAnsweredAndAcknowledged(t *testing.T) {
	a, h, api := New(), &fakeHost{}, &fakeAPI{}
	c := newClient(api, fakeDir{})
	e := textFrom(alice, alice, "hello bot")
	deliver(t, a, c, h, alice.String(), "", e)

	got := h.inbound()
	if len(got) != 1 {
		t.Fatalf("delivered %d", len(got))
	}
	if got[0].ExternalID != alice.String() || got[0].Text != "hello bot" || got[0].Author != "Alice A" || got[0].Title != "Alice A" {
		t.Fatalf("inbound = %+v", got[0])
	}
	if !slices.Equal(api.reads, []types.MessageID{e.Info.ID}) || !slices.Equal(api.presence, []types.ChatPresence{types.ChatPresenceComposing}) {
		t.Fatalf("expected a read receipt and typing, got reads=%v presence=%v", api.reads, api.presence)
	}
}

func TestOtherChatsAndTheOwnersRepliesAreIgnored(t *testing.T) {
	a, h := New(), &fakeHost{}
	c := newClient(&fakeAPI{}, fakeDir{})
	deliver(t, a, c, h, alice.String(), "", textFrom(bob, bob, "wrong person"))
	deliver(t, a, c, h, alice.String(), "", textFrom(crew, bob, "wrong group"))

	// The owner answering Alice from their phone is not input for the Bot.
	mine := textFrom(alice, me, "I'll handle this one")
	mine.Info.IsFromMe = true
	deliver(t, a, c, h, alice.String(), "", mine)

	if got := h.inbound(); len(got) != 0 {
		t.Fatalf("delivered %+v", got)
	}
}

func TestMessageYourselfWorksButNeverAnswersItsOwnEcho(t *testing.T) {
	a, h, api := New(), &fakeHost{}, &fakeAPI{}
	c := newClient(api, fakeDir{})

	own := textFrom(me, me, "remind me at 5")
	own.Info.IsFromMe = true
	deliver(t, a, c, h, me.String(), "", own)
	if got := h.inbound(); len(got) != 1 || got[0].Title != "Notes to self" || got[0].Author != "You" {
		t.Fatalf("inbound = %+v", got)
	}
	if len(api.reads) != 0 {
		t.Fatal("marked the owner's own message as read")
	}

	// The Bot replies into the same chat; the reply comes back as an event.
	if err := c.send(context.Background(), me, channels.Outbound{Text: "ok, 5pm"}); err != nil {
		t.Fatal(err)
	}
	echo := textFrom(me, me, "ok, 5pm")
	echo.Info.IsFromMe = true
	echo.Info.ID = api.sent[0].ID
	deliver(t, a, c, h, me.String(), "", echo)
	if got := h.inbound(); len(got) != 1 {
		t.Fatalf("the bot answered itself: %+v", got)
	}
}

func TestSelfChatIsRecognisedUnderItsLID(t *testing.T) {
	a, h := New(), &fakeHost{}
	c := newClient(&fakeAPI{}, fakeDir{})
	own := textFrom(meLID, meLID, "note")
	own.Info.IsFromMe = true
	deliver(t, a, c, h, me.String(), "", own)
	if len(h.inbound()) != 0 {
		// no alt and no mapping: cannot be proven to be the same chat
		t.Fatal("matched a LID chat to a phone number with nothing linking them")
	}
	own2 := textFrom(meLID, meLID, "note two")
	own2.Info.IsFromMe = true
	c.dir = fakeDir{lids: map[types.JID]types.JID{meLID: me}}
	deliver(t, a, c, h, me.String(), "", own2)
	if len(h.inbound()) != 1 {
		t.Fatal("did not match the self chat through the LID mapping")
	}
}

func TestDirectChatsMatchAcrossPhoneNumberAndLID(t *testing.T) {
	for name, tc := range map[string]struct {
		dir  directory
		edit func(*events.Message)
		want int
	}{
		"alternate address sent along": {fakeDir{}, func(e *events.Message) { e.Info.SenderAlt = alice }, 1},
		"library's LID map":            {fakeDir{lids: map[types.JID]types.JID{aliceLID: alice}}, func(*events.Message) {}, 1},
		"nothing links them":           {fakeDir{}, func(*events.Message) {}, 0},
		"a different person's alt":     {fakeDir{}, func(e *events.Message) { e.Info.SenderAlt = bob }, 0},
	} {
		t.Run(name, func(t *testing.T) {
			a, h := New(), &fakeHost{}
			c := newClient(&fakeAPI{}, tc.dir)
			e := textFrom(aliceLID, aliceLID, "hi")
			tc.edit(e)
			deliver(t, a, c, h, alice.String(), "", e)
			if got := len(h.inbound()); got != tc.want {
				t.Fatalf("delivered %d, want %d", got, tc.want)
			}
			// Whatever the address, the conversation key stays the target's.
			if tc.want == 1 && h.inbound()[0].ExternalID != alice.String() {
				t.Fatalf("conversation key = %s", h.inbound()[0].ExternalID)
			}
		})
	}
}

func TestOwnMessageToSomeoneElseIsNotTakenForTheSelfChat(t *testing.T) {
	// I write to Alice (LID chat); WhatsApp tells us Alice's number as the
	// recipient alt. My own number must not be what gets compared.
	a, h := New(), &fakeHost{}
	c := newClient(&fakeAPI{}, fakeDir{})
	e := textFrom(aliceLID, me, "hey")
	e.Info.IsFromMe = true
	e.Info.SenderAlt = me
	e.Info.RecipientAlt = alice
	deliver(t, a, c, h, me.String(), "", e)
	if len(h.inbound()) != 0 {
		t.Fatal("a message to Alice was read as a note to self")
	}
}

func TestGroupsWaitForAMentionUnlessToldOtherwise(t *testing.T) {
	mention := func(jids ...string) *events.Message {
		e := textFrom(crew, bob, "@48100200300 are you there?")
		e.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String("@48100200300 are you there?"),
			ContextInfo: &waE2E.ContextInfo{MentionedJID: jids},
		}}
		return e
	}
	reply := textFrom(crew, bob, "yes please")
	reply.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String("yes please"),
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("X"), Participant: proto.String(me.String())},
	}}
	imageMention := textFrom(crew, bob, "")
	imageMention.Message = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:     proto.String("look @48100200300"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{me.String()}},
	}}

	for _, tc := range []struct {
		name string
		mode string
		e    *events.Message
		want int
	}{
		{"plain message, default mode", "", textFrom(crew, bob, "anyone?"), 0},
		{"plain message, mentions mode", "mentions", textFrom(crew, bob, "anyone?"), 0},
		{"plain message, always", "always", textFrom(crew, bob, "anyone?"), 1},
		{"mentioned by number", "mentions", mention(me.String()), 1},
		{"mentioned by LID", "mentions", mention(meLID.String()), 1},
		{"someone else mentioned", "mentions", mention(alice.String()), 0},
		{"reply to the bot", "mentions", reply, 1},
		{"mention on a photo", "mentions", imageMention, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, h := New(), &fakeHost{}
			c := newClient(&fakeAPI{groups: []*types.GroupInfo{{JID: crew, GroupName: types.GroupName{Name: "The Crew"}}}}, fakeDir{})
			deliver(t, a, c, h, crew.String(), tc.mode, tc.e)
			if got := len(h.inbound()); got != tc.want {
				t.Fatalf("delivered %d, want %d", got, tc.want)
			}
			if tc.want == 1 && h.inbound()[0].Title != "The Crew" {
				t.Fatalf("title = %q", h.inbound()[0].Title)
			}
		})
	}
}

func TestInboundNeverReplaysABacklogOrNoise(t *testing.T) {
	a, h := New(), &fakeHost{}
	c := newClient(&fakeAPI{}, fakeDir{})

	old := textFrom(alice, alice, "sent while we were offline")
	old.Info.Timestamp = c.liveSince.Add(-time.Hour)
	deliver(t, a, c, h, alice.String(), "", old)

	status := textFrom(types.StatusBroadcastJID, alice, "a status update")
	deliver(t, a, c, h, alice.String(), "", status)

	edit := textFrom(alice, alice, "typo fixed")
	edit.IsEdit = true
	deliver(t, a, c, h, alice.String(), "", edit)

	react := textFrom(alice, alice, "")
	react.Message = &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}}
	deliver(t, a, c, h, alice.String(), "", react)

	deliver(t, a, c, h, alice.String(), "", &events.Message{Info: textFrom(alice, alice, "").Info}) // no body at all
	deliver(t, a, c, h, "", "", textFrom(alice, alice, "hi"))                                       // no chat picked yet
	deliver(t, a, c, h, "not a chat", "", textFrom(alice, alice, "hi"))

	if got := h.inbound(); len(got) != 0 {
		t.Fatalf("delivered %+v", got)
	}
}

func TestInboundIgnoresARedelivery(t *testing.T) {
	a, h := New(), &fakeHost{}
	c := newClient(&fakeAPI{}, fakeDir{})
	e := textFrom(alice, alice, "once")
	deliver(t, a, c, h, alice.String(), "", e)
	deliver(t, a, c, h, alice.String(), "", e)
	if got := len(h.inbound()); got != 1 {
		t.Fatalf("ran %d times", got)
	}
}

func TestInboundFollowsAChatPickedAfterStart(t *testing.T) {
	a := New()
	h := &fakeHost{current: newChannel(alice.String())}
	c := newClient(&fakeAPI{}, fakeDir{})
	if err := a.onMessage(context.Background(), newChannel(""), cfg(""), h, c, textFrom(alice, alice, "hi")); err != nil {
		t.Fatal(err)
	}
	if len(h.inbound()) != 1 {
		t.Fatal("the freshly picked chat was ignored")
	}
}

func TestChatTitleFallsBackToTheSavedContactThenTheNumber(t *testing.T) {
	c := newClient(&fakeAPI{}, fakeDir{contacts: map[types.JID]types.ContactInfo{
		alice: {FullName: "Alice Appleseed", PushName: "al"},
	}})
	// A message the owner sent has no push name of the other side to use.
	sentByMe := textFrom(alice, me, "hello Alice")
	sentByMe.Info.PushName = ""
	sentByMe.Info.IsFromMe = true
	if got := c.title(context.Background(), alice, sentByMe.Info); got != "Alice Appleseed" {
		t.Fatalf("title = %q", got)
	}
	stranger := textFrom(bob, bob, "")
	stranger.Info.PushName = ""
	if got := c.title(context.Background(), bob, stranger.Info); got != "+48111222333" {
		t.Fatalf("title without any name = %q", got)
	}
}

// --- outbound ---

func TestSendConvertsMarkdownAndSplitsLongText(t *testing.T) {
	api := &fakeAPI{}
	c := newClient(api, fakeDir{})
	long := "## Plan\n\n" + strings.Repeat("**word** and more words. ", 400)
	if err := c.send(context.Background(), alice, channels.Outbound{Text: long}); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) < 2 {
		t.Fatalf("expected the text in several messages, got %d", len(api.sent))
	}
	for i, s := range api.sent {
		text := s.Msg.GetConversation()
		if s.To != alice || text == "" || len([]rune(text)) > maxMessageRunes {
			t.Fatalf("message %d: to=%s, %d runes", i, s.To, len([]rune(text)))
		}
		if strings.Contains(text, "**") || strings.Contains(text, "##") {
			t.Fatalf("message %d still has Markdown: %.80q", i, text)
		}
		if !c.sent.Has(s.ID) || s.ID == "" {
			t.Fatalf("message %d id %q was not remembered", i, s.ID)
		}
	}
	if !strings.HasPrefix(api.sent[0].Msg.GetConversation(), "*Plan*") {
		t.Fatalf("heading = %.40q", api.sent[0].Msg.GetConversation())
	}
	if !slices.Equal(api.presence, []types.ChatPresence{types.ChatPresencePaused}) {
		t.Fatalf("presence = %v", api.presence)
	}
}

func TestSendFilesAsTheirNaturalKind(t *testing.T) {
	api := &fakeAPI{}
	c := newClient(api, fakeDir{})
	png1 := []byte("\x89PNG\r\n\x1a\n-----")
	err := c.send(context.Background(), alice, channels.Outbound{Text: "here", Files: []channels.Attachment{
		{Name: "chart.png", Mime: "image/png", Data: png1},
		{Name: "clip.mp4", Mime: "video/mp4", Data: []byte("v")},
		{Name: "note.m4a", Mime: "audio/mp4", Data: []byte("a")},
		{Name: "report.pdf", Mime: "application/pdf", Data: []byte("p")},
		{Name: "anim.gif", Mime: "image/gif", Data: []byte("g")},
		{Name: "", Data: png1}, // no name, no mime: sniffed
		{Name: "empty.txt"},    // skipped
	}})
	if err != nil {
		t.Fatal(err)
	}
	kinds := []whatsmeow.MediaType{}
	for _, u := range api.uploads {
		kinds = append(kinds, u.Kind)
	}
	want := []whatsmeow.MediaType{whatsmeow.MediaImage, whatsmeow.MediaVideo, whatsmeow.MediaAudio, whatsmeow.MediaDocument, whatsmeow.MediaDocument, whatsmeow.MediaImage}
	if !slices.Equal(kinds, want) {
		t.Fatalf("upload kinds = %v, want %v", kinds, want)
	}
	if len(api.sent) != 1+len(want) {
		t.Fatalf("sent %d messages", len(api.sent))
	}
	if api.sent[1].Msg.GetImageMessage().GetMimetype() != "image/png" || api.sent[2].Msg.GetVideoMessage() == nil || api.sent[3].Msg.GetAudioMessage() == nil {
		t.Fatalf("wrong message types: %+v", api.sent)
	}
	doc := api.sent[4].Msg.GetDocumentMessage()
	if doc.GetFileName() != "report.pdf" || doc.GetMimetype() != "application/pdf" || doc.GetFileLength() != 1 {
		t.Fatalf("document = %+v", doc)
	}
	if api.sent[5].Msg.GetDocumentMessage().GetMimetype() != "image/gif" {
		t.Fatal("a GIF must go as a document: WhatsApp image messages are still pictures")
	}
}

func TestSendStopsOnAnUploadFailure(t *testing.T) {
	api := &fakeAPI{uploadErr: errors.New("413 too large")}
	c := newClient(api, fakeDir{})
	err := c.send(context.Background(), alice, channels.Outbound{Files: []channels.Attachment{{Name: "big.bin", Data: []byte("x")}}})
	if err == nil || !strings.Contains(err.Error(), "big.bin") || !strings.Contains(err.Error(), "413") {
		t.Fatalf("err = %v", err)
	}
	if len(api.sent) != 0 {
		t.Fatal("sent a message for a file that never uploaded")
	}
}

func TestSendNothingIsNotAnError(t *testing.T) {
	api := &fakeAPI{}
	c := newClient(api, fakeDir{})
	if err := c.send(context.Background(), alice, channels.Outbound{Text: "  "}); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) != 0 || len(api.presence) != 0 {
		t.Fatal("did something for an empty message")
	}
}

func TestAdapterSendResolvesTheTarget(t *testing.T) {
	a := New()
	if err := a.Send(context.Background(), newChannel(alice.String()), channels.Config{}, channels.Outbound{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("err = %v", err)
	}
	api := &fakeAPI{}
	r := newRun()
	r.setClient(newClient(api, fakeDir{}))
	a.runs["ch1"] = r

	if err := a.Send(context.Background(), newChannel("+48 600 700 800"), channels.Config{}, channels.Outbound{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), newChannel(alice.String()), channels.Config{}, channels.Outbound{Text: "to bob", ExternalID: bob.String()}); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) != 2 || api.sent[0].To != alice || api.sent[1].To != bob {
		t.Fatalf("sent = %+v", api.sent)
	}
	if err := a.Send(context.Background(), newChannel("alice"), channels.Config{}, channels.Outbound{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "not a WhatsApp chat") {
		t.Fatalf("err = %v", err)
	}
}

func TestHistoryIsNotAvailable(t *testing.T) {
	if _, err := New().History(context.Background(), newChannel(alice.String()), channels.Config{}, "", 10); err == nil {
		t.Fatal("expected the host to be told to use its own log")
	}
}

// --- setup actions ---

func TestListChatsOffersYouGroupsAndSavedContacts(t *testing.T) {
	api := &fakeAPI{groups: []*types.GroupInfo{
		{JID: crew, GroupName: types.GroupName{Name: "The Crew"}},
		{JID: types.NewJID("120363000000000002", types.GroupServer), GroupName: types.GroupName{Name: "Alumni"}},
	}}
	c := newClient(api, fakeDir{contacts: map[types.JID]types.ContactInfo{
		alice:    {FullName: "Alice Appleseed"},
		bob:      {FirstName: "Bob"},
		aliceLID: {FullName: "Alice (lid)"}, // not a phone number
		types.NewJID("48999000111", types.DefaultUserServer): {PushName: "stranger"}, // never saved
	}})
	st, err := c.listChats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, o := range st.Options {
		labels = append(labels, o.Label)
	}
	want := []string{"You (message yourself)", "Alice Appleseed · +48600700800", "Bob · +48111222333", "Group · Alumni", "Group · The Crew"}
	if st.Kind != channels.StateSelect || !slices.Equal(labels, want) {
		t.Fatalf("kind %s, options %q, want %q", st.Kind, labels, want)
	}
	if st.Options[0].Value != me.String() || st.Options[3].Value != "120363000000000002@g.us" {
		t.Fatalf("values = %+v", st.Options)
	}
	// Every offered value is something normalizeTarget accepts.
	for _, o := range st.Options {
		if _, ok := normalizeTarget(o.Value); !ok {
			t.Errorf("%q cannot be used as a target", o.Value)
		}
	}
}

func TestListChatsReportsAGroupFailure(t *testing.T) {
	c := newClient(&fakeAPI{groupsErr: errors.New("not connected")}, fakeDir{})
	st, err := c.listChats(context.Background())
	if err != nil || st.Kind != channels.StateError || !strings.Contains(st.Message, "not connected") {
		t.Fatalf("state = %+v, err = %v", st, err)
	}
}

func TestActions(t *testing.T) {
	ctx := context.Background()
	a := New()
	ch := newChannel("")
	if st, _ := a.Action(ctx, ch, channels.Config{}, "refresh", nil); st.Kind != channels.StateError {
		t.Fatalf("no session: %+v", st)
	}

	r := newRun()
	a.runs["ch1"] = r
	// Pairing: a QR is up, nobody is waiting for a relink, so Link device just shows it.
	r.last = channels.State{Kind: channels.StateQR, QR: "data:image/png;base64,AAAA"}
	pending := &client{failed: make(chan error, 1), names: map[types.JID]string{}}
	r.setClient(pending)
	if st, _ := a.Action(ctx, ch, channels.Config{}, "link", nil); st.Kind != channels.StateQR || st.QR == "" {
		t.Fatalf("link while a code shows = %+v", st)
	}
	if st, _ := a.Action(ctx, ch, channels.Config{}, "refresh", nil); st.Kind != channels.StateQR {
		t.Fatalf("refresh while pairing = %+v", st)
	}
	if st, _ := a.Action(ctx, ch, channels.Config{}, "list_chats", nil); st.Kind != channels.StateError || !strings.Contains(st.Message, "Link the device") {
		t.Fatalf("list before linking = %+v", st)
	}

	// Waiting for the owner: Link device wakes the pairing loop.
	r.last = channels.State{Kind: channels.StateError, Message: "The code expired"}
	woke := make(chan struct{})
	go func() { <-r.relink; close(woke) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, _ := a.Action(ctx, ch, channels.Config{}, "link", nil)
		if st.Kind == channels.StateAuth && strings.Contains(st.Message, "new code") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Link device never reached the waiting run")
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-woke

	// Linked and online.
	live := newClient(&fakeAPI{}, fakeDir{})
	r.setClient(live)
	if st, _ := a.Action(ctx, ch, channels.Config{}, "refresh", nil); st.Kind != channels.StateInfo || st.Message != "Connected as +48100200300" {
		t.Fatalf("refresh = %+v", st)
	}
	if st, _ := a.Action(ctx, ch, channels.Config{}, "link", nil); st.Kind != channels.StateInfo || !strings.Contains(st.Message, "already linked") {
		t.Fatalf("link when linked = %+v", st)
	}
	if st, _ := a.Action(ctx, ch, channels.Config{}, "list_chats", nil); st.Kind != channels.StateSelect {
		t.Fatalf("list = %+v", st)
	}
	if _, err := a.Action(ctx, ch, channels.Config{}, "nope", nil); err == nil {
		t.Fatal("unknown action accepted")
	}
}

// --- pairing ---

func TestConsumeQR(t *testing.T) {
	feed := func(items ...whatsmeow.QRChannelItem) <-chan whatsmeow.QRChannelItem {
		ch := make(chan whatsmeow.QRChannelItem, len(items))
		for _, it := range items {
			ch <- it
		}
		close(ch)
		return ch
	}
	code := func(s string) whatsmeow.QRChannelItem {
		return whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: s}
	}

	t.Run("codes then success", func(t *testing.T) {
		var states []channels.State
		err := consumeQR(context.Background(), feed(code("2@AAAA,BBBB,CCCC"), code("2@DDDD,EEEE,FFFF"), whatsmeow.QRChannelSuccess),
			func(st channels.State) { states = append(states, st) })
		if err != nil {
			t.Fatal(err)
		}
		if len(states) != 3 || states[0].Kind != channels.StateQR || states[1].Kind != channels.StateQR || states[2].Kind != channels.StateInfo {
			t.Fatalf("states = %+v", states)
		}
		if states[0].QR == states[1].QR {
			t.Fatal("a new code must draw a new image")
		}
		raw, ok := strings.CutPrefix(states[0].QR, "data:image/png;base64,")
		if !ok {
			t.Fatalf("not a PNG data URL: %.40s", states[0].QR)
		}
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil || img.Bounds().Dx() < 100 || img.Bounds().Dx() != img.Bounds().Dy() {
			t.Fatalf("QR image: %v %v", img.Bounds(), err)
		}
	})

	for name, tc := range map[string]struct {
		item whatsmeow.QRChannelItem
		want string
	}{
		"expired":       {whatsmeow.QRChannelTimeout, "expired"},
		"old phone":     {whatsmeow.QRChannelScannedWithoutMultidevice, "linked devices"},
		"outdated":      {whatsmeow.QRChannelClientOutdated, "update Silo"},
		"pairing error": {whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventError, Error: errors.New("boom")}, "boom"},
		"surprise":      {whatsmeow.QRChannelErrUnexpectedEvent, "pairing stopped"},
	} {
		t.Run(name, func(t *testing.T) {
			err := consumeQR(context.Background(), feed(tc.item), func(channels.State) {})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}

	t.Run("closed early", func(t *testing.T) {
		if err := consumeQR(context.Background(), feed(), func(channels.State) {}); err == nil {
			t.Fatal("a pairing that vanished was called a success")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := consumeQR(ctx, make(chan whatsmeow.QRChannelItem), func(channels.State) {}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
}

// --- events ---

func TestSessionEvents(t *testing.T) {
	a, h, r := New(), &fakeHost{}, newRun()
	c := newClient(&fakeAPI{}, fakeDir{})
	handle := func(evt any) {
		a.handleEvent(context.Background(), newChannel(alice.String()), cfg(""), h, r, c, evt)
	}

	// Pairing: who we became must be saved before anything else can fail.
	dev := types.NewADJID("48100200300", 0, 12)
	handle(&events.PairSuccess{ID: dev, LID: meLID})
	if got := h.lastState().Values["jid"]; got != dev.String() {
		t.Fatalf("jid published = %q", got)
	}
	if h.lastState().Kind != channels.StateNone {
		t.Fatal("saving the JID must not change the channel's status")
	}
	if !c.isSelf(me) || !c.isSelf(meLID) {
		t.Fatal("the account's own addresses were not learned")
	}

	handle(&events.Connected{})
	if st := h.lastState(); st.Kind != channels.StateInfo || st.Message != "Connected as +48100200300" || st.Values["jid"] != dev.String() {
		t.Fatalf("connected = %+v", st)
	}

	// A logout that the connect-failure also announces is reported once, as a logout.
	handle(&events.ConnectFailure{Reason: events.ConnectFailureLoggedOut})
	handle(&events.LoggedOut{})
	handle(&events.StreamReplaced{})
	if err := <-c.failed; !errors.Is(err, errLoggedOut) {
		t.Fatalf("first failure = %v", err)
	}
	select {
	case err := <-c.failed:
		t.Fatalf("a second failure leaked through: %v", err)
	default:
	}

	for name, tc := range map[string]struct {
		evt  any
		want string
	}{
		"replaced": {&events.StreamReplaced{}, "somewhere else"},
		"outdated": {&events.ClientOutdated{}, "update Silo"},
		"banned":   {&events.TemporaryBan{Code: events.TempBanSentToTooManyPeople, Expire: time.Hour}, "temporarily banned"},
		"refused":  {&events.ConnectFailure{Reason: events.ConnectFailureServiceUnavailable, Message: "busy"}, "refused the connection"},
	} {
		c := newClient(&fakeAPI{}, fakeDir{})
		a.handleEvent(context.Background(), newChannel(""), cfg(""), h, r, c, tc.evt)
		select {
		case err := <-c.failed:
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: %v", name, err)
			}
		default:
			t.Errorf("%s: no failure recorded", name)
		}
	}
}

func TestSessionPublishesNothingForAnUnhandledEvent(t *testing.T) {
	a, h, r := New(), &fakeHost{}, newRun()
	c := newClient(&fakeAPI{}, fakeDir{})
	a.handleEvent(context.Background(), newChannel(""), cfg(""), h, r, c, &events.Disconnected{})
	if len(h.states) != 0 || len(c.failed) != 0 {
		t.Fatal("reacted to a plain disconnect, which the library reconnects from by itself")
	}
}

func TestRemoveLogsTheDeviceOut(t *testing.T) {
	a, h, r := New(), &fakeHost{}, newRun()
	c := newClient(&fakeAPI{}, fakeDir{})
	called := 0
	c.logout = func(context.Context) error { called++; return nil }
	r.setClient(c)
	a.runs["ch1"] = r
	if err := a.Remove(context.Background(), newChannel(""), h); err != nil || called != 1 {
		t.Fatalf("err = %v, logout called %d times", err, called)
	}
	// Nothing running and nothing ever paired: nothing to undo, and no database needed.
	if err := New().Remove(context.Background(), newChannel(""), &fakeHost{}); err != nil {
		t.Fatal(err)
	}
}

// --- the session store, against the dev Postgres ---

// storeDSN is a connection string that lands in the test's own schema, as the
// Control Plane's database_url would land in its own.
func storeDSN(t *testing.T) string {
	t.Helper()
	gdb := dbtest.New(t)
	var schema string
	if err := gdb.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil || schema == "" {
		t.Fatalf("current_schema: %q %v", schema, err)
	}
	u, err := url.Parse(dbtest.URL())
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	return u.String()
}

func TestSessionSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	dsn := storeDSN(t)
	a := New()
	container, err := a.container(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Close() })

	if again, err := a.container(ctx, dsn); err != nil || again != container {
		t.Fatal("the store must be opened once and shared by every channel")
	}

	// A channel with no saved JID starts unpaired.
	fresh, err := loadDevice(ctx, container, "")
	if err != nil || fresh.ID != nil {
		t.Fatalf("fresh device = %+v, %v", fresh, err)
	}

	// Pairing saves the device; a later start finds the same keys by its JID.
	jid := types.NewADJID("48100200300", 0, 12)
	fresh.ID = &jid
	fresh.Account = &waAdv.ADVSignedDeviceIdentity{
		Details: []byte{1}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64),
	}
	if err := fresh.Save(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := json.Marshal(channels.State{Kind: channels.StateInfo, Values: map[string]string{"jid": jid.String()}})
	saved := &db.Channel{ID: "ch1", StateJSON: string(st)}
	if stateJID(saved) != jid.String() {
		t.Fatalf("stateJID = %q", stateJID(saved))
	}
	back, err := loadDevice(ctx, container, stateJID(saved))
	if err != nil || back.ID == nil || back.RegistrationID != fresh.RegistrationID {
		t.Fatalf("reloaded = %+v, %v", back, err)
	}

	// A JID that is not in the store (database reset) pairs from scratch instead of failing.
	other, err := loadDevice(ctx, container, types.NewADJID("48999999999", 0, 1).String())
	if err != nil || other.ID != nil {
		t.Fatalf("unknown device = %+v, %v", other, err)
	}

	// Deleting the channel drops the keys even when no session is running.
	h := &fakeHost{dsn: dsn}
	if err := a.Remove(ctx, saved, h); err != nil {
		t.Fatal(err)
	}
	gone, err := container.GetDevice(ctx, jid)
	if err != nil || gone != nil {
		t.Fatalf("the device survived its channel: %+v, %v", gone, err)
	}
}

func TestContainerNeedsADatabase(t *testing.T) {
	if _, err := New().container(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "database_url") {
		t.Fatalf("err = %v", err)
	}
}

func TestStateJID(t *testing.T) {
	for in, want := range map[string]string{
		"":                "",
		"not json":        "",
		`{"Kind":"info"}`: "",
		`{"Values":{"jid":"1:2@s.whatsapp.net"}}`: "1:2@s.whatsapp.net",
		`{"Values":{"jid":""}}`:                   "",
	} {
		if got := stateJID(&db.Channel{StateJSON: in}); got != want {
			t.Errorf("stateJID(%q) = %q, want %q", in, got, want)
		}
	}
}
