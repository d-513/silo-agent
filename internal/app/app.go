package app

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/auth"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/masker"
)

type ctxKey int

const (
	reqKey ctxKey = iota
	rwKey
	userKey
	botKey
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
}

type liveRun struct {
	cancel context.CancelFunc
	botID  string
}

type App struct {
	Cfg    *config.Config
	DB     *gorm.DB
	Docker dockerx.Host
	Hub    *hub.Hub
	Bus    *bus

	mu        sync.Mutex
	approvals map[string]*waiter
	cmdRun    map[string]string
	runs      map[string]*liveRun
	mask      map[string]*masker.Masker
	lifecycle sync.Map
}

func New(cfg *config.Config, gdb *gorm.DB, eng dockerx.Host) *App {
	a := &App{
		Cfg:       cfg,
		DB:        gdb,
		Docker:    eng,
		Hub:       hub.New(),
		Bus:       newBus(),
		approvals: map[string]*waiter{},
		cmdRun:    map[string]string{},
		runs:      map[string]*liveRun{},
		mask:      map[string]*masker.Masker{},
	}
	a.recoverOrphans()
	return a
}

func (a *App) recoverOrphans() {
	if a.DB == nil {
		return
	}
	res := a.DB.Model(&db.Run{}).Where("status = ?", "running").Update("status", "interrupted")
	if res.RowsAffected > 0 {
		log.Printf("interrupted %d orphaned runs", res.RowsAffected)
	}
	res = a.DB.Model(&db.Approval{}).Where("status = ?", "pending").Update("status", "interrupted")
	if res.RowsAffected > 0 {
		log.Printf("interrupted %d orphaned approvals", res.RowsAffected)
	}
}

func (a *App) Shutdown() {
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
	a.mu.Unlock()
}

func ListenAndServe(cfg *config.Config, h http.Handler) error {
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: h}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sh, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return srv.Shutdown(sh)
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
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		return next(context.WithValue(ctx, userKey, u), req)
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
			return connect.NewError(connect.CodeUnauthenticated, err)
		}
		return next(context.WithValue(ctx, userKey, u), conn)
	}
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

func currentUser(ctx context.Context) *db.User {
	u, _ := ctx.Value(userKey).(*db.User)
	return u
}

func currentBot(ctx context.Context) *db.Bot {
	b, _ := ctx.Value(botKey).(*db.Bot)
	return b
}

func (a *App) botFromToken(h string) (*db.Bot, string, error) {
	tok := strings.TrimPrefix(h, "Bearer ")
	if tok == "" {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	var b db.Bot
	if err := a.DB.First(&b, "token_hash = ?", ids.Hash(tok)).Error; err != nil {
		return nil, "", err
	}
	return &b, tok, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	uiPath, uiH := silov1connect.NewUIHandler(a, connect.WithInterceptors(uiInterceptor{a}))
	wkPath, wkH := silov1connect.NewBotWorkerHandler(a, connect.WithInterceptors(workerInterceptor{a}))
	mux.Handle(uiPath, uiH)
	mux.Handle(wkPath, wkH)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/vnc", a.handleVNC)
	log.Printf("mounted %s %s", uiPath, wkPath)
	return withHTTP(mux)
}

func (a *App) trackRun(botID, runID string, cancel context.CancelFunc) {
	a.mu.Lock()
	a.runs[runID] = &liveRun{cancel: cancel, botID: botID}
	a.mu.Unlock()
}

func (a *App) untrackRun(runID string) {
	a.mu.Lock()
	delete(a.runs, runID)
	a.mu.Unlock()
}

func (a *App) cancelBot(botID string) {
	a.mu.Lock()
	for id, lr := range a.runs {
		if lr.botID == botID {
			lr.cancel()
			delete(a.runs, id)
		}
	}
	for id, w := range a.approvals {
		if w.botID == botID {
			select {
			case w.ch <- "canceled":
			default:
			}
			delete(a.approvals, id)
		}
	}
	a.mu.Unlock()
	a.DB.Model(&db.Run{}).Where("bot_id = ? AND status = ?", botID, "running").Update("status", "interrupted")
	a.DB.Model(&db.Approval{}).Where("bot_id = ? AND status = ?", botID, "pending").Update("status", "interrupted")
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
