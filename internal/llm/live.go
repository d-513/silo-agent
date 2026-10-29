package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// Live speech: transcription while the person is still speaking, and a voice
// that stays connected for a whole call. Both are optional capabilities of a
// provider, and per model: a provider that streams with one model may only
// take whole recordings with another. SpeechCaps is how a caller asks, and
// everything that is not live falls back to the one-request interfaces
// (Transcriber, Speaker).

// SpeechCaps are what a transcription or speech model can do beyond one
// request at a time.
type SpeechCaps struct {
	// Live: a transcription model hears audio as it is spoken and has the
	// text ready when the person stops; a speech model keeps one connection
	// open for a call and speaks each piece on it.
	Live bool `json:"live,omitempty"`
	// Words: a speech model says when each word is spoken, so what a
	// listener heard before cutting in is exact, not estimated.
	Words bool `json:"words,omitempty"`
	// EndOfTurn: a transcription model judges from the words when the person
	// has finished, sooner than a silence timer.
	EndOfTurn bool `json:"end_of_turn,omitempty"`
}

// SpeechCapser is the optional interface of a provider whose models have
// SpeechCaps.
type SpeechCapser interface {
	SpeechCaps(model string) SpeechCaps
}

// CapsOf is what a client says a model can do; a client that says nothing
// can do nothing live.
func CapsOf(c Client, model string) SpeechCaps {
	if s, ok := c.(SpeechCapser); ok {
		return s.SpeechCaps(model)
	}
	return SpeechCaps{}
}

// LiveSampleRate is the PCM a live transcription session is sent: 16-bit
// little-endian mono at 16 kHz, what every streaming recognizer takes.
const LiveSampleRate = 16000

// LiveOptions configure a live transcription session.
type LiveOptions struct {
	// Language is an ISO-639-1 hint; empty lets the model detect it.
	Language string
}

// TranscriptEvent is what a live session has heard so far.
type TranscriptEvent struct {
	// Text is the whole turn so far: what the model is sure of, then its
	// current guess.
	Text string
	// EndOfTurn: the model judged the person has finished speaking.
	EndOfTurn bool
}

// LiveTranscriber is the optional interface of a provider that transcribes
// while the person speaks.
type LiveTranscriber interface {
	OpenTranscription(ctx context.Context, model string, opts LiveOptions) (TranscriptionSession, error)
}

// TranscriptionSession is one turn being transcribed live.
type TranscriptionSession interface {
	// Send passes on audio: 16-bit LE mono PCM at LiveSampleRate.
	Send(pcm []byte) error
	// Events are the partial transcripts as they come; closed when the
	// session ends.
	Events() <-chan TranscriptEvent
	// Finish asks for the final transcript of everything sent and waits for
	// it.
	Finish(ctx context.Context) (string, error)
	// Close ends the session; Finish's answer stays valid.
	Close() error
}

// SpeechWord is one spoken word and when it is said, from the start of the
// piece's audio.
type SpeechWord struct {
	Text    string
	StartMs int
	EndMs   int
}

// LiveSpeaker is the optional interface of a provider that keeps a voice
// connected for a whole call.
type LiveSpeaker interface {
	// OpenSpeech connects a voice (req's voice, speed and language; its Text
	// is unused).
	OpenSpeech(ctx context.Context, model string, req SpeechRequest) (SpeechConn, error)
}

// SpeechConn is a connected voice. Pieces are spoken one at a time.
type SpeechConn interface {
	// Say speaks one piece: its audio (16-bit LE mono PCM at Rate) and its
	// words' timings go to the callbacks as they come, and Say returns when
	// the piece is complete. A cancelled ctx stops the piece; the connection
	// may then be closed and Say answer ErrSpeechClosed.
	Say(ctx context.Context, text string, audio func(pcm []byte), words func([]SpeechWord)) error
	// Rate is the sample rate of the audio Say hands over.
	Rate() int
	Close() error
}

// ErrSpeechClosed is what a SpeechConn answers once it can say no more: the
// caller opens another, or speaks one request at a time.
var ErrSpeechClosed = errors.New("speech connection closed")

// liveTurn is the bookkeeping every live transcription adapter shares: the
// parts the model is sure of, its current guess, the events channel, and the
// moment Finish's final answer is in.
type liveTurn struct {
	mu       sync.Mutex
	final    []string
	partial  string
	events   chan TranscriptEvent
	closed   bool
	finished chan struct{}
	finOnce  sync.Once
	ended    chan struct{}
	endOnce  sync.Once
	err      error
}

func newLiveTurn() *liveTurn {
	return &liveTurn{events: make(chan TranscriptEvent, 64), finished: make(chan struct{}), ended: make(chan struct{})}
}

// text is the turn so far.
func (l *liveTurn) text() string {
	parts := append(append([]string(nil), l.final...), l.partial)
	return joinWords(parts)
}

// joinWords joins transcript parts with one space where neither side has
// one.
func joinWords(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") && !strings.HasPrefix(p, " ") {
			b.WriteByte(' ')
		}
		b.WriteString(p)
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

// guess replaces the current guess.
func (l *liveTurn) guess(s string) {
	l.mu.Lock()
	l.partial = s
	ev := TranscriptEvent{Text: l.text()}
	l.mu.Unlock()
	l.emit(ev)
}

// sure adds a part the model is sure of and drops the guess it replaces.
func (l *liveTurn) sure(s string) {
	l.mu.Lock()
	if s = strings.TrimSpace(s); s != "" {
		l.final = append(l.final, s)
	}
	l.partial = ""
	ev := TranscriptEvent{Text: l.text()}
	l.mu.Unlock()
	l.emit(ev)
}

// set replaces the whole turn with what the model now says it is (a model
// that answers the full transcript each time, not parts).
func (l *liveTurn) set(sure, guess string) {
	l.mu.Lock()
	l.final = nil
	if s := strings.TrimSpace(sure); s != "" {
		l.final = []string{s}
	}
	l.partial = guess
	ev := TranscriptEvent{Text: l.text()}
	l.mu.Unlock()
	l.emit(ev)
}

// endOfTurn reports the model's judgment that the person finished.
func (l *liveTurn) endOfTurn() {
	l.mu.Lock()
	ev := TranscriptEvent{Text: l.text(), EndOfTurn: true}
	l.mu.Unlock()
	l.emit(ev)
}

func (l *liveTurn) emit(ev TranscriptEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	select {
	case l.events <- ev:
	default:
		// A reader that fell behind wants the newest, not every guess.
		select {
		case <-l.events:
		default:
		}
		select {
		case l.events <- ev:
		default:
		}
	}
}

// done says Finish's final answer has arrived.
func (l *liveTurn) done() { l.finOnce.Do(func() { close(l.finished) }) }

// end closes the session from the reading side, with the error that ended
// it (nil for a clean end).
func (l *liveTurn) end(err error) {
	l.endOnce.Do(func() {
		l.mu.Lock()
		if err != nil && l.err == nil {
			l.err = err
		}
		l.closed = true
		close(l.events)
		l.mu.Unlock()
		close(l.ended)
	})
}

// wait blocks until the final answer is in (or the session ended, or ctx),
// and returns the turn's text. A session that ended with an error and no
// words answers the error.
func (l *liveTurn) wait(ctx context.Context) (string, error) {
	select {
	case <-l.finished:
	case <-l.ended:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := joinWords(append(append([]string(nil), l.final...), l.partial))
	if out == "" && l.err != nil {
		return "", l.err
	}
	return out, nil
}
