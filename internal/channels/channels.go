// Package channels is the adapter engine: built-in ways to talk to a Bot.
//
// An Adapter is a transport (Telegram, WhatsApp, …). The Control Plane owns
// every adapter: tokens and sessions stay here, never in the Bot. A channel
// row is one attached adapter instance. The engine is transport-agnostic; the
// host (internal/app) drives it and supplies inbound messages to the shared
// agent execution engine.
package channels

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/db"
)

// FieldType controls the generated form input in the Channels tab.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldSecret   FieldType = "secret"
	FieldToggle   FieldType = "toggle"
	FieldSelect   FieldType = "select"
	FieldNumber   FieldType = "number"
	FieldTextarea FieldType = "textarea"
)

// Option is one choice for a select field or a dynamic state picker.
type Option struct {
	Value string
	Label string
}

// Field is one declarative config input. Secrets are never returned to the UI;
// only a "set" marker is.
type Field struct {
	Key         string
	Label       string
	Description string
	Type        FieldType
	Required    bool
	Options     []Option
}

// ActionKind tells the Setup menu how to render an interactive adapter action.
type ActionKind string

const (
	// ActionPick lists choices (State.Options); picking one binds the target.
	ActionPick ActionKind = "pick"
	// ActionRun does something on demand and returns a State (e.g. refresh).
	ActionRun ActionKind = "run"
	// ActionQR asks the adapter to produce a QR code in State.QR.
	ActionQR ActionKind = "qr"
)

// Action is one interactive setup step an adapter exposes. The Setup menu is
// generated from these, so a new adapter needs no UI change.
type Action struct {
	Key         string
	Label       string
	Description string
	Kind        ActionKind
}

// Descriptor is the static, UI-facing shape of an adapter. Logo is an
// optional image URL (usually a small embedded data URL) for the picker.
// Guide is setup instructions (markdown) shown in the add/edit form; adapters
// keep theirs in a GUIDE.md beside the adapter and go:embed it.
type Descriptor struct {
	Slug        string
	Name        string
	Description string
	Logo        string
	Guide       string
	// RequiresTarget means a channel instance binds to exactly one
	// conversation picked in the UI (e.g. a Telegram chat).
	RequiresTarget bool
	// Actions drive the Setup menu.
	Actions []Action
	Fields  []Field
}

// StateKind tells the UI how to render the current dynamic state.
type StateKind string

const (
	StateNone   StateKind = ""
	StateInfo   StateKind = "info"
	StateAuth   StateKind = "auth"
	StateQR     StateKind = "qr"
	StateSelect StateKind = "select"
	StateError  StateKind = "error"
)

// State is adapter-owned dynamic status: connection detail, a QR code, or a
// picker (e.g. which Telegram chat to send to). It is how an adapter passes
// information the static form cannot express.
type State struct {
	Kind    StateKind
	Message string
	QR      string // data: URL, for StateQR
	Options []Option
	Values  map[string]string
}

// Config is a channel's resolved config, including decrypted secrets.
type Config struct {
	Values map[string]string
}

func (c Config) Get(key string) string {
	return c.Values[key]
}

func (c Config) Bool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(c.Values[key])) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Attachment is an outbound file (present/artifact on a channel run).
type Attachment struct {
	Name string
	Mime string
	Data []byte
}

// Inbound is one message received from the outside.
type Inbound struct {
	ExternalID string
	Title      string
	Author     string
	Text       string
	Raw        map[string]any
}

// Outbound is one message to deliver.
type Outbound struct {
	ExternalID string
	Text       string
	ReplyTo    string
	Files      []Attachment
}

// Message is one historical message from the platform, oldest first.
type Message struct {
	Author string
	Text   string
	At     time.Time
	Out    bool // sent by the Bot
}

// Host is implemented by the Control Plane app. Adapters call back into it;
// they never touch the DB or agent loop directly.
type Host interface {
	// GetSecret returns a Bot secret by name (same store as the Secrets tab).
	GetSecret(botID, name string) (string, error)
	// DeliverInbound hands a message to the agent execution engine. It may
	// inject into a live run for that conversation instead of starting one.
	DeliverInbound(ctx context.Context, ch *db.Channel, in Inbound) error
	// PublishState updates the dynamic state shown in the UI.
	PublishState(channelID string, st State)
	// CurrentChannel re-reads a channel row so a running adapter sees edits
	// (config, target) without a restart.
	CurrentChannel(id string) (*db.Channel, bool)
	// DataDir is the Control Plane's data directory, for adapter sessions.
	DataDir() string
}

// Adapter is one built-in transport.
type Adapter interface {
	Descriptor() Descriptor
	// Validate probes credentials/config and returns the initial state.
	Validate(ctx context.Context, ch *db.Channel, cfg Config) (State, error)
	// Start runs the inbound loop until ctx is done. It blocks; the host runs
	// it in its own goroutine.
	Start(ctx context.Context, ch *db.Channel, cfg Config, host Host) error
	// Send delivers one outbound message.
	Send(ctx context.Context, ch *db.Channel, cfg Config, msg Outbound) error
	// History reads the platform's own message history for a conversation, so
	// the Bot can see older messages than this process has observed. Return
	// oldest first.
	History(ctx context.Context, ch *db.Channel, cfg Config, externalID string, limit int) ([]Message, error)
	// Action handles a UI-initiated adapter action (list_dialogs, refresh, …).
	Action(ctx context.Context, ch *db.Channel, cfg Config, action string, payload map[string]string) (State, error)
}

var (
	regMu    sync.RWMutex
	registry = map[string]Adapter{}
)

// Register installs a built-in adapter. Called from adapter init().
func Register(a Adapter) {
	regMu.Lock()
	registry[a.Descriptor().Slug] = a
	regMu.Unlock()
}

// Lookup returns an adapter by slug.
func Lookup(slug string) (Adapter, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	a, ok := registry[slug]
	return a, ok
}

// Descriptors lists adapters for the add-channel picker.
func Descriptors() []Descriptor {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Descriptor, 0, len(registry))
	for _, a := range registry {
		out = append(out, a.Descriptor())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SecretName is the reserved Bot-secret name for a channel config field.
func SecretName(channelID, key string) string {
	return "channel." + channelID + "." + key
}

// IsSecretName reports whether a secret name belongs to a channel, so it can
// be hidden from the Secrets tab and the model-facing secrets action.
func IsSecretName(name string) bool {
	return strings.HasPrefix(name, "channel.")
}
