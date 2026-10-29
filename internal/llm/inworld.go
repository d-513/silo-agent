package llm

// Inworld: TTS 2 speech, one request (newline-delimited JSON around the
// audio) or a bidirectional socket a call keeps open (a context per piece,
// done at contextClosed, with word timestamps).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const inworldBase = "https://api.inworld.ai"

type inworld struct {
	speechOnly
	key  string
	base string
}

func newInworld(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	base := strings.TrimRight(s.Get("base_url"), "/")
	if base == "" {
		base = inworldBase
	}
	return &inworld{speechOnly: speechOnly{"Inworld"}, key: key, base: base}, nil
}

// header is Basic auth with the key as the Portal gives it (already
// base64).
func (i *inworld) header() http.Header { return http.Header{"Authorization": {"Basic " + i.key}} }

func (i *inworld) SpeechCaps(string) SpeechCaps { return SpeechCaps{Live: true, Words: true} }

func inworldAudio(speed float64) map[string]any {
	a := map[string]any{"audioEncoding": "PCM", "sampleRateHertz": SpeechSampleRate}
	if speed > 0 && speed != 1 {
		a["speakingRate"] = min(max(speed, 0.5), 1.5)
	}
	return a
}

func (i *inworld) Speak(ctx context.Context, model string, req SpeechRequest) (Speech, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Speech{}, fmt.Errorf("nothing to say")
	}
	if req.Voice == "" {
		return Speech{}, fmt.Errorf("inworld: pick a voice for this Bot (Settings → Voice)")
	}
	body := map[string]any{"text": text, "voiceId": req.Voice, "modelId": model, "audioConfig": inworldAudio(req.Speed)}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, i.base+"/tts/v1/voice:stream", jsonBody(body))
	if err != nil {
		return Speech{}, err
	}
	hreq.Header = i.header()
	hreq.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return Speech{}, err
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		return Speech{}, vendorError("inworld", res, i.key)
	}
	pcm := pcmPipe(res.Body, func(r io.Reader, w io.Writer) error {
		return streamLines(r, func(line []byte) error {
			var m struct {
				Result struct {
					AudioContent string `json:"audioContent"`
				} `json:"result"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(line, &m) != nil {
				return nil
			}
			if m.Error != nil {
				return fmt.Errorf("inworld: %s", m.Error.Message)
			}
			if m.Result.AudioContent != "" {
				b, _ := base64.StdEncoding.DecodeString(m.Result.AudioContent)
				_, err := w.Write(b)
				return err
			}
			return nil
		})
	})
	return Speech{Body: pcm, Format: "pcm", SampleRate: SpeechSampleRate}, nil
}

// OpenSpeech opens the bidirectional socket: each piece is a context that
// is created, sent with a flush, and closed; contextClosed ends it.
func (i *inworld) OpenSpeech(ctx context.Context, model string, req SpeechRequest) (SpeechConn, error) {
	if req.Voice == "" {
		return nil, fmt.Errorf("inworld: pick a voice for this Bot (Settings → Voice)")
	}
	conn, err := dialWS(ctx, "ws"+strings.TrimPrefix(i.base, "http")+"/tts/v1/voice:streamBidirectional", i.header())
	if err != nil {
		return nil, fmt.Errorf("inworld: %w", err)
	}
	create := map[string]any{"voiceId": req.Voice, "modelId": model, "audioConfig": inworldAudio(req.Speed), "timestampType": "WORD", "timestampTransportStrategy": "SYNC"}
	v := newCtxVoice(conn, SpeechSampleRate)
	v.start = func(id, text string) []any {
		return []any{
			map[string]any{"contextId": id, "create": create},
			map[string]any{"contextId": id, "sendText": map[string]any{"text": strings.TrimSpace(text), "flushContext": map[string]any{}}},
			map[string]any{"contextId": id, "closeContext": map[string]any{}},
		}
	}
	v.stop = func(id string) any { return map[string]any{"contextId": id, "cancelContext": map[string]any{}} }
	v.read = readInworld
	go v.run()
	return v, nil
}

func readInworld(data []byte) voiceEvent {
	var m struct {
		Result *struct {
			ContextID  string `json:"contextId"`
			AudioChunk *struct {
				AudioContent  string `json:"audioContent"`
				TimestampInfo *struct {
					Word *struct {
						Words  []string  `json:"words"`
						Starts []float64 `json:"wordStartTimeSeconds"`
						Ends   []float64 `json:"wordEndTimeSeconds"`
					} `json:"wordAlignment"`
				} `json:"timestampInfo"`
			} `json:"audioChunk"`
			ContextClosed *struct{} `json:"contextClosed"`
			Status        *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"status"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &m) != nil {
		return voiceEvent{}
	}
	if m.Error != nil {
		return voiceEvent{err: fmt.Errorf("inworld: %s", m.Error.Message)}
	}
	r := m.Result
	if r == nil {
		return voiceEvent{}
	}
	ev := voiceEvent{ctx: r.ContextID}
	if r.Status != nil && r.Status.Code != 0 {
		ev.err = fmt.Errorf("inworld: %s", firstOf(r.Status.Message, "error"))
		return ev
	}
	if a := r.AudioChunk; a != nil {
		ev.pcm, _ = base64.StdEncoding.DecodeString(a.AudioContent)
		if a.TimestampInfo != nil && a.TimestampInfo.Word != nil {
			w := a.TimestampInfo.Word
			for k, word := range w.Words {
				if k < len(w.Starts) && k < len(w.Ends) {
					ev.words = append(ev.words, SpeechWord{Text: word, StartMs: int(w.Starts[k] * 1000), EndMs: int(w.Ends[k] * 1000)})
				}
			}
		}
	}
	ev.done = r.ContextClosed != nil
	return ev
}

// ListModels is Inworld's current speech models (no list endpoint).
func (i *inworld) ListModels(context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "inworld-tts-2-flash", Name: "Inworld TTS 2 Flash (fastest)", Kind: KindSpeech},
		{ID: "inworld-tts-2", Name: "Inworld TTS 2", Kind: KindSpeech},
	}, nil
}

// Voices lists the voices (GET /voices/v1/voices).
func (i *inworld) Voices(ctx context.Context, _ string) ([]Voice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.base+"/voices/v1/voices?"+url.Values{"pageSize": {"2000"}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header = i.header()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, vendorError("inworld", res, i.key)
	}
	var body struct {
		Voices []struct {
			ID       string `json:"voiceId"`
			Name     string `json:"displayName"`
			Language string `json:"languageCode"`
		} `json:"voices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("inworld: %w", err)
	}
	out := make([]Voice, 0, len(body.Voices))
	for _, v := range body.Voices {
		out = append(out, Voice{ID: v.ID, Name: firstOf(v.Name, v.ID), Language: v.Language})
	}
	return out, nil
}
