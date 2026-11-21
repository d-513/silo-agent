package voice

// Speech as files, with nobody talking: the Bot's speak tool writes text
// read aloud into its workspace (narration for a video, a voice-over), and
// its transcribe tool can answer with timings and write them as subtitles.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
	"unicode"

	"silo.agent/internal/app/workspace"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/speech"
)

const (
	// speakMax is the most text one speak call takes: about a quarter of an
	// hour of audio, which as a WAV still fits what the workspace takes.
	speakMax = 12000
	// speakPiece is how much goes to the model in one request.
	speakPiece = 3000
	// speakFileTimeout bounds a whole speak call.
	speakFileTimeout = 5 * time.Minute
	// audioDir is where a recording goes when the Bot names no path.
	audioDir = "audio"
)

// SpeakTool is the Bot's speak (silo_runtime.speak): text to a WAV file in
// the workspace, in the Bot's voice unless the call names another. The caller
// already authorized bot.speak.
func (s *Service) SpeakTool(ctx context.Context, b *db.Bot, args map[string]any, structured bool) (string, error) {
	text, _ := args["text"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("text is empty: pass what to say")
	}
	if n := len([]rune(text)); n > speakMax {
		return "", fmt.Errorf("text is %d characters; one call takes %d. Speak it in parts and join the files with ffmpeg", n, speakMax)
	}
	snd := s.SoundOf(ctx, b)
	if snd.Model == "" {
		return "", errNoSpeech
	}
	cfg := s.cfg()
	if m, _ := args["model"].(string); strings.TrimSpace(m) != "" && strings.TrimSpace(m) != snd.Model {
		m = strings.TrimSpace(m)
		allowed := cfg.KindModels(llm.KindSpeech)
		if !llm.Allowed(m, allowed) {
			return "", fmt.Errorf("model %q is not an allowed speech model; allowed: %s", m, strings.Join(allowed, ", "))
		}
		// A voice means nothing to another model: its own default is picked.
		voices, _ := s.voices(ctx, m)
		snd.Model, snd.Voice = m, speech.PickVoice(voices, snd.Language)
	}
	if l, _ := args["language"].(string); strings.TrimSpace(l) != "" {
		snd.Language = strings.TrimSpace(l)
		if _, named := args["voice"]; !named {
			if voices, _ := s.voices(ctx, snd.Model); len(voices) > 0 && !voiceSpeaks(voices, snd.Voice, snd.Language) {
				snd.Voice = speech.PickVoice(voices, snd.Language)
			}
		}
	}
	if v, _ := args["voice"].(string); strings.TrimSpace(v) != "" {
		v = strings.TrimSpace(v)
		if voices, _ := s.voices(ctx, snd.Model); len(voices) > 0 && !hasVoice(voices, v) {
			return "", fmt.Errorf("%s has no voice %q; silo_runtime.list_voices() lists them", snd.Model, v)
		}
		snd.Voice = v
	}
	if sp, ok := args["speed"].(float64); ok && sp > 0 {
		snd.Speed = min(max(sp, 0.5), 2)
	}
	dest, _ := args["path"].(string)
	dest, err := wavPath(dest, text)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeoutCause(ctx, speakFileTimeout, fmt.Errorf("no answer in %s", speakFileTimeout))
	defer cancel()
	var pcm []byte
	rate := 0
	for _, piece := range speakPieces(text, speakPiece) {
		out, err := s.Speak(ctx, b.ID, snd, piece, "pcm")
		if err != nil {
			return "", fmt.Errorf("speak: %w", err)
		}
		part, r, err := readSpeech(out)
		if err != nil {
			return "", fmt.Errorf("speak: %w", err)
		}
		if rate == 0 {
			rate = r
		}
		pcm = append(pcm, speech.Resample(part, r, rate)...)
		if len(pcm) > workspace.PutMax-64 {
			return "", fmt.Errorf("the recording is over %d MB; speak it in parts", workspace.PutMax>>20)
		}
	}
	if len(pcm) == 0 || rate == 0 {
		return "", errors.New("speak: the model answered with no audio")
	}
	wav := speech.WAV(pcm, rate)
	if err := s.ws.Put(ctx, b.ID, dest, wav); err != nil {
		return "", fmt.Errorf("save %s: %w", dest, err)
	}
	seconds := float64(len(pcm)) / 2 / float64(rate)
	if !structured {
		return fmt.Sprintf("Saved %s (%.1f s, WAV %d Hz mono) spoken by %s, voice %q. Convert it with ffmpeg if another format is needed.", dest, seconds, rate, snd.Model, snd.Voice), nil
	}
	body, err := json.Marshal(map[string]any{"path": dest, "seconds": seconds, "bytes": len(wav), "sample_rate": rate, "model": snd.Model, "voice": snd.Voice})
	return string(body), err
}

// VoicesTool is the Bot's list_voices: the speech models it may name, and the
// voices of one of them (its own by default).
func (s *Service) VoicesTool(ctx context.Context, b *db.Bot, args map[string]any) (string, error) {
	snd := s.SoundOf(ctx, b)
	if snd.Model == "" {
		return "", errNoSpeech
	}
	allowed := s.cfg().KindModels(llm.KindSpeech)
	model := snd.Model
	if m, _ := args["model"].(string); strings.TrimSpace(m) != "" {
		if model = strings.TrimSpace(m); !llm.Allowed(model, allowed) {
			return "", fmt.Errorf("model %q is not an allowed speech model; allowed: %s", model, strings.Join(allowed, ", "))
		}
	}
	voices, err := s.voices(ctx, model)
	if err != nil && len(voices) == 0 {
		return "", fmt.Errorf("list voices: %w", err)
	}
	type row struct {
		ID       string `json:"id"`
		Name     string `json:"name,omitempty"`
		Language string `json:"language,omitempty"`
	}
	rows := make([]row, 0, len(voices))
	for _, v := range voices {
		rows = append(rows, row{ID: v.ID, Name: v.Name, Language: v.Language})
	}
	out := map[string]any{"model": model, "models": allowed, "voices": rows}
	if model == snd.Model {
		out["default_voice"] = snd.Voice
	}
	body, err := json.Marshal(out)
	return string(body), err
}

// voiceSpeaks reports whether a voice is of a language (a voice with no
// language speaks any).
func voiceSpeaks(voices []speech.Voice, id, lang string) bool {
	for _, v := range voices {
		if v.ID == id {
			return v.Language == "" || strings.HasPrefix(strings.ToLower(v.Language), strings.ToLower(lang))
		}
	}
	return false
}

// readSpeech is a spoken answer as 16-bit mono PCM and its rate, whatever
// form the model answered in.
func readSpeech(out llm.Speech) ([]byte, int, error) {
	defer out.Body.Close()
	switch out.Format {
	case "mp3":
		return speech.DecodeMP3(out.Body)
	case "wav":
		raw, err := io.ReadAll(io.LimitReader(out.Body, workspace.PutMax))
		if err != nil {
			return nil, 0, err
		}
		return speech.ReadWAV(raw)
	}
	raw, err := io.ReadAll(io.LimitReader(out.Body, workspace.PutMax))
	if err != nil {
		return nil, 0, err
	}
	rate := out.SampleRate
	if rate <= 0 {
		rate = llm.SpeechSampleRate
	}
	return raw[:len(raw)&^1], rate, nil
}

// speakPieces cuts a text into requests of at most max characters: at a
// paragraph where one fits, else a sentence, else a word.
func speakPieces(text string, max int) []string {
	var out []string
	rest := []rune(text)
	for len(rest) > max {
		cut := -1
		for _, seps := range []string{"\n", ".!?…", " "} {
			for i := max - 1; i >= max/3; i-- {
				if strings.ContainsRune(seps, rest[i]) {
					cut = i + 1
					break
				}
			}
			if cut > 0 {
				break
			}
		}
		if cut < 0 {
			cut = max
		}
		if piece := strings.TrimSpace(string(rest[:cut])); piece != "" {
			out = append(out, piece)
		}
		rest = rest[cut:]
	}
	if piece := strings.TrimSpace(string(rest)); piece != "" {
		out = append(out, piece)
	}
	return out
}

// wavPath is where a recording goes: the path the Bot gave with .wav as its
// extension (that is the format written), or audio/<first words>-<id>.wav.
func wavPath(dest, text string) (string, error) {
	dest = workspace.Rel(dest)
	for _, part := range strings.Split(dest, "/") {
		if part == ".." {
			return "", errors.New("path must stay inside the workspace")
		}
	}
	if dest == "" || strings.HasSuffix(dest, "/") {
		return path.Join(dest, audioDirFor(dest), fileWords(text)+"-"+ids.New()[:6]+".wav"), nil
	}
	dest = path.Clean(dest)
	return strings.TrimSuffix(dest, path.Ext(dest)) + ".wav", nil
}

func audioDirFor(dest string) string {
	if dest == "" {
		return audioDir
	}
	return ""
}

// fileWords is the first words of a text as a file name.
func fileWords(text string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(text) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if gap && b.Len() > 0 {
				b.WriteByte('-')
			}
			gap = false
			b.WriteRune(r)
		} else {
			gap = true
		}
		if b.Len() >= 32 {
			break
		}
	}
	if b.Len() == 0 {
		return "speech"
	}
	return b.String()
}

// Subtitles writes timed segments as a subtitle file: WebVTT for a .vtt
// name, SubRip otherwise.
func Subtitles(segments []llm.Segment, vtt bool) string {
	var b strings.Builder
	sep := ","
	if vtt {
		b.WriteString("WEBVTT\n\n")
		sep = "."
	}
	stamp := func(sec float64) string {
		ms := int64(sec*1000 + 0.5)
		if ms < 0 {
			ms = 0
		}
		return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3600000, ms/60000%60, ms/1000%60, sep, ms%1000)
	}
	n := 0
	for _, sg := range segments {
		text := strings.TrimSpace(sg.Text)
		if text == "" {
			continue
		}
		n++
		end := sg.End
		if end <= sg.Start {
			end = sg.Start + 2
		}
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n, stamp(sg.Start), stamp(end), text)
	}
	return b.String()
}
