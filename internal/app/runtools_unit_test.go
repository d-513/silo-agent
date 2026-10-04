package app

import (
	"testing"

	"silo.agent/internal/app/models"
	"silo.agent/internal/app/voice"
)

func TestRunToolsDropsTranscribeWhenVoiceOff(t *testing.T) {
	has := func(a *App) bool {
		for _, tl := range a.runTools() {
			if tl.Name == "transcribe" {
				return true
			}
		}
		return false
	}
	a := &App{}
	a.models = models.New(nil, a.cfg)
	a.voice = voice.New(nil, a.cfg, a.models, nil)
	if has(a) {
		t.Fatal("no config: transcribe must not be offered")
	}
}
