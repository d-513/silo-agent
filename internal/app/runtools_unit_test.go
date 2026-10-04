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
	a.Models = models.New(nil, a.cfg)
	a.Voice = voice.New(nil, a.cfg, a.Models, nil)
	if has(a) {
		t.Fatal("no config: transcribe must not be offered")
	}
}
