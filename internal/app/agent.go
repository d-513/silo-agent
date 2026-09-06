package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/prompts"
)

var toolDefs = []openai.ChatCompletionToolUnionParam{
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "terminal",
		Description: openai.String("Run a shell command in the Bot workspace."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "exec_python",
		Description: openai.String("Run Python in the Bot. Secrets: silo_runtime.get_secret. Scratch files go in /workspace/bot. User-facing files go in /workspace (not bot/). Browser: chrome_page(); screenshot to /workspace/bot/page.png, present bot/page.png (you see the pixels; the human gets a collapsed row), one action, screenshot+present again. Do not oneshot cookies+search. Connectors are import tools.<slug>. Persist user-facing results to /workspace here, then present — do not hand them to write."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"code": map[string]any{"type": "string"},
			},
			"required": []string{"code"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "read",
		Description: openai.String("Read a workspace file. Returns numbered lines. Use offset (1-based line) and limit to read a slice — do not dump large files."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string"},
				"offset": map[string]any{"type": "integer", "description": "1-based start line"},
				"limit":  map[string]any{"type": "integer", "description": "max lines to return"},
			},
			"required": []string{"path"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "write",
		Description: openai.String("Write a small file you compose yourself, relative to /workspace. Prefer patch for existing files. Do not copy Python or connector output here — save that from exec_python, then present."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "patch",
		Description: openai.String("Replace exactly one occurrence of old_text with new_text. old_text must match once; if it matches several times, add surrounding lines."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path":     map[string]any{"type": "string"},
				"old_text": map[string]any{"type": "string"},
				"new_text": map[string]any{"type": "string"},
			},
			"required": []string{"path", "old_text", "new_text"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "grep",
		Description: openai.String("Search workspace files. Prefer include (e.g. *.py) over a full-tree scan. Results are capped."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"pattern":  map[string]any{"type": "string"},
				"path":     map[string]any{"type": "string"},
				"include":  map[string]any{"type": "string", "description": "glob such as *.py or *.md"},
				"max_hits": map[string]any{"type": "integer"},
			},
			"required": []string{"pattern"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "soul",
		Description: openai.String("Update this Bot's SOUL (identity, tone, hard rules). Already in the system prompt — do not read a file. Pass content to replace, or old_text/new_text to patch one unique snippet."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"content":  map[string]any{"type": "string"},
				"old_text": map[string]any{"type": "string"},
				"new_text": map[string]any{"type": "string"},
			},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "memory",
		Description: openai.String("Update this Bot's MEMORY (lasting facts). Already in the system prompt. Pass append to add a line, or old_text/new_text to edit or compact. If over the cap, compact first — do not append."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"append":   map[string]any{"type": "string"},
				"old_text": map[string]any{"type": "string"},
				"new_text": map[string]any{"type": "string"},
			},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "present",
		Description: openai.String("Show a workspace file. Path is relative to /workspace — notes.md or bot/page.png, not /workspace/notes.md. bot/ is your scratch (you get the pixels; the human sees a collapsed row). Other paths are for the human as a folio. Images are sent to you as pixels. Do not retype the contents."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}),
}

func (a *App) emit(botID, chatID, runID, kind, body, tool string) {
	body = a.Mask(botID).Apply(body)
	id := ids.New()
	a.DB.Create(&db.RunEvent{ID: id, RunID: runID, Kind: kind, Body: body, Tool: tool, CreatedAt: time.Now()})
	a.Bus.Publish(botID, &v1.RunEvent{Id: id, RunId: runID, ChatId: chatID, Kind: kind, Body: body, Tool: tool})
}

func (a *App) runLoop(botID, chatID, runID, userText string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	a.trackRun(botID, chatID, runID, cancel)
	defer a.untrackRun(runID)
	key := ""
	if a.Cfg != nil {
		key = a.Cfg.OpenRouter.APIKey
	}
	model := a.getSetting("model")
	if model == "" {
		model = config.DefaultModel
	}
	if key == "" {
		a.emit(botID, chatID, runID, "error", "OpenRouter key missing in operator config (silo.yaml / SILO_OPENROUTER__API_KEY).", "")
		a.finish(botID, chatID, runID, "error")
		return
	}
	client := openai.NewClient(
		option.WithAPIKey(key),
		option.WithBaseURL("https://openrouter.ai/api/v1"),
	)

	a.emit(botID, chatID, runID, "user", userText, "")
	a.touchChatTitle(chatID, userText)

	var bot db.Bot
	sysText := prompts.System
	if a.DB.First(&bot, "id = ?", botID).Error == nil {
		sysText = buildSystem(&bot, a.connectorBlurb(botID))
	}
	msgs := []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(sysText)}
	msgs = append(msgs, a.historyFromDB(chatID)...)

	for {
		if a.stopped(ctx, botID, chatID, runID) {
			return
		}
		params := openai.ChatCompletionNewParams{
			Model:    model,
			Messages: msgs,
			Tools:    toolDefs,
		}
		stream := client.Chat.Completions.NewStreaming(ctx, params)
		acc := openai.ChatCompletionAccumulator{}
		var think strings.Builder
		type toolDelta struct {
			name    string
			started bool
		}
		openTools := map[int64]*toolDelta{}
		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)
			if len(chunk.Choices) == 0 {
				continue
			}
			d := chunk.Choices[0].Delta
			if d.Content != "" {
				a.emit(botID, chatID, runID, "chunk", d.Content, "")
			}
			if r := reasoningDelta(d.RawJSON()); r != "" {
				think.WriteString(r)
				a.emit(botID, chatID, runID, "thinking_chunk", r, "")
			}
			for _, tc := range d.ToolCalls {
				st := openTools[tc.Index]
				if st == nil {
					st = &toolDelta{}
					openTools[tc.Index] = st
				}
				if tc.Function.Name != "" {
					st.name = tc.Function.Name
				}
				if st.name != "" && !st.started {
					a.emit(botID, chatID, runID, "tool", "", st.name)
					st.started = true
				}
				if tc.Function.Arguments != "" {
					a.emit(botID, chatID, runID, "tool_args_chunk", tc.Function.Arguments, st.name)
				}
			}
		}
		if err := stream.Err(); err != nil {
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			a.emit(botID, chatID, runID, "error", err.Error(), "")
			a.finish(botID, chatID, runID, "error")
			return
		}
		if think.Len() > 0 {
			a.emit(botID, chatID, runID, "thinking", think.String(), "")
		}
		if len(acc.Choices) == 0 {
			a.emit(botID, chatID, runID, "error", "empty completion", "")
			a.finish(botID, chatID, runID, "error")
			return
		}
		msg := acc.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			txt := strings.TrimSpace(msg.Content)
			a.emit(botID, chatID, runID, "assistant", txt, "")
			a.finish(botID, chatID, runID, "done")
			return
		}
		msgs = append(msgs, msg.ToParam())
		for _, tc := range msg.ToolCalls {
			fn := tc.Function
			out, img, err := a.execTool(ctx, botID, runID, fn.Name, fn.Arguments)
			if a.stopped(ctx, botID, chatID, runID) {
				return
			}
			if err != nil {
				out = "error: " + err.Error()
			}
			out = a.Mask(botID).Apply(out)
			if len(out) > 12000 {
				out = out[:12000] + "\n…truncated"
			}
			a.emit(botID, chatID, runID, "tool_result", out, fn.Name)
			msgs = append(msgs, openai.ToolMessage(out, tc.ID))
			if img != "" {
				msgs = append(msgs, openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
					openai.TextContentPart("You presented this image. Read the visible labels before you act."),
					openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
						URL: img, Detail: "high",
					}),
				}))
			}
			if fn.Name == "soul" || fn.Name == "memory" {
				var row db.Bot
				if a.DB.First(&row, "id = ?", botID).Error == nil {
					msgs[0] = openai.SystemMessage(buildSystem(&row, a.connectorBlurb(botID)))
				}
			}
		}
	}
}

func reasoningDelta(raw string) string {
	if raw == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	for _, k := range []string{"reasoning", "reasoning_content"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func (a *App) touchChatTitle(chatID, userText string) {
	var c db.Chat
	if err := a.DB.First(&c, "id = ?", chatID).Error; err != nil {
		return
	}
	c.UpdatedAt = time.Now()
	if c.Title == "New chat" || c.Title == "Chat" {
		t := strings.TrimSpace(strings.ReplaceAll(userText, "\n", " "))
		if t == "" {
			t = "New chat"
		}
		if len(t) > 48 {
			t = t[:48] + "…"
		}
		c.Title = t
	}
	a.DB.Save(&c)
}

func (a *App) historyFromDB(chatID string) []openai.ChatCompletionMessageParamUnion {
	var runs []db.Run
	a.DB.Where("chat_id = ?", chatID).Order("created_at").Find(&runs)
	var msgs []openai.ChatCompletionMessageParamUnion
	for _, run := range runs {
		var evs []db.RunEvent
		a.DB.Where("run_id = ?", run.ID).Order("created_at").Find(&evs)
		var pending []openai.ChatCompletionMessageToolCallUnionParam
		var lastCall string
		var results []openai.ChatCompletionMessageParamUnion
		flushTools := func() {
			if len(pending) == 0 {
				return
			}
			for len(results) < len(pending) {
				id := pending[len(results)].OfFunction.ID
				if id == "" {
					id = "call_missing"
				}
				results = append(results, openai.ToolMessage("error: interrupted", id))
			}
			msgs = append(msgs, openai.ChatCompletionMessageParamUnion{
				OfAssistant: &openai.ChatCompletionAssistantMessageParam{ToolCalls: pending},
			})
			msgs = append(msgs, results...)
			pending = nil
			results = nil
			lastCall = ""
		}
		for _, ev := range evs {
			switch ev.Kind {
			case "user":
				flushTools()
				msgs = append(msgs, openai.UserMessage(ev.Body))
			case "assistant":
				flushTools()
				if strings.TrimSpace(ev.Body) != "" {
					msgs = append(msgs, openai.AssistantMessage(ev.Body))
				}
			case "tool":
				if n := len(pending); n > 0 && pending[n-1].OfFunction != nil {
					last := pending[n-1].OfFunction
					same := last.Function.Name == ev.Tool || last.Function.Name == "" || ev.Tool == ""
					incomplete := last.Function.Arguments == "" || !jsonLooksComplete(last.Function.Arguments)
					if same && (ev.Body == "" || incomplete || ev.Body == last.Function.Arguments) {
						if ev.Body != "" {
							last.Function.Arguments = ev.Body
						}
						if ev.Tool != "" {
							last.Function.Name = ev.Tool
						}
						lastCall = last.ID
						continue
					}
				}
				id := "call_" + ev.ID
				lastCall = id
				pending = append(pending, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: id,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name: ev.Tool, Arguments: ev.Body,
						},
					},
				})
			case "tool_args_chunk":
				if n := len(pending); n > 0 && pending[n-1].OfFunction != nil {
					pending[n-1].OfFunction.Function.Arguments += ev.Body
				}
			case "tool_result":
				id := lastCall
				if id == "" {
					id = "call_" + ev.ID
				}
				results = append(results, openai.ToolMessage(ev.Body, id))
			}
		}
		flushTools()
	}
	return msgs
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

func (a *App) execTool(ctx context.Context, botID, runID, name, argsJSON string) (string, string, error) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	str := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	path := relWorkspace(str("path"))
	if name == "soul" || name == "memory" {
		out, err := a.execDoc(botID, name, args)
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
	case "grep":
		max := int32(num(args, "max_hits"))
		if max <= 0 {
			max = 80
		}
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Grep{Grep: &v1.GrepCmd{
			Pattern: str("pattern"), Path: path, Include: str("include"), MaxHits: max,
		}}}
	case "present":
		if path == "" {
			return "", "", fmt.Errorf("path required")
		}
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: path}}}
	default:
		return "", "", fmt.Errorf("unknown tool %s", name)
	}
	log.Printf("exec %s bot=%s run=%s", name, botID, runID)
	a.mu.Lock()
	a.cmdRun[id] = runID
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.cmdRun, id)
		a.mu.Unlock()
	}()
	out, err := a.Hub.Exec(ctx, botID, cmd)
	if err != nil {
		return "", "", err
	}
	switch name {
	case "read":
		return formatRead(out, num(args, "offset"), num(args, "limit")), "", nil
	case "grep":
		max := num(args, "max_hits")
		if max <= 0 {
			max = 80
		}
		return capHits(out, max), "", nil
	case "present":
		return presentAck(path, out), presentImageURL(path, out), nil
	default:
		return out, "", nil
	}
}

func presentImageURL(path, raw string) string {
	var row struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &row) != nil || row.Data == "" {
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
			msg += " Preview is the first 2 MB."
		}
		return msg
	}
	msg := fmt.Sprintf("presented %s (%s). Shown in the thread — do not retype it.", row.Name, formatSize(row.Size))
	if row.Truncated {
		msg += " Preview is the first 2 MB."
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

func formatRead(raw string, offset, limit int) string {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	width := len(fmt.Sprintf("%d", max(end, 1)))
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%*d|%s\n", width, i+1, lines[i])
	}
	if start > 0 || end < len(lines) {
		fmt.Fprintf(&b, "… lines %d–%d of %d\n", start+1, end, len(lines))
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
