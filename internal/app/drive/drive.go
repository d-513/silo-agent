// Package drive is the control plane's side of Bot drives: rclone remotes
// mounted under /workspace/drives by one sidecar container per Bot. It owns the
// drive rows and their add flow (sign-in, pickers, browsing), renders each
// drive for the sidecar, keeps the sidecar's session and lifecycle, and serves
// the DriveHost endpoint the sidecar dials.
package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/drives"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

// Drive states the CP stores on a row. The sidecar reports mounting, mounted,
// error, and stopped; the renderer adds the needs_* states from
// drives.MissingError; draft is the add form's working row.
const (
	driveDraft    = "draft"
	driveMounting = "mounting"
	driveMounted  = "mounted"
	driveError    = "error"
	driveStopped  = "stopped"
	driveNoBind   = "needs_reset"
)

const (
	// driveWait bounds how long the CP waits for a fresh sidecar to dial in.
	driveWait = 90 * time.Second
	// driveListWait bounds a one-shot listing round trip.
	driveListWait = 60 * time.Second
	// draftTTL is how long an unsaved add-form row lives.
	draftTTL = 24 * time.Hour
)

// driveSecrets is the SecretsJSON column: secret user answers and every
// dynamic value.
type driveSecrets struct {
	User    map[string]string `json:"user,omitempty"`
	Dynamic map[string]string `json:"dynamic,omitempty"`
}

func decodeMap(raw string) map[string]string {
	out := map[string]string{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func decodeSecrets(raw string) driveSecrets {
	var s driveSecrets
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	if s.User == nil {
		s.User = map[string]string{}
	}
	if s.Dynamic == nil {
		s.Dynamic = map[string]string{}
	}
	return s
}

func mustJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Host is what the drives need from the App around them.
type Host interface {
	// PublicURL is the address the browser (and an OAuth provider) reaches the
	// control plane at.
	PublicURL(ctx context.Context) string
	// AwaitOAuth registers a pending sign-in under state: when the provider
	// redirects back to /oauth/callback, complete runs with the query and its
	// outcome is shown in the popup. The entry lapses after ttl.
	AwaitOAuth(state string, ttl time.Duration, complete func(ctx context.Context, q url.Values) error)
}

// Service owns every drive and each Bot's drive sidecar.
type Service struct {
	db     *gorm.DB
	docker dockerx.Host
	store  *config.Store
	cfg    func() config.Config
	host   Host
	// http reaches drive providers (OAuth token endpoints and account
	// lookups); it is read on every call so a test can swap in a fake.
	http func() *http.Client

	hub *driveHub
	// locks serializes sidecar start and removal per Bot.
	locks sync.Map

	// OnChange hears what a drive's sidecar saw happen on the mount (the drive
	// journal). The App points it at change tracking.
	OnChange func(d *db.Drive, changes []*v1.DriveChange)
}

func New(gdb *gorm.DB, d dockerx.Host, store *config.Store, cfg func() config.Config, h Host, httpClient func() *http.Client) *Service {
	return &Service{db: gdb, docker: d, store: store, cfg: cfg, host: h, http: httpClient, hub: &driveHub{live: map[string]*driveSession{}}}
}

func (s *Service) httpClient() *http.Client {
	if s.http == nil {
		return nil
	}
	return s.http()
}

// templates is the catalog the app serves; a seam for tests.
func (s *Service) templates() *drives.Registry { return drives.Builtin() }

// driveValues assembles a drive's answers by kind.
func (s *Service) driveValues(d *db.Drive, t *drives.Template) drives.Values {
	sec := decodeSecrets(d.SecretsJSON)
	user := decodeMap(d.OptionsJSON)
	for k, v := range sec.User {
		user[k] = v
	}
	sys := map[string]string{}
	if s.store != nil {
		sys = s.store.DriveSystem(t.Key)
	}
	return drives.Values{User: user, System: sys, Dynamic: sec.Dynamic}
}

// driveSpec renders one drive for the sidecar. An OAuth token moves from env
// to the spec's token field: the sidecar writes it to a private config file,
// the only place rclone saves a refresh to (env would shadow the file).
func (s *Service) driveSpec(d *db.Drive) (*v1.DriveSpec, *drives.Template, error) {
	t, ok := s.templates().Get(d.Template)
	if !ok {
		return nil, nil, fmt.Errorf("unknown drive type %q", d.Template)
	}
	r, err := t.Render(s.driveValues(d, t), d.Name)
	if err != nil {
		return nil, t, err
	}
	dir := d.Name
	if d.Draft {
		// Drafts only list, never mount; their placeholder name is not a dir.
		dir = "draft"
	}
	spec := &v1.DriveSpec{
		Id: d.ID, Dir: dir, Remote: r.Name, Env: r.Env, Path: r.Path, Flags: r.Flags, ReadOnly: d.ReadOnly,
	}
	if t.Auth.Kind == drives.AuthOAuth2 {
		key := drives.EnvKey(r.Name, "token")
		spec.Token = r.Env[key]
		delete(spec.Env, key)
	}
	return spec, t, nil
}

func (s *Service) setDriveState(id, state, detail string) {
	if s.db == nil {
		return
	}
	s.db.Model(&db.Drive{}).Where("id = ? AND draft = ?", id, false).
		Updates(map[string]any{"state": state, "state_detail": textx.ClipRunes(detail, 500)})
}

// BotDrives lists a Bot's saved (non-draft) drives.
func (s *Service) BotDrives(botID string) []db.Drive {
	var out []db.Drive
	s.db.Where("bot_id = ? AND draft = ?", botID, false).Order("name").Find(&out)
	return out
}

// renderApply builds the desired set. A drive that cannot render (an admin
// has not set it up, it needs a new sign-in, a field is empty) is left out and
// its row says why, so one broken drive never blocks the rest.
func (s *Service) renderApply(botID string) *v1.DriveApply {
	out := &v1.DriveApply{CacheMaxSize: strings.TrimSpace(s.cfg().Drives.CacheMaxSize)}
	for _, d := range s.BotDrives(botID) {
		spec, _, err := s.driveSpec(&d)
		if err != nil {
			state, detail := driveError, err.Error()
			if m, ok := drives.AsMissing(err); ok {
				state = m.State()
			}
			if d.State != state || d.StateDetail != detail {
				s.setDriveState(d.ID, state, detail)
			}
			continue
		}
		if d.State == drives.StateNeedsSetup || d.State == drives.StateNeedsAuth || d.State == drives.StateNeedsInput || d.State == "" {
			s.setDriveState(d.ID, driveMounting, "")
		}
		out.Drives = append(out.Drives, spec)
	}
	return out
}

// driveSession is one live connection from a Bot's drive sidecar.
type driveSession struct {
	botID string
	send  chan *v1.DriveDown
	done  chan struct{}
	once  sync.Once

	mu      sync.Mutex
	waiters map[string]chan *v1.DriveListResult
}

func (s *driveSession) close() { s.once.Do(func() { close(s.done) }) }

func (s *driveSession) push(m *v1.DriveDown) bool {
	select {
	case s.send <- m:
		return true
	case <-s.done:
		return false
	}
}

type driveHub struct {
	mu   sync.Mutex
	live map[string]*driveSession
}

func (s *Service) driveHub() *driveHub { return s.hub }

func (h *driveHub) get(botID string) *driveSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.live[botID]
	if s != nil {
		select {
		case <-s.done:
			delete(h.live, botID)
			return nil
		default:
		}
	}
	return s
}

func (h *driveHub) put(s *driveSession) {
	h.mu.Lock()
	old := h.live[s.botID]
	h.live[s.botID] = s
	h.mu.Unlock()
	if old != nil {
		old.close()
	}
}

func (h *driveHub) remove(s *driveSession) {
	h.mu.Lock()
	if h.live[s.botID] == s {
		delete(h.live, s.botID)
	}
	h.mu.Unlock()
	s.close()
}

// Session is the DriveHost endpoint the sidecar dials.
func (s *Service) Session(ctx context.Context, stream *connect.BidiStream[v1.DriveUp, v1.DriveDown]) error {
	host, _ := ctx.Value(driveHostKey).(*db.DriveHost)
	if host == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	sess := &driveSession{
		botID:   host.BotID,
		send:    make(chan *v1.DriveDown, 16),
		done:    make(chan struct{}),
		waiters: map[string]chan *v1.DriveListResult{},
	}
	h := s.driveHub()
	h.put(sess)
	defer h.remove(sess)

	// Every session starts from the full desired set.
	sess.send <- &v1.DriveDown{Body: &v1.DriveDown_Apply{Apply: s.renderApply(host.BotID)}}

	errc := make(chan error, 2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			case <-sess.done:
				errc <- nil
				return
			case m := <-sess.send:
				if err := stream.Send(m); err != nil {
					errc <- err
					return
				}
			}
		}
	}()
	go func() {
		for {
			up, err := stream.Receive()
			if err != nil {
				errc <- err
				return
			}
			s.driveUp(sess, up)
		}
	}()
	err := <-errc
	return err
}

// driveUp handles one frame from a sidecar. Every write is scoped to the
// session's Bot so a sidecar can only touch its own drives.
func (s *Service) driveUp(sess *driveSession, up *v1.DriveUp) {
	switch b := up.GetBody().(type) {
	case *v1.DriveUp_Status:
		st := b.Status
		var d db.Drive
		if s.db.Where("id = ? AND bot_id = ?", st.GetId(), sess.botID).Limit(1).Find(&d).Error != nil || d.ID == "" {
			return
		}
		s.setDriveState(d.ID, st.GetState(), st.GetDetail())
	case *v1.DriveUp_Changes:
		var d db.Drive
		if s.db.Where("id = ? AND bot_id = ?", b.Changes.GetId(), sess.botID).Limit(1).Find(&d).Error != nil || d.ID == "" {
			return
		}
		if s.OnChange != nil && len(b.Changes.GetChanges()) > 0 {
			s.OnChange(&d, b.Changes.GetChanges())
		}
	case *v1.DriveUp_Token:
		s.saveDriveToken(sess.botID, b.Token.GetId(), b.Token.GetToken())
	case *v1.DriveUp_List:
		sess.mu.Lock()
		w := sess.waiters[b.List.GetRequestId()]
		delete(sess.waiters, b.List.GetRequestId())
		sess.mu.Unlock()
		if w != nil {
			w <- b.List
		}
	}
}

// saveDriveToken stores a token rclone refreshed. Only a token that parses is
// kept: a truncated write must not replace a working one.
func (s *Service) saveDriveToken(botID, driveID, tok string) {
	if _, err := drives.ParseToken(tok); err != nil {
		return
	}
	var d db.Drive
	if s.db.Where("id = ? AND bot_id = ?", driveID, botID).Limit(1).Find(&d).Error != nil || d.ID == "" {
		return
	}
	sec := decodeSecrets(d.SecretsJSON)
	if sec.Dynamic["token"] == tok {
		return
	}
	sec.Dynamic["token"] = tok
	s.db.Model(&db.Drive{}).Where("id = ?", d.ID).Update("secrets_json", mustJSONString(sec))
}

type driveHostCtxKey struct{}

var driveHostKey = driveHostCtxKey{}

func (s *Service) Intercept(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		host, _, err := access.RowByToken[db.DriveHost](s.db, "token_hash", conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return err
		}
		return next(context.WithValue(ctx, driveHostKey, host), conn)
	}
}

func (s *Service) lockSidecar(botID string) func() {
	v, _ := s.locks.LoadOrStore(botID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// ensureDrives brings the Bot's drive sidecar up (when it has drives, or when
// force is set for an add-form listing) and pushes the desired set. It is
// cheap when the sidecar is already connected.
func (s *Service) ensureDrives(ctx context.Context, botID string, force bool) (*driveSession, error) {
	if s.db == nil {
		return nil, errors.New("no database")
	}
	if !force && len(s.BotDrives(botID)) == 0 {
		return nil, nil
	}
	h := s.driveHub()
	if sess := h.get(botID); sess != nil {
		sess.push(&v1.DriveDown{Body: &v1.DriveDown_Apply{Apply: s.renderApply(botID)}})
		return sess, nil
	}
	if s.docker == nil {
		return nil, errors.New("docker unavailable")
	}
	unlock := s.lockSidecar(botID)
	defer unlock()
	if sess := h.get(botID); sess != nil {
		return sess, nil
	}
	var host db.DriveHost
	s.db.Where("bot_id = ?", botID).Limit(1).Find(&host)
	cfg := s.cfg()
	if err := s.docker.PrepareDriveDir(ctx, cfg.DriveImage(), cfg.DriveMountRoot(), botID); err != nil {
		return nil, fmt.Errorf("drives are not available on this host: %w", err)
	}
	started := false
	if host.ContainerID != "" {
		st, err := s.docker.Inspect(ctx, host.ContainerID)
		switch {
		case err == nil && st.Running:
			started = true
		case err == nil:
			if s.docker.Start(ctx, st.ID) == nil {
				started = true
			} else {
				s.docker.DropDrive(ctx, botID, st.ID)
			}
		case dockerx.IsNotFound(err):
		default:
			return nil, err
		}
	}
	if !started {
		if err := s.startDriveSidecar(ctx, botID); err != nil {
			return nil, err
		}
	}
	return s.waitDriveSession(ctx, botID)
}

func (s *Service) startDriveSidecar(ctx context.Context, botID string) error {
	cfg := s.cfg()
	s.docker.DropDrive(ctx, botID, "")
	token := ids.New() + ids.New()
	cid, err := s.docker.CreateDrive(ctx, dockerx.DriveSpec{
		BotID: botID,
		Image: cfg.DriveImage(),
		Env: []string{
			"SILO_CP_URL=" + cfg.CPURL,
			"SILO_DRIVE_TOKEN=" + token,
			"SILO_BOT_ID=" + botID,
		},
		Root:     cfg.DriveMountRoot(),
		CacheDir: filepath.Join(cfg.DataDir, "drives", botID, "cache"),
	})
	if err != nil {
		return err
	}
	if err := s.docker.Start(ctx, cid); err != nil {
		s.docker.DropDrive(ctx, botID, cid)
		return err
	}
	// Persist the container and token hash only after a successful start.
	return s.db.Save(&db.DriveHost{BotID: botID, ContainerID: cid, TokenHash: ids.Hash(token)}).Error
}

func (s *Service) waitDriveSession(ctx context.Context, botID string) (*driveSession, error) {
	deadline := time.Now().Add(driveWait)
	for {
		if sess := s.driveHub().get(botID); sess != nil {
			return sess, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the drive sidecar did not connect to the control plane")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// EnsureBg is the fire-and-forget form used when a Bot starts.
func (s *Service) EnsureBg(botID string) {
	if s.db == nil || s.docker == nil || s.driveHub().get(botID) != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if _, err := s.ensureDrives(ctx, botID, false); err != nil {
			log.Printf("drives bot=%s: %v", botID, err)
			for _, d := range s.BotDrives(botID) {
				s.setDriveState(d.ID, driveError, err.Error())
			}
		}
	}()
}

// applyDrives pushes the desired set to a connected sidecar, or starts one.
func (s *Service) applyDrives(botID string) {
	if sess := s.driveHub().get(botID); sess != nil {
		sess.push(&v1.DriveDown{Body: &v1.DriveDown_Apply{Apply: s.renderApply(botID)}})
		return
	}
	s.EnsureBg(botID)
}

// listDrive asks the sidecar to list folders of a drive (saved or draft).
func (s *Service) listDrive(ctx context.Context, d *db.Drive, path string) (*v1.DriveListResult, error) {
	spec, _, err := s.driveSpec(d)
	if err != nil {
		return nil, err
	}
	sess, err := s.ensureDrives(ctx, d.BotID, true)
	if err != nil {
		return nil, err
	}
	req := ids.New()
	w := make(chan *v1.DriveListResult, 1)
	sess.mu.Lock()
	sess.waiters[req] = w
	sess.mu.Unlock()
	defer func() {
		sess.mu.Lock()
		delete(sess.waiters, req)
		sess.mu.Unlock()
	}()
	if !sess.push(&v1.DriveDown{Body: &v1.DriveDown_List{List: &v1.DriveList{RequestId: req, Spec: spec, Path: path}}}) {
		return nil, errors.New("the drive sidecar disconnected")
	}
	ctx, cancel := context.WithTimeout(ctx, driveListWait)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, errors.New("the drive sidecar did not answer in time")
	case <-sess.done:
		return nil, errors.New("the drive sidecar disconnected")
	case res := <-w:
		if tok := res.GetToken(); tok != "" {
			s.saveDriveToken(d.BotID, d.ID, tok)
		}
		return res, nil
	}
}

// Stop stops the sidecar with the Bot (Stop Bot). Its container and
// cache stay; the next start mounts again.
func (s *Service) Stop(ctx context.Context, botID string) {
	if sess := s.driveHub().get(botID); sess != nil {
		s.driveHub().remove(sess)
	}
	if s.docker != nil {
		_ = s.docker.Stop(ctx, dockerx.DriveName(botID))
	}
	if s.db != nil {
		s.db.Model(&db.Drive{}).Where("bot_id = ? AND draft = ? AND state IN ?", botID, false,
			[]string{driveMounting, driveMounted}).Updates(map[string]any{"state": driveStopped, "state_detail": ""})
	}
}

// Drop removes the sidecar, its cache, and the Bot's drive dir for good
// (DeleteBot).
func (s *Service) Drop(ctx context.Context, botID string) {
	unlock := s.lockSidecar(botID)
	defer unlock()
	if sess := s.driveHub().get(botID); sess != nil {
		s.driveHub().remove(sess)
	}
	var host db.DriveHost
	s.db.Where("bot_id = ?", botID).Limit(1).Find(&host)
	if s.docker != nil {
		s.docker.DropDrive(ctx, botID, host.ContainerID)
		cfg := s.cfg()
		if err := s.docker.RemoveDriveDir(ctx, cfg.DriveImage(), cfg.DriveMountRoot(), botID); err != nil {
			log.Printf("drives bot=%s: remove dir: %v", botID, err)
		}
	}
	s.db.Where("bot_id = ?", botID).Delete(&db.DriveHost{})
	s.db.Where("bot_id = ?", botID).Delete(&db.Drive{})
	if dir := s.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(filepath.Join(dir, "drives", botID))
	}
}

// Reconcile reclaims sidecars whose Bot is gone and sweeps stale drafts.
// Live sidecars reconnect on their own and get a fresh DriveApply.
func (s *Service) Reconcile() {
	if s.db == nil {
		return
	}
	s.db.Where("draft = ? AND created_at < ?", true, time.Now().Add(-draftTTL)).Delete(&db.Drive{})
	if s.docker == nil {
		return
	}
	list, err := s.docker.ListDrives(context.Background())
	if err != nil {
		return
	}
	for _, c := range list {
		bot := c.BotID
		if bot == "" {
			bot = strings.TrimPrefix(c.Name, "silo-drive-")
		}
		var n int64
		s.db.Model(&db.Bot{}).Where("id = ?", bot).Count(&n)
		if n == 0 {
			s.docker.DropDrive(context.Background(), bot, c.ID)
			s.db.Where("bot_id = ?", bot).Delete(&db.DriveHost{})
		}
	}
}

// RemoveSidecar drops the Bot's drive sidecar but keeps its drives: the next
// start makes a new sidecar and mounts them again.
func (s *Service) RemoveSidecar(ctx context.Context, botID string) {
	unlock := s.lockSidecar(botID)
	defer unlock()
	if sess := s.driveHub().get(botID); sess != nil {
		s.driveHub().remove(sess)
	}
	var host db.DriveHost
	s.db.Where("bot_id = ?", botID).Limit(1).Find(&host)
	if s.docker != nil {
		s.docker.DropDrive(ctx, botID, host.ContainerID)
	}
	s.db.Where("bot_id = ?", botID).Delete(&db.DriveHost{})
	s.db.Model(&db.Drive{}).Where("bot_id = ? AND draft = ?", botID, false).
		Updates(map[string]any{"state": driveStopped, "state_detail": ""})
}
