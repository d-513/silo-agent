package dummy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"

	"silo.agent/internal/llm"
)

// SpeechMsPerRune is how long the dummy speaks each rune, so a test can
// reason about playout position from text alone.
const SpeechMsPerRune = 50

var spoken []llm.SpeechRequest

// Speak answers PCM at llm.SpeechSampleRate: a quiet tone, SpeechMsPerRune
// per rune of the text. Text holding Test_TTS_Error is refused.
func (c *client) Speak(_ context.Context, _ string, req llm.SpeechRequest) (llm.Speech, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return llm.Speech{}, errors.New("nothing to say")
	}
	if strings.Contains(text, "Test_TTS_Error") {
		return llm.Speech{}, errors.New("speech: http 400: the dummy refuses to say that")
	}
	mu.Lock()
	spoken = append(spoken, req)
	mu.Unlock()
	return llm.Speech{Body: io.NopCloser(bytes.NewReader(Tone(len([]rune(text)) * SpeechMsPerRune))), Format: "pcm", SampleRate: llm.SpeechSampleRate}, nil
}

// Tone is ms of a quiet 440 Hz tone as 16-bit LE mono PCM at the speech rate.
func Tone(ms int) []byte {
	n := llm.SpeechSampleRate * ms / 1000
	out := make([]byte, 2*n)
	for i := range n {
		v := int16(2000 * math.Sin(2*math.Pi*440*float64(i)/llm.SpeechSampleRate))
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}

// Spoken returns what Speak was asked to say, in order.
func Spoken() []llm.SpeechRequest {
	mu.Lock()
	defer mu.Unlock()
	return append([]llm.SpeechRequest(nil), spoken...)
}
