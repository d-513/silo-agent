package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func setVoice(t *testing.T, h *apptest.H, model string) error {
	t.Helper()
	_, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{"transcribe_model": model}}))
	return err
}

// voiceHarness writes silo.yaml to disk so PutSettings can patch it.
func voiceHarness(t *testing.T) *apptest.H {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/silo.yaml"
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	return apptest.New(t, apptest.WithConfigPath(path))
}

func voiceEnabled(t *testing.T, h *apptest.H, botID string) bool {
	t.Helper()
	res, err := h.Client.ListModels(h.Ctx(), connect.NewRequest(&v1.ListModelsRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetVoiceEnabled()
}

func TestDictationTranscribe(t *testing.T) {
	dummy.Reset()
	h := voiceHarness(t)
	bot := h.CreateBot("Scribe")
	id := bot.GetId()

	if !voiceEnabled(t, h, id) {
		t.Fatal("voice should be on with a transcribing provider")
	}
	// No worker: dictation must not need the box.
	res, err := h.Client.Transcribe(h.Ctx(), connect.NewRequest(&v1.TranscribeRequest{
		BotId: id, Audio: []byte("  hello from the mic "), Mime: "audio/webm;codecs=opus",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.GetText() != "hello from the mic" {
		t.Fatalf("text %q", res.Msg.GetText())
	}
	if _, err := h.NewClient().Transcribe(h.Ctx(), connect.NewRequest(&v1.TranscribeRequest{BotId: id, Audio: []byte("x")})); err == nil {
		t.Fatal("an anonymous caller must be refused")
	}
	if _, err := h.Client.Transcribe(h.Ctx(), connect.NewRequest(&v1.TranscribeRequest{BotId: id})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty audio: %v", err)
	}
	if _, err := h.Client.Transcribe(h.Ctx(), connect.NewRequest(&v1.TranscribeRequest{BotId: id, Audio: make([]byte, llm.MaxAudioBytes+1)})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("oversize audio: %v", err)
	}

	if err := setVoice(t, h, "anthropic/claude-sonnet-5"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("a provider that cannot transcribe must be rejected")
	}
	if err := setVoice(t, h, "off"); err != nil {
		t.Fatal(err)
	}
	if voiceEnabled(t, h, id) {
		t.Fatal("off should hide the mic")
	}
	if _, err := h.Client.Transcribe(h.Ctx(), connect.NewRequest(&v1.TranscribeRequest{BotId: id, Audio: []byte("x")})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("voice off: %v", err)
	}
}

func TestVoiceNeedsProviderConfig(t *testing.T) {
	dummy.Reset()
	h := voiceHarness(t)
	bot := h.CreateBot("Keyless")
	// Local needs a base URL but no key; without the URL voice stays off.
	if err := setVoice(t, h, "local/whisper-1"); err != nil {
		t.Fatal(err)
	}
	if voiceEnabled(t, h, bot.GetId()) {
		t.Fatal("local without base_url cannot transcribe")
	}
	if _, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{"providers.local.base_url": "http://127.0.0.1:9/v1"}})); err != nil {
		t.Fatal(err)
	}
	if !voiceEnabled(t, h, bot.GetId()) {
		t.Fatal("local with a base_url and no key should enable voice")
	}
}

func TestTranscribeToolSharedPath(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_91",
		memCall("transcribe", `{"path":"memo.mp3"}`),
		memCall("transcribe", `{"path":"notes.txt"}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Listener")
	id := bot.GetId()
	w := h.StartWorker(id)
	if err := os.WriteFile(filepath.Join(w.Workspace, "memo.mp3"), []byte("buy oat milk"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(w.Workspace, "notes.txt"), []byte("not audio"), 0o644)

	run, _ := h.Send(id, h.FirstChat(id), "Test_91_Input")
	res := memResults(h.WaitRun(run))
	if len(res) != 2 || res[0] != "buy oat milk" || !strings.Contains(res[1], "not an audio file") {
		t.Fatalf("tool results %q\n%s", res, h.RunBody(run))
	}

	out := pyCall(t, h, id, "", "bot", "transcribe", `{"path":"/workspace/memo.mp3","language":"en"}`)
	var got struct {
		Text     string `json:"text"`
		Language string `json:"language"`
	}
	if out.GetError() != "" || json.Unmarshal([]byte(out.GetResultJson()), &got) != nil || got.Text != "buy oat milk" || got.Language != "en" {
		t.Fatalf("python transcribe should be structured: %+v", out)
	}

	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "bot", Action: "transcribe", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if out := pyCall(t, h, id, "", "bot", "transcribe", `{"path":"memo.mp3"}`); out.GetError() != "denied" {
		t.Fatalf("deny rule: %+v", out)
	}
}
