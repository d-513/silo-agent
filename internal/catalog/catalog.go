package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"gorm.io/gorm"

	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
)

//go:embed connectors.json images/*
var files embed.FS

const (
	KindLibrary = "library"
	KindCustom  = "custom"
	typeMCP     = "mcp"
)

type entry struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Guide       string `json:"guide"`
	HTTPURL     string `json:"http_url"`
	Auth        string `json:"auth"`
	DefaultMode string `json:"default_mode"`
	Image       string `json:"image"`
}

// Guide is catalog-only copy shown on the Bot add screen. It is not stored
// on the connector row and is never an editable field.
func Guide(key string) string {
	if key == "" {
		return ""
	}
	xs, err := Load()
	if err != nil {
		return ""
	}
	for _, e := range xs {
		if e.Key == key {
			return e.Guide
		}
	}
	return ""
}

type file struct {
	Connectors []entry `json:"connectors"`
}

func Load() ([]entry, error) {
	raw, err := files.ReadFile("connectors.json")
	if err != nil {
		return nil, err
	}
	var spec file
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, err
	}
	for i, e := range spec.Connectors {
		if strings.TrimSpace(e.Key) == "" || strings.TrimSpace(e.Name) == "" || strings.TrimSpace(e.HTTPURL) == "" {
			return nil, fmt.Errorf("catalog entry %d needs key, name, and http_url", i)
		}
	}
	return spec.Connectors, nil
}

func imageOf(name string) ([]byte, string, error) {
	if name == "" {
		return nil, "", nil
	}
	b, err := files.ReadFile(path.Join("images", name))
	if err != nil {
		return nil, "", err
	}
	typ := "image/png"
	switch {
	case strings.HasSuffix(name, ".svg"):
		typ = "image/svg+xml"
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		typ = "image/jpeg"
	case strings.HasSuffix(name, ".webp"):
		typ = "image/webp"
	}
	return b, typ, nil
}

// Seed inserts missing library presets. Existing rows (including admin edits and
// deletions) are left alone.
func Seed(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	entries, err := Load()
	if err != nil {
		return err
	}
	for _, e := range entries {
		var n int64
		if err := gdb.Model(&db.Connector{}).Where("seed_key = ?", e.Key).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		img, typ, err := imageOf(e.Image)
		if err != nil {
			return fmt.Errorf("%s image: %w", e.Key, err)
		}
		auth := strings.ToLower(strings.TrimSpace(e.Auth))
		if auth == "" {
			auth = "none"
		}
		row := db.Connector{
			ID: ids.New(), Kind: KindLibrary, SeedKey: e.Key, Type: typeMCP,
			Name: e.Name, Description: e.Description, Image: img, ImageType: typ,
			Transport: "http", HTTPURL: e.HTTPURL, Auth: auth,
			DefaultMode: security.Rule(e.DefaultMode), CreatedAt: time.Now(),
		}
		if err := gdb.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
