package app

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

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/skills"
)

func (a *App) dataDir() string {
	return a.cfg().DataDir
}

func (a *App) SeedConnectors(ctx context.Context, _ *connect.Request[v1.SeedConnectorsRequest]) (*connect.Response[v1.SeedConnectorsResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := catalog.Seed(a.DB); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.SeedConnectorsResponse{}), nil
}

func (a *App) SeedSkills(ctx context.Context, _ *connect.Request[v1.SeedSkillsRequest]) (*connect.Response[v1.SeedSkillsResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := catalog.SeedSkills(a.dataDir()); err != nil {
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

func (a *App) ListSkills(ctx context.Context, req *connect.Request[v1.ListSkillsRequest]) (*connect.Response[v1.ListSkillsResponse], error) {
	kind, userID, err := skillScope(req.Msg.GetScope(), currentUser(ctx))
	if err != nil {
		return nil, err
	}
	root := skills.RootFor(a.dataDir(), kind, userID)
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

func (a *App) InstallSkill(ctx context.Context, req *connect.Request[v1.InstallSkillRequest]) (*connect.Response[v1.InstallSkillResponse], error) {
	u := currentUser(ctx)
	kind, userID, err := skillScope(req.Msg.GetScope(), u)
	if err != nil {
		return nil, err
	}
	if kind == skills.KindLibrary {
		if err := requireAdmin(ctx); err != nil {
			return nil, err
		}
	}
	root := skills.RootFor(a.dataDir(), kind, userID)
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

func (a *App) DeleteSkill(ctx context.Context, req *connect.Request[v1.DeleteSkillRequest]) (*connect.Response[v1.DeleteSkillResponse], error) {
	kind, userID, err := skillScope(req.Msg.GetScope(), currentUser(ctx))
	if err != nil {
		return nil, err
	}
	if kind == skills.KindLibrary {
		if err := requireAdmin(ctx); err != nil {
			return nil, err
		}
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if err := skills.Delete(skills.RootFor(a.dataDir(), kind, userID), name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	q := a.DB.Where("kind = ? AND name = ?", kind, name)
	if kind == skills.KindPersonal {
		q = q.Where("bot_id IN (?)", a.DB.Model(&db.Bot{}).Select("id").Where("user_id = ?", userID))
	}
	q.Delete(&db.BotSkill{})
	return connect.NewResponse(&v1.DeleteSkillResponse{}), nil
}

func (a *App) ListBotSkills(ctx context.Context, req *connect.Request[v1.ListBotSkillsRequest]) (*connect.Response[v1.ListBotSkillsResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	a.ensureDefaultSkill(b.ID)
	out := &v1.ListBotSkillsResponse{}
	out.Skills = append(out.Skills, a.botSkillRows(b, skills.KindLibrary, "")...)
	out.Skills = append(out.Skills, a.botSkillRows(b, skills.KindPersonal, b.UserID)...)
	return connect.NewResponse(out), nil
}

func (a *App) botSkillRows(b *db.Bot, kind, userID string) []*v1.BotSkill {
	root := skills.RootFor(a.dataDir(), kind, userID)
	rows, err := skills.List(root, kind)
	if err != nil {
		return nil
	}
	on := map[string]bool{}
	var stored []db.BotSkill
	a.DB.Where("bot_id = ? AND kind = ?", b.ID, kind).Find(&stored)
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

func (a *App) SetBotSkill(ctx context.Context, req *connect.Request[v1.SetBotSkillRequest]) (*connect.Response[v1.BotSkill], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
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
	root := skills.RootFor(a.dataDir(), kind, userID)
	info, err := skills.Load(filepath.Join(root, name))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("skill not found"))
	}
	if req.Msg.GetEnabled() {
		var clash db.BotSkill
		a.DB.Where("bot_id = ? AND name = ? AND enabled = ? AND NOT (kind = ?)", b.ID, name, true, kind).Limit(1).Find(&clash)
		if clash.ID != "" {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("%s is already enabled from %s", name, clash.Kind))
		}
	}
	var row db.BotSkill
	a.DB.Where("bot_id = ? AND kind = ? AND name = ?", b.ID, kind, name).Limit(1).Find(&row)
	if row.ID == "" {
		row = db.BotSkill{ID: ids.New(), BotID: b.ID, Kind: kind, Name: name, Enabled: req.Msg.GetEnabled(), CreatedAt: time.Now()}
		if err := a.DB.Create(&row).Error; err != nil {
			return nil, err
		}
	} else {
		row.Enabled = req.Msg.GetEnabled()
		if err := a.DB.Save(&row).Error; err != nil {
			return nil, err
		}
	}
	go a.pushSkills(b.ID)
	return connect.NewResponse(&v1.BotSkill{
		Name: info.Name, Description: info.Description, Kind: kind, Enabled: row.Enabled, Source: info.Source,
	}), nil
}

const skillBrowseLimit = 2 << 20

func (a *App) skillRoot(ctx context.Context, scope, name string) (root string, err error) {
	kind, userID, err := skillScope(scope, currentUser(ctx))
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if err := skills.ValidName(name); err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, err)
	}
	root = skills.RootFor(a.dataDir(), kind, userID)
	if !skills.Exists(root, name) {
		return "", connect.NewError(connect.CodeNotFound, errors.New("skill not found"))
	}
	return root, nil
}

func (a *App) ListSkillFiles(ctx context.Context, req *connect.Request[v1.ListSkillFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	root, err := a.skillRoot(ctx, req.Msg.GetScope(), req.Msg.GetName())
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

func (a *App) ReadSkillFile(ctx context.Context, req *connect.Request[v1.ReadSkillFileRequest]) (*connect.Response[v1.ReadFileResponse], error) {
	root, err := a.skillRoot(ctx, req.Msg.GetScope(), req.Msg.GetName())
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
		out.Content = string(raw)
	}
	return connect.NewResponse(out), nil
}

func (a *App) SaveSkill(ctx context.Context, req *connect.Request[v1.SaveSkillRequest]) (*connect.Response[v1.SaveSkillResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(strings.TrimPrefix(req.Msg.GetPath(), "/"))
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	peek, err := a.peekSkillProposal(ctx, b.ID, path)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, err := a.proposeSkill(ctx, b, path); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	runID := strings.TrimSpace(req.Msg.GetRunId())
	if runID != "" {
		a.emitSkillArtifact(b.ID, runID, peek.Name, peek.Name, peek.Path, skills.KindPersonal, "saved")
	}
	return connect.NewResponse(&v1.SaveSkillResponse{Name: peek.Name}), nil
}

func (a *App) ensureDefaultSkill(botID string) {
	if a.DB == nil || a.dataDir() == "" {
		return
	}
	name := catalog.DefaultSkill
	if !skills.Exists(skills.LibraryDir(a.dataDir()), name) {
		return
	}
	var n int64
	a.DB.Model(&db.BotSkill{}).Where("bot_id = ? AND kind = ? AND name = ?", botID, skills.KindLibrary, name).Count(&n)
	if n > 0 {
		return
	}
	_ = a.DB.Create(&db.BotSkill{
		ID: ids.New(), BotID: botID, Kind: skills.KindLibrary, Name: name, Enabled: true, CreatedAt: time.Now(),
	}).Error
}

func (a *App) enabledSkills(botID string) []skills.Info {
	var bot db.Bot
	if a.DB.First(&bot, "id = ?", botID).Error != nil {
		return nil
	}
	a.ensureDefaultSkill(botID)
	var rows []db.BotSkill
	a.DB.Where("bot_id = ? AND enabled = ?", botID, true).Order("name").Find(&rows)
	var out []skills.Info
	seen := map[string]bool{}
	for _, r := range rows {
		userID := ""
		if r.Kind == skills.KindPersonal {
			userID = bot.UserID
		}
		info, err := skills.Load(filepath.Join(skills.RootFor(a.dataDir(), r.Kind, userID), r.Name))
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

func (a *App) skillBlurb(botID string) string {
	xs := a.enabledSkills(botID)
	var b strings.Builder
	b.WriteString("\n\n## Skills\n")
	b.WriteString("Load with `skill` when the task matches. Instructions are not in this prompt. Scripts and extras are at `/opt/silo/skills/<name>/`.\n")
	if len(xs) == 0 {
		b.WriteString("None enabled.\n")
		return b.String()
	}
	for _, s := range xs {
		fmt.Fprintf(&b, "- `%s` — %s\n", s.Name, s.Description)
	}
	return b.String()
}

func (a *App) pushSkills(botID string) {
	xs := a.enabledSkills(botID)
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
	if _, err := a.Hub.Exec(ctx, botID, cmd); err != nil {
		log.Printf("sync skills bot=%s: %v", botID, err)
	}
}

func (a *App) loadSkill(botID, name, rel string) (string, error) {
	name = strings.TrimSpace(name)
	if err := skills.ValidName(name); err != nil {
		return "", err
	}
	var found *skills.Info
	for _, s := range a.enabledSkills(botID) {
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

func (a *App) proposeSkill(ctx context.Context, bot *db.Bot, rel string) (string, error) {
	rel = strings.TrimSpace(strings.TrimPrefix(rel, "/"))
	if rel == "" {
		return "", errors.New("path required")
	}
	files, err := a.pullWorkspaceTree(ctx, bot.ID, rel)
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
	root := skills.PersonalDir(a.dataDir(), bot.UserID)
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
	a.DB.Where("bot_id = ? AND name = ? AND enabled = ?", bot.ID, info.Name, true).Limit(1).Find(&clash)
	if clash.ID != "" && clash.Kind != skills.KindPersonal {
		return fmt.Sprintf("installed personal skill %s; not enabled (name already on from %s)", info.Name, clash.Kind), nil
	}
	row := db.BotSkill{ID: ids.New(), BotID: bot.ID, Kind: skills.KindPersonal, Name: info.Name, Enabled: true, CreatedAt: time.Now()}
	if err := a.DB.Create(&row).Error; err != nil {
		return "", err
	}
	go a.pushSkills(bot.ID)
	return "installed personal skill " + info.Name, nil
}

type skillPeek struct {
	Name        string
	Description string
	Path        string
	Files       string
}

func skillArtifactJSON(name, title, path, scope, status string) string {
	b, _ := json.Marshal(map[string]string{
		"type": "skill", "name": name, "title": title, "path": path,
		"scope": scope, "status": status,
	})
	return string(b)
}

func (a *App) emitSkillArtifact(botID, runID, name, title, path, scope, status string) {
	a.emit(botID, a.chatOfRun(runID), runID, "artifact", skillArtifactJSON(name, title, path, scope, status), "")
}

func (a *App) peekSkillProposal(ctx context.Context, botID, rel string) (skillPeek, error) {
	rel = strings.TrimSpace(strings.TrimPrefix(rel, "/"))
	if rel == "" {
		return skillPeek{}, errors.New("path required")
	}
	names, err := a.listWorkspaceTreeNames(ctx, botID, rel)
	if err != nil {
		return skillPeek{}, err
	}
	hasMD := false
	for _, n := range names {
		if n == "SKILL.md" {
			hasMD = true
			break
		}
	}
	if !hasMD {
		return skillPeek{}, errors.New("directory must contain SKILL.md")
	}
	raw, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: rel + "/SKILL.md"}}})
	if err != nil {
		return skillPeek{}, err
	}
	var row struct {
		Content string `json:"content"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return skillPeek{}, err
	}
	body := row.Content
	if body == "" && row.Data != "" {
		b, err := base64.StdEncoding.DecodeString(row.Data)
		if err != nil {
			return skillPeek{}, err
		}
		body = string(b)
	}
	m, err := skills.Parse(body)
	if err != nil {
		return skillPeek{}, err
	}
	return skillPeek{Name: m.Name, Description: m.Description, Path: rel, Files: strings.Join(names, "\n")}, nil
}

func (a *App) listWorkspaceTreeNames(ctx context.Context, botID, rel string) ([]string, error) {
	var names []string
	var walk func(string) error
	walk = func(dir string) error {
		raw, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_DirList{DirList: &v1.DirListCmd{Path: dir}}})
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

func (a *App) pullWorkspaceTree(ctx context.Context, botID, rel string) (map[string][]byte, error) {
	out := map[string][]byte{}
	var walk func(string) error
	walk = func(dir string) error {
		raw, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_DirList{DirList: &v1.DirListCmd{Path: dir}}})
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
			view, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: child}}})
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
