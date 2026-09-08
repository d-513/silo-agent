package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
)

func memDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(
		&db.User{}, &db.Session{}, &db.Bot{}, &db.Secret{}, &db.Rule{},
		&db.Chat{}, &db.Run{}, &db.RunEvent{}, &db.Approval{}, &db.Audit{},
		&db.Connector{}, &db.BotConnector{}, &db.BotSkill{},
	); err != nil {
		t.Fatal(err)
	}
	return gdb
}

type fakeHost struct {
	mu         sync.Mutex
	inspect    map[string]dockerx.State
	inspectErr error
	createErr  error
	envHash    map[string]string
	creates    atomic.Int32
	starts     atomic.Int32
	drops      atomic.Int32
}

func (f *fakeHost) Inspect(_ context.Context, id string) (dockerx.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inspectErr != nil {
		return dockerx.State{}, f.inspectErr
	}
	if st, ok := f.inspect[id]; ok {
		return st, nil
	}
	return dockerx.State{}, dockerx.ErrNotFound
}

func (f *fakeHost) Create(_ context.Context, botID, _ string) (string, error) {
	f.creates.Add(1)
	if f.createErr != nil {
		return "", f.createErr
	}
	id := "cid-" + botID
	f.mu.Lock()
	if f.inspect == nil {
		f.inspect = map[string]dockerx.State{}
	}
	st := dockerx.State{ID: id, Running: true}
	f.inspect[id] = st
	f.inspect[dockerx.Name(botID)] = st
	f.mu.Unlock()
	return id, nil
}

func (f *fakeHost) EnvTokenHash(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.envHash == nil {
		return "", nil
	}
	return f.envHash[id], nil
}

func (f *fakeHost) Stats(_ context.Context, id string) (dockerx.Stats, error) {
	st, err := f.Inspect(context.Background(), id)
	if err != nil {
		return dockerx.Stats{}, err
	}
	if !st.Running {
		return dockerx.Stats{}, nil
	}
	return dockerx.Stats{CPUPercent: 1.5, MemUsed: 100 << 20, MemLimit: 1 << 30}, nil
}
func (f *fakeHost) Start(_ context.Context, id string) error {
	f.starts.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.inspect[id]
	if !ok {
		return dockerx.ErrNotFound
	}
	st.Running = true
	for k, v := range f.inspect {
		if v.ID == st.ID {
			v.Running = true
			f.inspect[k] = v
		}
	}
	return nil
}
func (f *fakeHost) Stop(context.Context, string) error  { return nil }
func (f *fakeHost) Drop(_ context.Context, _, _ string) { f.drops.Add(1) }

func testApp(t *testing.T, d dockerx.Host) *App {
	t.Helper()
	if d == nil {
		d = &fakeHost{}
	}
	return New(nil, memDB(t), d)
}

func testStore(t *testing.T) *config.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	st, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		conn, run bool
		db, want  string
	}{
		{true, true, "working", "working"},
		{true, true, "needs_you", "needs_you"},
		{true, true, "starting", "online"},
		{true, false, "stopped", "online"},
		{false, true, "idle", "starting"},
		{true, true, "idle", "online"},
		{false, false, "starting", "stopped"},
		{false, false, "working", "stopped"},
	}
	for _, c := range cases {
		if got := DeriveStatus(c.conn, c.run, c.db); got != c.want {
			t.Fatalf("conn=%v run=%v db=%s got %s want %s", c.conn, c.run, c.db, got, c.want)
		}
	}
}

func TestLiveInspectErrorPreservesContainer(t *testing.T) {
	h := &fakeHost{inspectErr: errors.New("podman busy"), inspect: map[string]dockerx.State{}}
	a := testApp(t, h)
	b := &db.Bot{ID: "bot1", ContainerID: "abc", Status: "online"}
	a.DB.Create(b)
	if !a.live(context.Background(), b) {
		t.Fatal("expected live on inspect error")
	}
	if h.drops.Load() != 0 {
		t.Fatal("dropped on transient error")
	}
	a.DB.First(b, "id = ?", "bot1")
	if b.ContainerID != "abc" {
		t.Fatalf("cleared id %q", b.ContainerID)
	}
}

func TestLiveNotFoundClears(t *testing.T) {
	a := testApp(t, &fakeHost{})
	b := &db.Bot{ID: "bot1", ContainerID: "gone", Status: "online"}
	a.DB.Create(b)
	if a.live(context.Background(), b) {
		t.Fatal("expected gone")
	}
	a.DB.First(b, "id = ?", "bot1")
	if b.ContainerID != "" {
		t.Fatalf("id %q", b.ContainerID)
	}
}

func TestLiveStoppedKeepsContainer(t *testing.T) {
	id := "cid-bot1"
	name := dockerx.Name("bot1")
	h := &fakeHost{inspect: map[string]dockerx.State{
		id: {ID: id, Running: false}, name: {ID: id, Running: false},
	}}
	a := testApp(t, h)
	b := &db.Bot{ID: "bot1", ContainerID: id, Status: "online"}
	a.DB.Create(b)
	if a.live(context.Background(), b) {
		t.Fatal("expected not live")
	}
	if h.drops.Load() != 0 {
		t.Fatal("dropped a stopped box")
	}
	a.DB.First(b, "id = ?", "bot1")
	if b.ContainerID != id {
		t.Fatalf("cleared id %q", b.ContainerID)
	}
	if b.Status != "stopped" {
		t.Fatalf("status %s", b.Status)
	}
}

func TestEnsureRunningStartsExisting(t *testing.T) {
	id := "cid-bot1"
	name := dockerx.Name("bot1")
	h := &fakeHost{inspect: map[string]dockerx.State{
		id: {ID: id, Running: false}, name: {ID: id, Running: false},
	}}
	a := testApp(t, h)
	b := &db.Bot{ID: "bot1", ContainerID: id, TokenHash: "h", Status: "stopped"}
	a.DB.Create(b)
	if err := a.ensureRunning(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if h.creates.Load() != 0 {
		t.Fatalf("creates %d", h.creates.Load())
	}
	if h.starts.Load() != 1 {
		t.Fatalf("starts %d", h.starts.Load())
	}
	if h.drops.Load() != 0 {
		t.Fatalf("drops %d", h.drops.Load())
	}
	a.DB.First(b, "id = ?", "bot1")
	if b.ContainerID != id {
		t.Fatalf("id %q", b.ContainerID)
	}
}

func TestEnsureRunningSerial(t *testing.T) {
	h := &fakeHost{}
	a := testApp(t, h)
	b := &db.Bot{ID: "bot1", Status: "stopped"}
	a.DB.Create(b)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var row db.Bot
			a.DB.First(&row, "id = ?", "bot1")
			if err := a.ensureRunning(context.Background(), &row); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if h.creates.Load() != 1 {
		t.Fatalf("creates %d", h.creates.Load())
	}
}

func TestEnsureCreateFailKeepsHash(t *testing.T) {
	h := &fakeHost{createErr: errors.New("name in use")}
	a := testApp(t, h)
	b := &db.Bot{ID: "bot1", TokenHash: "old", Status: "stopped"}
	a.DB.Create(b)
	if err := a.ensureRunning(context.Background(), b); err == nil {
		t.Fatal("expected create error")
	}
	a.DB.First(b, "id = ?", "bot1")
	if b.TokenHash != "old" {
		t.Fatalf("rotated hash on failed create: %q", b.TokenHash)
	}
}

func TestLiveStaleTokenDrops(t *testing.T) {
	name := dockerx.Name("bot1")
	h := &fakeHost{
		inspect: map[string]dockerx.State{name: {ID: "cid-old", Running: true}, "cid-old": {ID: "cid-old", Running: true}},
		envHash: map[string]string{"cid-old": "box-hash"},
	}
	a := testApp(t, h)
	b := &db.Bot{ID: "bot1", TokenHash: "db-hash", ContainerID: "cid-old", Status: "stopped"}
	a.DB.Create(b)
	if a.live(context.Background(), b) {
		t.Fatal("expected stale box dropped")
	}
	if h.drops.Load() != 1 {
		t.Fatalf("drops %d", h.drops.Load())
	}
	a.DB.First(b, "id = ?", "bot1")
	if b.ContainerID != "" {
		t.Fatalf("kept id %q", b.ContainerID)
	}
}

func TestRecoverOrphans(t *testing.T) {
	gdb := memDB(t)
	gdb.Create(&db.Run{ID: "r1", BotID: "b", Status: "running"})
	gdb.Create(&db.Approval{ID: "a1", BotID: "b", Status: "pending"})
	New(nil, gdb, &fakeHost{})
	var r db.Run
	gdb.First(&r, "id = ?", "r1")
	if r.Status != "interrupted" {
		t.Fatalf("run %s", r.Status)
	}
	var ap db.Approval
	gdb.First(&ap, "id = ?", "a1")
	if ap.Status != "interrupted" {
		t.Fatalf("approval %s", ap.Status)
	}
}

func TestRunOfCmd(t *testing.T) {
	a := testApp(t, nil)
	a.mu.Lock()
	a.cmdRun["c1"] = "r1"
	a.mu.Unlock()
	if a.runOfCmd("c1") != "r1" || a.runOfCmd("x") != "" {
		t.Fatal("cmd map")
	}
}

func TestEventsAfter(t *testing.T) {
	rows := []db.RunEvent{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got := eventsAfter(rows, "a")
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("%+v", got)
	}
	if len(eventsAfter(rows, "")) != 3 {
		t.Fatal("empty after")
	}
}

func TestBusOverflowCloses(t *testing.T) {
	b := newBus()
	ch, unsub := b.Subscribe("bot")
	defer unsub()
	for i := 0; i < 300; i++ {
		b.Publish("bot", &v1.RunEvent{Id: "e"})
	}
	drained := 0
	for range ch {
		drained++
	}
	if drained == 0 {
		t.Fatal("expected some events then close")
	}
}

func TestRecomputeStatusParallelRuns(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", Status: "idle"})
	a.DB.Create(&db.Run{ID: "r1", BotID: "b1", Status: "running"})
	a.DB.Create(&db.Run{ID: "r2", BotID: "b1", Status: "running"})
	a.finish("b1", "c", "r1", "done")
	var bot db.Bot
	a.DB.First(&bot, "id = ?", "b1")
	if bot.Status != "working" {
		t.Fatalf("status %s", bot.Status)
	}
	a.finish("b1", "c", "r2", "done")
	a.DB.First(&bot, "id = ?", "b1")
	if bot.Status != "idle" {
		t.Fatalf("status %s", bot.Status)
	}
}

func TestStopChat(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", Status: "working"})
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1"})
	a.DB.Create(&db.Chat{ID: "c2", BotID: "b1"})
	a.DB.Create(&db.Run{ID: "r1", BotID: "b1", ChatID: "c1", Status: "running"})
	a.DB.Create(&db.Run{ID: "r2", BotID: "b1", ChatID: "c2", Status: "running"})
	a.DB.Create(&db.Approval{ID: "ap1", BotID: "b1", RunID: "r1", Status: "pending"})
	hit := make(chan struct{}, 1)
	other := make(chan struct{}, 1)
	a.trackRun("b1", "c1", "r1", func() { hit <- struct{}{} })
	a.trackRun("b1", "c2", "r2", func() { other <- struct{}{} })
	ch := make(chan string, 1)
	a.mu.Lock()
	a.approvals["ap1"] = &waiter{ch: ch, botID: "b1", runID: "r1"}
	a.mu.Unlock()
	a.stopChat("b1", "c1")
	select {
	case <-hit:
	default:
		t.Fatal("run not canceled")
	}
	select {
	case <-other:
		t.Fatal("other chat canceled")
	default:
	}
	select {
	case v := <-ch:
		if v != "canceled" {
			t.Fatal(v)
		}
	default:
		t.Fatal("approval waiter")
	}
	var r1, r2 db.Run
	a.DB.First(&r1, "id = ?", "r1")
	a.DB.First(&r2, "id = ?", "r2")
	if r1.Status != "stopped" || r2.Status != "running" {
		t.Fatalf("runs %s %s", r1.Status, r2.Status)
	}
}

func TestApprovalWaiterRemoved(t *testing.T) {
	a := testApp(t, nil)
	ch := make(chan string, 1)
	a.mu.Lock()
	a.approvals["ap"] = &waiter{ch: ch, botID: "b1"}
	a.mu.Unlock()
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.User{ID: "u", Email: "a@b.c"})
	a.dropWaiter("ap")
	a.mu.Lock()
	_, ok := a.approvals["ap"]
	a.mu.Unlock()
	if ok {
		t.Fatal("waiter remains")
	}
}

func TestEmitPersists(t *testing.T) {
	a := testApp(t, nil)
	a.emit("b", "c", "r", "chunk", "hi", "")
	var n int64
	a.DB.Model(&db.RunEvent{}).Count(&n)
	if n != 1 {
		t.Fatalf("events %d", n)
	}
}

func TestResetContainerDropsBox(t *testing.T) {
	h := &fakeHost{inspect: map[string]dockerx.State{
		"cid": {ID: "cid", Running: true},
	}}
	a := testApp(t, h)
	a.DB.Create(&db.User{ID: "u", Email: "a@b.c"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u", ContainerID: "cid", Status: "online"})
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u"})
	res, err := a.ResetContainer(ctx, connect.NewRequest(&v1.GetBotRequest{Id: "b1"}))
	if err != nil {
		t.Fatal(err)
	}
	if h.drops.Load() != 1 {
		t.Fatalf("drops %d", h.drops.Load())
	}
	if res.Msg.Status != "stopped" {
		t.Fatalf("status %s", res.Msg.Status)
	}
	var b db.Bot
	a.DB.First(&b, "id = ?", "b1")
	if b.ContainerID != "" || b.Status != "stopped" {
		t.Fatalf("id %q status %s", b.ContainerID, b.Status)
	}
}

func TestDeleteBotRemovesRow(t *testing.T) {
	h := &fakeHost{}
	a := testApp(t, h)
	a.DB.Create(&db.User{ID: "u", Email: "a@b.c"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u", ContainerID: "cid"})
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1"})
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u"})
	if _, err := a.DeleteBot(ctx, connect.NewRequest(&v1.GetBotRequest{Id: "b1"})); err != nil {
		t.Fatal(err)
	}
	if h.drops.Load() != 1 {
		t.Fatalf("drops %d", h.drops.Load())
	}
	var n int64
	a.DB.Model(&db.Bot{}).Count(&n)
	if n != 0 {
		t.Fatalf("bots %d", n)
	}
	a.DB.Model(&db.Chat{}).Count(&n)
	if n != 0 {
		t.Fatalf("chats %d", n)
	}
}
