// Package claudetest is a stand-in for the Claude Code binary, so the Claude
// provider is tested without the real one, an account or the network. It
// speaks the same lines on stdin and stdout (the stream-json protocol, the
// in-process MCP server, the permission prompt, `auth` and `setup-token`),
// and the last Test_ word in a user message says what the "model" does:
//
//	Test_Call_<tool> <json>   call one of the host's tools, then say its result
//	Test_Two                  call echo twice in one turn, one after the other
//	Test_Dump                 answer with what it was given, as JSON (Dump)
//	Test_Alien                reach for a tool that is not the host's
//	Test_Fail                 end the turn with an error
//	Test_Slow                 write half a reply and wait
//	anything else             "echo: <the message>"
//
// A test package turns its own binary into the fake with Main in an init
// function, and points the provider at Install's script.
package claudetest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const envFake = "SILO_FAKE_CLAUDE"

// GoodCode is the sign-in code the fake accepts; Token what its setup-token
// issues; Email who its account is.
const (
	GoodCode = "good-code"
	Token    = "sk-ant-oat01-FAKEfakeFAKEfakeFAKEfakeFAKEfake0123456789"
	Email    = "owner@example.com"
)

// Dump is what Test_Dump answers with.
type Dump struct {
	System  string   `json:"system"`
	Tools   []string `json:"tools"`
	History []string `json:"history"`
	Model   string   `json:"model"`
	Effort  string   `json:"effort"`
	Session string   `json:"session"`
	Resumed bool     `json:"resumed"`
	Args    []string `json:"args"`
	Env     []string `json:"env"`
	Cwd     string   `json:"cwd"`
}

// Main runs the fake and exits when this process was started as one;
// otherwise it returns at once. Call it from an init function of the test
// package.
func Main() {
	if os.Getenv(envFake) != "1" {
		return
	}
	os.Exit(run(os.Args[1:]))
}

// Install writes a script that runs the test binary as the fake and returns
// its path, for the provider's `binary` setting.
func Install(t testing.TB) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "claude")
	script := fmt.Sprintf("#!/bin/sh\n%s=1 exec %q \"$@\"\n", envFake, self)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func stateDir() string { return os.Getenv("CLAUDE_CONFIG_DIR") }

func run(args []string) int {
	if len(args) >= 2 && args[0] == "auth" {
		return auth(args[1])
	}
	if len(args) >= 1 && args[0] == "setup-token" {
		return setupToken()
	}
	return (&session{args: args}).serve()
}

const page = "https://claude.com/cai/oauth/authorize?code=true&client_id=fake&state=fake-state"

func auth(cmd string) int {
	login := filepath.Join(stateDir(), "fake-login")
	switch cmd {
	case "status":
		st := map[string]any{"loggedIn": false, "authMethod": "none"}
		if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
			st = map[string]any{"loggedIn": true, "authMethod": "oauth_token"}
		} else if _, err := os.Stat(login); err == nil {
			st = map[string]any{"loggedIn": true, "authMethod": "claude.ai", "email": Email, "subscriptionType": "max", "loginExpiresAt": "2030-01-02T03:04:05.000Z"}
		}
		raw, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(raw))
		if st["loggedIn"] == false {
			return 1
		}
		return 0
	case "login":
		fmt.Printf("Opening browser to sign in…\nIf the browser didn't open, visit: %s\nPaste code here if prompted > ", page)
		code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(code) != GoodCode {
			fmt.Println("Login failed: that code is not valid")
			return 1
		}
		_ = os.MkdirAll(stateDir(), 0o700)
		_ = os.WriteFile(login, []byte("x"), 0o600)
		fmt.Println("Login successful.")
		return 0
	case "logout":
		_ = os.Remove(login)
		return 0
	}
	return 2
}

// setupToken draws the way the real one does: only on a terminal, with
// control sequences between the words.
func setupToken() int {
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return 0
	}
	fmt.Printf("\x1b[2J\x1b[1mWelcome\x1b[0m\x1b[3Cto setup\r\n\x1b[36m%s\x1b[0m\r\nPaste code here if prompted > ", page)
	code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(code) != GoodCode {
		fmt.Print("\r\n\x1b[31mOAuth error: Invalid code\x1b[0m\r\n")
		return 1
	}
	fmt.Printf("\r\n\x1b[32mYour OAuth token (valid for 1 year):\x1b[0m\r\n\r\n%s\r\n", Token)
	return 0
}

type line map[string]any

type session struct {
	args    []string
	in      chan line
	out     *bufio.Writer
	dump    Dump
	queued  []string
	n       int
	persist string
}

func flag(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
	}
	return "", false
}

func (s *session) send(v any) {
	raw, _ := json.Marshal(v)
	s.out.Write(raw)
	s.out.WriteByte('\n')
	s.out.Flush()
}

func (s *session) serve() int {
	s.out = bufio.NewWriter(os.Stdout)
	s.dump.Args = s.args
	s.dump.Env = os.Environ()
	s.dump.Cwd, _ = os.Getwd()
	s.dump.Model, _ = flag(s.args, "--model")
	s.dump.Effort, _ = flag(s.args, "--effort")
	if v, ok := flag(s.args, "--thinking"); ok {
		s.dump.Effort = "thinking=" + v
	}
	if id, ok := flag(s.args, "--resume"); ok {
		s.dump.Session, s.dump.Resumed = id, true
		s.persist = filepath.Join(stateDir(), "fake-sessions", id+".json")
		raw, err := os.ReadFile(s.persist)
		if err != nil {
			fmt.Fprintf(os.Stderr, "No conversation found with session ID: %s\n", id)
			return 1
		}
		_ = json.Unmarshal(raw, &s.dump.History)
	} else if id, ok := flag(s.args, "--session-id"); ok {
		s.dump.Session = id
		s.persist = filepath.Join(stateDir(), "fake-sessions", id+".json")
	}
	s.in = make(chan line, 64)
	go func() {
		r := bufio.NewReaderSize(os.Stdin, 1<<20)
		for {
			raw, err := r.ReadBytes('\n')
			var l line
			if json.Unmarshal(raw, &l) == nil && l != nil {
				s.in <- l
			}
			if err != nil {
				close(s.in)
				return
			}
		}
	}()
	for l := range s.in {
		switch l["type"] {
		case "control_request":
			s.control(l)
		case "user":
			s.turn(userText(l))
		}
		// A message that came while the session was still starting.
		for len(s.queued) > 0 {
			text := s.queued[0]
			s.queued = s.queued[1:]
			s.turn(text)
		}
	}
	return 0
}

func userText(l line) string {
	msg, _ := l["message"].(map[string]any)
	switch c := msg["content"].(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, b := range c {
			block, _ := b.(map[string]any)
			switch block["type"] {
			case "text":
				text, _ := block["text"].(string)
				parts = append(parts, text)
			case "image":
				parts = append(parts, "[image]")
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

var models = []map[string]any{
	{"value": "default", "resolvedModel": "claude-opus-5-5", "displayName": "Default (recommended)", "description": "Opus 5.5"},
	{"value": "opus", "resolvedModel": "claude-opus-5-5", "displayName": "Opus", "description": "For complex work", "supportedEffortLevels": []string{"low", "medium", "high", "xhigh", "max"}},
	{"value": "haiku", "resolvedModel": "claude-haiku-5-5", "displayName": "Haiku", "description": "Fastest", "supportedEffortLevels": []string{"low", "medium", "high"}, "supportsThinkingOff": true},
	{"value": "claude-sonnet-5", "resolvedModel": "claude-sonnet-5", "displayName": "Sonnet 5", "description": "Efficient"},
}

func (s *session) control(l line) {
	id, _ := l["request_id"].(string)
	req, _ := l["request"].(map[string]any)
	switch req["subtype"] {
	case "initialize":
		if sys, ok := req["systemPrompt"].([]any); ok {
			var parts []string
			for _, p := range sys {
				text, _ := p.(string)
				parts = append(parts, text)
			}
			s.dump.System = strings.Join(parts, "\n\n")
		}
		s.send(line{"type": "control_response", "response": line{"subtype": "success", "request_id": id, "response": line{"models": models, "commands": []any{}}}})
		names := []string{}
		if servers, _ := req["sdkMcpServers"].([]any); len(servers) > 0 {
			s.mcp("initialize", line{"protocolVersion": "2025-11-25", "capabilities": line{}})
			res := s.mcp("tools/list", nil)
			tools, _ := res["tools"].([]any)
			for _, t := range tools {
				tool, _ := t.(map[string]any)
				name, _ := tool["name"].(string)
				names = append(names, "mcp__silo__"+name)
				s.dump.Tools = append(s.dump.Tools, name)
			}
		}
		// A real binary started without --tools "" would offer its own.
		if v, ok := flag(s.args, "--tools"); !ok || v != "" {
			names = append(names, "Bash")
		}
		s.send(line{"type": "system", "subtype": "init", "session_id": s.dump.Session, "tools": names})
	default:
		s.send(line{"type": "control_response", "response": line{"subtype": "success", "request_id": id, "response": line{}}})
	}
}

// ask sends a control request and waits for its answer; messages that come
// meanwhile are kept for the turn.
func (s *session) ask(request line) map[string]any {
	s.n++
	id := fmt.Sprintf("req-%d", s.n)
	s.send(line{"type": "control_request", "request_id": id, "request": request})
	for l := range s.in {
		if l["type"] == "user" {
			s.queued = append(s.queued, userText(l))
			continue
		}
		res, _ := l["response"].(map[string]any)
		if l["type"] == "control_response" && res["request_id"] == id {
			body, _ := res["response"].(map[string]any)
			return body
		}
		if l["type"] == "control_request" {
			s.control(l)
		}
	}
	os.Exit(0)
	return nil
}

func (s *session) mcp(method string, params any) map[string]any {
	s.n++
	msg := line{"jsonrpc": "2.0", "id": s.n, "method": method}
	if params != nil {
		msg["params"] = params
	}
	body := s.ask(line{"subtype": "mcp_message", "server_name": "silo", "message": msg})
	res, _ := body["mcp_response"].(map[string]any)
	result, _ := res["result"].(map[string]any)
	return result
}

func (s *session) event(ev line) {
	s.send(line{"type": "stream_event", "event": ev, "parent_tool_use_id": nil})
}

func (s *session) start() {
	s.event(line{"type": "message_start", "message": line{"usage": line{"input_tokens": 3, "cache_read_input_tokens": 100, "cache_creation_input_tokens": 20}}})
}

func (s *session) stop(reason string) {
	s.event(line{"type": "message_delta", "delta": line{"stop_reason": reason}, "usage": line{"output_tokens": 7}})
	s.event(line{"type": "message_stop"})
}

func (s *session) say(text string) {
	s.start()
	s.event(line{"type": "content_block_start", "index": 0, "content_block": line{"type": "text", "text": ""}})
	// Two pieces, so a reader that drops one shows.
	half := len(text) / 2
	for _, piece := range []string{text[:half], text[half:]} {
		s.event(line{"type": "content_block_delta", "index": 0, "delta": line{"type": "text_delta", "text": piece}})
	}
	s.event(line{"type": "content_block_stop", "index": 0})
	s.stop("end_turn")
}

func (s *session) result(text string, failed bool) {
	s.send(line{
		"type": "result", "subtype": "success", "is_error": failed, "result": text, "session_id": s.dump.Session,
		"modelUsage": line{"claude-opus-5-5": line{"contextWindow": 400000}},
	})
}

type call struct {
	id, name, args string
}

// calls writes one API message that calls tools, runs them one after another
// through the host, and reports what each answered. ended says a result asked
// for the turn to end there.
func (s *session) calls(list []call) (results []string, ended bool) {
	s.start()
	for i, c := range list {
		s.event(line{"type": "content_block_start", "index": i, "content_block": line{"type": "tool_use", "id": c.id, "name": c.name, "input": line{}}})
		s.event(line{"type": "content_block_delta", "index": i, "delta": line{"type": "input_json_delta", "partial_json": ""}})
		s.event(line{"type": "content_block_delta", "index": i, "delta": line{"type": "input_json_delta", "partial_json": c.args}})
		s.event(line{"type": "content_block_stop", "index": i})
	}
	s.stop("tool_use")
	for _, c := range list {
		var input any
		_ = json.Unmarshal([]byte(c.args), &input)
		verdict := s.ask(line{"subtype": "can_use_tool", "tool_name": c.name, "input": input, "tool_use_id": c.id})
		if verdict["behavior"] != "allow" {
			results = append(results, "denied")
			s.send(line{"type": "user", "message": line{"role": "user", "content": []line{{"type": "tool_result", "tool_use_id": c.id, "content": "denied"}}}})
			continue
		}
		res := s.mcp("tools/call", line{"name": strings.TrimPrefix(c.name, "mcp__silo__"), "arguments": input, "_meta": line{"claudecode/toolUseId": c.id}})
		text := ""
		if content, _ := res["content"].([]any); len(content) > 0 {
			block, _ := content[0].(map[string]any)
			text, _ = block["text"].(string)
		}
		if meta, _ := res["_meta"].(map[string]any); meta["claude/endTurn"] == true {
			ended = true
		}
		results = append(results, text)
		s.send(line{"type": "user", "message": line{"role": "user", "content": []line{{"type": "tool_result", "tool_use_id": c.id, "content": text}}}})
	}
	return results, ended
}

var word = regexp.MustCompile(`Test_[A-Za-z]+(_[A-Za-z0-9_-]+)?`)

func (s *session) turn(text string) {
	s.dump.History = append(s.dump.History, text)
	defer s.save()
	all := word.FindAllStringIndex(text, -1)
	cmd, rest := "", ""
	if len(all) > 0 {
		last := all[len(all)-1]
		cmd, rest = text[last[0]:last[1]], strings.TrimSpace(text[last[1]:])
	}
	s.n++
	id := fmt.Sprintf("toolu_%s_%d", strings.ReplaceAll(s.dump.Session, "-", "")[:min(6, len(strings.ReplaceAll(s.dump.Session, "-", "")))], s.n)
	finish := func(results []string, ended bool) {
		if ended {
			s.result("", false)
			return
		}
		reply := "result: " + strings.Join(results, " | ")
		if len(s.queued) > 0 {
			reply += " queued: " + strings.Join(s.queued, " | ")
			s.dump.History = append(s.dump.History, s.queued...)
			s.queued = nil
		}
		s.say(reply)
		s.result(reply, false)
	}
	switch {
	case strings.HasPrefix(cmd, "Test_Call_"):
		args := rest
		if i := strings.IndexByte(args, '\n'); i >= 0 {
			args = args[:i]
		}
		if !json.Valid([]byte(args)) {
			args = "{}"
		}
		finish(s.calls([]call{{id, "mcp__silo__" + strings.TrimPrefix(cmd, "Test_Call_"), args}}))
	case cmd == "Test_Two":
		finish(s.calls([]call{{id + "a", "mcp__silo__echo", `{"text":"first"}`}, {id + "b", "mcp__silo__echo", `{"text":"second"}`}}))
	case cmd == "Test_Alien":
		finish(s.calls([]call{{id, "Bash", `{"command":"id"}`}}))
	case cmd == "Test_Fail":
		s.result("API Error: 500 boom", true)
	case cmd == "Test_Slow":
		s.start()
		s.event(line{"type": "content_block_start", "index": 0, "content_block": line{"type": "text", "text": ""}})
		s.event(line{"type": "content_block_delta", "index": 0, "delta": line{"type": "text_delta", "text": "half a "}})
		for l := range s.in {
			if l["type"] == "control_request" {
				s.control(l)
			}
		}
	case cmd == "Test_Dump":
		raw, _ := json.Marshal(s.dump)
		s.say(string(raw))
		s.result(string(raw), false)
	default:
		s.say("echo: " + text)
		s.result("echo: "+text, false)
	}
}

func (s *session) save() {
	if s.persist == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.persist), 0o700)
	raw, _ := json.Marshal(s.dump.History)
	_ = os.WriteFile(s.persist, raw, 0o600)
}
