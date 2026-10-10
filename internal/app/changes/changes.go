// Package changes is what changed in a Bot's workspace: the worker snapshots
// the workspace into a history store on the Bot's own disk (a shadow git
// repository; cmd/silo-worker/history.go), and this service decides when a
// snapshot is taken, says which runs were working at the time, and serves the
// diffs to the Changes pane. Nothing is kept in Postgres: the store is the
// history, so it needs the Bot's machine to be read.
package changes

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/config"
	"silo.agent/internal/hub"
	"silo.agent/internal/masker"
)

// A snapshot is taken before a run's first tool that can change files and when
// the run ends, so what lies between two snapshots is what the runs live in
// that stretch did; anything found by a run's first snapshot happened outside
// it (the owner in Files or the Console, a background process).
const (
	// beginWait bounds how long a run's first changing tool waits for its
	// snapshot. A slow one is cancelled and the tool goes ahead.
	beginWait = 90 * time.Second
	endWait   = 3 * time.Minute
	listWait  = 2 * time.Minute

	// keep is how many snapshots a Bot keeps, and storeMax its store's size
	// budget; the worker forgets the oldest past either.
	keep     = 300
	storeMax = 1 << 30
)

// Host is what change tracking needs from the App around it.
type Host interface {
	host.Runs
}

// Service takes the snapshots and serves the Changes pane.
type Service struct {
	db   *gorm.DB
	cfg  func() config.Config
	ws   *workspace.Service
	host Host
	mask func(botID string) *masker.Masker

	mu   sync.Mutex
	bots map[string]*botState
	// ended is each Bot's runs that stopped changing things a moment ago: a
	// drive change can land just after its run (drives.go). driveSeen counts
	// journal lines since the last trim.
	ended     map[string][]endedRun
	driveSeen map[string]int
}

type endedRun struct {
	source Source
	at     time.Time
}

// botState is one Bot's runs that have begun changing things. A run's channel
// closes once its opening snapshot is done, so its other tool calls (and its
// end) wait for that instead of racing it.
type botState struct {
	// snap makes the Bot's snapshots take turns.
	snap sync.Mutex
	live map[string]chan struct{}
}

func New(gdb *gorm.DB, cfg func() config.Config, ws *workspace.Service, h Host, mask func(botID string) *masker.Masker) *Service {
	return &Service{db: gdb, cfg: cfg, ws: ws, host: h, mask: mask, bots: map[string]*botState{}, ended: map[string][]endedRun{}, driveSeen: map[string]int{}}
}

func (s *Service) enabled() bool { return s.cfg().Changes.Enabled }

func (s *Service) limits() *v1.HistoryLimits {
	c := s.cfg().Changes
	return &v1.HistoryLimits{MaxFileBytes: c.MaxFileBytes(), Keep: keep, KeepDays: int32(c.Days()), MaxStoreBytes: storeMax}
}

func (s *Service) bot(botID string) *botState {
	st := s.bots[botID]
	if st == nil {
		st = &botState{live: map[string]chan struct{}{}}
		s.bots[botID] = st
	}
	return st
}

// Begin is called before a run's tool that can change the workspace. The first
// call of a run snapshots what is there, so earlier changes are not taken for
// the run's own; later calls return at once. It never fails the tool: a
// snapshot that cannot be taken is logged.
func (s *Service) Begin(ctx context.Context, botID, runID string) {
	if runID == "" || !s.enabled() {
		return
	}
	s.mu.Lock()
	st := s.bot(botID)
	if ready, ok := st.live[runID]; ok {
		s.mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
		}
		return
	}
	ready := make(chan struct{})
	others := s.sources(st, runID)
	st.live[runID] = ready
	s.mu.Unlock()
	defer close(ready)

	ctx, cancel := context.WithTimeout(ctx, beginWait)
	defer cancel()
	st.snap.Lock()
	defer st.snap.Unlock()
	if err := s.checkpoint(ctx, botID, "run_start", others); err != nil && !outdated(err) {
		log.Printf("changes: snapshot before run %s: %v", runID, err)
	}
}

// End is called when a run is over. If the run began changing things, the
// workspace is snapshotted once more: what differs is the run's doing (and
// that of any other run still at work on the same Bot).
func (s *Service) End(botID, runID string) {
	s.mu.Lock()
	st := s.bot(botID)
	ready, ok := st.live[runID]
	s.mu.Unlock()
	if !ok {
		return
	}
	<-ready
	st.snap.Lock()
	defer st.snap.Unlock()
	s.mu.Lock()
	sources := s.sources(st, "")
	s.mu.Unlock()
	if s.enabled() {
		ctx, cancel := context.WithTimeout(context.Background(), endWait)
		if err := s.checkpoint(ctx, botID, "run_end", sources); err != nil && !outdated(err) {
			log.Printf("changes: snapshot after run %s: %v", runID, err)
		}
		cancel()
	}
	s.mu.Lock()
	if src, ok := s.source(runID); ok {
		s.ended[botID] = append(s.recent(botID), endedRun{source: src, at: time.Now()})
	}
	delete(st.live, runID)
	if len(st.live) == 0 {
		delete(s.bots, botID)
	}
	s.mu.Unlock()
}

// recent is the Bot's runs that ended within driveGrace. s.mu is held.
func (s *Service) recent(botID string) []endedRun {
	var out []endedRun
	for _, e := range s.ended[botID] {
		if time.Since(e.at) < driveGrace {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		delete(s.ended, botID)
	} else {
		s.ended[botID] = out
	}
	return out
}

// recentlyWorking names the runs changing things on the Bot now, and the ones
// that stopped within driveGrace.
func (s *Service) recentlyWorking(botID string) []Source {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Source
	seen := map[string]bool{}
	if st := s.bots[botID]; st != nil {
		for _, src := range s.sources(st, "") {
			seen[src.ChatID] = true
			out = append(out, src)
		}
	}
	for _, e := range s.recent(botID) {
		if !seen[e.source.ChatID] {
			seen[e.source.ChatID] = true
			out = append(out, e.source)
		}
	}
	return out
}

// source names one run by the conversation it belongs to.
func (s *Service) source(runID string) (Source, bool) {
	chatID := s.host.ChatOfRun(runID)
	if chatID == "" {
		return Source{}, false
	}
	kind, name := chats.Source(s.db, chatID)
	if kind == "" {
		return Source{}, false
	}
	return Source{Kind: kind, Name: name, ChatID: chatID}, true
}

// Source is one run that was at work, as stored with a snapshot.
type Source struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	ChatID string `json:"chat_id,omitempty"`
}

type note struct {
	Kind    string   `json:"kind"`
	Sources []Source `json:"sources,omitempty"`
}

// sources names the Bot's runs that have begun changing things, but for skip.
// Two runs of one conversation are one source. s.mu is held.
func (s *Service) sources(st *botState, skip string) []Source {
	var out []Source
	seen := map[string]bool{}
	for runID := range st.live {
		if runID == skip {
			continue
		}
		src, ok := s.source(runID)
		if !ok || seen[src.ChatID] {
			continue
		}
		seen[src.ChatID] = true
		out = append(out, src)
	}
	return out
}

// working names the Bot's runs that are changing things right now.
func (s *Service) working(botID string) []Source {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.bots[botID]
	if st == nil {
		return nil
	}
	return s.sources(st, "")
}

func (s *Service) checkpoint(ctx context.Context, botID, kind string, sources []Source) error {
	raw, err := json.Marshal(note{Kind: kind, Sources: sources})
	if err != nil {
		return err
	}
	_, err = s.ws.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_Checkpoint{Checkpoint: &v1.CheckpointCmd{Note: string(raw), Limits: s.limits()}}})
	if errors.Is(err, hub.ErrNoWorker) || errors.Is(err, hub.ErrClosed) || connect.CodeOf(err) == connect.CodeFailedPrecondition {
		return nil // the machine is off: there is nothing to snapshot
	}
	return err
}

// outdated reports a worker error that means the Bot's machine predates change
// tracking: an old worker, or an image without git.
func outdated(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unknown cmd") || strings.Contains(msg, "git is not installed")
}

type row struct {
	ID      string `json:"id"`
	At      int64  `json:"at"`
	Base    string `json:"base"`
	Head    string `json:"head"`
	Files   int32  `json:"files"`
	Added   int32  `json:"added"`
	Deleted int32  `json:"deleted"`
	Note    note   `json:"note"`
}

func stamp(nanos int64) string {
	if nanos <= 0 {
		return ""
	}
	return time.Unix(0, nanos).UTC().Format(time.RFC3339)
}

func (r row) proto(sources []Source, pending bool) *v1.Change {
	c := &v1.Change{Id: r.ID, At: stamp(r.At), Base: r.Base, Head: r.Head, Files: r.Files, Added: r.Added, Deleted: r.Deleted, Pending: pending}
	for _, src := range sources {
		c.Sources = append(c.Sources, &v1.ChangeSource{Kind: src.Kind, Name: src.Name, ChatId: src.ChatID})
	}
	return c
}

func (s *Service) ListChanges(ctx context.Context, req *connect.Request[v1.ListChangesRequest]) (*connect.Response[v1.ListChangesResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	out := &v1.ListChangesResponse{State: "ok", MaxFileBytes: s.cfg().Changes.MaxFileBytes()}
	if !s.enabled() {
		out.State = "off"
		return connect.NewResponse(out), nil
	}
	ctx, cancel := context.WithTimeout(ctx, listWait)
	defer cancel()
	raw, err := s.ws.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_Changes{Changes: &v1.ChangesCmd{Limits: s.limits()}}})
	if outdated(err) {
		out.State = "outdated"
		return connect.NewResponse(out), nil
	}
	if err != nil {
		return nil, err
	}
	var res struct {
		Changes  []row `json:"changes"`
		Pending  *row  `json:"pending"`
		Since    int64 `json:"since"`
		Indexing bool  `json:"indexing"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, err
	}
	out.Since, out.Indexing = stamp(res.Since), res.Indexing
	if res.Pending != nil {
		// Not snapshotted yet, so nothing recorded who: it is whoever is at
		// work now.
		out.Pending = res.Pending.proto(s.working(b.ID), true)
	}
	for _, r := range res.Changes {
		out.Changes = append(out.Changes, r.proto(r.Note.Sources, false))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ListChangeFiles(ctx context.Context, req *connect.Request[v1.ListChangeFilesRequest]) (*connect.Response[v1.ListChangeFilesResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	raw, err := s.ws.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_ChangeFiles{ChangeFiles: &v1.ChangeFilesCmd{Base: req.Msg.GetBase(), Head: req.Msg.GetHead()}}})
	if err != nil {
		return nil, gone(err)
	}
	var res struct {
		Files []struct {
			Path    string `json:"path"`
			OldPath string `json:"old_path"`
			Status  string `json:"status"`
			Added   int32  `json:"added"`
			Deleted int32  `json:"deleted"`
			Binary  bool   `json:"binary"`
			Large   bool   `json:"large"`
			OldSize int64  `json:"old_size"`
			NewSize int64  `json:"new_size"`
		} `json:"files"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, err
	}
	out := &v1.ListChangeFilesResponse{Truncated: res.Truncated}
	for _, f := range res.Files {
		out.Files = append(out.Files, &v1.ChangeFile{
			Path: f.Path, OldPath: f.OldPath, Status: f.Status, Added: f.Added, Deleted: f.Deleted,
			Binary: f.Binary, Large: f.Large, OldSize: f.OldSize, NewSize: f.NewSize,
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Service) GetChangePatch(ctx context.Context, req *connect.Request[v1.GetChangePatchRequest]) (*connect.Response[v1.GetChangePatchResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := workspace.Rel(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	raw, err := s.ws.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_ChangePatch{ChangePatch: &v1.ChangePatchCmd{
		Base: req.Msg.GetBase(), Head: req.Msg.GetHead(), Path: path, OldPath: workspace.Rel(req.Msg.GetOldPath()),
	}}})
	if err != nil {
		return nil, gone(err)
	}
	var res struct {
		Patch     string `json:"patch"`
		Truncated bool   `json:"truncated"`
		Binary    bool   `json:"binary"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, err
	}
	// A diff shows file content, so a secret in it is masked like in ReadFile.
	return connect.NewResponse(&v1.GetChangePatchResponse{
		Patch: s.mask(b.ID).Apply(res.Patch), Truncated: res.Truncated, Binary: res.Binary,
	}), nil
}

// gone turns the worker's "that snapshot is no longer kept" into NotFound, so
// the pane can say so instead of showing a failure.
func gone(err error) error {
	if connect.CodeOf(err) == connect.CodeUnknown && (strings.Contains(err.Error(), "no longer in the history") || strings.Contains(err.Error(), "unknown change")) {
		return connect.NewError(connect.CodeNotFound, errors.New("this change is no longer in the history"))
	}
	return err
}
