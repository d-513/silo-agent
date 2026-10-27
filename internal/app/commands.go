package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/command"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
)

// The run log's command events. A command that changes the conversation
// leaves one in a run of its own (origin "command"), so the thread shows
// where it happened and replay knows.
const (
	// contextClearedKind is /clear: the model's context starts after it.
	// Nothing before it is deleted; the thread keeps it above a divider.
	contextClearedKind = "context_cleared"
	// wipedKind is /wipe: Body is how many messages were deleted for good.
	wipedKind = "wiped"

	originCommand = "command"
)

func (a *App) commandDefs() []command.Def {
	return []command.Def{
		{Name: "clear", Summary: "Start the conversation's context over. Earlier messages stay visible above a divider.", Channel: true, Run: a.cmdClear},
		{Name: "wipe", Args: "[last|all]", Summary: "Delete messages for good: since the last /clear, the last exchange, or the whole chat.", Confirm: true, Run: a.cmdWipe},
		{Name: "compact", Summary: "Summarize the conversation so far to free the context.", Run: a.cmdCompact},
		{Name: "model", Args: "[model]", Summary: "Show or switch this chat's model.", Run: a.cmdModel},
		{Name: "thinking", Args: "[level]", Summary: "Show or set how hard the model thinks.", Run: a.cmdThinking},
		{Name: "stop", Summary: "Stop the reply that is running.", Channel: true, Run: a.cmdStop},
		{Name: "new", Args: "[message]", Summary: "Open a new chat, optionally starting it with a message.", Run: a.cmdNew},
		{Name: "rename", Args: "<title>", Summary: "Rename this chat.", Run: a.cmdRename},
		{Name: "remember", Summary: "Collect memories from this chat now.", Run: a.cmdRemember},
		{Name: "help", Summary: "List the commands.", Channel: true, Run: a.cmdHelp},
		{Name: "onboard", Summary: "Let the Bot interview you again: what you want, what to connect, how proactive to be.", Run: a.cmdOnboard},
	}
}

// runCommand runs a parsed command for a chat. A refusal is the human's
// answer, not a failure: it comes back as FailedPrecondition with the text.
func (a *App) runCommand(ctx context.Context, d *command.Def, c command.Call) (command.Result, error) {
	res, err := a.Slash.Run(ctx, d, c)
	if errors.Is(err, command.ErrRefused) {
		return res, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return res, err
}

// Command runs text as a command typed in a channel conversation; handled is
// false when it is a message. The notice (or the refusal) is what to send
// back over the channel.
func (a *App) Command(botID, chatID, text string) (string, bool) {
	p := a.Slash.Parse(text)
	if p.Def == nil {
		return "", false
	}
	res, err := a.Slash.Run(context.Background(), p.Def, command.Call{BotID: botID, ChatID: chatID, Args: p.Args, Surface: command.Channel})
	if err != nil {
		if errors.Is(err, command.ErrRefused) {
			return err.Error(), true
		}
		return "That did not work: " + err.Error(), true
	}
	return res.Notice, true
}

// commandEvent records a command's event in a run of its own and shows it to
// the chat's viewers.
func (a *App) commandEvent(botID, chatID, kind, body string) {
	var ch db.Chat
	a.DB.Select("id, channel_id").Where("id = ?", chatID).Limit(1).Find(&ch)
	r := db.Run{ID: ids.New(), BotID: botID, ChatID: chatID, ChannelID: ch.ChannelID, Origin: originCommand, Status: "done", CreatedAt: time.Now()}
	if a.DB.Create(&r).Error != nil {
		return
	}
	a.Emit(botID, chatID, r.ID, kind, body, "")
}

func (a *App) cmdClear(ctx context.Context, c command.Call) (command.Result, error) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if a.LiveRunID(c.BotID, c.ChatID) != "" {
		return command.Result{}, command.Refused("A reply is running. Stop it first (/stop), then /clear.")
	}
	if len(a.historyFromDB(c.ChatID)) == 0 {
		return command.Result{Notice: "The context is already empty.", Value: "empty"}, nil
	}
	// The conversation starts over: its taskboard and any helpers still
	// working for it go too. The Python interpreter follows the boundary
	// (pySessionFor). What was said is not lost: the memory collector reads
	// up to the divider like any other part of the chat.
	a.stopSubagents(c.BotID, c.ChatID)
	a.DB.Where("chat_id = ?", c.ChatID).Delete(&db.TaskItem{})
	a.commandEvent(c.BotID, c.ChatID, contextClearedKind, "")
	return command.Result{Notice: "Context cleared. The Bot starts fresh from here; its memories stay."}, nil
}

// wipeScope is which part of a chat /wipe deletes.
type wipeScope string

const (
	wipeSinceClear wipeScope = ""
	wipeLast       wipeScope = "last"
	wipeAll        wipeScope = "all"
)

func (a *App) cmdWipe(ctx context.Context, c command.Call) (command.Result, error) {
	scope := wipeScope(strings.ToLower(c.Args))
	switch scope {
	case wipeSinceClear, wipeLast, wipeAll:
	default:
		return command.Result{}, command.Refused("Use /wipe, /wipe last or /wipe all.")
	}
	ch, err := a.chatRow(c.BotID, c.ChatID)
	if err != nil {
		return command.Result{}, err
	}
	if err := writableChat(ch); err != nil {
		return command.Result{}, err
	}
	a.convMu.Lock()
	defer a.convMu.Unlock()
	events := a.orderedChatEvents(c.ChatID)
	from := wipeStart(events, scope)
	n := countMessages(events[from:])
	if n == 0 {
		return command.Result{Notice: "There is nothing to wipe.", Value: "0"}, nil
	}
	if !c.Confirmed {
		what := map[wipeScope]string{
			wipeSinceClear: "everything since the last /clear",
			wipeLast:       "the last exchange",
			wipeAll:        "the whole chat",
		}[scope]
		q := fmt.Sprintf("Delete %s (%d %s) for good? Its tool output and files shown in it go too; memories stay.", what, n, plural(n, "message", "messages"))
		if ch.ChannelID != "" {
			q += " Messages already delivered over the channel cannot be taken back."
		}
		return command.Result{Confirm: q, Value: fmt.Sprint(n)}, nil
	}
	a.StopChatLive(c.BotID, c.ChatID)
	a.stopSubagents(c.BotID, c.ChatID)
	if err := a.deleteChatEvents(events[from:]); err != nil {
		return command.Result{}, err
	}
	if scope != wipeLast {
		a.DB.Where("chat_id = ?", c.ChatID).Delete(&db.TaskItem{})
	}
	a.publishReset(c.BotID, c.ChatID)
	a.commandEvent(c.BotID, c.ChatID, wipedKind, fmt.Sprint(n))
	// Files the wiped turns showed are forgotten once nothing shows them.
	go a.Artifacts.SweepBot(context.Background(), c.BotID)
	return command.Result{Notice: fmt.Sprintf("Wiped %d %s.", n, plural(n, "message", "messages")), Value: fmt.Sprint(n)}, nil
}

// wipeStart is the index of the first event a /wipe of scope deletes.
func wipeStart(events []chatEvent, scope wipeScope) int {
	switch scope {
	case wipeAll:
		return 0
	case wipeLast:
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].ev.Kind == "user" {
				return i
			}
		}
		return len(events)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].ev.Kind == contextClearedKind {
			return i + 1
		}
	}
	return 0
}

// countMessages is how many messages (the human's and the Bot's) events hold.
func countMessages(events []chatEvent) int {
	n := 0
	for _, e := range events {
		switch e.ev.Kind {
		case "user", "assistant", "section", subagentReportKind, "feed_quote":
			n++
		}
	}
	return n
}

// deleteChatEvents deletes events for good, with the runs they leave empty
// and those runs' settled approvals (their arguments are what was said).
func (a *App) deleteChatEvents(events []chatEvent) error {
	if len(events) == 0 {
		return nil
	}
	evIDs := make([]string, 0, len(events))
	runs := map[string]bool{}
	for _, e := range events {
		evIDs = append(evIDs, e.ev.ID)
		runs[e.ev.RunID] = true
	}
	for i := 0; i < len(evIDs); i += 1000 {
		end := min(i+1000, len(evIDs))
		if err := a.DB.Where("id IN ?", evIDs[i:end]).Delete(&db.RunEvent{}).Error; err != nil {
			return err
		}
	}
	for runID := range runs {
		var n int64
		a.DB.Model(&db.RunEvent{}).Where("run_id = ?", runID).Count(&n)
		if n == 0 {
			a.DB.Where("run_id = ? AND status <> ?", runID, "pending").Delete(&db.Approval{})
			a.DB.Where("id = ?", runID).Delete(&db.Run{})
		}
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// pySessionFor is the persistent Python interpreter a chat's runs share. A
// /clear or /wipe starts a new one, so nothing defined before it lingers.
func (a *App) pySessionFor(chatID string) string {
	if a.DB == nil || chatID == "" {
		return chatID
	}
	var ev db.RunEvent
	a.DB.Select("run_events.id").Joins("JOIN runs ON runs.id = run_events.run_id").
		Where("runs.chat_id = ? AND run_events.kind IN ?", chatID, []string{contextClearedKind, wipedKind}).
		Order("run_events.seq DESC").Limit(1).Find(&ev)
	if ev.ID == "" {
		return chatID
	}
	return chatID + "/" + ev.ID
}

func (a *App) chatRow(botID, chatID string) (*db.Chat, error) {
	var ch db.Chat
	a.DB.Where("id = ? AND bot_id = ?", chatID, botID).Limit(1).Find(&ch)
	if ch.ID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("chat not found"))
	}
	return &ch, nil
}

func (a *App) cmdCompact(ctx context.Context, c command.Call) (command.Result, error) {
	runID, err := a.compactChat(c.BotID, c.ChatID)
	if err != nil {
		return command.Result{}, err
	}
	return command.Result{RunID: runID, Notice: "Compacting the conversation."}, nil
}

func (a *App) cmdModel(ctx context.Context, c command.Call) (command.Result, error) {
	ch, err := a.chatRow(c.BotID, c.ChatID)
	if err != nil {
		return command.Result{}, err
	}
	allowed := a.Models.Allowed()
	if c.Args == "" {
		cur := ch.Model
		if cur == "" {
			cur = a.Models.BotDefault(c.BotID) + " (the Bot's default)"
		}
		names := make([]string, len(allowed))
		for i, o := range allowed {
			names[i] = o.ID
		}
		return command.Result{Notice: "This chat uses " + cur + ". Allowed: " + strings.Join(names, ", ") + "."}, nil
	}
	if c.Args == "default" {
		a.DB.Model(&db.Chat{}).Where("id = ?", ch.ID).Update("model", "")
		return command.Result{Notice: "This chat now uses the Bot's default model."}, nil
	}
	ids := make([]string, len(allowed))
	for i, o := range allowed {
		ids[i] = o.ID
	}
	id, err := matchModel(c.Args, ids)
	if err != nil {
		return command.Result{}, err
	}
	a.DB.Model(&db.Chat{}).Where("id = ?", ch.ID).Update("model", id)
	return command.Result{Notice: "This chat now uses " + id + ".", Value: id}, nil
}

// matchModel finds the one allowed model a typed name means: its id, then the
// model part, then a unique piece of either.
func matchModel(q string, allowed []string) (string, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	for _, pass := range []func(id, model string) bool{
		func(id, _ string) bool { return id == q },
		func(_, model string) bool { return model == q },
		func(id, _ string) bool { return strings.Contains(id, q) },
	} {
		var hits []string
		for _, id := range allowed {
			_, model, _ := llm.Parse(id)
			if pass(strings.ToLower(id), strings.ToLower(model)) {
				hits = append(hits, id)
			}
		}
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			return "", command.Refused("\"" + q + "\" could be " + strings.Join(hits, ", ") + ". Be more specific.")
		}
	}
	return "", command.Refused("No allowed model matches \"" + q + "\". /model lists them.")
}

func (a *App) cmdThinking(ctx context.Context, c command.Call) (command.Result, error) {
	ch, err := a.chatRow(c.BotID, c.ChatID)
	if err != nil {
		return command.Result{}, err
	}
	if c.Args == "" {
		cur := ch.Thinking
		if cur == "" {
			cur = "the model's default"
		}
		return command.Result{Notice: "Thinking: " + cur + ". Levels: default, " + strings.Join(llm.ThinkingOrder, ", ") + "."}, nil
	}
	level := strings.ToLower(c.Args)
	if level == "default" {
		level = ""
	}
	if !llm.ValidThinking(level) {
		return command.Result{}, command.Refused("Unknown level \"" + c.Args + "\". Levels: default, " + strings.Join(llm.ThinkingOrder, ", ") + ".")
	}
	a.DB.Model(&db.Chat{}).Where("id = ?", ch.ID).Update("thinking", level)
	if level == "" {
		return command.Result{Notice: "Thinking: the model's default."}, nil
	}
	return command.Result{Notice: "Thinking: " + level + ".", Value: level}, nil
}

func (a *App) cmdStop(ctx context.Context, c command.Call) (command.Result, error) {
	if a.LiveRunID(c.BotID, c.ChatID) == "" {
		return command.Result{Notice: "Nothing is running.", Value: "idle"}, nil
	}
	a.stopChat(c.BotID, c.ChatID)
	a.stopSubagents(c.BotID, c.ChatID)
	return command.Result{Notice: "Stopped."}, nil
}

func (a *App) cmdNew(ctx context.Context, c command.Call) (command.Result, error) {
	if c.Args == "" {
		return command.Result{NewChat: true}, nil
	}
	// The new chat is whoever typed it: the author of the chat it was typed in.
	var from db.Chat
	a.DB.Select("user_id").Where("id = ?", c.ChatID).Limit(1).Find(&from)
	if u := access.User(ctx); u != nil {
		from.UserID = u.ID
	}
	ch := db.Chat{ID: ids.New(), BotID: c.BotID, UserID: from.UserID, Title: "New chat", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := a.DB.Create(&ch).Error; err != nil {
		return command.Result{}, err
	}
	runID, err := a.StartOrInject(c.BotID, ch.ID, c.Args, run.Extra{}, nil)
	if err != nil {
		a.DB.Delete(&db.Chat{}, "id = ?", ch.ID)
		return command.Result{}, command.Refused(err.Error())
	}
	return command.Result{ChatID: ch.ID, RunID: runID}, nil
}

func (a *App) cmdRename(ctx context.Context, c command.Call) (command.Result, error) {
	title := strings.Join(strings.Fields(c.Args), " ")
	if title == "" {
		return command.Result{}, command.Refused("Give the new title: /rename Trip to Lisbon.")
	}
	if ch, err := a.chatRow(c.BotID, c.ChatID); err != nil {
		return command.Result{}, err
	} else if ch.Main {
		return command.Result{}, command.Refused("The main chat keeps its name.")
	}
	if r := []rune(title); len(r) > 80 {
		title = string(r[:80])
	}
	a.DB.Model(&db.Chat{}).Where("id = ? AND bot_id = ?", c.ChatID, c.BotID).Updates(map[string]any{"title": title, "updated_at": time.Now()})
	a.Bus.Publish(c.BotID, &v1.RunEvent{ChatId: c.ChatID, Kind: "chat_title", Body: title})
	return command.Result{Notice: "Renamed to " + title + ".", Value: title}, nil
}

func (a *App) cmdRemember(ctx context.Context, c command.Call) (command.Result, error) {
	res, err := a.collectChat(ctx, c.ChatID)
	if errors.Is(err, errCollecting) {
		return command.Result{}, command.Refused("Memories are already being collected for this chat.")
	}
	if err != nil {
		return command.Result{}, command.Refused("Memory collection failed: " + err.Error())
	}
	if res.Saved+res.Updated+res.Forgotten == 0 {
		return command.Result{Notice: "Nothing new to remember.", Value: "0/0/0"}, nil
	}
	return command.Result{Notice: fmt.Sprintf("Memories: %d saved, %d updated, %d forgotten.", res.Saved, res.Updated, res.Forgotten), Value: fmt.Sprintf("%d/%d/%d", res.Saved, res.Updated, res.Forgotten)}, nil
}

func (a *App) cmdHelp(ctx context.Context, c command.Call) (command.Result, error) {
	var b strings.Builder
	b.WriteString("Commands:")
	for _, d := range a.Slash.List(c.Surface) {
		b.WriteString("\n/" + d.Name)
		if d.Args != "" {
			b.WriteString(" " + d.Args)
		}
		b.WriteString(" — " + d.Summary)
	}
	b.WriteString("\nStart a message with // to send it as text.")
	return command.Result{Notice: b.String()}, nil
}

// ListCommands is the commands the composer offers.
func (a *App) ListCommands(ctx context.Context, req *connect.Request[v1.ListCommandsRequest]) (*connect.Response[v1.ListCommandsResponse], error) {
	if _, err := a.bot(ctx, req.Msg.GetBotId(), access.Use); err != nil {
		return nil, err
	}
	out := &v1.ListCommandsResponse{}
	for _, d := range a.Slash.List(command.Web) {
		out.Commands = append(out.Commands, &v1.Command{Name: d.Name, Args: d.Args, Summary: d.Summary, Confirm: d.Confirm, Channel: d.Channel})
	}
	return connect.NewResponse(out), nil
}

func (a *App) cmdOnboard(ctx context.Context, c command.Call) (command.Result, error) {
	runID, err := a.startInterview(c.BotID, c.ChatID)
	if errors.Is(err, run.ErrBusy) {
		return command.Result{}, command.Refused("A reply is running. Stop it first (/stop), then /onboard.")
	}
	if err != nil {
		return command.Result{}, command.Refused(err.Error())
	}
	return command.Result{RunID: runID}, nil
}
