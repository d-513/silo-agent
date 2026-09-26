package dummy

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"silo.agent/internal/llm"
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
	text := fmt.Sprintf("(%d bytes of audio)", len(audio.Data))
	if utf8.Valid(audio.Data) {
		text = strings.TrimSpace(string(audio.Data))
	}
	return llm.Transcript{Text: text, Language: audio.Language, Seconds: 1}, nil
}
