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
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
// error, and stopped (sidecarStates) for the drives it was asked to mount; the
// renderer adds the needs_* states from drives.MissingError, and an error of
// its own, for the ones it leaves out; draft is the add form's working row. A
// box made before drives has no bind: ListDrives says so (bind_ok), read off
// the engine, not stored.
const (
	driveDraft    = "draft"
	driveMounting = "mounting"
	driveMounted  = "mounted"
	driveError    = "error"
	driveStopped  = "stopped"
)

// sidecarStates are the states a sidecar may report.
var sidecarStates = map[string]bool{driveMounting: true, driveMounted: true, driveError: true, driveStopped: true}

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

// httpClient reaches a provider's token endpoint and account lookups. Most of
// those addresses are the template's own, but some carry a host the sign-in
// handed back (pCloud's API host arrives as a callback parameter anyone can
// write), so the client keeps to the operator's outbound policy.
func (s *Service) httpClient() *http.Client {
	if s.http != nil {
		if hc := s.http(); hc != nil {
			return hc
		}
	}
	return s.cfg().OutboundPolicy().Client(time.Minute)
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

// setMountState records how one Bot's mount of a drive is doing.
func (s *Service) setMountState(botID, driveID, state, detail string) {
	if s.db == nil {
		return
	}
	s.db.Model(&db.BotDrive{}).Where("bot_id = ? AND drive_id = ?", botID, driveID).
		Updates(map[string]any{"state": state, "state_detail": textx.ClipRunes(detail, 500)})
}

// BotDrives lists the drives granted to a Bot as it mounts them: read-only
// when the drive or the grant is, with that mount's state.
func (s *Service) BotDrives(botID string) []db.Drive {
	var rows []struct {
		db.Drive
		GrantReadOnly bool
		GrantState    string
		GrantDetail   string
	}
	s.db.Model(&db.Drive{}).
		Select("drives.*, g.read_only AS grant_read_only, g.state AS grant_state, g.state_detail AS grant_detail").
		Joins("JOIN bot_drives g ON g.drive_id = drives.id").
		Where("g.bot_id = ? AND drives.draft = ?", botID, false).Order("drives.name").Scan(&rows)
	out := make([]db.Drive, 0, len(rows))
	for _, r := range rows {
		d := r.Drive
		d.ReadOnly = d.ReadOnly || r.GrantReadOnly
		d.State, d.StateDetail = r.GrantState, r.GrantDetail
		out = append(out, d)
	}
	return out
}

// grantees are the Bots a drive is granted to.
func (s *Service) grantees(driveID string) []string {
	var out []string
	s.db.Model(&db.BotDrive{}).Where("drive_id = ?", driveID).Order("created_at").Pluck("bot_id", &out)
	return out
}

// applyAll remounts a drive in every Bot it is granted to: after an edit, a
// sign-in, or a token another Bot's sidecar refreshed.
func (s *Service) applyAll(driveID string) {
	for _, b := range s.grantees(driveID) {
		s.applyDrives(b)
	}
}

// listVia is the Bot whose sidecar lists a drive for the add or edit form: a
// draft's own, else one it is granted to.
func (s *Service) listVia(d *db.Drive) string {
	if d.BotID != "" {
		return d.BotID
	}
	if g := s.grantees(d.ID); len(g) > 0 {
		return g[0]
	}
	return ""
}

// renderApply builds the desired set. A drive that cannot render (an admin
// has not set it up, it needs a new sign-in, a field is empty) is left out and
// its row says why, so one broken drive never blocks the rest.
func (s *Service) renderApply(botID string) *v1.DriveApply {
	out := &v1.DriveApply{CacheMaxSize: strings.TrimSpace(s.cfg().Drives.CacheMaxSize)}
	for _, d := range s.BotDrives(botID) {
		// A disabled lender's account is not lent while they cannot sign in.
		if err := access.LenderOK(s.db, d.LentBy); err != nil {
			if d.State != driveError || d.StateDetail != err.Error() {
				s.setMountState(botID, d.ID, driveError, err.Error())
			}
			continue
		}
		spec, _, err := s.driveSpec(&d)
		if err != nil {
			state, detail := driveError, err.Error()
			if m, ok := drives.AsMissing(err); ok {
				state = m.State()
			}
			if d.State != state || d.StateDetail != detail {
				s.setMountState(botID, d.ID, state, detail)
			}
			continue
		}
		if d.State == drives.StateNeedsSetup || d.State == drives.StateNeedsAuth || d.State == drives.StateNeedsInput || d.State == "" {
			s.setMountState(botID, d.ID, driveMounting, "")
		}
		out.Drives = append(out.Drives, spec)
	}
	return out
}

// driveSession is one live connection from a Bot's drive sidecar. The
// desired set is never rendered by whoever changes it and then pushed, since
// two such pushes can arrive in the wrong order: a change marks the session
// dirty (kick) and its one sender renders the set as it is at that moment.
type driveSession struct {
	botID string
	send  chan *v1.DriveDown
	kick  chan struct{}
	done  chan struct{}
	once  sync.Once

	mu      sync.Mutex
	waiters map[string]chan *v1.DriveListResult
	// desired are the drives the last set rendered asks the sidecar to
	// mount. A status for any other is not the sidecar's to give: the CP
	// left that drive out and its row says why.
	desired map[string]bool
	// gone is set when the session is taken off the hub (Stop Bot, a new
	// session): a report still in flight from it is no longer the
	// sidecar's to give, or it would undo the stopped state.
	gone bool
	// restart are drives to stop and mount again (a new sign-in) with the
	// next set.
	restart map[string]bool
}

func (s *driveSession) close() { s.once.Do(func() { close(s.done) }) }

// dirty asks the sender for a fresh desired set.
func (s *driveSession) dirty() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// restartDrive asks for a drive to be mounted again from scratch.
func (s *driveSession) restartDrive(driveID string) {
	s.mu.Lock()
	if s.restart == nil {
		s.restart = map[string]bool{}
	}
	s.restart[driveID] = true
	s.mu.Unlock()
	s.dirty()
}

// render is the desired set as it stands now, noted as what the sidecar
// was asked for.
func (s *Service) render(sess *driveSession) *v1.DriveApply {
	a := s.renderApply(sess.botID)
	want := map[string]bool{}
	for _, d := range a.GetDrives() {
		want[d.GetId()] = true
	}
	sess.mu.Lock()
	sess.desired = want
	sess.mu.Unlock()
	return a
}

// report runs write for a drive the sidecar was asked to mount, while the
// session is still the live one. It holds the session's lock across the
// write, so taking the session off the hub waits for a write in flight and
// refuses every later one.
func (s *driveSession) report(driveID string, write func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gone || !s.desired[driveID] {
		return
	}
	write()
}

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
	s.mu.Lock()
	s.gone = true
	s.mu.Unlock()
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
		kick:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		waiters: map[string]chan *v1.DriveListResult{},
	}
	h := s.driveHub()
	h.put(sess)
	defer h.remove(sess)

	// Every session starts from the full desired set, rendered before the
	// sidecar's snapshot is read so its statuses are judged against it.
	first := s.render(sess)

	errc := make(chan error, 1)
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
	// Sends stay on this goroutine: one still sending after the handler
	// returned would panic the whole control plane.
	apply := func(a *v1.DriveApply) error {
		return stream.Send(&v1.DriveDown{Body: &v1.DriveDown_Apply{Apply: a}})
	}
	if err := apply(first); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sess.done:
			return nil
		case err := <-errc:
			return err
		case m := <-sess.send:
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-sess.kick:
			sess.mu.Lock()
			restart := sess.restart
			sess.restart = nil
			sess.mu.Unlock()
			if len(restart) > 0 {
				// A drive left out of one set and back in the next is
				// stopped and mounted again with what it has now.
				cut := s.render(sess)
				cut.Drives = slices.DeleteFunc(cut.Drives, func(d *v1.DriveSpec) bool { return restart[d.GetId()] })
				if err := apply(cut); err != nil {
					return err
				}
			}
			if err := apply(s.render(sess)); err != nil {
				return err
			}
		}
	}
}

// granted loads a drive granted to the Bot, or nil.
func (s *Service) granted(botID, driveID string) *db.Drive {
	var d db.Drive
	s.db.Model(&db.Drive{}).Joins("JOIN bot_drives g ON g.drive_id = drives.id").
		Where("drives.id = ? AND g.bot_id = ?", driveID, botID).Limit(1).Find(&d)
	if d.ID == "" {
		return nil
	}
	return &d
}

// driveUp handles one frame from a sidecar. Every write is scoped to the
// session's Bot so a sidecar can only touch the drives granted to it.
func (s *Service) driveUp(sess *driveSession, up *v1.DriveUp) {
	switch b := up.GetBody().(type) {
	case *v1.DriveUp_Status:
		st := b.Status
		// Only a drive it was asked to mount is the sidecar's to report on:
		// stopping one the CP left out must not wipe the reason it was left
		// out ("Sign in to …").
		if !sidecarStates[st.GetState()] || s.granted(sess.botID, st.GetId()) == nil {
			return
		}
		sess.report(st.GetId(), func() {
			s.setMountState(sess.botID, st.GetId(), st.GetState(), st.GetDetail())
		})
	case *v1.DriveUp_Changes:
		d := s.granted(sess.botID, b.Changes.GetId())
		if d == nil {
			return
		}
		// The journal is the mounting Bot's: what was written through it.
		d.BotID = sess.botID
		if s.OnChange != nil && len(b.Changes.GetChanges()) > 0 {
			s.OnChange(d, b.Changes.GetChanges())
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

// saveDriveToken stores a token one Bot's rclone refreshed, and hands it to
// every other Bot the drive is mounted in: a provider that rotates refresh
// tokens has invalidated the one they hold. Only a token that parses is kept:
// a truncated write must not replace a working one.
func (s *Service) saveDriveToken(botID, driveID, tok string) {
	if _, err := drives.ParseToken(tok); err != nil {
		return
	}
	d := s.granted(botID, driveID)
	if d == nil {
		// A draft lists through the Bot it was started from.
		var draft db.Drive
		if s.db.Where("id = ? AND bot_id = ? AND draft = ?", driveID, botID, true).Limit(1).Find(&draft); draft.ID == "" {
			return
		}
		d = &draft
	}
	changed := false
	if _, err := s.editSecrets(d.ID, nil, func(sec *driveSecrets) {
		changed = sec.Dynamic["token"] != tok
		sec.Dynamic["token"] = tok
	}); err != nil || !changed {
		return
	}
	for _, other := range s.grantees(d.ID) {
		if other != botID {
			s.applyDrives(other)
		}
	}
}

var errDriveGone = errors.New("the drive was removed")

// editSecrets changes a drive's secrets as they are now, under the row's
// lock, and writes the extra columns with them. A sign-in and a token refresh
// own the dynamic values and the add form the user's answers: none writes
// back the others' part from an older read. It answers the new column.
func (s *Service) editSecrets(driveID string, extra map[string]any, edit func(*driveSecrets)) (string, error) {
	var out string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var d db.Drive
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "secrets_json").Where("id = ?", driveID).Limit(1).Find(&d).Error; err != nil {
			return err
		}
		if d.ID == "" {
			return errDriveGone
		}
		sec := decodeSecrets(d.SecretsJSON)
		edit(&sec)
		out = mustJSONString(sec)
		cols := map[string]any{"secrets_json": out}
		for k, v := range extra {
			cols[k] = v
		}
		return tx.Model(&db.Drive{}).Where("id = ?", driveID).Updates(cols).Error
	})
	return out, err
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
		sess.dirty()
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
				s.setMountState(botID, d.ID, driveError, err.Error())
			}
		}
	}()
}

// applyDrives has a connected sidecar sent the desired set as it is when it
// goes out, or starts one.
func (s *Service) applyDrives(botID string) {
	if sess := s.driveHub().get(botID); sess != nil {
		sess.dirty()
		return
	}
	s.EnsureBg(botID)
}

// remountAll mounts a drive again from scratch in every Bot it is granted
// to: after a new sign-in, which may be another account. (A token one Bot's
// rclone refreshed only goes to the others' config files: applyAll.)
func (s *Service) remountAll(driveID string) {
	for _, b := range s.grantees(driveID) {
		if sess := s.driveHub().get(b); sess != nil {
			sess.restartDrive(driveID)
			continue
		}
		s.EnsureBg(b)
	}
}

// listDrive asks the sidecar to list folders of a drive (saved or draft).
func (s *Service) listDrive(ctx context.Context, d *db.Drive, path string) (*v1.DriveListResult, error) {
	spec, _, err := s.driveSpec(d)
	if err != nil {
		return nil, err
	}
	via := s.listVia(d)
	if via == "" {
		return nil, errors.New("give the drive to a Bot first: its machine is what reads the drive")
	}
	sess, err := s.ensureDrives(ctx, via, true)
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
			s.saveDriveToken(via, d.ID, tok)
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
		s.db.Model(&db.BotDrive{}).Where("bot_id = ? AND state IN ?", botID,
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
	// The drives are the Workspace's: the Bot's grants go, and the drafts
	// that listed through it.
	s.db.Where("bot_id = ?", botID).Delete(&db.BotDrive{})
	s.db.Where("bot_id = ? AND draft = ?", botID, true).Delete(&db.Drive{})
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
	s.db.Model(&db.BotDrive{}).Where("bot_id = ?", botID).
		Updates(map[string]any{"state": driveStopped, "state_detail": ""})
}

// DropSpace deletes a Workspace's drives, for a Workspace that is going (its
// Bots, and so the grants, are gone already).
func (s *Service) DropSpace(spaceID string) {
	var ids []string
	s.db.Model(&db.Drive{}).Where("space_id = ?", spaceID).Pluck("id", &ids)
	if len(ids) > 0 {
		s.db.Where("drive_id IN ?", ids).Delete(&db.BotDrive{})
	}
	s.db.Where("space_id = ?", spaceID).Delete(&db.Drive{})
}
