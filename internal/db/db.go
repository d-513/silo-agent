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
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"index"`
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Run struct {
	ID        string `gorm:"primaryKey"`
	BotID     string `gorm:"index"`
	ChatID    string `gorm:"index"`
	Status    string
	CreatedAt time.Time
}

type RunEvent struct {
	ID        string `gorm:"primaryKey"`
	RunID     string `gorm:"index"`
	Kind      string
	Body      string
	Tool      string
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

type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string
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
	Image             []byte
	ImageType         string
	Transport         string
	HTTPURL           string
	Auth              string
	OAuthClientID     string
	OAuthClientSecret string
	HeadersJSON       string
	DefaultMode       string
	CreatedAt         time.Time
}

type BotConnector struct {
	ID          string `gorm:"primaryKey"`
	BotID       string `gorm:"uniqueIndex:bot_connector"`
	ConnectorID string `gorm:"uniqueIndex:bot_connector"`
	AuthStatus  string
	TokenJSON   string
	ToolsJSON   string
	LastError   string
	CreatedAt   time.Time
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
		&Chat{}, &Run{}, &RunEvent{}, &Approval{}, &Setting{}, &Audit{},
		&Connector{}, &BotConnector{},
	)
	if err != nil {
		return nil, err
	}
	return gdb, nil
}
