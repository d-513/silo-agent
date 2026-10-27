package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"silo.agent/internal/app/access"
	"silo.agent/internal/app/tunnels"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/names"
)

// Live apps and shared links are served on the content host, never in the
// console's origin: app-<label> for the owner (a pass in the path, bound to
// their Silo session) and share-<label> for anyone with the link (a copy on
// the control plane, so it works while the Bot is stopped). Both answer with
// a sandbox policy: the page gets an opaque origin, so it has no cookies and
// no storage of anyone's, and without network (artifact.app_network) it can
// load only its own files.

const (
	viewTTL = 12 * time.Hour
	// viewPrefix heads every app path: /_v/<pass>/<file>.
	viewPrefix = "/_v/"
)

// contentOn reports whether the content host is configured.
func (s *Service) contentOn() bool { return s.cfg().ContentHost() != "" }

// appURL mints the owner's pass to one app version and returns its address.
func (s *Service) appURL(art *db.Artifact, v *db.ArtifactVersion, userID, sessionID string) (string, error) {
	cfg := s.cfg()
	if cfg.ContentHost() == "" {
		return "", errors.New("apps need a content host: the operator sets content.host")
	}
	tok := ids.Token()
	now := time.Now()
	s.db.Where("expires_at < ?", now).Delete(&db.ArtifactView{})
	if err := s.db.Create(&db.ArtifactView{ID: ids.Hash(tok), VersionID: v.ID, BotID: art.BotID, UserID: userID, SessionID: sessionID, ExpiresAt: now.Add(viewTTL)}).Error; err != nil {
		return "", err
	}
	return cfg.ContentURL(config.ContentApp, art.Label) + viewPrefix + tok + "/" + escapePath(v.Entry), nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, x := range parts {
		parts[i] = url.PathEscape(x)
	}
	return strings.Join(parts, "/")
}

// ServeApp answers app-<label>.<content.host>/_v/<pass>/<file>.
func (s *Service) ServeApp(w http.ResponseWriter, r *http.Request, label string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "an app's files are read-only", http.StatusMethodNotAllowed)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, viewPrefix)
	tok, file, _ := strings.Cut(rest, "/")
	if !ok || tok == "" {
		tunnels.Page(w, http.StatusNotFound, "Open it from Silo", "This app opens from its card in a Silo chat.")
		return
	}
	art, v, ok := s.viewed(tok, label)
	if !ok {
		tunnels.Page(w, http.StatusForbidden, "This link has expired", "Open the app from its card in Silo again.")
		return
	}
	file = strings.TrimSuffix(file, "/")
	if file == "" {
		file = v.Entry
	}
	if !safeRel(file) {
		http.NotFound(w, r)
		return
	}
	_, data, err := s.fileOf(r.Context(), v, file)
	if err != nil {
		s.contentError(w, r, err)
		return
	}
	appHeaders(w.Header(), s.cfg(), s.cfg().ContentURL(config.ContentApp, art.Label), v.Network)
	writeContent(w, file, data)
}

// viewed checks a pass: unexpired, its session still live, the Bot still the
// same owner's, the owner not disabled, and the host the app's own.
func (s *Service) viewed(tok, label string) (*db.Artifact, *db.ArtifactVersion, bool) {
	var view db.ArtifactView
	if res := s.db.Where("id = ? AND expires_at > ?", ids.Hash(tok), time.Now()).Limit(1).Find(&view); res.Error != nil || res.RowsAffected == 0 {
		return nil, nil, false
	}
	var live int64
	s.db.Model(&db.Session{}).Where("id = ? AND user_id = ? AND expires_at > ?", view.SessionID, view.UserID, time.Now()).Count(&live)
	if live != 1 {
		return nil, nil, false
	}
	if !access.CanUse(s.db, view.BotID, view.UserID) {
		return nil, nil, false
	}
	var v db.ArtifactVersion
	if res := s.db.Where("id = ? AND bot_id = ?", view.VersionID, view.BotID).Limit(1).Find(&v); res.Error != nil || res.RowsAffected == 0 {
		return nil, nil, false
	}
	var art db.Artifact
	if res := s.db.Where("id = ? AND label = ?", v.ArtifactID, label).Limit(1).Find(&art); res.Error != nil || res.RowsAffected == 0 {
		return nil, nil, false
	}
	return &art, &v, true
}

func (s *Service) contentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errNoFile):
		http.NotFound(w, r)
	case errors.Is(err, ErrGone):
		tunnels.Page(w, http.StatusGone, "No longer kept", ErrGone.Error()+".")
	case errors.Is(err, ErrAltered):
		tunnels.Page(w, http.StatusConflict, "Changed on the machine", ErrAltered.Error()+".")
	default:
		tunnels.Page(w, http.StatusServiceUnavailable, "The Bot is offline", "This app is kept on the Bot's machine, which is not running. Start the Bot in Silo and reload.")
	}
}

// appHeaders is the policy every app and shared page is served with: a
// sandbox (an opaque origin: no cookies, no storage, no reach into anything),
// and its own files only unless the version may use the network.
func appHeaders(h http.Header, cfg config.Config, self string, network bool) {
	src := "'self' " + self + " data: blob:"
	connect := src
	if network {
		src += " https:"
		connect += " https: wss:"
	}
	ancestors := "'none'"
	if u, err := url.Parse(strings.TrimSpace(cfg.PublicURL)); err == nil && u.Host != "" {
		ancestors = u.Scheme + "://" + u.Host
	}
	h.Set("Content-Security-Policy", strings.Join([]string{
		"sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads allow-pointer-lock",
		"default-src " + src,
		"script-src " + src + " 'unsafe-inline' 'unsafe-eval'",
		"style-src " + src + " 'unsafe-inline'",
		"connect-src " + connect,
		"form-action 'none'",
		"base-uri 'self'",
		"frame-ancestors " + ancestors,
	}, "; "))
	// The pass is in the path: never hand it to another site.
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	// The sandboxed page is an opaque origin: its fetch of its own data.json
	// is cross-origin, and carries no credentials anyway.
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cache-Control", "private, max-age=300, immutable")
}

func writeContent(w http.ResponseWriter, name string, data []byte) {
	ct := workspace.MIME(name)
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		ct = "text/html; charset=utf-8"
	case ".js", ".mjs":
		ct = "text/javascript; charset=utf-8"
	case ".css":
		ct = "text/css; charset=utf-8"
	case ".wasm":
		ct = "application/wasm"
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write(data)
}

// --- shared links -----------------------------------------------------------

func (s *Service) sharesDir() string {
	return filepath.Join(s.cfg().DataDir, "shares")
}

func (s *Service) removeShareFiles(id string) {
	if s.cfg().DataDir == "" || !validShareID(id) {
		return
	}
	_ = os.RemoveAll(filepath.Join(s.sharesDir(), id))
}

func validShareID(id string) bool {
	return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == ""
}

// share copies one pinned version to the control plane and makes its link.
func (s *Service) share(ctx context.Context, art *db.Artifact, v *db.ArtifactVersion) (*db.ArtifactShare, error) {
	if s.cfg().ContentHost() == "" {
		return nil, errors.New("shared links need a content host: the operator sets content.host")
	}
	if art.Type == TypeSkill {
		return nil, errors.New("a skill is saved, not shared: download it instead")
	}
	if v.Blob == "" {
		return nil, errors.New("this version was too large to keep, so it cannot be shared")
	}
	files := map[string][]byte{}
	if v.Kind == "tree" {
		tree, err := s.treeFiles(ctx, art.BotID, v.Blob)
		if err != nil {
			return nil, err
		}
		files = tree
	} else {
		_, b, err := s.fileOf(ctx, v, "")
		if err != nil {
			return nil, err
		}
		files[v.Name] = b
	}
	sh := &db.ArtifactShare{
		ID: ids.New(), BotID: art.BotID, ArtifactID: art.ID, VersionID: v.ID, Label: names.New() + "-" + ids.New()[:6],
		Title: art.Title, Type: art.Type, Kind: v.Kind, Name: v.Name, Entry: v.Entry, Network: v.Network, CreatedAt: time.Now(),
	}
	if err := s.writeShare(sh.ID, files); err != nil {
		return nil, err
	}
	if err := s.db.Create(sh).Error; err != nil {
		s.removeShareFiles(sh.ID)
		return nil, err
	}
	return sh, nil
}

// writeShare lays a share's files out under data/shares/<id>, all or nothing.
func (s *Service) writeShare(id string, files map[string][]byte) error {
	root := s.sharesDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(root, ".new-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for p, b := range files {
		if !safeRel(p) {
			continue
		}
		dst := filepath.Join(tmp, "files", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o600); err != nil {
			return err
		}
	}
	list := make([]string, 0, len(files))
	for p := range files {
		list = append(list, p)
	}
	idx, _ := json.Marshal(list)
	if err := os.WriteFile(filepath.Join(tmp, "index.json"), idx, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(root, id))
}

// ServeShare answers share-<label>.<content.host>: no sign-in.
func (s *Service) ServeShare(w http.ResponseWriter, r *http.Request, label string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "a shared link is read-only", http.StatusMethodNotAllowed)
		return
	}
	var sh db.ArtifactShare
	if res := s.db.Where("label = ?", label).Limit(1).Find(&sh); res.Error != nil || res.RowsAffected == 0 || s.ownerDisabled(sh.BotID) {
		tunnels.Page(w, http.StatusNotFound, "Nothing shared here", "This link was removed, or never existed.")
		return
	}
	file := strings.Trim(r.URL.Path, "/")
	if file == "" {
		file = sh.Entry
		if sh.Kind != "tree" {
			file = sh.Name
		}
	}
	if !safeRel(file) || !validShareID(sh.ID) {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(s.sharesDir(), sh.ID, "files", filepath.FromSlash(file)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.touchShare(&sh)
	if sh.Type == TypeApp {
		appHeaders(w.Header(), s.cfg(), s.cfg().ContentURL(config.ContentShare, sh.Label), sh.Network)
		writeContent(w, file, data)
		return
	}
	// A shared file: shown in place when it cannot act as a page, else a
	// download (with the same sandbox should a browser open it anyway).
	appHeaders(w.Header(), s.cfg(), s.cfg().ContentURL(config.ContentShare, sh.Label), false)
	w.Header().Set("Cache-Control", "public, max-age=300")
	serveBytes(w, path.Base(file), data, true)
}

// ownerDisabled reports whether the Bot is suspended: no enabled manager.
func (s *Service) ownerDisabled(botID string) bool {
	return access.Suspended(s.db, botID)
}

func (s *Service) touchShare(sh *db.ArtifactShare) {
	if sh.LastUsedAt != nil && time.Since(*sh.LastUsedAt) < time.Minute {
		return
	}
	now := time.Now()
	if err := s.db.Model(&db.ArtifactShare{}).Where("id = ?", sh.ID).Update("last_used_at", now).Error; err != nil {
		log.Printf("share %s: %v", sh.ID, err)
	}
}

func (s *Service) shareURL(sh *db.ArtifactShare) string {
	if !s.contentOn() {
		return ""
	}
	return s.cfg().ContentURL(config.ContentShare, sh.Label) + "/"
}
