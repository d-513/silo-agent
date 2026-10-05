package db

import (
	"fmt"
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type User struct {
	ID           string `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	Admin        bool
	CreatedAt    time.Time
}

type Session struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index"`
	ExpiresAt time.Time
}

type Bot struct {
	ID          string `gorm:"primaryKey"`
	UserID      string `gorm:"index"`
	Name        string
	Description string
	Soul        string
	Memory      string
	// Model is the per-bot provider/model default; empty falls back to the
	// operator default. A chat override still wins for its conversation.
	Model string
	// AutoApprove is the free-text policy the approval model reads when a rule
	// decision is "auto".
	AutoApprove string
	TokenHash   string `gorm:"uniqueIndex"`
	ContainerID string
	Status      string
	LastTask    string
	Crest       int
	CreatedAt   time.Time
}

type Secret struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"uniqueIndex:bot_secret"`
	Name       string `gorm:"uniqueIndex:bot_secret"`
	Value      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type Rule struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"uniqueIndex:bot_rule"`
	Connector string `gorm:"uniqueIndex:bot_rule"`
	Action    string `gorm:"uniqueIndex:bot_rule"`
	Decision  string
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
	Title      string
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
	Seq       int64  `gorm:"autoIncrement;uniqueIndex"`
	RunID     string `gorm:"index"`
	Kind      string
	Body      string
	Tool      string
	Meta      string
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

type Audit struct {
	ID        string `gorm:"primaryKey"`
	BotID     string
	BotName   string
	Crest     int
	Actor     string
	Action    string
	Decision  string
	CreatedAt time.Time
}

type Connector struct {
	ID                string `gorm:"primaryKey"`
	Kind              string // library | custom
	BotID             string `gorm:"index"`
	SeedKey           string `gorm:"index"`
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
	// Builtin names an internal/builtin registry entry (Transport "builtin").
	// ConfigJSON holds its plain field values and SecretsJSON the secret ones,
	// both map[string]string; library rows carry neither.
	Builtin     string
	ConfigJSON  string
	SecretsJSON string
	CreatedAt   time.Time
}

type BotConnector struct {
	ID              string `gorm:"primaryKey"`
	BotID           string `gorm:"uniqueIndex:bot_connector"`
	ConnectorID     string `gorm:"uniqueIndex:bot_connector"`
	AuthStatus      string
	StatusDetail    string
	TokenJSON       string
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
type Drive struct {
	ID       string `gorm:"primaryKey"`
	BotID    string `gorm:"index;uniqueIndex:bot_drive_name"`
	Name     string `gorm:"uniqueIndex:bot_drive_name"`
	Template string
	ReadOnly bool
	// Draft rows exist while the add form signs in, tests, and browses; they
	// never mount and are swept after a day.
	Draft       bool
	OptionsJSON string
	SecretsJSON string
	State       string
	StateDetail string
	CreatedAt   time.Time
	UpdatedAt   time.Time
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

// Memory is one long-term fact a Bot saved with `remember`. MEMORY (bots.memory)
// stays the small always-in-prompt set; these are searched by embedding.
// Kind is "fact" or "lesson" (a pitfall and what worked). ChatID is set when
// the memory collector saved it from that conversation.
type Memory struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index"`
	Kind       string `gorm:"default:'fact'"`
	Content    string
	Embedding  pgvector.Vector `gorm:"type:vector(1536)"`
	EmbedModel string
	RunID      string
	ChatID     string `gorm:"default:''"`
	CreatedAt  time.Time
	LastUsedAt *time.Time
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

// Models is every table the Control Plane migrates, shared by Open and tests.
func Models() []any {
	return []any{
		&User{}, &Session{}, &Bot{}, &Secret{}, &Rule{},
		&Chat{}, &Run{}, &RunEvent{}, &Approval{}, &Audit{}, &LLMLog{},
		&Connector{}, &BotConnector{}, &BotSkill{}, &Channel{}, &CatalogSeed{},
		&Memory{}, &Automation{}, &FeedPost{}, &Subagent{}, &TaskItem{},
		&Drive{}, &DriveHost{},
		&KnowledgeFolder{}, &KnowledgeSource{}, &KnowledgeChunk{},
		&Tunnel{}, &TunnelGrant{},
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
	hadWatermark := !gdb.Migrator().HasTable(&Chat{}) || gdb.Migrator().HasColumn(&Chat{}, "MemorySeq")
	if err := gdb.AutoMigrate(Models()...); err != nil {
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
	// The always-in-prompt MEMORY tool is now core_memory; keep the owner's rule.
	if err := gdb.Exec("UPDATE rules SET action = 'core_memory' WHERE connector = 'bot' AND action = 'memory'").Error; err != nil {
		return err
	}
	for _, stmt := range []string{
		"CREATE INDEX IF NOT EXISTS memories_embedding_hnsw ON memories USING hnsw (embedding vector_cosine_ops)",
		// 'simple' keeps the keyword half language-neutral: names, part
		// numbers and error codes match exactly in any language.
		"ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED",
		"CREATE INDEX IF NOT EXISTS knowledge_chunks_embedding_hnsw ON knowledge_chunks USING hnsw (embedding vector_cosine_ops)",
		"CREATE INDEX IF NOT EXISTS knowledge_chunks_tsv ON knowledge_chunks USING gin (tsv)",
	} {
		if err := gdb.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
