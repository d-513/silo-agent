package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/run"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/lang"
	"silo.agent/internal/llm"
	"silo.agent/internal/textx"
)

// inboxMsg is a user message injected into a live run. id, at and zone are its
// persisted event: the run moves the event to where it reads the message, and
// stamps the message with the time and zone replay will stamp it with.
type inboxMsg struct {
	text string
	ex   run.Extra
	id   string
	at   time.Time
	zone string
}

// StartRun records a run and launches the agent loop. Callers with a live run
// for the conversation should inject instead.
func (a *App) StartRun(req run.Request) (string, error) {
	var b db.Bot
	if err := a.DB.First(&b, "id = ?", req.BotID).Error; err != nil {
		return "", fmt.Errorf("unknown bot")
	}
	if a.ownerDisabled(&b) {
		return "", errOwnerDisabled
	}
	channelID := ""
	origin := "chat"
	if req.Origin != nil && req.Origin.Channel != nil {
		channelID = req.Origin.Channel.ID
		origin = "channel"
	}
	if req.Origin != nil && req.Origin.Automation != nil {
		origin = "automation"
	}
	if req.Origin != nil && req.Origin.Subagent != nil {
		origin = "subagent"
	}
	if req.Report != nil && channelID == "" {
		origin = "subagents"
	}
	if req.Compact {
		origin = "compact"
	}
	runID := ids.New()
	run := db.Run{ID: runID, BotID: req.BotID, ChatID: req.ChatID, ChannelID: channelID, Origin: origin, Status: "running", CreatedAt: time.Now()}
	if err := a.DB.Create(&run).Error; err != nil {
		return "", err
	}
	if !req.Compact {
		// Only touch the run fields: saving the whole row here could clobber a
		// ContainerID/TokenHash that a concurrent ensureRunning just wrote.
		task := req.Text
		if req.Report != nil {
			task = "Subagents reported"
		}
		a.DB.Model(&db.Bot{}).Where("id = ?", b.ID).Updates(map[string]any{
			"last_task": task,
			"status":    "working",
		})
		// Always ensure the box: a message must start a stopped Bot, and a
		// worker session can outlive a container that was removed out of band.
		cp := b
		a.ensureRunningBg(&cp)
	}
	// Tracked before the goroutine starts, so a caller that releases the
	// conversation lock right after this already sees the run as live.
	ctx, cancel := runContext(a.cfg().Runs.Timeout())
	lr := &liveRun{cancel: cancel, botID: req.BotID, chatID: req.ChatID, inbox: make(chan inboxMsg, 32), done: make(chan struct{}), wake: make(chan string, 8), pySession: a.pySessionFor(req.ChatID)}
	if req.Origin != nil && req.Origin.Automation != nil {
		lr.pySession = runID
	}
	a.mu.Lock()
	a.runs[runID] = lr
	a.mu.Unlock()
	go a.runLoop(ctx, req, runID, lr)
	return runID, nil
}

// StartIfIdle is StartRun for callers that must not steer or stack on a live
// run: it holds the conversation lock across the check and the start.
func (a *App) StartIfIdle(req run.Request) (string, error) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if a.LiveRunID(req.BotID, req.ChatID) != "" {
		return "", run.ErrBusy
	}
	return a.StartRun(req)
}

// LiveRunID returns the active run for a conversation, if any.
func (a *App) LiveRunID(botID, chatID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, lr := range a.runs {
		if lr.botID == botID && lr.chatID == chatID {
			return id
		}
	}
	return ""
}

// inject hands a new user message to a live run for a conversation, so it is
// seen on the model's next turn instead of starting a parallel run. It reports
// whether the message was queued.
func (a *App) inject(botID, chatID, runID, text string, ex run.Extra, from string) bool {
	a.mu.Lock()
	lr := a.runs[runID]
	a.mu.Unlock()
	if lr == nil || lr.botID != botID || lr.chatID != chatID {
		return false
	}
	lr.injecting.Lock()
	defer lr.injecting.Unlock()
	if lr.closed {
		return false
	}
	id, at, zn := ids.New(), time.Now(), a.botZone(botID)
	select {
	case lr.inbox <- inboxMsg{text: text, ex: ex, id: id, at: at, zone: zn}:
		a.emitUserAt(botID, chatID, runID, id, at, zn, text, ex, from)
		a.dbSaveBotWorking(botID, text)
		lr.signal("message")
		return true
	default:
		return false
	}
}

func (a *App) dbSaveBotWorking(botID, text string) {
	a.DB.Model(&db.Bot{}).Where("id = ?", botID).Updates(map[string]any{"last_task": text, "status": "working"})
}

// drainInbox appends the messages injected since the last turn. Each one's
// event moves to this point of the run log, so the log's order is the order
// the model read things in and a later run replays the message right here.
// fold joins the first one to the last message, a compaction summary, the way
// replay folds the message after a summary into it.
func (a *App) drainInbox(lr *liveRun, msgs []llm.Message, fold bool) []llm.Message {
	lr.injecting.Lock()
	var got []inboxMsg
	for more := true; more; {
		select {
		case m := <-lr.inbox:
			got = append(got, m)
		default:
			more = false
		}
	}
	lr.injecting.Unlock()
	for _, m := range got {
		a.DB.Exec("UPDATE run_events SET seq = nextval(pg_get_serial_sequence('run_events', 'seq')) WHERE id = ?", m.id)
		text := stampUserText(userTurnText(a.kept(lr.botID, m.text), a.userMeta(lr.botID, m.ex)), m.at, m.zone)
		if n := len(msgs); fold && n > 0 && msgs[n-1].Role == llm.RoleUser {
			msgs[n-1].Text += "\n\n" + text
		} else {
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: text})
		}
		fold = false
	}
	return msgs
}

// closeInbox ends the run's inbox when it is empty, and reports whether it did.
// From then on the run is no longer the conversation's live run, so a message
// sent while it finishes starts a new one instead of being lost in an inbox
// nobody will read. A message that is already waiting keeps the run going.
func (a *App) closeInbox(lr *liveRun, runID string) bool {
	lr.injecting.Lock()
	defer lr.injecting.Unlock()
	if len(lr.inbox) > 0 {
		return false
	}
	lr.closed = true
	a.untrackRun(runID)
	return true
}

// turnResult is one streamed assistant turn: the assembled message plus any
// user-visible sections the model closed with <section_send />.
type turnResult struct {
	assistant llm.Message
	sections  []string
	tail      string
	usage     llm.Usage
}

// streamTurn drives one provider completion, emitting chunks, reasoning, and
// tool-call events, and returns the assembled assistant message.
func (a *App) streamTurn(ctx context.Context, botID, chatID, runID string, client llm.Client, req llm.Request) (turnResult, error) {
	stream, err := client.Stream(ctx, req)
	if err != nil {
		return turnResult{}, err
	}
	defer stream.Close()

	sp := &channels.Splitter{}
	var text strings.Builder
	var think strings.Builder
	var sections []string
	type tcState struct {
		call     llm.ToolCall
		notified bool
	}
	open := map[int]*tcState{}
	var order []int
	touch := func(idx int) *tcState {
		st := open[idx]
		if st == nil {
			st = &tcState{}
			open[idx] = st
			order = append(order, idx)
		}
		return st
	}
	var usage llm.Usage
	var signed []llm.ThinkingBlock
	saw := false
	pieces := &coalescer{emit: func(kind, body, tool string) { a.Emit(botID, chatID, runID, kind, body, tool) }}
	defer pieces.flush()

	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case llm.EventText:
			if ev.Text == "" {
				continue
			}
			saw = true
			text.WriteString(ev.Text)
			pieces.add("chunk", ev.Text, "")
			sections = append(sections, sp.Write(ev.Text)...)
		case llm.EventReasoning:
			if ev.Text == "" {
				continue
			}
			saw = true
			think.WriteString(ev.Text)
			pieces.add("thinking_chunk", ev.Text, "")
		case llm.EventToolCallStart:
			saw = true
			st := touch(ev.Index)
			if ev.ToolCallID != "" {
				st.call.ID = ev.ToolCallID
			}
			if ev.ToolName != "" {
				st.call.Name = ev.ToolName
			}
			if st.call.Name != "" && !st.notified {
				pieces.flush()
				a.Emit(botID, chatID, runID, "tool", "", st.call.Name)
				a.setStep(runID, st.call.Name)
				st.notified = true
			}
		case llm.EventToolCallDelta:
			saw = true
			st := touch(ev.Index)
			st.call.Arguments += ev.Text
			pieces.add("tool_args_chunk", ev.Text, st.call.Name)
		case llm.EventThinkingBlock:
			if ev.Block != nil {
				signed = append(signed, *ev.Block)
			}
		case llm.EventUsage:
			usage = ev.Usage
		}
	}
	pieces.flush()
	if err := stream.Err(); err != nil {
		return turnResult{}, err
	}
	if think.Len() > 0 {
		a.Emit(botID, chatID, runID, "thinking", think.String(), "")
	}
	if !saw {
		return turnResult{}, fmt.Errorf("empty completion")
	}
	// Signed reasoning rides on the in-run turn only (never persisted): the
	// provider hands it back to the same model while the tool loop goes on.
	assistant := llm.Message{Role: llm.RoleAssistant, Text: text.String(), Thinking: signed}
	if len(signed) > 0 {
		assistant.ThinkingModel = req.Model
	}
	for _, idx := range order {
		c := open[idx].call
		if c.ID == "" {
			c.ID = "call_" + strconv.Itoa(idx)
		}
		assistant.ToolCalls = append(assistant.ToolCalls, c)
	}
	return turnResult{assistant: assistant, sections: sections, tail: sp.Flush(), usage: usage}, nil
}

// coalescer gathers a turn's streamed pieces of one kind into fewer events:
// each event is a row in the run log, and a provider sends a piece per token.
// A piece waits at most coalesceDelay, so the thread still reads live.
type coalescer struct {
	emit  func(kind, body, tool string)
	mu    sync.Mutex
	kind  string
	tool  string
	buf   strings.Builder
	timer *time.Timer
}

const (
	coalesceDelay = 60 * time.Millisecond
	coalesceMax   = 4096
)

func (c *coalescer) add(kind, body, tool string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf.Len() > 0 && (kind != c.kind || tool != c.tool) {
		c.flushLocked()
	}
	c.kind, c.tool = kind, tool
	c.buf.WriteString(body)
	if c.buf.Len() >= coalesceMax {
		c.flushLocked()
	} else if c.timer == nil {
		c.timer = time.AfterFunc(coalesceDelay, c.flush)
	}
}

// flush emits what is gathered. Called before any other event of the turn so
// the log keeps its order.
func (c *coalescer) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked()
}

func (c *coalescer) flushLocked() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.buf.Len() == 0 {
		return
	}
	body := c.buf.String()
	c.buf.Reset()
	c.emit(c.kind, body, c.tool)
}

func (a *App) runLoop(ctx context.Context, req run.Request, runID string, lr *liveRun) {
	botID, chatID, userText, atts, origin := req.BotID, req.ChatID, req.Text, req.Extra.Files, req.Origin
	defer lr.cancel()
	defer close(lr.done)
	defer a.runExited(runID)

	modelID := a.Models.Resolve(botID, chatID)
	client, provider, model, err := a.Models.Observed(modelID, botID, "chat")
	if err != nil {
		a.Emit(botID, chatID, runID, "error", err.Error(), "")
		a.finish(botID, chatID, runID, "error")
		return
	}
	settings := a.cfg().ProviderSettings(provider)
	rt := a.runToolsFor(botID, origin)
	tools := rt.list
	window := a.contextWindow(ctx, modelID)

	switch {
	case req.Report != nil:
		a.emitReport(botID, chatID, runID, req.Report)
		a.bumpChat(chatID)
	case !req.Compact:
		a.emitUser(botID, chatID, runID, userText, req.Extra, req.From)
		a.bumpChat(chatID)
		title := userText
		if strings.TrimSpace(title) == "" && len(atts) > 0 {
			title = "attached " + atts[0].GetName()
		}
		if origin == nil || origin.Subagent == nil {
			go a.nameChat(botID, chatID, runID, title)
		}
	}

	var past replayed
	if origin != nil && origin.Automation != nil {
		// Each firing is independent: only this run is the model's history.
		past = a.contextFromRuns([]db.Run{{ID: runID}})
	} else {
		past = a.contextFromDB(chatID)
	}
	// From here the history only grows: what a turn adds is recorded so that
	// replaying the log gives these same messages (replayEvents), and nothing
	// already sent is edited. A provider's prompt cache holds for exactly as
	// long as that is true. The two deliberate exceptions, clearing stale tool
	// results and compaction, rewrite it once and record that they did.
	msgs := past.msgs
	// lastNote is the status note the model was last told, cleared is how
	// many old tool results are blanked in msgs.
	lastNote, cleared := past.note, past.cleared
	if req.Compact {
		out, err := a.compact(ctx, botID, chatID, runID, compactManual, modelID, msgs, window, false)
		if err != nil {
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			a.Emit(botID, chatID, runID, "error", err.Error(), "")
			a.finish(botID, chatID, runID, "error")
			return
		}
		// A message sent while the summary was written continues as a normal
		// turn; otherwise the run is just the compaction.
		msgs = a.drainInbox(lr, out, true)
		lastNote, cleared = "", 0
		if len(msgs) == len(out) {
			a.finish(botID, chatID, runID, "done")
			return
		}
		userText = msgs[len(msgs)-1].Text
	}
	var lastLook string
	// seen counts identical read/grep calls this run so a stuck loop does not
	// re-send content the model already has. Mutating tools clear it.
	seen := map[string]int{}
	// lastTotal is the provider-reported size of the previous turn (input +
	// output) and lastLen the history length it covered; the next request is
	// that plus an estimate of what was appended since. Zero means no turn yet.
	lastTotal, lastLen := 0, 0
	// turned is set once this run has had a model turn: a compaction after
	// that is in the middle of the task.
	turned := false

	// The system prompt is built once: a run that edits its own SOUL or
	// memory is told so by the tool's result, and the next run reads the new
	// prompt. Rebuilding it per turn would re-bill the whole conversation.
	system := a.buildSystemBlocks(botID, origin)
	for {
		msgs = a.drainInbox(lr, msgs, false)
		if a.stopped(ctx, botID, chatID, runID) {
			return
		}
		// Re-resolve each turn so a switch_model tool call takes effect on the
		// next model call without restarting the run.
		if m := a.Models.Resolve(botID, chatID); m != modelID {
			if c, pr, mo, e := a.Models.Observed(m, botID, "chat"); e == nil {
				modelID, client, provider, model = m, c, pr, mo
				settings = a.cfg().ProviderSettings(pr)
				window = a.contextWindow(ctx, modelID)
			}
		}
		base := estimateBase(system, tools)
		used := base + estimateMessages(msgs)
		// scale is how the provider's count of the last turn compares with
		// the estimate of the same content (1 before the first turn).
		scale := 1.0
		if lastTotal > 0 && lastLen <= len(msgs) {
			scale = float64(lastTotal) / float64(max(base+estimateMessages(msgs[:lastLen]), 1))
			used = lastTotal + estimateMessages(msgs[lastLen:])
		}
		// compactNow replaces the history with a summary; mid-task it tells
		// the model to carry on. It reports whether the history was replaced.
		compactNow := func() bool {
			out, err := a.compact(ctx, botID, chatID, runID, compactAuto, modelID, msgs, window, turned)
			if err != nil {
				if ctx.Err() == nil {
					a.Emit(botID, chatID, runID, "error", err.Error(), "")
				}
				return false
			}
			msgs = out
			lastTotal, lastLen = 0, 0
			// The summary starts a new context: the status is told again.
			lastNote, cleared = "", 0
			return true
		}
		if needsCompaction(used, base, window, a.cfg().Context.Threshold()) {
			// Clearing old tool output is tried first: it costs no model call
			// and keeps the conversation itself.
			var freed int
			if msgs, cleared, freed = a.clearStale(runID, msgs, cleared, used, base, window, scale); freed > 0 {
				lastTotal = max(lastTotal-freed, 0)
			} else {
				compactNow()
			}
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
		}
		// The taskboard and subagent status are told when they change, as a
		// turn of their own that stays in the history: the model reads the
		// newest one, and nothing before it is rewritten.
		if note := a.turnNote(chatID, origin); note != "" && note != lastNote {
			a.record(runID, noteKind, note, "", "")
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: note})
			lastNote = note
		}
		turnReq := llm.Request{
			Model:    model,
			System:   system,
			Messages: msgs,
			Tools:    tools,
			Cache:    models.CachePolicy(settings, botID),
			// Read each turn, like the model, so a change in the composer
			// applies from the next model call.
			Thinking: a.Models.ChatThinking(ctx, chatID, modelID),
		}
		res, err := a.streamTurn(ctx, botID, chatID, runID, client, turnReq)
		// The estimate can be off: a provider that refuses the request as too
		// long gets one compaction and one retry.
		if err != nil && isContextOverflow(err) && len(msgs) > 1 && ctx.Err() == nil {
			if compactNow() {
				turnReq.Messages = msgs
				res, err = a.streamTurn(ctx, botID, chatID, runID, client, turnReq)
			}
		}
		if err != nil {
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			a.Emit(botID, chatID, runID, "error", err.Error(), "")
			a.finish(botID, chatID, runID, "error")
			return
		}
		if u := res.usage; u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			a.emitUsage(botID, chatID, runID, u, window)
			lastTotal = u.InputTokens + u.OutputTokens
		} else {
			// No count for this turn: an older one no longer describes the
			// history, so the next check falls back to the estimate.
			lastTotal = 0
		}
		if len(res.assistant.ToolCalls) == 0 {
			for _, sec := range res.sections {
				a.emitDelivered(botID, chatID, runID, "section", sec, origin)
			}
			if res.tail != "" {
				kind := "section"
				if len(res.sections) == 0 {
					kind = "assistant"
				}
				a.emitDelivered(botID, chatID, runID, kind, res.tail, origin)
			}
			if a.closeInbox(lr, runID) {
				a.finish(botID, chatID, runID, "done")
				return
			}
			// A message came in while the reply was written: answer it in
			// this run, with the reply as replay will read it back.
			for _, t := range append(res.sections, res.tail) {
				if t = a.kept(botID, t); strings.TrimSpace(t) != "" {
					msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: t})
				}
			}
			lastLen = len(msgs)
			turned = true
			continue
		}
		// A tool-call turn: send any section the model explicitly closed so the
		// human gets a progress note, but keep it out of replayed history.
		for _, sec := range res.sections {
			a.emitDelivered(botID, chatID, runID, "section_live", sec, origin)
		}
		// The turn is kept as the log stores it (secrets masked), so the next
		// run replays these very bytes; the calls run with what the model wrote.
		turn := res.assistant
		turn.Text = a.kept(botID, turn.Text)
		turn.ToolCalls = make([]llm.ToolCall, len(res.assistant.ToolCalls))
		meta := turnMeta{}
		for i, tc := range res.assistant.ToolCalls {
			tc.Arguments = a.kept(botID, tc.Arguments)
			turn.ToolCalls[i] = tc
			meta.Calls = append(meta.Calls, turnCall{ID: tc.ID, Name: tc.Name, Args: tc.Arguments})
		}
		rawMeta, _ := json.Marshal(meta)
		a.record(runID, turnKind, turn.Text, "", string(rawMeta))
		msgs = append(msgs, turn)
		lastLen = len(msgs)
		turned = true
		// Pixels go after the turn's last result, where replay puts their
		// seen notes: a user turn between two results breaks the pairing.
		var shown []llm.Message
		for _, tc := range res.assistant.ToolCalls {
			def := rt.byName[tc.Name]
			if def == nil {
				// Not offered to this run: the engine says why (a lead's
				// tool, a feature that is off) instead of "unknown tool".
				def = a.toolDef(botID, tc.Name)
			}
			if def != nil && def.Writes {
				clear(seen)
			}
			if def != nil && def.RepeatGuard {
				key := tc.Name + "\x00" + tc.Arguments
				seen[key]++
				if seen[key] >= 3 {
					out := "unchanged since your last identical " + tc.Name + " — use the result you already have"
					a.Emit(botID, chatID, runID, "tool_result", out, tc.Name)
					msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Text: out})
					continue
				}
			}
			out, img, err := a.execTool(ctx, def, botID, chatID, runID, tc.Name, tc.Arguments, isSubagent(origin))
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			if err != nil {
				out = "error: " + err.Error()
			}
			// What the output filter has to say about results the tool's own
			// code received goes first, where the cap cannot cut it.
			out = a.takeNotices(botID, runID) + out
			out = a.kept(botID, textx.Cap(a.Mask(botID).Apply(out), 12000))
			sameLook := img != "" && img == lastLook && isLookTool(tc.Name)
			if sameLook {
				out += " (screen unchanged since the last look)"
			}
			a.Emit(botID, chatID, runID, "tool_result", out, tc.Name)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Text: out})
			if img != "" && !sameLook {
				itype, vimg := visionImage(tc.Name, img)
				// later is what this turn reads as once its pixels are no
				// longer sent: a look is dropped by the next one, a presented
				// image by the end of the run.
				later := itype
				if isLookTool(tc.Name) {
					pruneLookImages(msgs)
					pruneLookImages(shown)
					lastLook = img
					later = lookOmitted
				}
				a.record(runID, seenKind, later, tc.Name, "")
				shown = append(shown, llm.Message{Role: llm.RoleUser, Text: itype, Images: []llm.Image{vimg}})
			}
			if origin != nil && origin.Channel != nil && err == nil {
				a.deliverTool(ctx, botID, chatID, runID, origin, tc.Name, tc.Arguments, img)
			}
		}
		msgs = append(msgs, shown...)
		// A tool handed the conversation to the human: their answer starts
		// the next run, unless a message is already waiting for this one.
		if a.turnEnded(runID) && a.closeInbox(lr, runID) {
			a.finish(botID, chatID, runID, "done")
			return
		}
	}
}

func (a *App) bumpChat(chatID string) {
	a.DB.Model(&db.Chat{}).Where("id = ?", chatID).Update("updated_at", time.Now())
}

// botLanguage is the language a Bot is set to write in ("" when it has none).
func (a *App) botLanguage(botID string) string {
	var b db.Bot
	a.DB.Select("language").Where("id = ?", botID).Limit(1).Find(&b)
	return b.Language
}

func (a *App) nameChat(botID, chatID, runID, userText string) {
	var c db.Chat
	if a.DB.First(&c, "id = ?", chatID).Error != nil || !chats.Untitled(c.Title) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	snippet := userText
	if len(snippet) > 800 {
		snippet = textx.TruncateUTF8(snippet, 800)
	}
	client, provider, model, err := a.Models.Observed(a.Models.Title(botID, chatID), botID, "title")
	if err != nil {
		log.Printf("name chat %s: %v", chatID, err)
		return
	}
	res, err := client.Complete(ctx, llm.Request{
		Model:    model,
		System:   []llm.SystemBlock{{Text: "Reply with only a 2-6 word chat title for the user's message. Capture intent, not a quote. No quotes, no punctuation, no explanation. " + lang.Title(a.botLanguage(botID))}},
		Messages: []llm.Message{{Role: llm.RoleUser, Text: snippet}},
		Cache:    models.CachePolicy(a.cfg().ProviderSettings(provider), botID),
	})
	if err != nil {
		log.Printf("name chat %s: %v", chatID, err)
		return
	}
	title := cleanTitle(res.Text)
	if title == "" {
		log.Printf("name chat %s: empty title from model", chatID)
		return
	}
	if !a.applyGeneratedTitle(chatID, title) {
		return
	}
	a.Emit(botID, chatID, runID, "chat_title", title, "")
}

func (a *App) stopped(ctx context.Context, botID, chatID, runID string) bool {
	if ctx.Err() == nil {
		return false
	}
	a.Emit(botID, chatID, runID, "assistant", "Stopped.", "")
	a.finish(botID, chatID, runID, "stopped")
	return true
}

func (a *App) finish(botID, chatID, runID, st string) {
	a.DB.Model(&db.Run{}).Where("id = ?", runID).Update("status", st)
	a.untrackRun(runID)
	a.dropNotices(runID)
	a.recomputeStatus(botID)
	a.Emit(botID, chatID, runID, "done", st, "")
	// The closing snapshot is a worker round trip; nothing waits for it.
	go a.Changes.End(botID, runID)
	a.afterRun(botID, chatID, runID, st)
}

// StartOrInject serializes the decision to either steer a live run or start a
// new one. This is the single entry point for chats and channels.
func (a *App) StartOrInject(botID, chatID, text string, ex run.Extra, origin *run.Origin) (string, error) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	return a.startOrInjectLocked(botID, chatID, text, ex, origin)
}

// startOrInjectLocked is startOrInject with convMu already held, so callers can
// truncate history and start the replacement run atomically.
func (a *App) startOrInjectLocked(botID, chatID, text string, ex run.Extra, origin *run.Origin) (string, error) {
	if runID := a.LiveRunID(botID, chatID); runID != "" {
		if a.inject(botID, chatID, runID, text, ex, "") {
			return runID, nil
		}
		// Refused with the run still live: its inbox is full. A second run
		// beside it would interleave the conversation.
		if a.LiveRunID(botID, chatID) != "" {
			return "", run.ErrBusy
		}
	}
	return a.StartRun(run.Request{BotID: botID, ChatID: chatID, Text: text, Extra: ex, Origin: origin})
}
