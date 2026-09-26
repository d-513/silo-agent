package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/security"
)

// validUTF8 replaces invalid UTF-8 bytes and drops NULs so protobuf string
// fields stay marshalable and Postgres text accepts them. Command output (a
// docx dump, terminal bytes, a masking splice) can otherwise poison a run event
// and make the whole chat unreplayable. It matches what the DB layer stores, so
// a live event and its replay are the same string.
func validUTF8(s string) string {
	return db.CleanText(s)
}

// truncateUTF8 cuts s to at most n bytes without splitting a rune. Slicing a
// Go string at an arbitrary byte offset can leave a partial multibyte rune,
// which is invalid UTF-8 and unparseable over protobuf.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// tool builds a neutral tool definition from a JSON-schema property map.
func tool(name, desc string, params map[string]any) llm.Tool {
	raw, _ := json.Marshal(params)
	return llm.Tool{Name: name, Description: desc, Parameters: raw}
}

var toolDefs = []llm.Tool{
	tool("terminal", "Run a shell command in the Bot workspace. Packages, git, and one-off shell work; use exec_python for logic and parsing.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string"},
		},
		"required": []string{"command"},
	}),
	tool("exec_python", "Run Python in the Bot. Secrets: silo_runtime.get_secret. Web: silo_runtime.web_search. Deliverables: silo_runtime.artifact(path) shows a skill dir or a file as a card — not the same as present. Programmatic GUI: silo_runtime.look/click/type_text/key/scroll (type a secret this way, not with chat type). Scratch in /workspace/bot. User-facing files in /workspace. Live clicks: look/click/type/key/scroll chat tools. chrome_page() is page screenshots, mutating displayed HTML, and automated scripts — not live clicking. Connectors are import tools.<slug>. Persist user-facing results to /workspace here, then present — do not hand them to write.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{"type": "string"},
		},
		"required": []string{"code"},
	}),
	tool("read", "Read a workspace file. Returns numbered lines as `N|content`. N is the file's absolute line number, so offset=2 starts the output at `2|` (the first line shown is line 2 of the file, not line 1). offset is 1-based. Use offset and limit to read a slice — do not dump large files. A truncated read ends with a `read offset=N for more` hint.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string"},
			"offset": map[string]any{"type": "integer", "description": "1-based start line"},
			"limit":  map[string]any{"type": "integer", "description": "max lines to return"},
		},
		"required": []string{"path"},
	}),
	tool("write", "Write a small file you compose yourself, relative to /workspace. Prefer patch for existing files. Do not copy Python or connector output here — save that from exec_python, then present.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []string{"path", "content"},
	}),
	tool("patch", "Replace exactly one occurrence of old_text with new_text. old_text must match once; if it matches several times, add surrounding lines.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":     map[string]any{"type": "string"},
			"old_text": map[string]any{"type": "string"},
			"new_text": map[string]any{"type": "string"},
		},
		"required": []string{"path", "old_text", "new_text"},
	}),
	tool("delete", "Delete a workspace file or directory (directories are removed recursively). Destructive and not undoable — use only for files the human asked you to remove, or scratch you created. Never delete the workspace root.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}),
	tool("grep", "Search workspace files. Prefer include (e.g. *.py) over a full-tree scan. Results are capped.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":  map[string]any{"type": "string"},
			"path":     map[string]any{"type": "string"},
			"include":  map[string]any{"type": "string", "description": "glob such as *.py or *.md"},
			"max_hits": map[string]any{"type": "integer"},
		},
		"required": []string{"pattern"},
	}),
	tool("soul", "Update this Bot's SOUL (identity, tone, hard rules). Already in the system prompt — do not read a file. Pass content to replace, or old_text/new_text to patch one unique snippet.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content":  map[string]any{"type": "string"},
			"old_text": map[string]any{"type": "string"},
			"new_text": map[string]any{"type": "string"},
		},
	}),
	tool("core_memory", "Update this Bot's CORE MEMORY (small, always-needed facts). Already in the system prompt. Pass append to add a line, or old_text/new_text to edit or compact. If over the cap, compact first — do not append.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"append":   map[string]any{"type": "string"},
			"old_text": map[string]any{"type": "string"},
			"new_text": map[string]any{"type": "string"},
		},
	}),
	tool("remember", "Save one durable fact to this Bot's long-term memory (searched by meaning, not always in the prompt). One fact per call; a near-duplicate updates the existing memory.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "the fact, self-contained"},
		},
		"required": []string{"content"},
	}),
	tool("recall", "Search this Bot's long-term memories by meaning. Returns the closest ones with ids and dates.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer", "description": "max memories (default 5, max 20)"},
		},
		"required": []string{"query"},
	}),
	tool("forget", "Delete one long-term memory by id (from recall).", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
		"required": []string{"id"},
	}),
	tool("list_models", "List the models this Bot may switch to, with the current chat model and the operator default. Use this before switch_model.", map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}),
	tool("switch_model", "Switch this conversation to another allowed model. model must be one of the ids from list_models. Takes effect from the next model call.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"model": map[string]any{"type": "string", "description": "provider/model id from list_models"},
		},
		"required": []string{"model"},
	}),
	tool("present", "Show a workspace file that is already on disk. Path is relative to /workspace — notes.md or bot/page.png, not /workspace/notes.md. bot/ is your scratch (you get the pixels; the human sees a collapsed row). Other paths are for the human as a folio. Images are sent to you as pixels. Only previewable types render inline in the thread (images, PDF, Markdown, CSV, JSON, code/text, DOCX, video, audio) — for anything else such as decks, workbooks, or archives use artifact so the human gets a downloadable card. Do not retype the contents.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}),
	tool("look", "Primary GUI: screenshot the 1600×900 desktop. Origin top-left. click(x,y) uses these pixels with no scale. Human sees a collapsed row; you get the pixels.", map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}),
	tool("click", "Click the desktop at screenshot pixels. Image is 1600×900. Optional button: left (default), right, double.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"x":      map[string]any{"type": "integer"},
			"y":      map[string]any{"type": "integer"},
			"button": map[string]any{"type": "string", "enum": []string{"left", "right", "double"}},
		},
		"required": []string{"x", "y"},
	}),
	tool("type", "Type Unicode into the focused window. Click a field first. For shortcuts use key (ctrl+l) — if you pass a chord here it is sent as a shortcut, not typed as letters.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}),
	tool("key", "Press a key or shortcut on the desktop. Examples: Return, Tab, Escape, BackSpace, ctrl+l, ctrl+shift+t, alt+Tab, ctrl+a. Use this for shortcuts, not type.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}),
	tool("scroll", "Scroll at screenshot pixels. dy is wheel steps (negative up, positive down).", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"x":  map[string]any{"type": "integer"},
			"y":  map[string]any{"type": "integer"},
			"dy": map[string]any{"type": "integer"},
		},
		"required": []string{"x", "y", "dy"},
	}),
	tool("skill", "Load an enabled skill. Pass name (from the Skills list in the system prompt). Optional path is a file inside the skill (default SKILL.md). Scripts live at /opt/silo/skills/<name>/.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"path": map[string]any{"type": "string", "description": "relative file inside the skill, default SKILL.md"},
		},
		"required": []string{"name"},
	}),
	tool("artifact", "Show a deliverable as a card in the thread. Path is relative to /workspace. A directory that contains SKILL.md becomes an installable skill (the human clicks Save skill); any other file becomes a downloadable card with a preview. Use this for things the human keeps or downloads. This is not present — present merely displays a file inline.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string"},
			"title": map[string]any{"type": "string", "description": "display title (defaults to the file or skill name)"},
			"kind":  map[string]any{"type": "string", "enum": []string{"skill", "file"}, "description": "override the auto-detected type"},
		},
		"required": []string{"path"},
	}),
	tool("web_search", "Search the public web. Returns titles, URLs, and snippets. Prefer this over typing a search URL on the desktop.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":       map[string]any{"type": "string"},
			"max_results": map[string]any{"type": "integer", "description": "how many hits to return (default 8, max 20)"},
		},
		"required": []string{"query"},
	}),
	tool("feed", "Post a markdown message to the human's Feed: a read-only inbox with an unread badge that they check later. Use it for results they should see after the fact (automation findings, a finished long task, a digest) — not for replies in this chat. Self-contained: they may read it days later.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "description": "optional short headline"},
			"text":  map[string]any{"type": "string", "description": "the post, markdown"},
		},
		"required": []string{"text"},
	}),
	tool("channel", "Send a message to one of this Bot's channels (Telegram, …). Defaults to the channel this conversation came from; pass channel to send to a different one. A channel is bound to one chat, so there is no destination to choose.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{"type": "string", "description": "channel name; omit for the current channel"},
			"text":    map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}),
	tool("list_automations", "List this Bot's automations (scheduled background prompts), including the pinned Heartbeat, with ids, schedules, next and last runs.", map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}),
	tool("create_automation", "Create an automation: a prompt this Bot runs on its own on a cron schedule, in the background. Each run starts with a fresh context (only the prompt, SOUL, and memory), so write a self-contained prompt that says what to do and where to keep state. Runs are logged on the Automations page.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":     map[string]any{"type": "string", "description": "short unique name"},
			"prompt":   map[string]any{"type": "string", "description": "the instruction each run receives as its message"},
			"schedule": map[string]any{"type": "string", "description": "5-field cron in the machine's local time (minute hour day-of-month month day-of-week), e.g. `0 9 * * 1-5` for weekdays at 09:00, `*/30 * * * *` every 30 minutes. At least 5 minutes apart. Empty never fires."},
			"enabled":  map[string]any{"type": "boolean", "description": "default true"},
		},
		"required": []string{"name", "prompt", "schedule"},
	}),
	tool("update_automation", "Change an automation by id or name. Pass only the fields to change. Use this to set the pinned Heartbeat's schedule or prompt. schedule \"\" stops it firing.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"automation": map[string]any{"type": "string", "description": "id or name"},
			"name":       map[string]any{"type": "string"},
			"prompt":     map[string]any{"type": "string"},
			"schedule":   map[string]any{"type": "string", "description": "5-field cron, local time; empty never fires"},
			"enabled":    map[string]any{"type": "boolean"},
		},
		"required": []string{"automation"},
	}),
	tool("delete_automation", "Delete an automation and its run log, by id or name. The Heartbeat cannot be deleted — clear its schedule instead.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"automation": map[string]any{"type": "string", "description": "id or name"},
		},
		"required": []string{"automation"},
	}),
	tool("chats", "Read this Bot's chats and channel conversations. With no chat, lists them. With chat (id or title), returns recent messages. Stays inside this Bot.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chat":  map[string]any{"type": "string", "description": "chat id or title; omit to list chats"},
			"limit": map[string]any{"type": "integer", "description": "max messages (default 20, max 50)"},
		},
	}),
}

func (a *App) emit(botID, chatID, runID, kind, body, tool string) {
	body = validUTF8(a.Mask(botID).Apply(body))
	tool = validUTF8(tool)
	id := ids.New()
	a.DB.Create(&db.RunEvent{ID: id, RunID: runID, Kind: kind, Body: body, Tool: tool, CreatedAt: time.Now()})
	a.Bus.Publish(botID, &v1.RunEvent{Id: id, RunId: runID, ChatId: chatID, Kind: kind, Body: body, Tool: tool})
}

// presentBrowseLimit is the byte budget the CP asks the worker for when a
// tool presents a file. It is higher than the Files-tab preview budget so a
// photo or a rendered PDF page still reaches the multimodal model.
const presentBrowseLimit = 32 << 20

type eventAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"`
}

// emitUser persists the opening user message and carries any chat uploads so
// the thread can render chips on reload and the model can be told the paths.
func (a *App) emitUser(botID, chatID, runID, body string, atts []*v1.Attachment) {
	body = validUTF8(a.Mask(botID).Apply(body))
	meta := ""
	if len(atts) > 0 {
		flat := make([]eventAttachment, 0, len(atts))
		for _, at := range atts {
			flat = append(flat, eventAttachment{Name: at.GetName(), Path: at.GetPath(), Size: at.GetSize(), Mime: at.GetMime()})
		}
		if b, err := json.Marshal(flat); err == nil {
			meta = string(b)
		}
	}
	id := ids.New()
	now := time.Now()
	a.DB.Create(&db.RunEvent{ID: id, RunID: runID, Kind: "user", Body: body, Meta: meta, CreatedAt: now})
	a.Bus.Publish(botID, &v1.RunEvent{Id: id, RunId: runID, ChatId: chatID, Kind: "user", Body: body, Attachments: atts, CreatedAt: now.Format(time.RFC3339)})
}

func attachmentsFromMeta(meta string) []eventAttachment {
	if meta == "" {
		return nil
	}
	var out []eventAttachment
	if json.Unmarshal([]byte(meta), &out) != nil {
		return nil
	}
	return out
}

func v1Attachments(flat []eventAttachment) []*v1.Attachment {
	if len(flat) == 0 {
		return nil
	}
	out := make([]*v1.Attachment, 0, len(flat))
	for _, a := range flat {
		out = append(out, &v1.Attachment{Name: a.Name, Path: a.Path, Size: a.Size, Mime: a.Mime})
	}
	return out
}

type inboxMsg struct {
	text string
	atts []*v1.Attachment
}

// runOrigin identifies where a run came from and how to deliver user-visible
// sections. A nil origin or channel is a Web UI chat.
type runOrigin struct {
	channel  *db.Channel
	external string
	deliver  func(channels.Outbound) error
	// automation is set when a scheduled automation started the run. Its runs
	// start from a fresh context and are not delivered anywhere.
	automation *db.Automation
	// recall is the auto-recalled memory note, computed once per run.
	recall string
}

// runRequest is one call into the shared execution engine. Chats, channels,
// and future automations all go through here.
type runRequest struct {
	botID  string
	chatID string
	text   string
	atts   []*v1.Attachment
	origin *runOrigin
}

// startRun records a run and launches the agent loop. Callers with a live run
// for the conversation should inject instead.
func (a *App) startRun(req runRequest) (string, error) {
	var b db.Bot
	if err := a.DB.First(&b, "id = ?", req.botID).Error; err != nil {
		return "", fmt.Errorf("unknown bot")
	}
	channelID := ""
	origin := "chat"
	if req.origin != nil && req.origin.channel != nil {
		channelID = req.origin.channel.ID
		origin = "channel"
	}
	if req.origin != nil && req.origin.automation != nil {
		origin = "automation"
	}
	runID := ids.New()
	run := db.Run{ID: runID, BotID: req.botID, ChatID: req.chatID, ChannelID: channelID, Origin: origin, Status: "running", CreatedAt: time.Now()}
	if err := a.DB.Create(&run).Error; err != nil {
		return "", err
	}
	b.LastTask = req.text
	b.Status = "working"
	// Only touch the run fields: saving the whole row here could clobber a
	// ContainerID/TokenHash that a concurrent ensureRunning just wrote.
	a.DB.Model(&db.Bot{}).Where("id = ?", b.ID).Updates(map[string]any{
		"last_task": req.text,
		"status":    "working",
	})
	// Always ensure the box: a message must start a stopped Bot, and a worker
	// session can outlive a container that was removed out of band.
	cp := b
	a.ensureRunningBg(&cp)
	inbox := make(chan inboxMsg, 32)
	done := make(chan struct{})
	go a.runLoop(req.botID, req.chatID, runID, req.text, req.atts, req.origin, inbox, done)
	return runID, nil
}

// liveRunID returns the active run for a conversation, if any.
func (a *App) liveRunID(botID, chatID string) string {
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
func (a *App) inject(botID, chatID, runID, text string, atts []*v1.Attachment) bool {
	a.mu.Lock()
	lr := a.runs[runID]
	a.mu.Unlock()
	if lr == nil || lr.botID != botID || lr.chatID != chatID {
		return false
	}
	select {
	case lr.inbox <- inboxMsg{text: text, atts: atts}:
		a.emitUser(botID, chatID, runID, text, atts)
		a.dbSaveBotWorking(botID, text)
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

// messageTimeStamp renders t in the machine's local timezone as a compact
// marker the model can read. The date and time ride on the user message rather
// than the system prompt so the cached prompt prefix is never invalidated.
func messageTimeStamp(t time.Time) string {
	return "[" + t.Local().Format("Mon, 2006-01-02 15:04:05 MST") + "] "
}

// stampUserText prefixes a user message with the moment it was sent. History
// keeps its original timestamp (stable across runs, so prompt caches hold) and
// the newest message carries the current local time.
func stampUserText(text string, t time.Time) string {
	return messageTimeStamp(t) + text
}

func userTextWithAttachments(text string, atts []*v1.Attachment) string {
	if len(atts) == 0 {
		return text
	}
	paths := make([]string, 0, len(atts))
	for _, at := range atts {
		paths = append(paths, at.GetPath())
	}
	return text + "\n\n[Attached files in the workspace: " + strings.Join(paths, ", ") + "]"
}

// emitDelivered persists one user-visible block and, on a channel origin,
// delivers it. kind is "assistant" for a plain reply, "section" for a
// sentinel-bounded block on the final turn, or "section_live" for one emitted
// mid-turn (displayed and delivered but not replayed as model history).
func (a *App) emitDelivered(botID, chatID, runID, kind, body string, origin *runOrigin) {
	if strings.TrimSpace(body) == "" {
		return
	}
	a.emit(botID, chatID, runID, kind, body, "")
	if origin != nil && origin.deliver != nil {
		if err := origin.deliver(channels.Outbound{ExternalID: origin.external, Text: body}); err != nil {
			a.emit(botID, chatID, runID, "error", "channel send failed: "+err.Error(), "")
		}
	}
}

// deliverTool forwards a present/artifact side effect to a channel by reading
// the real bytes from the workspace and attaching them, so the human on the
// other side gets the file itself. A skill directory is zipped.
func (a *App) deliverTool(ctx context.Context, botID, chatID, runID string, origin *runOrigin, name, argsJSON, img string) {
	if origin == nil || origin.deliver == nil {
		return
	}
	send := func(text string, att *channels.Attachment) {
		msg := channels.Outbound{ExternalID: origin.external, Text: text}
		if att != nil {
			msg.Files = []channels.Attachment{*att}
		}
		if err := origin.deliver(msg); err != nil {
			a.emit(botID, chatID, runID, "error", "channel attach failed: "+err.Error(), "")
		}
	}
	var args struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		Kind  string `json:"kind"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	switch name {
	case "present":
		if img != "" {
			if att, ok := dataURLAttachment(args.Path, img); ok {
				send("File: "+filepath.Base(args.Path), &att)
				return
			}
		}
		if att, err := a.workspaceAttachment(ctx, botID, args.Path); err == nil {
			send("File: "+att.Name, &att)
			return
		}
		if args.Path != "" {
			send("Presented in the workspace: "+args.Path, nil)
		}
	case "artifact":
		if info, err := a.describeArtifact(ctx, botID, args.Path, args.Kind, args.Title); err == nil {
			if info.Type == "skill" {
				if att, err := a.skillZipAttachment(ctx, botID, info.Path, info.Title); err == nil {
					send("Skill: "+info.Name, &att)
					return
				}
			} else if att, err := a.workspaceAttachment(ctx, botID, info.Path); err == nil {
				send("Artifact: "+info.Title, &att)
				return
			}
		}
		title := strings.TrimSpace(args.Title)
		if title == "" {
			title = filepath.Base(args.Path)
		}
		if title != "" {
			send("Artifact: "+title, nil)
		}
	}
}

func dataURLAttachment(path, dataURL string) (channels.Attachment, bool) {
	const marker = ";base64,"
	i := strings.Index(dataURL, marker)
	if !strings.HasPrefix(dataURL, "data:") || i < 0 {
		return channels.Attachment{}, false
	}
	mime := strings.TrimPrefix(dataURL[:i], "data:")
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+len(marker):])
	if err != nil || len(raw) == 0 {
		return channels.Attachment{}, false
	}
	name := filepath.Base(path)
	if name == "." || name == "/" || name == "" {
		name = "file"
	}
	return channels.Attachment{Name: name, Mime: mime, Data: raw}, true
}

// turnResult is one streamed assistant turn: the assembled message plus any
// user-visible sections the model closed with <section_send />.
type turnResult struct {
	assistant llm.Message
	sections  []string
	tail      string
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
	if usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0 {
		a.emitUsage(botID, chatID, runID, usage)
	}
	if !saw {
		return turnResult{}, fmt.Errorf("empty completion")
	}
	assistant := llm.Message{Role: llm.RoleAssistant, Text: text.String()}
	for _, idx := range order {
		c := open[idx].call
		if c.ID == "" {
			c.ID = "call_" + strconv.Itoa(idx)
		}
		assistant.ToolCalls = append(assistant.ToolCalls, c)
	}
	return turnResult{assistant: assistant, sections: sections, tail: sp.Flush()}, nil
}

func (a *App) runLoop(botID, chatID, runID, userText string, atts []*v1.Attachment, origin *runOrigin, inbox chan inboxMsg, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	a.trackRun(botID, chatID, runID, cancel, inbox, done)
	defer close(done)
	defer a.untrackRun(runID)

	modelID := a.resolveModel(botID, chatID)
	client, provider, model, err := a.modelClient(modelID, botID, "chat")
	if err != nil {
		a.emit(botID, chatID, runID, "error", err.Error(), "")
		a.finish(botID, chatID, runID, "error")
		return
	}
	settings := a.cfg().ProviderSettings(provider)

	a.emitUser(botID, chatID, runID, userText, atts)
	a.bumpChat(chatID)
	title := userText
	if strings.TrimSpace(title) == "" && len(atts) > 0 {
		title = "attached " + atts[0].GetName()
	}
	go a.nameChat(botID, chatID, runID, title)

	var msgs []llm.Message
	if origin != nil && origin.automation != nil {
		// Each firing is independent: only this run is the model's history.
		msgs = a.historyFromRuns([]db.Run{{ID: runID}})
	} else {
		msgs = a.historyFromDB(chatID)
	}
	if note := a.autoRecall(ctx, botID, userText); note != "" {
		if origin == nil {
			origin = &runOrigin{}
		}
		origin.recall = note
	}
	var lastLook string
	// seen counts identical read/grep calls this run so a stuck loop does not
	// re-send content the model already has. Mutating tools clear it.
	seen := map[string]int{}

	for {
		msgs = drainInbox(msgs, inbox)
		if a.stopped(ctx, botID, chatID, runID) {
			return
		}
		// Re-resolve each turn so a switch_model tool call takes effect on the
		// next model call without restarting the run.
		if m := a.resolveModel(botID, chatID); m != modelID {
			if c, pr, mo, e := a.modelClient(m, botID, "chat"); e == nil {
				modelID, client, provider, model = m, c, pr, mo
				settings = a.cfg().ProviderSettings(pr)
			}
		}
		res, err := a.streamTurn(ctx, botID, chatID, runID, client, llm.Request{
			Model:    model,
			System:   a.buildSystemBlocks(botID, origin),
			Messages: msgs,
			Tools:    toolDefs,
			Cache:    cachePolicy(settings, botID),
		})
		if err != nil {
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			a.emit(botID, chatID, runID, "error", err.Error(), "")
			a.finish(botID, chatID, runID, "error")
			return
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
			if len(out) > 12000 {
				out = truncateUTF8(out, 12000) + "\n…truncated"
			}
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
			if origin != nil && origin.channel != nil && err == nil {
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
	if a.DB.First(&c, "id = ?", chatID).Error != nil || !untitledTitle(c.Title) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	snippet := userText
	if len(snippet) > 800 {
		snippet = truncateUTF8(snippet, 800)
	}
	client, provider, model, err := a.modelClient(a.titleModel(botID, chatID), botID, "title")
	if err != nil {
		log.Printf("name chat %s: %v", chatID, err)
		return
	}
	res, err := client.Complete(ctx, llm.Request{
		Model:    model,
		System:   []llm.SystemBlock{{Text: "Reply with only a 2-6 word chat title for the user's message. Capture intent, not a quote. No quotes, no punctuation, no explanation."}},
		Messages: []llm.Message{{Role: llm.RoleUser, Text: snippet}},
		Cache:    cachePolicy(a.cfg().ProviderSettings(provider), botID),
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

func (a *App) historyFromDB(chatID string) []llm.Message {
	var runs []db.Run
	a.DB.Where("chat_id = ?", chatID).Order("created_at").Find(&runs)
	return a.historyFromRuns(runs)
}

// historyFromRuns replays the given runs, in order, as model messages.
func (a *App) historyFromRuns(runs []db.Run) []llm.Message {
	var msgs []llm.Message
	quoted := false
	for _, run := range runs {
		var evs []db.RunEvent
		a.DB.Where("run_id = ?", run.ID).Order("seq").Find(&evs)
		var pending []llm.ToolCall
		var lastCall string
		var results []llm.Message
		flushTools := func() {
			if len(pending) == 0 {
				return
			}
			for len(results) < len(pending) {
				id := pending[len(results)].ID
				if id == "" {
					id = "call_missing"
				}
				results = append(results, llm.Message{Role: llm.RoleTool, ToolCallID: id, Text: "error: interrupted"})
			}
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, ToolCalls: pending})
			msgs = append(msgs, results...)
			pending = nil
			results = nil
			lastCall = ""
		}
		for _, ev := range evs {
			switch ev.Kind {
			case feedQuoteKind:
				flushTools()
				msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: feedQuoteText(ev.Tool, ev.Body, ev.CreatedAt)})
				quoted = true
				continue
			case "user":
				flushTools()
				text := ev.Body
				if atts := attachmentsFromMeta(ev.Meta); len(atts) > 0 {
					paths := make([]string, 0, len(atts))
					for _, at := range atts {
						paths = append(paths, at.Path)
					}
					text += "\n\n[Attached files in the workspace: " + strings.Join(paths, ", ") + "]"
				}
				text = stampUserText(text, ev.CreatedAt)
				// A quote is its own user turn; fold the reply into it so
				// roles still alternate.
				if n := len(msgs); quoted && n > 0 && msgs[n-1].Role == llm.RoleUser {
					msgs[n-1].Text += "\n\n" + text
				} else {
					msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: text})
				}
			case "assistant", "section":
				flushTools()
				if strings.TrimSpace(ev.Body) != "" {
					msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: ev.Body})
				}
			case "tool":
				if n := len(pending); n > 0 {
					last := &pending[n-1]
					same := last.Name == ev.Tool || last.Name == "" || ev.Tool == ""
					incomplete := last.Arguments == "" || !jsonLooksComplete(last.Arguments)
					if same && (ev.Body == "" || incomplete || ev.Body == last.Arguments) {
						if ev.Body != "" {
							last.Arguments = ev.Body
						}
						if ev.Tool != "" {
							last.Name = ev.Tool
						}
						lastCall = last.ID
						continue
					}
				}
				id := "call_" + ev.ID
				lastCall = id
				pending = append(pending, llm.ToolCall{ID: id, Name: ev.Tool, Arguments: ev.Body})
			case "tool_args_chunk":
				if n := len(pending); n > 0 {
					pending[n-1].Arguments += ev.Body
				}
			case "tool_result":
				id := lastCall
				if id == "" {
					id = "call_" + ev.ID
				}
				results = append(results, llm.Message{Role: llm.RoleTool, ToolCallID: id, Text: ev.Body})
			}
		}
		flushTools()
	}
	return msgs
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
}

func (a *App) execTool(ctx context.Context, botID, chatID, runID, name, argsJSON string) (string, string, error) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	str := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	path := relWorkspace(str("path"))
	if name == "click" || name == "scroll" {
		if err := screenPoint(num(args, "x"), num(args, "y")); err != nil {
			return "", "", err
		}
	}
	if name == "type" && str("text") == "" {
		return "", "", fmt.Errorf("text required")
	}
	if name == "key" && str("name") == "" {
		return "", "", fmt.Errorf("key required")
	}
	if name == "scroll" && num(args, "dy") == 0 {
		return "", "", fmt.Errorf("dy required")
	}
	if name == "present" && path == "" {
		return "", "", fmt.Errorf("path required")
	}
	if name == "delete" && path == "" {
		return "", "", fmt.Errorf("path required")
	}
	if name == "skill" && str("name") == "" {
		return "", "", fmt.Errorf("name required")
	}
	if name == "artifact" && path == "" {
		return "", "", fmt.Errorf("path required")
	}
	if name == "web_search" && str("query") == "" {
		return "", "", fmt.Errorf("query required")
	}
	if name == "channel" && str("text") == "" {
		return "", "", fmt.Errorf("text required")
	}
	if name == "channel" {
		out, err := a.channelSendTool(ctx, botID, runID, args)
		return out, "", err
	}
	if name == "chats" {
		out, err := a.chatsReadTool(ctx, botID, runID, args)
		return out, "", err
	}
	if name == "feed" {
		var bot db.Bot
		if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
			return "", "", fmt.Errorf("unknown bot")
		}
		if args == nil {
			args = map[string]any{}
		}
		out, err := a.feedTool(ctx, &bot, runID, args)
		return out, "", err
	}
	if isAutomationTool(name) {
		var bot db.Bot
		if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
			return "", "", fmt.Errorf("unknown bot")
		}
		if args == nil {
			args = map[string]any{}
		}
		out, err := a.automationTool(ctx, &bot, runID, name, args)
		return out, "", err
	}
	conn, action, ok := chatTool(name)
	if !ok {
		return "", "", fmt.Errorf("unknown tool %s", name)
	}
	var bot db.Bot
	if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
		return "", "", fmt.Errorf("unknown bot")
	}
	if _, err := a.authorizeAction(ctx, &bot, runID, conn, action, argsJSON, ""); err != nil {
		return "", "", err
	}
	if name == "soul" || name == "core_memory" {
		out, err := a.execDoc(botID, name, args)
		return out, "", err
	}
	if name == "remember" {
		out, err := a.remember(ctx, botID, runID, str("content"))
		return out, "", err
	}
	if name == "recall" {
		out, err := a.recallTool(ctx, botID, args)
		return out, "", err
	}
	if name == "forget" {
		out, err := a.forget(botID, str("id"))
		return out, "", err
	}
	if name == "list_models" {
		out, err := a.listModelsTool(botID, chatID)
		return out, "", err
	}
	if name == "switch_model" {
		out, err := a.switchModelTool(chatID, str("model"))
		return out, "", err
	}
	if name == "skill" {
		out, err := a.loadSkill(botID, str("name"), str("path"))
		return out, "", err
	}
	if name == "artifact" {
		out, err := a.artifact(ctx, &bot, runID, argsJSON)
		return out, "", err
	}
	if name == "web_search" {
		out, err := a.runWebSearch(ctx, argsJSON)
		return out, "", err
	}
	id := ids.New()
	var cmd *v1.Cmd
	switch name {
	case "terminal":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{Command: str("command")}}}
	case "exec_python":
		code := str("code")
		if wantsChrome(code) {
			if err := a.ensureChrome(ctx, botID, runID); err != nil {
				return "", "", err
			}
		}
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_ExecPython{ExecPython: &v1.ExecPythonCmd{Code: code}}}
	case "read":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_FileRead{FileRead: &v1.FileReadCmd{
			Path: path, Offset: int32(num(args, "offset")), Limit: int32(num(args, "limit")),
		}}}
	case "write":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_FileWrite{FileWrite: &v1.FileWriteCmd{Path: path, Content: str("content")}}}
	case "patch":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_FilePatch{FilePatch: &v1.FilePatchCmd{Path: path, OldText: str("old_text"), NewText: str("new_text")}}}
	case "delete":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Remove{Remove: &v1.RemoveCmd{Path: path}}}
	case "grep":
		max := int32(num(args, "max_hits"))
		if max <= 0 {
			max = 80
		}
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Grep{Grep: &v1.GrepCmd{
			Pattern: str("pattern"), Path: path, Include: str("include"), MaxHits: max,
		}}}
	case "present":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: path, Limit: presentBrowseLimit}}}
	case "look":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Look{Look: &v1.LookCmd{}}}
	case "click":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Click{Click: &v1.ClickCmd{
			X: int32(num(args, "x")), Y: int32(num(args, "y")), Button: str("button"),
		}}}
	case "type":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Type{Type: &v1.TypeCmd{Text: str("text")}}}
	case "key":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Key{Key: &v1.KeyCmd{Name: str("name")}}}
	case "scroll":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Scroll{Scroll: &v1.ScrollCmd{
			X: int32(num(args, "x")), Y: int32(num(args, "y")), Dy: int32(num(args, "dy")),
		}}}
	default:
		return "", "", fmt.Errorf("unknown tool %s", name)
	}
	log.Printf("exec %s bot=%s run=%s", name, botID, runID)
	if !a.waitWorker(ctx, botID, 90*time.Second) {
		return "", "", fmt.Errorf("the Bot machine is not running yet")
	}
	a.mu.Lock()
	a.cmdRun[id] = runID
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.cmdRun, id)
		a.mu.Unlock()
	}()
	out, err := a.Hub.ExecResult(ctx, botID, cmd)
	if err != nil {
		return "", "", err
	}
	switch name {
	case "read":
		return formatRead(out.Out, num(args, "offset")), "", nil
	case "grep":
		max := num(args, "max_hits")
		if max <= 0 {
			max = 80
		}
		return capHits(out.Out, max), "", nil
	case "present":
		return presentAck(path, out.Out), presentImageURL(path, out.Out), nil
	case "look":
		return lookAck(out.Out), presentImageURL(lookPath, out.Out), nil
	default:
		// Python/terminal commands may have taken a desktop look; the worker
		// reports the fresh screenshot as a data: URL on the done event.
		return out.Out, out.Image, nil
	}
}

const (
	screenW      = 1600
	screenH      = 900
	lookPath     = "bot/screen.jpg"
	lookCoordLaw = "Image is 1600×900. Origin top-left. click(x,y) is in these pixels. The worker applies them with no scale."
)

// isLookImage reports whether a user turn carries a live desktop screenshot.
// The text is the coordinate law set by visionImage.
func isLookImage(m llm.Message) bool {
	return m.Role == llm.RoleUser && len(m.Images) > 0 && strings.HasPrefix(strings.TrimSpace(m.Text), "Image is 1600×900")
}

// pruneLookImages drops pixels from earlier screenshots so a long GUI run does
// not re-send every frame on each model call. Only the newest look is kept.
func pruneLookImages(msgs []llm.Message) {
	for i := range msgs {
		if isLookImage(msgs[i]) {
			msgs[i].Images = nil
			msgs[i].Text = "Earlier screenshot omitted — act on the current one."
		}
	}
}

// isLookTool reports whether a tool call's image is the desktop, not a
// presented file. Python and terminal can take a look as a side effect.
func isLookTool(name string) bool {
	switch name {
	case "look", "exec_python", "terminal":
		return true
	default:
		return false
	}
}

func chatTool(name string) (conn, action string, ok bool) {
	switch name {
	case "exec_python":
		return security.Python, "run", true
	case "terminal":
		return security.Terminal, "run", true
	case "read", "write", "patch", "grep", "delete", "present":
		return security.Files, name, true
	case "look", "click", "type", "key", "scroll":
		return security.Desktop, name, true
	case "soul", "core_memory", "remember", "recall", "forget":
		return security.Bot, name, true
	case "skill":
		return security.Skills, "load", true
	case "artifact":
		return security.Artifact, "emit", true
	case "web_search":
		return security.Web, "search", true
	case "list_models":
		return security.Model, "list", true
	case "switch_model":
		return security.Model, "switch", true
	default:
		return "", "", false
	}
}

func screenPoint(x, y int) error {
	if x < 0 || x >= screenW || y < 0 || y >= screenH {
		return fmt.Errorf("(%d,%d) is outside %d×%d", x, y, screenW, screenH)
	}
	return nil
}

func visionImage(tool, url string) (string, llm.Image) {
	img := imageFromDataURL(url)
	if isLookTool(tool) {
		return lookCoordLaw, img
	}
	img.Detail = "high"
	return "You presented this image. Read the visible labels before you act.", img
}

// imageFromDataURL decodes a data: URL into a neutral image.
func imageFromDataURL(url string) llm.Image {
	const marker = ";base64,"
	i := strings.Index(url, marker)
	if !strings.HasPrefix(url, "data:") || i < 0 {
		return llm.Image{}
	}
	raw, err := base64.StdEncoding.DecodeString(url[i+len(marker):])
	if err != nil {
		return llm.Image{}
	}
	return llm.Image{Mime: strings.TrimPrefix(url[:i], "data:"), Data: raw}
}

func lookAck(raw string) string {
	return lookCoordLaw + " " + presentAck(lookPath, raw)
}

func presentImageURL(path, raw string) string {
	var row struct {
		Name      string `json:"name"`
		Data      string `json:"data"`
		Truncated bool   `json:"truncated"`
	}
	if json.Unmarshal([]byte(raw), &row) != nil || row.Data == "" || row.Truncated {
		// A truncated payload is a broken image; never attach it.
		return ""
	}
	mime := imageMIME(path)
	if mime == "" {
		mime = imageMIME(row.Name)
	}
	if mime == "" {
		return ""
	}
	return "data:" + mime + ";base64," + row.Data
}

func imageMIME(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

// previewExts are the extensions the thread renders inline (keep in sync with
// web/src/fileKind.ts). Anything else has no preview, so a present of it
// should have gone through artifact instead.
var previewExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".svg": true, ".ico": true, ".avif": true,
	".pdf": true,
	".mp4": true, ".webm": true, ".ogv": true, ".mov": true,
	".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".flac": true, ".aac": true,
	".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".json": true, ".docx": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true, ".go": true, ".rs": true, ".rb": true,
	".java": true, ".kt": true, ".c": true, ".h": true, ".cpp": true, ".cc": true,
	".sh": true, ".bash": true, ".zsh": true, ".css": true, ".html": true, ".htm": true,
	".xml": true, ".yml": true, ".yaml": true, ".toml": true, ".sql": true,
	".txt": true, ".log": true, ".env": true, ".cfg": true, ".ini": true, ".conf": true,
	".gitignore": true, ".dockerfile": true,
}

func hasInlinePreview(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return true // extensionless files render as text
	}
	return previewExts[ext]
}

func presentAck(path, raw string) string {
	var row struct {
		Name      string `json:"name"`
		Truncated bool   `json:"truncated"`
		Size      int64  `json:"size"`
	}
	if json.Unmarshal([]byte(raw), &row) != nil || row.Name == "" {
		if botScratch(path) {
			return "seen. Scratch — you have the pixels; do not retype."
		}
		return "presented. Shown in the thread — do not retype it."
	}
	label := path
	if label == "" {
		label = row.Name
	}
	if botScratch(path) {
		msg := fmt.Sprintf("seen %s (%s). Scratch — you have the pixels; the human has a collapsed row.", label, formatSize(row.Size))
		if row.Truncated {
			msg += " Preview is truncated."
		}
		return msg
	}
	msg := fmt.Sprintf("presented %s (%s). Shown in the thread — do not retype it.", row.Name, formatSize(row.Size))
	if row.Truncated {
		msg += " Preview is truncated."
	}
	if !hasInlinePreview(row.Name) {
		msg += " This type has no inline preview — use artifact instead so the human gets a downloadable card."
	}
	return msg
}

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func jsonLooksComplete(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}

func num(args map[string]any, k string) int {
	switch v := args[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

// readResult is the Worker's sliced-read payload (see worker readFileSlice).
type readResult struct {
	Content    string `json:"content"`
	NextOffset int    `json:"next_offset"`
	TotalLines int    `json:"total_lines"`
	Truncated  bool   `json:"truncated"`
}

// formatRead numbers the already-sliced Worker read and appends a continuation
// hint. A non-JSON body (older worker) falls through unchanged.
func formatRead(raw string, offset int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	res := readResult{}
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			return raw
		}
	} else {
		res.Content = raw
	}
	content := strings.TrimRight(res.Content, "\n")
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if offset < 1 {
		offset = 1
	}
	last := offset + len(lines) - 1
	width := len(fmt.Sprintf("%d", max(max(last, res.TotalLines), 1)))
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%*d|%s\n", width, offset+i, line)
	}
	if res.Truncated {
		if res.TotalLines > 0 {
			fmt.Fprintf(&b, "… lines %d–%d of %d; read offset=%d for more\n", offset, last, res.TotalLines, res.NextOffset)
		} else {
			fmt.Fprintf(&b, "… lines %d–%d; read offset=%d for more\n", offset, last, res.NextOffset)
		}
	} else if offset > 1 || res.TotalLines > last {
		fmt.Fprintf(&b, "… lines %d–%d of %d\n", offset, last, res.TotalLines)
	}
	return b.String()
}

func capHits(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n…%d more hits", len(lines)-n)
}
