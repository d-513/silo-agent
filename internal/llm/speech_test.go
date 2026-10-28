package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Speak posts the OpenAI shape, streams the body back, and reads the format
// and rate from the answer's Content-Type.
func TestSpeak(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" || r.Header.Get("Authorization") != "Bearer sk-k" {
			t.Errorf("request %s auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "audio/pcm;rate=16000")
		_, _ = w.Write([]byte{1, 2, 3, 4})
	}))
	defer srv.Close()
	c, _ := New(OpenRouter, Settings{"api_key": "sk-k", "base_url": srv.URL})
	sp, err := c.(Speaker).Speak(context.Background(), "hexgrad/kokoro-82m", SpeechRequest{Text: " Hello. ", Voice: "af_heart", Speed: 1.2})
	if err != nil {
		t.Fatal(err)
	}
	defer sp.Body.Close()
	b, _ := io.ReadAll(sp.Body)
	if len(b) != 4 || sp.Format != "pcm" || sp.SampleRate != 16000 {
		t.Fatalf("got %d bytes, %s at %d", len(b), sp.Format, sp.SampleRate)
	}
	if got["input"] != "Hello." || got["voice"] != "af_heart" || got["response_format"] != "pcm" || got["speed"] != 1.2 || got["model"] != "hexgrad/kokoro-82m" {
		t.Fatalf("body %v", got)
	}
}

// A refusal carries the upstream's reason with the key masked; an mp3 answer
// is reported as mp3 whatever was asked.
func TestSpeakErrorsAndFormats(t *testing.T) {
	status := http.StatusBadRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"bad voice for sk-k"}}`))
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3"))
	}))
	defer srv.Close()
	c, _ := New(Local, Settings{"api_key": "sk-k", "base_url": srv.URL})
	_, err := c.(Speaker).Speak(context.Background(), "tts-1", SpeechRequest{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "bad voice") || strings.Contains(err.Error(), "sk-k") {
		t.Fatalf("err %v", err)
	}
	status = http.StatusOK
	sp, err := c.(Speaker).Speak(context.Background(), "tts-1", SpeechRequest{Text: "hi"})
	if err != nil || sp.Format != "mp3" {
		t.Fatalf("format %q err %v", sp.Format, err)
	}
	sp.Body.Close()
	if _, err := c.(Speaker).Speak(context.Background(), "tts-1", SpeechRequest{Text: "  "}); err == nil {
		t.Fatal("empty text should be refused")
	}
}

func TestGuessKind(t *testing.T) {
	for id, want := range map[string]string{
		"whisper-1": KindTranscription, "gpt-4o-mini-transcribe": KindTranscription, "nvidia/parakeet-tdt-0.6b-v3": KindTranscription,
		"tts-1-hd": KindSpeech, "kokoro": KindSpeech, "gpt-4o-mini-tts": KindSpeech,
		"text-embedding-3-small": KindEmbedding, "gpt-5.6-luna": KindText, "qwen3-8b": KindText,
	} {
		if got := GuessKind(id); got != want {
			t.Errorf("%s: %s, want %s", id, got, want)
		}
	}
}
