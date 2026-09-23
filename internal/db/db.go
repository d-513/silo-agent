package db

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
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
	Title      string
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
	ID        string `gorm:"primaryKey"`
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

func Open(dataDir string) (*gorm.DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "silo.db")
	gdb, err := gorm.Open(sqlite.Open(path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: %w", err)
	}
	err = gdb.AutoMigrate(
		&User{}, &Session{}, &Bot{}, &Secret{}, &Rule{},
		&Chat{}, &Run{}, &RunEvent{}, &Approval{}, &Audit{}, &LLMLog{},
		&Connector{}, &BotConnector{}, &BotSkill{}, &Channel{}, &CatalogSeed{},
	)
	if err != nil {
		return nil, err
	}
	return gdb, nil
}
