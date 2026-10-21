package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/models"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
	"silo.agent/internal/textx"
)

// The memory collector reads what a conversation said since it was last
// collected (chats.memory_seq) and asks a cheap model, in one call with no
// tools, for the facts worth keeping. Each event is read at most
// once: the watermark moves past everything a collection read, including a
// reply it could not parse, and stays put only on a provider error.
const (
	// collectTick is how often the sweep looks for idle chats.
	collectTick = 5 * time.Minute
	// collectIdle is how long a chat must be quiet before the sweep reads it,
	// so it never reads a conversation mid-flow.
	collectIdle = 10 * time.Minute
	// collectPerTick caps the chats one sweep reads.
	collectPerTick = 5
	// collectPauseFor holds the sweep back after a provider error.
	collectPauseFor = 15 * time.Minute
	// collectRest is how long a chat whose collection failed is skipped: the
	// pause at first, doubling with each failure in a row up to
	// collectRestMax, so a chat that always fails stops holding up the rest.
	collectRest    = collectPauseFor
	collectRestMax = 24 * time.Hour

	// collectBudget is the excerpt size of one call in tokens; a larger delta
	// is split into at most collectChunks calls and the oldest part dropped.
	collectBudget = 12000
	collectChunks = 3
	// collectShown is how many existing memories each call sees for dedupe.
	collectShown = 15
	// collectQueryRunes is the tail of the excerpt embedded to find them.
	collectQueryRunes = 6000
	// Caps on what one reply may change.
	collectMaxSave   = 10
	collectMaxUpdate = 5
	collectMaxForget = 5
)

// collectCaps keep tool noise short while retaining enough detail on errors.
var collectCaps = transcriptCaps{Args: 300, Result: 400, ErrorResult: 800}

var errCollecting = errors.New("memories are already being collected for this chat")

type collectResult struct {
	Saved, Updated, Forgotten int
	// IDs are the memories this collection saved or updated.
	IDs  []string
	Note string
}

// collectReply is the JSON the collector model answers with.
type collectReply struct {
	Save []struct {
		Text string `json:"text"`
	} `json:"save"`
	Update []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"update"`
	Forget []string `json:"forget"`
}

// collectChat reads chatID's events after its watermark and saves what the
// collector model picks out.
func (a *App) collectChat(ctx context.Context, chatID string) (collectResult, error) {
	if _, busy := a.collecting.LoadOrStore(chatID, struct{}{}); busy {
		return collectResult{}, errCollecting
	}
	defer a.collecting.Delete(chatID)

	var ch db.Chat
	if err := a.DB.Where("id = ?", chatID).Limit(1).Find(&ch).Error; err != nil {
		return collectResult{}, err
	}
	if ch.ID == "" {
		return collectResult{}, fmt.Errorf("no chat %s", chatID)
	}
	// The watermark moves past every event, streamed ones too, or the sweep
	// would keep finding this chat; only what replay reads is loaded.
	var maxSeq int64
	a.DB.Model(&db.RunEvent{}).Joins("JOIN runs ON runs.id = run_events.run_id").
		Where("runs.chat_id = ? AND run_events.seq > ?", ch.ID, ch.MemorySeq).
		Select("COALESCE(MAX(run_events.seq), 0)").Scan(&maxSeq)
	if maxSeq == 0 {
		return collectResult{Note: "nothing new"}, nil
	}
	// A compaction is left out: the collector reads the turns themselves,
	// not a summary of what it may already have read.
	var evs []db.RunEvent
	a.DB.Joins("JOIN runs ON runs.id = run_events.run_id").
		Where("runs.chat_id = ? AND run_events.seq > ? AND run_events.seq <= ? AND run_events.kind NOT IN ?",
			ch.ID, ch.MemorySeq, maxSeq, append([]string{compactionKind, compactingKind}, streamedKinds...)).
		Order("run_events.seq").Find(&evs)

	if !hasUserText(historyFromEvents(evs)) {
		a.advanceMemorySeq(ch.ID, maxSeq)
		return collectResult{Note: "nothing new"}, nil
	}
	// The delta is split by events, not by transcript, so the watermark can
	// move past each part once it is applied: a failure in a later part
	// retries only that part, and the model never re-reads (and re-saves in
	// other words) what it already took from an earlier one.
	parts, dropped := splitEvents(evs, collectBudget, collectChunks)

	modelID := a.cfg().MemoryModel()
	client, provider, model, err := a.Models.Observed(modelID, ch.BotID, "memory")
	if err != nil {
		return collectResult{}, models.ProviderError{Err: err}
	}
	cache := models.CachePolicy(a.cfg().ProviderSettings(provider), "memory")
	cache.Key, cache.Messages = "silo-memory-collector", false

	var res collectResult
	for i, part := range parts {
		// The last part carries the watermark past the delta's streamed tail.
		upTo := part[len(part)-1].Seq
		if i == len(parts)-1 {
			upTo = maxSeq
		}
		entries, _ := transcriptEntries(historyFromEvents(part), collectCaps)
		if len(entries) == 0 {
			a.advanceMemorySeq(ch.ID, upTo)
			continue
		}
		// A part with no place to cut keeps its newest budget's worth.
		cut, over := chunkEntries(entries, collectBudget, 1)
		chunk := cut[len(cut)-1]
		note := ""
		if len(parts) > 1 {
			note = fmt.Sprintf(" (part %d of %d)", i+1, len(parts))
		}
		if i == 0 && dropped > 0 {
			chunk = fmt.Sprintf("[%d older events were dropped to fit.]\n\n", dropped) + chunk
		}
		if over > 0 {
			chunk = fmt.Sprintf("[%d older entries of this part were dropped to fit.]\n\n", over) + chunk
		}
		shown, err := a.Memory.Search(ctx, ch.BotID, tailRunes(chunk, collectQueryRunes), collectShown, 0)
		if err != nil {
			return res, models.ProviderError{Err: err}
		}
		out, err := client.Complete(ctx, llm.Request{
			Model:     model,
			System:    []llm.SystemBlock{{Text: prompts.Memory, CacheAfter: true}},
			Messages:  []llm.Message{{Role: llm.RoleUser, Text: collectInput(shown, chunk, note)}},
			Cache:     cache,
			MaxTokens: 1500,
		})
		if err != nil {
			return res, models.ProviderError{Err: err}
		}
		reply, ok := parseCollectReply(out.Text)
		if !ok {
			log.Printf("memory collector: chat %s: unreadable reply %q", ch.ID, textx.TruncateUTF8(out.Text, 200))
			res.Note = "the model's reply could not be read"
			a.advanceMemorySeq(ch.ID, upTo)
			continue
		}
		if err := a.applyCollect(ctx, &ch, shown, reply, &res); err != nil {
			return res, err
		}
		a.advanceMemorySeq(ch.ID, upTo)
	}
	return res, nil
}

// applyCollect saves, rewrites, and forgets what one reply asked for. Only ids
// the model was shown may be rewritten or forgotten.
func (a *App) applyCollect(ctx context.Context, ch *db.Chat, shown []memory.Recalled, r collectReply, res *collectResult) error {
	known := map[string]bool{}
	for _, m := range shown {
		known[m.ID] = true
	}
	for i, s := range r.Save {
		if i == collectMaxSave {
			break
		}
		text := strings.TrimSpace(s.Text)
		if text == "" || utf8.RuneCountInString(text) > memory.RememberMax {
			continue
		}
		saved, err := a.Memory.Save(ctx, memory.Entry{BotID: ch.BotID, ChatID: ch.ID, Content: text})
		if err != nil {
			return models.ProviderError{Err: err}
		}
		res.IDs = append(res.IDs, saved.ID)
		if saved.Updated {
			res.Updated++
		} else {
			res.Saved++
		}
	}
	for i, u := range r.Update {
		if i == collectMaxUpdate {
			break
		}
		id := strings.TrimSpace(u.ID)
		if !known[id] {
			continue
		}
		if err := a.Memory.Rewrite(ctx, ch.BotID, id, u.Text); err != nil {
			continue
		}
		res.IDs = append(res.IDs, id)
		res.Updated++
	}
	for i, id := range r.Forget {
		if i == collectMaxForget {
			break
		}
		id = strings.TrimSpace(id)
		if !known[id] {
			continue
		}
		if _, err := a.Memory.Forget(ch.BotID, id); err == nil {
			res.Forgotten++
		}
	}
	return nil
}

// advanceMemorySeq moves a chat's watermark forward, never back.
func (a *App) advanceMemorySeq(chatID string, seq int64) {
	a.DB.Model(&db.Chat{}).Where("id = ? AND memory_seq < ?", chatID, seq).UpdateColumn("memory_seq", seq)
}

func hasUserText(msgs []llm.Message) bool {
	for _, m := range msgs {
		if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
			return true
		}
	}
	return false
}

// splitEvents cuts a delta into parts of about budget tokens, at most max of
// them (limit), oldest first. A part only ends before a user message or a model turn,
// so no tool call is parted from its results. What does not fit in max parts
// is dropped from the front (dropped counts those events), so the newest part
// of a conversation is always read.
func splitEvents(evs []db.RunEvent, budget, limit int) (parts [][]db.RunEvent, dropped int) {
	// A result reads capped in the transcript; count it that way.
	capTokens := runeTokens(strings.Repeat("x", max(collectCaps.Result, collectCaps.ErrorResult)))
	start, size := 0, 0
	for i, e := range evs {
		cuttable := e.Kind == "user" || e.Kind == turnKind || e.Kind == "assistant"
		if cuttable && size >= budget && i > start {
			parts = append(parts, evs[start:i])
			start, size = i, 0
		}
		n := runeTokens(e.Body)
		if e.Kind == "tool_result" {
			n = min(n, capTokens)
		}
		size += n + 1
	}
	parts = append(parts, evs[start:])
	if len(parts) > limit {
		for _, p := range parts[:len(parts)-limit] {
			dropped += len(p)
		}
		parts = parts[len(parts)-limit:]
	}
	return parts, dropped
}

// chunkEntries packs transcript entries into at most max chunks of about
// budget tokens each, oldest first. What does not fit is dropped from the
// front, so the newest part of a conversation is always read.
func chunkEntries(entries []string, budget, max int) (chunks []string, dropped int) {
	capRunes := budget * 3
	sizes := make([]int, len(entries))
	total := 0
	for i, e := range entries {
		if utf8.RuneCountInString(e) > capRunes {
			entries[i] = textx.TruncateUTF8(e, capRunes) + "\n…truncated"
		}
		sizes[i] = runeTokens(entries[i]) + 1
		total += sizes[i]
	}
	start := 0
	for total > budget*max && start < len(entries)-1 {
		total -= sizes[start]
		start++
	}
	var cur []string
	n := 0
	for i := start; i < len(entries); i++ {
		if n > 0 && n+sizes[i] > budget {
			chunks = append(chunks, strings.Join(cur, "\n\n"))
			cur, n = nil, 0
		}
		cur = append(cur, entries[i])
		n += sizes[i]
	}
	if len(cur) > 0 {
		chunks = append(chunks, strings.Join(cur, "\n\n"))
	}
	// Greedy packing can spill one chunk over max; keep the newest.
	if len(chunks) > max {
		chunks = chunks[len(chunks)-max:]
	}
	return chunks, start
}

// collectInput is the one user message of a collection: what the Bot already
// knows, then the excerpt, last.
func collectInput(shown []memory.Recalled, excerpt, note string) string {
	var b strings.Builder
	b.WriteString("Already saved (closest to this excerpt):\n")
	if len(shown) == 0 {
		b.WriteString("(none)\n")
	}
	for _, m := range shown {
		fmt.Fprintf(&b, "- [%s] %s\n", m.ID, m.Content)
	}
	fmt.Fprintf(&b, "\nConversation excerpt%s:\n\n%s", note, excerpt)
	return b.String()
}

// parseCollectReply reads the model's JSON, tolerating a code fence or prose
// around it.
func parseCollectReply(s string) (collectReply, bool) {
	var r collectReply
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return r, false
	}
	if err := json.Unmarshal([]byte(s[i:j+1]), &r); err != nil {
		return collectReply{}, false
	}
	return r, true
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// --- sweep ---

// SweepMemories collects every web and channel chat that has new messages and
// has been quiet for collectIdle, at most collectPerTick of them, oldest
// first. It returns how many it read.
func (a *App) SweepMemories(now time.Time) int {
	if a.DB == nil || !a.cfg().Memory.Collect || now.UnixNano() < a.collectPause.Load() {
		return 0
	}
	type due struct {
		ID    string
		BotID string
	}
	// A chat whose collection failed rests (collectBackoff), so one that
	// always fails cannot hold the head of the queue for every Bot.
	resting := []string{""}
	a.collectBackoff.Range(func(k, v any) bool {
		if now.Before(v.(collectRestState).until) {
			resting = append(resting, k.(string))
		}
		return true
	})
	var rows []due
	a.DB.Raw(`SELECT chats.id, chats.bot_id FROM chats
		JOIN runs ON runs.chat_id = chats.id
		JOIN run_events e ON e.run_id = runs.id
		WHERE chats.automation_id = '' AND chats.subagent_id = '' AND e.seq > chats.memory_seq
			AND chats.id NOT IN ?
		GROUP BY chats.id, chats.bot_id
		HAVING MAX(e.created_at) < ?
		ORDER BY MAX(e.created_at)
		LIMIT ?`, resting, now.Add(-collectIdle), collectPerTick*4).Scan(&rows)
	read := 0
	for _, r := range rows {
		if read == collectPerTick {
			break
		}
		if a.LiveRunID(r.BotID, r.ID) != "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		_, err := a.collectChat(ctx, r.ID)
		cancel()
		if errors.Is(err, errCollecting) {
			continue
		}
		read++
		if err == nil {
			a.collectBackoff.Delete(r.ID)
		}
		if err != nil {
			log.Printf("memory collector: chat %s: %v", r.ID, err)
			a.restChat(r.ID, now)
			var pe models.ProviderError
			if errors.As(err, &pe) {
				a.collectPause.Store(now.Add(collectPauseFor).UnixNano())
				return read
			}
		}
	}
	return read
}

// collectRestState is a chat's place in the collector's backoff.
type collectRestState struct {
	until    time.Time
	failures int
}

// restChat backs a chat whose collection failed off for twice as long as
// last time.
func (a *App) restChat(chatID string, now time.Time) {
	st := collectRestState{failures: 1}
	if v, ok := a.collectBackoff.Load(chatID); ok {
		st.failures = v.(collectRestState).failures + 1
	}
	rest := collectRest
	for i := 1; i < st.failures && rest < collectRestMax; i++ {
		rest *= 2
	}
	st.until = now.Add(min(rest, collectRestMax))
	a.collectBackoff.Store(chatID, st)
}

// --- RPC ---

func (a *App) CollectMemories(ctx context.Context, req *connect.Request[v1.CollectMemoriesRequest]) (*connect.Response[v1.CollectMemoriesResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	ch, err := a.ownChat(ctx, b.ID, req.Msg.GetChatId())
	if err != nil {
		return nil, err
	}
	if err := writableChat(ch); err != nil {
		return nil, err
	}
	res, err := a.collectChat(ctx, ch.ID)
	if errors.Is(err, errCollecting) {
		return nil, connect.NewError(connect.CodeAborted, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("memory collection failed: %w", err))
	}
	out := &v1.CollectMemoriesResponse{
		Saved: int32(res.Saved), Updated: int32(res.Updated), Forgotten: int32(res.Forgotten), Note: res.Note,
	}
	if len(res.IDs) > 0 {
		var rows []db.Memory
		a.DB.Select("id, chat_id, content, created_at, last_used_at").Where("id IN ?", res.IDs).Order("created_at").Find(&rows)
		for _, r := range rows {
			out.Memories = append(out.Memories, memory.Proto(r))
		}
	}
	return connect.NewResponse(out), nil
}
