// Package artifact is what a Bot hands the human in the thread: a file to
// download or preview, a skill to save, or a live app. Every emit of a path
// pins its bytes in the Bot's artifact store (on its machine, outside the
// workspace) as a new version of the card, so a card never changes under the
// human and older versions stay readable. The service records the versions,
// emits the card, serves downloads (cookie auth), the apps and the owner's
// public links on the content host, and runs "Save skill".
package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"silo.agent/internal/app/host"
	"silo.agent/internal/app/skill"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/names"
	"silo.agent/internal/security"
	"silo.agent/internal/skills"
)

// Host is what artifacts need from the App around them.
type Host interface {
	host.Emitter
	host.Runs
	host.Authorizer
}

// Service describes, pins, emits and serves artifacts.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	ws     *workspace.Service
	skills *skill.Service
	host   Host
	cache  blobCache
}

func New(gdb *gorm.DB, cfg func() config.Config, ws *workspace.Service, sk *skill.Service, h Host) *Service {
	return &Service{db: gdb, cfg: cfg, ws: ws, skills: sk, host: h}
}

// The types of card.
const (
	TypeFile  = "file"
	TypeSkill = "skill"
	TypeApp   = "app"
)

// Info is the JSON carried in an `artifact` RunEvent: one version of a card.
// Every emit of the same path in a chat carries the same artifact_id and the
// next version; the thread keeps one card and lists the versions on it.
type Info struct {
	Type       string `json:"type"`
	ArtifactID string `json:"artifact_id"`
	Version    int    `json:"version"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	Scope      string `json:"scope"`
	// Status is ready, or pending/saved for a skill.
	Status string `json:"status"`
	Size   int64  `json:"size,omitempty"`
	// Kind is file or tree (a folder: a skill or a multi-file app).
	Kind string `json:"kind"`
	// Pinned: this version's bytes are kept. Not pinned (over the caps), the
	// card shows the live file and Note says why.
	Pinned  bool     `json:"pinned"`
	Note    string   `json:"note,omitempty"`
	Network bool     `json:"network,omitempty"`
	Entry   string   `json:"entry,omitempty"`
	Files   int      `json:"files,omitempty"`
	Preview *Preview `json:"preview,omitempty"`
}

func (s *Service) emit(botID, runID string, info Info) {
	b, _ := json.Marshal(info)
	s.host.Emit(botID, s.host.ChatOfRun(runID), runID, "artifact", string(b), "")
}

func titleOr(title, fallback string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return fallback
}

// args is the artifact tool's input.
type args struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Network bool   `json:"network"`
}

func isHTML(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return true
	}
	return false
}

// Tool is the shared engine behind the chat `artifact` tool and the Python
// `silo_runtime.artifact` / CallTool path: it pins the path, records the
// version, emits the card and returns a short ack for the model. It gates
// itself (artifact.emit, and artifact.app_network for an app that asks for
// the internet), since the second rule depends on what the path turns out to
// be.
func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID, argsJSON string) (string, error) {
	var a args
	if raw := strings.TrimSpace(argsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			return "", errors.New("invalid args")
		}
	}
	rel := workspace.Rel(a.Path)
	if rel == "" {
		return "", errors.New("path required")
	}
	kind := strings.ToLower(strings.TrimSpace(a.Kind))
	switch kind {
	case "", TypeFile, TypeSkill, TypeApp:
	default:
		return "", errors.New("kind is file, skill or app")
	}
	if _, err := s.host.AuthorizeAction(ctx, bot, runID, security.Artifact, "emit", argsJSON, ""); err != nil {
		return "", err
	}
	chatID := s.host.ChatOfRun(runID)
	if chatID == "" {
		return "", errors.New("artifact needs a live chat run; this process is not attached to one")
	}

	asTree := isHTML(rel) && kind != TypeFile
	var requires []string
	switch kind {
	case TypeSkill:
		requires = []string{"SKILL.md"}
	case TypeApp:
		requires = []string{"index.html"}
	default:
		requires = []string{"SKILL.md", "index.html"}
	}
	p, err := s.pin(ctx, bot.ID, rel, asTree, requires)
	if err != nil {
		return "", err
	}
	typ, entry, err := cardType(kind, rel, p)
	if err != nil {
		s.release(ctx, bot.ID, p.ID)
		return "", err
	}
	network := a.Network && typ == TypeApp
	if network {
		if _, err := s.host.AuthorizeAction(ctx, bot, runID, security.Artifact, "app_network", argsJSON, ""); err != nil {
			s.release(ctx, bot.ID, p.ID)
			return "", err
		}
	}
	s.markEvicted(bot.ID, p.Evicted)

	info := Info{
		Type: typ, Path: rel, Scope: "workspace", Status: "ready", Size: p.Size, Kind: p.Kind,
		Pinned: p.ID != "", Note: p.TooLarge, Network: network, Entry: entry, Files: len(p.Entries),
		Name: p.Name, Preview: p.Preview,
	}
	if typ == TypeSkill {
		info.Status = "pending"
		if p.ID != "" {
			if m, err := s.skillMeta(ctx, bot.ID, p.Entries); err == nil {
				info.Name = m.Name
				if d := strings.TrimSpace(m.Description); d != "" {
					info.Preview = &Preview{Text: d}
				}
			}
		} else if peek, err := s.skills.Peek(ctx, bot.ID, rel); err == nil {
			info.Name = peek.Name
		}
	}
	info.Title = titleOr(a.Title, info.Name)
	art, ver, err := s.record(bot.ID, chatID, runID, info, p.ID)
	if err != nil {
		return "", err
	}
	info.ArtifactID, info.Version = art.ID, ver.N
	s.emit(bot.ID, runID, info)
	return ack(info), nil
}

// cardType decides what a pinned path is: a skill (a folder with SKILL.md), an
// app (an HTML file, or a folder with index.html) or a file.
func cardType(kind, rel string, p pinned) (typ, entry string, err error) {
	if p.Kind == "tree" {
		var hasSkill, hasIndex bool
		for _, e := range p.Entries {
			hasSkill = hasSkill || e.Path == "SKILL.md"
			hasIndex = hasIndex || e.Path == "index.html"
		}
		switch {
		case kind == TypeFile:
			return "", "", errors.New("this is a folder: a file artifact names one file (or leave kind out)")
		case len(p.Entries) == 1 && isHTML(p.Entries[0].Path) && kind != TypeSkill:
			return TypeApp, p.Entries[0].Path, nil
		case p.ID == "" && kind != "":
			// Over the caps: the entries are unknown, so trust the kind asked for.
			return kind, "index.html", nil
		case hasSkill && kind != TypeApp:
			return TypeSkill, "", nil
		case hasIndex && kind != TypeSkill:
			return TypeApp, "index.html", nil
		case p.ID == "":
			if isHTML(rel) {
				return TypeApp, path.Base(rel), nil
			}
			return TypeSkill, "", nil
		}
		return "", "", errors.New("a folder artifact must hold SKILL.md (a skill) or index.html (an app) at its top")
	}
	if kind == TypeSkill {
		return "", "", errors.New("a skill artifact is a folder holding SKILL.md")
	}
	if kind == TypeApp {
		return "", "", errors.New("an app artifact is an .html file or a folder holding index.html")
	}
	return TypeFile, "", nil
}

func (s *Service) skillMeta(ctx context.Context, botID string, entries []entry) (skills.Meta, error) {
	for _, e := range entries {
		if e.Path == "SKILL.md" {
			b, err := s.blob(ctx, botID, e.SHA)
			if err != nil {
				return skills.Meta{}, err
			}
			return skills.Parse(string(b))
		}
	}
	return skills.Meta{}, errors.New("directory must contain SKILL.md")
}

func ack(info Info) string {
	v := fmt.Sprintf(" (version %d)", info.Version)
	note := ""
	if !info.Pinned && info.Note != "" {
		note = " It is too large to keep (" + info.Note + "), so the card shows the live file: changing it changes the card."
	}
	switch info.Type {
	case TypeSkill:
		return "showed skill " + info.Name + " as an artifact" + v + ". It is not installed until the human clicks Save skill." + note
	case TypeApp:
		net := "with no network: it can load only its own files"
		if info.Network {
			net = "with network access"
		}
		return "showed " + info.Name + " as a live app in the thread" + v + ", sandboxed " + net + "." + note
	}
	return "showed " + info.Name + " as a downloadable artifact" + v + "." + note
}

// record finds or makes the chat's card for the path and adds the version.
func (s *Service) record(botID, chatID, runID string, info Info, blob string) (*db.Artifact, *db.ArtifactVersion, error) {
	var art db.Artifact
	var ver db.ArtifactVersion
	preview := ""
	if info.Preview != nil {
		if b, err := json.Marshal(info.Preview); err == nil {
			preview = string(b)
		}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("chat_id = ? AND path = ?", chatID, info.Path).Limit(1).Find(&art)
		if res.Error != nil {
			return res.Error
		}
		now := time.Now()
		if res.RowsAffected == 0 {
			art = db.Artifact{ID: ids.New(), BotID: botID, ChatID: chatID, Path: info.Path, Type: info.Type, Title: info.Title,
				Label: names.New() + "-" + ids.New()[:6], CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&art).Error; err != nil {
				return err
			}
		}
		art.Latest++
		art.Type, art.Title, art.UpdatedAt = info.Type, info.Title, now
		if err := tx.Model(&art).Updates(map[string]any{"latest": art.Latest, "type": art.Type, "title": art.Title, "updated_at": now}).Error; err != nil {
			return err
		}
		ver = db.ArtifactVersion{
			ID: ids.New(), ArtifactID: art.ID, N: art.Latest, BotID: botID, RunID: runID, Kind: info.Kind, Name: info.Name,
			Size: info.Size, Blob: blob, Entry: info.Entry, Network: info.Network, Preview: preview, CreatedAt: now,
		}
		if err := tx.Create(&ver).Error; err != nil {
			return err
		}
		if blob != "" {
			// Pinning again keeps the bytes for every version that has them.
			return tx.Model(&db.ArtifactVersion{}).Where("bot_id = ? AND blob = ?", botID, blob).Update("gone", false).Error
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &art, &ver, nil
}

// release forgets a pin no version uses (a refused emit).
func (s *Service) release(ctx context.Context, botID, id string) {
	if id == "" {
		return
	}
	var n int64
	s.db.Model(&db.ArtifactVersion{}).Where("bot_id = ? AND blob = ?", botID, id).Count(&n)
	if n == 0 {
		_ = s.forget(ctx, botID, []string{id})
	}
}

// markEvicted records what the store's budget dropped.
func (s *Service) markEvicted(botID string, evicted []string) {
	if len(evicted) == 0 {
		return
	}
	s.cache.drop(botID)
	s.db.Model(&db.ArtifactVersion{}).Where("bot_id = ? AND blob IN ?", botID, evicted).Update("gone", true)
}

// version finds a Bot's artifact and one of its versions (0 = the newest).
func (s *Service) version(botID, artifactID string, n int) (*db.Artifact, *db.ArtifactVersion, error) {
	var art db.Artifact
	if res := s.db.Where("id = ? AND bot_id = ?", artifactID, botID).Limit(1).Find(&art); res.Error != nil {
		return nil, nil, res.Error
	} else if res.RowsAffected == 0 || art.UpdatedAt.Year() <= 1970 {
		return nil, nil, errNotFound
	}
	if n <= 0 {
		n = art.Latest
	}
	var v db.ArtifactVersion
	if res := s.db.Where("artifact_id = ? AND n = ?", art.ID, n).Limit(1).Find(&v); res.Error != nil {
		return nil, nil, res.Error
	} else if res.RowsAffected == 0 {
		return nil, nil, errNotFound
	}
	return &art, &v, nil
}

var errNotFound = errors.New("artifact not found")

// Drop removes a Bot's artifacts and shared links (DeleteBot); the store goes
// with the Bot's data directory.
func (s *Service) Drop(botID string) {
	var shares []db.ArtifactShare
	s.db.Where("bot_id = ?", botID).Find(&shares)
	for i := range shares {
		s.removeShareFiles(shares[i].ID)
	}
	s.db.Where("bot_id = ?", botID).Delete(&db.ArtifactShare{})
	s.db.Where("bot_id = ?", botID).Delete(&db.ArtifactView{})
	s.db.Where("bot_id = ?", botID).Delete(&db.ArtifactVersion{})
	s.db.Where("bot_id = ?", botID).Delete(&db.Artifact{})
	s.cache.drop(botID)
}
