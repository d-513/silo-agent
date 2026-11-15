package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The Claude provider runs the official Claude Code binary the way Anthropic's
// Agent SDK does: `claude --input-format stream-json --output-format
// stream-json`, one JSON object per line each way. Besides the conversation,
// the lines carry control requests; the ones that matter here are the
// binary's `mcp_message` (JSON-RPC to the one in-process MCP server that
// holds Silo's tools) and `can_use_tool` (its permission prompt).
//
// The binary never runs anything. Its built-in tools are off (--tools ""), no
// settings, plugins, skills or other MCP servers are loaded, and every tool it
// can call is one of Silo's: a `tools/call` is parked until the agent loop has
// run the tool through its own engine (rules, approvals, the output filter)
// and hands the result back with the next request.

const (
	// claudeServer is the name of the MCP server that holds Silo's tools, so
	// the model knows each as mcp__silo__<name>.
	claudeServer     = "silo"
	claudeToolPrefix = "mcp__" + claudeServer + "__"
	// claudeToolWait is how long the binary may wait on one tool: an approval
	// can wait on a human for days.
	claudeToolWait = 7 * 24 * time.Hour
	claudeInitWait = 90 * time.Second
)

// claudeConf is where the binary is and where it keeps its state.
type claudeConf struct {
	binary string
	dir    string
	token  string
}

// path finds the binary: the configured path, else claude on PATH. A relative
// path is read against the control plane's directory, not the one the binary
// runs in.
func (c claudeConf) path() (string, error) {
	name := c.binary
	if name == "" {
		name = "claude"
	}
	if strings.ContainsRune(name, filepath.Separator) {
		abs, err := filepath.Abs(name)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("the Claude Code program is not at %s (providers.claude.binary)", abs)
		}
		return abs, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("the Claude Code program %q is not on this server's PATH; install it or set providers.claude.binary", name)
	}
	return filepath.Abs(p)
}

func (c claudeConf) configDir() string { return filepath.Join(c.dir, "config") }
func (c claudeConf) ledgerDir() string { return filepath.Join(c.dir, "sessions") }
func (c claudeConf) workDir() string   { return filepath.Join(c.dir, "work") }
func (c claudeConf) homeDir() string   { return filepath.Join(c.dir, "home") }
func (c claudeConf) cacheKey() string  { return c.binary + "\x00" + c.dir + "\x00" + sigText(c.token) }
func (c claudeConf) hasToken() bool    { return strings.TrimSpace(c.token) != "" }
func (c claudeConf) mkdirs() error {
	for _, d := range []string{c.configDir(), c.ledgerDir(), c.workDir(), c.homeDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// claudePassEnv are the only variables of the control plane the binary sees:
// what it needs to run and to reach the network. Nothing else is inherited,
// so no key, token or harness setting of the host leaks into it.
var claudePassEnv = []string{
	"PATH", "LANG", "LC_ALL", "TZ", "TMPDIR",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NIX_SSL_CERT_FILE", "NIX_LD", "NIX_LD_LIBRARY_PATH",
}

func (c claudeConf) env() []string {
	env := []string{
		"HOME=" + c.homeDir(),
		"CLAUDE_CONFIG_DIR=" + c.configDir(),
		"CLAUDE_AGENT_SDK_CLIENT_APP=silo-agent",
		"DISABLE_AUTOUPDATER=1",
		"DISABLE_INSTALLATION_CHECKS=1",
		// Silo decides when a conversation is compacted and what the Bot
		// remembers; the binary's own versions of both stay off.
		"DISABLE_AUTO_COMPACT=1",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		fmt.Sprintf("MCP_TOOL_TIMEOUT=%d", claudeToolWait.Milliseconds()),
	}
	// A sign-in must not open a browser on the server: the operator opens
	// the page where they are.
	if t, err := exec.LookPath("true"); err == nil {
		env = append(env, "BROWSER="+t)
	}
	for _, k := range claudePassEnv {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	if c.hasToken() {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+strings.TrimSpace(c.token))
	}
	return env
}

// command builds one run of the binary in its own empty directory.
func (c claudeConf) command(args ...string) (*exec.Cmd, error) {
	bin, err := c.path()
	if err != nil {
		return nil, err
	}
	if err := c.mkdirs(); err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = c.workDir()
	cmd.Env = c.env()
	return cmd, nil
}

// claudeLaunch is what one process is started with.
type claudeLaunch struct {
	model  string
	effort string // a thinking level, "" for the model's default
	system string
	tools  []Tool
	// session is the binary's session id; resume continues it, otherwise it
	// is made. Empty keeps nothing on disk.
	session string
	resume  bool
}

func (l claudeLaunch) args() []string {
	args := []string{
		"--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
		"--include-partial-messages",
		// Every tool call asks this process first; only Silo's are let through.
		"--permission-prompt-tool=stdio",
		// No built-in tool, no settings file, no MCP server but ours, no skills.
		"--tools", "",
		"--strict-mcp-config",
		"--setting-sources", "",
		"--disable-slash-commands",
		"--thinking-display", "summarized",
	}
	if len(l.tools) > 0 {
		cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{claudeServer: map[string]string{"type": "sdk", "name": claudeServer}}})
		args = append(args, "--mcp-config", string(cfg))
	}
	if l.model != "" {
		args = append(args, "--model", l.model)
	}
	switch l.effort {
	case "":
	case ThinkingOff:
		args = append(args, "--thinking=disabled")
	case ThinkingMinimal:
		args = append(args, "--effort", ThinkingLow)
	default:
		args = append(args, "--effort", l.effort)
	}
	switch {
	case l.session == "":
		args = append(args, "--no-session-persistence")
	case l.resume:
		args = append(args, "--resume", l.session)
	default:
		args = append(args, "--session-id", l.session)
	}
	return args
}

// claudeLine is one line of the binary's output, as far as this client reads it.
type claudeLine struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
	Event     json.RawMessage `json:"event"`
	Message   json.RawMessage `json:"message"`
	// A result line.
	IsError    bool   `json:"is_error"`
	Result     string `json:"result"`
	ModelUsage map[string]struct {
		ContextWindow int `json:"contextWindow"`
	} `json:"modelUsage"`
	// The init line.
	Tools []string `json:"tools"`
}

// parkedCall is a tool call the binary is waiting on.
type parkedCall struct {
	request string          // the control request to answer
	rpcID   json.RawMessage // the JSON-RPC id inside it
}

// claudeProc is one running binary.
type claudeProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	wmu    sync.Mutex
	tools  []Tool
	stderr *tailBuffer

	mu sync.Mutex
	// queue holds the lines a turn reads; signal wakes its reader.
	queue  []claudeLine
	signal chan struct{}
	// parked are the tool calls waiting on Silo, by tool_use id; answered the
	// ones Silo answered; own the ones the binary answered itself (a call it
	// refused before asking), which Silo's history knows nothing about.
	parked   map[string]parkedCall
	answered map[string]bool
	own      map[string]bool
	replies  map[string]chan claudeReply
	// foreign names a tool that is not Silo's, seen offered or called.
	foreign string
	done    chan struct{}
	exit    error
}

type claudeReply struct {
	body json.RawMessage
	err  string
}

// startClaude starts the binary and its reader, and initializes the session.
func startClaude(conf claudeConf, l claudeLaunch) (*claudeProc, json.RawMessage, error) {
	cmd, err := conf.command(l.args()...)
	if err != nil {
		return nil, nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	p := &claudeProc{
		cmd: cmd, stdin: stdin, tools: l.tools, stderr: &tailBuffer{max: 4096},
		signal: make(chan struct{}, 1), done: make(chan struct{}),
		parked: map[string]parkedCall{}, answered: map[string]bool{}, own: map[string]bool{},
		replies: map[string]chan claudeReply{},
	}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start Claude Code: %w", err)
	}
	go p.read(stdout)

	init := map[string]any{
		"subtype": "initialize",
		// Sent every launch and never recorded by the binary: a Bot whose
		// prompt changed (its SOUL, a new connector) is told so on its next run.
		"systemPrompt":         []string{l.system},
		"systemPromptSnapshot": false,
		// A title given here keeps the binary from asking a model for one.
		"title": "Silo",
	}
	if len(l.tools) > 0 {
		init["sdkMcpServers"] = []string{claudeServer}
		init["sdkMcpServerConfigs"] = map[string]any{claudeServer: map[string]any{"timeout": claudeToolWait.Milliseconds()}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeInitWait)
	defer cancel()
	body, err := p.control(ctx, init)
	if err != nil {
		p.kill()
		return nil, nil, fmt.Errorf("Claude Code did not start: %w", p.why(err))
	}
	return p, body, nil
}

// why adds what the binary wrote on stderr to an error about it.
func (p *claudeProc) why(err error) error {
	if tail := strings.TrimSpace(p.stderr.String()); tail != "" {
		return fmt.Errorf("%w: %s", err, lastLines(tail, 3))
	}
	return err
}

func (p *claudeProc) send(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.stdin.Write(append(raw, '\n'))
	return err
}

// control sends a control request and waits for the binary's answer.
func (p *claudeProc) control(ctx context.Context, request map[string]any) (json.RawMessage, error) {
	id := newUUID()
	ch := make(chan claudeReply, 1)
	p.mu.Lock()
	p.replies[id] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.replies, id)
		p.mu.Unlock()
	}()
	if err := p.send(map[string]any{"type": "control_request", "request_id": id, "request": request}); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.err != "" {
			return nil, errors.New(r.err)
		}
		return r.body, nil
	case <-p.done:
		return nil, p.exited()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *claudeProc) exited() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exit != nil {
		return fmt.Errorf("Claude Code stopped: %w", p.exit)
	}
	return errors.New("Claude Code stopped")
}

func (p *claudeProc) dead() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *claudeProc) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.stdin.Close()
}

// userMessage hands the binary the human's next message.
func (p *claudeProc) userMessage(text string, images []Image) error {
	var content any = text
	if len(images) > 0 {
		blocks := []map[string]any{{"type": "text", "text": text}}
		for _, img := range images {
			blocks = append(blocks, claudeImage(img))
		}
		content = blocks
	}
	return p.send(map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": content},
		"parent_tool_use_id": nil,
		"session_id":         "",
	})
}

// answer gives a parked tool call its result. last ends the binary's turn
// there, with no further model call.
func (p *claudeProc) answer(ctx context.Context, toolUseID, text string, last bool) error {
	call, err := p.waitParked(ctx, toolUseID)
	if err != nil {
		return err
	}
	if text == "" {
		text = "(no output)"
	}
	result := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
	if last {
		result["_meta"] = map[string]any{"claude/endTurn": true}
	}
	p.mu.Lock()
	p.answered[toolUseID] = true
	delete(p.parked, toolUseID)
	p.mu.Unlock()
	return p.reply(call.request, map[string]any{"mcp_response": map[string]any{"jsonrpc": "2.0", "id": call.rpcID, "result": result}})
}

// errClaudeDiverged says the binary's conversation is no longer the one Silo
// holds, so the session cannot go on and is started again from Silo's history.
var errClaudeDiverged = errors.New("claude session diverged")

// waitParked waits for the binary to ask for a tool's result. The binary runs
// a turn's calls one after another, so a later call is asked for only once
// the one before it is answered.
func (p *claudeProc) waitParked(ctx context.Context, id string) (parkedCall, error) {
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	for {
		p.mu.Lock()
		call, ok := p.parked[id]
		own := p.own[id]
		p.mu.Unlock()
		switch {
		case ok:
			return call, nil
		case own:
			return parkedCall{}, errClaudeDiverged
		}
		select {
		case <-p.signal:
			// The turn's reader is not running while results are handed
			// over, so the signal is ours; pass it on in case it is.
			p.wake()
		case <-time.After(50 * time.Millisecond):
		case <-p.done:
			return parkedCall{}, errClaudeDiverged
		case <-deadline.C:
			return parkedCall{}, errClaudeDiverged
		case <-ctx.Done():
			return parkedCall{}, ctx.Err()
		}
	}
}

func (p *claudeProc) reply(requestID string, response any) error {
	return p.send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": requestID, "response": response}})
}

func (p *claudeProc) wake() {
	select {
	case p.signal <- struct{}{}:
	default:
	}
}

// next is the next line of the conversation, waiting for one.
func (p *claudeProc) next(ctx context.Context) (claudeLine, error) {
	for {
		p.mu.Lock()
		if len(p.queue) > 0 {
			l := p.queue[0]
			p.queue = p.queue[1:]
			p.mu.Unlock()
			return l, nil
		}
		p.mu.Unlock()
		select {
		case <-p.signal:
		case <-p.done:
			// Lines read before the exit are still handed out.
			p.mu.Lock()
			n := len(p.queue)
			p.mu.Unlock()
			if n == 0 {
				return claudeLine{}, p.why(p.exited())
			}
		case <-ctx.Done():
			return claudeLine{}, ctx.Err()
		}
	}
}

// drain drops lines left over from before a turn (the tail of the last one).
func (p *claudeProc) drain() {
	p.mu.Lock()
	p.queue = nil
	p.mu.Unlock()
}

func (p *claudeProc) read(stdout io.Reader) {
	r := bufio.NewReaderSize(stdout, 1<<20)
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 {
			var l claudeLine
			if json.Unmarshal(raw, &l) == nil && l.Type != "" {
				p.handle(l)
			}
		}
		if err != nil {
			break
		}
	}
	exit := p.cmd.Wait()
	p.mu.Lock()
	p.exit = exit
	p.mu.Unlock()
	close(p.done)
	p.wake()
}

func (p *claudeProc) handle(l claudeLine) {
	switch l.Type {
	case "control_request":
		p.request(l)
		return
	case "control_response":
		var r struct {
			Subtype   string          `json:"subtype"`
			RequestID string          `json:"request_id"`
			Response  json.RawMessage `json:"response"`
			Error     string          `json:"error"`
		}
		_ = json.Unmarshal(l.Response, &r)
		p.mu.Lock()
		ch := p.replies[r.RequestID]
		p.mu.Unlock()
		if ch != nil {
			reply := claudeReply{body: r.Response}
			if r.Subtype != "success" {
				reply.err = r.Error
				if reply.err == "" {
					reply.err = "request refused"
				}
			}
			ch <- reply
		}
		return
	case "system":
		if l.Subtype == "init" {
			for _, name := range l.Tools {
				if !strings.HasPrefix(name, claudeToolPrefix) {
					p.alien(name)
				}
			}
		}
	case "user":
		// A tool result the binary wrote itself, for a call Silo never saw.
		for _, id := range claudeResultIDs(l.Message) {
			p.mu.Lock()
			if !p.answered[id] {
				p.own[id] = true
			}
			p.mu.Unlock()
		}
	}
	p.mu.Lock()
	p.queue = append(p.queue, l)
	p.mu.Unlock()
	p.wake()
}

// alien records a tool that is not Silo's and stops the binary: it must never
// be offered one, and a model must never get to call one.
func (p *claudeProc) alien(name string) {
	p.mu.Lock()
	if p.foreign == "" {
		p.foreign = name
	}
	p.mu.Unlock()
	p.kill()
}

func (p *claudeProc) alienTool() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.foreign
}

// request answers one control request of the binary.
func (p *claudeProc) request(l claudeLine) {
	var req struct {
		Subtype    string          `json:"subtype"`
		ServerName string          `json:"server_name"`
		Message    json.RawMessage `json:"message"`
		ToolName   string          `json:"tool_name"`
		Input      json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(l.Request, &req)
	switch req.Subtype {
	case "mcp_message":
		p.mcp(l.RequestID, req.Message)
	case "can_use_tool":
		if strings.HasPrefix(req.ToolName, claudeToolPrefix) {
			input := req.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			_ = p.reply(l.RequestID, map[string]any{"behavior": "allow", "updatedInput": input})
			return
		}
		_ = p.reply(l.RequestID, map[string]any{"behavior": "deny", "message": "This tool is not available here."})
		p.alien(req.ToolName)
	default:
		_ = p.reply(l.RequestID, map[string]any{})
	}
}

// mcp is the in-process MCP server: it lists Silo's tools and parks calls.
func (p *claudeProc) mcp(requestID string, raw json.RawMessage) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Name            string         `json:"name"`
			Meta            map[string]any `json:"_meta"`
		} `json:"params"`
	}
	_ = json.Unmarshal(raw, &msg)
	respond := func(result any) {
		_ = p.reply(requestID, map[string]any{"mcp_response": map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result}})
	}
	switch {
	case len(msg.ID) == 0:
		// A notification takes no answer, but the control request does.
		_ = p.reply(requestID, map[string]any{"mcp_response": map[string]any{"jsonrpc": "2.0", "id": 0, "result": map[string]any{}}})
	case msg.Method == "initialize":
		respond(map[string]any{
			"protocolVersion": msg.Params.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": claudeServer, "version": "1"},
		})
	case msg.Method == "tools/list":
		list := make([]map[string]any, 0, len(p.tools))
		for _, t := range p.tools {
			schema := t.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema})
		}
		respond(map[string]any{"tools": list})
	case msg.Method == "tools/call":
		id, _ := msg.Params.Meta["claudecode/toolUseId"].(string)
		if id == "" {
			respond(map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "tool call without an id"}}})
			return
		}
		p.mu.Lock()
		p.parked[id] = parkedCall{request: requestID, rpcID: msg.ID}
		p.mu.Unlock()
		p.wake()
	default:
		respond(map[string]any{})
	}
}

// claudeResultIDs are the tool_use ids a user line of the binary carries
// results for.
func claudeResultIDs(message json.RawMessage) []string {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &m) != nil {
		return nil
	}
	var blocks []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
	}
	if json.Unmarshal(m.Content, &blocks) != nil {
		return nil
	}
	var ids []string
	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID != "" {
			ids = append(ids, b.ToolUseID)
		}
	}
	return ids
}

func claudeImage(img Image) map[string]any {
	mime := img.Mime
	if mime == "" {
		mime = "image/png"
	}
	return map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": img.Data}}
}

// tailBuffer keeps the end of what was written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.buf), "")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " · ")
}
