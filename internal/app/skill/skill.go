// Package skill is the skills the Bots draw on: skill bodies on the control
// plane's disk (the site-wide library, and each Workspace's own), which ones
// each Bot has enabled, the install, delete and file-browse RPCs, syncing the
// enabled set into the Bot's machine, and saving a Bot's skill card into its
// Workspace.
package skill

import (
	"bytes"
	"context"
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
	"silo.agent/internal/app/space"
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

// scope is where a request's skills live: the library (anyone reads it, an
// admin changes it) or a Workspace the caller has need in. spaceID is the
// Workspace's id, empty for the library; role is the caller's there.
type scope struct {
	kind, spaceID, role string
}

func (s *Service) skillScope(ctx context.Context, name, spaceID string, need access.SpaceNeed) (scope, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "", skills.KindLibrary:
		return scope{kind: skills.KindLibrary}, nil
	case skills.KindSpace, "personal":
		// "personal" is a client from before Workspaces (iOS): the caller's
		// own first Workspace.
		if name == "personal" && spaceID == "" {
			if u := access.User(ctx); u != nil {
				spaceID = space.HomeID(s.db, u.ID)
			}
		}
		sp, role, err := access.Space(ctx, s.db, spaceID, need)
		if err != nil {
			return scope{}, err
		}
		return scope{kind: skills.KindSpace, spaceID: sp.ID, role: role}, nil
	default:
		return scope{}, connect.NewError(connect.CodeInvalidArgument, errors.New("scope is library or space"))
	}
}

// root is the directory the scope's skills are in.
func (s *Service) root(sc scope) string {
	return skills.RootFor(s.DataDir(), sc.kind, sc.spaceID)
}

func (s *Service) ListSkills(ctx context.Context, req *connect.Request[v1.ListSkillsRequest]) (*connect.Response[v1.ListSkillsResponse], error) {
	sc, err := s.skillScope(ctx, req.Msg.GetScope(), req.Msg.GetSpaceId(), access.Build)
	if err != nil {
		return nil, err
	}
	rows, err := skills.List(s.root(sc), sc.kind)
	if err != nil {
		return nil, err
	}
	installers := s.installers(sc.spaceID)
	userID := access.User(ctx).ID
	out := &v1.ListSkillsResponse{}
	for _, r := range rows {
		p := protoSkill(r)
		if sc.kind == skills.KindSpace {
			by := installers[r.Name]
			p.InstalledBy = by.email
			p.CanRemove = by.userID == userID || access.Rank(sc.role) >= access.Admin
		}
		out.Skills = append(out.Skills, p)
	}
	return connect.NewResponse(out), nil
}

type installer struct{ userID, email string }

// installers maps a Workspace's skills to who installed them.
func (s *Service) installers(spaceID string) map[string]installer {
	out := map[string]installer{}
	if spaceID == "" {
		return out
	}
	var rows []struct {
		Name, InstalledBy, Email string
	}
	s.db.Model(&db.SpaceSkill{}).Select("space_skills.name, space_skills.installed_by, users.email").
		Joins("LEFT JOIN users ON users.id = space_skills.installed_by").
		Where("space_skills.space_id = ?", spaceID).Scan(&rows)
	for _, r := range rows {
		out[r.Name] = installer{userID: r.InstalledBy, email: r.Email}
	}
	return out
}

func protoSkill(r skills.Info) *v1.Skill {
	return &v1.Skill{
		Name: r.Name, Description: r.Description, Kind: r.Kind,
		Source: r.Source, Seeded: r.Kind == skills.KindLibrary && catalog.SeededName(r.Name),
	}
}

func (s *Service) InstallSkill(ctx context.Context, req *connect.Request[v1.InstallSkillRequest]) (*connect.Response[v1.InstallSkillResponse], error) {
	u := access.User(ctx)
	sc, err := s.skillScope(ctx, req.Msg.GetScope(), req.Msg.GetSpaceId(), access.Build)
	if err != nil {
		return nil, err
	}
	if sc.kind == skills.KindLibrary {
		if err := access.RequireAdmin(ctx); err != nil {
			return nil, err
		}
	}
	root := s.root(sc)
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
		got, err = skills.InstallURL(root, req.Msg.GetUrl(), s.cfg().OutboundPolicy().Client(2*time.Minute))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if sc.kind == skills.KindSpace {
		// A new Workspace skill is on in every Bot its installer manages
		// there; each can switch it off.
		bots := s.managedIn(sc.spaceID, u.ID)
		for _, name := range got.Installed {
			s.recordInstall(sc.spaceID, name, u.ID)
			for _, b := range bots {
				s.enable(b, skills.KindSpace, name)
			}
		}
		for _, b := range bots {
			go s.Push(b)
		}
	}
	return connect.NewResponse(&v1.InstallSkillResponse{Installed: got.Installed, Skipped: got.Skipped}), nil
}

// recordInstall notes who installed a Workspace skill.
func (s *Service) recordInstall(spaceID, name, userID string) {
	s.db.Where("space_id = ? AND name = ?", spaceID, name).Delete(&db.SpaceSkill{})
	s.db.Create(&db.SpaceSkill{SpaceID: spaceID, Name: name, InstalledBy: userID, CreatedAt: time.Now()})
}

// managedIn is the ids of the Bots in the Workspace the user manages.
func (s *Service) managedIn(spaceID, userID string) []string {
	var ids []string
	s.db.Model(&db.Bot{}).Joins("JOIN bot_access a ON a.bot_id = bots.id").
		Where("bots.space_id = ? AND a.user_id = ? AND a.role = ?", spaceID, userID, access.RoleManage).
		Pluck("bots.id", &ids)
	return ids
}

// enable switches a skill on in a Bot, unless the Bot already has one of that
// name on from the other kind (a name is one skill in the prompt).
func (s *Service) enable(botID, kind, name string) {
	var clash int64
	s.db.Model(&db.BotSkill{}).Where("bot_id = ? AND name = ? AND enabled = ? AND kind <> ?", botID, name, true, kind).Count(&clash)
	if clash > 0 {
		return
	}
	var row db.BotSkill
	s.db.Where("bot_id = ? AND kind = ? AND name = ?", botID, kind, name).Limit(1).Find(&row)
	if row.ID != "" {
		s.db.Model(&row).Update("enabled", true)
		return
	}
	s.db.Create(&db.BotSkill{ID: ids.New(), BotID: botID, Kind: kind, Name: name, Enabled: true, CreatedAt: time.Now()})
}

// EnableSpace switches every skill of the Bot's Workspace on in a new Bot.
func (s *Service) EnableSpace(botID string) {
	var b db.Bot
	if s.db.Where("id = ?", botID).Limit(1).Find(&b); b.ID == "" || s.DataDir() == "" {
		return
	}
	rows, err := skills.List(skills.SpaceDir(s.DataDir(), b.SpaceID), skills.KindSpace)
	if err != nil {
		return
	}
	for _, r := range rows {
		s.enable(b.ID, skills.KindSpace, r.Name)
	}
}

// DropSpace deletes a Workspace's skills, for a Workspace that is going.
func (s *Service) DropSpace(spaceID string) {
	s.db.Where("space_id = ?", spaceID).Delete(&db.SpaceSkill{})
	if dir := s.DataDir(); dir != "" && spaceID != "" {
		_ = os.RemoveAll(skills.SpaceDir(dir, spaceID))
	}
}

func (s *Service) DeleteSkill(ctx context.Context, req *connect.Request[v1.DeleteSkillRequest]) (*connect.Response[v1.DeleteSkillResponse], error) {
	sc, err := s.skillScope(ctx, req.Msg.GetScope(), req.Msg.GetSpaceId(), access.Build)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if sc.kind == skills.KindLibrary {
		if err := access.RequireAdmin(ctx); err != nil {
			return nil, err
		}
	} else if access.Rank(sc.role) < access.Admin && s.installers(sc.spaceID)[name].userID != access.User(ctx).ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only whoever installed it, or an admin of the workspace, can remove this skill"))
	}
	if err := skills.Delete(s.root(sc), name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	q := s.db.Where("kind = ? AND name = ?", sc.kind, name)
	if sc.kind == skills.KindSpace {
		q = q.Where("bot_id IN (?)", s.db.Model(&db.Bot{}).Select("id").Where("space_id = ?", sc.spaceID))
		s.db.Where("space_id = ? AND name = ?", sc.spaceID, name).Delete(&db.SpaceSkill{})
	}
	var touched []string
	s.db.Model(&db.BotSkill{}).Where(q).Where("enabled = ?", true).Distinct().Pluck("bot_id", &touched)
	q.Delete(&db.BotSkill{})
	for _, b := range touched {
		go s.Push(b)
	}
	return connect.NewResponse(&v1.DeleteSkillResponse{}), nil
}

func (s *Service) ListBotSkills(ctx context.Context, req *connect.Request[v1.ListBotSkillsRequest]) (*connect.Response[v1.ListBotSkillsResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	s.EnsureDefaults(b.ID)
	out := &v1.ListBotSkillsResponse{}
	out.Skills = append(out.Skills, s.botSkillRows(b, skills.KindLibrary)...)
	out.Skills = append(out.Skills, s.botSkillRows(b, skills.KindSpace)...)
	return connect.NewResponse(out), nil
}

func (s *Service) botSkillRows(b *db.Bot, kind string) []*v1.BotSkill {
	root := skills.RootFor(s.DataDir(), kind, b.SpaceID)
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
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(req.Msg.GetKind()))
	name := strings.TrimSpace(req.Msg.GetName())
	if kind != skills.KindLibrary && kind != skills.KindSpace {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("kind is library or space"))
	}
	if err := skills.ValidName(name); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	root := skills.RootFor(s.DataDir(), kind, b.SpaceID)
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

// skillRoot is the directory of a skill the caller may read: the library's,
// or one in a Workspace they are in (a guest may look at what their Bots use).
func (s *Service) skillRoot(ctx context.Context, scopeName, spaceID, name string) (root string, err error) {
	sc, err := s.skillScope(ctx, scopeName, spaceID, access.View)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if err := skills.ValidName(name); err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, err)
	}
	root = s.root(sc)
	if !skills.Exists(root, name) {
		return "", connect.NewError(connect.CodeNotFound, errors.New("skill not found"))
	}
	return root, nil
}

func (s *Service) ListSkillFiles(ctx context.Context, req *connect.Request[v1.ListSkillFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	root, err := s.skillRoot(ctx, req.Msg.GetScope(), req.Msg.GetSpaceId(), req.Msg.GetName())
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
	root, err := s.skillRoot(ctx, req.Msg.GetScope(), req.Msg.GetSpaceId(), req.Msg.GetName())
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
		info, err := skills.Load(filepath.Join(skills.RootFor(s.DataDir(), r.Kind, bot.SpaceID), r.Name))
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

// InstallFiles installs a skill's files (path to bytes, SKILL.md at the top)
// as a skill of the Bot's Workspace, installed by userID, and enables it on
// the Bot.
func (s *Service) InstallFiles(bot *db.Bot, userID string, files map[string][]byte) (string, error) {
	md, ok := files["SKILL.md"]
	if !ok {
		return "", errors.New("directory must contain SKILL.md")
	}
	m, err := skills.Parse(string(md))
	if err != nil {
		return "", err
	}
	root := skills.SpaceDir(s.DataDir(), bot.SpaceID)
	if skills.Exists(root, m.Name) {
		return "", fmt.Errorf("the workspace already has a skill named %s", m.Name)
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
	s.recordInstall(bot.SpaceID, info.Name, userID)
	var clash db.BotSkill
	s.db.Where("bot_id = ? AND name = ? AND enabled = ?", bot.ID, info.Name, true).Limit(1).Find(&clash)
	if clash.ID != "" && clash.Kind != skills.KindSpace {
		return fmt.Sprintf("installed workspace skill %s; not enabled (name already on from %s)", info.Name, clash.Kind), nil
	}
	row := db.BotSkill{ID: ids.New(), BotID: bot.ID, Kind: skills.KindSpace, Name: info.Name, Enabled: true, CreatedAt: time.Now()}
	if err := s.db.Create(&row).Error; err != nil {
		return "", err
	}
	go s.Push(bot.ID)
	return "installed workspace skill " + info.Name, nil
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
	f, err := s.ws.Read(ctx, botID, rel+"/SKILL.md", 0)
	if err != nil {
		return Proposal{}, err
	}
	body := string(f.Data)
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
			f, err := s.ws.Read(ctx, botID, child, workspace.PresentLimit)
			if err != nil {
				return err
			}
			b := f.Data
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
