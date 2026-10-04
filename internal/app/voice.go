package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/llm"
)

// transcribeTimeout bounds one speech-to-text call. OpenRouter's upstream cap
// is 60 s; local servers on a CPU can be slower.
const transcribeTimeout = 90 * time.Second

// errVoiceOff is returned when the operator set transcribe_model: off.
var errVoiceOff = errors.New("voice is off (transcribe_model: off)")

// voiceEnabled reports whether dictation and the transcribe tool can work: a
// model is configured and its provider can be built (key and URL present).
func (a *App) voiceEnabled() bool {
	modelID := a.cfg().TranscribeModel()
	if modelID == "" {
		return false
	}
	client, _, _, err := a.providerClient(modelID)
	if err != nil {
		return false
	}
	_, ok := client.(llm.Transcriber)
	return ok
}

// transcribe turns one recording into text with the operator's
// transcribe_model. label names the caller in the LLM debug log.
func (a *App) transcribe(ctx context.Context, botID, label string, audio llm.Audio) (llm.Transcript, error) {
	modelID := a.cfg().TranscribeModel()
	if modelID == "" {
		return llm.Transcript{}, errVoiceOff
	}
	if len(audio.Data) == 0 {
		return llm.Transcript{}, errors.New("audio is empty")
	}
	if len(audio.Data) > llm.MaxAudioBytes {
		return llm.Transcript{}, fmt.Errorf("audio is %d MB; the cap is %d MB", len(audio.Data)>>20, llm.MaxAudioBytes>>20)
	}
	client, provider, model, err := a.providerClient(modelID)
	if err != nil {
		return llm.Transcript{}, err
	}
	tr, ok := client.(llm.Transcriber)
	if !ok {
		return llm.Transcript{}, fmt.Errorf("%s cannot transcribe; set transcribe_model to an OpenAI-compatible model", provider)
	}
	ctx, cancel := context.WithTimeout(ctx, transcribeTimeout)
	defer cancel()
	start := time.Now()
	out, err := tr.Transcribe(ctx, model, audio)
	out.Text = validUTF8(strings.TrimSpace(out.Text))
	a.recordLLM(botID, label, llm.Record{
		Provider: provider,
		Model:    model,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: fmt.Sprintf("[audio %s, %d bytes, %.1fs]", audio.MIME, len(audio.Data), out.Seconds)}},
		Text:     out.Text,
		Error:    err,
		Duration: time.Since(start),
	})
	if err != nil {
		return llm.Transcript{}, fmt.Errorf("transcribe: %w", err)
	}
	return out, nil
}

// canTranscribe reports whether modelID names a provider that implements
// llm.Transcriber, without needing its API key.
func (a *App) canTranscribe(modelID string) error {
	return providerCan[llm.Transcriber](a, modelID, "transcribe")
}

// Transcribe is composer dictation: the browser records, the CP transcribes,
// and the text goes back into the composer for the human to edit. It needs no
// worker, so it works while the box is down.
func (a *App) Transcribe(ctx context.Context, req *connect.Request[v1.TranscribeRequest]) (*connect.Response[v1.TranscribeResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	data := req.Msg.GetAudio()
	if len(data) == 0 || len(data) > llm.MaxAudioBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("recording must be 1 byte to %d MB", llm.MaxAudioBytes>>20))
	}
	out, err := a.transcribe(ctx, b.ID, "dictation", llm.Audio{
		Data:     data,
		MIME:     strings.TrimSpace(req.Msg.GetMime()),
		Language: strings.TrimSpace(req.Msg.GetLanguage()),
	})
	if errors.Is(err, errVoiceOff) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	return connect.NewResponse(&v1.TranscribeResponse{Text: out.Text}), nil
}

// runTools is the chat tool list for one run: toolDefs minus transcribe when
// voice is off or unconfigured, so the model is never offered a dead tool.
func (a *App) runTools() []llm.Tool {
	if a.voiceEnabled() {
		return toolDefs
	}
	out := make([]llm.Tool, 0, len(toolDefs))
	for _, t := range toolDefs {
		if t.Name != "transcribe" {
			out = append(out, t)
		}
	}
	return out
}

// audioMIME maps a recording's extension to the MIME type sent upstream; ""
// means not audio.
func audioMIME(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3", ".mpga", ".mpeg":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a", ".mp4", ".aac":
		return "audio/mp4"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".webm":
		return "audio/webm"
	case ".flac":
		return "audio/flac"
	}
	return ""
}

// transcribeTool is the Bot's transcribe (chat tool and
// silo_runtime.transcribe). The caller already authorized bot.transcribe.
func (a *App) transcribeTool(ctx context.Context, botID, path, language string, structured bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path required")
	}
	mime := audioMIME(path)
	if mime == "" {
		return "", fmt.Errorf("%s is not an audio file (mp3, wav, m4a, ogg, opus, webm, flac)", path)
	}
	if a.cfg().TranscribeModel() == "" {
		return "", errVoiceOff
	}
	file, err := a.workspaceAttachment(ctx, botID, path)
	if err != nil {
		return "", err
	}
	out, err := a.transcribe(ctx, botID, "transcribe", llm.Audio{
		Data: file.Data, MIME: mime, Filename: file.Name, Language: strings.TrimSpace(language),
	})
	if err != nil {
		return "", err
	}
	if !structured {
		if out.Text == "" {
			return "(no speech found)", nil
		}
		return out.Text, nil
	}
	b, err := json.Marshal(map[string]any{"text": out.Text, "language": out.Language, "seconds": out.Seconds})
	return string(b), err
}
