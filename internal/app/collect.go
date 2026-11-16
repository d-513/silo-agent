package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/models"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/textx"
)

// Collecting is reading what a conversation said since it was last read
// (chats.memory_seq) and handing it to the memory keeper, whose run saves what
// is worth keeping through the memory tools (memory.Service.Read). Each event
// is read at most once: the watermark moves past everything a run read to its
// end, and stays put when the run failed or was stopped.
const (
	// collectTick is how often the sweep looks for idle chats.
	collectTick = 5 * time.Minute
	// collectIdle is how long a chat must be quiet before the sweep reads it,
	// so it never reads a conversation mid-flow.
	collectIdle = 10 * time.Minute
	// collectPerTick caps the chats one sweep reads, and collectWait how long
	// it waits on one chat's runs.
	collectPerTick = 5
	collectWait    = 5 * time.Minute
	// collectPauseFor holds the sweep back after a provider error.
	collectPauseFor = 15 * time.Minute
	// collectRest is how long a chat whose collection failed is skipped: the
	// pause at first, doubling with each failure in a row up to
	// collectRestMax, so a chat that always fails stops holding up the rest.
	collectRest    = collectPauseFor
	collectRestMax = 24 * time.Hour

	// collectBudget is the excerpt size of one run in tokens; a larger delta
	// is split into at most collectChunks runs and the oldest part dropped.
	collectBudget = 12000
	collectChunks = 3
	// collectShown is how many existing memories each run is shown beside
	// the excerpt.
	collectShown = 15
	// collectQueryRunes is the tail of the excerpt embedded to find them.
	collectQueryRunes = 6000
)

// collectCaps keep tool noise short while retaining enough detail on errors.
var collectCaps = transcriptCaps{Args: 300, Result: 400, ErrorResult: 800}

var errCollecting = errors.New("memories are being collected for this Bot; try again in a moment")

// errReplyRunning refuses to collect a chat with a live run, like the sweep:
// the run moves the events it reads (drainInbox), so a watermark taken now
// could skip one or read it twice.
var errReplyRunning = errors.New("a reply is running in this chat; collect memories once it is done")

type collectResult struct {
	memory.Tally
	Note string
}

// collectChat reads chatID's events after its watermark and has the memory
// keeper save what is worth keeping.
func (a *App) collectChat(ctx context.Context, chatID string) (collectResult, error) {
	var ch db.Chat
	if err := a.DB.Where("id = ?", chatID).Limit(1).Find(&ch).Error; err != nil {
		return collectResult{}, err
	}
	if ch.ID == "" {
		return collectResult{}, fmt.Errorf("no chat %s", chatID)
	}
	if a.LiveRunID(ch.BotID, ch.ID) != "" {
		return collectResult{}, errReplyRunning
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
	// A compaction is left out: the keeper reads the turns themselves,
	// not a summary of what it may already have read. So is a /clear: what
	// came before it is still the conversation's to remember.
	var evs []db.RunEvent
	a.DB.Joins("JOIN runs ON runs.id = run_events.run_id").
		Where("runs.chat_id = ? AND run_events.seq > ? AND run_events.seq <= ? AND run_events.kind NOT IN ?",
			ch.ID, ch.MemorySeq, maxSeq, append([]string{compactionKind, compactingKind, contextClearedKind, wipedKind}, streamedKinds...)).
		Order("run_events.seq").Find(&evs)

	if !hasUserText(historyFromEvents(evs)) {
		a.advanceMemorySeq(ch.ID, maxSeq)
		return collectResult{Note: "nothing new"}, nil
	}
	// The delta is split by events, not by transcript, so the watermark can
	// move past each part once its run is done: a failure in a later part
	// retries only that part, and the keeper never re-reads (and re-saves in
	// other words) what it already took from an earlier one.
	parts, dropped := splitEvents(evs, collectBudget, collectChunks)

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
		shown, err := a.Memory.Closest(ctx, ch.BotID, tailRunes(chunk, collectQueryRunes), collectShown)
		if err != nil {
			return res, models.ProviderError{Err: err}
		}
		tally, status, err := a.Memory.Read(ctx, ch.BotID, ch.ID, memory.Excerpt(shown, chunk, note))
		res.Saved, res.Updated, res.Forgotten = res.Saved+tally.Saved, res.Updated+tally.Updated, res.Forgotten+tally.Forgotten
		res.IDs = append(res.IDs, tally.IDs...)
		switch {
		case errors.Is(err, memory.ErrBusy):
			return res, errCollecting
		case err != nil:
			return res, err
		case status == "error":
			// A run ends in error when its model call failed.
			return res, models.ProviderError{Err: errors.New("the memory model failed; its run is in the Memory automation's log")}
		case status != "done":
			return res, fmt.Errorf("the memory run did not finish (%s)", cmp.Or(status, "still running"))
		}
		a.advanceMemorySeq(ch.ID, upTo)
	}
	return res, nil
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
		ctx, cancel := context.WithTimeout(context.Background(), collectWait)
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
	b, err := a.bot(ctx, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	ch, err := a.chat(ctx, b.ID, req.Msg.GetChatId(), access.Write)
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
	if errors.Is(err, errReplyRunning) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("memory collection failed: %w", err))
	}
	out := &v1.CollectMemoriesResponse{
		Saved: int32(res.Saved), Updated: int32(res.Updated), Forgotten: int32(res.Forgotten), Note: res.Note,
	}
	out.Memories = a.Memory.Protos(res.IDs)
	return connect.NewResponse(out), nil
}
