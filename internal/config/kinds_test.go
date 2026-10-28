package config

import (
	"strings"
	"testing"

	"silo.agent/internal/llm"
)

// Every kind has its own allowlist, and the model a role names counts as
// allowed even when the list is older than the role.
func TestKindModels(t *testing.T) {
	s, err := FromYAML([]byte(`transcription_models: [openrouter/openai/gpt-4o-transcribe]
speech_models: [openrouter/hexgrad/kokoro-82m, openrouter/elevenlabs/eleven-flash-v2.5]
transcribe_model: openrouter/openai/whisper-1
voice:
  speech_model: openrouter/elevenlabs/eleven-flash-v2.5
`))
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if got := c.KindModels(llm.KindTranscription); strings.Join(got, ",") != "openrouter/openai/gpt-4o-transcribe,openrouter/openai/whisper-1" {
		t.Fatalf("transcription models %q", got)
	}
	if got := c.KindModels(llm.KindSpeech); len(got) != 2 {
		t.Fatalf("a listed role is not added twice: %q", got)
	}
	// The default embedding model is allowed with no list at all.
	if got := c.KindModels(llm.KindEmbedding); len(got) != 1 || got[0] != DefaultEmbeddingModel {
		t.Fatalf("embedding models %q", got)
	}
	for id, want := range map[string]string{
		"openrouter/openai/gpt-4o-transcribe": llm.KindTranscription,
		"openrouter/openai/whisper-1":         llm.KindTranscription,
		"openrouter/hexgrad/kokoro-82m":       llm.KindSpeech,
		DefaultEmbeddingModel:                 llm.KindEmbedding,
		"openrouter/openai/gpt-5.6-luna":      llm.KindText,
	} {
		if got := c.ModelKind(id); got != want {
			t.Errorf("kind of %s = %s, want %s", id, got, want)
		}
	}
	if c.SpeechModel() != "openrouter/elevenlabs/eleven-flash-v2.5" || c.VoiceMode() != VoiceModeDirect {
		t.Fatalf("speech model %q, mode %q (no talker model means direct)", c.SpeechModel(), c.VoiceMode())
	}
	c.ModelVoice = "openrouter/openai/gpt-5.6-luna"
	if c.VoiceMode() != VoiceModeTalker {
		t.Fatal("a talker model makes the call talker mode")
	}
	c.Voice.Enabled = false
	if c.SpeechModel() != "" {
		t.Fatal("voice off has no speech model")
	}
}

func TestValidateKindModels(t *testing.T) {
	const kokoro = "openrouter/hexgrad/kokoro-82m"
	for name, tc := range map[string]struct{ yaml, want string }{
		"anthropic cannot speak":   {"speech_models: [anthropic/claude-opus-5-5]\n", "no speech models"},
		"on two lists":             {"speech_models: [" + kokoro + "]\nmodels: [" + kokoro + "]\n", "both"},
		"speech model as the chat": {"speech_models: [" + kokoro + "]\nmodel: " + kokoro + "\n", "a speech model, not a text model"},
		"speech role elsewhere":    {"transcription_models: [" + kokoro + "]\nvoice:\n  speech_model: " + kokoro + "\n", "transcription_models"},
		"talker is a speaker":      {"speech_models: [" + kokoro + "]\nmodel_voice: " + kokoro + "\n", "model_voice"},
		"bad mode":                 {"voice:\n  mode: shout\n", "voice.mode"},
		"typesafe transcribes":     {"transcribe_model: typesafe/jev-latest\n", "transcribe_model"},
	} {
		if err := validateYAML([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error with %q", name, err, tc.want)
		}
	}
	if err := validateYAML([]byte("speech_models: [" + kokoro + "]\nvoice:\n  speech_model: " + kokoro + "\ntranscribe_model: off\n")); err != nil {
		t.Fatalf("a valid voice config: %v", err)
	}
}
