package llm

// Soniox: real-time transcription over a socket. Final tokens are sent once
// and appended, the rest are the current guess; an "<end>" token is the
// model judging the end of a turn, and "<fin>" answers a finalize. A file
// goes through a live session (Soniox's batch API is upload-and-poll).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const sonioxBase = "https://api.soniox.com"

type soniox struct {
	speechOnly
	key  string
	base string
	ws   string
}

func newSoniox(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	base := strings.TrimRight(s.Get("base_url"), "/")
	ws := "wss://stt-rt.soniox.com"
	if base == "" {
		base = sonioxBase
	} else {
		ws = "ws" + strings.TrimPrefix(base, "http")
	}
	return &soniox{speechOnly: speechOnly{"Soniox"}, key: key, base: base, ws: ws}, nil
}

// SpeechCaps: the real-time models stream and detect the end of a turn.
func (s *soniox) SpeechCaps(model string) SpeechCaps {
	if strings.Contains(model, "async") {
		return SpeechCaps{}
	}
	return SpeechCaps{Live: true, EndOfTurn: true}
}

func (s *soniox) Transcribe(ctx context.Context, model string, a Audio) (Transcript, error) {
	if strings.Contains(model, "async") {
		return Transcript{}, fmt.Errorf("soniox: %s takes files through the upload API, which is not wired; pick a real-time model (stt-rt-v5)", model)
	}
	return transcribeByLive(ctx, s, model, a)
}

func (s *soniox) OpenTranscription(ctx context.Context, model string, opts LiveOptions) (TranscriptionSession, error) {
	conn, err := dialWS(ctx, s.ws+"/transcribe-websocket", http.Header{"Authorization": {"Bearer " + s.key}})
	if err != nil {
		return nil, fmt.Errorf("soniox: %w", err)
	}
	cfg := map[string]any{"model": model, "audio_format": "pcm_s16le", "sample_rate": LiveSampleRate, "num_channels": 1, "enable_endpoint_detection": true}
	if opts.Language != "" {
		cfg["language_hints"] = []string{opts.Language}
	}
	t := newWSTurn(conn)
	if err := t.writeJSON(cfg); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("soniox: %w", err)
	}
	t.finish = []any{map[string]string{"type": "finalize"}}
	t.bye = ""
	t.keepalive(15*time.Second, map[string]string{"type": "keepalive"})
	go readSoniox(t)
	return t, nil
}

func readSoniox(t *wsTurn) {
	var final strings.Builder
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			t.end(closeErr(err))
			return
		}
		var m struct {
			Tokens []struct {
				Text    string `json:"text"`
				IsFinal bool   `json:"is_final"`
			} `json:"tokens"`
			Finished     bool   `json:"finished"`
			ErrorType    string `json:"error_type"`
			ErrorMessage string `json:"error_message"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ErrorType != "" || m.ErrorMessage != "" {
			t.end(fmt.Errorf("soniox: %s", firstOf(m.ErrorMessage, m.ErrorType)))
			return
		}
		var guess strings.Builder
		endpoint, fin := false, false
		for _, tok := range m.Tokens {
			switch tok.Text {
			case "<end>":
				endpoint = true
				continue
			case "<fin>":
				fin = true
				continue
			}
			if tok.IsFinal {
				final.WriteString(tok.Text)
			} else {
				guess.WriteString(tok.Text)
			}
		}
		// Tokens carry their own spaces: the turn is their concatenation.
		t.set(final.String(), guess.String())
		if endpoint && !t.isFinishing() {
			t.endOfTurn()
		}
		if fin || m.Finished {
			t.done()
		}
	}
}

// ListModels lists the models (GET /v1/models).
func (s *soniox) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, vendorError("soniox", res, s.key)
	}
	var body struct {
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Mode string `json:"transcription_mode"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("soniox: %w", err)
	}
	var out []ModelInfo
	for _, m := range body.Models {
		name := m.Name
		if m.Mode == "real_time" {
			name = strings.TrimSpace(name + " (live)")
		}
		out = append(out, ModelInfo{ID: m.ID, Name: name, Kind: KindTranscription})
	}
	return out, nil
}
