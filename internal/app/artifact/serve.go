package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"silo.agent/internal/app/access"
	"silo.agent/internal/app/space"
	"silo.agent/internal/app/workspace"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

// ServeHTTP answers the console's downloads (cookie auth):
//
//	/artifacts/download?bot_id&artifact_id&v  one version: a file, or a folder as a zip
//	/artifacts/skill.zip?scope&space_id&name  a library or Workspace skill
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u, err := siloauth.UserFromRequest(s.db, r, s.cfg().HostCookies())
	if err != nil {
		access.HTTPSessionError(w, err)
		return
	}
	switch strings.Trim(strings.TrimPrefix(r.URL.Path, "/artifacts/"), "/") {
	case "download":
		s.downloadVersion(w, r, u)
	case "skill.zip":
		s.downloadSkillZip(w, r, u)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) downloadVersion(w http.ResponseWriter, r *http.Request, u *db.User) {
	q := r.URL.Query()
	ctx := access.WithUser(r.Context(), u)
	b, err := access.Bot(ctx, s.db, strings.TrimSpace(q.Get("bot_id")), access.Use)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	n, _ := strconv.Atoi(q.Get("v"))
	art, v, err := s.version(b.ID, q.Get("artifact_id"), n)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if v.Kind == "tree" {
		files, err := s.versionTree(ctx, art, v)
		if err != nil {
			http.Error(w, err.Error(), statusOf(err))
			return
		}
		name := v.Name
		if art.Type == TypeSkill {
			name = skillName(files, name)
		}
		writeZip(w, name, files)
		return
	}
	name, data, err := s.versionFile(ctx, art, v)
	if err != nil {
		http.Error(w, err.Error(), statusOf(err))
		return
	}
	serveBytes(w, name, data, false)
}

// versionFile is a file version's bytes: pinned, or the live file when it was
// too large to keep.
func (s *Service) versionFile(ctx context.Context, art *db.Artifact, v *db.ArtifactVersion) (string, []byte, error) {
	if v.Blob == "" {
		f, err := s.ws.Read(ctx, art.BotID, art.Path, workspace.PresentLimit)
		return f.Name, f.Data, err
	}
	return s.fileOf(ctx, v, "")
}

// versionTree is a folder version's files: pinned, or the live folder.
func (s *Service) versionTree(ctx context.Context, art *db.Artifact, v *db.ArtifactVersion) (map[string][]byte, error) {
	if v.Gone {
		return nil, ErrGone
	}
	if v.Blob == "" {
		return s.skills.PullTree(ctx, art.BotID, art.Path)
	}
	return s.treeFiles(ctx, art.BotID, v.Blob)
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, ErrGone), errors.Is(err, errNoFile), errors.Is(err, errNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrAltered):
		return http.StatusConflict
	}
	return http.StatusBadGateway
}

// serveBytes writes a file the Bot made. Only a type that cannot act as a page
// opens in place; the rest is a download whatever the link asks for.
func serveBytes(w http.ResponseWriter, name string, data []byte, inline bool) {
	ct := workspace.MIME(name)
	if ct == "" {
		ct = "application/octet-stream"
	}
	disp := "attachment"
	if inline && workspace.Passive(ct) {
		disp = "inline"
	}
	workspace.Untrusted(w.Header(), ct)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", disp+`; filename="`+safeFilename(name)+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data)
}

func (s *Service) downloadSkillZip(w http.ResponseWriter, r *http.Request, u *db.User) {
	q := r.URL.Query()
	name := strings.TrimSpace(q.Get("name"))
	scope := strings.ToLower(strings.TrimSpace(q.Get("scope")))
	if scope == "" {
		scope = skills.KindSpace
	}
	if scope == "personal" {
		// A link from before Workspaces (iOS): the user's own first one.
		scope = skills.KindSpace
		if q.Get("space_id") == "" {
			q.Set("space_id", space.HomeID(s.db, u.ID))
		}
	}
	if scope != skills.KindLibrary && scope != skills.KindSpace {
		http.Error(w, "scope is library or space", http.StatusBadRequest)
		return
	}
	spaceID := ""
	if scope == skills.KindSpace {
		sp, _, err := access.Space(access.WithUser(r.Context(), u), s.db, q.Get("space_id"), access.View)
		if err != nil {
			http.Error(w, "skill not found", http.StatusNotFound)
			return
		}
		spaceID = sp.ID
	}
	root := skills.RootFor(s.skills.DataDir(), scope, spaceID)
	if !skills.Exists(root, name) {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	walked, err := skills.WalkFiles(filepath.Join(root, name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	files := map[string][]byte{}
	for _, f := range walked {
		files[f.Path] = f.Data
	}
	writeZip(w, name, files)
}

// skillName is the name a skill's files give themselves, else fallback.
func skillName(files map[string][]byte, fallback string) string {
	if md, ok := files["SKILL.md"]; ok {
		if m, err := skills.Parse(string(md)); err == nil && m.Name != "" {
			return m.Name
		}
	}
	if fallback == "" {
		return "skill"
	}
	return fallback
}

var errTooBig = errors.New("too large to download")

// zipTree packs files (path to bytes) into a zip, in path order, under the
// skill-install caps; a path that could climb out is left out.
func zipTree(out io.Writer, files map[string][]byte) error {
	total := 0
	for _, b := range files {
		total += len(b)
	}
	if len(files) == 0 {
		return errors.New("nothing to download")
	}
	if len(files) > skills.MaxExtractFiles || total > skills.MaxExtractBytes {
		return errTooBig
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	zw := zip.NewWriter(out)
	for _, k := range keys {
		if !safeRel(k) {
			continue
		}
		hdr := &zip.FileHeader{Name: k, Method: zip.Deflate}
		hdr.SetMode(0o644)
		part, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := part.Write(files[k]); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeZip(w http.ResponseWriter, name string, files map[string][]byte) {
	var buf bytes.Buffer
	if err := zipTree(&buf, files); err != nil {
		code := http.StatusNotFound
		if errors.Is(err, errTooBig) {
			code = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(name)+`.zip"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(buf.Bytes())
}

// Attachment is the newest version of the chat's card for a path as a channel
// attachment: the pinned bytes, a folder zipped (a one-file app is its file).
func (s *Service) Attachment(ctx context.Context, botID, chatID, rel string) (channels.Attachment, Info, error) {
	var art db.Artifact
	if res := s.db.Where("bot_id = ? AND chat_id = ? AND path = ?", botID, chatID, workspace.Rel(rel)).Limit(1).Find(&art); res.Error != nil || res.RowsAffected == 0 {
		return channels.Attachment{}, Info{}, errNotFound
	}
	_, v, err := s.version(botID, art.ID, 0)
	if err != nil {
		return channels.Attachment{}, Info{}, err
	}
	info := Info{Type: art.Type, Title: art.Title, Name: v.Name}
	if v.Kind != "tree" {
		name, data, err := s.versionFile(ctx, &art, v)
		if err != nil {
			return channels.Attachment{}, info, err
		}
		f := workspace.File{Name: name}
		return channels.Attachment{Name: name, Mime: f.Mime(), Data: data}, info, nil
	}
	files, err := s.versionTree(ctx, &art, v)
	if err != nil {
		return channels.Attachment{}, info, err
	}
	if len(files) == 1 && v.Entry != "" {
		if b, ok := files[v.Entry]; ok {
			return channels.Attachment{Name: path.Base(v.Entry), Mime: "text/html", Data: b}, info, nil
		}
	}
	name := v.Name
	if art.Type == TypeSkill {
		name = skillName(files, titleOr(art.Title, name))
		info.Name = name
	}
	var buf bytes.Buffer
	if err := zipTree(&buf, files); err != nil {
		return channels.Attachment{}, info, err
	}
	return channels.Attachment{Name: safeFilename(name) + ".zip", Mime: "application/zip", Data: buf.Bytes()}, info, nil
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
