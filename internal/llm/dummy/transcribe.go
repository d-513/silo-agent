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
	out := llm.Transcript{Text: text, Language: audio.Language, Seconds: 1}
	// Asked for timings, each line of the text is a segment two seconds
	// long. A recording that holds Test_No_Timings has none, like a model
	// that gives only text.
	if audio.Timestamps && !strings.Contains(text, "Test_No_Timings") {
		at := 0.0
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				out.Segments = append(out.Segments, llm.Segment{Start: at, End: at + 2, Text: line})
				at += 2
			}
		}
		out.Seconds = at
	}
	return out, nil
}
