package app

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

	v1 "silo.agent/gen/silo/v1"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

// artifactInfo is the JSON carried in a `artifact` RunEvent. A skill is an
// installable deliverable (Save skill copies it to personal); a file is a
// download with a preview. `present` is separate — it shows a file inline and
// never produces a card.
type artifactInfo struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Path   string `json:"path"`
	Scope  string `json:"scope"`
	Status string `json:"status"`
	Size   int64  `json:"size,omitempty"`
}

func artifactJSON(info artifactInfo) string {
	b, _ := json.Marshal(info)
	return string(b)
}

func (a *App) emitArtifact(botID, runID string, info artifactInfo) {
	a.emit(botID, a.chatOfRun(runID), runID, "artifact", artifactJSON(info), "")
}

func titleOr(title, fallback string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return fallback
}

// describeArtifact resolves a workspace path into a skill (directory with
// SKILL.md) or a file. The file branch is tried first because it is a cheap
// stat; a directory then falls through to the skill peek.
func (a *App) describeArtifact(ctx context.Context, botID, rel, kind, title string) (artifactInfo, error) {
	rel = strings.TrimSpace(strings.TrimPrefix(rel, "/"))
	if rel == "" {
		return artifactInfo{}, errors.New("path required")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "" && kind != "skill" && kind != "file" {
		return artifactInfo{}, errors.New("kind is skill or file")
	}

	var fileErr error
	if kind == "" || kind == "file" {
		info, err := a.fileArtifact(ctx, botID, rel, title)
		if err == nil {
			return info, nil
		}
		if kind == "file" {
			return artifactInfo{}, err
		}
		fileErr = err
	}

	if kind == "" || kind == "skill" {
		peek, err := a.peekSkillProposal(ctx, botID, rel)
		if err == nil {
			return artifactInfo{
				Type: "skill", Name: peek.Name, Title: titleOr(title, peek.Name),
				Path: peek.Path, Scope: "workspace", Status: "pending",
			}, nil
		}
		if kind == "skill" {
			return artifactInfo{}, err
		}
		if fileErr != nil && !strings.Contains(fileErr.Error(), "directory") {
			return artifactInfo{}, fileErr
		}
		return artifactInfo{}, err
	}
	return artifactInfo{}, fileErr
}

func (a *App) fileArtifact(ctx context.Context, botID, rel, title string) (artifactInfo, error) {
	raw, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{
		BrowseFile: &v1.BrowseFileCmd{Path: rel, Limit: 1},
	}})
	if err != nil {
		return artifactInfo{}, err
	}
	var row struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return artifactInfo{}, err
	}
	name := row.Name
	if name == "" {
		name = path.Base(rel)
	}
	return artifactInfo{
		Type: "file", Name: name, Title: titleOr(title, name),
		Path: rel, Scope: "workspace", Status: "ready", Size: row.Size,
	}, nil
}

// artifact is the shared engine behind the chat `artifact` tool and the Python
// `silo_runtime.artifact` / CallTool path. It emits the card and returns a
// short ack for the model.
func (a *App) artifact(ctx context.Context, bot *db.Bot, runID, argsJSON string) (string, error) {
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
	info, err := a.describeArtifact(ctx, bot.ID, args.Path, args.Kind, args.Title)
	if err != nil {
		return "", err
	}
	a.emitArtifact(bot.ID, runID, info)
	switch info.Type {
	case "skill":
		return "showed skill " + info.Name + " as an artifact. It is not installed until the human clicks Save skill.", nil
	default:
		return "showed " + info.Name + " as a downloadable artifact.", nil
	}
}

// --- download endpoints -----------------------------------------------------

func (a *App) handleArtifactDownload(w http.ResponseWriter, r *http.Request) {
	u, err := siloauth.UserFromRequest(a.DB, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch strings.Trim(strings.TrimPrefix(r.URL.Path, "/artifacts/"), "/") {
	case "file":
		a.downloadWorkspaceFile(w, r, u)
	case "skill.zip":
		a.downloadSkillZip(w, r, u)
	default:
		http.NotFound(w, r)
	}
}

func (a *App) downloadWorkspaceFile(w http.ResponseWriter, r *http.Request, u *db.User) {
	rel := relWorkspace(r.URL.Query().Get("path"))
	botID := strings.TrimSpace(r.URL.Query().Get("bot_id"))
	if botID == "" || rel == "" {
		http.Error(w, "bot_id and path required", http.StatusBadRequest)
		return
	}
	ctx := context.WithValue(r.Context(), userKey, u)
	b, err := a.ownBot(ctx, botID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	raw, err := a.callWorker(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{
		BrowseFile: &v1.BrowseFileCmd{Path: rel, Limit: presentBrowseLimit},
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
	data, err := decodeFileData(row.Data)
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
	ct := artifactMIME(name)
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

func (a *App) downloadSkillZip(w http.ResponseWriter, r *http.Request, u *db.User) {
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
		ctx := context.WithValue(r.Context(), userKey, u)
		b, err := a.ownBot(ctx, botID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		tree, err := a.pullWorkspaceTree(ctx, b.ID, p)
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
		root := skills.RootFor(a.dataDir(), scope, userID)
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

// workspaceAttachment reads one workspace file as a channel attachment.
func (a *App) workspaceAttachment(ctx context.Context, botID, rel string) (channels.Attachment, error) {
	rel = relWorkspace(rel)
	if rel == "" {
		return channels.Attachment{}, errors.New("path required")
	}
	raw, err := a.callWorker(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{
		BrowseFile: &v1.BrowseFileCmd{Path: rel, Limit: presentBrowseLimit},
	}})
	if err != nil {
		return channels.Attachment{}, err
	}
	var row struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return channels.Attachment{}, err
	}
	data, err := decodeFileData(row.Data)
	if err != nil {
		return channels.Attachment{}, err
	}
	if len(data) == 0 {
		data = []byte(row.Content)
	}
	if len(data) == 0 {
		return channels.Attachment{}, errors.New("file is empty")
	}
	name := row.Name
	if name == "" {
		name = path.Base(rel)
	}
	mime := artifactMIME(name)
	if mime == "" {
		mime = "application/octet-stream"
	}
	return channels.Attachment{Name: name, Mime: mime, Data: data}, nil
}

// skillZipAttachment packs a workspace skill directory into a zip attachment.
func (a *App) skillZipAttachment(ctx context.Context, botID, rel, title string) (channels.Attachment, error) {
	tree, err := a.pullWorkspaceTree(ctx, botID, rel)
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

func artifactMIME(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".ogv":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".aac":
		return "audio/aac"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".zip":
		return "application/zip"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".tar":
		return "application/x-tar"
	case ".json":
		return "application/json"
	case ".csv":
		return "text/csv"
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt", ".log":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	default:
		return ""
	}
}
