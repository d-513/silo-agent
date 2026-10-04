package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/desktop"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
)

func (a *App) execTool(ctx context.Context, botID, chatID, runID, name, argsJSON string) (string, string, error) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	str := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	path := relWorkspace(str("path"))
	if name == "click" || name == "scroll" {
		if err := desktop.CheckPoint(num(args, "x"), num(args, "y")); err != nil {
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
	if name == "sleep" {
		out, err := a.sleepTool(ctx, chatID, runID, args)
		return out, "", err
	}
	if isAgentTool(name) {
		var bot db.Bot
		if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
			return "", "", fmt.Errorf("unknown bot")
		}
		out, err := a.agentTool(ctx, &bot, chatID, runID, name, args)
		return out, "", err
	}
	if _, ok := sharedTools[name]; ok {
		var bot db.Bot
		if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
			return "", "", fmt.Errorf("unknown bot")
		}
		out, err := a.runShared(ctx, &bot, runID, name, args, false)
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
	if _, err := a.AuthorizeAction(ctx, &bot, runID, conn, action, argsJSON, ""); err != nil {
		return "", "", err
	}
	if name == "soul" || name == "core_memory" {
		out, err := a.execDoc(botID, name, args)
		return out, "", err
	}
	if name == "switch_model" {
		out, err := a.models.SwitchTool(chatID, str("model"))
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
		if desktop.WantsChromium(code) {
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
	case "write", "patch", "delete":
		a.markKnowledgeDirty(botID, path)
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
	case "soul", "core_memory":
		return security.Bot, name, true
	case "skill":
		return security.Skills, "load", true
	case "artifact":
		return security.Artifact, "emit", true
	case "web_search":
		return security.Web, "search", true
	case "switch_model":
		return security.Model, "switch", true
	default:
		return "", "", false
	}
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
