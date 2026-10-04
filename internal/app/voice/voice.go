// Package voice is speech-to-text: composer dictation and the Bot's transcribe
// tool, both through the operator's transcribe_model.
package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/config"
	"silo.agent/internal/llm"
	"silo.agent/internal/textx"
)

// transcribeTimeout bounds one speech-to-text call. OpenRouter's upstream cap
// is 60 s; local servers on a CPU can be slower.
const transcribeTimeout = 90 * time.Second

// errVoiceOff is returned when the operator set transcribe_model: off.
var errVoiceOff = errors.New("voice is off (transcribe_model: off)")

// Service transcribes audio with the operator's transcribe model.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	models *models.Service
	ws     *workspace.Service
}

func New(gdb *gorm.DB, cfg func() config.Config, m *models.Service, ws *workspace.Service) *Service {
	return &Service{db: gdb, cfg: cfg, models: m, ws: ws}
}

// Enabled reports whether dictation and the transcribe tool can work: a
// model is configured and its provider can be built (key and URL present).
func (s *Service) Enabled() bool {
	modelID := s.cfg().TranscribeModel()
	if modelID == "" {
		return false
	}
	client, _, _, err := s.models.Client(modelID)
	if err != nil {
		return false
	}
	_, ok := client.(llm.Transcriber)
	return ok
}

// transcribe turns one recording into text with the operator's
// transcribe_model. label names the caller in the LLM debug log.
func (s *Service) transcribe(ctx context.Context, botID, label string, audio llm.Audio) (llm.Transcript, error) {
	modelID := s.cfg().TranscribeModel()
	if modelID == "" {
		return llm.Transcript{}, errVoiceOff
	}
	if len(audio.Data) == 0 {
		return llm.Transcript{}, errors.New("audio is empty")
	}
	if len(audio.Data) > llm.MaxAudioBytes {
		return llm.Transcript{}, fmt.Errorf("audio is %d MB; the cap is %d MB", len(audio.Data)>>20, llm.MaxAudioBytes>>20)
	}
	client, provider, model, err := s.models.Client(modelID)
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
	out.Text = textx.ValidUTF8(strings.TrimSpace(out.Text))
	s.models.Record(botID, label, llm.Record{
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

// CanTranscribe reports whether modelID names a provider that implements
// llm.Transcriber, without needing its API key.
func (s *Service) CanTranscribe(modelID string) error {
	return models.Can[llm.Transcriber](s.models, modelID, "transcribe")
}

// Transcribe is composer dictation: the browser records, the CP transcribes,
// and the text goes back into the composer for the human to edit. It needs no
// worker, so it works while the box is down.
func (s *Service) Transcribe(ctx context.Context, req *connect.Request[v1.TranscribeRequest]) (*connect.Response[v1.TranscribeResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	data := req.Msg.GetAudio()
	if len(data) == 0 || len(data) > llm.MaxAudioBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("recording must be 1 byte to %d MB", llm.MaxAudioBytes>>20))
	}
	out, err := s.transcribe(ctx, b.ID, "dictation", llm.Audio{
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

// Tool is the Bot's transcribe (chat tool and
// silo_runtime.transcribe). The caller already authorized bot.transcribe.
func (s *Service) Tool(ctx context.Context, botID, path, language string, structured bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path required")
	}
	mime := audioMIME(path)
	if mime == "" {
		return "", fmt.Errorf("%s is not an audio file (mp3, wav, m4a, ogg, opus, webm, flac)", path)
	}
	if s.cfg().TranscribeModel() == "" {
		return "", errVoiceOff
	}
	file, err := s.ws.Attachment(ctx, botID, path)
	if err != nil {
		return "", err
	}
	out, err := s.transcribe(ctx, botID, "transcribe", llm.Audio{
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
