package app

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/run"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/textx"
)

type inboxMsg struct {
	text string
	atts []*v1.Attachment
}

// StartRun records a run and launches the agent loop. Callers with a live run
// for the conversation should inject instead.
func (a *App) StartRun(req run.Request) (string, error) {
	var b db.Bot
	if err := a.DB.First(&b, "id = ?", req.BotID).Error; err != nil {
		return "", fmt.Errorf("unknown bot")
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
	inbox := make(chan inboxMsg, 32)
	done := make(chan struct{})
	go a.runLoop(req, runID, inbox, done)
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
func (a *App) inject(botID, chatID, runID, text string, atts []*v1.Attachment, from string) bool {
	a.mu.Lock()
	lr := a.runs[runID]
	a.mu.Unlock()
	if lr == nil || lr.botID != botID || lr.chatID != chatID {
		return false
	}
	select {
	case lr.inbox <- inboxMsg{text: text, atts: atts}:
		a.emitUser(botID, chatID, runID, text, atts, from)
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

func drainInbox(msgs []llm.Message, inbox chan inboxMsg) []llm.Message {
	if inbox == nil {
		return msgs
	}
	for {
		select {
		case m := <-inbox:
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: stampUserText(userTextWithAttachments(m.text, m.atts), time.Now())})
		default:
			return msgs
		}
	}
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

	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case llm.EventText:
			if ev.Text == "" {
				continue
			}
			saw = true
			text.WriteString(ev.Text)
			a.emit(botID, chatID, runID, "chunk", ev.Text, "")
			sections = append(sections, sp.Write(ev.Text)...)
		case llm.EventReasoning:
			if ev.Text == "" {
				continue
			}
			saw = true
			think.WriteString(ev.Text)
			a.emit(botID, chatID, runID, "thinking_chunk", ev.Text, "")
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
				a.emit(botID, chatID, runID, "tool", "", st.call.Name)
				st.notified = true
			}
		case llm.EventToolCallDelta:
			saw = true
			st := touch(ev.Index)
			st.call.Arguments += ev.Text
			a.emit(botID, chatID, runID, "tool_args_chunk", ev.Text, st.call.Name)
		case llm.EventThinkingBlock:
			if ev.Block != nil {
				signed = append(signed, *ev.Block)
			}
		case llm.EventUsage:
			usage = ev.Usage
		}
	}
	if err := stream.Err(); err != nil {
		return turnResult{}, err
	}
	if think.Len() > 0 {
		a.emit(botID, chatID, runID, "thinking", think.String(), "")
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

func (a *App) runLoop(req run.Request, runID string, inbox chan inboxMsg, done chan struct{}) {
	botID, chatID, userText, atts, origin := req.BotID, req.ChatID, req.Text, req.Atts, req.Origin
	ctx, cancel := runContext(a.cfg().Runs.Timeout())
	defer cancel()
	a.trackRun(botID, chatID, runID, cancel, inbox, done)
	defer close(done)
	defer a.untrackRun(runID)

	modelID := a.Models.Resolve(botID, chatID)
	client, provider, model, err := a.Models.Observed(modelID, botID, "chat")
	if err != nil {
		a.emit(botID, chatID, runID, "error", err.Error(), "")
		a.finish(botID, chatID, runID, "error")
		return
	}
	settings := a.cfg().ProviderSettings(provider)
	tools := a.toolsFor(botID, origin)
	window := a.contextWindow(ctx, modelID)

	switch {
	case req.Report != nil:
		a.emitReport(botID, chatID, runID, req.Report)
		a.bumpChat(chatID)
	case !req.Compact:
		a.emitUser(botID, chatID, runID, userText, atts, req.From)
		a.bumpChat(chatID)
		title := userText
		if strings.TrimSpace(title) == "" && len(atts) > 0 {
			title = "attached " + atts[0].GetName()
		}
		if origin == nil || origin.Subagent == nil {
			go a.nameChat(botID, chatID, runID, title)
		}
	}

	var msgs []llm.Message
	if origin != nil && origin.Automation != nil {
		// Each firing is independent: only this run is the model's history.
		msgs = a.historyFromRuns([]db.Run{{ID: runID}})
	} else {
		msgs = a.historyFromDB(chatID)
	}
	if req.Compact {
		out, err := a.compact(ctx, botID, chatID, runID, compactManual, modelID, msgs, window)
		if err != nil {
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			a.emit(botID, chatID, runID, "error", err.Error(), "")
			a.finish(botID, chatID, runID, "error")
			return
		}
		// A message sent while the summary was written continues as a normal
		// turn; otherwise the run is just the compaction.
		msgs = drainInbox(out, inbox)
		if len(msgs) == len(out) {
			a.finish(botID, chatID, runID, "done")
			return
		}
		userText = msgs[len(msgs)-1].Text
	}
	if note := a.Memory.AutoRecall(ctx, botID, userText); note != "" {
		if origin == nil {
			origin = &run.Origin{}
		}
		origin.Recall = note
	}
	var lastLook string
	// seen counts identical read/grep calls this run so a stuck loop does not
	// re-send content the model already has. Mutating tools clear it.
	seen := map[string]int{}
	// lastTotal is the provider-reported size of the previous turn (input +
	// output) and lastLen the history length it covered; the next request is
	// that plus an estimate of what was appended since. Zero means no turn yet.
	lastTotal, lastLen := 0, 0

	for {
		msgs = drainInbox(msgs, inbox)
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
		system := a.buildSystemBlocks(botID, origin)
		base := estimateBase(system, tools)
		used := base + estimateMessages(msgs)
		if lastTotal > 0 && lastLen <= len(msgs) {
			used = lastTotal + estimateMessages(msgs[lastLen:])
		}
		// compactNow replaces the history with a summary; mid-task it tells
		// the model to carry on. It reports whether the history was replaced.
		compactNow := func() bool {
			out, err := a.compact(ctx, botID, chatID, runID, compactAuto, modelID, msgs, window)
			if err != nil {
				if ctx.Err() == nil {
					a.emit(botID, chatID, runID, "error", err.Error(), "")
				}
				return false
			}
			if n := len(msgs); n > 0 && msgs[n-1].Role != llm.RoleUser {
				out[0].Text += compactionContinue
			}
			msgs = out
			lastTotal, lastLen = 0, 0
			return true
		}
		if needsCompaction(used, base, window, a.cfg().Context.Threshold()) {
			compactNow()
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
		}
		// The taskboard and subagent status ride on the request's last message
		// only: the model always sees the latest version, history never keeps
		// the stale ones, and every cache breakpoint stays in front of it.
		note := a.turnNote(chatID, origin)
		turnReq := llm.Request{
			Model:    model,
			System:   system,
			Messages: withTurnNote(msgs, note),
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
				turnReq.Messages = withTurnNote(msgs, note)
				res, err = a.streamTurn(ctx, botID, chatID, runID, client, turnReq)
			}
		}
		if err != nil {
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			a.emit(botID, chatID, runID, "error", err.Error(), "")
			a.finish(botID, chatID, runID, "error")
			return
		}
		if u := res.usage; u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			a.emitUsage(botID, chatID, runID, u, window)
			lastTotal = u.InputTokens + u.OutputTokens
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
			a.finish(botID, chatID, runID, "done")
			return
		}
		// A tool-call turn: send any section the model explicitly closed so the
		// human gets a progress note, but keep it out of replayed history.
		for _, sec := range res.sections {
			a.emitDelivered(botID, chatID, runID, "section_live", sec, origin)
		}
		msgs = append(msgs, res.assistant)
		lastLen = len(msgs)
		for _, tc := range res.assistant.ToolCalls {
			if tc.Name == "write" || tc.Name == "patch" || tc.Name == "delete" || tc.Name == "terminal" || tc.Name == "exec_python" {
				clear(seen)
			}
			if tc.Name == "read" || tc.Name == "grep" {
				key := tc.Name + "\x00" + tc.Arguments
				seen[key]++
				if seen[key] >= 3 {
					out := "unchanged since your last identical " + tc.Name + " — use the result you already have"
					a.emit(botID, chatID, runID, "tool_result", out, tc.Name)
					msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Text: out})
					continue
				}
			}
			out, img, err := a.execTool(ctx, botID, chatID, runID, tc.Name, tc.Arguments)
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			if err != nil {
				out = "error: " + err.Error()
			}
			out = a.Mask(botID).Apply(out)
			out = textx.Cap(out, 12000)
			a.emit(botID, chatID, runID, "tool_result", out, tc.Name)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Text: out})
			if img != "" {
				if img == lastLook && isLookTool(tc.Name) {
					msgs[len(msgs)-1].Text += " (screen unchanged since the last look)"
				} else {
					itype, vimg := visionImage(tc.Name, img)
					if isLookTool(tc.Name) {
						pruneLookImages(msgs)
						lastLook = img
					}
					msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: itype, Images: []llm.Image{vimg}})
				}
			}
			if origin != nil && origin.Channel != nil && err == nil {
				a.deliverTool(ctx, botID, chatID, runID, origin, tc.Name, tc.Arguments, img)
			}
		}
	}
}

func (a *App) bumpChat(chatID string) {
	a.DB.Model(&db.Chat{}).Where("id = ?", chatID).Update("updated_at", time.Now())
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
		System:   []llm.SystemBlock{{Text: "Reply with only a 2-6 word chat title for the user's message. Capture intent, not a quote. No quotes, no punctuation, no explanation."}},
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
	a.emit(botID, chatID, runID, "chat_title", title, "")
}

// waitWorker blocks until the Bot's worker connects, so a message that arrives
// while the machine is starting still gets its tools. Bounded; callers fail the
// tool call if it never comes up.
func (a *App) waitWorker(ctx context.Context, botID string, d time.Duration) bool {
	if a.Hub.Connected(botID) {
		return true
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return a.Hub.Connected(botID)
		case <-time.After(250 * time.Millisecond):
		}
		if a.Hub.Connected(botID) {
			return true
		}
	}
	return a.Hub.Connected(botID)
}

func (a *App) stopped(ctx context.Context, botID, chatID, runID string) bool {
	if ctx.Err() == nil {
		return false
	}
	a.emit(botID, chatID, runID, "assistant", "Stopped.", "")
	a.finish(botID, chatID, runID, "stopped")
	return true
}

func (a *App) finish(botID, chatID, runID, st string) {
	a.DB.Model(&db.Run{}).Where("id = ?", runID).Update("status", st)
	a.untrackRun(runID)
	a.recomputeStatus(botID)
	a.emit(botID, chatID, runID, "done", st, "")
	a.afterRun(botID, chatID, runID, st)
}

// StartOrInject serializes the decision to either steer a live run or start a
// new one. This is the single entry point for chats and channels.
func (a *App) StartOrInject(botID, chatID, text string, atts []*v1.Attachment, origin *run.Origin) (string, error) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	return a.startOrInjectLocked(botID, chatID, text, atts, origin)
}

// startOrInjectLocked is startOrInject with convMu already held, so callers can
// truncate history and start the replacement run atomically.
func (a *App) startOrInjectLocked(botID, chatID, text string, atts []*v1.Attachment, origin *run.Origin) (string, error) {
	if runID := a.LiveRunID(botID, chatID); runID != "" {
		if a.inject(botID, chatID, runID, text, atts, "") {
			return runID, nil
		}
	}
	return a.StartRun(run.Request{BotID: botID, ChatID: chatID, Text: text, Atts: atts, Origin: origin})
}
