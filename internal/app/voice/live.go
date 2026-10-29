package voice

// The live paths of a call, used whenever the model can (llm.SpeechCaps):
//
// Hearing: the browser streams a turn's audio while the person speaks
// ("speech", binary PCM frames, then "turn" or "cancel"); a transcription
// session hears it as it comes, partial words go back as live captions, and
// the transcript is ready moments after they stop. A transcriber that judges
// the end of a turn says so ("eot"), and the browser ends the turn early if
// it hears silence too. Should the session fail, the audio kept for the turn
// is transcribed in one request instead.
//
// Speaking: one voice connection for the whole call; each piece is said on
// it, with the voice's word timings when it gives them. A connection that
// breaks is dropped, the piece is spoken in one request, and the next piece
// opens a new one.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/llm"
	"silo.agent/internal/speech"
)

const (
	// liveFinishTimeout bounds the wait for a live session's final words.
	liveFinishTimeout = 6 * time.Second
	// liveOpenTimeout bounds opening a transcription session or a voice.
	liveOpenTimeout = 8 * time.Second
)

// hearing is how the call hears turns, chosen when it starts.
type hearing struct {
	model string
	live  llm.LiveTranscriber // nil: one request per turn
	caps  llm.SpeechCaps
	batch bool // the model can also take a whole recording (the fallback)
}

// hearingFor is how a call with this model hears.
func (s *Service) hearingFor(modelID string) hearing {
	h := hearing{model: modelID}
	client, _, model, err := s.models.Client(modelID)
	if err != nil {
		return h
	}
	_, h.batch = client.(llm.Transcriber)
	if lt, ok := client.(llm.LiveTranscriber); ok {
		if caps := llm.CapsOf(client, model); caps.Live {
			h.live, h.caps = lt, caps
		}
	}
	return h
}

// liveTurnIn is one turn being heard live.
type liveTurnIn struct {
	n     int
	start time.Time
	// ready closes once the session is open, or could not be.
	ready chan struct{}

	mu      sync.Mutex
	sess    llm.TranscriptionSession
	failed  bool
	pending [][]byte
	pcm     bytes.Buffer
	ended   bool
}

// openTurn starts hearing turn n: a session is opened while the person
// speaks, and their audio waits for it.
func (c *call) openTurn(n int) {
	t := &liveTurnIn{n: n, start: time.Now(), ready: make(chan struct{})}
	c.mu.Lock()
	if old := c.heard; old != nil {
		old.close()
	}
	c.heard = t
	c.mu.Unlock()
	c.touch()
	c.setState(stateHearing, "")
	go c.dialTurn(t)
}

func (c *call) dialTurn(t *liveTurnIn) {
	readyOnce := sync.OnceFunc(func() { close(t.ready) })
	defer readyOnce()
	_, _, model, err := c.s.models.Client(c.hear.model)
	if err != nil {
		t.fail(err)
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, liveOpenTimeout)
	defer cancel()
	sess, err := c.hear.live.OpenTranscription(ctx, model, llm.LiveOptions{Language: c.snd.Language})
	if err != nil {
		log.Printf("voice: call %s live transcription: %v", c.id, err)
		t.fail(err)
		return
	}
	t.mu.Lock()
	if t.ended && t.failed {
		t.mu.Unlock()
		sess.Close()
		return
	}
	t.sess = sess
	for _, f := range t.pending {
		if err := sess.Send(f); err != nil {
			t.failed = true
			break
		}
	}
	t.pending = nil
	t.mu.Unlock()
	readyOnce()
	// Partial words are live captions.
	for ev := range sess.Events() {
		_ = c.send(map[string]any{"t": "caption", "n": t.n, "text": ev.Text})
		if ev.EndOfTurn && c.hear.caps.EndOfTurn && meaningful(ev.Text) {
			_ = c.send(map[string]any{"t": "eot", "n": t.n})
		}
	}
}

func (t *liveTurnIn) fail(err error) {
	t.mu.Lock()
	t.failed = true
	t.pending = nil
	t.mu.Unlock()
}

// feed passes on a frame of the turn's audio.
func (c *call) feed(pcm []byte) {
	c.mu.Lock()
	t := c.heard
	c.mu.Unlock()
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended || t.pcm.Len()+len(pcm) > maxTurnBytes {
		return
	}
	t.pcm.Write(pcm)
	switch {
	case t.sess != nil && !t.failed:
		if err := t.sess.Send(pcm); err != nil {
			t.failed = true
		}
	case !t.failed:
		t.pending = append(t.pending, append([]byte(nil), pcm...))
	}
}

// endTurn is the browser saying turn n is over: it is heard in order with
// the others.
func (c *call) endTurn(n, eouMs int) {
	c.mu.Lock()
	t := c.heard
	if t == nil || t.n != n {
		c.mu.Unlock()
		return
	}
	c.heard = nil
	c.mu.Unlock()
	t.mu.Lock()
	t.ended = true
	t.mu.Unlock()
	c.touch()
	select {
	case c.turns <- turnIn{live: t, eouMs: eouMs, at: time.Now()}:
	default:
		log.Printf("voice: call %s dropped a turn: too many waiting", c.id)
		t.close()
	}
}

// cancelTurn drops turn n: it was not speech after all.
func (c *call) cancelTurn(n int) {
	c.mu.Lock()
	t := c.heard
	if t == nil || t.n != n {
		c.mu.Unlock()
		return
	}
	c.heard = nil
	c.mu.Unlock()
	t.close()
	c.settle()
}

func (t *liveTurnIn) close() {
	t.mu.Lock()
	t.ended, t.failed = true, true
	sess := t.sess
	t.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// heardText is a live turn's transcript: the session's final words, or, if
// it failed, the turn's audio in one request.
func (c *call) heardText(t *liveTurnIn) (string, error) {
	select {
	case <-t.ready:
	case <-time.After(liveOpenTimeout):
	case <-c.ctx.Done():
		return "", c.ctx.Err()
	}
	t.mu.Lock()
	sess, failed := t.sess, t.failed
	t.mu.Unlock()
	if sess != nil && !failed {
		ctx, cancel := context.WithTimeout(c.ctx, liveFinishTimeout)
		text, err := sess.Finish(ctx)
		cancel()
		sess.Close()
		if err == nil {
			return text, nil
		}
		log.Printf("voice: call %s live transcript: %v; transcribing the turn in one request", c.id, err)
	} else if sess != nil {
		sess.Close()
	}
	if !c.hear.batch {
		return "", errors.New("the transcription model could not be reached")
	}
	t.mu.Lock()
	wav := speech.WAV(t.pcm.Bytes(), llm.LiveSampleRate)
	t.mu.Unlock()
	tr, err := c.s.transcribeWith(c.ctx, c.botID, "call", c.hear.model, llm.Audio{Data: wav, MIME: "audio/wav", Filename: "turn.wav", Language: c.snd.Language})
	return strings.TrimSpace(tr.Text), err
}

// voiceConn is the call's connected voice, opened on first use. nil when
// the speech model has no live voice, or it could not connect.
func (c *call) voiceConn() llm.SpeechConn {
	c.ttsMu.Lock()
	defer c.ttsMu.Unlock()
	if c.tts != nil || c.ttsOff || time.Now().Before(c.ttsRetry) {
		return c.tts
	}
	client, _, model, err := c.s.models.Client(c.snd.Model)
	if err != nil {
		c.ttsOff = true
		return nil
	}
	ls, ok := client.(llm.LiveSpeaker)
	if !ok || !llm.CapsOf(client, model).Live {
		c.ttsOff = true
		return nil
	}
	ctx, cancel := context.WithTimeout(c.ctx, liveOpenTimeout)
	defer cancel()
	conn, err := ls.OpenSpeech(ctx, model, llm.SpeechRequest{Voice: c.snd.Voice, Speed: c.snd.Speed, Language: c.snd.Language, Format: "pcm"})
	if err != nil {
		log.Printf("voice: call %s live voice: %v; speaking one request at a time for a while", c.id, err)
		c.ttsRetry = time.Now().Add(30 * time.Second)
		return nil
	}
	// The connection lives as long as the call, not the open's timeout.
	c.tts = conn
	return conn
}

// dropVoice closes a voice that broke; the next piece opens another.
func (c *call) dropVoice(conn llm.SpeechConn) {
	c.ttsMu.Lock()
	if c.tts == conn {
		c.tts = nil
	}
	c.ttsMu.Unlock()
	conn.Close()
}

// sayLive speaks a piece on the connected voice. done is false when nothing
// was said and the piece should be spoken in one request instead.
func (c *call) sayLive(ctx context.Context, conn llm.SpeechConn, p *piece) (done bool) {
	fe, err := newFrameEncoder(conn.Rate(), p)
	if err != nil {
		return false
	}
	var werr error
	err = conn.Say(ctx, p.text, func(pcm []byte) {
		if werr == nil {
			werr = fe.write(pcm)
		}
	}, p.addWords)
	fe.close()
	if err == nil && werr == nil {
		return true
	}
	if ctx.Err() != nil {
		// Cut off: a voice that cannot stop mid-piece is closed by Say.
		if errors.Is(err, llm.ErrSpeechClosed) {
			c.dropVoice(conn)
		}
		return true
	}
	log.Printf("voice: call %s live voice: %v", c.id, err)
	c.dropVoice(conn)
	p.mu.Lock()
	said := len(p.frames) > 0
	p.mu.Unlock()
	return said
}

func (c *call) closeLive() {
	c.mu.Lock()
	t := c.heard
	c.heard = nil
	c.mu.Unlock()
	if t != nil {
		t.close()
	}
	c.ttsMu.Lock()
	conn := c.tts
	c.tts = nil
	c.ttsMu.Unlock()
	if conn != nil {
		conn.Close()
	}
}
