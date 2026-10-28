package voice

// A call is a spoken conversation with a Bot, held in one of its chats.
//
// The browser does what must be next to the microphone: echo cancellation,
// detecting speech, and deciding when a turn is over. It sends each finished
// turn as one recording on the call's WebSocket; the control plane
// transcribes it and hands it to the Bot like a typed message (marked as
// spoken). Everything the Bot then writes in that chat is read aloud as it
// streams, sentence by sentence, on a WebRTC audio track, which is what gives
// the browser something its echo canceller can hear.
//
// The socket also carries the WebRTC handshake and the call's state, and is
// the call's life: when it closes, the call ends.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/run"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/speech"
	"silo.agent/internal/speech/opus"
)

// Call limits that are not the operator's to set.
const (
	// maxTurnBytes caps one spoken turn's recording (16 kHz WAV: ~2 minutes).
	maxTurnBytes = 4 << 20
	// handshakeTimeout is how long a browser has to send its offer.
	handshakeTimeout = 20 * time.Second
	// interruptedMax caps the cut-off words a turn tells the Bot about.
	interruptedMax = 300
)

// The states a call is in, as the browser shows them.
const (
	stateListening = "listening"
	stateThinking  = "thinking"
	stateWorking   = "working"
	stateSpeaking  = "speaking"
)

// calls are the live calls: one per person per Bot. A second one (another
// tab) takes over from the first.
type calls struct {
	mu   sync.Mutex
	live map[string]*call
}

func (c *calls) swap(key string, next *call) (prev *call) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live == nil {
		c.live = map[string]*call{}
	}
	prev = c.live[key]
	c.live[key] = next
	return prev
}

func (c *calls) drop(key string, cur *call) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live[key] == cur {
		delete(c.live, key)
	}
}

// clientMsg is what the browser sends as text.
type clientMsg struct {
	T         string                   `json:"t"`
	SDP       string                   `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	EouMs     int                      `json:"eou_ms,omitempty"`
	DelayMs   int                      `json:"delay_ms,omitempty"`
}

// turnIn is one spoken turn as it arrived.
type turnIn struct {
	wav   []byte
	eouMs int
	at    time.Time
}

type call struct {
	s      *Service
	id     string
	botID  string
	chatID string
	userID string
	ch     *db.Chat
	snd    Sound
	mode   string
	start  time.Time

	ctx    context.Context
	cancel context.CancelCauseFunc
	ws     *websocket.Conn
	wmu    sync.Mutex
	pc     *webrtc.PeerConnection
	track  *webrtc.TrackLocalStaticSample
	out    *outbox
	turns  chan turnIn

	mu       sync.Mutex
	state    string
	tool     string
	replies  map[string]*speech.Stream
	muted    map[string]bool
	said     []string // the last pieces heard in full, for an interruption's note
	cutOff   string   // what the next turn tells the Bot it cut off
	activity time.Time
	nTurns   int
	pending  *db.VoiceTurn // the turn waiting for its first sound
	pendAt   time.Time
}

// errHangup and friends say why a call ended.
var (
	errHangup   = errors.New("hangup")
	errIdle     = errors.New("idle")
	errLimit    = errors.New("limit")
	errAccess   = errors.New("access")
	errReplaced = errors.New("replaced")
	errNetwork  = errors.New("network")
)

// ServeCall runs one call on an accepted request. The caller has checked who
// is calling and that they may write in the chat.
func (s *Service) ServeCall(w http.ResponseWriter, r *http.Request, userID string, b *db.Bot, ch *db.Chat) {
	if !s.CallEnabled() {
		http.Error(w, "calls are off: voice needs a speech model and speech-to-text (Admin → Settings → Voice)", http.StatusConflict)
		return
	}
	cfg := s.cfg()
	if err := opus.Load(cfg.Voice.OpusLibrary); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: true, // the caller has checked the Origin
	})
	if err != nil {
		log.Printf("voice: call ws accept bot=%s: %v", b.ID, err)
		return
	}
	ws.SetReadLimit(maxTurnBytes + 4096)
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(r.Context()))
	c := &call{
		s: s, id: ids.New(), botID: b.ID, chatID: ch.ID, userID: userID, ch: ch,
		snd: s.SoundOf(ctx, b), mode: cfg.VoiceMode(), start: time.Now(),
		ctx: ctx, cancel: cancel, ws: ws, out: newOutbox(), turns: make(chan turnIn, 4),
		state: stateListening, replies: map[string]*speech.Stream{}, muted: map[string]bool{}, activity: time.Now(),
	}
	key := b.ID + "/" + userID
	if prev := s.calls.swap(key, c); prev != nil {
		prev.cancel(errReplaced)
	}
	defer s.calls.drop(key, c)
	c.run(r.Context())
}

// run is the call from hello to goodbye.
func (c *call) run(reqCtx context.Context) {
	cfg := c.s.cfg()
	defer c.ws.CloseNow()
	if err := c.send(map[string]any{"t": "hello", "call_id": c.id, "chat_id": c.chatID, "mode": c.mode, "ice_servers": iceServers(cfg), "voice": c.snd.Voice}); err != nil {
		return
	}
	if err := c.handshake(cfg); err != nil {
		log.Printf("voice: call %s handshake: %v", c.id, err)
		_ = c.send(map[string]any{"t": "bye", "reason": "error", "message": err.Error()})
		c.ws.Close(websocket.StatusPolicyViolation, "handshake")
		return
	}
	defer c.pc.Close()
	c.s.db.Create(&db.VoiceCall{ID: c.id, BotID: c.botID, ChatID: c.chatID, UserID: c.userID, Mode: c.mode, StartedAt: c.start})
	c.putEvent(CallStartKind, callMeta{CallID: c.id})

	c.out.synth = c.synth
	c.out.write = func(f []byte) error { return c.track.WriteSample(media.Sample{Data: f, Duration: frameDur}) }
	c.out.onStart = c.pieceStarted
	c.out.onIdle = c.spokeAll
	go c.out.synthLoop(c.ctx)
	go c.out.pace(c.ctx)
	go c.follow()
	go c.listen()
	go c.read()

	lost, stopLost := c.s.host.Lost(c.botID, c.userID)
	defer stopLost()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	maxDur := time.Duration(max(cfg.Voice.MaxMinutes, 1)) * time.Minute
	idle := time.Duration(max(cfg.Voice.IdleMinutes, 1)) * time.Minute
	for c.ctx.Err() == nil {
		select {
		case <-c.ctx.Done():
		case <-reqCtx.Done():
			c.cancel(errNetwork)
		case <-lost:
			c.cancel(errAccess)
		case <-tick.C:
			if time.Since(c.start) > maxDur {
				c.cancel(errLimit)
			} else if c.idleFor() > idle && !c.out.busy() && c.s.host.LiveRunID(c.botID, c.chatID) == "" {
				c.cancel(errIdle)
			}
		}
	}
	c.end(context.Cause(c.ctx))
}

// handshake trades the browser's offer for the call's answer.
func (c *call) handshake(cfg config.Config) error {
	hctx, cancel := context.WithTimeout(c.ctx, handshakeTimeout)
	defer cancel()
	var offer clientMsg
	for offer.T != "offer" {
		typ, data, err := c.ws.Read(hctx)
		if err != nil {
			return fmt.Errorf("no offer: %w", err)
		}
		if typ != websocket.MessageText || json.Unmarshal(data, &offer) != nil {
			continue
		}
	}
	pc, track, err := c.s.newPeer(cfg)
	if err != nil {
		return err
	}
	c.pc, c.track = pc, track
	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		init := cand.ToJSON()
		_ = c.send(map[string]any{"t": "ice", "candidate": init})
	})
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		if st == webrtc.PeerConnectionStateFailed {
			_ = c.send(map[string]any{"t": "bye", "reason": "network"})
			c.cancel(errNetwork)
		}
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer.SDP}); err != nil {
		pc.Close()
		return err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return err
	}
	return c.send(map[string]any{"t": "answer", "sdp": answer.SDP})
}

// read takes what the browser sends: control messages, and a spoken turn's
// recording right after the message that announces it.
func (c *call) read() {
	var eou int
	expectTurn := false
	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			if c.ctx.Err() == nil {
				c.cancel(errNetwork)
			}
			return
		}
		if typ == websocket.MessageBinary {
			if !expectTurn || len(data) == 0 || len(data) > maxTurnBytes {
				continue
			}
			expectTurn = false
			select {
			case c.turns <- turnIn{wav: data, eouMs: eou, at: time.Now()}:
			default:
				log.Printf("voice: call %s dropped a turn: too many waiting", c.id)
			}
			continue
		}
		var m clientMsg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.T {
		case "ice":
			if m.Candidate != nil && c.pc != nil {
				_ = c.pc.AddICECandidate(*m.Candidate)
			}
		case "turn":
			eou, expectTurn = m.EouMs, true
			c.touch()
		case "duck":
			c.out.duck(true)
		case "unduck":
			c.out.duck(false)
		case "interrupt":
			c.interrupt()
		case "playout":
			c.out.setDelay(m.DelayMs)
		case "hangup":
			c.cancel(errHangup)
			return
		}
	}
}

// listen handles the spoken turns one at a time, in order.
func (c *call) listen() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case t := <-c.turns:
			c.turn(t)
		}
	}
}

// turn transcribes one spoken turn and hands it to the Bot.
func (c *call) turn(t turnIn) {
	c.setState(stateThinking, "")
	started := time.Now()
	tr, err := c.s.transcribe(c.ctx, c.botID, "call", llm.Audio{Data: t.wav, MIME: "audio/wav", Filename: "turn.wav", Language: c.snd.Language})
	stt := time.Since(started)
	if err != nil {
		if c.ctx.Err() != nil {
			return
		}
		log.Printf("voice: call %s transcribe: %v", c.id, err)
		_ = c.send(map[string]any{"t": "error", "message": err.Error()})
		c.say(sayNotHeard.in(c.snd.Language), "")
		return
	}
	text := strings.TrimSpace(tr.Text)
	_ = c.send(map[string]any{"t": "heard", "text": text})
	if !meaningful(text) {
		c.settle()
		return
	}
	c.mu.Lock()
	cut := c.cutOff
	c.cutOff = ""
	c.nTurns++
	c.mu.Unlock()
	runID, err := c.s.host.StartOrInject(c.botID, c.chatID, text, run.Extra{Spoken: &run.Spoken{CallID: c.id, Interrupted: cut}}, nil)
	if err != nil {
		log.Printf("voice: call %s: %v", c.id, err)
		if errors.Is(err, run.ErrBusy) {
			c.say(sayBusy.in(c.snd.Language), "")
		} else {
			c.say(sayFailed.in(c.snd.Language), "")
		}
		return
	}
	c.mu.Lock()
	// What the Bot says next answers this turn: speak it even if an earlier
	// sentence of the same run was cut off.
	delete(c.muted, runID)
	c.pending = &db.VoiceTurn{CallID: c.id, BotID: c.botID, EndOfTurnMs: t.eouMs, SpeechToTextMs: int(stt / time.Millisecond), CreatedAt: time.Now()}
	c.pendAt = t.at
	c.mu.Unlock()
}

// meaningful reports whether a transcript holds any words at all: silence
// often comes back as nothing, or punctuation.
func meaningful(text string) bool {
	for _, r := range text {
		if r > ' ' && !strings.ContainsRune(".,!?…-–—'\"()[]", r) {
			return true
		}
	}
	return false
}

// follow reads the chat's events as they are published and speaks what the
// Bot writes there.
func (c *call) follow() {
	events, stop := c.s.host.Subscribe(c.botID)
	defer stop()
	for {
		var ev *v1.RunEvent
		select {
		case <-c.ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			ev = e
		}
		if ev.GetChatId() != c.chatID {
			continue
		}
		runID := ev.GetRunId()
		switch ev.GetKind() {
		case "chunk":
			c.reply(runID, ev.GetBody(), false)
		case "tool":
			c.reply(runID, "", true)
			c.unmute(runID)
			c.setState(stateWorking, ev.GetTool())
		case "assistant", "section", "section_live":
			c.reply(runID, "", true)
			c.unmute(runID)
		case "done":
			c.reply(runID, "", true)
			c.forget(runID)
			c.saveTurn()
			c.settle()
		case "error":
			c.reply(runID, "", true)
			if !c.isMuted(runID) {
				c.say(sayFailed.in(c.snd.Language), runID)
			}
		case "approval":
			c.say(sayApproval.in(c.snd.Language), runID)
		}
	}
}

// reply feeds the Bot's streamed text to the run's reader and queues what is
// ready to be said; end says the model's turn is over.
func (c *call) reply(runID, text string, end bool) {
	c.mu.Lock()
	if c.muted[runID] {
		c.mu.Unlock()
		return
	}
	st := c.replies[runID]
	if st == nil {
		st = speech.NewStream(speech.PointersFor(c.snd.Language))
		c.replies[runID] = st
	}
	var ready []string
	if text != "" {
		ready = st.Push(text)
	}
	if end {
		ready = append(ready, st.Flush()...)
	}
	c.mu.Unlock()
	for _, r := range ready {
		c.say(r, runID)
	}
}

func (c *call) say(text, runID string) {
	c.touch()
	c.out.say(text, runID)
}

func (c *call) unmute(runID string) {
	c.mu.Lock()
	delete(c.muted, runID)
	c.mu.Unlock()
}

func (c *call) isMuted(runID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.muted[runID]
}

func (c *call) forget(runID string) {
	c.mu.Lock()
	delete(c.replies, runID)
	delete(c.muted, runID)
	c.mu.Unlock()
}

// interrupt is the human speaking over the Bot: the voice stops, the rest of
// what that turn of the model writes is not said, and the next turn tells the
// Bot where it was cut off.
func (c *call) interrupt() {
	heard, runID := c.out.flush()
	c.mu.Lock()
	note := strings.TrimSpace(strings.Join(append(c.said, heard), " "))
	if r := []rune(note); len(r) > interruptedMax {
		note = "…" + strings.TrimSpace(string(r[len(r)-interruptedMax:]))
	}
	c.cutOff, c.said = note, nil
	if runID != "" {
		c.muted[runID] = true
		delete(c.replies, runID)
	}
	if c.pending != nil {
		c.pending.Interrupted = true
	}
	c.mu.Unlock()
	c.settle()
}

// synth makes one piece's speech.
func (c *call) synth(ctx context.Context, p *piece) {
	sp, err := c.s.Speak(ctx, c.botID, c.snd, p.text, "pcm")
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("voice: call %s speak: %v", c.id, err)
			_ = c.send(map[string]any{"t": "error", "message": err.Error()})
		}
		return
	}
	defer sp.Body.Close()
	if err := encodeSpeech(sp.Body, sp.Format, sp.SampleRate, p); err != nil && ctx.Err() == nil {
		log.Printf("voice: call %s encode: %v", c.id, err)
	}
}

// pieceStarted is the first frame of a piece going out.
func (c *call) pieceStarted(p *piece) {
	c.touch()
	c.mu.Lock()
	if len(c.said) > 2 {
		c.said = c.said[len(c.said)-2:]
	}
	c.said = append(c.said, p.text)
	if c.pending != nil && c.pending.FirstAudioMs == 0 {
		c.pending.FirstAudioMs = int(time.Since(c.pendAt) / time.Millisecond)
	}
	c.mu.Unlock()
	_ = c.send(map[string]any{"t": "say", "text": p.text, "id": p.id})
	c.setState(stateSpeaking, "")
}

// spokeAll is the queue running dry.
func (c *call) spokeAll() { c.settle() }

// settle sets the state from what is going on: speaking, or the Bot still
// at work, or listening.
func (c *call) settle() {
	if c.out.busy() {
		return
	}
	if c.s.host.LiveRunID(c.botID, c.chatID) != "" {
		c.mu.Lock()
		st, tool := c.state, c.tool
		c.mu.Unlock()
		if st != stateWorking {
			st, tool = stateThinking, ""
		}
		c.setState(st, tool)
		return
	}
	c.setState(stateListening, "")
}

func (c *call) setState(st, tool string) {
	c.mu.Lock()
	if c.state == st && c.tool == tool {
		c.mu.Unlock()
		return
	}
	c.state, c.tool = st, tool
	c.mu.Unlock()
	_ = c.send(map[string]any{"t": "state", "state": st, "tool": tool})
}

// saveTurn files the timings of the turn the Bot just finished answering.
func (c *call) saveTurn() {
	c.mu.Lock()
	t := c.pending
	c.pending = nil
	c.mu.Unlock()
	if t != nil {
		c.s.db.Create(t)
	}
}

func (c *call) touch() {
	c.mu.Lock()
	c.activity = time.Now()
	c.mu.Unlock()
}

func (c *call) idleFor() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.activity)
}

// send writes one control message.
func (c *call) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// putEvent writes one of the call's events into its chat and shows it.
func (c *call) putEvent(kind string, m callMeta) {
	ev, err := chats.Put(c.s.db, c.ch, "voice", db.RunEvent{Kind: kind, Meta: m.encode()})
	if err != nil || ev == nil {
		log.Printf("voice: call %s %s: %v", c.id, kind, err)
		return
	}
	c.s.host.Publish(c.botID, &v1.RunEvent{Id: ev.ID, RunId: ev.RunID, ChatId: c.chatID, Kind: kind, CreatedAt: ev.CreatedAt.Format(time.RFC3339), Spoken: SpokenProto(ev.Meta)})
}

// end closes the call and says why.
func (c *call) end(cause error) {
	reason := "error"
	switch {
	case errors.Is(cause, errHangup):
		reason = "hangup"
	case errors.Is(cause, errIdle):
		reason = "idle"
	case errors.Is(cause, errLimit):
		reason = "limit"
	case errors.Is(cause, errAccess):
		reason = "access"
	case errors.Is(cause, errReplaced):
		reason = "replaced"
	case errors.Is(cause, errNetwork):
		reason = "lost"
	}
	c.saveTurn()
	_ = c.send(map[string]any{"t": "bye", "reason": reason})
	c.ws.Close(websocket.StatusNormalClosure, reason)
	secs := int(time.Since(c.start) / time.Second)
	now := time.Now()
	c.mu.Lock()
	turns := c.nTurns
	c.mu.Unlock()
	c.s.db.Model(&db.VoiceCall{}).Where("id = ?", c.id).Updates(map[string]any{"ended_at": now, "end_reason": reason, "turns": turns})
	c.putEvent(CallEndKind, callMeta{CallID: c.id, Seconds: secs, Reason: reason})
}
