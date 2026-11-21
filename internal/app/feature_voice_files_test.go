package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silo.agent/internal/apptest"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
	"silo.agent/internal/speech"
)

// Speech with nobody talking: speak writes a WAV into the workspace in the
// Bot's voice or a named one, a long text goes to the model in pieces and
// comes back as one file, and list_voices says what there is.
func TestSpeakToolWritesAFile(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+"speech_models: [dummy/cast]\n"))
	bot := h.CreateBot("Narrator")
	id := bot.GetId()
	w := h.StartWorker(id)
	speak := func(args string) (out struct {
		Path    string  `json:"path"`
		Seconds float64 `json:"seconds"`
		Rate    int     `json:"sample_rate"`
		Model   string  `json:"model"`
		Voice   string  `json:"voice"`
	}, errText string) {
		t.Helper()
		res := pyCall(t, h, id, "", "bot", "speak", args)
		if res.GetError() != "" {
			return out, res.GetError()
		}
		if err := json.Unmarshal([]byte(res.GetResultJson()), &out); err != nil {
			t.Fatalf("result %q: %v", res.GetResultJson(), err)
		}
		return out, ""
	}

	out, errText := speak(`{"text":"Welcome to the tour."}`)
	if errText != "" || !strings.HasPrefix(out.Path, "audio/welcome-to-the-tour-") || !strings.HasSuffix(out.Path, ".wav") || out.Model != "dummy/voice" {
		t.Fatalf("speak: %+v %s", out, errText)
	}
	raw, err := os.ReadFile(filepath.Join(w.Workspace, out.Path))
	if err != nil {
		t.Fatal(err)
	}
	pcm, rate, err := speech.ReadWAV(raw)
	want := len([]rune("Welcome to the tour.")) * dummy.SpeechMsPerRune
	if err != nil || rate != llm.SpeechSampleRate || len(pcm)/2*1000/rate != want || int(out.Seconds*1000+0.5) != want {
		t.Fatalf("the file: %d bytes of PCM at %d Hz, %v; answer %+v", len(pcm), rate, err, out)
	}

	// A path is kept, with the extension of what is written; a named model
	// speaks with a voice of its own for the language, or the one asked for.
	out, errText = speak(`{"text":"Dzień dobry.","path":"video/intro.mp3","model":"dummy/cast","language":"pl","speed":1.5}`)
	if errText != "" || out.Path != "video/intro.wav" || out.Model != "dummy/cast" || out.Voice != "jan" {
		t.Fatalf("speak with a model: %+v %s", out, errText)
	}
	out, errText = speak(`{"text":"Hello.","model":"dummy/cast","voice":"anna"}`)
	if errText != "" || out.Voice != "anna" {
		t.Fatalf("speak with a voice: %+v %s", out, errText)
	}
	said := dummy.Spoken()
	if last := said[len(said)-1]; last.Voice != "anna" || last.Format != "pcm" {
		t.Fatalf("the model was asked %+v", last)
	}
	if mid := said[len(said)-2]; mid.Speed != 1.5 || mid.Language != "pl" {
		t.Fatalf("the model was asked %+v", mid)
	}

	// A long script is several requests and one file.
	before := len(dummy.Spoken())
	long := strings.Repeat("This is one sentence of the script. ", 200)
	out, errText = speak(`{"text":` + quoted(long) + `,"path":"long.wav"}`)
	if errText != "" || len(dummy.Spoken())-before < 2 {
		t.Fatalf("a long text: %+v %s (%d requests)", out, errText, len(dummy.Spoken())-before)
	}
	spokenRunes := 0
	for _, r := range dummy.Spoken()[before:] {
		spokenRunes += len([]rune(r.Text))
	}
	if int(out.Seconds*1000+0.5) != spokenRunes*dummy.SpeechMsPerRune {
		t.Fatalf("the file is %.2fs for %d characters spoken", out.Seconds, spokenRunes)
	}

	for name, args := range map[string]string{
		"no text":           `{"text":"  "}`,
		"too long":          `{"text":` + quoted(strings.Repeat("a", 12001)) + `}`,
		"a model not there": `{"text":"x","model":"dummy/echo"}`,
		"an unknown voice":  `{"text":"x","model":"dummy/cast","voice":"zed"}`,
		"out of the box":    `{"text":"x","path":"../x.wav"}`,
		"the model's no":    `{"text":"Test_TTS_Error"}`,
	} {
		if _, errText := speak(args); errText == "" {
			t.Errorf("%s was accepted", name)
		}
	}

	voices := pyCall(t, h, id, "", "bot", "list_voices", `{"model":"dummy/cast"}`)
	if voices.GetError() != "" || !strings.Contains(voices.GetResultJson(), `"id":"jan"`) || !strings.Contains(voices.GetResultJson(), `"dummy/voice"`) {
		t.Fatalf("list_voices: %+v", voices)
	}
}

// transcribe answers timed segments when asked and writes them as subtitles;
// a model with no timings says so instead of writing an empty file.
func TestTranscribeWritesSubtitles(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Subtitler")
	id := bot.GetId()
	w := h.StartWorker(id)
	if err := os.WriteFile(filepath.Join(w.Workspace, "talk.mp3"), []byte("Hello there.\nThis is the second line."), 0o644); err != nil {
		t.Fatal(err)
	}
	res := pyCall(t, h, id, "", "bot", "transcribe", `{"path":"talk.mp3","timestamps":true}`)
	var got struct {
		Text     string        `json:"text"`
		Segments []llm.Segment `json:"segments"`
	}
	if res.GetError() != "" || json.Unmarshal([]byte(res.GetResultJson()), &got) != nil || len(got.Segments) != 2 || got.Segments[1].Start != 2 || got.Segments[1].Text != "This is the second line." {
		t.Fatalf("timed transcript: %+v", res)
	}
	// Without the ask the answer is as it always was.
	if plain := pyCall(t, h, id, "", "bot", "transcribe", `{"path":"talk.mp3"}`); strings.Contains(plain.GetResultJson(), "segments") {
		t.Fatalf("segments nobody asked for: %s", plain.GetResultJson())
	}

	if res := pyCall(t, h, id, "", "bot", "transcribe", `{"path":"talk.mp3","subtitles":"subs/talk.srt"}`); res.GetError() != "" {
		t.Fatalf("subtitles: %s", res.GetError())
	}
	srt, err := os.ReadFile(filepath.Join(w.Workspace, "subs/talk.srt"))
	if err != nil || string(srt) != "1\n00:00:00,000 --> 00:00:02,000\nHello there.\n\n2\n00:00:02,000 --> 00:00:04,000\nThis is the second line.\n\n" {
		t.Fatalf("srt %q %v", srt, err)
	}
	if res := pyCall(t, h, id, "", "bot", "transcribe", `{"path":"talk.mp3","subtitles":"talk.vtt"}`); res.GetError() != "" {
		t.Fatalf("vtt: %s", res.GetError())
	}
	vtt, _ := os.ReadFile(filepath.Join(w.Workspace, "talk.vtt"))
	if !strings.HasPrefix(string(vtt), "WEBVTT\n\n1\n00:00:00.000 --> 00:00:02.000\n") {
		t.Fatalf("vtt %q", vtt)
	}

	_ = os.WriteFile(filepath.Join(w.Workspace, "plain.mp3"), []byte("Words only. Test_No_Timings"), 0o644)
	res = pyCall(t, h, id, "", "bot", "transcribe", `{"path":"plain.mp3","subtitles":"plain.srt"}`)
	if !strings.Contains(res.GetError(), "gives no timings") {
		t.Fatalf("a model with no timings: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(w.Workspace, "plain.srt")); err == nil {
		t.Fatal("an empty subtitle file was written")
	}
	if res := pyCall(t, h, id, "", "bot", "transcribe", `{"path":"talk.mp3","subtitles":"talk.txt"}`); res.GetError() == "" {
		t.Fatal("subtitles in a format that is not one")
	}
}

// quoted is a string as a JSON string.
func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
