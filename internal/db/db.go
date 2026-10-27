package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type User struct {
	ID    string `gorm:"primaryKey"`
	Email string `gorm:"uniqueIndex"`
	// EmailSelfSet marks an email the user typed in themselves and nobody
	// checked. Single sign-on never matches an account by such an address:
	// anyone could claim a colleague's and wait for them to sign in.
	EmailSelfSet bool `gorm:"not null;default:false"`
	PasswordHash string
	Admin        bool
	// Disabled blocks sign-in and pauses the user's Bots; nothing is deleted.
	// (The defaults below fill the rows of users from before the column.)
	Disabled bool `gorm:"not null;default:false"`
	// Language is the display language of the console for this user; empty (a
	// user who never picked) reads as the default: lang.UI.
	Language string `gorm:"not null;default:''"`
	// OIDCIssuer and OIDCSubject name the identity at the OIDC provider this
	// user signs in with; both empty for a password-only account.
	OIDCIssuer  string `gorm:"column:oidc_issuer"`
	OIDCSubject string `gorm:"column:oidc_subject"`
	// TOTPSecret is the base32 secret of an enrolled authenticator; TOTPPending
	// is one being enrolled, not yet confirmed with a code. TOTPStep is the last
	// time step a code was accepted for, so a code cannot be used twice.
	TOTPSecret   string `gorm:"column:totp_secret"`
	TOTPPending  string `gorm:"column:totp_pending"`
	TOTPStep     int64  `gorm:"column:totp_step;not null;default:0"`
	LastSignInAt *time.Time
	CreatedAt    time.Time
}

// Session is one signed-in browser. ID is the hash of the cookie's value, so
// the table alone signs nobody in.
type Session struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index"`
	ExpiresAt time.Time
	CreatedAt time.Time
	// LastSeenAt is refreshed at most every few minutes.
	LastSeenAt time.Time
	UserAgent  string
	IP         string `gorm:"column:ip"`
	// Method is how the session was made: password, oidc, or invite.
	Method string
}

// Invite is a single-use link an admin hands out. With no UserID it creates the
// account for Email; with one it lets that user pick a new password. ID is the
// hash of the link's token.
type Invite struct {
	ID        string `gorm:"primaryKey"`
	Email     string `gorm:"index"`
	Admin     bool
	UserID    string `gorm:"index"`
	CreatedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// RecoveryCode is one two-factor backup code, stored as the hash of the user id
// and the code.
type RecoveryCode struct {
	ID     string `gorm:"primaryKey"`
	UserID string `gorm:"index"`
	UsedAt *time.Time
}

// Space is a Workspace: a container of Bots, the people who use them, and the
// connectors, skills and drives they share. The code says space because
// "workspace" is the Bot's /workspace; the UI says Workspace.
type Space struct {
	ID   string `gorm:"primaryKey"`
	Name string
	// Mark is the one or two letters on the Workspace's tile.
	Mark      string
	CreatedAt time.Time
}

// SpaceMember is one person in a Workspace. Role is owner, admin, member or
// guest; a Workspace has exactly one owner (the space_owner index).
type SpaceMember struct {
	SpaceID   string `gorm:"primaryKey"`
	UserID    string `gorm:"primaryKey;index"`
	Role      string `gorm:"not null"`
	InvitedBy string `gorm:"not null;default:''"`
	CreatedAt time.Time
}

// SpaceInvite is a link that joins a Workspace with a role. ID is the hash
// of the link's token; a link is single use and lasts 7 days.
type SpaceInvite struct {
	ID        string `gorm:"primaryKey"`
	SpaceID   string `gorm:"not null;index"`
	Role      string `gorm:"not null"`
	CreatedBy string `gorm:"not null;default:''"`
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	UsedBy    string `gorm:"not null;default:''"`
}

// BotAccess is one person on a Bot: Role manage or use. A Bot always has a
// manager. FeedReadAt and DashboardSeenAt are that person's read state.
type BotAccess struct {
	BotID           string `gorm:"primaryKey"`
	UserID          string `gorm:"primaryKey;index"`
	Role            string `gorm:"not null"`
	FeedReadAt      *time.Time
	DashboardSeenAt *time.Time
	CreatedAt       time.Time
}

// TableName keeps the plan's name; GORM would say bot_accesses.
func (BotAccess) TableName() string { return "bot_access" }

// SpaceSkill records who installed one of a Workspace's skills. The body is
// on disk (data/skills/spaces/<space>/<name>); which Bots use it is
// bot_skills.
type SpaceSkill struct {
	SpaceID     string `gorm:"primaryKey"`
	Name        string `gorm:"primaryKey"`
	InstalledBy string `gorm:"not null;default:''"`
	CreatedAt   time.Time
}

type Bot struct {
	ID string `gorm:"primaryKey"`
	// SpaceID is the Workspace the Bot is in.
	SpaceID string `gorm:"not null;default:'';index"`
	// CreatedBy is who made the Bot. It is not who may use it: that is
	// bot_access.
	CreatedBy   string `gorm:"not null;default:'';index"`
	Name        string
	Description string
	Soul        string
	// Model is the per-bot provider/model default; empty falls back to the
	// operator default. A chat override still wins for its conversation.
	Model string
	// AutoApprove is the free-text policy the approval model reads when a rule
	// decision is "auto".
	AutoApprove string
	// OutputPolicy is the free-text policy the filter model judges tool
	// results by, for actions whose output rule is Check. Empty means the
	// built-in policy.
	OutputPolicy string `gorm:"not null;default:''"`
	// TimeZone is the IANA zone this Bot's schedules and the times it reads
	// are in. Empty (a Bot from before zones) reads as the server's:
	// zone.Resolve. Unless pinned it follows the device the owner writes from.
	TimeZone       string `gorm:"not null;default:''"`
	TimeZonePinned bool   `gorm:"not null;default:false"`
	// Language is what this Bot writes to people in: a language code or
	// "mixed". Empty (a Bot from before languages) reads as mixed: lang.Bot.
	Language string `gorm:"not null;default:''"`
	// Proactivity is how much the Bot does unasked: off (no Heartbeat),
	// quiet (the Heartbeat reports to the Feed; questions wait on the
	// Dashboard) or active (it may also write into the main chat).
	Proactivity string `gorm:"not null;default:'off'"`
	// OtherProtocols lets this Bot's box open connections that are not HTTP
	// (or TLS): the network filter offers SOCKS5 for them. Off by default.
	OtherProtocols bool `gorm:"not null;default:false"`
	// AuditUnseen counts lines the network filter or the output filter flagged
	// for a human that the owner has not opened the Audit tab over yet.
	AuditUnseen int    `gorm:"not null;default:0"`
	TokenHash   string `gorm:"uniqueIndex"`
	ContainerID string
	Status      string
	LastTask    string
	Crest       int
	CreatedAt   time.Time
}

type Secret struct {
	ID    string `gorm:"primaryKey"`
	BotID string `gorm:"uniqueIndex:bot_secret"`
	Name  string `gorm:"uniqueIndex:bot_secret"`
	Value string
	// Mode is how the Bot's box sees the secret. "surrogate" (the default):
	// the box only ever gets Surrogate, and the network filter swaps Value in
	// on the way to the allowed Hosts and the stand-in back in answers, so the
	// real value never lives on the box. "plain": the box gets Value (SSH keys,
	// database passwords that a host must see as-is); seeing it leave is a flag.
	Mode string `gorm:"not null;default:'surrogate'"`
	// Hosts is the JSON list of host patterns a surrogate's real value may be
	// sent to. A surrogate with no hosts is not made.
	Hosts string `gorm:"not null;default:''"`
	// SwapBody lets the filter swap the real value into request bodies too.
	// Off, only headers carry it (Authorization, an API-key header): a body
	// to an allowed host can still publish what it holds, as an issue
	// comment or a paste does, so the owner opts in per secret.
	SwapBody bool `gorm:"not null;default:false"`
	// Surrogate is the stable stand-in the box knows, generated when a secret
	// is saved in surrogate mode. Empty for a plain secret, for a value too
	// short to stand in for, and for a channel's own secret.
	Surrogate string `gorm:"not null;default:''"`
	// SetBy is who saved the value: adding a person to the Bot names the
	// secrets someone else set, which that person could have it use.
	SetBy      string `gorm:"not null;default:''"`
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type Rule struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"uniqueIndex:bot_rule"`
	Connector string `gorm:"uniqueIndex:bot_rule"`
	Action    string `gorm:"uniqueIndex:bot_rule"`
	// Decision is the call rule (allow, ask, deny, auto); empty means the
	// row holds only an output rule and the call's default applies.
	Decision string
	// Output is the output rule (pass, flag, check, withhold): what happens
	// to the action's result before the Bot reads it. Empty means the default.
	Output string `gorm:"not null;default:''"`
}

// OperatorRule is one row of the operator's rule layer: the installation's
// rule for a connector.action (or connector.*), which every Bot obeys. A
// locked row is a floor: an owner may tighten it but not loosen it.
type OperatorRule struct {
	ID        string `gorm:"primaryKey"`
	Connector string `gorm:"uniqueIndex:operator_rule"`
	Action    string `gorm:"uniqueIndex:operator_rule"`
	// Decision is the call rule (allow, ask, deny, auto); empty means the row
	// sets only the output side.
	Decision string `gorm:"not null;default:''"`
	// Output is the output rule, empty for an action with no result side.
	Output    string `gorm:"not null;default:''"`
	Locked    bool   `gorm:"not null;default:false"`
	UpdatedAt time.Time
}

type Chat struct {
	ID string `gorm:"primaryKey"`
	// ChannelID is empty for a Web UI chat. Non-empty binds the thread to a
	// channel (Telegram, WhatsApp, Discord, …) and ExternalID is the adapter's conversation id.
	BotID      string `gorm:"index"`
	ChannelID  string `gorm:"index"`
	ExternalID string
	// AutomationID marks the hidden Chat that holds one automation's run log.
	// Web chat lists and Send skip these.
	AutomationID string `gorm:"index;default:''"`
	// SubagentID marks the hidden Chat that holds one subagent's work log.
	// Web chat lists and Send skip these too.
	SubagentID string `gorm:"index;default:''"`
	// Main marks the Bot's one main chat: where its page opens, where
	// replies to what the Bot made go, and where it asks the owner things.
	// It cannot be deleted or renamed; /clear and /wipe keep it light.
	Main  bool `gorm:"not null;default:false"`
	Title string
	// Model is the per-chat provider/model override; empty falls back to the
	// operator default.
	Model string
	// Thinking is the chat's thinking level (llm.ThinkingOrder); empty is the
	// model's default. It is fitted to each model's levels when a turn runs,
	// so it survives a model switch.
	Thinking string `gorm:"default:''"`
	// MemorySeq is the memory collector's watermark: the highest run_events.seq
	// it has already read. Only events after it reach the next collection.
	MemorySeq int64 `gorm:"default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Run struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"index"`
	ChatID    string `gorm:"index"`
	ChannelID string `gorm:"index"`
	Origin    string
	Status    string
	CreatedAt time.Time
}

// Channel is one attached adapter instance for a Bot. Adapters are built-in;
// ConfigJSON holds non-secret config only. Secret fields live in Secret rows
// named channel.<channelID>.<fieldKey> and never reach the Bot.
type Channel struct {
	ID           string `gorm:"primaryKey"`
	BotID        string `gorm:"index;uniqueIndex:bot_channel_name"`
	Adapter      string
	Name         string `gorm:"uniqueIndex:bot_channel_name"`
	Enabled      bool
	Inbound      bool
	Prompt       string
	ConfigJSON   string
	ExternalID   string
	TargetTitle  string
	Status       string
	StatusDetail string
	StateJSON    string
	CreatedAt    time.Time
}

type RunEvent struct {
	ID string `gorm:"primaryKey"`
	// Seq is the insert order. Postgres timestamps are microsecond and IDs are
	// random, so events of one run can tie on CreatedAt; replay orders by Seq.
	Seq int64 `gorm:"autoIncrement;uniqueIndex;index:idx_run_events_run_seq,priority:2"`
	// (run_id, seq) serves every read of a run's log in order and the
	// memory collector's "events after the watermark".
	RunID string `gorm:"index;index:idx_run_events_run_seq,priority:1"`
	Kind  string
	Body  string
	Tool  string
	Meta  string
	// Zone is the Bot's time zone when a message was written. Its timestamp
	// is told to the model in this zone on every replay, so a Bot that
	// changes zone never rewrites the history it already sent. Empty on
	// events from before zones.
	Zone      string `gorm:"not null;default:''"`
	CreatedAt time.Time
}

type Approval struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"index"`
	RunID     string
	Connector string
	Action    string
	ArgsJSON  string
	Status    string
	// Auto: the rule was Auto and its model would not decide, so a human is
	// asked about this one call and the rule is not theirs to change from here.
	Auto      bool `gorm:"not null;default:false"`
	CreatedAt time.Time
}

// LLMLog is one model request/response captured while debug is on. It is the
// raw debug view, not run history.
type LLMLog struct {
	ID         string `gorm:"primaryKey"`
	At         time.Time
	BotID      string `gorm:"index"`
	Label      string
	Provider   string
	Model      string
	Request    string
	Response   string
	Error      string
	InputTok   int
	OutputTok  int
	DurationMs int64
}

// AuditEvent is one line of the audit timeline: a tool call and its decision, a
// result the output filter did not pass, or something the network filter
// blocked, flagged or alerted on. Detail lines are kept for audit.detail_days
// and then swept; the traffic summaries beside them are kept longer.
type AuditEvent struct {
	ID      string `gorm:"primaryKey"`
	BotID   string `gorm:"index"`
	BotName string
	Crest   int
	// Actor is who decided or observed: worker, auto, filter, egress, system.
	Actor string
	// Action is what it was about: connector.action for a call, empty for a
	// network event (which names its Host).
	Action string
	// Decision is the outcome: allow, ask, deny, auto_allow, auto_deny,
	// output_flagged, output_withheld, output_unchecked, network_allow, block,
	// flag.
	Decision string
	// Reason is the network filter's short code (threat_list, private, …).
	Reason string
	// Detail is human words from the network filter: which list, which secret,
	// which pattern.
	Detail string
	Host   string
	Port   int32
	Proto  string
	Method string
	// Path is the request path and query, capped (the gateway masks the Bot's
	// own secrets before it leaves).
	Path      string
	BytesUp   int64
	BytesDown int64
	// Attention marks a line the owner should look at: a block, or an alert
	// reason (a secret leaving, an exfil sink).
	Attention bool
	// ConnectorID names the STDIO sidecar whose traffic it was, when the event
	// was a sidecar's rather than the Bot's box.
	ConnectorID string
	// RunID and ChatID are the run at work on the Bot when it happened, when
	// exactly one was; empty means it was outside a run.
	RunID     string
	ChatID    string
	CreatedAt time.Time `gorm:"index"`
}

// Connector is a library preset (Kind library: the operator's, site-wide) or
// a Workspace's connection (Kind custom, SpaceID set): defined once there and
// granted to its Bots through BotConnector. BotID is unused.
type Connector struct {
	ID                string `gorm:"primaryKey"`
	Kind              string // library | custom
	BotID             string `gorm:"index"`
	SpaceID           string `gorm:"not null;default:'';index"`
	SeedKey           string `gorm:"index"`
	Identifier        string `gorm:"uniqueIndex:idx_connectors_identifier,where:identifier <> ''"` // library presets only; catalog.Identifier form
	SourceID          string
	Type              string
	Name              string
	Description       string
	Category          string `gorm:"index"`
	Image             []byte
	ImageType         string
	Transport         string
	HTTPURL           string
	Auth              string
	OAuthClientID     string
	OAuthClientSecret string
	HeadersJSON       string
	StdioCommand      string
	StdioArgsJSON     string
	StdioImage        string
	EnvJSON           string
	DefaultMode       string
	Prompt            string
	AutoAttach        bool `gorm:"index"`
	// ExposeCompletion offers the connector's tools to the model as completion
	// tools (`<slug>__<action>`) as well as through Python's `import tools`.
	ExposeCompletion bool `gorm:"not null;default:false"`
	// Builtin names an internal/builtin registry entry (Transport "builtin").
	// ConfigJSON holds its plain field values and SecretsJSON the secret ones,
	// both map[string]string; library rows carry neither.
	Builtin     string
	ConfigJSON  string
	SecretsJSON string
	// TokenJSON is a connection's OAuth sign-in (connector.storedToken: the
	// token and what refreshes it), shared by every Bot it is granted to.
	TokenJSON string `gorm:"not null;default:''"`
	// CreatedBy added the connection to its Workspace; LentBy is whose
	// identity it carries (who signed it in, or typed a credential into it),
	// empty when none. A connection with a lender is Shared or personal.
	CreatedBy string `gorm:"not null;default:''"`
	LentBy    string `gorm:"not null;default:''"`
	Shared    bool   `gorm:"not null;default:false"`
	CreatedAt time.Time
}

// BotConnector grants a connection to a Bot. What is per Bot lives here: its
// MCP session (keyed by this ID), STDIO sidecar and bridge token, the tools
// its own session listed, and its status.
type BotConnector struct {
	ID              string `gorm:"primaryKey"`
	BotID           string `gorm:"uniqueIndex:bot_connector"`
	ConnectorID     string `gorm:"uniqueIndex:bot_connector"`
	AuthStatus      string
	StatusDetail    string
	ToolsJSON       string
	LastError       string
	ContainerID     string
	BridgeTokenHash string
	CreatedAt       time.Time
}

// Drive is one rclone remote mounted into a Bot at /workspace/drives/<Name>.
// Template is a key of the embedded drive catalog. Values are split the way
// the template declares them: plain user answers in OptionsJSON, secret user
// answers and every dynamic value (OAuth token, account label) in SecretsJSON,
// which never leaves the CP except rendered into the drive sidecar's env.
//
// A drive is its Workspace's (SpaceID; names are unique there) and mounts in
// the Bots it is granted to (BotDrive). BotID is the Bot whose sidecar a
// draft lists through; a saved drive has none. State is the drive's own (a
// draft, or what it lacks to render); each Bot's mount has its state on its
// grant.
type Drive struct {
	ID       string `gorm:"primaryKey"`
	SpaceID  string `gorm:"not null;default:'';index;uniqueIndex:space_drive_name"`
	BotID    string `gorm:"index"`
	Name     string `gorm:"uniqueIndex:space_drive_name"`
	Template string
	ReadOnly bool
	// Draft rows exist while the add form signs in, tests, and browses; they
	// never mount and are swept after a day.
	Draft       bool
	OptionsJSON string
	SecretsJSON string
	State       string
	StateDetail string
	// CreatedBy added the drive; LentBy is whose account it reaches (who
	// signed it in or typed its credentials). Shared is the lender's say-so.
	CreatedBy string `gorm:"not null;default:''"`
	LentBy    string `gorm:"not null;default:''"`
	Shared    bool   `gorm:"not null;default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BotDrive grants a drive to a Bot: its sidecar mounts it at
// /workspace/drives/<name>, read-only when the drive or the grant is. State
// and StateDetail are that mount's (mounting, mounted, error, stopped, or
// what the drive lacks).
type BotDrive struct {
	BotID       string `gorm:"primaryKey"`
	DriveID     string `gorm:"primaryKey;index"`
	ReadOnly    bool   `gorm:"not null;default:false"`
	State       string `gorm:"not null;default:''"`
	StateDetail string `gorm:"not null;default:''"`
	CreatedAt   time.Time
}

// DriveChange is one line of a Bot's drive journal: something written, deleted
// or renamed on a mounted drive, as its sidecar read it off rclone's log. It
// holds what happened, never content. Drive is the drive's name at the time,
// so a line still reads after its drive is removed; Sources is the JSON list
// of runs at work then (empty: outside a run).
type DriveChange struct {
	ID      string `gorm:"primaryKey"`
	BotID   string `gorm:"not null;default:'';index:drive_change_bot_seq,priority:1"`
	DriveID string `gorm:"not null;default:''"`
	Drive   string `gorm:"not null;default:''"`
	Op      string `gorm:"not null;default:''"`
	Path    string `gorm:"not null;default:''"`
	OldPath string `gorm:"not null;default:''"`
	Size    int64  `gorm:"not null;default:0"`
	Sources string `gorm:"not null;default:''"`
	// Seq orders the journal: lines of one batch share a timestamp.
	Seq int64     `gorm:"not null;autoIncrement;index:drive_change_bot_seq,priority:2"`
	At  time.Time `gorm:"not null"`
}

// DriveHost is a Bot's drive sidecar: the last container and the hash of the
// token it dials the CP with.
type DriveHost struct {
	BotID       string `gorm:"primaryKey"`
	ContainerID string
	TokenHash   string `gorm:"index"`
}

type BotSkill struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"uniqueIndex:bot_skill"`
	Kind      string `gorm:"uniqueIndex:bot_skill"`
	Name      string `gorm:"uniqueIndex:bot_skill"`
	Enabled   bool
	CreatedAt time.Time
}

// CatalogSeed records every library preset key that has ever been seeded. It is
// the durable memory that keeps a preset an admin removed from being re-added,
// while still letting newly published presets get seeded on the next run.
type CatalogSeed struct {
	Key      string `gorm:"primaryKey"`
	SeededAt time.Time
}

// Memory is one long-term fact a Bot saved with `remember` or that the memory
// collector saved from a conversation (ChatID). Searched by embedding.
type Memory struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index"`
	Content    string
	Embedding  pgvector.Vector `gorm:"type:vector(1536)"`
	EmbedModel string
	RunID      string
	ChatID     string `gorm:"default:''"`
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// MemoryDigest is the summary that stands in for a Bot's memories in
// the system prompt once they are too many to list. Key is a hash of the
// memories it was written from, so an unchanged store is not summarized again.
// It is a table of its own: the bots row is saved whole in places, and a
// digest written by the sweep must not be overwritten by a stale copy.
type MemoryDigest struct {
	BotID     string `gorm:"primaryKey"`
	Key       string `gorm:"not null;default:''"`
	Text      string `gorm:"not null;default:''"`
	UpdatedAt time.Time
}

// DesktopPause is a Bot whose desktop its owner has taken over: while the row
// exists the Bot's desktop actions and Chromium are refused. A table of its
// own because the bots row is saved whole in places, and a stale copy must
// not hand the desktop back while the owner is using it.
type DesktopPause struct {
	BotID     string `gorm:"primaryKey"`
	CreatedAt time.Time
}

// KnowledgeFolder is a workspace folder (drives included) the owner chose to
// index for `search_docs`. Files stay on the box; the CP keeps the sync state
// and the searchable chunks. Status is idle | syncing | error. DirtyAt is set
// when something wrote under the folder, so the sweep looks again soon.
type KnowledgeFolder struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index;uniqueIndex:bot_knowledge_path"`
	Path       string `gorm:"uniqueIndex:bot_knowledge_path"`
	Status     string
	Detail     string
	Files      int
	Skipped    int
	Chunks     int
	LastSyncAt *time.Time
	DirtyAt    *time.Time
	CreatedAt  time.Time
}

// KnowledgeSource is one file of a folder. Size+Mtime is the cheap change
// check; Hash (sha256 of the bytes) confirms a change and finds renames.
// Status is ok | skipped (binary, too large) | error (extractor failed).
type KnowledgeSource struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index"`
	FolderID   string `gorm:"index;uniqueIndex:knowledge_folder_path"`
	Path       string `gorm:"uniqueIndex:knowledge_folder_path"`
	Size       int64
	Mtime      int64
	Hash       string `gorm:"index"`
	EmbedModel string
	Status     string
	Detail     string
	Chunks     int
	IndexedAt  time.Time
}

// KnowledgeChunk is one embedded piece of a source. Locator says where in the
// file it came from ("p. 12", "Setup · L40"). Migrate adds a generated tsv
// column and the HNSW and GIN indexes beside it.
type KnowledgeChunk struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"index"`
	SourceID  string `gorm:"index"`
	Ord       int
	Locator   string
	Content   string
	Embedding pgvector.Vector `gorm:"type:vector(1536)"`
}

// PinnedAutomationKinds are the automation kinds a Bot has exactly one of
// (automation.Service.Pin registers them). The partial unique index
// automations_pinned covers these and no others, so a new pinned kind is
// added here too.
var PinnedAutomationKinds = []string{"heartbeat", "dashboard"}

// pinnedKindList is PinnedAutomationKinds as an SQL list.
var pinnedKindList = "('" + strings.Join(PinnedAutomationKinds, "', '") + "')"

// pinnedIndex is automations_pinned over PinnedAutomationKinds.
var pinnedIndex = "CREATE UNIQUE INDEX IF NOT EXISTS automations_pinned ON automations (bot_id, kind) WHERE kind IN " + pinnedKindList

// Automation is a scheduled background prompt. Its runs land in ChatID (a
// hidden Chat) and each firing starts with a fresh context. Kind "heartbeat" is
// the pinned one every Bot has; it is created without a schedule.
type Automation struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"index"`
	Kind      string
	Name      string
	Prompt    string
	Schedule  string
	Enabled   bool
	ChatID    string
	CreatedBy string
	NextRunAt *time.Time `gorm:"index"`
	LastRunAt *time.Time
	LastRunID string
	// LastStatus holds a firing that never became a run ("skipped"); a real
	// run's status is read from its Run row.
	LastStatus string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// FeedPost is one read-only message a Bot posted to its owner's Feed with the
// `feed` tool. ChatID/RunID name the conversation or automation log it came
// from; ReadAt is set once the owner has seen it.
type FeedPost struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index"`
	Title      string
	Body       string
	SourceKind string
	SourceName string
	ChatID     string
	RunID      string
	ReadAt     *time.Time
	CreatedAt  time.Time `gorm:"index"`
}

// DashboardCard is one ask on a Bot's Dashboard: something the Bot needs the
// owner to decide, with display blocks and form controls and a submit button
// that opens a chat. Key is an optional per-Bot slug for updates in place.
type DashboardCard struct {
	ID          string `gorm:"primaryKey"`
	BotID       string `gorm:"index"`
	Key         string `gorm:"index"`
	Title       string
	Summary     string
	Priority    int    `gorm:"not null;default:2"`
	BlocksJSON  string `gorm:"not null;default:'[]'"`
	SubmitLabel string `gorm:"not null;default:''"`
	Status      string `gorm:"index;not null;default:'open'"`
	Suppressed  bool   `gorm:"not null;default:false"`
	SourceKind  string
	SourceName  string
	ChatID      string
	RunID       string
	ReadAt      *time.Time
	DismissedAt *time.Time
	CreatedAt   time.Time `gorm:"index"`
	UpdatedAt   time.Time `gorm:"index"`
}

// SetupOffer is the Bot offering its owner something to add from the
// catalog (internal/app/setup): a connector, a channel or a drive, with the
// form's values it filled in. Status is open, done or skipped.
type SetupOffer struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index"`
	ChatID     string `gorm:"index"`
	RunID      string
	Kind       string
	Target     string
	Label      string
	Why        string
	ValuesJSON string `gorm:"not null;default:'{}'"`
	Status     string `gorm:"not null;default:'open'"`
	// Told is set once the Bot has heard what the owner did with it.
	Told      bool `gorm:"not null;default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DashboardWidget is one view a Bot keeps on its Dashboard. The Bot writes the
// spec once (kind, title, settings, actions, an optional probe); the data comes
// from the probe, which the CP runs on the box on a schedule without a model,
// or from code that pushes it. Key is unique per Bot so a put updates in place.
// Size/Rows/Position are the Bot's suggestion until the owner arranges the
// board (OwnerPlaced), after which only the owner moves it.
type DashboardWidget struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"not null;uniqueIndex:idx_dash_widget_key,priority:1"`
	Key       string `gorm:"not null;uniqueIndex:idx_dash_widget_key,priority:2"`
	Kind      string `gorm:"not null;default:''"`
	Title     string `gorm:"not null;default:''"`
	Source    string `gorm:"not null;default:''"`
	SpecJSON  string `gorm:"not null;default:'{}'"`
	DataJSON  string `gorm:"not null;default:'{}'"`
	StateJSON string `gorm:"not null;default:'{}'"`
	// Probe is Python that sets `result` (or prints JSON) to the widget's
	// data; Every is its interval in seconds (0: only when asked).
	Probe string `gorm:"not null;default:''"`
	Every int    `gorm:"not null;default:0"`
	// ApprovedHash is the hash of the probe and actions the python.run rule
	// let through when the Bot put them; only that code ever runs unattended.
	ApprovedHash string `gorm:"not null;default:''"`
	Size         string `gorm:"not null;default:'m'"`
	Rows         int    `gorm:"not null;default:1"`
	Position     int    `gorm:"not null;default:0"`
	OwnerPlaced  bool   `gorm:"not null;default:false"`
	Error        string `gorm:"not null;default:''"`
	ReceiptJSON  string `gorm:"not null;default:''"`
	ArtifactID   string `gorm:"not null;default:''"`
	ChatID       string `gorm:"not null;default:''"`
	RunID        string `gorm:"not null;default:''"`
	DataAt       *time.Time
	// ChangedAt is when the data last differed; SeenAt when the owner looked.
	ChangedAt *time.Time
	SeenAt    *time.Time
	NextAt    *time.Time `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Subagent is a background agent loop a chat's lead started with spawn_agent.
// Its work lands in ChatID (a hidden Chat) and starts from a fresh context
// (the goal and context the lead passed). ParentChatID is the lead's chat.
// Reported flips true once the lead has seen the latest result, either through
// agent_status or a wake report.
type Subagent struct {
	ID           string `gorm:"primaryKey"`
	BotID        string `gorm:"index"`
	ParentChatID string `gorm:"index"`
	ChatID       string `gorm:"index"`
	Name         string
	Goal         string
	Context      string
	Model        string
	Status       string
	Result       string
	Reported     bool
	CreatedAt    time.Time
	FinishedAt   *time.Time
}

// TaskItem is one line on a chat's Taskboard. The board belongs to the lead
// chat and is shared with its subagents; Assignee is the [NAME] prefix.
type TaskItem struct {
	ID        string `gorm:"primaryKey"`
	ChatID    string `gorm:"index"`
	N         int
	Text      string
	Assignee  string
	Done      bool
	DoneBy    string
	Note      string
	CreatedBy string
	CreatedAt time.Time
	DoneAt    *time.Time
}

// Tunnel is a declared route into a service on a Bot's localhost: a generated
// name (the subdomain), the port, and who may open it. Public tunnels need no
// sign-in; private ones need a TunnelGrant from the Bot's owner. A Bot has one
// tunnel per port.
type Tunnel struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"uniqueIndex:idx_tunnel_bot_port"`
	Port       int    `gorm:"uniqueIndex:idx_tunnel_bot_port"`
	Name       string `gorm:"uniqueIndex"`
	Public     bool
	CreatedBy  string // "owner" or "bot"
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// TunnelGrant is a browser's permission to open one private tunnel: the owner
// signed in on the Control Plane and was handed a cookie for the tunnel's
// origin, bound to their Silo session. ID is the hash of the cookie value, so a
// database leak is not a session leak.
type TunnelGrant struct {
	ID       string `gorm:"primaryKey"`
	TunnelID string `gorm:"index"`
	UserID   string
	// SessionID is the Silo session the owner was signed in with when they
	// opened the tunnel. The grant is good only while that session is: signing
	// out (or it expiring) ends tunnel access with it.
	SessionID string    `gorm:"index"`
	ExpiresAt time.Time `gorm:"index"`
}

// Artifact is one card in a chat: what the Bot handed the human at a path.
// Each emit of the same path in the same chat is a new ArtifactVersion of it.
// Label names it on the content host (app-<label>, share links have their
// own).
type Artifact struct {
	ID     string `gorm:"primaryKey"`
	BotID  string `gorm:"index"`
	ChatID string `gorm:"uniqueIndex:idx_artifact_chat_path,priority:1"`
	Path   string `gorm:"uniqueIndex:idx_artifact_chat_path,priority:2"`
	// Type is file, skill or app.
	Type      string
	Title     string
	Label     string `gorm:"uniqueIndex"`
	Latest    int    `gorm:"not null;default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ArtifactVersion is one emit of an artifact. Blob is its id in the Bot's
// artifact store (the sha256 of the file, or of a folder's manifest); empty
// when it was over the size caps and only the live file is there. Gone: the
// store's budget evicted it.
type ArtifactVersion struct {
	ID         string `gorm:"primaryKey"`
	ArtifactID string `gorm:"uniqueIndex:idx_artifact_version,priority:1"`
	N          int    `gorm:"uniqueIndex:idx_artifact_version,priority:2"`
	BotID      string `gorm:"index"`
	RunID      string
	// Kind is file or tree; Entry is an app's page within its tree.
	Kind  string
	Name  string
	Size  int64
	Blob  string `gorm:"index"`
	Entry string
	// Network: an app allowed to reach the internet (artifact.app_network).
	Network bool `gorm:"not null;default:false"`
	Gone    bool `gorm:"not null;default:false"`
	// Preview is the card's JSON look: a text excerpt or a small image.
	Preview   string
	CreatedAt time.Time
}

// ArtifactShare is an owner's public link to one version: a copy of its bytes
// on the control plane (data/shares/<id>), served at share-<label> with no
// sign-in, so it works while the Bot is stopped.
type ArtifactShare struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index"`
	ArtifactID string `gorm:"index"`
	VersionID  string
	Label      string `gorm:"uniqueIndex"`
	Title      string
	Type       string
	Kind       string
	Name       string
	Entry      string
	Network    bool `gorm:"not null;default:false"`
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// ArtifactView is the owner's pass to one app version on the content host:
// the token is in the app's path (its hash is the id), bound to the Silo
// session that opened it like a tunnel grant, so signing out ends it.
type ArtifactView struct {
	ID        string `gorm:"primaryKey"`
	VersionID string
	BotID     string `gorm:"index"`
	UserID    string
	SessionID string    `gorm:"index"`
	ExpiresAt time.Time `gorm:"index"`
}

// Mailbox is a Bot's receive-only address: Name is the part before the @ of
// mail.domain, so a change of the domain moves every address at once. One per
// Bot, made the first time it is needed.
type Mailbox struct {
	BotID string `gorm:"primaryKey"`
	Name  string `gorm:"uniqueIndex"`
	// Wake starts a chat when a verified message from a WakeFrom sender
	// arrives. WakeFrom is one address or @domain per line.
	Wake      bool   `gorm:"not null;default:false"`
	WakeFrom  string `gorm:"not null;default:''"`
	CreatedAt time.Time
}

// MailMessage is one message a Bot's mailbox received. Text is the body as a
// person or the model reads it; the bytes as they arrived are its MailBody.
// Hash (of those bytes) makes a sender's retry the same row, not a second one.
type MailMessage struct {
	ID    string `gorm:"primaryKey"`
	BotID string `gorm:"index:idx_mail_bot_created,priority:1;uniqueIndex:idx_mail_bot_hash,priority:1"`
	Hash  string `gorm:"uniqueIndex:idx_mail_bot_hash,priority:2"`
	// Sender is the From header as written; SenderAddr its one address, empty
	// when the header names none or several. EnvelopeFrom is SMTP's MAIL FROM.
	Sender       string
	SenderAddr   string
	EnvelopeFrom string
	Recipients   string
	Subject      string
	Text         string
	Size         int
	// Attachments is a JSON list of {index, name, type, size}.
	Attachments string
	// Verified: the sender's domain vouched for the message (aligned DKIM or
	// SPF). AuthDetail is the evidence.
	Verified   bool
	AuthDetail string
	SentAt     *time.Time
	// ReadAt is when the Bot first read it with read_mail.
	ReadAt *time.Time
	// ChatID is the chat this message started, when it woke the Bot.
	ChatID    string
	CreatedAt time.Time `gorm:"index:idx_mail_bot_created,priority:2"`
}

// MailBody holds a MailMessage's raw bytes, in a table of its own so that
// listing mail never loads them.
type MailBody struct {
	ID    string `gorm:"primaryKey"`
	BotID string `gorm:"index"`
	Data  []byte
}

// Models is every table the Control Plane migrates, shared by Open and tests.
func Models() []any {
	return []any{
		&User{}, &Session{}, &Invite{}, &RecoveryCode{}, &Space{}, &SpaceMember{}, &SpaceInvite{}, &BotAccess{}, &SpaceSkill{}, &Bot{}, &Secret{}, &Rule{}, &OperatorRule{},
		&Chat{}, &Run{}, &RunEvent{}, &Approval{}, &AuditEvent{}, &LLMLog{},
		&Connector{}, &BotConnector{}, &BotSkill{}, &Channel{}, &CatalogSeed{},
		&Memory{}, &MemoryDigest{}, &DesktopPause{}, &Automation{}, &FeedPost{}, &DashboardCard{}, &SetupOffer{}, &DashboardWidget{}, &Subagent{}, &TaskItem{},
		&Drive{}, &BotDrive{}, &DriveHost{}, &DriveChange{},
		&KnowledgeFolder{}, &KnowledgeSource{}, &KnowledgeChunk{},
		&Tunnel{}, &TunnelGrant{},
		&Artifact{}, &ArtifactVersion{}, &ArtifactShare{}, &ArtifactView{},
		&Mailbox{}, &MailMessage{}, &MailBody{},
		&EgressHost{}, &EgressClient{}, &EgressCA{}, &EgressTraffic{}, &EgressPattern{}, &EgressFeedState{},
	}
}

// Open connects to Postgres (a pgvector build) and migrates the schema.
func Open(url string) (*gorm.DB, error) {
	gdb, err := gorm.Open(postgres.Open(url), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := Migrate(gdb); err != nil {
		return nil, err
	}
	return gdb, nil
}

// Migrate registers the text scrubber, creates the vector extension, and
// migrates every table.
func Migrate(gdb *gorm.DB) error {
	if err := registerScrub(gdb); err != nil {
		return err
	}
	if err := gdb.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return fmt.Errorf("pgvector: %w", err)
	}
	// The audit page searches its rows: a trigram index makes a substring of a
	// host, path or decision a query instead of a scan. It is installed in
	// public, like vector, so a per-test schema (which is on the search path
	// with public) finds the operator class and dropping the schema does not
	// take the extension with it.
	if err := gdb.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public").Error; err != nil {
		return fmt.Errorf("pg_trgm: %w", err)
	}
	hadWatermark := !gdb.Migrator().HasTable(&Chat{}) || gdb.Migrator().HasColumn(&Chat{}, "MemorySeq")
	if err := gdb.AutoMigrate(Models()...); err != nil {
		return err
	}
	if err := hashSessionIDs(gdb); err != nil {
		return err
	}
	if !hadWatermark {
		// Chats that predate the memory collector start at their newest event,
		// so the first sweep reads only what is said from now on.
		if err := gdb.Exec(`UPDATE chats SET memory_seq = COALESCE((
			SELECT MAX(e.seq) FROM run_events e JOIN runs r ON r.id = e.run_id WHERE r.chat_id = chats.id), 0)`).Error; err != nil {
			return err
		}
	}
	if err := gdb.Exec("DELETE FROM rules WHERE connector = 'bot' AND action IN ('memory', 'core_memory')").Error; err != nil {
		return err
	}
	if err := gdb.Exec("DELETE FROM operator_rules WHERE connector = 'bot' AND action IN ('memory', 'core_memory')").Error; err != nil {
		return err
	}
	if err := dedupePinnedAutomations(gdb); err != nil {
		return err
	}
	if err := repinIndex(gdb); err != nil {
		return err
	}
	for _, stmt := range []string{
		// One Heartbeat and one Dashboard per Bot: the insert that pins them
		// relies on this to lose a race instead of making a second.
		pinnedIndex,
		// One main chat per Bot: chats.Main relies on it to lose a race.
		"CREATE UNIQUE INDEX IF NOT EXISTS chats_main ON chats (bot_id) WHERE main",
		// One owner per Workspace: a transfer demotes the old one first.
		"CREATE UNIQUE INDEX IF NOT EXISTS space_owner ON space_members (space_id) WHERE role = 'owner'",
		"CREATE INDEX IF NOT EXISTS memories_embedding_hnsw ON memories USING hnsw (embedding vector_cosine_ops)",
		// 'simple' keeps the keyword half language-neutral: names, part
		// numbers and error codes match exactly in any language.
		"ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED",
		"CREATE INDEX IF NOT EXISTS knowledge_chunks_embedding_hnsw ON knowledge_chunks USING hnsw (embedding vector_cosine_ops)",
		"CREATE INDEX IF NOT EXISTS knowledge_chunks_tsv ON knowledge_chunks USING gin (tsv)",
		// One user per identity at the provider; password-only users have none.
		"CREATE UNIQUE INDEX IF NOT EXISTS users_oidc_identity ON users (oidc_issuer, oidc_subject) WHERE oidc_subject <> ''",
		"CREATE INDEX IF NOT EXISTS audit_events_search ON audit_events USING gin ((coalesce(bot_name, '') || ' ' || coalesce(action, '') || ' ' || coalesce(decision, '') || ' ' || coalesce(reason, '') || ' ' || coalesce(host, '') || ' ' || coalesce(method, '') || ' ' || coalesce(path, '') || ' ' || coalesce(detail, '')) gin_trgm_ops)",
	} {
		if err := gdb.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// dedupePinnedAutomations removes the extra Heartbeats and Dashboards that a
// racing create made before the unique index existed, with their log chats.
// The copy kept is the one that was used: scheduled, then most recently run,
// then the oldest.
func dedupePinnedAutomations(gdb *gorm.DB) error {
	extra := `SELECT id FROM (SELECT id, row_number() OVER (PARTITION BY bot_id, kind
		ORDER BY (schedule <> '') DESC, last_run_at DESC NULLS LAST, created_at, id) AS n
		FROM automations WHERE kind IN ` + pinnedKindList + `) ranked WHERE n > 1`
	return gdb.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range []string{
			"DELETE FROM run_events WHERE run_id IN (SELECT r.id FROM runs r JOIN chats c ON c.id = r.chat_id WHERE c.automation_id IN (" + extra + "))",
			"DELETE FROM runs WHERE chat_id IN (SELECT id FROM chats WHERE automation_id IN (" + extra + "))",
			"DELETE FROM chats WHERE automation_id IN (" + extra + ")",
			"DELETE FROM automations WHERE id IN (" + extra + ")",
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// repinIndex drops automations_pinned when it misses a pinned kind (one
// added since it was made), so the CREATE below makes it over every kind.
func repinIndex(gdb *gorm.DB) error {
	var def string
	gdb.Raw("SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'automations_pinned'").Scan(&def)
	if def == "" {
		return nil
	}
	for _, k := range PinnedAutomationKinds {
		if !strings.Contains(def, "'"+k+"'") {
			return gdb.Exec("DROP INDEX automations_pinned").Error
		}
	}
	return nil
}

// hashSessionIDs moves sessions from before the id was a hash (they have no
// created_at) to the hash of the cookie they were issued with, and the tunnel
// grants made from them along, so nobody is signed out by the upgrade.
func hashSessionIDs(gdb *gorm.DB) error {
	const hashed = "encode(sha256(convert_to(%s, 'UTF8')), 'hex')"
	return gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE tunnel_grants SET session_id = " + fmt.Sprintf(hashed, "tunnel_grants.session_id") +
			" FROM sessions WHERE sessions.id = tunnel_grants.session_id AND sessions.created_at IS NULL").Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE sessions SET id = " + fmt.Sprintf(hashed, "id") +
			", created_at = now(), last_seen_at = now() WHERE created_at IS NULL").Error
	})
}
