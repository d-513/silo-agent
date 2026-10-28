package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// MaxSpeechChars caps one speech request; the OpenAI shape takes 4096.
const MaxSpeechChars = 4096

// SpeechSampleRate is the rate of PCM speech when the upstream does not say:
// the OpenAI shape (OpenAI, OpenRouter, Kokoro-FastAPI) is 24 kHz mono.
const SpeechSampleRate = 24000

// SpeechRequest is one piece of text to speak.
type SpeechRequest struct {
	Text string
	// Voice is the provider's own voice id; empty lets the provider choose,
	// which some refuse.
	Voice string
	// Language is an ISO-639-1 hint for providers that take one.
	Language string
	// Speed is a multiplier; 0 means the provider's default.
	Speed float64
	// Instructions describe delivery (tone, pace) where the model takes them.
	Instructions string
	// Format asks for "pcm" (16-bit little-endian mono) or "mp3". The answer's
	// Format says what came, since some models only give one.
	Format string
}

// Speech is a spoken answer as it streams in. The caller must close Body.
type Speech struct {
	Body io.ReadCloser
	// Format is "pcm" (16-bit LE mono at SampleRate), "mp3" or "wav".
	Format     string
	SampleRate int
}

// Speaker is the optional interface a provider implements when it can turn
// text into speech: the OpenAI /audio/speech shape that OpenRouter, LocalAI,
// Speaches, Kokoro-FastAPI and vLLM-omni speak.
type Speaker interface {
	Speak(ctx context.Context, model string, req SpeechRequest) (Speech, error)
}

// Speak posts one text to /audio/speech and hands back the body as it
// streams, so playback can start before the provider has finished.
func (c *openAICompatClient) Speak(ctx context.Context, model string, req SpeechRequest) (Speech, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Speech{}, errors.New("nothing to say")
	}
	if n := len([]rune(text)); n > MaxSpeechChars {
		return Speech{}, fmt.Errorf("text is %d characters; the cap is %d", n, MaxSpeechChars)
	}
	format := req.Format
	if format == "" {
		format = "pcm"
	}
	body := map[string]any{"model": model, "input": text, "response_format": format}
	if v := strings.TrimSpace(req.Voice); v != "" {
		body["voice"] = v
	}
	if req.Speed > 0 && req.Speed != 1 {
		body["speed"] = req.Speed
	}
	if s := strings.TrimSpace(req.Instructions); s != "" {
		body["instructions"] = s
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Speech{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.base, "/")+"/audio/speech", bytes.NewReader(raw))
	if err != nil {
		return Speech{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if c.key != "" && c.key != "none" {
		hreq.Header.Set("Authorization", "Bearer "+c.key)
	}
	for k, v := range c.headers {
		hreq.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return Speech{}, err
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		return Speech{}, speechError(res, c.key)
	}
	out := Speech{Body: res.Body, Format: format, SampleRate: SpeechSampleRate}
	mt, params, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	switch mt {
	case "audio/mpeg", "audio/mp3":
		out.Format = "mp3"
	case "audio/wav", "audio/wave", "audio/x-wav":
		out.Format = "wav"
	case "audio/pcm", "audio/l16":
		out.Format = "pcm"
	}
	if r, err := strconv.Atoi(params["rate"]); err == nil && r > 0 {
		out.SampleRate = r
	}
	return out, nil
}

// speechError words a refused speech request from its body, never echoing
// the key.
func speechError(res *http.Response, key string) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	msg := strings.TrimSpace(string(b))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		msg = e.Error.Message
	}
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "••••")
	}
	if msg == "" {
		return fmt.Errorf("speech: http %d", res.StatusCode)
	}
	return fmt.Errorf("speech: http %d: %s", res.StatusCode, msg)
}
