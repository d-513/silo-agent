package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
		Description: openai.String("Run Python in the Bot. Use silo_runtime.get_secret(name) for secrets."),
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
		Description: openai.String("Read a file relative to /workspace."),
		Parameters: openai.FunctionParameters{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "write",
		Description: openai.String("Write a file relative to /workspace."),
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
		Description: openai.String("Replace old_text with new_text in a file."),
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
		Description: openai.String("Search workspace files."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
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
	a.trackRun(botID, runID, cancel)
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

	sys := openai.SystemMessage(prompts.System)
	msgs := []openai.ChatCompletionMessageParamUnion{sys}
	msgs = append(msgs, a.historyFromDB(chatID)...)

	for i := 0; i < 12; i++ {
		params := openai.ChatCompletionNewParams{
			Model:    model,
			Messages: msgs,
			Tools:    toolDefs,
		}
		stream := client.Chat.Completions.NewStreaming(ctx, params)
		acc := openai.ChatCompletionAccumulator{}
		var think strings.Builder
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
		}
		if err := stream.Err(); err != nil {
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
			a.emit(botID, chatID, runID, "tool", fn.Arguments, fn.Name)
			out, err := a.execTool(ctx, botID, runID, fn.Name, fn.Arguments)
			if err != nil {
				out = "error: " + err.Error()
			}
			out = a.Mask(botID).Apply(out)
			if len(out) > 12000 {
				out = out[:12000] + "\n…truncated"
			}
			a.emit(botID, chatID, runID, "tool_result", out, fn.Name)
			msgs = append(msgs, openai.ToolMessage(out, tc.ID))
		}
	}
	a.emit(botID, chatID, runID, "error", "tool loop limit", "")
	a.finish(botID, chatID, runID, "error")
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

func (a *App) finish(botID, chatID, runID, st string) {
	a.DB.Model(&db.Run{}).Where("id = ?", runID).Update("status", st)
	a.untrackRun(runID)
	a.recomputeStatus(botID)
	a.emit(botID, chatID, runID, "done", st, "")
}

func (a *App) execTool(ctx context.Context, botID, runID, name, argsJSON string) (string, error) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	str := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	id := ids.New()
	var cmd *v1.Cmd
	switch name {
	case "terminal":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{Command: str("command")}}}
	case "exec_python":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_ExecPython{ExecPython: &v1.ExecPythonCmd{Code: str("code")}}}
	case "read":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_FileRead{FileRead: &v1.FileReadCmd{Path: str("path")}}}
	case "write":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_FileWrite{FileWrite: &v1.FileWriteCmd{Path: str("path"), Content: str("content")}}}
	case "patch":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_FilePatch{FilePatch: &v1.FilePatchCmd{Path: str("path"), OldText: str("old_text"), NewText: str("new_text")}}}
	case "grep":
		cmd = &v1.Cmd{Id: id, RunId: runID, Body: &v1.Cmd_Grep{Grep: &v1.GrepCmd{Pattern: str("pattern"), Path: str("path")}}}
	default:
		return "", fmt.Errorf("unknown tool %s", name)
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
	return a.Hub.Exec(ctx, botID, cmd)
}
