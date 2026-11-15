package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// claudeWarning is shown above the provider's settings: what Anthropic allows
// a Claude subscription to be used for, and what the binary tells the model.
const claudeWarning = "For your own use only. This runs Claude on a Claude subscription, and Anthropic does not allow a subscription sign-in or its rate limits to be offered in a product for other people. Use it on an instance only you use, for work you start yourself: not with several people on it, and not for Bots that act unattended (automations, a Heartbeat, channels that answer by themselves). For those, use the Anthropic provider with an API key. Also know that Claude Code tells the model the signed-in account's email address and this server's operating system and working directory, so a Bot on this provider can read them."

// What the binary puts around Silo's prompt is told to the model, where it
// would otherwise contradict it. claudeTools and claudeEnvironment go with a
// request that has tools (an agent's turn; a title or a summary needs
// neither); claudeContext with one that has a part of the prompt that changes
// between runs.
const (
	// claudeTools: the binary lists each tool under its MCP server's name,
	// and Silo's prompt names them bare.
	claudeTools = "Your tools are listed with the prefix `" + claudeToolPrefix + "`: `" + claudeToolPrefix + "read` is the `read` this prompt speaks of, and the same for every tool it names."
	// claudeEnvironment: the binary adds a note on its own process (the
	// control plane's directory, platform and date), which is not the Bot's.
	claudeEnvironment = "A note headed Environment in this conversation (a working directory, a platform, an operating system, today's date) describes the program that relays your messages, not your machine. Your machine, its files and its clock are as this prompt says, and the time is the one on the human's newest message."
	// claudeContext: where the part of Silo's prompt that changes between
	// runs (memories, the notes of one conversation) is put, since the
	// binary caches the system prompt as one block.
	claudeContext = "A message may open with a <silo-context> block. It is from Silo, not from the person, and reads as part of this prompt: what you remember and the notes on this conversation, as they stand now. A newer block replaces an older one."
)

func newClaude(s Settings) (Client, error) {
	dir := s.Get("dir")
	if dir == "" {
		dir = filepath.Join("data", "claude")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &claudeClient{conf: claudeConf{binary: s.Get("binary"), dir: abs, token: s.Get("oauth_token")}}, nil
}

type claudeClient struct {
	conf claudeConf
}

// Stream runs one model turn. A request that names its conversation goes on
// in that conversation's session; any other is a session of its own.
func (c *claudeClient) Stream(ctx context.Context, req Request) (Stream, error) {
	s := &claudeSession{conf: c.conf, once: true}
	if req.Conversation != "" {
		s = claudeSessions.get(c.conf, req.Conversation)
	}
	s.mu.Lock()
	st, err := s.turn(ctx, req)
	if err != nil {
		if s.once {
			s.stop()
		}
		s.mu.Unlock()
		return nil, err
	}
	return st, nil
}

func (c *claudeClient) Complete(ctx context.Context, req Request) (Response, error) {
	st, err := c.Stream(ctx, req)
	if err != nil {
		return Response{}, err
	}
	defer st.Close()
	var res Response
	var text, think strings.Builder
	calls := map[int]*ToolCall{}
	var order []int
	for st.Next() {
		ev := st.Event()
		switch ev.Kind {
		case EventText:
			text.WriteString(ev.Text)
		case EventReasoning:
			think.WriteString(ev.Text)
		case EventToolCallStart:
			calls[ev.Index] = &ToolCall{ID: ev.ToolCallID, Name: ev.ToolName}
			order = append(order, ev.Index)
		case EventToolCallDelta:
			if tc := calls[ev.Index]; tc != nil {
				tc.Arguments += ev.Text
			}
		case EventUsage:
			res.Usage = ev.Usage
		}
	}
	if err := st.Err(); err != nil {
		return Response{}, err
	}
	res.Text, res.Reasoning = text.String(), think.String()
	for _, i := range order {
		res.ToolCalls = append(res.ToolCalls, *calls[i])
	}
	return res, nil
}

// EndTurn hands the session the results of a turn's tools when the run ends
// there, so the binary is not left waiting on them: its turn ends with the
// last result and no model is called.
func (c *claudeClient) EndTurn(ctx context.Context, req Request) error {
	if req.Conversation == "" {
		return nil
	}
	s := claudeSessions.get(c.conf, req.Conversation)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endTurn(ctx, req)
}

// claudeModel is one row of the binary's model list.
type claudeModel struct {
	Value          string   `json:"value"`
	Resolved       string   `json:"resolvedModel"`
	Name           string   `json:"displayName"`
	Description    string   `json:"description"`
	Efforts        []string `json:"supportedEffortLevels"`
	ThinkingOff    bool     `json:"supportsThinkingOff"`
	SupportsEffort bool     `json:"supportsEffort"`
}

type claudeModelList struct {
	at     time.Time
	err    error
	models []claudeModel
}

// claudeModelCache holds the binary's model list per setup, shared by every
// client the CP builds; claudeWindows the context window each model reported
// with its last turn.
var (
	claudeModelCache = struct {
		sync.Mutex
		m map[string]*claudeModelList
	}{m: map[string]*claudeModelList{}}
	claudeWindows sync.Map // model -> int
)

// models asks the binary which models this account may use. It is the list
// its own model picker shows: the aliases that follow the newest release
// (opus, sonnet, haiku, …) and the pinned ids.
func (c *claudeClient) models(ctx context.Context, fresh bool) ([]claudeModel, error) {
	key := c.conf.cacheKey()
	claudeModelCache.Lock()
	defer claudeModelCache.Unlock()
	if l := claudeModelCache.m[key]; l != nil && !fresh {
		if (l.err == nil && time.Since(l.at) < windowTTL) || (l.err != nil && time.Since(l.at) < windowRetry) {
			return l.models, l.err
		}
	}
	l := &claudeModelList{at: time.Now()}
	p, body, err := startClaude(c.conf, claudeLaunch{})
	if err != nil {
		l.err = err
	} else {
		p.kill()
		var init struct {
			Models []claudeModel `json:"models"`
		}
		if err := json.Unmarshal(body, &init); err != nil {
			l.err = fmt.Errorf("read Claude Code's model list: %w", err)
		}
		l.models = init.Models
	}
	claudeModelCache.m[key] = l
	return l.models, l.err
}

// ListModels is the binary's model list. Its "default" row is left out: it is
// one of the others under another name, and which one changes with the plan.
func (c *claudeClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	list, err := c.models(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(list))
	for _, m := range list {
		if m.Value == "" || m.Value == "default" {
			continue
		}
		name := m.Name
		if m.Description != "" {
			name += " · " + m.Description
		}
		info := ModelInfo{ID: m.Value, Name: name, Kind: KindText}
		for _, id := range []string{m.Value, m.Resolved} {
			if w, ok := claudeWindows.Load(id); ok {
				info.ContextWindow = w.(int)
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// ThinkingLevels are the effort levels the binary lists for a model.
func (c *claudeClient) ThinkingLevels(ctx context.Context, model string) ([]string, error) {
	list, err := c.models(ctx, false)
	if err != nil {
		return nil, err
	}
	// The row of that name, else the alias that stands for that release.
	for _, byName := range []bool{true, false} {
		for _, m := range list {
			if (byName && m.Value != model) || (!byName && (m.Resolved != model || m.Value == "default")) {
				continue
			}
			levels := append([]string{}, m.Efforts...)
			if m.ThinkingOff {
				levels = append(levels, ThinkingOff)
			}
			return SortThinking(levels), nil
		}
	}
	return nil, fmt.Errorf("Claude Code does not list %q", model)
}

// ContextWindow is the window the binary reported with the model's last turn;
// before the first one the operator's setting stands in.
func (c *claudeClient) ContextWindow(ctx context.Context, model string) (int, error) {
	if w, ok := claudeWindows.Load(model); ok {
		return w.(int), nil
	}
	return 0, errNoWindow
}

// claudeSystem splits Silo's system prompt for the binary, which caches the
// whole system prompt as one block: the tiers that end in a cache breakpoint
// are the system prompt, and what follows the last one (it changes between
// runs) is handed over inside the conversation. tools says the request has
// tools, so the notes on the binary's own additions go with it.
func claudeSystem(blocks []SystemBlock, tools bool) (stable, volatile string) {
	last := -1
	for i, b := range blocks {
		if b.CacheAfter {
			last = i
		}
	}
	if last < 0 {
		// No breakpoints: a one-off prompt, all of it the system prompt.
		last = len(blocks) - 1
	}
	var st, vo []string
	for i, b := range blocks {
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		if i <= last {
			st = append(st, b.Text)
		} else {
			vo = append(vo, b.Text)
		}
	}
	volatile = strings.Join(vo, "\n\n")
	var notes []string
	if tools {
		notes = append(notes, claudeTools, claudeEnvironment)
	}
	if volatile != "" {
		notes = append(notes, claudeContext)
	}
	if len(notes) > 0 {
		st = append(st, "## How you are run\n"+strings.Join(notes, "\n\n"))
	}
	return strings.TrimSpace(strings.Join(st, "\n\n")), volatile
}

// claudeSig is what the session remembers of one message of Silo's history.
type claudeSig struct {
	Hash string `json:"h"`
	// Image marks a user turn that carried pixels: Silo later replaces such a
	// turn with a line saying the image was seen, which is the same turn.
	Image bool `json:"i,omitempty"`
}

func claudeSigOf(m Message) claudeSig {
	h := sha256.New()
	put := func(s string) {
		fmt.Fprintf(h, "%d:", len(s))
		h.Write([]byte(s))
	}
	put(string(m.Role))
	put(m.Text)
	put(m.ToolCallID)
	for _, tc := range m.ToolCalls {
		put(tc.ID)
		put(tc.Name)
		put(tc.Arguments)
	}
	for _, img := range m.Images {
		put(img.Mime)
		h.Write(img.Data)
	}
	return claudeSig{Hash: hex.EncodeToString(h.Sum(nil)[:12]), Image: m.Role == RoleUser && len(m.Images) > 0}
}

func (g claudeSig) matches(m Message) bool {
	if g.Image && m.Role == RoleUser {
		return true
	}
	return g == claudeSigOf(m)
}

func claudeSigs(msgs []Message) []claudeSig {
	out := make([]claudeSig, len(msgs))
	for i, m := range msgs {
		out[i] = claudeSigOf(m)
	}
	return out
}

func sigText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func claudeToolsSig(tools []Tool) string {
	h := sha256.New()
	for _, t := range tools {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", t.Name, t.Description, t.Parameters)
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// claudeSeed renders a history as the opening message of a new session. The
// binary's own record of a conversation is its to write, so a history it does
// not hold (one compacted, edited, or begun with another model) is told as
// text: the record, then the messages that are still to be answered.
func claudeSeed(msgs []Message) (string, []Image) {
	if len(msgs) == 1 && msgs[0].Role == RoleUser {
		return msgs[0].Text, msgs[0].Images
	}
	// The user turns at the end are the ones being asked now.
	cut := len(msgs)
	for cut > 0 && msgs[cut-1].Role == RoleUser {
		cut--
	}
	var images []Image
	var b strings.Builder
	if cut > 0 {
		names := map[string]string{}
		b.WriteString("[Silo: this conversation continues in a new session. Its record so far follows; tool calls and their results are written out as text. Go on from where it ends.]\n\n<record>\n")
		for _, m := range msgs[:cut] {
			switch m.Role {
			case RoleUser:
				b.WriteString("## user\n" + m.Text + "\n\n")
			case RoleAssistant:
				if strings.TrimSpace(m.Text) != "" {
					b.WriteString("## you\n" + m.Text + "\n\n")
				}
				for _, tc := range m.ToolCalls {
					names[tc.ID] = tc.Name
					b.WriteString("## you called " + tc.Name + "\n" + tc.Arguments + "\n\n")
				}
			case RoleTool:
				b.WriteString("## result of " + names[m.ToolCallID] + "\n" + m.Text + "\n\n")
			}
			images = append(images, m.Images...)
		}
		b.WriteString("</record>\n\n")
	}
	var asks []string
	for _, m := range msgs[cut:] {
		asks = append(asks, m.Text)
		images = append(images, m.Images...)
	}
	if len(asks) == 0 {
		asks = []string{"[Silo: continue from where the record ends.]"}
	}
	b.WriteString(strings.Join(asks, "\n\n"))
	return b.String(), images
}

// claudeUsage is the token count of one API message as the binary relays it.
type claudeUsage struct {
	Input  int `json:"input_tokens"`
	Output int `json:"output_tokens"`
	Read   int `json:"cache_read_input_tokens"`
	Write  int `json:"cache_creation_input_tokens"`
}

// claudeFailure words a failed turn for the thread.
func claudeFailure(result string) error {
	result = strings.TrimSpace(result)
	low := strings.ToLower(result)
	switch {
	case result == "":
		return errors.New("Claude Code ended the turn with an error")
	case strings.Contains(low, "not logged in") || strings.Contains(low, "/login") || strings.Contains(low, "authentication"):
		return fmt.Errorf("Claude is not signed in (Admin → Settings → Models → Providers → Claude Agent): %s", result)
	}
	return fmt.Errorf("Claude Code: %s", result)
}
