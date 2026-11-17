package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// talk sends one message, waits for the reply and has the keeper read the chat.
func talk(t *testing.T, h *apptest.H, botID, chatID, text string) *v1.CollectMemoriesResponse {
	t.Helper()
	run, _ := h.Send(botID, chatID, text)
	h.WaitRun(run)
	return collect(t, h, botID, chatID)
}

func memoryLike(h *apptest.H, botID, like string) db.Memory {
	var m db.Memory
	h.DB.Where("bot_id = ? AND content LIKE ?", botID, "%"+like+"%").Order("created_at DESC").Limit(1).Find(&m)
	return m
}

// keeperRequests are the model requests the keeper's runs made.
func keeperRequests() []llm.Request {
	var out []llm.Request
	for _, r := range dummy.Streamed() {
		for _, blk := range r.System {
			if strings.Contains(blk.Text, dummy.CollectMarker) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// What nobody said is kept tentative: out of the prompt and out of recall
// until a separate conversation shows it again.
func TestInferredPreferenceWaitsForASecondSighting(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Noticer").GetId()
	chat := h.FirstChat(bot)

	dummy.Collect("Test_K1", dummy.Remember(`{"content":"The human wants answers under five lines","kind":"preference","inferred":true}`))
	res := talk(t, h, bot, chat, "Test_K1_Input shorter.")
	m := memoryLike(h, bot, "five lines")
	if res.GetSaved() != 1 || m.Status != memory.StatusTentative || !m.Inferred || m.Kind != memory.KindPreference {
		t.Fatalf("saved %d; memory %+v", res.GetSaved(), m)
	}
	if p := h.App.SystemPromptText(bot); strings.Contains(p, "five lines") {
		t.Fatal("a tentative memory is in the prompt")
	}
	if rows, err := h.App.Memory.Recall(h.Ctx(), bot, "answers under five lines", 5, false); err != nil || len(rows) != 0 {
		t.Fatalf("the Bot recalled a tentative memory: %v %v", rows, err)
	}

	// The same conversation going on is not a second sighting.
	again := fmt.Sprintf(`{"ids":[%q]}`, m.ID)
	dummy.Collect("Test_K2", dummy.Call("reinforce", again))
	talk(t, h, bot, chat, "Test_K2_Input still too long")
	if got := memoryLike(h, bot, "five lines"); got.Status != memory.StatusTentative || got.Evidence != 1 {
		t.Fatalf("promoted within the hour: %+v", got)
	}
	calls := dummy.CollectCalls()
	if last := calls[len(calls)-1]; !strings.Contains(last, m.ID) || !strings.Contains(last, "tentative, seen 1×") {
		t.Fatalf("the keeper should be shown the tentative memory as such:\n%s", last)
	}

	// A later one is.
	h.DB.Exec("UPDATE memories SET evidence_at = evidence_at - interval '2 hours' WHERE id = ?", m.ID)
	dummy.Collect("Test_K3", dummy.Call("reinforce", again))
	talk(t, h, bot, chat, "Test_K3_Input shorter please")
	if got := memoryLike(h, bot, "five lines"); got.Status != memory.StatusActive || got.Evidence != 2 {
		t.Fatalf("not promoted by a second sighting: %+v", got)
	}
	p := h.App.SystemPromptText(bot)
	if i, j := strings.Index(p, "Preferences:"), strings.Index(p, "five lines"); i < 0 || j < i {
		t.Fatalf("the preference is not told under Preferences:\n%s", p[max(0, len(p)-800):])
	}
}

// The human saying what was only worked out settles it at once.
func TestStatedMemorySettlesAGuess(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Settler").GetId()
	text := "The human wants the weekly report as a table"
	guess, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: text, Kind: memory.KindPreference, Inferred: true})
	if err != nil || guess.Status != memory.StatusTentative {
		t.Fatalf("guess %+v %v", guess, err)
	}
	said, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: text, Kind: memory.KindPreference})
	if err != nil || said.ID != guess.ID || !said.Updated || said.Status != memory.StatusActive {
		t.Fatalf("said %+v %v", said, err)
	}
	if m := memoryLike(h, bot, "weekly report"); m.Inferred {
		t.Fatal("a stated memory is still marked inferred")
	}
}

// With memory.infer off the keeper is told to keep only what was said, and a
// guess is refused whoever makes it.
func TestInferOffKeepsOnlyWhatWasSaid(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+"memory:\n  infer: false\n"))
	bot := h.CreateBot("Literal").GetId()
	dummy.Collect("Test_K4", dummy.Remember(`{"content":"The human dislikes long answers","kind":"preference","inferred":true}`))
	if res := talk(t, h, bot, h.FirstChat(bot), "Test_K4_Input shorter."); res.GetSaved() != 0 {
		t.Fatalf("saved %d with inference off", res.GetSaved())
	}
	var n int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ?", bot).Count(&n)
	if n != 0 {
		t.Fatalf("%d memories with inference off", n)
	}
	reqs := keeperRequests()
	if sys := systemText(reqs[0]); !strings.Contains(sys, "Only what was said") || strings.Contains(sys, "What nobody said") {
		t.Fatalf("the keeper's prompt with inference off:\n%s", sys)
	}
}

// The keeper's run is a run like any other, narrowed to its job: the memory
// tools and nothing else, a prompt of its own, and no machine.
func TestKeeperRunIsNarrowedToItsJob(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Narrow").GetId()
	chat := h.FirstChat(bot)
	run, _ := h.Send(bot, chat, "Test_K5_Input hello")
	h.WaitRun(run)
	fake := h.Host.(*apptest.FakeHost)
	creates := fake.Creates.Load()
	var before db.Bot
	h.DB.First(&before, "id = ?", bot)

	dummy.Collect("Test_K5",
		dummy.Call("terminal", `{"command":"echo owned"}`),
		dummy.Remember(`{"content":"The human said hello"}`),
	)
	collect(t, h, bot, chat)

	au := h.App.Automations.Pinned(bot, memory.KeeperKind)
	if au == nil || au.ChatID == "" {
		t.Fatalf("no pinned memory automation: %+v", au)
	}
	var log db.Run
	h.DB.Where("chat_id = ?", au.ChatID).Order("created_at DESC").First(&log)
	if log.Origin != "automation" || log.Status != "done" {
		t.Fatalf("keeper run %+v", log)
	}
	var results []string
	for _, ev := range h.Events(log.ID) {
		if ev.Kind == "tool_result" {
			results = append(results, ev.Body)
		}
	}
	if len(results) != 2 || !strings.Contains(results[0], "unknown tool terminal") || !strings.Contains(results[1], "remembered") {
		t.Fatalf("tool results %q", results)
	}

	reqs := keeperRequests()
	var names []string
	for _, tl := range reqs[0].Tools {
		names = append(names, tl.Name)
	}
	if got := strings.Join(names, " "); got != "remember recall forget reinforce review_memories" {
		t.Fatalf("keeper tools: %s", got)
	}
	if sys := systemText(reqs[0]); strings.Contains(sys, "## SOUL") || strings.Contains(sys, "exec_python") || !strings.Contains(sys, "This Bot's name is Narrow") {
		t.Fatalf("the keeper's prompt should be its own:\n%s", sys)
	}
	if fake.Creates.Load() != creates {
		t.Fatal("a keeper run started the Bot's machine")
	}
	var after db.Bot
	h.DB.First(&after, "id = ?", bot)
	if after.LastTask != before.LastTask {
		t.Fatalf("a keeper run became the Bot's task: %q", after.LastTask)
	}
}

// The keeper counts the memories a conversation leaned on.
func TestKeeperCountsWhatWasUsed(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Counter").GetId()
	saved, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: "The human's accountant is Marta"})
	if err != nil {
		t.Fatal(err)
	}
	dummy.Collect("Test_K6", dummy.Call("reinforce", fmt.Sprintf(`{"ids":[%q,"nope"]}`, saved.ID)))
	talk(t, h, bot, h.FirstChat(bot), "Test_K6_Input mail my accountant")
	if m := memoryLike(h, bot, "Marta"); m.UseCount != 1 || m.LastUsedAt == nil {
		t.Fatalf("use not counted: %+v", m)
	}
}

// remember shows what is already close, so whoever saves can retire what the
// new memory makes out of date.
func TestRememberShowsWhatIsClose(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Mover").GetId()
	old, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: "The human's office is in Warsaw"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.App.Memory.Remember(h.Ctx(), memory.Entry{BotID: bot, Content: "The human's office is in Berlin"})
	if err != nil || !strings.Contains(out, "Close to it already") || !strings.Contains(out, old.ID) {
		t.Fatalf("remember: %q %v", out, err)
	}
	out, err = h.App.Memory.Remember(h.Ctx(), memory.Entry{BotID: bot, Content: "The human's office moved to Berlin in 2026", Replaces: []string{old.ID}})
	if err != nil || !strings.Contains(out, "retired "+old.ID) {
		t.Fatalf("replace: %q %v", out, err)
	}
	if m := memoryLike(h, bot, "Warsaw"); m.Status != memory.StatusRetired {
		t.Fatalf("the replaced memory is %q", m.Status)
	}
}

// recall finds a memory by an exact word as well as by meaning.
func TestRecallFindsExactWords(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Exact").GetId()
	for _, c := range []string{
		"The staging server is srv-4471-b in Frankfurt",
		"The production server is in Dublin",
		"The human prefers the Frankfurt office",
	} {
		if _, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: c}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := h.App.Memory.Recall(h.Ctx(), bot, "what is srv-4471-b", 3, false)
	if err != nil || len(rows) == 0 || !strings.Contains(rows[0].Content, "staging") {
		t.Fatalf("recall: %+v %v", rows, err)
	}
}

// Tidying looks the store over in batches, each memory beside its neighbours,
// and merges by replacing.
func TestKeeperTidiesByReplacing(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Tidy").GetId()
	var made []string
	for _, c := range []string{"The human's office is in Warsaw", "The human's office is in Warsaw now", "The human's dog is called Rex"} {
		s, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: c})
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, s.ID)
	}
	if len(made) != 3 || made[0] == made[1] {
		t.Fatalf("the two office memories should be separate rows: %v", made)
	}
	dummy.Collect("",
		dummy.Call("review_memories", `{}`),
		dummy.Remember(fmt.Sprintf(`{"content":"The human's office has been in Warsaw since 2026","replaces":[%q,%q]}`, made[0], made[1])),
	)
	runID, status, err := h.App.Automations.Do(h.Ctx(), bot, memory.KeeperKind, "")
	if err != nil || status != "done" {
		t.Fatalf("tidy run: %q %v", status, err)
	}
	batch := ""
	for _, ev := range h.Events(runID) {
		if ev.Kind == "tool_result" && batch == "" {
			batch = ev.Body
		}
	}
	if !strings.Contains(batch, "3 to look over") || !strings.Contains(batch, "close to it: ["+made[1]+"]") {
		t.Fatalf("the batch should set neighbours side by side:\n%s", batch)
	}
	merged := memoryLike(h, bot, "since 2026")
	for _, id := range made[:2] {
		var m db.Memory
		h.DB.First(&m, "id = ?", id)
		if m.Status != memory.StatusRetired || m.SupersededBy != merged.ID {
			t.Fatalf("%s: %q superseded by %q, want %q", id, m.Status, m.SupersededBy, merged.ID)
		}
	}
	var rex db.Memory
	h.DB.First(&rex, "id = ?", made[2])
	if rex.Status != memory.StatusActive || rex.ReviewedAt == nil {
		t.Fatalf("an untouched memory: %+v", rex)
	}
}

// The keeper tidies unasked only once enough has piled up, and not again
// right after.
func TestTendTidiesWhenMemoriesPileUp(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Piler").GetId()
	save := func(i int) {
		if _, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: fmt.Sprintf("Fact number %d about topic%d", i, i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 5 {
		save(i)
	}
	if n := h.App.Memory.Tend(h.Ctx(), time.Now()); n != 0 {
		t.Fatalf("tidied %d Bots with five memories", n)
	}
	for i := 5; i < 20; i++ {
		save(i)
	}
	dummy.Collect("", dummy.Call("review_memories", `{}`))
	if n := h.App.Memory.Tend(h.Ctx(), time.Now()); n != 1 {
		t.Fatalf("tidied %d Bots with twenty memories waiting", n)
	}
	var reviewed int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ? AND reviewed_at IS NOT NULL", bot).Count(&reviewed)
	// The model has room for the whole store, so one batch is all of it.
	if reviewed != 20 {
		t.Fatalf("one batch should look over all twenty, did %d", reviewed)
	}
	if n := h.App.Memory.Tend(h.Ctx(), time.Now().Add(time.Hour)); n != 0 {
		t.Fatal("tidied again within hours")
	}
}

// The owner keeps a guess or puts a retired memory back; what was retired
// long ago, and a guess nothing ever supported, are purged.
func TestKeepMemoryAndPurge(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Owner").GetId()
	keep := func(id string) *v1.Memory {
		res, err := h.Client.KeepMemory(h.Ctx(), connect.NewRequest(&v1.KeepMemoryRequest{BotId: bot, Id: id}))
		if err != nil {
			t.Fatalf("keep: %v", err)
		}
		return res.Msg
	}
	guess, _ := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: "The human reads mail only in the morning", Inferred: true})
	if m := keep(guess.ID); m.GetStatus() != memory.StatusActive || !m.GetInferred() {
		t.Fatalf("kept guess %+v", m)
	}
	if _, err := h.App.Memory.Forget(bot, guess.ID, false); err != nil {
		t.Fatal(err)
	}
	list, _ := h.Client.ListMemories(h.Ctx(), connect.NewRequest(&v1.ListMemoriesRequest{BotId: bot}))
	if ms := list.Msg.GetMemories(); len(ms) != 1 || ms[0].GetStatus() != memory.StatusRetired || ms[0].GetRetiredAt() == "" {
		t.Fatalf("a forgotten memory should still be listed as retired: %+v", ms)
	}
	if m := keep(guess.ID); m.GetStatus() != memory.StatusActive || m.GetRetiredAt() != "" {
		t.Fatalf("put back %+v", m)
	}

	h.App.Memory.Forget(bot, guess.ID, false)
	stale, _ := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: "The human may like jazz in the evening", Inferred: true})
	fresh, _ := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: "The human may prefer trains to planes", Inferred: true})
	h.DB.Exec("UPDATE memories SET retired_at = retired_at - interval '31 days' WHERE id = ?", guess.ID)
	h.DB.Exec("UPDATE memories SET created_at = created_at - interval '91 days' WHERE id = ?", stale.ID)
	h.App.Memory.Tend(h.Ctx(), time.Now())
	var left []string
	h.DB.Model(&db.Memory{}).Where("bot_id = ?", bot).Pluck("id", &left)
	if len(left) != 1 || left[0] != fresh.ID {
		t.Fatalf("after the purge: %v, want only %s", left, fresh.ID)
	}
}

// Preferences and lessons are told word for word, each under its own head,
// ahead of the facts.
func TestPromptTellsPreferencesAndLessonsFirst(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Layout").GetId()
	for _, e := range []memory.Entry{
		{Content: "The human's sister is Ana"},
		{Content: "The bank site rejects scripted input; use the desktop", Kind: memory.KindLesson},
		{Content: "The human wants dates written as 2026-11-16", Kind: memory.KindPreference},
	} {
		e.BotID = bot
		if _, err := h.App.Memory.Save(h.Ctx(), e); err != nil {
			t.Fatal(err)
		}
	}
	p := h.App.SystemPromptText(bot)
	at := func(s string) int { return strings.Index(p, s) }
	order := []int{at("Preferences:"), at("2026-11-16"), at("Lessons:"), at("bank site"), at("Facts, oldest first"), at("sister is Ana")}
	for i, v := range order {
		if v < 0 || (i > 0 && v < order[i-1]) {
			t.Fatalf("memory section out of order %v:\n%s", order, p[max(0, len(p)-900):])
		}
	}
}

// What the Bot was working on, and where, is told as the newest few entries
// after the facts. It is what happened, so it never waits as a guess, and a
// later entry for the same work replaces the earlier one.
func TestPromptTellsRecentWork(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Worker").GetId()
	save := func(e memory.Entry) memory.Saved {
		t.Helper()
		e.BotID = bot
		s, err := h.App.Memory.Save(h.Ctx(), e)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	save(memory.Entry{Content: "The human's sister is Ana"})
	var made []memory.Saved
	for i := range memory.WorkShown + 1 {
		made = append(made, save(memory.Entry{
			Content: fmt.Sprintf("Built report%d in /workspace/reports/job%d; charts still to add", i, i),
			Kind:    memory.KindWork, Inferred: i == 1,
		}))
	}
	if made[1].Status != memory.StatusActive {
		t.Fatalf("work is what happened, not a guess: saved as %q", made[1].Status)
	}
	p := h.App.SystemPromptText(bot)
	head := strings.Index(p, "Recent work")
	if head < 0 || head < strings.Index(p, "sister is Ana") {
		t.Fatalf("recent work should follow the facts:\n%s", p[max(0, len(p)-900):])
	}
	if strings.Contains(p, "job0;") || !strings.Contains(p, "job1;") || !strings.Contains(p, fmt.Sprintf("job%d;", memory.WorkShown)) {
		t.Fatalf("want the newest %d entries only:\n%s", memory.WorkShown, p[head:])
	}

	save(memory.Entry{Content: "Finished report1 in /workspace/reports/job1 with its charts", Kind: memory.KindWork, Replaces: []string{made[1].ID}})
	p = h.App.SystemPromptText(bot)
	if strings.Contains(p, "job1;") || !strings.Contains(p, "Finished report1") {
		t.Fatalf("the newer entry should stand in for the one it replaces:\n%s", p[strings.Index(p, "Recent work"):])
	}
}

// Work that nobody touched for a month comes up for tidying again, marked,
// so the keeper folds it into a fact or lets it go.
func TestOldWorkIsLookedOverAgain(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Aged").GetId()
	old, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: "Set up the hiking site in /workspace/site on port 5180", Kind: memory.KindWork})
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := h.App.Memory.Review(bot, "r1"); !strings.Contains(out, old.ID) || strings.Contains(out, "untouched") {
		t.Fatalf("a new entry is looked over once, unmarked:\n%s", out)
	}
	if out, _ := h.App.Memory.Review(bot, "r2"); strings.Contains(out, old.ID) {
		t.Fatalf("looked over twice:\n%s", out)
	}
	h.DB.Exec("UPDATE memories SET created_at = created_at - interval '31 days', reviewed_at = reviewed_at - interval '31 days' WHERE id = ?", old.ID)
	if out, _ := h.App.Memory.Review(bot, "r3"); !strings.Contains(out, old.ID) || !strings.Contains(out, "untouched for over 30 days") {
		t.Fatalf("old work should come up again, marked:\n%s", out)
	}
}

// The keeper reads chats and tidies a pile-up by itself, whatever its
// schedule says, and its row says so: "no schedule" must not read as "never
// saves". With collecting off, it only reads when asked.
func TestKeeperSaysItWorksOnItsOwn(t *testing.T) {
	keeper := func(h *apptest.H) *v1.Automation {
		bot := h.CreateBot("Keeper")
		for _, a := range listAutomations(t, h, bot.GetId()) {
			if a.GetKind() == "memory" {
				return a
			}
		}
		t.Fatal("no Memory automation")
		return nil
	}
	if a := keeper(apptest.New(t)); !a.GetOnItsOwn() || a.GetSchedule() != "" {
		t.Fatalf("the keeper of a new Bot: %+v", a)
	}
	off := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+"memory:\n  collect: false\n"))
	if a := keeper(off); a.GetOnItsOwn() {
		t.Fatalf("collecting is off, yet the keeper says it works on its own: %+v", a)
	}
	// A schedule is the automation's whole story for every other kind.
	h := apptest.New(t)
	bot := h.CreateBot("Plain")
	if a := createAutomation(t, h, bot.GetId(), "Nightly", "sweep", ""); a.GetOnItsOwn() {
		t.Fatalf("an ordinary automation: %+v", a)
	}
}

// A review batch is what fits the room the keeper's model has, and only what
// it showed counts as looked over: the rest is the next batch's.
func TestReviewBatchFitsItsRoom(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Roomy").GetId()
	for i := range 120 {
		if _, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: bot, Content: fmt.Sprintf("Fact number %d about topic%d, told at some length so that a hundred of them do not fit a small room", i, i)}); err != nil {
			t.Fatal(err)
		}
	}
	reviewed := func() int64 {
		var n int64
		h.DB.Model(&db.Memory{}).Where("bot_id = ? AND reviewed_at IS NOT NULL", bot).Count(&n)
		return n
	}
	// Saving folds a near-copy into the memory it repeats, so count what
	// the store holds.
	var all int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ?", bot).Count(&all)
	// A small model: the least room there is.
	h.App.Memory.Room = func() int { return 0 }
	out, err := h.App.Memory.Review(bot, "small")
	if err != nil {
		t.Fatal(err)
	}
	first := reviewed()
	if first == 0 || first >= all || len(out) > memory.ReviewRoomMin+500 {
		t.Fatalf("a small room showed %d of %d in %d bytes", first, all, len(out))
	}
	if want := fmt.Sprintf("%d to look over (%d more wait after these)", first, all-first); !strings.HasPrefix(out, want) {
		t.Fatalf("the batch says %q, want %q", out[:min(len(out), 60)], want)
	}
	// A large one takes the rest at once.
	h.App.Memory.Room = func() int { return memory.ReviewRoomMax }
	if _, err := h.App.Memory.Review(bot, "large"); err != nil {
		t.Fatal(err)
	}
	if got := reviewed(); got != all {
		t.Fatalf("a large room left %d of %d unlooked-at", all-got, all)
	}
}
