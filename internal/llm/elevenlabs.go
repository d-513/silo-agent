package llm

// ElevenLabs: speech (one streamed request, or the multi-context socket a
// call keeps open, with character timings), and Scribe transcription (a
// file in one request, or Scribe v2 Realtime live).

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const elevenLabsBase = "https://api.elevenlabs.io"

type elevenLabs struct {
	speechOnly
	key  string
	base string
}

func newElevenLabs(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	base := strings.TrimRight(s.Get("base_url"), "/")
	if base == "" {
		base = elevenLabsBase
	}
	return &elevenLabs{speechOnly: speechOnly{"ElevenLabs"}, key: key, base: base}, nil
}

func (e *elevenLabs) header() http.Header { return http.Header{"xi-api-key": {e.key}} }

func (e *elevenLabs) ws() string { return "ws" + strings.TrimPrefix(e.base, "http") }

// isScribe says a model is a transcription one.
func isScribe(model string) bool { return strings.HasPrefix(model, "scribe") }

// SpeechCaps: Scribe's realtime models stream; the speech models that the
// socket takes (not v3/v4) stream with character timings.
func (e *elevenLabs) SpeechCaps(model string) SpeechCaps {
	if isScribe(model) {
		return SpeechCaps{Live: strings.Contains(model, "realtime")}
	}
	if strings.Contains(model, "_v3") || strings.Contains(model, "_v4") {
		return SpeechCaps{}
	}
	return SpeechCaps{Live: true, Words: true}
}

// voiceSettings carries the speed (0.7–1.2) when one is asked for.
func elevenSettings(speed float64) map[string]any {
	if speed <= 0 || speed == 1 {
		return nil
	}
	return map[string]any{"speed": min(max(speed, 0.7), 1.2)}
}

// elevenLanguage is the language_code only the newer models take.
func elevenLanguage(model, lang string) string {
	if lang != "" && (strings.Contains(model, "flash") || strings.Contains(model, "turbo")) {
		return lang
	}
	return ""
}

func (e *elevenLabs) Speak(ctx context.Context, model string, req SpeechRequest) (Speech, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Speech{}, fmt.Errorf("nothing to say")
	}
	if req.Voice == "" {
		return Speech{}, fmt.Errorf("elevenlabs: pick a voice for this Bot (Settings → Voice)")
	}
	body := map[string]any{"text": text, "model_id": model}
	if vs := elevenSettings(req.Speed); vs != nil {
		body["voice_settings"] = vs
	}
	if l := elevenLanguage(model, req.Language); l != "" {
		body["language_code"] = l
	}
	u := e.base + "/v1/text-to-speech/" + url.PathEscape(req.Voice) + "/stream?output_format=pcm_24000"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, jsonBody(body))
	if err != nil {
		return Speech{}, err
	}
	hreq.Header = e.header()
	hreq.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return Speech{}, err
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		return Speech{}, vendorError("elevenlabs", res, e.key)
	}
	return Speech{Body: res.Body, Format: "pcm", SampleRate: SpeechSampleRate}, nil
}

// OpenSpeech opens the multi-context socket: each piece is a context that
// is flushed and closed, and done when its isFinal arrives.
func (e *elevenLabs) OpenSpeech(ctx context.Context, model string, req SpeechRequest) (SpeechConn, error) {
	if req.Voice == "" {
		return nil, fmt.Errorf("elevenlabs: pick a voice for this Bot (Settings → Voice)")
	}
	q := url.Values{"model_id": {model}, "output_format": {"pcm_24000"}, "inactivity_timeout": {"180"}}
	if l := elevenLanguage(model, req.Language); l != "" {
		q.Set("language_code", l)
	}
	conn, err := dialWS(ctx, e.ws()+"/v1/text-to-speech/"+url.PathEscape(req.Voice)+"/multi-stream-input?"+q.Encode(), e.header())
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: %w", err)
	}
	settings := elevenSettings(req.Speed)
	v := newCtxVoice(conn, SpeechSampleRate)
	v.start = func(id, text string) []any {
		first := map[string]any{"text": " ", "context_id": id}
		if settings != nil {
			first["voice_settings"] = settings
		}
		return []any{
			first,
			map[string]any{"text": strings.TrimSpace(text) + " ", "context_id": id, "flush": true},
			map[string]any{"context_id": id, "close_context": true},
		}
	}
	v.stop = func(id string) any { return map[string]any{"context_id": id, "close_context": true} }
	v.bye = map[string]any{"close_socket": true}
	v.read = readEleven
	go v.run()
	return v, nil
}

// readEleven reads one multi-context message: audio with its characters'
// timings (from the start of this chunk), or the end of a context.
func readEleven(data []byte) voiceEvent {
	var m struct {
		Audio      string `json:"audio"`
		ContextID  string `json:"contextId"`
		ContextID2 string `json:"context_id"`
		IsFinal    *bool  `json:"isFinal"`
		IsFinal2   *bool  `json:"is_final"`
		Error      any    `json:"error"`
		Message    string `json:"message"`
		Alignment  *struct {
			Chars     []string `json:"chars"`
			Starts    []int    `json:"charStartTimesMs"`
			Durations []int    `json:"charDurationsMs"`
		} `json:"alignment"`
	}
	if json.Unmarshal(data, &m) != nil {
		return voiceEvent{}
	}
	ev := voiceEvent{ctx: firstOf(m.ContextID, m.ContextID2)}
	if m.Error != nil {
		ev.err = fmt.Errorf("elevenlabs: %v", firstOf(fmt.Sprint(textOf(m.Error)), m.Message))
		return ev
	}
	if m.Audio != "" {
		ev.pcm, _ = base64.StdEncoding.DecodeString(m.Audio)
	}
	if a := m.Alignment; a != nil {
		for i, c := range a.Chars {
			if i < len(a.Starts) && i < len(a.Durations) {
				ev.chars = append(ev.chars, charTime{c: c, startMs: a.Starts[i], endMs: a.Starts[i] + a.Durations[i]})
			}
		}
	}
	ev.done = m.IsFinal != nil && *m.IsFinal || m.IsFinal2 != nil && *m.IsFinal2
	return ev
}

// Transcribe sends a file to Scribe; a realtime model only streams, so it
// goes through a live session.
func (e *elevenLabs) Transcribe(ctx context.Context, model string, a Audio) (Transcript, error) {
	if strings.Contains(model, "realtime") {
		return transcribeByLive(ctx, e, model, a)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model_id", model)
	if a.Language != "" {
		_ = w.WriteField("language_code", a.Language)
	}
	name := a.Filename
	if name == "" {
		name = "audio" + AudioExt(a.MIME)
	}
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		return Transcript{}, err
	}
	_, _ = fw.Write(a.Data)
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/v1/speech-to-text", &buf)
	if err != nil {
		return Transcript{}, err
	}
	req.Header = e.header()
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Transcript{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Transcript{}, vendorError("elevenlabs", res, e.key)
	}
	var body struct {
		Text         string `json:"text"`
		LanguageCode string `json:"language_code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Transcript{}, fmt.Errorf("elevenlabs: %w", err)
	}
	return Transcript{Text: strings.TrimSpace(body.Text), Language: body.LanguageCode}, nil
}

// OpenTranscription opens Scribe v2 Realtime with manual commits: partial
// transcripts are the guess, the committed one after Finish's commit is the
// turn.
func (e *elevenLabs) OpenTranscription(ctx context.Context, model string, opts LiveOptions) (TranscriptionSession, error) {
	q := url.Values{"model_id": {model}, "audio_format": {"pcm_16000"}, "commit_strategy": {"manual"}}
	if opts.Language != "" {
		q.Set("language_code", opts.Language)
	}
	conn, err := dialWS(ctx, e.ws()+"/v1/speech-to-text/realtime?"+q.Encode(), e.header())
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: %w", err)
	}
	t := newWSTurn(conn)
	chunk := func(pcm []byte, commit bool) []byte {
		b, _ := json.Marshal(map[string]any{"message_type": "input_audio_chunk", "audio_base_64": base64.StdEncoding.EncodeToString(pcm), "commit": commit, "sample_rate": LiveSampleRate})
		return b
	}
	t.audio = func(pcm []byte) (websocket.MessageType, []byte) { return websocket.MessageText, chunk(pcm, false) }
	t.finish = []any{string(chunk(nil, true))}
	t.grace = 4 * time.Second
	go readScribe(t)
	return t, nil
}

func readScribe(t *wsTurn) {
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			t.end(closeErr(err))
			return
		}
		var m struct {
			Type  string `json:"message_type"`
			Text  string `json:"text"`
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "partial_transcript":
			t.guess(m.Text)
		case "committed_transcript", "committed_transcript_with_timestamps":
			t.sure(m.Text)
			if t.isFinishing() {
				t.done()
			}
		case "session_started", "warning":
		default:
			if m.Error != "" || strings.Contains(m.Type, "error") || strings.HasSuffix(m.Type, "exceeded") || m.Type == "rate_limited" {
				t.end(fmt.Errorf("elevenlabs: %s", firstOf(m.Error, m.Type)))
				return
			}
		}
	}
}

// ListModels lists the speech models (GET /v1/models) and the Scribe
// models, which that list does not hold.
func (e *elevenLabs) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header = e.header()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, vendorError("elevenlabs", res, e.key)
	}
	var list []struct {
		ID   string `json:"model_id"`
		Name string `json:"name"`
		TTS  bool   `json:"can_do_text_to_speech"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("elevenlabs: %w", err)
	}
	out := []ModelInfo{
		{ID: "scribe_v2", Name: "Scribe v2 (files)", Kind: KindTranscription},
		{ID: "scribe_v2_realtime", Name: "Scribe v2 Realtime (live)", Kind: KindTranscription},
	}
	for _, m := range list {
		if m.TTS {
			out = append(out, ModelInfo{ID: m.ID, Name: m.Name, Kind: KindSpeech})
		}
	}
	return out, nil
}

// Voices lists the account's voices (GET /v2/voices), a few pages at most.
func (e *elevenLabs) Voices(ctx context.Context, _ string) ([]Voice, error) {
	var out []Voice
	token := ""
	for page := 0; page < 5; page++ {
		q := url.Values{"page_size": {"100"}}
		if token != "" {
			q.Set("next_page_token", token)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base+"/v2/voices?"+q.Encode(), nil)
		if err != nil {
			return out, err
		}
		req.Header = e.header()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return out, err
		}
		var body struct {
			Voices []struct {
				ID        string            `json:"voice_id"`
				Name      string            `json:"name"`
				Labels    map[string]string `json:"labels"`
				Languages []struct {
					Language string `json:"language"`
				} `json:"verified_languages"`
			} `json:"voices"`
			HasMore bool   `json:"has_more"`
			Next    string `json:"next_page_token"`
		}
		if res.StatusCode != http.StatusOK {
			err := vendorError("elevenlabs", res, e.key)
			res.Body.Close()
			return out, err
		}
		err = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if err != nil {
			return out, fmt.Errorf("elevenlabs: %w", err)
		}
		for _, v := range body.Voices {
			lang := v.Labels["language"]
			if len(v.Languages) > 0 {
				lang = v.Languages[0].Language
			}
			name := v.Name
			if accent := v.Labels["accent"]; accent != "" {
				name += " (" + accent + ")"
			}
			out = append(out, Voice{ID: v.ID, Name: name, Language: lang})
		}
		if !body.HasMore || body.Next == "" {
			break
		}
		token = body.Next
	}
	return out, nil
}

