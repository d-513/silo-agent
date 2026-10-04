package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/feed"
	"silo.agent/internal/app/models"
	"silo.agent/internal/auth"
	"silo.agent/internal/channels"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/masker"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/rpcx"
)

type ctxKey int

const (
	reqKey ctxKey = iota
	rwKey
	botKey
	bridgeKey
)

type bus struct {
	mu   sync.Mutex
	subs map[string][]*sub
}

type sub struct {
	ch   chan *v1.RunEvent
	once sync.Once
}

func (s *sub) close() {
	s.once.Do(func() { close(s.ch) })
}

func newBus() *bus { return &bus{subs: map[string][]*sub{}} }

func (b *bus) Publish(botID string, ev *v1.RunEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	xs := b.subs[botID]
	out := xs[:0]
	for _, s := range xs {
		select {
		case s.ch <- ev:
			out = append(out, s)
		default:
			s.close()
		}
	}
	if len(out) == 0 {
		delete(b.subs, botID)
	} else {
		b.subs[botID] = out
	}
}

func (b *bus) Subscribe(botID string) (<-chan *v1.RunEvent, func()) {
	s := &sub{ch: make(chan *v1.RunEvent, 256)}
	b.mu.Lock()
	b.subs[botID] = append(b.subs[botID], s)
	b.mu.Unlock()
	return s.ch, func() {
		b.mu.Lock()
		xs := b.subs[botID]
		for i, c := range xs {
			if c == s {
				b.subs[botID] = append(xs[:i], xs[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		s.close()
	}
}

type waiter struct {
	ch    chan string
	botID string
	runID string
}

type liveRun struct {
	cancel context.CancelFunc
	botID  string
	chatID string
	// inbox carries user messages injected while the run is live.
	inbox chan inboxMsg
	// done closes when the run's goroutine exits, so callers can wait for a
	// stopped run to finish writing before truncating history.
	done chan struct{}
	// wake carries why a sleeping run should get up: an injected message or,
	// for a lead, a subagent finishing. Sends never block.
	wake chan string
}

// signal nudges a sleeping run. A full buffer already has a reason queued.
func (lr *liveRun) signal(why string) {
	if lr == nil || lr.wake == nil {
		return
	}
	select {
	case lr.wake <- why:
	default:
	}
}

type App struct {
	Store  *config.Store
	DB     *gorm.DB
	Docker dockerx.Host
	Hub    *hub.Hub
	Bus    *bus
	// DriveHTTP reaches drive providers (OAuth token endpoints and account
	// lookups); nil is http.DefaultClient. Tests point it at a fake provider.
	DriveHTTP *http.Client

	mu        sync.Mutex
	approvals map[string]*waiter
	cmdRun    map[string]string
	runs      map[string]*liveRun
	mask      map[string]*masker.Masker
	oauth     map[string]*oauthWait
	mcp       map[string]*mcpx.Session
	lifecycle sync.Map
	bridgesMu sync.Mutex
	bridges   map[string]*bridgeTunnel // botConnectorID -> live reverse tunnel
	// drivesHub holds each Bot's live drive sidecar session.
	drivesOnce sync.Once
	drivesHub  *driveHub

	// convMu serializes the inject-or-start decision per conversation. chatMu
	// serializes find-or-create of a channel conversation.
	convMu     sync.Mutex
	chatMu     sync.Mutex
	chanMu     sync.Mutex
	chanCancel map[string]context.CancelFunc
	chanStates map[string]channels.State
	// stopAutomations ends the scheduler loop on Shutdown.
	stopAutomations chan struct{}
	stopOnce        sync.Once
	// collecting holds the chats the memory collector is reading now, so a
	// manual collection and the sweep never read the same delta twice.
	collecting sync.Map
	// collectPause (unix nanos) holds the sweep back after a provider error.
	collectPause atomic.Int64
	// knowLocks serializes syncs per knowledge folder; knowCancel stops a
	// running one when its folder is removed; knowPause (unix nanos) holds the
	// sweep back after a provider error.
	knowLocks  sync.Map
	knowCancel sync.Map
	knowPause  atomic.Int64
	// wakeTimers debounce waking a lead chat when its subagents finish.
	wakeMu     sync.Mutex
	wakeTimers map[string]*time.Timer

	feed   *feed.Service
	models *models.Service

	// bridgeTransportFn is a test seam; when set it replaces the real
	// sidecar container + reverse tunnel for STDIO connectors.
	bridgeTransportFn func(ctx context.Context, row *db.BotConnector, c *db.Connector) (mcp.Transport, error)
}

func New(store *config.Store, gdb *gorm.DB, eng dockerx.Host) *App {
	a := &App{
		Store:      store,
		DB:         gdb,
		Docker:     eng,
		Hub:        hub.New(),
		Bus:        newBus(),
		approvals:  map[string]*waiter{},
		cmdRun:     map[string]string{},
		runs:       map[string]*liveRun{},
		mask:       map[string]*masker.Masker{},
		oauth:      map[string]*oauthWait{},
		mcp:        map[string]*mcpx.Session{},
		bridges:    map[string]*bridgeTunnel{},
		chanCancel: map[string]context.CancelFunc{},
		chanStates: map[string]channels.State{},

		stopAutomations: make(chan struct{}),
	}
	a.models = models.New(a.DB, a.cfg)
	a.feed = feed.New(a.DB, a)
	a.recoverOrphans()
	a.initConnectors()
	a.reconcileStdio()
	a.reconcileDrives()
	a.resumeConnectors()
	a.migrateSettings()
	a.reconcileChannels()
	if a.DB != nil {
		a.rescheduleAutomations()
		go tickLoop(a.stopAutomations, automationTick, func(now time.Time) { a.FireDueAutomations(now) })
		go tickLoop(a.stopAutomations, collectTick, func(now time.Time) { a.SweepMemories(now) })
		a.recoverKnowledge()
		go tickLoop(a.stopAutomations, knowledgeTick, func(now time.Time) { a.SweepKnowledge(now) })
	}
	return a
}

// tickLoop calls fn on every tick until stop is closed: the shape of each
// background sweep (automations, memory collection, knowledge sync).
func tickLoop(stop <-chan struct{}, every time.Duration, fn func(now time.Time)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			fn(now)
		}
	}
}

func (a *App) cfg() config.Config {
	if a.Store == nil {
		return config.Config{}
	}
	return a.Store.Config()
}

func (a *App) migrateSettings() {
	if a.Store == nil || a.DB == nil {
		return
	}
	// settings is a legacy table. New databases do not create it, so avoid
	// issuing a query that would make SQLite log "no such table" on startup.
	if !a.DB.Migrator().HasTable("settings") {
		return
	}
	var rows []struct {
		Key   string `gorm:"column:key"`
		Value string `gorm:"column:value"`
	}
	if err := a.DB.Table("settings").Find(&rows).Error; err != nil {
		return
	}
	patch := map[string]string{}
	for _, r := range rows {
		if r.Key != "model" && r.Key != "search.engine" {
			continue
		}
		if strings.TrimSpace(r.Value) == "" {
			continue
		}
		if a.Store.Source(r.Key) == config.SourceYAML {
			continue
		}
		patch[r.Key] = r.Value
	}
	if len(patch) == 0 {
		return
	}
	if err := a.Store.Patch(patch); err != nil {
		log.Printf("settings migrate: %v", err)
	}
}

func (a *App) recoverOrphans() {
	if a.DB == nil {
		return
	}
	var runs []db.Run
	a.DB.Where("status = ?", "running").Find(&runs)
	if len(runs) > 0 {
		a.DB.Model(&db.Run{}).Where("status = ?", "running").Update("status", "interrupted")
		for _, r := range runs {
			a.ensureTerminalEvent(r, "interrupted")
		}
		log.Printf("interrupted %d orphaned runs", len(runs))
	}
	a.DB.Model(&db.Subagent{}).Where("status = ?", "running").Updates(map[string]any{"status": "interrupted", "reported": true})
	res := a.DB.Model(&db.Approval{}).Where("status = ?", "pending").Update("status", "interrupted")
	if res.RowsAffected > 0 {
		log.Printf("interrupted %d orphaned approvals", res.RowsAffected)
	}
}

// ensureTerminalEvent appends a done event for a run that is no longer live so
// replayed history never leaves the client spinning on an open run.
func (a *App) ensureTerminalEvent(run db.Run, status string) {
	var n int64
	a.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", run.ID, "done").Count(&n)
	if n > 0 {
		return
	}
	a.emit(run.BotID, run.ChatID, run.ID, "done", status, "")
}

func (a *App) Shutdown() {
	a.stopOnce.Do(func() { close(a.stopAutomations) })
	a.chanMu.Lock()
	for id, cancel := range a.chanCancel {
		cancel()
		delete(a.chanCancel, id)
	}
	a.chanMu.Unlock()
	a.Hub.CloseAll()
	a.mu.Lock()
	for _, lr := range a.runs {
		lr.cancel()
	}
	a.runs = map[string]*liveRun{}
	for id, w := range a.approvals {
		select {
		case w.ch <- "canceled":
		default:
		}
		delete(a.approvals, id)
	}
	for id, s := range a.mcp {
		go s.Close()
		delete(a.mcp, id)
	}
	a.mu.Unlock()
	// Deliberately leave STDIO sidecars running: their bridges reconnect when
	// the CP comes back, so a CP restart does not re-pull npm packages. Genuine
	// orphans are reclaimed by reconcileStdio on the next start.
}

func ListenAndServe(cfg *config.Config, h http.Handler, onStop func()) error {
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: h}
	rpcx.EnableH2C(srv)
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errc:
		signal.Stop(sigc)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-sigc:
		signal.Stop(sigc)
		if onStop != nil {
			onStop()
		}
		if err := srv.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func (a *App) Mask(botID string) *masker.Masker {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.mask[botID]
	if m == nil {
		m = masker.New()
		var secs []db.Secret
		_ = a.DB.Where("bot_id = ?", botID).Find(&secs).Error
		var vs []string
		for _, s := range secs {
			vs = append(vs, s.Value)
		}
		m.AddMany(vs)
		a.mask[botID] = m
	}
	return m
}

func withHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), reqKey, r)
		ctx = context.WithValue(ctx, rwKey, w)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func httpReq(ctx context.Context) *http.Request {
	r, _ := ctx.Value(reqKey).(*http.Request)
	return r
}

func httpRW(ctx context.Context) http.ResponseWriter {
	w, _ := ctx.Value(rwKey).(http.ResponseWriter)
	return w
}

func (a *App) interceptUI(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if strings.HasSuffix(req.Spec().Procedure, "SignIn") {
			return next(ctx, req)
		}
		r := httpReq(ctx)
		if r == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}
		u, err := auth.UserFromRequest(a.DB, r)
		if err != nil {
			return nil, sessionError(err)
		}
		return next(access.WithUser(ctx, u), req)
	}
}

func (a *App) interceptUIStream(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		r := httpReq(ctx)
		if r == nil {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}
		u, err := auth.UserFromRequest(a.DB, r)
		if err != nil {
			return sessionError(err)
		}
		return next(access.WithUser(ctx, u), conn)
	}
}

// sessionError maps a session lookup failure: ErrAuth is a sign-out, anything
// else is the store failing, which the UI must retry rather than treat as one.
func sessionError(err error) *connect.Error {
	if errors.Is(err, auth.ErrAuth) {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	log.Printf("session lookup: %v", err)
	return connect.NewError(connect.CodeUnavailable, errors.New("session store unavailable"))
}

// httpSessionError is sessionError for plain HTTP handlers.
func httpSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrAuth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	log.Printf("session lookup: %v", err)
	http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
}

func (a *App) interceptWorker(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		bot, tok, err := a.botFromToken(req.Header().Get("Authorization"))
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		a.Mask(bot.ID).Add(tok)
		return next(context.WithValue(ctx, botKey, bot), req)
	}
}

func (a *App) interceptWorkerStream(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		bot, tok, err := a.botFromToken(conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return connect.NewError(connect.CodeUnauthenticated, err)
		}
		a.Mask(bot.ID).Add(tok)
		return next(context.WithValue(ctx, botKey, bot), conn)
	}
}

func currentBot(ctx context.Context) *db.Bot {
	b, _ := ctx.Value(botKey).(*db.Bot)
	return b
}

// rowByToken resolves an Authorization header to the row whose hashColumn holds
// the hash of its bearer token, and returns the raw token so the caller can mask
// it. A missing or unknown token is Unauthenticated; a store failure is itself.
func rowByToken[T any](gdb *gorm.DB, hashColumn, header string) (*T, string, error) {
	tok := strings.TrimPrefix(header, "Bearer ")
	if tok == "" || gdb == nil {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	var row T
	res := gdb.Where(hashColumn+" = ?", ids.Hash(tok)).Limit(1).Find(&row)
	if res.Error != nil {
		return nil, "", res.Error
	}
	if res.RowsAffected == 0 {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	return &row, tok, nil
}

func (a *App) botFromToken(h string) (*db.Bot, string, error) {
	return rowByToken[db.Bot](a.DB, "token_hash", h)
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	uiPath, uiH := silov1connect.NewUIHandler(a.uiHandler(), connect.WithInterceptors(rpcx.Handler{Unary: a.interceptUI, Stream: a.interceptUIStream}))
	wkPath, wkH := silov1connect.NewBotWorkerHandler(a, connect.WithInterceptors(rpcx.Handler{Unary: a.interceptWorker, Stream: a.interceptWorkerStream}))
	brPath, brH := silov1connect.NewMCPHostHandler(a, connect.WithInterceptors(rpcx.Handler{Stream: a.interceptBridgeStream}))
	drPath, drH := silov1connect.NewDriveHostHandler(a, connect.WithInterceptors(rpcx.Handler{Stream: a.interceptDriveStream}))
	mux.Handle(uiPath, uiH)
	mux.Handle(wkPath, wkH)
	mux.Handle(brPath, brH)
	mux.Handle(drPath, drH)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/vnc", a.handleVNC)
	mux.HandleFunc("/console", a.handleConsole)
	mux.HandleFunc("/oauth/callback", a.handleOAuthCallback)
	mux.HandleFunc("/connectors/", a.handleConnectorImage)
	mux.HandleFunc("/artifacts/", a.handleArtifactDownload)
	log.Printf("mounted %s %s", uiPath, wkPath)
	return withHTTP(mux)
}

func (a *App) trackRun(botID, chatID, runID string, cancel context.CancelFunc, inbox chan inboxMsg, done chan struct{}) {
	a.mu.Lock()
	a.runs[runID] = &liveRun{cancel: cancel, botID: botID, chatID: chatID, inbox: inbox, done: done, wake: make(chan string, 8)}
	a.mu.Unlock()
}

// liveRunFor returns the active run for a conversation, if any.
func (a *App) liveRunFor(botID, chatID string) *liveRun {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, lr := range a.runs {
		if lr.botID == botID && lr.chatID == chatID {
			return lr
		}
	}
	return nil
}

func (a *App) untrackRun(runID string) {
	a.mu.Lock()
	delete(a.runs, runID)
	a.mu.Unlock()
}

func (a *App) cancelBot(botID string) {
	// Its subagents stop with it; they must not wake a lead on the way down.
	a.quietSubagents("interrupted", "bot_id = ?", botID)
	a.cancelRuns(botID, "", "interrupted")
}

func (a *App) stopChat(botID, chatID string) {
	a.cancelRuns(botID, chatID, "stopped")
}

func (a *App) cancelRuns(botID, chatID, runStatus string) {
	a.mu.Lock()
	want := map[string]bool{}
	for id, lr := range a.runs {
		if lr.botID != botID {
			continue
		}
		if chatID != "" && lr.chatID != chatID {
			continue
		}
		lr.cancel()
		delete(a.runs, id)
		want[id] = true
	}
	for id, w := range a.approvals {
		if w.botID != botID {
			continue
		}
		if chatID != "" && !want[w.runID] {
			continue
		}
		select {
		case w.ch <- "canceled":
		default:
		}
		delete(a.approvals, id)
	}
	a.mu.Unlock()
	q := a.DB.Model(&db.Run{}).Where("bot_id = ? AND status = ?", botID, "running")
	if chatID != "" {
		q = q.Where("chat_id = ?", chatID)
	}
	q.Update("status", runStatus)
	aq := a.DB.Model(&db.Approval{}).Where("bot_id = ? AND status = ?", botID, "pending")
	if chatID != "" {
		aq = aq.Where("run_id IN (?)", a.DB.Model(&db.Run{}).Select("id").Where("bot_id = ? AND chat_id = ?", botID, chatID))
	}
	aq.Update("status", "interrupted")
	a.recomputeStatus(botID)
}

func (a *App) recomputeStatus(botID string) {
	var n int64
	a.DB.Model(&db.Approval{}).Where("bot_id = ? AND status = ?", botID, "pending").Count(&n)
	if n > 0 {
		a.setBotStatus(botID, "needs_you")
		return
	}
	a.DB.Model(&db.Run{}).Where("bot_id = ? AND status = ?", botID, "running").Count(&n)
	if n > 0 {
		a.setBotStatus(botID, "working")
		return
	}
	a.setBotStatus(botID, "idle")
}
