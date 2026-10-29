package llm

// AssemblyAI: Universal Streaming v3 over a socket (each Turn message is the
// whole turn so far; end_of_turn with formatting is final, and the model
// judges the end of a turn itself), and the Sync API for a file in one
// request.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type assemblyAI struct {
	speechOnly
	key  string
	ws   string
	sync string
}

func newAssemblyAI(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	a := &assemblyAI{speechOnly: speechOnly{"AssemblyAI"}, key: key, ws: "wss://streaming.assemblyai.com", sync: "https://sync.assemblyai.com"}
	if base := strings.TrimRight(s.Get("base_url"), "/"); base != "" {
		a.ws, a.sync = "ws"+strings.TrimPrefix(base, "http"), base
	}
	return a, nil
}

func (a *assemblyAI) SpeechCaps(string) SpeechCaps { return SpeechCaps{Live: true, EndOfTurn: true} }

func (a *assemblyAI) OpenTranscription(ctx context.Context, model string, opts LiveOptions) (TranscriptionSession, error) {
	q := url.Values{"sample_rate": {strconv.Itoa(LiveSampleRate)}, "encoding": {"pcm_s16le"}, "speech_model": {model}, "format_turns": {"true"}}
	if opts.Language == "" && !strings.Contains(model, "english") {
		q.Set("language_detection", "true")
	}
	conn, err := dialWS(ctx, a.ws+"/v3/ws?"+q.Encode(), http.Header{"Authorization": {a.key}})
	if err != nil {
		return nil, fmt.Errorf("assemblyai: %w", err)
	}
	t := newWSTurn(conn)
	t.finish = []any{map[string]string{"type": "ForceEndpoint"}}
	t.bye = map[string]string{"type": "Terminate"}
	t.grace = 3 * time.Second
	go readAssembly(t)
	return t, nil
}

func readAssembly(t *wsTurn) {
	// Each turn's newest text, by turn_order; a formatted final is kept.
	turns := map[int]string{}
	final := map[int]bool{}
	last := 0
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			t.end(closeErr(err))
			return
		}
		var m struct {
			Type       string `json:"type"`
			TurnOrder  int    `json:"turn_order"`
			Formatted  bool   `json:"turn_is_formatted"`
			EndOfTurn  bool   `json:"end_of_turn"`
			Transcript string `json:"transcript"`
			Error      string `json:"error"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "Turn":
			done := m.EndOfTurn && m.Formatted
			if !final[m.TurnOrder] || done {
				turns[m.TurnOrder] = m.Transcript
			}
			if done {
				final[m.TurnOrder] = true
			}
			last = max(last, m.TurnOrder)
			parts := make([]string, 0, last+1)
			for i := 0; i <= last; i++ {
				parts = append(parts, turns[i])
			}
			t.set(joinWords(parts), "")
			if done {
				if t.isFinishing() {
					t.done()
				} else {
					t.endOfTurn()
				}
			}
		case "Termination":
			t.done()
		case "Error":
			t.end(fmt.Errorf("assemblyai: %s", firstOf(m.Error, "error")))
			return
		}
	}
}

// Transcribe sends a file to the Sync API in one request.
func (a *assemblyAI) Transcribe(ctx context.Context, model string, au Audio) (Transcript, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	name := au.Filename
	if name == "" {
		name = "audio" + AudioExt(au.MIME)
	}
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="audio"; filename="` + name + `"`}
	h["Content-Type"] = []string{firstOf(au.MIME, "application/octet-stream")}
	part, err := w.CreatePart(h)
	if err != nil {
		return Transcript{}, err
	}
	_, _ = part.Write(au.Data)
	if au.Language != "" {
		cfg, _ := json.Marshal(map[string]any{"language_codes": []string{au.Language}})
		ch := map[string][]string{"Content-Disposition": {`form-data; name="config"`}, "Content-Type": {"application/json"}}
		if p, err := w.CreatePart(ch); err == nil {
			_, _ = p.Write(cfg)
		}
	}
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.sync+"/v1/transcribe", &buf)
	if err != nil {
		return Transcript{}, err
	}
	req.Header.Set("Authorization", a.key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	syncModel := model
	if strings.HasPrefix(model, "universal-streaming") {
		syncModel = "universal-3-5-pro"
	}
	req.Header.Set("X-AAI-Model", syncModel)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Transcript{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Transcript{}, vendorError("assemblyai", res, a.key)
	}
	var body struct {
		Text            string  `json:"text"`
		AudioDurationMs float64 `json:"audio_duration_ms"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Transcript{}, fmt.Errorf("assemblyai: %w", err)
	}
	return Transcript{Text: strings.TrimSpace(body.Text), Language: au.Language, Seconds: body.AudioDurationMs / 1000}, nil
}

// ListModels is AssemblyAI's streaming models (no list endpoint).
func (a *assemblyAI) ListModels(context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "universal-3-6-pro", Name: "Universal-3.6 Pro (32 languages)", Kind: KindTranscription},
		{ID: "universal-3-5-pro", Name: "Universal-3.5 Pro", Kind: KindTranscription},
		{ID: "universal-streaming-multilingual", Name: "Universal Streaming multilingual", Kind: KindTranscription},
		{ID: "universal-streaming-english", Name: "Universal Streaming English", Kind: KindTranscription},
	}, nil
}
