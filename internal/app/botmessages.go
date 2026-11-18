package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/filter"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// Bot messages are one Bot handing work to another of its Workspace
// (message_bot). The request is answered in the receiving Bot's conversation
// for the sender (Chat.PeerBotID: one per sending Bot and person), by an
// ordinary run with the receiver's own prompt, tools, rules and machine. When
// that conversation has nothing left in hand, its last reply goes back and
// wakes the sending chat the way a subagent's result does (scheduleWake).
//
// A message carries the authority of the person the sending run works for:
// they must be on the receiving Bot, and the receiving run is theirs. A run
// that works for nobody (a channel, the Bot's own chat) reaches only Bots
// managed by everyone who manages the sender. Chains go at most
// botMessageMaxHops Bots deep and never come back round.

const (
	botMessageMaxHops  = 3
	botMessageMaxLive  = 8
	botMessageTextMax  = 20000
	botMessageFilesMax = 10
	botReplyFilesMax   = 10
	// botFileTimeout bounds copying one message's files across.
	botFileTimeout = 3 * time.Minute
)

// --- who may message whom ---

// managersOf is the ids of the Bot's managers.
func (a *App) managersOf(botID string) []string {
	var ids []string
	a.DB.Model(&db.BotAccess{}).Where("bot_id = ? AND role = ?", botID, access.RoleManage).Pluck("user_id", &ids)
	return ids
}

// botReach says why the sender may not message target for userID, nil when
// it may.
func (a *App) botReach(sender, target *db.Bot, userID string) error {
	switch {
	case target.ID == sender.ID:
		return errors.New("that is you: message another Bot")
	case target.SpaceID != sender.SpaceID:
		return errors.New("no such Bot in your Workspace")
	case !target.AcceptBotMessages:
		return fmt.Errorf("%s does not take messages from other Bots (its managers turned that off)", target.Name)
	case access.Suspended(a.DB, target.ID):
		return fmt.Errorf("%s is suspended: it has no enabled manager", target.Name)
	}
	if userID != "" {
		if access.BotRole(a.DB, target.ID, userID) == "" {
			return fmt.Errorf("%s is not shared with the person you work for here (%s)", target.Name, access.Email(a.DB, userID))
		}
		return nil
	}
	theirs := a.managersOf(target.ID)
	for _, m := range a.managersOf(sender.ID) {
		if !slices.Contains(theirs, m) {
			return fmt.Errorf("%s is not managed by everyone who manages you, and this conversation is no one person's", target.Name)
		}
	}
	return nil
}

// botHandoff is where a message from a chat stands in a chain: the person it
// is for, the human's conversation it started from, how many Bots deep the
// next one is, and the Bots it has passed through.
type botHandoff struct {
	userID string
	root   string
	hops   int
	chain  []string
}

// handoffFrom is the chain a message sent from chatID joins: a new one from a
// person's conversation, or the next link of the request this chat answers.
func (a *App) handoffFrom(bot *db.Bot, chatID string) botHandoff {
	h := botHandoff{userID: a.chatAuthor(chatID), root: chatID, hops: 1}
	if in := a.incomingMessage(chatID); in != nil {
		h.userID, h.root, h.hops = in.UserID, in.RootChatID, in.Hops+1
		h.chain = splitChain(in.Chain)
	}
	h.chain = append(h.chain, bot.ID)
	return h
}

func splitChain(s string) []string {
	var out []string
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// incomingMessage is the newest message a conversation answers, nil for one
// that is not a Bot's conversation for another.
func (a *App) incomingMessage(chatID string) *db.BotMessage {
	if chatID == "" || a.DB == nil {
		return nil
	}
	var m db.BotMessage
	a.DB.Where("to_chat_id = ?", chatID).Order("created_at desc").Limit(1).Find(&m)
	if m.ID == "" {
		return nil
	}
	return &m
}

// findBot is a Bot of the sender's Workspace by id or name.
func (a *App) findBot(sender *db.Bot, name string) (*db.Bot, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("bot required: its name (list_bots)")
	}
	var rows []db.Bot
	a.DB.Where("space_id = ? AND (id = ? OR lower(name) = lower(?))", sender.SpaceID, name, name).Find(&rows)
	switch len(rows) {
	case 0:
		return nil, fmt.Errorf("no Bot named %q in your Workspace (list_bots)", name)
	case 1:
		return &rows[0], nil
	}
	return nil, fmt.Errorf("several Bots are named %q: use its id (list_bots)", name)
}

// --- tools ---

func (a *App) botTool(ctx context.Context, c *tool.Call) (string, error) {
	if c.ChatID == "" {
		return "", errors.New("Bots can be messaged only from a conversation, which their reply wakes")
	}
	switch c.Def.Name {
	case "list_bots":
		return a.listBots(ctx, c)
	case "message_bot":
		return a.messageBot(ctx, c)
	}
	return "", fmt.Errorf("unknown tool %s", c.Def.Name)
}

func (a *App) listBots(ctx context.Context, c *tool.Call) (string, error) {
	if _, err := a.AuthorizeAction(ctx, c.Bot, c.RunID, security.Bots, "list", "{}", ""); err != nil {
		return "", err
	}
	h := a.handoffFrom(c.Bot, c.ChatID)
	var rows []db.Bot
	a.DB.Where("space_id = ? AND id <> ?", c.Bot.SpaceID, c.Bot.ID).Order("name").Find(&rows)
	type entry struct {
		Name        string `json:"name"`
		ID          string `json:"id"`
		Description string `json:"description,omitempty"`
		Reachable   bool   `json:"reachable"`
		Why         string `json:"why,omitempty"`
	}
	var out []entry
	for i := range rows {
		e := entry{Name: rows[i].Name, ID: rows[i].ID, Description: oneLine(rows[i].Description, 300), Reachable: true}
		switch err := a.botReach(c.Bot, &rows[i], h.userID); {
		case err != nil:
			e.Reachable, e.Why = false, err.Error()
		case slices.Contains(h.chain, rows[i].ID):
			e.Reachable, e.Why = false, "already part of this request"
		case h.hops > botMessageMaxHops:
			e.Reachable, e.Why = false, "this request is already as many Bots deep as it may go"
		}
		out = append(out, e)
	}
	if c.Structured() {
		b, _ := json.Marshal(map[string]any{"bots": out})
		return string(b), nil
	}
	if len(out) == 0 {
		return "No other Bots in your Workspace.", nil
	}
	var b strings.Builder
	for _, e := range out {
		fmt.Fprintf(&b, "- %s", e.Name)
		if e.Description != "" {
			b.WriteString(": " + e.Description)
		}
		if !e.Reachable {
			b.WriteString(" (cannot message: " + e.Why + ")")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// botFile is one file a message carries across.
type botFile struct {
	name string
	mime string
	data []byte
}

func (a *App) messageBot(ctx context.Context, c *tool.Call) (string, error) {
	sender := c.Bot
	var chat db.Chat
	a.DB.Where("id = ?", c.ChatID).Limit(1).Find(&chat)
	if chat.AutomationID != "" {
		return "", errors.New("an automation's run cannot be woken with a reply, so it cannot message Bots; post to the Feed or ask the owner instead")
	}
	text := strings.TrimSpace(c.Str("text"))
	if text == "" {
		return "", errors.New("text required: what you need from it")
	}
	text = textx.CapRunes(text, botMessageTextMax)
	paths := toolarg.Strings(c.Args, "files")
	if len(paths) > botMessageFilesMax {
		return "", fmt.Errorf("at most %d files per message", botMessageFilesMax)
	}
	target, err := a.findBot(sender, c.Str("bot"))
	if err != nil {
		return "", err
	}
	h := a.handoffFrom(sender, c.ChatID)
	switch {
	case h.hops > botMessageMaxHops:
		return "", fmt.Errorf("this request is already %d Bots deep, the most it may go: answer it yourself", botMessageMaxHops)
	case slices.Contains(h.chain, target.ID):
		return "", fmt.Errorf("%s is already part of this request; it cannot be asked again down the chain", target.Name)
	}
	if err := a.botReach(sender, target, h.userID); err != nil {
		return "", err
	}
	var live int64
	a.DB.Model(&db.BotMessage{}).Where("from_chat_id = ? AND status = ?", c.ChatID, "running").Count(&live)
	if live >= botMessageMaxLive {
		return "", fmt.Errorf("at most %d messages to Bots may wait for replies at once; wait for one", botMessageMaxLive)
	}
	slip, _ := json.Marshal(map[string]any{"bot": target.Name, "text": text, "files": strings.Join(paths, ", ")})
	if _, err := a.AuthorizeAction(ctx, sender, c.RunID, security.Bots, "message", string(slip), ""); err != nil {
		return "", err
	}
	// Read everything before anything is made, so a missing file costs nothing.
	files := make([]botFile, 0, len(paths))
	for _, p := range paths {
		att, err := a.Workspace.Attachment(ctx, sender.ID, workspace.Rel(p))
		if err != nil {
			return "", fmt.Errorf("file %s: %w", p, err)
		}
		files = append(files, botFile{name: att.Name, mime: att.Mime, data: att.Data})
	}
	peer, err := a.peerChat(target, sender, h.userID)
	if err != nil {
		return "", err
	}
	pathsJSON, _ := json.Marshal(paths)
	msg := db.BotMessage{
		ID: ids.New(), FromBotID: sender.ID, FromChatID: c.ChatID, FromRunID: c.RunID, RootChatID: h.root,
		ToBotID: target.ID, ToChatID: peer.ID, UserID: h.userID, Text: text, Files: string(pathsJSON),
		Hops: h.hops, Chain: strings.Join(h.chain, ","), Status: "running", Reported: true, CreatedAt: time.Now(),
	}
	if err := a.DB.Create(&msg).Error; err != nil {
		return "", err
	}
	atts, err := a.handOver(ctx, target.ID, files)
	if err != nil {
		a.DB.Delete(&db.BotMessage{}, "id = ?", msg.ID)
		return "", err
	}
	ex := run.Extra{Files: atts, Bot: &run.BotFrom{Name: sender.Name, For: access.Email(a.DB, h.userID)}}
	if err := a.deliverBotMessage(&msg, target.ID, peer.ID, text, ex); err != nil {
		a.DB.Delete(&db.BotMessage{}, "id = ?", msg.ID)
		return "", err
	}
	a.pingLead(sender.ID, c.ChatID, "subagents")
	return fmt.Sprintf("sent to %s. It works on it in its own conversation for you. Unless you have your own work left, end your turn now: you are woken with its reply.", target.Name), nil
}

// deliverBotMessage injects the message into the conversation's live run, or
// starts one.
func (a *App) deliverBotMessage(msg *db.BotMessage, botID, chatID, text string, ex run.Extra) error {
	defer a.lockConv(chatID)()
	if runID := a.LiveRunID(botID, chatID); runID != "" {
		if a.inject(botID, chatID, runID, text, ex, run.FromBot) {
			return nil
		}
		if a.LiveRunID(botID, chatID) != "" {
			return errors.New("that Bot has too many unread messages from you; wait for its reply")
		}
	}
	_, err := a.StartRun(run.Request{BotID: botID, ChatID: chatID, Text: text, Extra: ex, From: run.FromBot, Origin: &run.Origin{Peer: msg}})
	return err
}

// peerChat is the receiving Bot's conversation for the sender and person,
// made on first use. It starts closed: it is not one of its people's tabs.
func (a *App) peerChat(target, sender *db.Bot, userID string) (*db.Chat, error) {
	a.peerMu.Lock()
	defer a.peerMu.Unlock()
	var c db.Chat
	a.DB.Where("bot_id = ? AND peer_bot_id = ? AND user_id = ?", target.ID, sender.ID, userID).Limit(1).Find(&c)
	if c.ID != "" {
		return &c, nil
	}
	now := time.Now()
	c = db.Chat{ID: ids.New(), BotID: target.ID, PeerBotID: sender.ID, UserID: userID, Title: textx.ClipRunes("From "+sender.Name, 80), Hidden: true, CreatedAt: now, UpdatedAt: now}
	if err := a.DB.Create(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// handOver puts files where uploads go on a Bot's machine (tmp/, wiped when
// the box starts, so it is brought up first).
func (a *App) handOver(ctx context.Context, botID string, files []botFile) ([]*v1.Attachment, error) {
	if len(files) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, botFileTimeout)
	defer cancel()
	if !a.Wake(ctx, botID) {
		return nil, errors.New("that Bot's machine did not come up to take the files")
	}
	var out []*v1.Attachment
	for _, f := range files {
		name := workspace.CleanName(f.name)
		rel := "tmp/" + ids.New()[:8] + "-" + name
		if err := a.Workspace.Put(ctx, botID, rel, f.data); err != nil {
			return nil, fmt.Errorf("copy %s: %w", name, err)
		}
		out = append(out, &v1.Attachment{Name: name, Path: rel, Size: int64(len(f.data)), Mime: f.mime})
	}
	return out, nil
}

// --- the reply ---

// notePeerFile remembers a file a run answering a Bot presented: it goes back
// with the reply. Scratch and what was sent in are the Bot looking, not
// answering.
func (a *App) notePeerFile(chatID, name, argsJSON string) {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	p := workspace.Rel(args.Path)
	if p == "" || workspace.IsScratch(p) || workspace.IsUpload(p) || (name != "present" && name != "artifact") {
		return
	}
	a.peerMu.Lock()
	defer a.peerMu.Unlock()
	if a.peerFiles == nil {
		a.peerFiles = map[string][]string{}
	}
	if fs := a.peerFiles[chatID]; !slices.Contains(fs, p) && len(fs) < botReplyFilesMax {
		a.peerFiles[chatID] = append(fs, p)
	}
}

func (a *App) takePeerFiles(chatID string) []string {
	a.peerMu.Lock()
	defer a.peerMu.Unlock()
	fs := a.peerFiles[chatID]
	delete(a.peerFiles, chatID)
	return fs
}

// replying is a message whose reply is on its way back (returnReply): it is
// settled, but the sender has not been handed the reply yet.
const replying = "replying"

// handedOff reports whether a chat still waits on work it handed off: a
// subagent at work or a result it has not read, a Bot it messaged that has
// not answered, whose reply is on its way, or whose reply it has not read.
func (a *App) handedOff(chatID string) bool {
	for _, sa := range a.subagentsOf(chatID) {
		if a.subagentLive(&sa) || (!sa.Reported && sa.Status != "running") {
			return true
		}
	}
	var n int64
	a.DB.Model(&db.BotMessage{}).Where("from_chat_id = ? AND (status IN ? OR reported = ?)", chatID, []string{"running", replying}, false).Count(&n)
	return n > 0
}

// peerRunEnded settles the messages a run answered once its conversation has
// nothing left in hand. A run that handed work off is not done: the wake that
// brings it back settles them when it ends.
func (a *App) peerRunEnded(botID, chatID, runID, st string) {
	var msgs []db.BotMessage
	a.DB.Where("to_chat_id = ? AND status = ?", chatID, "running").Order("created_at").Find(&msgs)
	if len(msgs) == 0 {
		return
	}
	if live := a.LiveRunID(botID, chatID); live != "" && live != runID {
		return // a newer run answers them
	}
	if (st == "done" || st == "error") && a.handedOff(chatID) {
		return
	}
	if st == "" {
		st = "done"
	}
	result := textx.TruncateUTF8(a.runResult(runID), subagentResultMax)
	files := a.takePeerFiles(chatID)
	now := time.Now()
	for _, m := range msgs {
		// Only while still running: a stop the sender made wins. Until the
		// reply is handed over the message reads as replying, so a sender
		// that is itself answering someone does not settle without it.
		if a.DB.Model(&db.BotMessage{}).Where("id = ? AND status = ?", m.ID, "running").Updates(map[string]any{
			"status": replying, "result": result, "finished_at": &now,
		}).RowsAffected == 0 {
			continue
		}
		m.Result = result
		go a.returnReply(m, st, files)
	}
}

// returnReply hands a settled message's reply to its sender: the sender's
// output rule judges it, its files are copied across, and the sending chat
// is woken.
func (a *App) returnReply(m db.BotMessage, st string, files []string) {
	ctx, cancel := context.WithTimeout(context.Background(), botFileTimeout)
	defer cancel()
	var sender db.Bot
	if a.DB.Where("id = ?", m.FromBotID).Limit(1).Find(&sender); sender.ID == "" {
		a.DB.Model(&db.BotMessage{}).Where("id = ?", m.ID).Update("status", st)
		return
	}
	text, verdict := a.filterReply(ctx, &sender, &m)
	var copied []string
	if verdict != filter.Withheld && len(files) > 0 {
		var notes []string
		copied, notes = a.carryBack(ctx, m.ToBotID, sender.ID, files)
		if len(notes) > 0 {
			text = strings.TrimSpace(text + "\n\n" + strings.Join(notes, "\n"))
		}
	}
	filesJSON, _ := json.Marshal(copied)
	if verdict == filter.Clean {
		verdict = ""
	}
	m.Status = st
	// Only while replying: a stop the sender made meanwhile wins.
	if a.DB.Model(&db.BotMessage{}).Where("id = ? AND status = ?", m.ID, replying).Updates(map[string]any{
		"status": st, "result": text, "verdict": string(verdict), "reply_files": string(filesJSON), "reported": false,
	}).RowsAffected == 0 {
		return
	}
	if lr := a.liveRunFor(sender.ID, m.FromChatID); lr != nil {
		lr.signal("a Bot replied (end your turn to read it)")
	}
	a.pingLead(sender.ID, m.FromChatID, "subagents")
	a.scheduleWake(sender.ID, m.FromChatID)
}

// filterReply applies the sender's output rule for bots.message to the reply:
// another Bot's words are outside content, whatever that Bot read to write
// them.
func (a *App) filterReply(ctx context.Context, sender *db.Bot, m *db.BotMessage) (string, filter.Verdict) {
	text := filter.Defang(a.Mask(sender.ID).Apply(m.Result))
	if strings.TrimSpace(text) == "" || !a.cfg().Filter.Enabled {
		return text, filter.Clean
	}
	mode := a.ruleOutput(sender.ID, security.Bots, "message", security.OutCheck)
	verdict := filter.Clean
	switch mode {
	case security.OutPass:
		return text, filter.Clean
	case security.OutFlag:
		verdict = filter.Flagged
	case security.OutWithhold:
		verdict = filter.Withheld
	default:
		args, _ := json.Marshal(map[string]any{"bot": a.botName(m.ToBotID), "text": m.Text})
		verdict = a.Filter.Check(ctx, filter.Call{Bot: sender, ChatID: m.FromChatID, Connector: security.Bots, Action: "message", ArgsJSON: string(args)}, text)
	}
	if verdict == filter.Clean {
		return text, verdict
	}
	key := security.Bots + ".message"
	a.audit(sender, "", "filter", key, "output_"+string(verdict))
	if verdict == filter.Withheld {
		return filter.Placeholder(key), verdict
	}
	return filter.Frame(verdict, mode, key, text), verdict
}

// carryBack copies the reply's files from the receiver into the sender's
// uploads, and says what could not be.
func (a *App) carryBack(ctx context.Context, fromBot, toBot string, paths []string) (copied, notes []string) {
	var files []botFile
	for _, p := range paths {
		att, err := a.Workspace.Attachment(ctx, fromBot, p)
		if err != nil {
			notes = append(notes, fmt.Sprintf("[file %s could not be sent back: %v]", p, err))
			continue
		}
		files = append(files, botFile{name: att.Name, mime: att.Mime, data: att.Data})
	}
	atts, err := a.handOver(ctx, toBot, files)
	if err != nil {
		notes = append(notes, "[its files could not be copied to you: "+err.Error()+"]")
		return nil, notes
	}
	for _, at := range atts {
		copied = append(copied, at.GetPath())
	}
	return copied, notes
}

func (a *App) botName(botID string) string {
	var b db.Bot
	a.DB.Select("name").Where("id = ?", botID).Limit(1).Find(&b)
	return b.Name
}

// unreadReplies are the settled messages a chat sent whose replies it has
// not read, oldest first.
func (a *App) unreadReplies(chatID string) []db.BotMessage {
	var rows []db.BotMessage
	a.DB.Where("from_chat_id = ? AND reported = ? AND status <> ?", chatID, false, "running").Order("finished_at").Find(&rows)
	return rows
}

// botReplySection is one message's reply as the sender's wake report shows it.
func (a *App) botReplySection(m *db.BotMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Bot %s — %s\n", a.botName(m.ToBotID), m.Status)
	res := strings.TrimSpace(m.Result)
	if res == "" {
		res = "(no reply)"
	}
	if len(res) > subagentReportMax {
		res = textx.TruncateUTF8(res, subagentReportMax) + "\n…truncated"
	}
	b.WriteString(res + "\n")
	var files []string
	_ = json.Unmarshal([]byte(m.ReplyFiles), &files)
	if len(files) > 0 {
		b.WriteString("Files it sent you: " + strings.Join(files, ", ") + "\n")
	}
	return b.String()
}

// --- stopping ---

// stopBotMessages withdraws what a chat is still waiting on from other Bots
// without waking it: the stop that caused it is something it knows about.
// The receiving conversation stops too unless it still answers someone else.
func (a *App) stopBotMessages(fromChatID string) {
	var rows []db.BotMessage
	a.DB.Where("from_chat_id = ? AND status IN ?", fromChatID, []string{"running", replying}).Find(&rows)
	now := time.Now()
	for _, m := range rows {
		if a.DB.Model(&db.BotMessage{}).Where("id = ? AND status IN ?", m.ID, []string{"running", replying}).
			Updates(map[string]any{"status": "stopped", "reported": true, "finished_at": &now}).RowsAffected == 0 {
			continue
		}
		if m.Status == replying {
			continue // its conversation already ended
		}
		var others int64
		a.DB.Model(&db.BotMessage{}).Where("to_chat_id = ? AND status = ?", m.ToChatID, "running").Count(&others)
		if others == 0 {
			a.stopChat(m.ToBotID, m.ToChatID)
			a.stopSubagents(m.ToBotID, m.ToChatID)
		}
	}
}

// dropBotMessages forgets what a deleted chat sent.
func (a *App) dropBotMessages(fromChatID string) {
	a.stopBotMessages(fromChatID)
	a.DB.Where("from_chat_id = ?", fromChatID).Delete(&db.BotMessage{})
}
