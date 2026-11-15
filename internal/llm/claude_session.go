package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A claudeSession is one conversation as the binary holds it. Silo's history
// stays the record: the session is a copy the binary keeps so that a turn
// costs one message instead of the whole history, and so its prompt cache
// holds. Each request is checked against what the session was last sent. One
// that only adds to it (the results of the last turn's tools, new messages)
// goes on in the same session; anything else (a compaction, an edit, turns
// another model wrote, a session lost) starts a new one from Silo's history.
type claudeSession struct {
	mu   sync.Mutex
	conf claudeConf
	key  string
	// once marks a call on its own: nothing is kept and the process ends
	// with the turn.
	once bool

	// id is the binary's session id; started says it has a record under it.
	id      string
	started bool
	proc    *claudeProc
	// launch is what the process was started with: a change of model,
	// thinking level, tools or system prompt needs a new process.
	launch string
	// sent is the history the session holds, as Silo knows it; pending the
	// tool calls of its last turn that still wait for their results.
	sent    []claudeSig
	pending []string
	// volatile is the changing part of the system prompt it was last told.
	volatile string
	// dirty says the binary's record no longer matches sent.
	dirty bool
	used  time.Time
}

// claudePlan is how a request continues a session.
type claudePlan struct {
	fresh   bool
	results map[string]string
	users   []Message
}

// plan reads a request's history against the session's.
func (s *claudeSession) plan(msgs []Message) claudePlan {
	fresh := claudePlan{fresh: true}
	if s.once || !s.started || s.dirty || s.id == "" || len(msgs) < len(s.sent) {
		return fresh
	}
	for i, g := range s.sent {
		if !g.matches(msgs[i]) {
			return fresh
		}
	}
	rest := msgs[len(s.sent):]
	// The last turn, as Silo kept it (a reply can be several messages).
	for len(rest) > 0 && rest[0].Role == RoleAssistant {
		rest = rest[1:]
	}
	p := claudePlan{results: map[string]string{}}
	for _, m := range rest {
		switch {
		case m.Role == RoleTool && len(p.users) == 0:
			p.results[m.ToolCallID] = m.Text
		case m.Role == RoleUser:
			p.users = append(p.users, m)
		default:
			return fresh
		}
	}
	if len(p.results) != len(s.pending) {
		return fresh
	}
	for _, id := range s.pending {
		if _, ok := p.results[id]; !ok {
			return fresh
		}
	}
	if len(s.pending) == 0 && len(p.users) == 0 {
		return fresh
	}
	// Calls parked in a process that is gone cannot be answered any more.
	if len(s.pending) > 0 && (s.proc == nil || s.proc.dead()) {
		return fresh
	}
	return p
}

// stop ends the session's process.
func (s *claudeSession) stop() {
	if s.proc != nil {
		s.proc.kill()
		s.proc = nil
	}
}

// reset forgets the binary's side: the next turn makes a new session.
func (s *claudeSession) reset() {
	s.stop()
	s.id, s.started, s.dirty = "", false, false
	s.sent, s.pending, s.volatile = nil, nil, ""
}

// ensure has a process running for this request.
func (s *claudeSession) ensure(req Request, stable string) error {
	launch := strings.Join([]string{req.Model, req.Thinking, claudeToolsSig(req.Tools), sigText(stable)}, "|")
	if s.proc != nil && (s.proc.dead() || s.launch != launch) {
		if len(s.pending) > 0 {
			return errClaudeDiverged
		}
		s.stop()
	}
	if s.proc != nil {
		return nil
	}
	l := claudeLaunch{model: req.Model, effort: req.Thinking, system: stable, tools: req.Tools}
	if !s.once {
		if s.id == "" {
			s.id = newUUID()
		}
		l.session, l.resume = s.id, s.started
	}
	p, _, err := startClaude(s.conf, l)
	if err != nil {
		return err
	}
	if name := p.alienTool(); name != "" {
		p.kill()
		return fmt.Errorf("Claude Code offered a tool that is not Silo's (%s); the turn was stopped", name)
	}
	s.proc, s.launch = p, launch
	return nil
}

// deliver hands the process what the request adds.
func (s *claudeSession) deliver(ctx context.Context, req Request, plan claudePlan, volatile string) error {
	p := s.proc
	p.drain()
	var text string
	var images []Image
	switch {
	case plan.fresh:
		text, images = claudeSeed(req.Messages)
	case len(plan.users) > 0:
		// One message, however many came: each message handed to the binary
		// on its own would be a turn of its own.
		var parts []string
		for _, m := range plan.users {
			parts = append(parts, m.Text)
			images = append(images, m.Images...)
		}
		text = strings.Join(parts, "\n\n")
	}
	if plan.fresh || len(plan.users) > 0 {
		if sig := sigText(volatile); volatile != "" && sig != s.volatile {
			text = "<silo-context>\n" + volatile + "\n</silo-context>\n\n" + text
			s.volatile = sig
		}
		// Before the results: a message that waits is read with them.
		if err := p.userMessage(text, images); err != nil {
			return errClaudeDiverged
		}
	}
	for _, id := range s.pending {
		if err := p.answer(ctx, id, plan.results[id], false); err != nil {
			return err
		}
	}
	return nil
}

// turn starts one model turn and returns its stream; the session stays locked
// until the stream is closed.
func (s *claudeSession) turn(ctx context.Context, req Request) (Stream, error) {
	stable, volatile := claudeSystem(req.System, len(req.Tools) > 0)
	plan := s.plan(req.Messages)
	for try := 0; ; try++ {
		if plan.fresh {
			s.reset()
		}
		err := s.ensure(req, stable)
		if err == nil {
			err = s.deliver(ctx, req, plan, volatile)
		}
		if err == nil {
			break
		}
		// A session that cannot go on is started again from Silo's history,
		// once: the same failure on a new session is the provider's.
		if !plan.fresh && ctx.Err() == nil {
			plan = claudePlan{fresh: true}
			continue
		}
		s.dirty = true
		s.stop()
		return nil, err
	}
	s.used = time.Now()
	return &claudeStream{s: s, p: s.proc, ctx: ctx, req: req}, nil
}

// endTurn answers the last turn's parked tool calls and ends the binary's
// turn with the last of them.
func (s *claudeSession) endTurn(ctx context.Context, req Request) error {
	if len(s.pending) == 0 {
		return nil
	}
	plan := s.plan(req.Messages)
	giveUp := func() error {
		s.dirty = true
		s.stop()
		s.save()
		return nil
	}
	if plan.fresh {
		return giveUp()
	}
	p := s.proc
	p.drain()
	for i, id := range s.pending {
		if err := p.answer(ctx, id, plan.results[id], i == len(s.pending)-1); err != nil {
			return giveUp()
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		line, err := p.next(ctx)
		if err != nil {
			return giveUp()
		}
		if line.Type == "result" {
			break
		}
	}
	// The session now holds the history up to the last result; what Silo
	// added after it (an image it showed) goes with the next message.
	end := len(req.Messages)
	for end > 0 && req.Messages[end-1].Role != RoleTool {
		end--
	}
	s.sent, s.pending, s.used = claudeSigs(req.Messages[:end]), nil, time.Now()
	s.save()
	return nil
}

// claudeLedger is a session as it is kept on disk, so a restarted control
// plane resumes the binary's session instead of telling the history again.
type claudeLedger struct {
	Key      string      `json:"key"`
	ID       string      `json:"id"`
	Sent     []claudeSig `json:"sent"`
	Pending  []string    `json:"pending,omitempty"`
	Volatile string      `json:"volatile,omitempty"`
	Dirty    bool        `json:"dirty,omitempty"`
}

func (s *claudeSession) ledgerPath() string {
	return filepath.Join(s.conf.ledgerDir(), sigText(s.key)+".json")
}

func (s *claudeSession) save() {
	if s.once {
		return
	}
	raw, err := json.Marshal(claudeLedger{Key: s.key, ID: s.id, Sent: s.sent, Pending: s.pending, Volatile: s.volatile, Dirty: s.dirty})
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.conf.ledgerDir(), 0o700); err != nil {
		return
	}
	tmp := s.ledgerPath() + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, s.ledgerPath())
	}
}

func (s *claudeSession) load() {
	raw, err := os.ReadFile(s.ledgerPath())
	if err != nil {
		return
	}
	var l claudeLedger
	if json.Unmarshal(raw, &l) != nil || l.Key != s.key || l.ID == "" {
		return
	}
	s.id, s.started, s.sent, s.volatile = l.ID, true, l.Sent, l.Volatile
	// Calls that were parked died with the process that held them.
	s.dirty = l.Dirty || len(l.Pending) > 0
}

// claudeIdle is how long a process with nothing to do is kept (its session
// resumes from disk); claudeParked how long one that waits on tool results.
const (
	claudeIdle   = 15 * time.Minute
	claudeParked = 2 * time.Hour
	claudeForget = 24 * time.Hour
)

type claudeRegistry struct {
	mu      sync.Mutex
	m       map[string]*claudeSession
	reaping bool
}

var claudeSessions = &claudeRegistry{m: map[string]*claudeSession{}}

func (r *claudeRegistry) get(conf claudeConf, key string) *claudeSession {
	k := conf.dir + "\x00" + key
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.m[k]
	if s == nil {
		s = &claudeSession{conf: conf, key: key, used: time.Now()}
		s.load()
		r.m[k] = s
	}
	if !r.reaping {
		r.reaping = true
		go r.reap()
	}
	// A setting saved since (a token) applies from the next process.
	if s.mu.TryLock() {
		s.conf = conf
		s.mu.Unlock()
	}
	return s
}

// reap stops processes nobody has used for a while.
func (r *claudeRegistry) reap() {
	for range time.Tick(time.Minute) {
		r.sweep(time.Now())
	}
}

func (r *claudeRegistry) sweep(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, s := range r.m {
		if !s.mu.TryLock() {
			continue
		}
		idle := now.Sub(s.used)
		switch {
		case s.proc != nil && len(s.pending) > 0 && idle > claudeParked:
			s.dirty = true
			s.stop()
			s.save()
		case s.proc != nil && len(s.pending) == 0 && idle > claudeIdle:
			s.stop()
		case s.proc == nil && idle > claudeForget:
			delete(r.m, k)
		}
		s.mu.Unlock()
	}
}

// stopAll ends every session's process (tests).
func (r *claudeRegistry) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, s := range r.m {
		s.mu.Lock()
		s.stop()
		s.mu.Unlock()
		delete(r.m, k)
	}
}

// claudeStream is one model turn read off the binary's output.
type claudeStream struct {
	s   *claudeSession
	p   *claudeProc
	ctx context.Context
	req Request

	out    []Event
	cur    Event
	err    error
	done   bool
	closed bool
	// calls are the turn's tool calls, stop the API message's stop reason.
	calls []string
	stop  string
	usage claudeUsage
}

func (st *claudeStream) Event() Event { return st.cur }
func (st *claudeStream) Err() error   { return st.err }

func (st *claudeStream) Next() bool {
	for {
		if len(st.out) > 0 {
			st.cur, st.out = st.out[0], st.out[1:]
			return true
		}
		if st.done || st.err != nil {
			return false
		}
		line, err := st.p.next(st.ctx)
		if err != nil {
			if name := st.p.alienTool(); name != "" {
				err = fmt.Errorf("Claude Code reached for a tool that is not Silo's (%s); the turn was stopped", name)
			}
			st.fail(err)
			return false
		}
		st.take(line)
	}
}

// Close ends the stream. A turn left unfinished (cut, stopped) leaves the
// binary's record ahead of Silo's, so the session is not used again.
func (st *claudeStream) Close() error {
	if st.closed {
		return nil
	}
	st.closed = true
	if !st.done && st.err == nil {
		st.fail(errors.New("turn abandoned"))
	}
	if st.s.once {
		st.s.stop()
	}
	st.s.mu.Unlock()
	return nil
}

func (st *claudeStream) fail(err error) {
	st.err = err
	st.s.dirty = true
	st.s.stop()
	st.s.save()
}

// finish closes the turn: the session now holds the request's history plus
// the turn just written, and waits on pending.
func (st *claudeStream) finish(pending []string) {
	st.done = true
	s := st.s
	s.sent, s.pending, s.started, s.used = claudeSigs(st.req.Messages), pending, true, time.Now()
	s.save()
}

func (st *claudeStream) take(line claudeLine) {
	switch line.Type {
	case "stream_event":
		st.event(line.Event)
	case "result":
		// Reported under the release the request's name stands for (an alias
		// such as "opus" names whichever is newest).
		for model, u := range line.ModelUsage {
			if u.ContextWindow > 0 {
				claudeWindows.Store(model, u.ContextWindow)
				if len(line.ModelUsage) == 1 || model == st.modelID() {
					claudeWindows.Store(st.req.Model, u.ContextWindow)
				}
			}
		}
		if line.IsError {
			st.fail(claudeFailure(line.Result))
			return
		}
		st.finish(nil)
	}
}

// modelID is the release the binary resolved the request's name to.
func (st *claudeStream) modelID() string {
	claudeModelCache.Lock()
	defer claudeModelCache.Unlock()
	if l := claudeModelCache.m[st.s.conf.cacheKey()]; l != nil {
		for _, m := range l.models {
			if m.Value == st.req.Model {
				return m.Resolved
			}
		}
	}
	return st.req.Model
}

func (st *claudeStream) event(raw json.RawMessage) {
	var ev struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			Usage claudeUsage `json:"usage"`
		} `json:"message"`
		Block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
		Delta struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			Thinking   string `json:"thinking"`
			JSON       string `json:"partial_json"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage claudeUsage `json:"usage"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		st.usage, st.stop = ev.Message.Usage, ""
	case "content_block_start":
		if ev.Block.Type != "tool_use" {
			return
		}
		name, ok := strings.CutPrefix(ev.Block.Name, claudeToolPrefix)
		if !ok {
			st.p.alien(ev.Block.Name)
			st.fail(fmt.Errorf("Claude Code reached for a tool that is not Silo's (%s); the turn was stopped", ev.Block.Name))
			return
		}
		st.calls = append(st.calls, ev.Block.ID)
		st.out = append(st.out, Event{Kind: EventToolCallStart, Index: ev.Index, ToolCallID: ev.Block.ID, ToolName: name})
	case "content_block_delta":
		switch ev.Delta.Type {
		case "text_delta":
			st.out = append(st.out, Event{Kind: EventText, Text: ev.Delta.Text})
		case "thinking_delta":
			st.out = append(st.out, Event{Kind: EventReasoning, Text: ev.Delta.Thinking})
		case "input_json_delta":
			if ev.Delta.JSON != "" {
				st.out = append(st.out, Event{Kind: EventToolCallDelta, Index: ev.Index, Text: ev.Delta.JSON})
			}
		}
	case "message_delta":
		st.stop = ev.Delta.StopReason
		if ev.Usage.Output > 0 {
			st.usage.Output = ev.Usage.Output
		}
		u := st.usage
		// The engine's InputTokens is the whole prompt, cached or not.
		st.out = append(st.out, Event{Kind: EventUsage, Usage: Usage{
			InputTokens: u.Input + u.Read + u.Write, OutputTokens: u.Output,
			CacheReadTokens: u.Read, CacheWriteTokens: u.Write,
		}})
	case "message_stop":
		// A turn that calls tools ends here for Silo: the binary now waits
		// on results only the agent loop can give.
		if st.stop == "tool_use" && len(st.calls) > 0 {
			st.finish(st.calls)
		}
	}
}
