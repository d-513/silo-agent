package llm

// The speech vendors' adapters against fakes that speak each vendor's
// documented protocol (the shapes in their API references): what the
// adapter sends is checked, and what it does with each answer.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeWS serves one socket per connection with handle.
func fakeWS(t *testing.T, handle func(t *testing.T, r *http.Request, c *websocket.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(1 << 22)
		handle(t, r, c)
		c.CloseNow()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readJSON(t *testing.T, c *websocket.Conn) (map[string]any, []byte, bool) {
	typ, data, err := c.Read(context.Background())
	if err != nil {
		return nil, nil, false
	}
	if typ == websocket.MessageBinary {
		return nil, data, true
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m == nil {
		m = map[string]any{"_raw": string(data)}
	}
	return m, nil, true
}

func sendJSON(c *websocket.Conn, v any) {
	b, _ := json.Marshal(v)
	_ = c.Write(context.Background(), websocket.MessageText, b)
}

func newVendor(t *testing.T, id, base string) Client {
	t.Helper()
	c, err := New(id, Settings{"api_key": "sk-k", "base_url": base})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// liveTurn sends audio, collects the guesses, and finishes.
func runLive(t *testing.T, c Client, model string) (string, []TranscriptEvent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := c.(LiveTranscriber).OpenTranscription(ctx, model, LiveOptions{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var mu sync.Mutex
	var evs []TranscriptEvent
	go func() {
		for ev := range sess.Events() {
			mu.Lock()
			evs = append(evs, ev)
			mu.Unlock()
		}
	}()
	if err := sess.Send(make([]byte, 3200)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	text, err := sess.Finish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return text, append([]TranscriptEvent(nil), evs...)
}

func TestDeepgramNovaLive(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/listen" || r.Header.Get("Authorization") != "Token sk-k" || q.Get("model") != "nova-3" || q.Get("encoding") != "linear16" || q.Get("sample_rate") != "16000" || q.Get("interim_results") != "true" || q.Get("language") != "en" {
			t.Errorf("dial %s %v auth %q", r.URL.Path, q, r.Header.Get("Authorization"))
		}
		for {
			m, audio, ok := readJSON(t, c)
			if !ok {
				return
			}
			switch {
			case audio != nil:
				sendJSON(c, map[string]any{"type": "Results", "is_final": false, "channel": map[string]any{"alternatives": []any{map[string]any{"transcript": "hello"}}}})
				sendJSON(c, map[string]any{"type": "Results", "is_final": true, "channel": map[string]any{"alternatives": []any{map[string]any{"transcript": "Hello"}}}})
			case m["type"] == "Finalize":
				sendJSON(c, map[string]any{"type": "Results", "is_final": true, "from_finalize": true, "channel": map[string]any{"alternatives": []any{map[string]any{"transcript": "world."}}}})
			}
		}
	})
	text, evs := runLive(t, newVendor(t, Deepgram, srv.URL), "nova-3")
	if text != "Hello world." || len(evs) == 0 {
		t.Fatalf("%q, %d events", text, len(evs))
	}
}

func TestDeepgramFluxEndsTurns(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		if r.URL.Path != "/v2/listen" || r.URL.Query().Get("model") != "flux-general-en" {
			t.Errorf("dial %s", r.URL)
		}
		for {
			m, audio, ok := readJSON(t, c)
			if !ok {
				return
			}
			switch {
			case audio != nil:
				sendJSON(c, map[string]any{"type": "TurnInfo", "event": "Update", "transcript": "book a"})
				sendJSON(c, map[string]any{"type": "TurnInfo", "event": "EndOfTurn", "trigger": "model", "transcript": "Book a table."})
			case m["type"] == "ForceEndTurn":
				sendJSON(c, map[string]any{"type": "Warning", "code": "FORCE_END_TURN_NO_ACTIVE_TURN"})
			}
		}
	})
	text, evs := runLive(t, newVendor(t, Deepgram, srv.URL), "flux-general-en")
	eot := false
	for _, ev := range evs {
		eot = eot || ev.EndOfTurn
	}
	if text != "Book a table." || !eot {
		t.Fatalf("%q, end of turn %v", text, eot)
	}
	if c := newVendor(t, Deepgram, srv.URL); !CapsOf(c, "flux-general-en").EndOfTurn || CapsOf(c, "nova-3").EndOfTurn {
		t.Fatal("only Flux judges the end of a turn")
	}
}

func TestDeepgramVoice(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		if r.URL.Path != "/v1/speak" || r.URL.Query().Get("model") != "aura-2-thalia-en" || r.URL.Query().Get("encoding") != "linear16" {
			t.Errorf("dial %s", r.URL)
		}
		var text string
		for {
			m, _, ok := readJSON(t, c)
			if !ok {
				return
			}
			switch m["type"] {
			case "Speak":
				text = m["text"].(string)
			case "Flush":
				if text != "never" {
					_ = c.Write(context.Background(), websocket.MessageBinary, make([]byte, 960))
					sendJSON(c, map[string]any{"type": "Flushed", "sequence_id": 0})
				}
			case "Clear":
				sendJSON(c, map[string]any{"type": "Cleared"})
			}
		}
	})
	conn, err := newVendor(t, Deepgram, srv.URL).(LiveSpeaker).OpenSpeech(context.Background(), "aura-2-thalia-en", SpeechRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	got := 0
	if err := conn.Say(context.Background(), "Hello.", func(b []byte) { got += len(b) }, func([]SpeechWord) {}); err != nil || got != 960 {
		t.Fatalf("said %d bytes: %v", got, err)
	}
	// A piece cut off is cleared; the connection goes on.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := conn.Say(ctx, "never", func([]byte) {}, func([]SpeechWord) {}); err == nil {
		t.Fatal("a cut piece should answer its context's error")
	}
	if err := conn.Say(context.Background(), "Again.", func(b []byte) {}, func([]SpeechWord) {}); err != nil {
		t.Fatalf("after a cut: %v", err)
	}
}

func TestElevenLabsVoice(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		if r.URL.Path != "/v1/text-to-speech/v1/multi-stream-input" || r.Header.Get("xi-api-key") != "sk-k" || r.URL.Query().Get("output_format") != "pcm_24000" || r.URL.Query().Get("model_id") != "eleven_flash_v2_5" {
			t.Errorf("dial %s %v", r.URL, r.Header)
		}
		for {
			m, _, ok := readJSON(t, c)
			if !ok {
				return
			}
			id, _ := m["context_id"].(string)
			if m["flush"] == true {
				// "Hi you" — characters with times from this chunk's start.
				sendJSON(c, map[string]any{"audio": base64.StdEncoding.EncodeToString(make([]byte, 4800)), "contextId": id, "alignment": map[string]any{
					"chars": []string{"H", "i", " ", "y", "o", "u"}, "charStartTimesMs": []int{0, 50, 100, 150, 200, 250}, "charDurationsMs": []int{50, 50, 50, 50, 50, 50}}})
			}
			if m["close_context"] == true {
				sendJSON(c, map[string]any{"isFinal": true, "contextId": id})
			}
		}
	})
	conn, err := newVendor(t, ElevenLabs, srv.URL).(LiveSpeaker).OpenSpeech(context.Background(), "eleven_flash_v2_5", SpeechRequest{Voice: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var words []SpeechWord
	bytes := 0
	if err := conn.Say(context.Background(), "Hi you", func(b []byte) { bytes += len(b) }, func(w []SpeechWord) { words = append(words, w...) }); err != nil {
		t.Fatal(err)
	}
	if bytes != 4800 || len(words) != 2 || words[0].Text != "Hi" || words[1].EndMs != 300 {
		t.Fatalf("%d bytes, words %+v", bytes, words)
	}
}

func TestCartesiaVoice(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		if r.URL.Path != "/tts/websocket" || r.URL.Query().Get("cartesia_version") != cartesiaVersion || r.Header.Get("X-API-Key") != "sk-k" {
			t.Errorf("dial %s", r.URL)
		}
		for {
			m, _, ok := readJSON(t, c)
			if !ok {
				return
			}
			id, _ := m["context_id"].(string)
			if m["transcript"] == nil || m["continue"] != false || m["add_timestamps"] != true || m["voice"].(map[string]any)["id"] != "v1" {
				t.Errorf("request %v", m)
			}
			sendJSON(c, map[string]any{"type": "chunk", "data": base64.StdEncoding.EncodeToString(make([]byte, 480)), "done": false, "context_id": id})
			sendJSON(c, map[string]any{"type": "timestamps", "context_id": id, "word_timestamps": map[string]any{"words": []string{"Hi"}, "start": []float64{0}, "end": []float64{0.25}}})
			sendJSON(c, map[string]any{"type": "done", "done": true, "context_id": id})
		}
	})
	conn, err := newVendor(t, Cartesia, srv.URL).(LiveSpeaker).OpenSpeech(context.Background(), "sonic-3.6", SpeechRequest{Voice: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var words []SpeechWord
	if err := conn.Say(context.Background(), "Hi.", func([]byte) {}, func(w []SpeechWord) { words = append(words, w...) }); err != nil {
		t.Fatal(err)
	}
	if len(words) != 1 || words[0].EndMs != 250 {
		t.Fatalf("words %+v", words)
	}
}

func TestInworldVoice(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		if r.URL.Path != "/tts/v1/voice:streamBidirectional" || r.Header.Get("Authorization") != "Basic sk-k" {
			t.Errorf("dial %s %q", r.URL, r.Header.Get("Authorization"))
		}
		for {
			m, _, ok := readJSON(t, c)
			if !ok {
				return
			}
			id, _ := m["contextId"].(string)
			if m["sendText"] != nil {
				sendJSON(c, map[string]any{"result": map[string]any{"contextId": id, "status": map[string]any{"code": 0}, "audioChunk": map[string]any{"audioContent": base64.StdEncoding.EncodeToString(make([]byte, 480)),
					"timestampInfo": map[string]any{"wordAlignment": map[string]any{"words": []string{"Hi"}, "wordStartTimeSeconds": []float64{0}, "wordEndTimeSeconds": []float64{0.2}}}}}})
			}
			if m["closeContext"] != nil {
				sendJSON(c, map[string]any{"result": map[string]any{"contextId": id, "contextClosed": map[string]any{}, "status": map[string]any{"code": 0}}})
			}
		}
	})
	conn, err := newVendor(t, Inworld, srv.URL).(LiveSpeaker).OpenSpeech(context.Background(), "inworld-tts-2-flash", SpeechRequest{Voice: "Ashley"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var words []SpeechWord
	if err := conn.Say(context.Background(), "Hi.", func([]byte) {}, func(w []SpeechWord) { words = append(words, w...) }); err != nil || len(words) != 1 || words[0].EndMs != 200 {
		t.Fatalf("words %+v: %v", words, err)
	}
}

func TestInworldSpeakReadsLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tts/v1/voice:stream" {
			t.Errorf("path %s", r.URL.Path)
		}
		for i := 0; i < 2; i++ {
			b, _ := json.Marshal(map[string]any{"result": map[string]any{"audioContent": base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})}})
			w.Write(append(b, '\n'))
		}
	}))
	defer srv.Close()
	sp, err := newVendor(t, Inworld, srv.URL).(Speaker).Speak(context.Background(), "inworld-tts-2", SpeechRequest{Text: "Hi", Voice: "Ashley"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(sp.Body)
	if len(b) != 8 || sp.Format != "pcm" {
		t.Fatalf("%d bytes %s", len(b), sp.Format)
	}
}

func TestSonioxLive(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		if r.Header.Get("Authorization") != "Bearer sk-k" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		cfg, _, _ := readJSON(t, c)
		if cfg["model"] != "stt-rt-v5" || cfg["audio_format"] != "pcm_s16le" || cfg["sample_rate"] != float64(16000) {
			t.Errorf("config %v", cfg)
		}
		for {
			m, audio, ok := readJSON(t, c)
			if !ok {
				return
			}
			switch {
			case audio != nil:
				sendJSON(c, map[string]any{"tokens": []any{map[string]any{"text": "Dzień", "is_final": true}, map[string]any{"text": " do", "is_final": false}}})
				sendJSON(c, map[string]any{"tokens": []any{map[string]any{"text": " dobry", "is_final": true}, map[string]any{"text": "<end>", "is_final": true}}})
			case m["type"] == "finalize":
				sendJSON(c, map[string]any{"tokens": []any{map[string]any{"text": ".", "is_final": true}, map[string]any{"text": "<fin>", "is_final": true}}})
			}
		}
	})
	text, evs := runLive(t, newVendor(t, Soniox, srv.URL), "stt-rt-v5")
	eot := false
	for _, ev := range evs {
		eot = eot || ev.EndOfTurn
	}
	if text != "Dzień dobry." || !eot {
		t.Fatalf("%q, end of turn %v", text, eot)
	}
}

func TestAssemblyAILive(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		if r.URL.Path != "/v3/ws" || r.Header.Get("Authorization") != "sk-k" || r.URL.Query().Get("speech_model") != "universal-3-6-pro" {
			t.Errorf("dial %s", r.URL)
		}
		for {
			m, audio, ok := readJSON(t, c)
			if !ok {
				return
			}
			switch {
			case audio != nil:
				sendJSON(c, map[string]any{"type": "Turn", "turn_order": 0, "end_of_turn": false, "transcript": "my name"})
			case m["type"] == "ForceEndpoint":
				sendJSON(c, map[string]any{"type": "Turn", "turn_order": 0, "end_of_turn": true, "turn_is_formatted": false, "transcript": "my name is sonny"})
				sendJSON(c, map[string]any{"type": "Turn", "turn_order": 0, "end_of_turn": true, "turn_is_formatted": true, "transcript": "My name is Sonny."})
			}
		}
	})
	text, _ := runLive(t, newVendor(t, AssemblyAI, srv.URL), "universal-3-6-pro")
	if text != "My name is Sonny." {
		t.Fatalf("%q", text)
	}
}

func TestScribeRealtime(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/speech-to-text/realtime" || q.Get("commit_strategy") != "manual" || q.Get("audio_format") != "pcm_16000" {
			t.Errorf("dial %s", r.URL)
		}
		for {
			m, _, ok := readJSON(t, c)
			if !ok {
				return
			}
			if m["message_type"] != "input_audio_chunk" {
				t.Errorf("message %v", m)
				continue
			}
			if m["commit"] == true {
				sendJSON(c, map[string]any{"message_type": "committed_transcript", "text": "Hello there."})
				continue
			}
			sendJSON(c, map[string]any{"message_type": "partial_transcript", "text": "hello"})
		}
	})
	text, evs := runLive(t, newVendor(t, ElevenLabs, srv.URL), "scribe_v2_realtime")
	if text != "Hello there." || len(evs) == 0 {
		t.Fatalf("%q %d", text, len(evs))
	}
}

func TestOpenAIRealtime(t *testing.T) {
	srv := fakeWS(t, func(t *testing.T, r *http.Request, c *websocket.Conn) {
		q := r.URL.Query()
		if r.URL.Path != "/realtime" || q.Get("intent") != "transcription" || q.Get("model") != "whisper-large-v3" {
			t.Errorf("dial %s", r.URL)
		}
		up, _, _ := readJSON(t, c)
		s, _ := up["session"].(map[string]any)
		if up["type"] != "session.update" || s["type"] != "transcription" {
			t.Errorf("update %v", up)
		}
		for {
			m, _, ok := readJSON(t, c)
			if !ok {
				return
			}
			switch m["type"] {
			case "input_audio_buffer.append":
				b, _ := base64.StdEncoding.DecodeString(m["audio"].(string))
				// 16 kHz in, 24 kHz to the server.
				if len(b) != 4800 {
					t.Errorf("appended %d bytes", len(b))
				}
			case "input_audio_buffer.commit":
				sendJSON(c, map[string]any{"type": "input_audio_buffer.committed", "item_id": "i1"})
				sendJSON(c, map[string]any{"type": "conversation.item.input_audio_transcription.delta", "item_id": "i1", "delta": "Hel"})
				sendJSON(c, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "i1", "transcript": "Hello."})
			}
		}
	})
	c, err := New(Local, Settings{"base_url": srv.URL, "realtime": "openai"})
	if err != nil {
		t.Fatal(err)
	}
	if !CapsOf(c, "whisper-large-v3").Live || CapsOf(c, "qwen3-8b").Live {
		t.Fatal("a realtime server streams its transcription models only")
	}
	text, _ := runLive(t, c, "whisper-large-v3")
	if text != "Hello." {
		t.Fatalf("%q", text)
	}
	plain, _ := New(Local, Settings{"base_url": srv.URL})
	if CapsOf(plain, "whisper-large-v3").Live {
		t.Fatal("a server that does not say it speaks Realtime is not live")
	}
	or, _ := New(OpenRouter, Settings{"api_key": "k"})
	if CapsOf(or, "openai/whisper-1").Live {
		t.Fatal("OpenRouter has no live transcription")
	}
}

func TestAssemblyAISync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/transcribe" || r.Header.Get("X-AAI-Model") != "universal-3-5-pro" || r.Header.Get("Authorization") != "sk-k" {
			t.Errorf("request %s %v", r.URL.Path, r.Header)
		}
		f, _, err := r.FormFile("audio")
		if err != nil {
			t.Errorf("no audio part: %v", err)
		} else {
			f.Close()
		}
		w.Write([]byte(`{"text":"Hello file."}`))
	}))
	defer srv.Close()
	tr, err := newVendor(t, AssemblyAI, srv.URL).(Transcriber).Transcribe(context.Background(), "universal-streaming-english", Audio{Data: []byte("RIFF"), MIME: "audio/wav"})
	if err != nil || tr.Text != "Hello file." {
		t.Fatalf("%q %v", tr.Text, err)
	}
}

func TestVendorErrorsMaskTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":{"message":"invalid key sk-k"}}`))
	}))
	defer srv.Close()
	_, err := newVendor(t, ElevenLabs, srv.URL).(Speaker).Speak(context.Background(), "eleven_flash_v2_5", SpeechRequest{Text: "hi", Voice: "v"})
	if err == nil || strings.Contains(err.Error(), "sk-k") || !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("%v", err)
	}
}
