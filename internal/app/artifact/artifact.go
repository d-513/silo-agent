// Package artifact is what a Bot hands the human in the thread: a file to
// download or preview, or a skill to save. It describes a workspace path as a
// card, emits it, serves the download endpoints (cookie auth) and runs the
// "Save skill" action that copies a proposed skill to the owner's personal
// library.
package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/skill"
	"silo.agent/internal/app/workspace"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/channels"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

// Host is what artifacts need from the App around them.
type Host interface {
	host.Emitter
	host.Runs
}

// Service describes, emits and serves artifacts.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	ws     *workspace.Service
	skills *skill.Service
	host   Host
}

func New(gdb *gorm.DB, cfg func() config.Config, ws *workspace.Service, sk *skill.Service, h Host) *Service {
	return &Service{db: gdb, cfg: cfg, ws: ws, skills: sk, host: h}
}

// Info is the JSON carried in a `artifact` RunEvent. A skill is an
// installable deliverable (Save skill copies it to personal); a file is a
// download with a preview. `present` is separate — it shows a file inline and
// never produces a card.
type Info struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Path   string `json:"path"`
	Scope  string `json:"scope"`
	Status string `json:"status"`
	Size   int64  `json:"size,omitempty"`
}

func artifactJSON(info Info) string {
	b, _ := json.Marshal(info)
	return string(b)
}

func (s *Service) emit(botID, runID string, info Info) {
	s.host.Emit(botID, s.host.ChatOfRun(runID), runID, "artifact", artifactJSON(info), "")
}

func titleOr(title, fallback string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return fallback
}

// Describe resolves a workspace path into a skill (directory with
// SKILL.md) or a file. The file branch is tried first because it is a cheap
// stat; a directory then falls through to the skill peek.
func (s *Service) Describe(ctx context.Context, botID, rel, kind, title string) (Info, error) {
	rel = strings.TrimSpace(strings.TrimPrefix(rel, "/"))
	if rel == "" {
		return Info{}, errors.New("path required")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "" && kind != "skill" && kind != "file" {
		return Info{}, errors.New("kind is skill or file")
	}

	var fileErr error
	if kind == "" || kind == "file" {
		info, err := s.fileArtifact(ctx, botID, rel, title)
		if err == nil {
			return info, nil
		}
		if kind == "file" {
			return Info{}, err
		}
		fileErr = err
	}

	if kind == "" || kind == "skill" {
		peek, err := s.skills.Peek(ctx, botID, rel)
		if err == nil {
			return Info{
				Type: "skill", Name: peek.Name, Title: titleOr(title, peek.Name),
				Path: peek.Path, Scope: "workspace", Status: "pending",
			}, nil
		}
		if kind == "skill" {
			return Info{}, err
		}
		if fileErr != nil && !strings.Contains(fileErr.Error(), "directory") {
			return Info{}, fileErr
		}
		return Info{}, err
	}
	return Info{}, fileErr
}

func (s *Service) fileArtifact(ctx context.Context, botID, rel, title string) (Info, error) {
	raw, err := s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{
		BrowseFile: &v1.BrowseFileCmd{Path: rel, Limit: 1},
	}})
	if err != nil {
		return Info{}, err
	}
	var row struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return Info{}, err
	}
	name := row.Name
	if name == "" {
		name = path.Base(rel)
	}
	return Info{
		Type: "file", Name: name, Title: titleOr(title, name),
		Path: rel, Scope: "workspace", Status: "ready", Size: row.Size,
	}, nil
}

// Tool is the shared engine behind the chat `artifact` tool and the Python
// `silo_runtime.artifact` / CallTool path. It emits the card and returns a
// short ack for the model.
func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID, argsJSON string) (string, error) {
	var args struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		Kind  string `json:"kind"`
	}
	if raw := strings.TrimSpace(argsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", errors.New("invalid args")
		}
	}
	info, err := s.Describe(ctx, bot.ID, args.Path, args.Kind, args.Title)
	if err != nil {
		return "", err
	}
	s.emit(bot.ID, runID, info)
	switch info.Type {
	case "skill":
		return "showed skill " + info.Name + " as an artifact. It is not installed until the human clicks Save skill.", nil
	default:
		return "showed " + info.Name + " as a downloadable artifact.", nil
	}
}

// --- download endpoints -----------------------------------------------------

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u, err := siloauth.UserFromRequest(s.db, r)
	if err != nil {
		access.HTTPSessionError(w, err)
		return
	}
	switch strings.Trim(strings.TrimPrefix(r.URL.Path, "/artifacts/"), "/") {
	case "file":
		s.downloadWorkspaceFile(w, r, u)
	case "skill.zip":
		s.downloadSkillZip(w, r, u)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) downloadWorkspaceFile(w http.ResponseWriter, r *http.Request, u *db.User) {
	rel := workspace.Rel(r.URL.Query().Get("path"))
	botID := strings.TrimSpace(r.URL.Query().Get("bot_id"))
	if botID == "" || rel == "" {
		http.Error(w, "bot_id and path required", http.StatusBadRequest)
		return
	}
	ctx := access.WithUser(r.Context(), u)
	b, err := access.OwnBot(ctx, s.db, botID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	raw, err := s.ws.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{
		BrowseFile: &v1.BrowseFileCmd{Path: rel, Limit: workspace.PresentLimit},
	}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var row struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		http.Error(w, "bad file", http.StatusInternalServerError)
		return
	}
	data, err := workspace.DecodeData(row.Data)
	if err != nil {
		http.Error(w, "bad file", http.StatusInternalServerError)
		return
	}
	if len(data) == 0 {
		data = []byte(row.Content)
	}
	name := row.Name
	if name == "" {
		name = path.Base(rel)
	}
	ct := workspace.MIME(name)
	if ct == "" {
		ct = "application/octet-stream"
	}
	disp := "attachment"
	if r.URL.Query().Get("inline") != "" {
		disp = "inline"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", disp+`; filename="`+safeFilename(name)+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data)
}

type zipEntry struct {
	Path string
	Data []byte
}

func (s *Service) downloadSkillZip(w http.ResponseWriter, r *http.Request, u *db.User) {
	q := r.URL.Query()
	var (
		files []zipEntry
		name  string
		total int
	)
	if p := strings.TrimSpace(q.Get("path")); p != "" {
		botID := strings.TrimSpace(q.Get("bot_id"))
		if botID == "" {
			http.Error(w, "bot_id required", http.StatusBadRequest)
			return
		}
		ctx := access.WithUser(r.Context(), u)
		b, err := access.OwnBot(ctx, s.db, botID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		tree, err := s.skills.PullTree(ctx, b.ID, p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		name = path.Base(strings.Trim(strings.TrimPrefix(p, "/"), "/"))
		if md, ok := tree["SKILL.md"]; ok {
			if m, err := skills.Parse(string(md)); err == nil && m.Name != "" {
				name = m.Name
			}
		}
		for k, v := range tree {
			total += len(v)
			files = append(files, zipEntry{Path: k, Data: v})
		}
	} else {
		name = strings.TrimSpace(q.Get("name"))
		scope := strings.ToLower(strings.TrimSpace(q.Get("scope")))
		if scope == "" {
			scope = skills.KindPersonal
		}
		if scope != skills.KindLibrary && scope != skills.KindPersonal {
			http.Error(w, "scope is library or personal", http.StatusBadRequest)
			return
		}
		userID := ""
		if scope == skills.KindPersonal {
			userID = u.ID
		}
		root := skills.RootFor(s.skills.DataDir(), scope, userID)
		if !skills.Exists(root, name) {
			http.Error(w, "skill not found", http.StatusNotFound)
			return
		}
		walked, err := skills.WalkFiles(filepath.Join(root, name))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, f := range walked {
			total += len(f.Data)
			files = append(files, zipEntry{Path: f.Path, Data: f.Data})
		}
	}
	if name == "" {
		name = "skill"
	}
	if len(files) == 0 {
		http.Error(w, "nothing to download", http.StatusNotFound)
		return
	}
	if len(files) > skills.MaxExtractFiles || total > skills.MaxExtractBytes {
		http.Error(w, "skill is too large to download", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(name)+`.zip"`)
	w.Header().Set("Cache-Control", "private, no-store")
	zw := zip.NewWriter(w)
	for _, f := range files {
		if strings.Contains(f.Path, "..") {
			continue
		}
		hdr := &zip.FileHeader{Name: f.Path, Method: zip.Deflate}
		hdr.SetMode(0o644)
		part, err := zw.CreateHeader(hdr)
		if err != nil {
			return
		}
		if _, err := part.Write(f.Data); err != nil {
			return
		}
	}
	_ = zw.Close()
}

// ZipAttachment packs a workspace skill directory into a zip attachment.
func (s *Service) ZipAttachment(ctx context.Context, botID, rel, title string) (channels.Attachment, error) {
	tree, err := s.skills.PullTree(ctx, botID, rel)
	if err != nil {
		return channels.Attachment{}, err
	}
	name := strings.TrimSpace(title)
	if md, ok := tree["SKILL.md"]; ok {
		if m, err := skills.Parse(string(md)); err == nil && m.Name != "" {
			name = m.Name
		}
	}
	if name == "" {
		name = path.Base(strings.Trim(strings.TrimPrefix(rel, "/"), "/"))
	}
	if name == "" {
		name = "skill"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for k, v := range tree {
		if strings.Contains(k, "..") {
			continue
		}
		hdr := &zip.FileHeader{Name: k, Method: zip.Deflate}
		hdr.SetMode(0o644)
		part, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = zw.Close()
			return channels.Attachment{}, err
		}
		if _, err := part.Write(v); err != nil {
			_ = zw.Close()
			return channels.Attachment{}, err
		}
	}
	if err := zw.Close(); err != nil {
		return channels.Attachment{}, err
	}
	return channels.Attachment{Name: safeFilename(name) + ".zip", Mime: "application/zip", Data: buf.Bytes()}, nil
}

func safeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), ". ")
	if out == "" {
		return "artifact"
	}
	return out
}

// SaveSkill is the Save skill action on a skill card: it installs the proposed
// workspace folder as a personal skill and re-emits the card as saved.
func (s *Service) SaveSkill(ctx context.Context, req *connect.Request[v1.SaveSkillRequest]) (*connect.Response[v1.SaveSkillResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(strings.TrimPrefix(req.Msg.GetPath(), "/"))
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	peek, err := s.skills.Peek(ctx, b.ID, path)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, err := s.skills.Propose(ctx, b, path); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	runID := strings.TrimSpace(req.Msg.GetRunId())
	if runID != "" {
		s.emit(b.ID, runID, Info{
			Type: "skill", Name: peek.Name, Title: peek.Name, Path: peek.Path,
			Scope: skills.KindPersonal, Status: "saved",
		})
	}
	return connect.NewResponse(&v1.SaveSkillResponse{Name: peek.Name}), nil
}
