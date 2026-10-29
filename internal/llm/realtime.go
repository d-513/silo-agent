package llm

// OpenAI Realtime transcription: a socket that hears 24 kHz PCM and sends
// the transcript of each committed buffer (deltas as it goes, then the
// completed text). OpenAI speaks the GA shape; a self-hosted server says
// which it speaks in its "realtime" setting (LocalAI the GA shape, Speaches
// a flat older one). Turns are ours: server VAD is off, and Finish commits.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"silo.agent/internal/speech"
)

// The Realtime dialects.
const (
	realtimeOpenAI   = "openai"
	realtimeSpeaches = "speaches"
)

// realtimeRate is the only PCM rate the Realtime API takes.
const realtimeRate = 24000

// openAIRealtimeModels are OpenAI's models a transcription session takes.
var openAIRealtimeModels = map[string]bool{
	"gpt-live-transcribe": true, "gpt-transcribe": true, "gpt-realtime-whisper": true,
	"gpt-4o-transcribe": true, "gpt-4o-mini-transcribe": true, "gpt-4o-mini-transcribe-2025-12-15": true, "whisper-1": true,
}

// realtimeDialect reads a self-hosted server's "realtime" setting.
func realtimeDialect(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "openai", "localai", "ga", "true", "on", "yes":
		return realtimeOpenAI
	case "speaches":
		return realtimeSpeaches
	}
	return ""
}

// SpeechCaps: a transcription model streams where the endpoint speaks the
// Realtime API (OpenAI's listed models, or every transcription model of a
// server that says it does).
func (c *openAICompatClient) SpeechCaps(model string) SpeechCaps {
	switch {
	case c.realtime == "":
		return SpeechCaps{}
	case c.openAI:
		return SpeechCaps{Live: openAIRealtimeModels[model]}
	default:
		return SpeechCaps{Live: GuessKind(model) == KindTranscription}
	}
}

func (c *openAICompatClient) OpenTranscription(ctx context.Context, model string, opts LiveOptions) (TranscriptionSession, error) {
	if c.realtime == "" {
		return nil, fmt.Errorf("this endpoint does not transcribe live")
	}
	u := "ws" + strings.TrimPrefix(strings.TrimRight(c.base, "/"), "http") + "/realtime"
	q := url.Values{"intent": {"transcription"}}
	if !c.openAI {
		// A self-hosted server picks its pipeline by model; OpenAI must not be
		// given one (it would open a conversation session).
		q.Set("model", model)
		if opts.Language != "" {
			q.Set("language", opts.Language)
		}
	}
	h := http.Header{}
	if c.key != "" && c.key != "none" {
		h.Set("Authorization", "Bearer "+c.key)
	}
	conn, err := dialWS(ctx, u+"?"+q.Encode(), h)
	if err != nil {
		return nil, fmt.Errorf("realtime: %w", err)
	}
	t := newWSTurn(conn)
	var update map[string]any
	if c.realtime == realtimeSpeaches {
		tr := map[string]any{"model": model}
		if opts.Language != "" {
			tr["language"] = opts.Language
		}
		update = map[string]any{"type": "session.update", "session": map[string]any{"input_audio_transcription": tr, "turn_detection": nil}}
	} else {
		tr := map[string]any{"model": model}
		if opts.Language != "" {
			tr["language"] = opts.Language
		}
		update = map[string]any{"type": "session.update", "session": map[string]any{
			"type":  "transcription",
			"audio": map[string]any{"input": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": realtimeRate}, "transcription": tr, "turn_detection": nil}},
		}}
	}
	if err := t.writeJSON(update); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("realtime: %w", err)
	}
	t.audio = func(pcm []byte) (websocket.MessageType, []byte) {
		b, _ := json.Marshal(map[string]string{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(speech.Resample(pcm, LiveSampleRate, realtimeRate))})
		return websocket.MessageText, b
	}
	t.finish = []any{map[string]string{"type": "input_audio_buffer.commit"}}
	t.grace = 8 * time.Second
	go readRealtime(t, model == "whisper-1")
	return t, nil
}

// readRealtime reads transcription events: deltas build the guess for an
// item, its completed text is final, and the item our commit made ends the
// turn.
func readRealtime(t *wsTurn, wholeDeltas bool) {
	var mu sync.Mutex
	acc := map[string]string{}
	committed := ""
	completed := map[string]bool{}
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			t.end(closeErr(err))
			return
		}
		var m struct {
			Type       string `json:"type"`
			ItemID     string `json:"item_id"`
			Delta      string `json:"delta"`
			Transcript string `json:"transcript"`
			Error      *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		mu.Lock()
		switch m.Type {
		case "conversation.item.input_audio_transcription.delta":
			if wholeDeltas {
				acc[m.ItemID] = m.Delta
			} else {
				acc[m.ItemID] += m.Delta
			}
			t.guess(acc[m.ItemID])
		case "conversation.item.input_audio_transcription.completed":
			completed[m.ItemID] = true
			t.sure(m.Transcript)
			if m.ItemID == committed && t.isFinishing() {
				t.done()
			}
		case "conversation.item.input_audio_transcription.failed":
			if m.ItemID == committed {
				msg := "transcription failed"
				if m.Error != nil {
					msg = m.Error.Message
				}
				mu.Unlock()
				t.end(fmt.Errorf("realtime: %s", msg))
				return
			}
		case "input_audio_buffer.committed":
			committed = m.ItemID
			if completed[committed] {
				t.done()
			}
		case "error":
			msg := "error"
			if m.Error != nil {
				msg = firstOf(m.Error.Message, m.Error.Code)
			}
			// An empty or too short buffer refused at commit: nothing was said.
			if t.isFinishing() && strings.Contains(strings.ToLower(msg), "buffer") {
				t.done()
				break
			}
			mu.Unlock()
			t.end(fmt.Errorf("realtime: %s", msg))
			return
		}
		mu.Unlock()
	}
}
