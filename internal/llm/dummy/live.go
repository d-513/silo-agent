package dummy

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	"silo.agent/internal/llm"
)

// Live speech for tests. A model whose name holds "live" is live: its
// transcription hears the PCM it is sent as text (like Transcribe hears a
// WAV), and its voice keeps one connection for a call and gives word
// timings. A name that also holds "eot" judges the end of a turn: when the
// words heard so far end in "over".

// SpeechCaps says which dummy models are live.
func (c *client) SpeechCaps(model string) llm.SpeechCaps {
	if !strings.Contains(model, "live") {
		return llm.SpeechCaps{}
	}
	return llm.SpeechCaps{Live: true, Words: true, EndOfTurn: strings.Contains(model, "eot")}
}

var (
	liveOpened int
	liveSaid   []string
)

// LiveOpened is how many live transcription sessions and voices were opened.
func LiveOpened() int {
	mu.Lock()
	defer mu.Unlock()
	return liveOpened
}

// LiveSaid is what live voices were asked to say, in order.
func LiveSaid() []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), liveSaid...)
}

type liveSession struct {
	mu     sync.Mutex
	model  string
	buf    []byte
	events chan llm.TranscriptEvent
	closed bool
}

func (c *client) OpenTranscription(_ context.Context, model string, _ llm.LiveOptions) (llm.TranscriptionSession, error) {
	mu.Lock()
	liveOpened++
	mu.Unlock()
	return &liveSession{model: model, events: make(chan llm.TranscriptEvent, 64)}, nil
}

func (s *liveSession) heard() string {
	if !utf8.Valid(s.buf) {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(s.buf), "\x00"))
}

func (s *liveSession) Send(pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return llm.ErrSpeechClosed
	}
	s.buf = append(s.buf, pcm...)
	text := s.heard()
	ev := llm.TranscriptEvent{Text: text, EndOfTurn: strings.Contains(s.model, "eot") && strings.HasSuffix(text, "over")}
	select {
	case s.events <- ev:
	default:
	}
	return nil
}

func (s *liveSession) Events() <-chan llm.TranscriptEvent { return s.events }

func (s *liveSession) Finish(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heard(), nil
}

func (s *liveSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

type liveVoice struct{ closed bool }

func (c *client) OpenSpeech(context.Context, string, llm.SpeechRequest) (llm.SpeechConn, error) {
	mu.Lock()
	liveOpened++
	mu.Unlock()
	return &liveVoice{}, nil
}

func (v *liveVoice) Rate() int { return llm.SpeechSampleRate }

// Say speaks SpeechMsPerRune per rune in 100 ms chunks, and each word's
// end time once its audio is out.
func (v *liveVoice) Say(ctx context.Context, text string, audio func([]byte), words func([]llm.SpeechWord)) error {
	if v.closed {
		return llm.ErrSpeechClosed
	}
	mu.Lock()
	liveSaid = append(liveSaid, text)
	mu.Unlock()
	pcm := Tone(len([]rune(text)) * SpeechMsPerRune)
	const chunk = llm.SpeechSampleRate / 10 * 2
	for at := 0; at < len(pcm); at += chunk {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		audio(pcm[at:min(at+chunk, len(pcm))])
	}
	var ws []llm.SpeechWord
	ms := 0
	for _, w := range strings.Fields(text) {
		start := ms
		ms += (len([]rune(w)) + 1) * SpeechMsPerRune
		ws = append(ws, llm.SpeechWord{Text: w, StartMs: start, EndMs: ms})
	}
	words(ws)
	return nil
}

func (v *liveVoice) Close() error {
	v.closed = true
	return nil
}
