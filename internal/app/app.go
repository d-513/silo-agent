package app

import (
	"context"
	"errors"
	"log"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/account"
	"silo.agent/internal/app/admin"
	"silo.agent/internal/app/approval"
	"silo.agent/internal/app/artifact"
	"silo.agent/internal/app/audit"
	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/changes"
	"silo.agent/internal/app/channel"
	"silo.agent/internal/app/command"
	"silo.agent/internal/app/connector"
	"silo.agent/internal/app/dashboard"
	"silo.agent/internal/app/drive"
	"silo.agent/internal/app/egress"
	"silo.agent/internal/app/feed"
	"silo.agent/internal/app/filter"
	"silo.agent/internal/app/imagegen"
	"silo.agent/internal/app/knowledge"
	"silo.agent/internal/app/mailbox"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/pause"
	"silo.agent/internal/app/proactivity"
	"silo.agent/internal/app/room"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/runlog"
	"silo.agent/internal/app/setup"
	"silo.agent/internal/app/siloos"
	"silo.agent/internal/app/skill"
	"silo.agent/internal/app/space"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/app/tunnels"
	"silo.agent/internal/app/voice"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/auth"
	"silo.agent/internal/caddy"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/hub"
	"silo.agent/internal/masker"
	"silo.agent/internal/reader"
	"silo.agent/internal/rpcx"
	"silo.agent/internal/webui"
)

type ctxKey int

const botKey ctxKey = iota

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

// liveRun is a run whose goroutine has not exited. It moves one way through
// three phases, and only these functions move it:
//
//   - live: in App.runs, the conversation's one live run (StartRun refuses a
//     second). Its inbox takes the messages sent to the conversation.
//   - ending: in App.ending, closed set. It takes no more messages, so one
//     sent now starts a new run, but its goroutine is still writing its last
//     events. retire is the only way here: the run itself once its inbox is
//     empty (closeInbox, finish), or a stop (cancelRuns, Shutdown).
//   - exited: in neither map, done closed (runExited).
//
// closed and the move between the maps happen together under injecting and
// App.mu, so inject never queues a message in a run that left App.runs.
type liveRun struct {
	cancel context.CancelFunc
	botID  string
	chatID string
	// stopAs is the final status whoever stopped the run chose (stopped,
	// interrupted); empty while nobody did, so a context that ended anyway
	// ran out of time.
	stopAs atomic.Value
	// started says which taskboard changes are this run's (currentTask).
	started time.Time
	// origin is where the run came from, for a tool called from Python that
	// delivers something back there.
	origin *run.Origin
	// inbox carries user messages injected while the run is live. injecting
	// is held while one is queued and recorded, and while the run takes them,
	// so the run never reads a message whose event is not written yet.
	inbox     chan inboxMsg
	injecting sync.Mutex
	// closed is set, under injecting, when the run retires (see above): a
	// message that arrives after it starts a new run instead of waiting in an
	// inbox nobody reads.
	closed bool
	// stopTurn stops the model turn being written, while a spoken message may
	// still replace it, and turnCut says one did. Under turnMu.
	turnMu   sync.Mutex
	stopTurn context.CancelFunc
	turnCut  bool
	// endTurn is set by a tool that hands the conversation to the human (a
	// question card in this chat): the run ends after the current turn's
	// calls instead of asking the model again. Under App.mu.
	endTurn bool
	// step is the tool the run is in now, for the Bot's status line; "" between
	// tools. Read and written under App.mu.
	step string
	// pySession names the persistent Python interpreter the run's
	// exec_python calls share: the conversation, so a later message finds
	// what an earlier one loaded. An automation's firings each get their own.
	pySession string
	// done closes when the run's goroutine exits, so callers can wait for a
	// stopped run to finish writing before truncating history.
	done chan struct{}
	// wake carries why a sleeping run should get up: an injected message or,
	// for a lead, a subagent finishing. Sends never block.
	wake chan string
}

// stopWith records why the run is being stopped, before its context is
// canceled. The first reason stands.
func (lr *liveRun) stopWith(status string) {
	lr.stopAs.CompareAndSwap(nil, status)
}

// stoppedAs is the status a stop chose, "" when nobody stopped the run.
func (lr *liveRun) stoppedAs() string {
	s, _ := lr.stopAs.Load().(string)
	return s
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
	// Gone ends what a person holds open on a Bot once their access ends.
	Gone access.Watch
	Bus  *bus
	// DriveHTTP reaches drive providers (OAuth token endpoints and account
	// lookups); nil is http.DefaultClient. Tests point it at a fake provider.
	DriveHTTP *http.Client

	// reader is the page reader behind web_read, made on first use.
	readerOnce sync.Once
	reader     *reader.Reader

	heldNotices
	mu        sync.Mutex
	approvals map[string]*waiter
	cmdRun    map[string]string
	runs      map[string]*liveRun
	// ending holds runs that are no longer live (stopped, or past their last
	// turn) but still writing, until their goroutine exits, so a truncation
	// can wait for them.
	ending map[string]*liveRun
	// maskMu guards mask, apart from mu: building a masker reads the
	// database, and run bookkeeping must not wait on that.
	maskMu sync.Mutex
	mask   map[string]*masker.Masker
	// boxes is when each Bot's box was last seen running, for the worker
	// API's check (boxauth.go).
	boxes     boxSeen
	lifecycle sync.Map

	// convs serializes the inject-or-start decision per conversation
	// (lockConv): what one chat waits on never holds up another.
	convs convLocks
	// stopAutomations ends the scheduler loop on Shutdown.
	stopAutomations chan struct{}
	stopOnce        sync.Once
	// collectPause (unix nanos) holds the sweep back after a provider error.
	collectPause atomic.Int64
	// collectBackoff (chat id → collectRestState) rests a chat whose
	// collection failed, so the sweep moves on to the others.
	collectBackoff sync.Map
	// wakeTimers debounce waking a lead chat when its subagents finish.
	wakeMu     sync.Mutex
	wakeTimers map[string]*time.Timer
	// peerMu guards making a Bot's conversation for another Bot, and
	// peerFiles: what runs answering one presented, by chat, to go back with
	// the reply.
	peerMu    sync.Mutex
	peerFiles map[string][]string

	// The domain services: each owns one slice of the UI service and of the
	// Bot's tools. uiHandler promotes their RPCs.
	Feed      *feed.Service
	Runlog    *runlog.Service
	Dashboard *dashboard.Service
	Tunnels   *tunnels.Service
	Mail      *mailbox.Service
	Models    *models.Service
	Approval  *approval.Service
	Filter    *filter.Service
	Workspace *workspace.Service
	Voice     *voice.Service
	// ImageGen is the Bot's generate_image tool.
	ImageGen   *imagegen.Service
	Knowledge  *knowledge.Service
	Changes    *changes.Service
	Memory     *memory.Service
	Connectors *connector.Service
	// Tools is the tool engine: every built-in tool and each Bot's connector
	// tools as definitions, run the same from chat and from Python.
	Tools       *tool.Engine
	Admin       *admin.Service
	Accounts    *account.Service
	Drives      *drive.Service
	Egress      *egress.Service
	Audit       *audit.Service
	Automations *automation.Service
	Channels    *channel.Service
	Skills      *skill.Service
	Artifacts   *artifact.Service
	Pause       *pause.Service
	// SiloOS is the machine's page in Admin (on SiloOS) and the HTTPS proxy.
	SiloOS *siloos.Service
	// Spaces is Workspaces: the containers of Bots and their people.
	Spaces *space.Service
	// Rooms are conversations several Bots and people share.
	Rooms       *room.Service
	Proactivity *proactivity.Service
	Setup       *setup.Service
	// Slash is the slash commands a human types in a conversation.
	Slash *command.Registry
}

func New(store *config.Store, gdb *gorm.DB, eng dockerx.Host) *App {
	a := &App{
		Store:     store,
		DB:        gdb,
		Docker:    eng,
		Hub:       hub.New(),
		Bus:       newBus(),
		approvals: map[string]*waiter{},
		cmdRun:    map[string]string{},
		runs:      map[string]*liveRun{},
		mask:      map[string]*masker.Masker{},

		stopAutomations: make(chan struct{}),
	}
	a.Models = models.New(a.DB, a.cfg)
	a.Approval = approval.New(a.DB, a.cfg, a.Models)
	a.Filter = filter.New(a.cfg, a.Models, a.Approval)
	a.Workspace = workspace.New(a.DB, a.Hub, a.Mask, func(botID, path string) { a.Knowledge.Dirty(botID, path) })
	a.Voice = voice.New(a.DB, a.cfg, a.Models, a.Workspace, a)
	a.ImageGen = imagegen.New(a.cfg, a.Models, a.Workspace)
	a.Knowledge = knowledge.New(a.DB, a.Hub, a.cfg, a.Models, a.Workspace)
	a.Changes = changes.New(a.DB, a.cfg, a.Workspace, a, a.Mask)
	a.Automations = automation.New(a.DB, a, a)
	a.Memory = memory.New(a.DB, a.cfg, a.Models, a.Automations)
	a.Memory.Room = a.reviewRoom
	a.Proactivity = proactivity.New(a.DB, a.Automations)
	a.Skills = skill.New(a.DB, a.Hub, a.cfg, a.Workspace)
	a.Artifacts = artifact.New(a.DB, a.cfg, a.Workspace, a.Skills, a)
	a.Channels = channel.New(a.DB, a.cfg, a, a, a.Mask, a.Workspace, a.Voice)
	a.Connectors = connector.New(a.DB, a.Docker, a.Hub, a.Store, a.cfg, a.Workspace, a.Mask, a)
	a.Drives = drive.New(a.DB, a.Docker, a.Store, a.cfg, a.Connectors, func() *http.Client { return a.DriveHTTP })
	a.Drives.OnChange = a.Changes.RecordDrive
	a.Egress = egress.New(a.DB, a.Docker, a.Store, a.cfg)
	a.Audit = audit.New(a.DB, a.cfg, a)
	a.Egress.OnEvents = a.Audit.IngestEvents
	a.Egress.OnTraffic = a.Audit.IngestTraffic
	a.Connectors.ClientNet = func(ctx context.Context, botID, connectorID, name string) (*dockerx.ClientNet, error) {
		return a.Egress.ClientNetwork(ctx, "stdio", botID, connectorID, name)
	}
	a.Connectors.ClientNetDrop = a.Egress.DropConnector
	a.Connectors.StaleCA = func(st dockerx.State, botID, connectorID string) bool {
		return a.Egress.StaleCA(st, "stdio", botID, connectorID)
	}
	a.Feed = feed.New(a.DB, a)
	a.Runlog = runlog.New(a.DB)
	a.Models.Seen = a.Runlog.Seen
	a.Pause = pause.New(a.DB)
	a.SiloOS = siloos.New(a.DB, a.cfg)
	a.Spaces = space.New(a.DB, a.cfg, spaceHost{a})
	a.Spaces.OnDelete(a.Skills.DropSpace)
	a.Spaces.OnDelete(a.Connectors.DropSpace)
	a.Spaces.OnDelete(a.Drives.DropSpace)
	a.Rooms = room.New(a.DB, a)
	a.Spaces.OnDelete(a.Rooms.DropSpace)
	a.Spaces.Lend("connector", a.Connectors)
	a.Spaces.Lend("drive", a.Drives)
	a.Setup = setup.New(a.DB, a)
	a.Dashboard = dashboard.New(a.DB, a, a.Memory, a.Automations, a.Feed)
	a.Tunnels = tunnels.New(a.DB, a.Hub, a.cfg, a)
	a.Mail = mailbox.New(a.DB, a.cfg, a, a, a.Workspace)
	a.Accounts = account.New(a.DB, a.cfg, a)
	a.Admin = admin.New(a.DB, a.Store, a.cfg, a.Models, a.Voice, a.Accounts.OIDCRedirectURL)
	a.Tools = a.newTools()
	a.Slash = command.New(a.commandDefs())
	a.recoverOrphans()
	a.Connectors.Init()
	a.Connectors.ReconcileStdio()
	a.Drives.Reconcile()
	a.Egress.Reconcile(context.Background())
	a.Connectors.Resume()
	a.migrateSettings()
	a.Channels.Reconcile()
	if a.DB != nil {
		a.Automations.Reschedule()
		a.Automations.UpgradePinned()
		go tickLoop(a.stopAutomations, automation.Tick, func(now time.Time) { a.Automations.FireDue(now) })
		go tickLoop(a.stopAutomations, collectTick, func(now time.Time) { a.SweepMemories(now) })
		go tickLoop(a.stopAutomations, collectTick, func(now time.Time) { a.Memory.SweepDigests(context.Background(), now) })
		go tickLoop(a.stopAutomations, collectTick, func(now time.Time) { a.Memory.Tend(context.Background(), now) })
		a.Knowledge.Recover()
		go tickLoop(a.stopAutomations, knowledge.Tick, func(now time.Time) { a.Knowledge.Sweep(now) })
		go tickLoop(a.stopAutomations, dashboard.Tick, func(now time.Time) { a.Dashboard.Sweep(now) })
		go tickLoop(a.stopAutomations, egress.Tick, func(now time.Time) { a.Egress.Reconcile(context.Background()) })
		go tickLoop(a.stopAutomations, audit.Tick, func(now time.Time) { a.Audit.Trim(now) })
		go tickLoop(a.stopAutomations, artifact.SweepTick, func(time.Time) { a.Artifacts.Sweep(context.Background()) })
		go tickLoop(a.stopAutomations, setup.Tick, func(now time.Time) { a.Setup.Sweep(now) })
		// Caddy is given what public_url and content.host call for, and given it
		// again if it lost it.
		go func() {
			a.SiloOS.SyncProxy(context.Background())
			tickLoop(a.stopAutomations, siloos.Tick, func(time.Time) { a.SiloOS.SyncProxy(context.Background()) })
		}()
		// The SMTP listener follows mail.* without a restart.
		a.Mail.Sync()
		go tickLoop(a.stopAutomations, mailbox.Tick, func(time.Time) { a.Mail.Sync() })
		go a.prefetchImages()
		go tickLoop(a.stopAutomations, imageTick, func(time.Time) {
			if a.cfg().PullPolicy() == config.PullAlways {
				a.prefetchImages()
			}
		})
	}
	return a
}

// imageTick is how often images.pull: always asks the registry for a moved
// tag. What it fetches reaches the gateway on its next Reconcile and the
// other containers when each is next made (see dropStale).
const imageTick = time.Hour

// prefetchImages pulls the images this config runs, so the first start after
// an upgrade does not wait on a download (dockerx.Engine.Prefetch).
func (a *App) prefetchImages() {
	if a.Docker == nil {
		return
	}
	cfg := a.cfg()
	refs := []string{cfg.BotImage, cfg.MCPStdioImage, cfg.DriveImage()}
	if cfg.Egress.Enabled {
		refs = append(refs, cfg.EgressImage())
	}
	a.Docker.Prefetch(context.Background(), refs)
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
	a.DB.Model(&db.BotMessage{}).Where("status IN ?", []string{"running", "replying"}).Updates(map[string]any{"status": "interrupted", "reported": true})
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
	a.Emit(run.BotID, run.ChatID, run.ID, "done", status, "")
}

func (a *App) Shutdown() {
	a.stopOnce.Do(func() { close(a.stopAutomations) })
	a.Channels.StopAll()
	a.Mail.Close()
	a.Hub.CloseAll()
	// The runs end as a restart would leave them (recoverOrphans), and take
	// no more messages on the way down.
	a.mu.Lock()
	live := maps.Clone(a.runs)
	a.mu.Unlock()
	for id, lr := range live {
		lr.stopWith("interrupted")
		a.retire(id, lr, false)
		lr.cancel()
	}
	a.mu.Lock()
	for id, w := range a.approvals {
		select {
		case w.ch <- "canceled":
		default:
		}
		delete(a.approvals, id)
	}
	a.mu.Unlock()
	a.Connectors.CloseAll()
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
	a.maskMu.Lock()
	defer a.maskMu.Unlock()
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

// publicRPCs are the UI procedures that answer with nobody signed in: signing
// in itself, and what the sign-in and invite pages need to draw.
var publicRPCs = map[string]bool{
	silov1connect.UISignInProcedure:       true,
	silov1connect.UIAuthOptionsProcedure:  true,
	silov1connect.UIGetInviteProcedure:    true,
	silov1connect.UIAcceptInviteProcedure: true,
}

// seenRPCs are public procedures that still learn who is asking when someone
// is signed in: a Workspace invite link joins the signed-in user, and makes
// an account for a visitor only while the operator allows it.
var seenRPCs = map[string]bool{
	silov1connect.UIGetSpaceInviteProcedure: true,
	silov1connect.UIJoinSpaceProcedure:      true,
}

func (a *App) interceptUI(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if seenRPCs[req.Spec().Procedure] {
			if signed, err := a.signedIn(ctx); err == nil {
				ctx = signed
			}
			return next(ctx, req)
		}
		if publicRPCs[req.Spec().Procedure] {
			return next(ctx, req)
		}
		ctx, err := a.signedIn(ctx)
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (a *App) interceptUIStream(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := a.signedIn(ctx)
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// signedIn puts the request's user and session on ctx, or says why not.
func (a *App) signedIn(ctx context.Context) (context.Context, error) {
	r := access.Request(ctx)
	if r == nil {
		return ctx, connect.NewError(connect.CodeUnauthenticated, nil)
	}
	s, u, err := auth.SessionFromRequest(a.DB, r, a.cfg().HostCookies())
	if err != nil {
		return ctx, sessionError(err)
	}
	return access.WithSession(access.WithUser(ctx, u), s), nil
}

func sessionError(err error) *connect.Error { return access.SessionError(err) }

func (a *App) interceptWorker(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		bot, tok, err := a.workerBot(ctx, req.Header().Get("Authorization"))
		if err != nil {
			return nil, err
		}
		a.Mask(bot.ID).Add(tok)
		return next(context.WithValue(ctx, botKey, bot), req)
	}
}

func (a *App) interceptWorkerStream(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		bot, tok, err := a.workerBot(ctx, conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return err
		}
		a.Mask(bot.ID).Add(tok)
		return next(context.WithValue(ctx, botKey, bot), conn)
	}
}

func currentBot(ctx context.Context) *db.Bot {
	b, _ := ctx.Value(botKey).(*db.Bot)
	return b
}

func (a *App) botFromToken(h string) (*db.Bot, string, error) {
	return access.RowByToken[db.Bot](a.DB, "token_hash", h)
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	uiPath, uiH := silov1connect.NewUIHandler(a.uiHandler(), connect.WithInterceptors(rpcx.Handler{Unary: a.interceptUI, Stream: a.interceptUIStream}))
	wkPath, wkH := silov1connect.NewBotWorkerHandler(a, connect.WithInterceptors(rpcx.Handler{Unary: a.interceptWorker, Stream: a.interceptWorkerStream}))
	brPath, brH := silov1connect.NewMCPHostHandler(a.Connectors, connect.WithInterceptors(rpcx.Handler{Stream: a.Connectors.InterceptBridge}))
	drPath, drH := silov1connect.NewDriveHostHandler(a.Drives, connect.WithInterceptors(rpcx.Handler{Stream: a.Drives.Intercept}))
	egPath, egH := silov1connect.NewEgressHostHandler(a.Egress, connect.WithInterceptors(rpcx.Handler{Stream: a.Egress.Intercept}))
	mux.Handle(uiPath, uiH)
	mux.Handle(wkPath, wkH)
	mux.Handle(brPath, brH)
	mux.Handle(drPath, drH)
	mux.Handle(egPath, egH)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/vnc", a.handleVNC)
	mux.HandleFunc("/console", a.handleConsole)
	mux.HandleFunc("/voice/call", a.handleVoiceCall)
	mux.HandleFunc("/voice/speak", a.Voice.ServeSpeak)
	mux.HandleFunc("/oauth/callback", a.Connectors.ServeOAuthCallback)
	mux.HandleFunc("/connectors/", a.Connectors.ServeImage)
	mux.HandleFunc("/artifacts/", a.Artifacts.ServeHTTP)
	mux.HandleFunc("/tunnels/auth", a.Tunnels.ServeAuth)
	mux.HandleFunc(caddy.AskPath, a.SiloOS.ServeAsk)
	mux.HandleFunc(mailbox.RawPath, a.Mail.ServeRaw)
	mux.HandleFunc(account.OIDCStartPath, a.Accounts.ServeOIDCStart)
	mux.HandleFunc(account.OIDCCallbackPath, a.Accounts.ServeOIDCCallback)
	// A build that carries the console (the webui tag) serves it under every
	// path nothing above owns; a dev build leaves that to Vite.
	if ui := webui.Handler(); ui != nil {
		mux.Handle("/", ui)
	}
	log.Printf("mounted %s %s", uiPath, wkPath)
	// <kind>-<name>.<content.host> is what Bots made; every other host is the
	// control plane.
	return a.contentRoute(access.WithHTTP(mux))
}

// contentRoute serves the content host: tunnel-<name> into a Bot's service,
// app-<name> a live app artifact, share-<name> an owner's public link. A
// content host label of no known kind, and anything while there is no content
// host, is the control plane's.
func (a *App) contentRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := a.cfg()
		kind, name, ok := cfg.ContentFromHost(r.Host)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		switch kind {
		case config.ContentTunnel:
			if cfg.TunnelsOn() {
				a.Tunnels.Serve(w, r, name)
				return
			}
		case config.ContentApp:
			a.Artifacts.ServeApp(w, r, name)
			return
		case config.ContentShare:
			a.Artifacts.ServeShare(w, r, name)
			return
		}
		tunnels.Page(w, http.StatusNotFound, "Nothing here", "There is nothing at this address.")
	})
}

// LiveRuns names the runs at work on a Bot now, for the audit timeline: a
// gateway event can be put down to a run only when exactly one was live.
func (a *App) LiveRuns(botID string) []audit.LiveRun {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []audit.LiveRun
	for id, lr := range a.runs {
		if lr.botID == botID {
			out = append(out, audit.LiveRun{RunID: id, ChatID: lr.chatID})
		}
	}
	return out
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

// retire moves a live run to ending (see liveRun): it takes no more
// messages and is no longer the conversation's live run. With onlyIfEmpty it
// does so only while no message waits in the inbox, and reports whether it
// did. Retiring an ending run again changes nothing and reports true.
func (a *App) retire(runID string, lr *liveRun, onlyIfEmpty bool) bool {
	lr.injecting.Lock()
	defer lr.injecting.Unlock()
	if onlyIfEmpty && !lr.closed && len(lr.inbox) > 0 {
		return false
	}
	lr.closed = true
	a.mu.Lock()
	if a.runs[runID] == lr {
		delete(a.runs, runID)
		if a.ending == nil {
			a.ending = map[string]*liveRun{}
		}
		a.ending[runID] = lr
	}
	a.mu.Unlock()
	return true
}

// runExited forgets a run whose goroutine is done.
func (a *App) runExited(runID string) {
	a.mu.Lock()
	delete(a.runs, runID)
	delete(a.ending, runID)
	a.mu.Unlock()
}

// convLocks is a mutex per conversation, made on first use and dropped when
// nobody holds or waits for it.
type convLocks struct {
	mu sync.Mutex
	m  map[string]*convLock
}

type convLock struct {
	mu sync.Mutex
	n  int
}

// lockConv takes the conversation lock of chatID and returns its release.
// Never hold two: a caller that needs another conversation releases first.
func (a *App) lockConv(chatID string) (unlock func()) {
	c := &a.convs
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*convLock{}
	}
	l := c.m[chatID]
	if l == nil {
		l = &convLock{}
		c.m[chatID] = l
	}
	l.n++
	c.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		c.mu.Lock()
		if l.n--; l.n == 0 {
			delete(c.m, chatID)
		}
		c.mu.Unlock()
	}
}

// trackedRun is a run whose goroutine has not exited, live or ending.
func (a *App) trackedRun(runID string) *liveRun {
	a.mu.Lock()
	defer a.mu.Unlock()
	if lr := a.runs[runID]; lr != nil {
		return lr
	}
	return a.ending[runID]
}

func (a *App) cancelBot(botID string) {
	// Its subagents stop with it; they must not wake a lead on the way down.
	a.quietSubagents("interrupted", "bot_id = ?", botID)
	// So does what it is waiting on from other Bots.
	var waiting []string
	a.DB.Model(&db.BotMessage{}).Distinct().Where("from_bot_id = ? AND status = ?", botID, "running").Pluck("from_chat_id", &waiting)
	for _, chatID := range waiting {
		a.stopBotMessages(chatID)
	}
	a.cancelRuns(botID, "", "interrupted")
}

func (a *App) stopChat(botID, chatID string) {
	a.cancelRuns(botID, chatID, "stopped")
}

// cancelRuns stops a Bot's live runs (or one chat's). Each run, its pending
// approvals and its row are settled here, before the goroutine unwinds: the
// run retires (a message sent from now on starts a new run), its final status
// is runStatus (finish keeps it), and an approval waiting on it is canceled,
// the same word the call's own gate writes when its context ends.
//
// Only the runs it stops are touched: a run already ending (past its last
// turn, writing its done) finishes with the status it earned.
func (a *App) cancelRuns(botID, chatID, runStatus string) {
	a.mu.Lock()
	stopping := map[string]*liveRun{}
	for id, lr := range a.runs {
		if lr.botID == botID && (chatID == "" || lr.chatID == chatID) {
			stopping[id] = lr
		}
	}
	a.mu.Unlock()
	ids := make([]string, 0, len(stopping))
	for id, lr := range stopping {
		lr.stopWith(runStatus)
		a.retire(id, lr, false)
		lr.cancel()
		ids = append(ids, id)
	}
	a.mu.Lock()
	for id, w := range a.approvals {
		if w.botID != botID {
			continue
		}
		// A Bot-wide stop ends every wait on the Bot, a background
		// process's too.
		if _, ok := stopping[w.runID]; !ok && chatID != "" {
			continue
		}
		select {
		case w.ch <- "canceled":
		default:
		}
		delete(a.approvals, id)
	}
	a.mu.Unlock()
	if len(ids) > 0 {
		a.DB.Model(&db.Run{}).Where("id IN ? AND status = ?", ids, "running").Updates(map[string]any{"status": runStatus, "duration_ms": runTook})
	}
	aq := a.DB.Model(&db.Approval{}).Where("bot_id = ? AND status = ?", botID, "pending")
	if chatID != "" {
		if len(ids) == 0 {
			return
		}
		aq = aq.Where("run_id IN ?", ids)
	}
	aq.Update("status", "canceled")
}
