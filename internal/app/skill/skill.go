// Package skill is the skill library the Bots draw on: skill bodies on the
// control plane's disk (library and personal), which ones each Bot has enabled,
// the install, delete and file-browse RPCs, syncing the enabled set into the
// Bot's machine, and turning a workspace folder into a personal skill.
package skill

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/catalog"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/skills"
	"silo.agent/internal/textx"
)

// Service reads and writes the skill library and each Bot's enabled set.
type Service struct {
	db  *gorm.DB
	hub *hub.Hub
	cfg func() config.Config
	ws  *workspace.Service
}

func New(gdb *gorm.DB, h *hub.Hub, cfg func() config.Config, ws *workspace.Service) *Service {
	return &Service{db: gdb, hub: h, cfg: cfg, ws: ws}
}

// DataDir is the control plane's data directory, where skill bodies live.
func (s *Service) DataDir() string {
	return s.cfg().DataDir
}

func (s *Service) SeedSkills(ctx context.Context, _ *connect.Request[v1.SeedSkillsRequest]) (*connect.Response[v1.SeedSkillsResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := catalog.SeedSkills(s.DataDir()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.SeedSkillsResponse{}), nil
}

func skillScope(scope string, u *db.User) (kind, userID string, err error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", skills.KindLibrary:
		return skills.KindLibrary, "", nil
	case skills.KindPersonal:
		if u == nil || u.ID == "" {
			return "", "", connect.NewError(connect.CodeUnauthenticated, errors.New("sign in"))
		}
		return skills.KindPersonal, u.ID, nil
	default:
		return "", "", connect.NewError(connect.CodeInvalidArgument, errors.New("scope is library or personal"))
	}
}

func (s *Service) ListSkills(ctx context.Context, req *connect.Request[v1.ListSkillsRequest]) (*connect.Response[v1.ListSkillsResponse], error) {
	kind, userID, err := skillScope(req.Msg.GetScope(), access.User(ctx))
	if err != nil {
		return nil, err
	}
	root := skills.RootFor(s.DataDir(), kind, userID)
	rows, err := skills.List(root, kind)
	if err != nil {
		return nil, err
	}
	out := &v1.ListSkillsResponse{}
	for _, r := range rows {
		out.Skills = append(out.Skills, protoSkill(r))
	}
	return connect.NewResponse(out), nil
}

func protoSkill(r skills.Info) *v1.Skill {
	return &v1.Skill{
		Name: r.Name, Description: r.Description, Kind: r.Kind,
		Source: r.Source, Seeded: r.Kind == skills.KindLibrary && catalog.SeededName(r.Name),
	}
}

func (s *Service) InstallSkill(ctx context.Context, req *connect.Request[v1.InstallSkillRequest]) (*connect.Response[v1.InstallSkillResponse], error) {
	u := access.User(ctx)
	kind, userID, err := skillScope(req.Msg.GetScope(), u)
	if err != nil {
		return nil, err
	}
	if kind == skills.KindLibrary {
		if err := access.RequireAdmin(ctx); err != nil {
			return nil, err
		}
	}
	root := skills.RootFor(s.DataDir(), kind, userID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	var got skills.InstallResult
	switch {
	case len(req.Msg.GetArchive()) > 0:
		if strings.TrimSpace(req.Msg.GetUrl()) != "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("url or archive, not both"))
		}
		got, err = skills.InstallArchive(root, req.Msg.GetArchive(), req.Msg.GetFilename())
	default:
		got, err = skills.InstallURL(root, req.Msg.GetUrl())
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&v1.InstallSkillResponse{Installed: got.Installed, Skipped: got.Skipped}), nil
}

func (s *Service) DeleteSkill(ctx context.Context, req *connect.Request[v1.DeleteSkillRequest]) (*connect.Response[v1.DeleteSkillResponse], error) {
	kind, userID, err := skillScope(req.Msg.GetScope(), access.User(ctx))
	if err != nil {
		return nil, err
	}
	if kind == skills.KindLibrary {
		if err := access.RequireAdmin(ctx); err != nil {
			return nil, err
		}
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if err := skills.Delete(skills.RootFor(s.DataDir(), kind, userID), name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	q := s.db.Where("kind = ? AND name = ?", kind, name)
	if kind == skills.KindPersonal {
		q = q.Where("bot_id IN (?)", s.db.Model(&db.Bot{}).Select("id").Where("user_id = ?", userID))
	}
	q.Delete(&db.BotSkill{})
	return connect.NewResponse(&v1.DeleteSkillResponse{}), nil
}

func (s *Service) ListBotSkills(ctx context.Context, req *connect.Request[v1.ListBotSkillsRequest]) (*connect.Response[v1.ListBotSkillsResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	s.EnsureDefaults(b.ID)
	out := &v1.ListBotSkillsResponse{}
	out.Skills = append(out.Skills, s.botSkillRows(b, skills.KindLibrary, "")...)
	out.Skills = append(out.Skills, s.botSkillRows(b, skills.KindPersonal, b.UserID)...)
	return connect.NewResponse(out), nil
}

func (s *Service) botSkillRows(b *db.Bot, kind, userID string) []*v1.BotSkill {
	root := skills.RootFor(s.DataDir(), kind, userID)
	rows, err := skills.List(root, kind)
	if err != nil {
		return nil
	}
	on := map[string]bool{}
	var stored []db.BotSkill
	s.db.Where("bot_id = ? AND kind = ?", b.ID, kind).Find(&stored)
	for _, r := range stored {
		on[r.Name] = r.Enabled
	}
	var out []*v1.BotSkill
	for _, r := range rows {
		out = append(out, &v1.BotSkill{
			Name: r.Name, Description: r.Description, Kind: kind,
			Enabled: on[r.Name], Source: r.Source,
		})
	}
	return out
}

func (s *Service) SetBotSkill(ctx context.Context, req *connect.Request[v1.SetBotSkillRequest]) (*connect.Response[v1.BotSkill], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(req.Msg.GetKind()))
	name := strings.TrimSpace(req.Msg.GetName())
	if kind != skills.KindLibrary && kind != skills.KindPersonal {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("kind is library or personal"))
	}
	if err := skills.ValidName(name); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	userID := ""
	if kind == skills.KindPersonal {
		userID = b.UserID
	}
	root := skills.RootFor(s.DataDir(), kind, userID)
	info, err := skills.Load(filepath.Join(root, name))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("skill not found"))
	}
	if req.Msg.GetEnabled() {
		var clash db.BotSkill
		s.db.Where("bot_id = ? AND name = ? AND enabled = ? AND NOT (kind = ?)", b.ID, name, true, kind).Limit(1).Find(&clash)
		if clash.ID != "" {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("%s is already enabled from %s", name, clash.Kind))
		}
	}
	var row db.BotSkill
	s.db.Where("bot_id = ? AND kind = ? AND name = ?", b.ID, kind, name).Limit(1).Find(&row)
	if row.ID == "" {
		row = db.BotSkill{ID: ids.New(), BotID: b.ID, Kind: kind, Name: name, Enabled: req.Msg.GetEnabled(), CreatedAt: time.Now()}
		if err := s.db.Create(&row).Error; err != nil {
			return nil, err
		}
	} else {
		row.Enabled = req.Msg.GetEnabled()
		if err := s.db.Save(&row).Error; err != nil {
			return nil, err
		}
	}
	go s.Push(b.ID)
	return connect.NewResponse(&v1.BotSkill{
		Name: info.Name, Description: info.Description, Kind: kind, Enabled: row.Enabled, Source: info.Source,
	}), nil
}

const skillBrowseLimit = 2 << 20

func (s *Service) skillRoot(ctx context.Context, scope, name string) (root string, err error) {
	kind, userID, err := skillScope(scope, access.User(ctx))
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if err := skills.ValidName(name); err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, err)
	}
	root = skills.RootFor(s.DataDir(), kind, userID)
	if !skills.Exists(root, name) {
		return "", connect.NewError(connect.CodeNotFound, errors.New("skill not found"))
	}
	return root, nil
}

func (s *Service) ListSkillFiles(ctx context.Context, req *connect.Request[v1.ListSkillFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	root, err := s.skillRoot(ctx, req.Msg.GetScope(), req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	ents, err := skills.ListRel(root, req.Msg.GetName(), req.Msg.GetPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, err
	}
	out := &v1.ListFilesResponse{}
	for _, e := range ents {
		out.Entries = append(out.Entries, &v1.FileEntry{
			Name: e.Name, Path: e.Path, Dir: e.Dir, Size: e.Size,
			Modified: e.Modified.Format(time.RFC3339),
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ReadSkillFile(ctx context.Context, req *connect.Request[v1.ReadSkillFileRequest]) (*connect.Response[v1.ReadFileResponse], error) {
	root, err := s.skillRoot(ctx, req.Msg.GetScope(), req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	st, full, err := skills.StatRel(root, req.Msg.GetName(), req.Msg.GetPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, skillBrowseLimit+1)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	raw := buf[:n]
	trunc := st.Size() > int64(len(raw)) || n > skillBrowseLimit
	if n > skillBrowseLimit {
		raw = raw[:skillBrowseLimit]
	}
	out := &v1.ReadFileResponse{Name: filepath.Base(full), Size: st.Size(), Truncated: trunc, Data: raw}
	if bytes.IndexByte(raw, 0) >= 0 {
		out.Binary = true
	} else {
		out.Content = textx.ValidUTF8(string(raw))
	}
	return connect.NewResponse(out), nil
}

// EnsureDefaults enables the catalog's default skills on a Bot that has not
// seen them yet.
func (s *Service) EnsureDefaults(botID string) {
	if s.db == nil || s.DataDir() == "" {
		return
	}
	root := skills.LibraryDir(s.DataDir())
	for _, name := range catalog.DefaultSkills {
		if !skills.Exists(root, name) {
			continue
		}
		var n int64
		s.db.Model(&db.BotSkill{}).Where("bot_id = ? AND kind = ? AND name = ?", botID, skills.KindLibrary, name).Count(&n)
		if n > 0 {
			continue
		}
		_ = s.db.Create(&db.BotSkill{
			ID: ids.New(), BotID: botID, Kind: skills.KindLibrary, Name: name, Enabled: true, CreatedAt: time.Now(),
		}).Error
	}
}

// Enabled lists the skills enabled on a Bot that load from disk.
func (s *Service) Enabled(botID string) []skills.Info {
	var bot db.Bot
	if s.db.First(&bot, "id = ?", botID).Error != nil {
		return nil
	}
	s.EnsureDefaults(botID)
	var rows []db.BotSkill
	s.db.Where("bot_id = ? AND enabled = ?", botID, true).Order("name").Find(&rows)
	var out []skills.Info
	seen := map[string]bool{}
	for _, r := range rows {
		userID := ""
		if r.Kind == skills.KindPersonal {
			userID = bot.UserID
		}
		info, err := skills.Load(filepath.Join(skills.RootFor(s.DataDir(), r.Kind, userID), r.Name))
		if err != nil {
			continue
		}
		if seen[info.Name] {
			continue
		}
		seen[info.Name] = true
		info.Kind = r.Kind
		out = append(out, info)
	}
	return out
}

// Prompt is the session-tier system-prompt note listing the enabled skills.
func Prompt(list []skills.Info) string {
	var b strings.Builder
	b.WriteString("Load with `skill` when the task matches. Instructions are not in this prompt. Scripts and extras are at `/opt/silo/skills/<name>/`.\n")
	if len(list) == 0 {
		b.WriteString("None enabled.\n")
		return b.String()
	}
	for _, sk := range list {
		fmt.Fprintf(&b, "- `%s` — %s\n", sk.Name, sk.Description)
	}
	return b.String()
}

// Push syncs the Bot's enabled skills into its machine.
func (s *Service) Push(botID string) {
	xs := s.Enabled(botID)
	var files []*v1.SkillFile
	for _, s := range xs {
		tree, err := skills.WalkFiles(s.Dir)
		if err != nil {
			log.Printf("walk skill %s: %v", s.Name, err)
			continue
		}
		for _, f := range tree {
			files = append(files, &v1.SkillFile{Path: s.Name + "/" + f.Path, Data: f.Data})
		}
	}
	cmd := &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_SyncSkills{SyncSkills: &v1.SyncSkillsCmd{Files: files}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.hub.Exec(ctx, botID, cmd); err != nil {
		log.Printf("sync skills bot=%s: %v", botID, err)
	}
}

// Read is the `skill` tool: a skill's SKILL.md or one of its files, for a skill
// enabled on this Bot.
func (s *Service) Read(botID, name, rel string) (string, error) {
	name = strings.TrimSpace(name)
	if err := skills.ValidName(name); err != nil {
		return "", err
	}
	var found *skills.Info
	for _, s := range s.Enabled(botID) {
		if s.Name == name {
			s := s
			found = &s
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("skill %s is not enabled on this Bot", name)
	}
	b, err := skills.ReadRel(filepath.Dir(found.Dir), name, rel)
	if err != nil {
		return "", err
	}
	note := fmt.Sprintf("Scripts and extras: /opt/silo/skills/%s/", name)
	return string(b) + "\n\n" + note, nil
}

// Propose installs a workspace folder holding a SKILL.md as a personal skill
// of the Bot's owner and enables it on the Bot.
func (s *Service) Propose(ctx context.Context, bot *db.Bot, rel string) (string, error) {
	rel = strings.TrimSpace(strings.TrimPrefix(rel, "/"))
	if rel == "" {
		return "", errors.New("path required")
	}
	files, err := s.PullTree(ctx, bot.ID, rel)
	if err != nil {
		return "", err
	}
	md, ok := files["SKILL.md"]
	if !ok {
		return "", errors.New("directory must contain SKILL.md")
	}
	m, err := skills.Parse(string(md))
	if err != nil {
		return "", err
	}
	root := skills.PersonalDir(s.DataDir(), bot.UserID)
	if skills.Exists(root, m.Name) {
		return "", fmt.Errorf("personal skill %s already exists", m.Name)
	}
	dst := filepath.Join(root, m.Name)
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return "", err
	}
	for p, data := range files {
		target := filepath.Join(dst, filepath.FromSlash(p))
		if strings.Contains(p, "..") {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	if err := skills.MatchDir(m.Name, m.Name); err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	info, err := skills.Load(dst)
	if err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	var clash db.BotSkill
	s.db.Where("bot_id = ? AND name = ? AND enabled = ?", bot.ID, info.Name, true).Limit(1).Find(&clash)
	if clash.ID != "" && clash.Kind != skills.KindPersonal {
		return fmt.Sprintf("installed personal skill %s; not enabled (name already on from %s)", info.Name, clash.Kind), nil
	}
	row := db.BotSkill{ID: ids.New(), BotID: bot.ID, Kind: skills.KindPersonal, Name: info.Name, Enabled: true, CreatedAt: time.Now()}
	if err := s.db.Create(&row).Error; err != nil {
		return "", err
	}
	go s.Push(bot.ID)
	return "installed personal skill " + info.Name, nil
}

// Proposal describes a workspace folder offered as a skill.
type Proposal struct {
	Name        string
	Description string
	Path        string
	Files       string
}

// Peek reads a workspace folder as a skill proposal without installing it.
func (s *Service) Peek(ctx context.Context, botID, rel string) (Proposal, error) {
	rel = strings.TrimSpace(strings.TrimPrefix(rel, "/"))
	if rel == "" {
		return Proposal{}, errors.New("path required")
	}
	names, err := s.listWorkspaceTreeNames(ctx, botID, rel)
	if err != nil {
		return Proposal{}, err
	}
	hasMD := false
	for _, n := range names {
		if n == "SKILL.md" {
			hasMD = true
			break
		}
	}
	if !hasMD {
		return Proposal{}, errors.New("directory must contain SKILL.md")
	}
	raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: rel + "/SKILL.md"}}})
	if err != nil {
		return Proposal{}, err
	}
	var row struct {
		Content string `json:"content"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return Proposal{}, err
	}
	body := row.Content
	if body == "" && row.Data != "" {
		b, err := base64.StdEncoding.DecodeString(row.Data)
		if err != nil {
			return Proposal{}, err
		}
		body = string(b)
	}
	m, err := skills.Parse(body)
	if err != nil {
		return Proposal{}, err
	}
	return Proposal{Name: m.Name, Description: m.Description, Path: rel, Files: strings.Join(names, "\n")}, nil
}

func (s *Service) listWorkspaceTreeNames(ctx context.Context, botID, rel string) ([]string, error) {
	var names []string
	var walk func(string) error
	walk = func(dir string) error {
		raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_DirList{DirList: &v1.DirListCmd{Path: dir}}})
		if err != nil {
			return err
		}
		var ents []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Dir  bool   `json:"dir"`
		}
		if err := json.Unmarshal([]byte(raw), &ents); err != nil {
			return err
		}
		if len(names)+len(ents) > skills.MaxExtractFiles {
			return errors.New("too many files")
		}
		for _, e := range ents {
			child := e.Path
			if child == "" {
				child = strings.TrimSuffix(dir, "/") + "/" + e.Name
			}
			if e.Dir {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			relPath, err := filepath.Rel(rel, child)
			if err != nil {
				relPath = e.Name
			}
			names = append(names, filepath.ToSlash(relPath))
		}
		return nil
	}
	if err := walk(rel); err != nil {
		return nil, err
	}
	return names, nil
}

// PullTree downloads a workspace folder's files (path to bytes) from the Bot.
func (s *Service) PullTree(ctx context.Context, botID, rel string) (map[string][]byte, error) {
	out := map[string][]byte{}
	var walk func(string) error
	walk = func(dir string) error {
		raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_DirList{DirList: &v1.DirListCmd{Path: dir}}})
		if err != nil {
			return err
		}
		var ents []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Dir  bool   `json:"dir"`
		}
		if err := json.Unmarshal([]byte(raw), &ents); err != nil {
			return err
		}
		if len(out)+len(ents) > skills.MaxExtractFiles {
			return errors.New("too many files")
		}
		for _, e := range ents {
			child := e.Path
			if child == "" {
				child = strings.TrimSuffix(dir, "/") + "/" + e.Name
			}
			if e.Dir {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			view, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: child}}})
			if err != nil {
				return err
			}
			var row struct {
				Data string `json:"data"`
			}
			if err := json.Unmarshal([]byte(view), &row); err != nil {
				return err
			}
			b, err := base64.StdEncoding.DecodeString(row.Data)
			if err != nil {
				return err
			}
			relPath, err := filepath.Rel(rel, child)
			if err != nil {
				relPath = e.Name
			}
			out[filepath.ToSlash(relPath)] = b
		}
		return nil
	}
	if err := walk(rel); err != nil {
		return nil, err
	}
	return out, nil
}
