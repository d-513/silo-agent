package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatTranscribe(t *testing.T) {
	var fields map[string]string
	var fileName, fileBody, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		fields = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			fields[k] = v[0]
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Errorf("file: %v", err)
		} else {
			fileName = h.Filename
			b, _ := io.ReadAll(f)
			fileBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":  " hello there ",
			"usage": map[string]any{"type": "duration", "seconds": 2.5, "cost": 0.001},
		})
	}))
	defer srv.Close()
	c, err := New(Local, Settings{"base_url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.(Transcriber)
	if !ok {
		t.Fatal("local client is not a Transcriber")
	}
	out, err := tr.Transcribe(context.Background(), "whisper-1", Audio{Data: []byte("RIFF"), MIME: "audio/webm;codecs=opus", Language: "pl"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "hello there" || out.Seconds != 2.5 || out.Cost != 0.001 {
		t.Fatalf("transcript %+v", out)
	}
	if fields["model"] != "whisper-1" || fields["language"] != "pl" || fields["response_format"] != "json" {
		t.Fatalf("fields %v", fields)
	}
	if fileName != "audio.webm" || fileBody != "RIFF" {
		t.Fatalf("file %q %q", fileName, fileBody)
	}
	if auth != "Bearer none" {
		t.Fatalf("keyless local should send a placeholder bearer, got %q", auth)
	}
}

func TestTranscribeRejectsEmptyAndOversize(t *testing.T) {
	c, _ := New(Local, Settings{"base_url": "http://127.0.0.1:1"})
	tr := c.(Transcriber)
	if _, err := tr.Transcribe(context.Background(), "m", Audio{}); err == nil {
		t.Fatal("empty audio must fail")
	}
	if _, err := tr.Transcribe(context.Background(), "m", Audio{Data: make([]byte, MaxAudioBytes+1)}); err == nil {
		t.Fatal("oversize audio must fail before any request")
	}
}

func TestLocalProviderSettings(t *testing.T) {
	if _, err := New(Local, Settings{}); err == nil {
		t.Fatal("local without base_url must fail")
	}
	d, _ := Lookup(Local)
	if !d.KeyOptional || !d.BaseURLRequired {
		t.Fatalf("descriptor %+v", d)
	}
	if _, err := New(OpenAI, Settings{}); err == nil {
		t.Fatal("openai still needs a key")
	}
	a, err := New(Anthropic, Settings{"api_key": "k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(Transcriber); ok {
		t.Fatal("anthropic cannot transcribe")
	}
}

func TestAudioExt(t *testing.T) {
	for mime, want := range map[string]string{
		"audio/webm;codecs=opus": ".webm",
		"audio/mp4":              ".m4a",
		"AUDIO/OGG; codecs=opus": ".ogg",
		"audio/mpeg":             ".mp3",
		"audio/x-wav":            ".wav",
		"text/plain":             "",
	} {
		if got := AudioExt(mime); got != want {
			t.Errorf("AudioExt(%q) = %q, want %q", mime, got, want)
		}
	}
}
