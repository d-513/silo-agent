package app_test

import (
	"fmt"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// modelYAML allows two dummy models so a Bot default and a chat override can be
// told apart. debug records every model call in LLMLog.
func modelYAML(dataDir string) string {
	return fmt.Sprintf(`http_addr: ":0"
data_dir: %q
cp_url: http://127.0.0.1:0
debug: true
model: dummy/echo
model_title: dummy/echo
models:
  - dummy/echo
  - dummy/extra
providers:
  dummy:
    api_key: test
search:
  engine: duckduckgo_scraper
`, dataDir)
}

func TestBotModelSetting(t *testing.T) {
	h := apptest.New(t, apptest.WithYAML(modelYAML(t.TempDir())))
	bot := h.CreateBot("Modeled")
	id := bot.GetId()

	upd, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{
		Id: id, Model: "dummy/extra",
	}))
	if err != nil {
		t.Fatalf("UpdateBot: %v", err)
	}
	if upd.Msg.GetModel() != "dummy/extra" {
		t.Fatalf("bot model %q", upd.Msg.GetModel())
	}

	lm, err := h.Client.ListModels(h.Ctx(), connect.NewRequest(&v1.ListModelsRequest{BotId: id}))
	if err != nil {
		t.Fatal(err)
	}
	if lm.Msg.GetDefaultModel() != "dummy/extra" {
		t.Fatalf("effective default %q, want dummy/extra", lm.Msg.GetDefaultModel())
	}

	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{
		Id: id, Model: "dummy/nope",
	})); err == nil {
		t.Fatal("model outside the allowlist should be rejected")
	}

	cleared, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: id}))
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Msg.GetModel() != "" {
		t.Fatalf("cleared model %q", cleared.Msg.GetModel())
	}
	lm, _ = h.Client.ListModels(h.Ctx(), connect.NewRequest(&v1.ListModelsRequest{BotId: id}))
	if lm.Msg.GetDefaultModel() != "dummy/echo" {
		t.Fatalf("fallback default %q, want operator dummy/echo", lm.Msg.GetDefaultModel())
	}
}

// TestModelResolutionOrder pins chat override -> Bot default -> operator
// default by inspecting the model each chat call actually used.
func TestModelResolutionOrder(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(modelYAML(t.TempDir())))
	bot := h.CreateBot("Resolver")
	id := bot.GetId()
	chat := h.FirstChat(id)

	chatModel := func() string {
		var rows []db.LLMLog
		h.DB.Where("bot_id = ? AND label = ?", id, "chat").Order("at").Find(&rows)
		if len(rows) == 0 {
			t.Fatal("no chat model call logged")
		}
		return rows[len(rows)-1].Model
	}

	// No bot or chat model: operator default.
	r1, _ := h.Send(id, chat, "Test_50_Input")
	h.WaitRun(r1)
	if got := chatModel(); got != "echo" {
		t.Fatalf("operator default used %q", got)
	}

	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{
		Id: id, Model: "dummy/extra",
	})); err != nil {
		t.Fatal(err)
	}
	r2, _ := h.Send(id, chat, "Test_50_Input")
	h.WaitRun(r2)
	if got := chatModel(); got != "extra" {
		t.Fatalf("bot default used %q, want extra", got)
	}

	if _, err := h.Client.SetChatModel(h.Ctx(), connect.NewRequest(&v1.SetChatModelRequest{
		BotId: id, ChatId: chat, Model: "dummy/echo",
	})); err != nil {
		t.Fatal(err)
	}
	r3, _ := h.Send(id, chat, "Test_50_Input")
	h.WaitRun(r3)
	if got := chatModel(); got != "echo" {
		t.Fatalf("chat override used %q, want echo", got)
	}
}

// TestSendRecreatesVanishedContainerWithStaleSession covers a worker session
// that outlives its container: a message must not trust the stale session, it
// must notice the missing box and recreate it.
func TestSendRecreatesVanishedContainerWithStaleSession(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Vanished")
	id := bot.GetId()
	before := h.Fake.Creates.Load()

	sess := h.App.Hub.Attach(id)
	t.Cleanup(func() { h.App.Hub.Detach(id, sess) })
	h.Fake.Forget(id)

	if h.App.Hub.Connected(id) != true {
		t.Fatal("stale session not registered")
	}
	if !h.Fake.Gone(id) {
		t.Fatal("container should be gone")
	}

	chat := h.FirstChat(id)
	runID, _ := h.Send(id, chat, "Test_50_Input")
	h.WaitRun(runID)

	if h.Fake.Creates.Load() != before+1 {
		t.Fatalf("vanished container not recreated: creates %d -> %d", before, h.Fake.Creates.Load())
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", id)
	if row.ContainerID == "" {
		t.Fatal("recreated container id not stored")
	}
}

// TestStartBotRecreatesVanishedContainerWithStaleSession is the explicit Start
// path for the same race.
func TestStartBotRecreatesVanishedContainerWithStaleSession(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("VanishedStart")
	id := bot.GetId()
	before := h.Fake.Creates.Load()

	sess := h.App.Hub.Attach(id)
	t.Cleanup(func() { h.App.Hub.Detach(id, sess) })
	h.Fake.Forget(id)

	res, err := h.Client.StartBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
	if err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	if res.Msg.GetStatus() != "starting" {
		t.Fatalf("status %q", res.Msg.GetStatus())
	}
	if h.Fake.Creates.Load() != before+1 {
		t.Fatalf("StartBot did not recreate: creates %d -> %d", before, h.Fake.Creates.Load())
	}
}
