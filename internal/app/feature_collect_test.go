package app_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func collect(t *testing.T, h *apptest.H, botID, chatID string) *v1.CollectMemoriesResponse {
	t.Helper()
	res, err := h.Client.CollectMemories(h.Ctx(), connect.NewRequest(&v1.CollectMemoriesRequest{BotId: botID, ChatId: chatID}))
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return res.Msg
}

// memorySeq reads the watermark with a raw query: a cached SELECT * plan
// would go stale when a test drops and re-adds the column.
func memorySeq(h *apptest.H, chatID string) int64 {
	var seq int64
	if err := h.DB.Raw("SELECT memory_seq FROM chats WHERE id = ?", chatID).Scan(&seq).Error; err != nil {
		h.T.Fatalf("memory_seq: %v", err)
	}
	return seq
}

// ageChat makes every event of a chat look older than the sweep's idle wait.
func ageChat(h *apptest.H, chatID string) {
	h.DB.Exec(`UPDATE run_events SET created_at = created_at - interval '1 hour'
		WHERE run_id IN (SELECT id FROM runs WHERE chat_id = ?)`, chatID)
}

func TestCollectMemoriesSavesFactsAndLessonsOnce(t *testing.T) {
	dummy.Reset()
	dummy.Collect("Test_C1", `Here you go:
`+"```json"+`
{"save":[{"kind":"fact","text":"The human's dog is called Rex"},{"kind":"lesson","text":"On example.com, log in before searching; the search form fails signed out"}]}
`+"```")
	h := apptest.New(t)
	bot := h.CreateBot("Collector")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "Test_C1_Input my dog is Rex")
	h.WaitRun(run)

	res := collect(t, h, bot.GetId(), chat)
	if res.GetSaved() != 2 || len(res.GetMemories()) != 2 {
		t.Fatalf("saved %d memories %v note %q", res.GetSaved(), res.GetMemories(), res.GetNote())
	}
	var rows []db.Memory
	h.DB.Where("bot_id = ?", bot.GetId()).Order("content").Find(&rows)
	kinds := map[string]string{}
	for _, r := range rows {
		kinds[r.Content] = r.Kind
		if r.ChatID != chat {
			t.Fatalf("memory %q chat %q, want %q", r.Content, r.ChatID, chat)
		}
	}
	if kinds["The human's dog is called Rex"] != "fact" || !strings.HasPrefix(rows[0].Content, "On example.com") || rows[0].Kind != "lesson" {
		t.Fatalf("kinds %v", kinds)
	}
	if memorySeq(h, chat) == 0 {
		t.Fatal("watermark did not move")
	}

	calls := len(dummy.CollectCalls())
	again := collect(t, h, bot.GetId(), chat)
	if again.GetNote() != "nothing new" || len(dummy.CollectCalls()) != calls {
		t.Fatalf("second collection should not call the model: note %q calls %d→%d", again.GetNote(), calls, len(dummy.CollectCalls()))
	}

	// Only what was said after the watermark is read, and saved memories are
	// shown back so the model can skip them.
	run2, _ := h.Send(bot.GetId(), chat, "Test_C2_Input new topic")
	h.WaitRun(run2)
	collect(t, h, bot.GetId(), chat)
	all := dummy.CollectCalls()
	last := all[len(all)-1]
	if strings.Contains(last, "Test_C1_Input") || !strings.Contains(last, "Test_C2_Input") {
		t.Fatalf("excerpt should hold only the new turn:\n%s", last)
	}
	if !strings.Contains(last, "Rex") {
		t.Fatalf("already saved memories should be shown:\n%s", last)
	}
}

func TestCollectMemoriesUpdateForgetOnlyShownIDs(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Editor")
	chat := h.FirstChat(bot.GetId())
	other := h.CreateBot("Other")

	dummy.Collect("Test_C3", `{"save":[{"kind":"fact","text":"The human lives in Oslo"},{"kind":"fact","text":"The human drinks tea"}]}`)
	run, _ := h.Send(bot.GetId(), chat, "Test_C3_Input")
	h.WaitRun(run)
	collect(t, h, bot.GetId(), chat)
	var oslo, tea db.Memory
	h.DB.Where("bot_id = ? AND content LIKE ?", bot.GetId(), "%Oslo%").First(&oslo)
	h.DB.Where("bot_id = ? AND content LIKE ?", bot.GetId(), "%tea%").First(&tea)

	// A memory of another Bot is never shown, so its id is refused.
	otherChat := h.FirstChat(other.GetId())
	dummy.Collect("Test_C0", `{"save":[{"text":"The stranger likes jazz"}]}`)
	run0, _ := h.Send(other.GetId(), otherChat, "Test_C0_Input")
	h.WaitRun(run0)
	collect(t, h, other.GetId(), otherChat)
	var jazz db.Memory
	h.DB.Where("bot_id = ?", other.GetId()).First(&jazz)

	dummy.Collect("Test_C4", `{"update":[{"id":"`+oslo.ID+`","text":"The human moved from Oslo to Bergen in 2026"},{"id":"`+jazz.ID+`","text":"hijacked"}],"forget":["`+tea.ID+`","`+jazz.ID+`"]}`)
	run2, _ := h.Send(bot.GetId(), chat, "Test_C4_Input Oslo tea")
	h.WaitRun(run2)
	res := collect(t, h, bot.GetId(), chat)
	if res.GetUpdated() != 1 || res.GetForgotten() != 1 {
		t.Fatalf("updated %d forgotten %d", res.GetUpdated(), res.GetForgotten())
	}
	h.DB.Where("id = ?", oslo.ID).First(&oslo)
	if !strings.Contains(oslo.Content, "Bergen") {
		t.Fatalf("oslo not rewritten: %q", oslo.Content)
	}
	var n int64
	h.DB.Model(&db.Memory{}).Where("id = ?", tea.ID).Count(&n)
	if n != 0 {
		t.Fatal("tea should be forgotten")
	}
	h.DB.Where("id = ?", jazz.ID).First(&jazz)
	if jazz.Content != "The stranger likes jazz" {
		t.Fatalf("another bot's memory changed: %q", jazz.Content)
	}
}

func TestCollectMemoriesBadReplyAdvancesProviderErrorDoesNot(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Flaky")
	chat := h.FirstChat(bot.GetId())

	dummy.Collect("Test_C5", "I think the human likes cats.")
	run, _ := h.Send(bot.GetId(), chat, "Test_C5_Input")
	h.WaitRun(run)
	res := collect(t, h, bot.GetId(), chat)
	if res.GetSaved() != 0 || res.GetNote() == "" || memorySeq(h, chat) == 0 {
		t.Fatalf("garbage reply: saved %d note %q seq %d", res.GetSaved(), res.GetNote(), memorySeq(h, chat))
	}

	before := memorySeq(h, chat)
	dummy.Collect("Test_C6", dummy.CollectError)
	run2, _ := h.Send(bot.GetId(), chat, "Test_C6_Input")
	h.WaitRun(run2)
	_, err := h.Client.CollectMemories(h.Ctx(), connect.NewRequest(&v1.CollectMemoriesRequest{BotId: bot.GetId(), ChatId: chat}))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		for _, c := range dummy.CollectCalls() {
			t.Logf("CALL:\n%s", c)
		}
		t.Fatalf("provider error: %v", err)
	}
	if memorySeq(h, chat) != before {
		t.Fatal("a provider error must leave the watermark")
	}

	// The sweep pauses after a provider error instead of retrying every tick.
	ageChat(h, chat)
	if n := h.App.SweepMemories(time.Now()); n != 1 {
		t.Fatalf("sweep read %d, want 1", n)
	}
	dummy.Collect("Test_C6", `{}`)
	if n := h.App.SweepMemories(time.Now()); n != 0 {
		t.Fatalf("sweep should pause after a provider error, read %d", n)
	}
	if n := h.App.SweepMemories(time.Now().Add(20 * time.Minute)); n != 1 {
		t.Fatalf("sweep after the pause read %d, want 1", n)
	}
}

func TestSweepMemoriesPicksIdleWebChatsOnly(t *testing.T) {
	dummy.Reset()
	dummy.Collect("Test_C7", `{"save":[{"text":"The human's favourite colour is green"}]}`)
	h := apptest.New(t)
	bot := h.CreateBot("Sweeper")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "Test_C7_Input")
	h.WaitRun(run)

	if n := h.App.SweepMemories(time.Now()); n != 0 {
		t.Fatalf("a chat quiet for under 10 minutes was read (%d)", n)
	}

	// Automation and subagent logs are never swept.
	hidden := db.Chat{ID: "hidden-auto", BotID: bot.GetId(), AutomationID: "a1", Title: "log"}
	h.DB.Create(&hidden)
	h.DB.Create(&db.Run{ID: "hidden-run", BotID: bot.GetId(), ChatID: hidden.ID, Status: "done", CreatedAt: time.Now()})
	h.DB.Create(&db.RunEvent{ID: "hidden-ev", RunID: "hidden-run", Kind: "user", Body: "Test_C7 secret automation", CreatedAt: time.Now().Add(-time.Hour)})

	ageChat(h, chat)
	if n := h.App.SweepMemories(time.Now()); n != 1 {
		t.Fatalf("sweep read %d chats, want 1", n)
	}
	var n int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ?", bot.GetId()).Count(&n)
	if n != 1 {
		t.Fatalf("memories %d, want 1", n)
	}
	for _, c := range dummy.CollectCalls() {
		if strings.Contains(c, "secret automation") {
			t.Fatal("an automation log was swept")
		}
	}
	if n := h.App.SweepMemories(time.Now()); n != 0 {
		t.Fatalf("a swept chat was read again (%d)", n)
	}
}

func TestSweepMemoriesOff(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+"memory:\n  collect: false\n"))
	bot := h.CreateBot("Quiet")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "Test_C8_Input")
	h.WaitRun(run)
	ageChat(h, chat)
	if n := h.App.SweepMemories(time.Now()); n != 0 {
		t.Fatalf("sweep ran with memory.collect off (%d)", n)
	}
	// The button still works.
	collect(t, h, bot.GetId(), chat)
	if len(dummy.CollectCalls()) != 1 {
		t.Fatalf("manual collect calls %d", len(dummy.CollectCalls()))
	}
}

func TestCollectMemoriesRefusesLogs(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Logs")
	log := db.Chat{ID: "sub-log", BotID: bot.GetId(), SubagentID: "s1", Title: "agent"}
	h.DB.Create(&log)
	_, err := h.Client.CollectMemories(h.Ctx(), connect.NewRequest(&v1.CollectMemoriesRequest{BotId: bot.GetId(), ChatId: log.ID}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("subagent log: %v", err)
	}
}

func TestMigrateStartsOldChatsAtTheirNewestEvent(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Old")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "Test_C9_Input")
	evs := h.WaitRun(run)
	if err := h.DB.Migrator().DropColumn(&db.Chat{}, "MemorySeq"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(h.DB); err != nil {
		t.Fatal(err)
	}
	var max int64
	for _, ev := range evs {
		if ev.Seq > max {
			max = ev.Seq
		}
	}
	if got := memorySeq(h, chat); got != max {
		t.Fatalf("memory_seq %d, want %d", got, max)
	}
}

func TestPutSettingsModelMemory(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/silo.yaml"
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	put := func(v string) error {
		_, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{"model_memory": v}}))
		return err
	}
	if err := put("dummy/other"); err == nil {
		t.Fatal("a model outside the allowlist was accepted")
	}
	if err := put("dummy/echo"); err != nil {
		t.Fatal(err)
	}
	if got := h.Store.Config().MemoryModel(); got != "dummy/echo" {
		t.Fatalf("MemoryModel %q", got)
	}
}
