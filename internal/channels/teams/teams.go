// Package teams is the built-in Microsoft Teams adapter: a bot registered with
// the Azure Bot Service, spoken to over the Bot Framework REST connector.
//
// Teams delivers by calling us. Every activity is a POST to the channel's
// webhook carrying a token signed by the Bot Framework for this bot's app id,
// and nothing in it is believed before that token is (auth.go, hook.go). The
// bot answers by posting to the service address the activity named, with a
// token it gets from Microsoft for its app id and client secret (send.go).
// Both secrets stay on the Control Plane.
//
// A channel is bound to exactly one Teams conversation: a personal chat, a
// group chat or a team's channel. A bot cannot list its conversations, so the
// ones it has been written in are remembered in the channel's state, which is
// also where the service address a later send needs comes from. Inbound is
// live-only, as Teams delivers it.
package teams

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// Microsoft's addresses, variables so tests can stand in for them.
var (
	// openIDMetadataURL names where the Bot Framework publishes the keys it
	// signs activities with.
	openIDMetadataURL = "https://login.botframework.com/v1/.well-known/openidconfiguration"
	// tokenIssuer is who must have issued the token on an activity.
	tokenIssuer = "https://api.botframework.com"
	// loginURL is where the bot gets its own token: {loginURL}/{tenant}/oauth2/v2.0/token.
	loginURL = "https://login.microsoftonline.com"
)

const (
	// connectorScope is the Bot Framework connector, for the bot's own token.
	connectorScope = "https://api.botframework.com/.default"
	// teamsChannel is the Bot Service channel id of Teams. A bot resource can
	// have other channels switched on (Web Chat, the emulator), which arrive
	// at the same endpoint and are not this adapter's.
	teamsChannel = "msteams"
	// stateKey holds the conversations seen, in the channel's durable state.
	stateKey = "conversations"
	// maxConversations bounds that list; the oldest go first.
	maxConversations = 200
	// maxBody caps an activity. Teams keeps a message well under this.
	maxBody = 1 << 20
	// maxMessageRunes keeps a message clear of Teams' size limit, which counts
	// the whole activity in bytes.
	maxMessageRunes = 4000
	// maxInlineImage is the largest picture sent inside a message. Teams takes
	// a data: address only for small ones, and it counts toward the message.
	maxInlineImage = 256 << 10
	// maxDownload is a safety net under the host's own cap on what it saves.
	maxDownload = 256 << 20
	// callTimeout bounds one call to Microsoft that carries no file.
	callTimeout = 30 * time.Second
	// keysFor is how long the signing keys are kept: Microsoft asks for a
	// fresh copy at least once a day.
	keysFor = 24 * time.Hour
)

//go:embed teams.svg
var teamsLogo []byte

//go:embed GUIDE.md
var teamsGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(teamsLogo)
}

// conversation is one Teams conversation the bot was written in.
type conversation struct {
	ID string `json:"id"`
	// ServiceURL is where the connector serves it, taken from a signed
	// activity only: a send goes there with the bot's token.
	ServiceURL string `json:"service_url"`
	Title      string `json:"title"`
	// Kind is Teams' conversationType: personal, groupChat or channel.
	Kind string `json:"kind,omitempty"`
	// Bot is what Teams calls the bot there, the sender of its messages.
	Bot string `json:"bot,omitempty"`
}

// label is what the picker calls a conversation. Teams often names none.
func (c conversation) label() string {
	switch {
	case c.Title != "":
		return c.Title
	case c.Kind == "channel":
		return "A team channel"
	case c.Kind == "personal":
		return "A personal chat"
	}
	return "A group chat"
}

// credentials are what the bot gets its own token with.
type credentials struct {
	appID, secret, tenant string
}

func credsOf(cfg channels.Config) credentials {
	return credentials{
		appID:  strings.TrimSpace(cfg.Get("app_id")),
		secret: strings.TrimSpace(cfg.Get("app_password")),
		tenant: strings.TrimSpace(cfg.Get("tenant_id")),
	}
}

func (c credentials) check() error {
	switch {
	case c.appID == "":
		return errors.New("app_id is required: the Microsoft App ID of the Azure Bot")
	case c.secret == "":
		return errors.New("app_password is required: a client secret of the bot's app registration")
	case c.tenant == "":
		return errors.New("tenant_id is required: the bot's App Tenant ID")
	}
	return nil
}

// Adapter holds what it has learned about each channel's conversations and
// the tokens it was given. There is no connection to hold.
type Adapter struct {
	// client makes every call to Microsoft; tests swap it.
	client *http.Client

	mu      sync.Mutex
	convs   map[string][]conversation // channelID -> conversations seen, oldest first
	loaded  map[string]bool           // channelID -> convs read from the row
	creds   map[string]credentials    // channelID -> the newest config seen (Typing gets none)
	threads map[string]string         // channelID/conversation -> thread of the newest message handed on

	tokMu  sync.Mutex
	tokens map[string]token // channelID -> the bot's own token

	keyMu  sync.Mutex
	keys   oidc.KeySet
	keysAt time.Time

	seen *channels.Seen // channelID/activity handed on (Teams retries a slow answer)
}

func init() { channels.Register(New()) }

// New makes an Adapter that knows nothing yet.
func New() *Adapter {
	return &Adapter{
		client:  &http.Client{},
		convs:   map[string][]conversation{},
		loaded:  map[string]bool{},
		creds:   map[string]credentials{},
		threads: map[string]string{},
		tokens:  map[string]token{},
		seen:    channels.NewSeen(4096),
	}
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "teams",
		Name:           "Microsoft Teams",
		Description:    "A Teams bot through the Azure Bot Service.",
		Logo:           logoURL(),
		Guide:          teamsGuide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_conversations", Label: "Pick a conversation", Kind: channels.ActionPick, Description: "Choose the chat or team channel this channel talks to, from those the bot was written in."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Check the credentials and show the messaging endpoint again."},
		},
		Fields: []channels.Field{
			{Key: "app_id", Label: "Microsoft App ID", Type: channels.FieldText, Required: true, Description: "The Azure Bot's Microsoft App ID (Configuration)."},
			{Key: "app_password", Label: "Client secret", Type: channels.FieldSecret, Required: true, Description: "A client secret's Value from the app registration (Certificates & secrets), not its Secret ID."},
			{Key: "tenant_id", Label: "Tenant ID", Type: channels.FieldText, Required: true, Description: "The Azure Bot's App Tenant ID (Configuration)."},
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "mentions", Label: "Only when mentioned"},
				{Value: "always", Label: "Every message"},
			}},
		},
	}
}

// checkHook says why Teams cannot reach this channel, or nil.
func checkHook(cfg channels.Config) error {
	u, err := url.Parse(cfg.HookURL)
	if cfg.HookURL == "" || err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("Teams delivers messages by calling Silo, so Silo needs a public https address: set public_url to one the internet can reach")
	}
	return nil
}

func endpointState(cfg channels.Config) channels.State {
	return channels.State{
		Kind:    channels.StateInfo,
		Message: "Messaging endpoint: " + cfg.HookURL + " — paste it into the Azure Bot's Configuration.",
	}
}

func failed(err error) (channels.State, error) {
	return channels.State{Kind: channels.StateError, Message: err.Error()}, err
}

func (a *Adapter) Validate(ctx context.Context, ch *db.Channel, cfg channels.Config) (channels.State, error) {
	c := credsOf(cfg)
	if err := c.check(); err != nil {
		return failed(err)
	}
	if _, err := a.token(ctx, ch.ID, c); err != nil {
		return failed(err)
	}
	if err := checkHook(cfg); err != nil {
		return failed(err)
	}
	return endpointState(cfg), nil
}

// Start holds nothing open: Teams calls Hook. It says where, and waits.
func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	fail := func(err error) error {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return channels.Permanent(err)
	}
	c := credsOf(cfg)
	if err := c.check(); err != nil {
		return fail(err)
	}
	if err := checkHook(cfg); err != nil {
		return fail(err)
	}
	a.keep(ch, c)
	host.PublishState(ch.ID, endpointState(cfg))
	<-ctx.Done()
	return ctx.Err()
}

// History: the connector serves no past messages (reading them is Microsoft
// Graph, with permissions of its own), so the Bot reads its own run log.
func (a *Adapter) History(context.Context, *db.Channel, channels.Config, string, int) ([]channels.Message, error) {
	return nil, errors.New("history is not available over Microsoft Teams")
}

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, cfg channels.Config, action string, _ map[string]string) (channels.State, error) {
	switch action {
	case "refresh":
		st, _ := a.Validate(ctx, ch, cfg)
		return st, nil
	case "list_conversations":
		a.keep(ch, credsOf(cfg))
		convs := a.conversations(ch.ID)
		if len(convs) == 0 {
			return channels.State{
				Kind:    channels.StateSelect,
				Message: "No conversations yet. Message the bot in Teams first, then pick the conversation here.",
			}, nil
		}
		options := make([]channels.Option, 0, len(convs))
		for _, c := range convs {
			options = append(options, channels.Option{Value: c.ID, Label: c.label()})
		}
		slices.SortFunc(options, func(x, y channels.Option) int {
			return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
		})
		return channels.State{
			Kind:    channels.StateSelect,
			Message: fmt.Sprintf("%d conversations", len(options)),
			Options: options,
		}, nil
	}
	return channels.State{}, fmt.Errorf("unknown action %q", action)
}

// --- what the adapter remembers ---

// keep notes a channel's newest credentials and, once per process, reads the
// conversations an earlier one saved in its row.
func (a *Adapter) keep(ch *db.Channel, c credentials) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.creds[ch.ID] = c
	if a.loaded[ch.ID] {
		return
	}
	a.loaded[ch.ID] = true
	var st channels.State
	if json.Unmarshal([]byte(ch.StateJSON), &st) != nil {
		return
	}
	var saved []conversation
	if json.Unmarshal([]byte(st.Values[stateKey]), &saved) != nil {
		return
	}
	for _, c := range saved {
		if c.ID != "" && isHTTPS(c.ServiceURL) {
			a.convs[ch.ID] = append(a.convs[ch.ID], c)
		}
	}
}

func (a *Adapter) conversations(channelID string) []conversation {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.convs[channelID])
}

func (a *Adapter) conversation(channelID, id string) (conversation, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.convs[channelID] {
		if c.ID == id {
			return c, true
		}
	}
	return conversation{}, false
}

// remember files a conversation and answers the state values to persist when
// that changed anything. bound is the channel's own conversation, which is
// never the one dropped to make room.
func (a *Adapter) remember(channelID string, c conversation, bound string) map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := a.convs[channelID]
	at := slices.IndexFunc(list, func(o conversation) bool { return o.ID == c.ID })
	switch {
	case at < 0:
		list = append(list, c)
		for i := 0; len(list) > maxConversations && i < len(list); {
			if list[i].ID == bound || list[i].ID == c.ID {
				i++
				continue
			}
			list = slices.Delete(list, i, i+1)
		}
	case list[at] == c:
		return nil
	default:
		// An activity that names less than an earlier one did (Teams gives a
		// team's name when the bot is added, rarely on a message) keeps it.
		if len(c.Title) < len(list[at].Title) && c.Kind != "personal" || c.Title == "" {
			c.Title = list[at].Title
		}
		if c.Bot == "" {
			c.Bot = list[at].Bot
		}
		if list[at] == c {
			return nil
		}
		list[at] = c
	}
	a.convs[channelID] = list
	b, err := json.Marshal(list)
	if err != nil {
		return nil
	}
	return map[string]string{stateKey: string(b)}
}

func (a *Adapter) setThread(channelID, conv, root string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if root == "" {
		delete(a.threads, channelID+"/"+conv)
		return
	}
	a.threads[channelID+"/"+conv] = root
}

func (a *Adapter) thread(channelID, conv string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.threads[channelID+"/"+conv]
}

func (a *Adapter) credsFor(channelID string) (credentials, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.creds[channelID]
	return c, ok
}

func isHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// splitConversation takes a conversation id apart. In a team's channel Teams
// names the thread in the id itself, as ";messageid=<root message>".
func splitConversation(id string) (conv, root string) {
	conv, root, _ = strings.Cut(id, ";messageid=")
	return conv, root
}
