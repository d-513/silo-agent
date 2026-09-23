package apptest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/glebarez/sqlite"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/app"
	"silo.agent/internal/auth"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"

	// Registers the deterministic "dummy" model provider for every test binary
	// that uses this harness.
	_ "silo.agent/internal/llm/dummy"
)

var dbSeq atomic.Int64

// H is one isolated control plane: temp data dir, in-memory SQLite, the real
// HTTP handler on an ephemeral h2c listener, and a signed-in UI client.
type H struct {
	T        *testing.T
	App      *app.App
	DB       *gorm.DB
	Store    *config.Store
	Host     dockerx.Host
	Fake     *FakeHost
	URL      string
	Client   silov1connect.UIClient
	HTTP     *http.Client
	DataDir  string
	Email    string
	Password string

	srv *http.Server
	ln  net.Listener
}

type options struct {
	host        dockerx.Host
	hostFactory func(*config.Store) (dockerx.Host, error)
	listener    net.Listener
	yaml        string
	configPath  string
	dataDir     string
	email       string
	password    string
	beforeApp   func(*gorm.DB)
	stdioBridge bool
}

// Option customizes the harness.
type Option func(*options)

// WithHost replaces the fake Docker host with a real one (container tier).
func WithHost(h dockerx.Host) Option { return func(o *options) { o.host = h } }

// WithHostFactory builds the Docker host from the harness config store, which
// a real dockerx.Engine needs (it reads data_dir and cp_url from it).
func WithHostFactory(f func(*config.Store) (dockerx.Host, error)) Option {
	return func(o *options) { o.hostFactory = f }
}

// WithListener serves the CP on a caller-provided listener. Container tests
// bind 0.0.0.0 so the Bot can reach the CP through host.containers.internal.
func WithListener(ln net.Listener) Option { return func(o *options) { o.listener = ln } }

// WithYAML loads config from the given YAML instead of the default test config.
func WithYAML(raw string) Option { return func(o *options) { o.yaml = raw } }

// WithConfigPath loads config from a real YAML file on disk, so write paths
// (PutSettings, SetModels) work. The test owns the file.
func WithConfigPath(path string) Option { return func(o *options) { o.configPath = path } }

// WithDataDir pins the config data_dir. Tests that need to inspect the
// workspace files use it.
func WithDataDir(dir string) Option { return func(o *options) { o.dataDir = dir } }

// WithCredentials overrides the bootstrap admin credentials.
func WithCredentials(email, password string) Option {
	return func(o *options) { o.email, o.password = email, password }
}

// WithBeforeApp seeds rows after migration but before the App starts, which is
// how startup recovery (orphaned runs, connector resume) is exercised.
func WithBeforeApp(fn func(*gorm.DB)) Option { return func(o *options) { o.beforeApp = fn } }

// WithStdioBridge makes the fake host run the real `silo-mcp-bridge` process
// for each STDIO sidecar, with the same env the container would get. The
// config must set cp_url to the harness listener for the bridge to dial back.
func WithStdioBridge() Option { return func(o *options) { o.stdioBridge = true } }

// New builds a harness and signs in as its bootstrap admin. Everything is
// torn down via t.Cleanup.
func New(t *testing.T, opts ...Option) *H {
	t.Helper()
	o := options{email: "admin@test.local", password: "test-pass"}
	for _, fn := range opts {
		fn(&o)
	}
	if o.dataDir == "" {
		o.dataDir = t.TempDir()
	}
	var store *config.Store
	var err error
	if o.configPath != "" {
		store, err = config.LoadPath(o.configPath)
	} else {
		if o.yaml == "" {
			o.yaml = DefaultYAML(o.dataDir)
		}
		store, err = config.FromYAML([]byte(o.yaml))
	}
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	gdb := memDB(t)
	if o.beforeApp != nil {
		o.beforeApp(gdb)
	}

	fake, _ := o.host.(*FakeHost)
	host := o.host
	if o.hostFactory != nil {
		built, ferr := o.hostFactory(store)
		if ferr != nil {
			t.Fatalf("host factory: %v", ferr)
		}
		host = built
	}
	if host == nil {
		fake = NewFakeHost()
		host = fake
	}

	ln := o.listener
	if ln == nil {
		var lerr error
		ln, lerr = net.Listen("tcp", "127.0.0.1:0")
		if lerr != nil {
			t.Fatalf("listen: %v", lerr)
		}
	}
	if o.stdioBridge && fake != nil {
		fake.StdioLaunch = func(spec dockerx.StdioSpec) func() {
			return StartBridge(t, spec)
		}
	}

	a := app.New(store, gdb, host)
	t.Cleanup(a.Shutdown)

	if err := auth.EnsureBootstrap(gdb, o.email, o.password); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := &http.Server{Handler: h2c.NewHandler(a.Handler(), &http2.Server{})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}
	h := &H{
		T: t, App: a, DB: gdb, Store: store, Host: host, Fake: fake,
		URL: "http://" + clientAddr(ln), HTTP: hc,
		DataDir: store.Config().DataDir, Email: o.email, Password: o.password,
		srv: srv, ln: ln,
	}
	h.Client = silov1connect.NewUIClient(hc, h.URL)
	if _, err := h.Client.SignIn(context.Background(), connect.NewRequest(&v1.SignInRequest{
		Email: o.email, Password: o.password,
	})); err != nil {
		t.Fatalf("sign in: %v", err)
	}
	return h
}

func (o options) DataDirFor(store *config.Store) string { return store.Config().DataDir }

// DefaultYAML is the deterministic test config: the dummy provider, a temp
// data dir, and no network.
func DefaultYAML(dataDir string) string {
	return fmt.Sprintf(`http_addr: ":0"
data_dir: %q
cp_url: http://127.0.0.1:0
model: dummy/echo
model_title: dummy/echo
models:
  - dummy/echo
providers:
  dummy:
    api_key: test
search:
  engine: duckduckgo_scraper
`, dataDir)
}

// Ctx returns a background context for client calls.
func (h *H) Ctx() context.Context { return context.Background() }

// NewClient returns a UI client with no session cookie, for auth-boundary
// tests (unauthenticated calls, bad credentials).
func (h *H) NewClient() silov1connect.UIClient {
	jar, _ := cookiejar.New(nil)
	return silov1connect.NewUIClient(&http.Client{Jar: jar}, h.URL)
}

// WaitApproval blocks until the bot has a pending approval and returns it.
func (h *H) WaitApproval(botID string) *v1.Approval {
	h.T.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		res, err := h.Client.ListApprovals(h.Ctx(), connect.NewRequest(&v1.ListApprovalsRequest{BotId: botID}))
		if err == nil && len(res.Msg.GetApprovals()) > 0 {
			return res.Msg.GetApprovals()[0]
		}
		if time.Now().After(deadline) {
			h.T.Fatalf("no approval for bot %s", botID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitBotStatus polls GetBot until the derived status matches want.
func (h *H) WaitBotStatus(botID, want string) *v1.Bot {
	h.T.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last *v1.Bot
	for {
		res, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: botID}))
		if err == nil {
			last = res.Msg
			if last.GetStatus() == want {
				return last
			}
		}
		if time.Now().After(deadline) {
			h.T.Fatalf("bot %s status %q, want %q", botID, last.GetStatus(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// CreateBot creates a bot through the public API and returns it.
func (h *H) CreateBot(name string) *v1.Bot {
	h.T.Helper()
	res, err := h.Client.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: name}))
	if err != nil {
		h.T.Fatalf("CreateBot: %v", err)
	}
	return res.Msg
}

// FirstChat returns the id of the bot's first chat.
func (h *H) FirstChat(botID string) string {
	h.T.Helper()
	res, err := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: botID}))
	if err != nil {
		h.T.Fatalf("ListChats: %v", err)
	}
	if len(res.Msg.GetChats()) == 0 {
		h.T.Fatalf("bot %s has no chats", botID)
	}
	return res.Msg.GetChats()[0].GetId()
}

// Send sends a message and returns (runID, chatID).
func (h *H) Send(botID, chatID, text string) (string, string) {
	h.T.Helper()
	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{
		BotId: botID, ChatId: chatID, Text: text,
	}))
	if err != nil {
		h.T.Fatalf("Send: %v", err)
	}
	return res.Msg.GetRunId(), res.Msg.GetChatId()
}

// EditMessage edits the last user message and returns the replacement run id.
func (h *H) EditMessage(botID, chatID, eventID, text string) string {
	h.T.Helper()
	res, err := h.Client.EditMessage(h.Ctx(), connect.NewRequest(&v1.EditMessageRequest{
		BotId: botID, ChatId: chatID, EventId: eventID, Text: text,
	}))
	if err != nil {
		h.T.Fatalf("EditMessage: %v", err)
	}
	return res.Msg.GetRunId()
}

// DeleteMessage removes the last user message and everything after it.
func (h *H) DeleteMessage(botID, chatID, eventID string) {
	h.T.Helper()
	if _, err := h.Client.DeleteMessage(h.Ctx(), connect.NewRequest(&v1.DeleteMessageRequest{
		BotId: botID, ChatId: chatID, EventId: eventID,
	})); err != nil {
		h.T.Fatalf("DeleteMessage: %v", err)
	}
}

// DivergeChat copies the context before eventID into a new chat.
func (h *H) DivergeChat(botID, chatID, eventID string) *v1.Chat {
	h.T.Helper()
	res, err := h.Client.DivergeChat(h.Ctx(), connect.NewRequest(&v1.DivergeChatRequest{
		BotId: botID, ChatId: chatID, EventId: eventID,
	}))
	if err != nil {
		h.T.Fatalf("DivergeChat: %v", err)
	}
	return res.Msg.GetChat()
}

// ChatEvents returns every persisted event of a chat in conversational order.
func (h *H) ChatEvents(chatID string) []db.RunEvent {
	h.T.Helper()
	var runs []db.Run
	h.DB.Where("chat_id = ?", chatID).Order("created_at").Order("id").Find(&runs)
	var out []db.RunEvent
	for _, r := range runs {
		var evs []db.RunEvent
		h.DB.Where("run_id = ?", r.ID).Order("created_at").Order("id").Find(&evs)
		out = append(out, evs...)
	}
	return out
}

// LastUserEvent returns the most recent user message in a chat.
func (h *H) LastUserEvent(chatID string) db.RunEvent {
	h.T.Helper()
	ev, ok := h.FindLastUserEvent(chatID)
	if !ok {
		h.T.Fatalf("chat %s has no user message", chatID)
	}
	return ev
}

// FindLastUserEvent returns the most recent user message and whether one
// exists, so a caller can poll without failing the test on the first read.
func (h *H) FindLastUserEvent(chatID string) (db.RunEvent, bool) {
	events := h.ChatEvents(chatID)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == "user" {
			return events[i], true
		}
	}
	return db.RunEvent{}, false
}

// WaitRun blocks until the run has a terminal event, then returns its events in
// order. It fails the test on timeout.
func (h *H) WaitRun(runID string) []db.RunEvent {
	h.T.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var done int64
		h.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", runID, "done").Count(&done)
		if done > 0 {
			break
		}
		if time.Now().After(deadline) {
			h.T.Fatalf("run %s did not finish\n%s", runID, h.dumpEvents(runID))
		}
		time.Sleep(10 * time.Millisecond)
	}
	var events []db.RunEvent
	h.DB.Where("run_id = ?", runID).Order("created_at").Find(&events)
	return events
}

// Events returns persisted events for a run.
func (h *H) Events(runID string) []db.RunEvent {
	var events []db.RunEvent
	h.DB.Where("run_id = ?", runID).Order("created_at").Find(&events)
	return events
}

// RunBody concatenates the user-visible body of a run (assistant, section, and
// streamed chunk events). Thinking and tool output are excluded.
func (h *H) RunBody(runID string) string {
	var b strings.Builder
	for _, ev := range h.Events(runID) {
		switch ev.Kind {
		case "chunk", "assistant", "section", "section_live":
			b.WriteString(ev.Body)
		}
	}
	return b.String()
}

func (h *H) dumpEvents(runID string) string {
	var b strings.Builder
	for _, ev := range h.Events(runID) {
		fmt.Fprintf(&b, "  %-12s %s\n", ev.Kind, ev.Body)
	}
	return b.String()
}

// clientAddr rewrites a wildcard listen address into one a local client can
// dial.
func clientAddr(ln net.Listener) string {
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return ln.Addr().String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// memDB opens a fresh shared in-memory SQLite and migrates the schema.
func memDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("file:apptest-%d?mode=memory&cache=shared", dbSeq.Add(1))
	gdb, err := gorm.Open(sqlite.Open(name), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&db.User{}, &db.Session{}, &db.Bot{}, &db.Secret{}, &db.Rule{},
		&db.Chat{}, &db.Run{}, &db.RunEvent{}, &db.Approval{}, &db.Audit{}, &db.LLMLog{},
		&db.Connector{}, &db.BotConnector{}, &db.BotSkill{}, &db.Channel{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}
