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
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
)

// The memory collector reads what a conversation said since it was last
// collected (chats.memory_seq) and asks a cheap model, in one call with no
// tools, for the facts and lessons worth keeping. Each event is read at most
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

// collectCaps keep tool noise short; failures keep more because that is where
// the lessons are.
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
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"save"`
	Update []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"update"`
	Forget []string `json:"forget"`
}

// providerError marks a failure that should leave the watermark in place and
// pause the sweep: the model or the embedder could not be reached.
type providerError struct{ err error }

func (e providerError) Error() string { return e.err.Error() }
func (e providerError) Unwrap() error { return e.err }

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
	var evs []db.RunEvent
	a.DB.Joins("JOIN runs ON runs.id = run_events.run_id").
		Where("runs.chat_id = ? AND run_events.seq > ?", ch.ID, ch.MemorySeq).
		Order("run_events.seq").Find(&evs)
	if len(evs) == 0 {
		return collectResult{Note: "nothing new"}, nil
	}
	maxSeq := evs[len(evs)-1].Seq

	msgs := historyFromEvents(evs)
	if !hasUserText(msgs) {
		a.advanceMemorySeq(ch.ID, maxSeq)
		return collectResult{Note: "nothing new"}, nil
	}
	entries, _ := transcriptEntries(msgs, collectCaps)
	chunks, dropped := chunkEntries(entries, collectBudget, collectChunks)

	var bot db.Bot
	a.DB.Select("id, memory").Where("id = ?", ch.BotID).Limit(1).Find(&bot)
	modelID := a.cfg().MemoryModel()
	client, provider, model, err := a.modelClient(modelID, ch.BotID, "memory")
	if err != nil {
		return collectResult{}, providerError{err}
	}
	cache := cachePolicy(a.cfg().ProviderSettings(provider), "memory")
	cache.Key, cache.Messages = "silo-memory-collector", false

	var res collectResult
	for i, chunk := range chunks {
		note := ""
		if len(chunks) > 1 {
			note = fmt.Sprintf(" (part %d of %d)", i+1, len(chunks))
		}
		if i == 0 && dropped > 0 {
			chunk = fmt.Sprintf("[%d older entries were dropped to fit.]\n\n", dropped) + chunk
		}
		shown, err := a.search(ctx, ch.BotID, tailRunes(chunk, collectQueryRunes), collectShown, 0)
		if err != nil {
			return res, providerError{err}
		}
		out, err := client.Complete(ctx, llm.Request{
			Model:     model,
			System:    []llm.SystemBlock{{Text: prompts.Memory, CacheAfter: true}},
			Messages:  []llm.Message{{Role: llm.RoleUser, Text: collectInput(bot.Memory, shown, chunk, note)}},
			Cache:     cache,
			MaxTokens: 1500,
		})
		if err != nil {
			return res, providerError{err}
		}
		reply, ok := parseCollectReply(out.Text)
		if !ok {
			log.Printf("memory collector: chat %s: unreadable reply %q", ch.ID, truncateUTF8(out.Text, 200))
			res.Note = "the model's reply could not be read"
			continue
		}
		if err := a.applyCollect(ctx, &ch, shown, reply, &res); err != nil {
			return res, err
		}
	}
	a.advanceMemorySeq(ch.ID, maxSeq)
	return res, nil
}

// applyCollect saves, rewrites, and forgets what one reply asked for. Only ids
// the model was shown may be rewritten or forgotten.
func (a *App) applyCollect(ctx context.Context, ch *db.Chat, shown []recalled, r collectReply, res *collectResult) error {
	known := map[string]bool{}
	for _, m := range shown {
		known[m.ID] = true
	}
	for i, s := range r.Save {
		if i == collectMaxSave {
			break
		}
		text := strings.TrimSpace(s.Text)
		if text == "" || len(text) > rememberMax {
			continue
		}
		kind := memoryFact
		if strings.EqualFold(strings.TrimSpace(s.Kind), memoryLesson) {
			kind = memoryLesson
		}
		saved, err := a.saveMemory(ctx, newMemory{botID: ch.BotID, chatID: ch.ID, kind: kind, content: text})
		if err != nil {
			return providerError{err}
		}
		res.IDs = append(res.IDs, saved.id)
		if saved.updated {
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
		if err := a.rewriteMemory(ctx, ch.BotID, id, u.Text); err != nil {
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
		if _, err := a.forget(ch.BotID, id); err == nil {
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

// chunkEntries packs transcript entries into at most max chunks of about
// budget tokens each, oldest first. What does not fit is dropped from the
// front, so the newest part of a conversation is always read.
func chunkEntries(entries []string, budget, max int) (chunks []string, dropped int) {
	capRunes := budget * 3
	sizes := make([]int, len(entries))
	total := 0
	for i, e := range entries {
		if utf8.RuneCountInString(e) > capRunes {
			entries[i] = truncateUTF8(e, capRunes) + "\n…truncated"
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
func collectInput(core string, shown []recalled, excerpt, note string) string {
	var b strings.Builder
	b.WriteString("CORE MEMORY:\n")
	if c := strings.TrimSpace(core); c != "" {
		b.WriteString(c)
	} else {
		b.WriteString("(empty)")
	}
	b.WriteString("\n\nAlready saved (closest to this excerpt):\n")
	if len(shown) == 0 {
		b.WriteString("(none)\n")
	}
	for _, m := range shown {
		kind := m.Kind
		if kind == "" {
			kind = memoryFact
		}
		fmt.Fprintf(&b, "- [%s] %s: %s\n", m.ID, kind, m.Content)
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
	var rows []due
	a.DB.Raw(`SELECT chats.id, chats.bot_id FROM chats
		JOIN runs ON runs.chat_id = chats.id
		JOIN run_events e ON e.run_id = runs.id
		WHERE chats.automation_id = '' AND chats.subagent_id = '' AND e.seq > chats.memory_seq
		GROUP BY chats.id, chats.bot_id
		HAVING MAX(e.created_at) < ?
		ORDER BY MAX(e.created_at)
		LIMIT ?`, now.Add(-collectIdle), collectPerTick*4).Scan(&rows)
	read := 0
	for _, r := range rows {
		if read == collectPerTick {
			break
		}
		if a.liveRunID(r.BotID, r.ID) != "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		_, err := a.collectChat(ctx, r.ID)
		cancel()
		if errors.Is(err, errCollecting) {
			continue
		}
		read++
		if err != nil {
			log.Printf("memory collector: chat %s: %v", r.ID, err)
			var pe providerError
			if errors.As(err, &pe) {
				a.collectPause.Store(now.Add(collectPauseFor).UnixNano())
				return read
			}
		}
	}
	return read
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
		a.DB.Select("id, kind, chat_id, content, created_at, last_used_at").Where("id IN ?", res.IDs).Order("created_at").Find(&rows)
		for _, r := range rows {
			out.Memories = append(out.Memories, memoryProto(r))
		}
	}
	return connect.NewResponse(out), nil
}
