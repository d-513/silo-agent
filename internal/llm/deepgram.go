package llm

// Deepgram: Nova and Flux transcription (live over a socket, or one
// request), and Aura speech (one request, or a socket that stays open for a
// call). A voice is part of an Aura model's id (aura-2-thalia-en), so there
// is no voice to pick.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const deepgramBase = "https://api.deepgram.com"

type deepgram struct {
	speechOnly
	key  string
	base string
}

func newDeepgram(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	base := strings.TrimRight(s.Get("base_url"), "/")
	if base == "" {
		base = deepgramBase
	}
	return &deepgram{speechOnly: speechOnly{"Deepgram"}, key: key, base: base}, nil
}

func (d *deepgram) ws() string {
	return "ws" + strings.TrimPrefix(d.base, "http")
}

func (d *deepgram) header() http.Header {
	return http.Header{"Authorization": {"Token " + d.key}}
}

func isAura(model string) bool { return strings.HasPrefix(model, "aura") }
func isFlux(model string) bool { return strings.HasPrefix(model, "flux") }

// SpeechCaps: every Deepgram model streams; Flux also judges the end of a
// turn. Aura gives no word timings on its socket.
func (d *deepgram) SpeechCaps(model string) SpeechCaps {
	return SpeechCaps{Live: true, EndOfTurn: isFlux(model)}
}

// Transcribe is one pre-recorded request (any container Deepgram reads); a
// Flux model only streams, so it goes through a live session.
func (d *deepgram) Transcribe(ctx context.Context, model string, a Audio) (Transcript, error) {
	if isFlux(model) {
		return transcribeByLive(ctx, d, model, a)
	}
	q := url.Values{"model": {model}, "smart_format": {"true"}, "punctuate": {"true"}}
	if a.Language != "" {
		q.Set("language", a.Language)
	} else {
		q.Set("detect_language", "true")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/v1/listen?"+q.Encode(), strings.NewReader(string(a.Data)))
	if err != nil {
		return Transcript{}, err
	}
	req.Header = d.header()
	if a.MIME != "" {
		req.Header.Set("Content-Type", a.MIME)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Transcript{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Transcript{}, vendorError("deepgram", res, d.key)
	}
	var body struct {
		Metadata struct {
			Duration float64 `json:"duration"`
		} `json:"metadata"`
		Results struct {
			Channels []struct {
				DetectedLanguage string `json:"detected_language"`
				Alternatives     []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Transcript{}, fmt.Errorf("deepgram: %w", err)
	}
	out := Transcript{Seconds: body.Metadata.Duration, Language: a.Language}
	if ch := body.Results.Channels; len(ch) > 0 {
		if ch[0].DetectedLanguage != "" {
			out.Language = ch[0].DetectedLanguage
		}
		if len(ch[0].Alternatives) > 0 {
			out.Text = strings.TrimSpace(ch[0].Alternatives[0].Transcript)
		}
	}
	return out, nil
}

// OpenTranscription opens a live session: Flux on /v2/listen, the rest on
// /v1/listen.
func (d *deepgram) OpenTranscription(ctx context.Context, model string, opts LiveOptions) (TranscriptionSession, error) {
	q := url.Values{"model": {model}, "encoding": {"linear16"}, "sample_rate": {strconv.Itoa(LiveSampleRate)}}
	if isFlux(model) {
		if opts.Language != "" && strings.Contains(model, "multi") {
			q.Set("language_hint", opts.Language)
		}
		conn, err := dialWS(ctx, d.ws()+"/v2/listen?"+q.Encode(), d.header())
		if err != nil {
			return nil, fmt.Errorf("deepgram: %w", err)
		}
		t := newWSTurn(conn)
		t.finish = []any{map[string]string{"type": "ForceEndTurn"}}
		t.bye = map[string]string{"type": "CloseStream"}
		go d.readFlux(t)
		return t, nil
	}
	q.Set("channels", "1")
	q.Set("interim_results", "true")
	q.Set("punctuate", "true")
	q.Set("smart_format", "true")
	q.Set("endpointing", "300")
	switch {
	case opts.Language != "":
		q.Set("language", opts.Language)
	case strings.HasPrefix(model, "nova-3"):
		q.Set("language", "multi")
	}
	conn, err := dialWS(ctx, d.ws()+"/v1/listen?"+q.Encode(), d.header())
	if err != nil {
		return nil, fmt.Errorf("deepgram: %w", err)
	}
	t := newWSTurn(conn)
	t.finish = []any{map[string]string{"type": "Finalize"}}
	t.bye = map[string]string{"type": "CloseStream"}
	t.grace = 2 * time.Second
	t.keepalive(4*time.Second, map[string]string{"type": "KeepAlive"})
	go d.readNova(t)
	return t, nil
}

// readNova reads /v1/listen: interim results replace the guess, final ones
// add a segment, and the result of a Finalize ends the turn.
func (d *deepgram) readNova(t *wsTurn) {
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			t.end(closeErr(err))
			return
		}
		var m struct {
			Type         string `json:"type"`
			IsFinal      bool   `json:"is_final"`
			FromFinalize bool   `json:"from_finalize"`
			Description  string `json:"description"`
			Message      string `json:"message"`
			Channel      struct {
				Alternatives []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channel"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "Results":
			text := ""
			if len(m.Channel.Alternatives) > 0 {
				text = m.Channel.Alternatives[0].Transcript
			}
			if m.IsFinal {
				t.sure(text)
			} else {
				t.guess(text)
			}
			if m.FromFinalize {
				t.done()
			}
		case "Error":
			t.end(fmt.Errorf("deepgram: %s", firstOf(m.Description, m.Message, "error")))
			return
		}
	}
}

// readFlux reads /v2/listen: each turn's transcript is cumulative, an end
// of turn makes it final, and the one ForceEndTurn asked for ends the
// session's turn.
func (d *deepgram) readFlux(t *wsTurn) {
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			t.end(closeErr(err))
			return
		}
		var m struct {
			Type        string `json:"type"`
			Event       string `json:"event"`
			Transcript  string `json:"transcript"`
			Trigger     string `json:"trigger"`
			Code        string `json:"code"`
			Description string `json:"description"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "TurnInfo":
			switch m.Event {
			case "EndOfTurn":
				t.sure(m.Transcript)
				if m.Trigger == "manual" || t.isFinishing() {
					t.done()
				} else {
					t.endOfTurn()
				}
			default:
				t.guess(m.Transcript)
			}
		case "Warning":
			if m.Code == "FORCE_END_TURN_NO_ACTIVE_TURN" {
				t.done()
			}
		case "Error":
			t.end(fmt.Errorf("deepgram: %s", firstOf(m.Description, m.Code, "error")))
			return
		}
	}
}

// Speak is one Aura request: raw 16-bit PCM at 24 kHz, streamed.
func (d *deepgram) Speak(ctx context.Context, model string, req SpeechRequest) (Speech, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Speech{}, fmt.Errorf("nothing to say")
	}
	q := url.Values{"model": {model}, "encoding": {"linear16"}, "container": {"none"}, "sample_rate": {strconv.Itoa(SpeechSampleRate)}}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/v1/speak?"+q.Encode(), jsonBody(map[string]string{"text": text}))
	if err != nil {
		return Speech{}, err
	}
	hreq.Header = d.header()
	hreq.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return Speech{}, err
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		return Speech{}, vendorError("deepgram", res, d.key)
	}
	return Speech{Body: res.Body, Format: "pcm", SampleRate: SpeechSampleRate}, nil
}

// OpenSpeech keeps an Aura socket open for a call.
func (d *deepgram) OpenSpeech(ctx context.Context, model string, req SpeechRequest) (SpeechConn, error) {
	q := url.Values{"model": {model}, "encoding": {"linear16"}, "sample_rate": {strconv.Itoa(SpeechSampleRate)}}
	if req.Speed > 0 && req.Speed != 1 {
		q.Set("speed", strconv.FormatFloat(min(max(req.Speed, 0.7), 1.5), 'f', 2, 64))
	}
	conn, err := dialWS(ctx, d.ws()+"/v1/speak?"+q.Encode(), d.header())
	if err != nil {
		return nil, fmt.Errorf("deepgram: %w", err)
	}
	v := &dgVoice{conn: conn, ended: make(chan struct{})}
	go v.run()
	return v, nil
}

// dgVoice is an Aura socket: Speak and Flush say one piece, Flushed says it
// is complete, Clear drops what is left of it.
type dgVoice struct {
	conn  *websocket.Conn
	say   sync.Mutex
	mu    sync.Mutex
	audio func([]byte)
	done  chan error
	// clearing drops audio until the server says the Clear is done.
	clearing bool
	ended    chan struct{}
	once     sync.Once
}

func (v *dgVoice) run() {
	for {
		typ, data, err := v.conn.Read(context.Background())
		if err != nil {
			v.fail()
			return
		}
		v.mu.Lock()
		audio, done, clearing := v.audio, v.done, v.clearing
		v.mu.Unlock()
		if typ == websocket.MessageBinary {
			if audio != nil && !clearing {
				audio(data)
			}
			continue
		}
		var m struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "Flushed":
			if done != nil && !clearing {
				select {
				case done <- nil:
				default:
				}
			}
		case "Cleared":
			v.mu.Lock()
			v.clearing = false
			v.mu.Unlock()
		case "Error":
			if done != nil {
				select {
				case done <- fmt.Errorf("deepgram: %s", m.Description):
				default:
				}
			}
		}
	}
}

func (v *dgVoice) fail() {
	v.once.Do(func() { close(v.ended) })
}

func (v *dgVoice) Rate() int { return SpeechSampleRate }

func (v *dgVoice) Say(ctx context.Context, text string, audio func([]byte), _ func([]SpeechWord)) error {
	v.say.Lock()
	defer v.say.Unlock()
	select {
	case <-v.ended:
		return ErrSpeechClosed
	default:
	}
	done := make(chan error, 2)
	v.mu.Lock()
	v.audio, v.done = audio, done
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.audio, v.done = nil, nil
		v.mu.Unlock()
	}()
	for _, m := range []map[string]string{{"type": "Speak", "text": text}, {"type": "Flush"}} {
		if err := writeJSON(ctx, v.conn, m); err != nil {
			v.fail()
			return ErrSpeechClosed
		}
	}
	select {
	case err := <-done:
		return err
	case <-v.ended:
		return ErrSpeechClosed
	case <-ctx.Done():
		v.mu.Lock()
		v.clearing = true
		v.mu.Unlock()
		_ = writeJSON(context.Background(), v.conn, map[string]string{"type": "Clear"})
		return ctx.Err()
	}
}

func (v *dgVoice) Close() error {
	_ = writeJSON(context.Background(), v.conn, map[string]string{"type": "Close"})
	v.fail()
	return v.conn.Close(websocket.StatusNormalClosure, "")
}

// ListModels lists Deepgram's transcription and Aura models (each Aura
// model is one voice); Flux is not in the list, so it is added.
func (d *deepgram) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header = d.header()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, vendorError("deepgram", res, d.key)
	}
	var body struct {
		STT []struct {
			CanonicalName string   `json:"canonical_name"`
			Languages     []string `json:"languages"`
		} `json:"stt"`
		TTS []struct {
			CanonicalName string   `json:"canonical_name"`
			Languages     []string `json:"languages"`
			Metadata      struct {
				DisplayName string `json:"display_name"`
				Accent      string `json:"accent"`
			} `json:"metadata"`
		} `json:"tts"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("deepgram: %w", err)
	}
	seen := map[string]bool{}
	out := []ModelInfo{{ID: "flux-general-en", Name: "Flux (English, judges the end of a turn)", Kind: KindTranscription}, {ID: "flux-general-multi", Name: "Flux (multilingual, judges the end of a turn)", Kind: KindTranscription}}
	for _, m := range body.STT {
		if m.CanonicalName == "" || seen[m.CanonicalName] {
			continue
		}
		seen[m.CanonicalName] = true
		out = append(out, ModelInfo{ID: m.CanonicalName, Kind: KindTranscription})
	}
	for _, m := range body.TTS {
		if m.CanonicalName == "" || seen[m.CanonicalName] {
			continue
		}
		seen[m.CanonicalName] = true
		name := strings.TrimSpace(m.Metadata.DisplayName + " " + strings.Join(m.Languages, " "))
		if m.Metadata.Accent != "" {
			name += " (" + m.Metadata.Accent + ")"
		}
		out = append(out, ModelInfo{ID: m.CanonicalName, Name: name, Kind: KindSpeech})
	}
	sortModels(out)
	return out, nil
}

// closeErr is a socket's end as an error: a normal close is none.
func closeErr(err error) error {
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
		return nil
	}
	return err
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
