package dummy

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"silo.agent/internal/llm"
	"silo.agent/internal/speech"
)

// Transcribe "hears" the bytes: valid UTF-8 audio is returned as its own
// transcript, so tests can speak words by sending text. Anything else reports
// its size.
func (c *client) Transcribe(_ context.Context, _ string, audio llm.Audio) (llm.Transcript, error) {
	if len(audio.Data) == 0 {
		return llm.Transcript{}, fmt.Errorf("audio is empty")
	}
	if len(audio.Data) > llm.MaxAudioBytes {
		return llm.Transcript{}, fmt.Errorf("audio is %d bytes; the cap is %d", len(audio.Data), llm.MaxAudioBytes)
	}
	data := audio.Data
	// A WAV's samples are what is "heard": a call sends each turn as one.
	if pcm, _, err := speech.ReadWAV(data); err == nil {
		data = pcm
	}
	text := fmt.Sprintf("(%d bytes of audio)", len(data))
	if utf8.Valid(data) {
		text = strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
	}
	return llm.Transcript{Text: text, Language: audio.Language, Seconds: 1}, nil
}
