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
	// channel (Telegram, …) and ExternalID is the adapter's conversation id.
	BotID      string `gorm:"index"`
	ChannelID  string `gorm:"index"`
	ExternalID string
	// AutomationID marks the hidden Chat that holds one automation's run log.
	// Web chat lists and Send skip these.
	AutomationID string `gorm:"index;default:''"`
	Title        string
	// Model is the per-chat provider/model override; empty falls back to the
	// operator default.
	Model     string
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
	CreatedAt         time.Time
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
type Memory struct {
	ID         string `gorm:"primaryKey"`
	BotID      string `gorm:"index"`
	Content    string
	Embedding  pgvector.Vector `gorm:"type:vector(1536)"`
	EmbedModel string
	RunID      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
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

// Models is every table the Control Plane migrates, shared by Open and tests.
func Models() []any {
	return []any{
		&User{}, &Session{}, &Bot{}, &Secret{}, &Rule{},
		&Chat{}, &Run{}, &RunEvent{}, &Approval{}, &Audit{}, &LLMLog{},
		&Connector{}, &BotConnector{}, &BotSkill{}, &Channel{}, &CatalogSeed{},
		&Memory{}, &Automation{}, &FeedPost{},
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
	if err := gdb.AutoMigrate(Models()...); err != nil {
		return err
	}
	// The always-in-prompt MEMORY tool is now core_memory; keep the owner's rule.
	if err := gdb.Exec("UPDATE rules SET action = 'core_memory' WHERE connector = 'bot' AND action = 'memory'").Error; err != nil {
		return err
	}
	return gdb.Exec("CREATE INDEX IF NOT EXISTS memories_embedding_hnsw ON memories USING hnsw (embedding vector_cosine_ops)").Error
}
