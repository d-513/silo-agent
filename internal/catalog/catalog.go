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
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Guide        string   `json:"guide"`
	Prompt       string   `json:"prompt"`
	Transport    string   `json:"transport"`
	HTTPURL      string   `json:"http_url"`
	Auth         string   `json:"auth"`
	DefaultMode  string   `json:"default_mode"`
	AutoAttach   bool     `json:"auto_attach"`
	Image        string   `json:"image"`
	StdioCommand string   `json:"stdio_command"`
	StdioArgs    []string `json:"stdio_args"`
	StdioImage   string   `json:"stdio_image"`
}

// transportOf resolves an entry's transport, defaulting by which endpoint is set.
func (e entry) transportOf() string {
	tr := strings.ToLower(strings.TrimSpace(e.Transport))
	if tr == "http" || tr == "stdio" {
		return tr
	}
	if strings.TrimSpace(e.StdioCommand) != "" {
		return "stdio"
	}
	return "http"
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
		if strings.TrimSpace(e.Key) == "" || strings.TrimSpace(e.Name) == "" {
			return nil, fmt.Errorf("catalog entry %d needs key and name", i)
		}
		tr := e.transportOf()
		if tr == "stdio" {
			if strings.TrimSpace(e.StdioCommand) == "" {
				return nil, fmt.Errorf("catalog entry %s needs stdio_command", e.Key)
			}
			continue
		}
		if strings.TrimSpace(e.HTTPURL) == "" {
			return nil, fmt.Errorf("catalog entry %s needs http_url", e.Key)
		}
	}
	return spec.Connectors, nil
}

// argsJSON stores the structured argv the bridge runs. An empty argv is the
// JSON empty array, matching the app's merge/serialization.
func argsJSON(args []string) string {
	if args == nil {
		args = []string{}
	}
	b, _ := json.Marshal(args)
	return string(b)
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

// Seed inserts library presets that have never been seeded before. The
// catalog_seeds table is the durable record of everything already offered, so a
// preset an admin removes stays gone while new upstream presets are still
// added. Existing rows (including admin edits) are left alone.
func Seed(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	entries, err := Load()
	if err != nil {
		return err
	}
	seen, err := seededKeys(gdb)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if seen[e.Key] {
			var existing db.Connector
			if gdb.First(&existing, "seed_key = ?", e.Key).Error == nil && e.Category != "" {
				if existing.Category == "" {
					gdb.Model(&existing).Update("category", e.Category)
				}
			}
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
			Name: e.Name, Description: e.Description, Category: e.Category, Image: img, ImageType: typ,
			Transport: e.transportOf(), HTTPURL: e.HTTPURL, Auth: auth,
			DefaultMode: security.Rule(e.DefaultMode), Prompt: e.Prompt, AutoAttach: e.AutoAttach,
			CreatedAt: time.Now(),
		}
		if row.Transport == "stdio" {
			row.Auth = "none"
			row.HTTPURL = ""
			row.StdioCommand = strings.TrimSpace(e.StdioCommand)
			row.StdioArgsJSON = argsJSON(e.StdioArgs)
			row.StdioImage = strings.TrimSpace(e.StdioImage)
		}
		if err := gdb.Create(&row).Error; err != nil {
			return err
		}
		if err := gdb.Create(&db.CatalogSeed{Key: e.Key, SeededAt: time.Now()}).Error; err != nil {
			return err
		}
	}
	return nil
}

// seededKeys returns the set of catalog keys already offered, backfilling from
// any library rows seeded before this table existed so an upgrade does not
// re-offer presets that are already present.
func seededKeys(gdb *gorm.DB) (map[string]bool, error) {
	var keys []string
	if err := gdb.Model(&db.CatalogSeed{}).Pluck("key", &keys).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	var rows []db.Connector
	if err := gdb.Select("seed_key, created_at").Where("kind = ? AND seed_key <> ''", KindLibrary).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.SeedKey == "" || seen[r.SeedKey] {
			continue
		}
		if err := gdb.Create(&db.CatalogSeed{Key: r.SeedKey, SeededAt: r.CreatedAt}).Error; err != nil {
			return nil, err
		}
		seen[r.SeedKey] = true
	}
	return seen, nil
}
