package voice

import "testing"

func TestAudioMIME(t *testing.T) {
	for name, want := range map[string]string{
		"a.MP3": "audio/mpeg", "b.wav": "audio/wav", "c.m4a": "audio/mp4", "d.opus": "audio/ogg",
		"e.webm": "audio/webm", "f.flac": "audio/flac", "g.txt": "", "noext": "",
	} {
		if got := audioMIME(name); got != want {
			t.Errorf("audioMIME(%q) = %q, want %q", name, got, want)
		}
	}
}
