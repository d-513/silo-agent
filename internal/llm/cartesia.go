package llm

// Cartesia: Sonic speech (one request, or a socket a call keeps open, a
// context per piece with word timestamps) and Ink transcription (live; a
// file goes through a live session).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/coder/websocket"
)

const (
	cartesiaBase    = "https://api.cartesia.ai"
	cartesiaVersion = "2026-08-14"
)

type cartesia struct {
	speechOnly
	key  string
	base string
}

func newCartesia(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	base := strings.TrimRight(s.Get("base_url"), "/")
	if base == "" {
		base = cartesiaBase
	}
	return &cartesia{speechOnly: speechOnly{"Cartesia"}, key: key, base: base}, nil
}

func (c *cartesia) ws() string { return "ws" + strings.TrimPrefix(c.base, "http") }

func isInk(model string) bool { return strings.HasPrefix(model, "ink") }

// SpeechCaps: everything streams; Sonic gives word timestamps.
func (c *cartesia) SpeechCaps(model string) SpeechCaps {
	if isInk(model) {
		return SpeechCaps{Live: true}
	}
	return SpeechCaps{Live: true, Words: true}
}

// cartesiaRequest is the body of a speech request, HTTP or socket.
func cartesiaRequest(model string, req SpeechRequest) map[string]any {
	body := map[string]any{
		"model_id":      model,
		"voice":         map[string]string{"id": req.Voice},
		"output_format": map[string]any{"container": "raw", "encoding": "pcm_s16le", "sample_rate": SpeechSampleRate},
	}
	if req.Language != "" {
		body["language"] = req.Language
	}
	if req.Speed > 0 && req.Speed != 1 {
		body["generation_config"] = map[string]any{"speed": min(max(req.Speed, 0.6), 1.5)}
	}
	return body
}

func (c *cartesia) Speak(ctx context.Context, model string, req SpeechRequest) (Speech, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Speech{}, fmt.Errorf("nothing to say")
	}
	if req.Voice == "" {
		return Speech{}, fmt.Errorf("cartesia: pick a voice for this Bot (Settings → Voice)")
	}
	body := cartesiaRequest(model, req)
	body["transcript"] = text
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/tts/bytes", jsonBody(body))
	if err != nil {
		return Speech{}, err
	}
	hreq.Header.Set("Authorization", "Bearer "+c.key)
	hreq.Header.Set("Cartesia-Version", cartesiaVersion)
	hreq.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return Speech{}, err
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		return Speech{}, vendorError("cartesia", res, c.key)
	}
	return Speech{Body: res.Body, Format: "pcm", SampleRate: SpeechSampleRate}, nil
}

// OpenSpeech keeps the TTS socket open: each piece is one fresh context,
// done when its "done" arrives.
func (c *cartesia) OpenSpeech(ctx context.Context, model string, req SpeechRequest) (SpeechConn, error) {
	if req.Voice == "" {
		return nil, fmt.Errorf("cartesia: pick a voice for this Bot (Settings → Voice)")
	}
	conn, err := dialWS(ctx, c.ws()+"/tts/websocket?cartesia_version="+cartesiaVersion, http.Header{"X-API-Key": {c.key}})
	if err != nil {
		return nil, fmt.Errorf("cartesia: %w", err)
	}
	base := cartesiaRequest(model, req)
	v := newCtxVoice(conn, SpeechSampleRate)
	v.start = func(id, text string) []any {
		m := map[string]any{}
		for k, val := range base {
			m[k] = val
		}
		m["transcript"] = strings.TrimSpace(text)
		m["context_id"] = id
		m["continue"] = false
		m["add_timestamps"] = true
		return []any{m}
	}
	v.stop = func(id string) any { return map[string]any{"context_id": id, "cancel": true} }
	v.read = readCartesia
	go v.run()
	return v, nil
}

func readCartesia(data []byte) voiceEvent {
	var m struct {
		Type      string `json:"type"`
		Data      string `json:"data"`
		Done      bool   `json:"done"`
		ContextID string `json:"context_id"`
		Message   string `json:"message"`
		Title     string `json:"title"`
		Words     *struct {
			Words []string  `json:"words"`
			Start []float64 `json:"start"`
			End   []float64 `json:"end"`
		} `json:"word_timestamps"`
	}
	if json.Unmarshal(data, &m) != nil {
		return voiceEvent{}
	}
	ev := voiceEvent{ctx: m.ContextID}
	switch m.Type {
	case "chunk":
		ev.pcm, _ = base64.StdEncoding.DecodeString(m.Data)
	case "timestamps":
		if w := m.Words; w != nil {
			for i, word := range w.Words {
				if i < len(w.Start) && i < len(w.End) {
					ev.words = append(ev.words, SpeechWord{Text: word, StartMs: int(w.Start[i] * 1000), EndMs: int(w.End[i] * 1000)})
				}
			}
		}
	case "done":
		ev.done = true
	case "error":
		ev.err = fmt.Errorf("cartesia: %s", firstOf(m.Message, m.Title, "error"))
	}
	return ev
}

// Transcribe sends a file through a live Ink session.
func (c *cartesia) Transcribe(ctx context.Context, model string, a Audio) (Transcript, error) {
	return transcribeByLive(ctx, c, model, a)
}

// OpenTranscription opens Ink's socket: binary audio, "finalize" asks for
// the final words, and flush_done says they are in.
func (c *cartesia) OpenTranscription(ctx context.Context, model string, opts LiveOptions) (TranscriptionSession, error) {
	q := url.Values{"model": {model}, "encoding": {"pcm_s16le"}, "sample_rate": {strconv.Itoa(LiveSampleRate)}, "cartesia_version": {cartesiaVersion}}
	if opts.Language != "" && strings.Contains(model, "whisper") {
		q.Set("language", opts.Language)
	}
	conn, err := dialWS(ctx, c.ws()+"/stt/websocket?"+q.Encode(), http.Header{"X-API-Key": {c.key}})
	if err != nil {
		return nil, fmt.Errorf("cartesia: %w", err)
	}
	t := newWSTurn(conn)
	t.finish = []any{"finalize"}
	t.bye = "close"
	go readInk(t)
	return t, nil
}

func readInk(t *wsTurn) {
	for {
		typ, data, err := t.conn.Read(context.Background())
		if err != nil {
			t.end(closeErr(err))
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var m struct {
			Type    string `json:"type"`
			IsFinal bool   `json:"is_final"`
			Text    string `json:"text"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "transcript":
			if m.IsFinal {
				t.sure(m.Text)
			} else {
				t.guess(m.Text)
			}
		case "flush_done":
			t.done()
		case "done":
			t.done()
		case "error":
			t.end(fmt.Errorf("cartesia: %s", firstOf(m.Message, "error")))
			return
		}
	}
}

// ListModels is Cartesia's current models (it has no list endpoint).
func (c *cartesia) ListModels(context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "sonic-3.6", Name: "Sonic 3.6 (fastest)", Kind: KindSpeech},
		{ID: "sonic-3.5", Name: "Sonic 3.5", Kind: KindSpeech},
		{ID: "sonic-3", Name: "Sonic 3", Kind: KindSpeech},
		{ID: "ink-whisper", Name: "Ink Whisper", Kind: KindTranscription},
		{ID: "ink-2", Name: "Ink 2", Kind: KindTranscription},
	}, nil
}

// Voices lists the voices (GET /voices), a few pages at most.
func (c *cartesia) Voices(ctx context.Context, _ string) ([]Voice, error) {
	var out []Voice
	after := ""
	for page := 0; page < 5; page++ {
		q := url.Values{"limit": {"100"}}
		if after != "" {
			q.Set("starting_after", after)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/voices?"+q.Encode(), nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Cartesia-Version", cartesiaVersion)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return out, err
		}
		if res.StatusCode != http.StatusOK {
			err := vendorError("cartesia", res, c.key)
			res.Body.Close()
			return out, err
		}
		var body struct {
			Data []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Language string `json:"language"`
			} `json:"data"`
			HasMore bool `json:"has_more"`
		}
		err = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if err != nil {
			return out, fmt.Errorf("cartesia: %w", err)
		}
		for _, v := range body.Data {
			out = append(out, Voice{ID: v.ID, Name: v.Name, Language: v.Language})
		}
		if !body.HasMore || len(body.Data) == 0 {
			break
		}
		after = body.Data[len(body.Data)-1].ID
	}
	return out, nil
}
