package voice

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/models"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/lang"
	"silo.agent/internal/llm"
	"silo.agent/internal/speech"
	"silo.agent/internal/textx"
)

// speakTimeout bounds one speech request: the provider has to start
// answering by then, and a long chunk has to finish.
const speakTimeout = 60 * time.Second

// Read aloud limits: a message up to readMax runes is spoken whole; a preview
// is a sentence or two.
const (
	readMax    = 20000
	previewMax = 300
	ticketTTL  = 5 * time.Minute
)

// errNoSpeech is answered when voice is off or no speech model is set.
var errNoSpeech = errors.New("read aloud is off: set a speech model under Admin → Settings → Voice")

// Sound is how a Bot sounds right now: the speech model, its voice and speed,
// and the language its pointers are said in.
type Sound struct {
	Model    string
	Voice    string
	Speed    float64
	Language string
}

// SpeechEnabled reports whether Read aloud can work: voice on and a speech
// model whose provider is a Speaker.
func (s *Service) SpeechEnabled() bool {
	m := s.cfg().SpeechModel()
	if m == "" {
		return false
	}
	return models.Can[llm.Speaker](s.models, m, "speak") == nil
}

// CallEnabled reports whether a call can work: speech both ways.
func (s *Service) CallEnabled() bool {
	return s.SpeechEnabled() && s.Enabled()
}

// SoundOf is how a Bot sounds: its own speech model while it is still
// allowed, else the operator's; its voice while it is a voice of that model,
// else one picked for its language.
func (s *Service) SoundOf(ctx context.Context, b *db.Bot) Sound {
	cfg := s.cfg()
	out := Sound{Model: cfg.SpeechModel(), Speed: b.SpeechSpeed, Language: lang.Bot(b.Language)}
	if m := strings.TrimSpace(b.SpeechModel); m != "" && llm.Allowed(m, cfg.KindModels(llm.KindSpeech)) {
		out.Model = m
	}
	if out.Language == lang.Mixed {
		out.Language = ""
	}
	voices, _ := s.voices(ctx, out.Model)
	switch v := strings.TrimSpace(b.Voice); {
	case v != "" && (len(voices) == 0 || contains(voices, v)):
		out.Voice = v
	default:
		out.Voice = speech.PickVoice(voices, out.Language)
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// voices are a speech model's voices, where its provider lists them.
func (s *Service) voices(ctx context.Context, modelID string) ([]string, error) {
	if modelID == "" {
		return nil, nil
	}
	client, _, model, err := s.models.Client(modelID)
	if err != nil {
		return nil, err
	}
	lister, ok := client.(llm.VoiceLister)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return lister.Voices(ctx, model)
}

// Speak asks the Bot's speech model to say one piece of text, already made
// speakable. It is recorded in the LLM log as the Bot's.
func (s *Service) Speak(ctx context.Context, botID string, snd Sound, text string, format string) (llm.Speech, error) {
	if snd.Model == "" {
		return llm.Speech{}, errNoSpeech
	}
	client, provider, model, err := s.models.Client(snd.Model)
	if err != nil {
		return llm.Speech{}, err
	}
	sp, ok := client.(llm.Speaker)
	if !ok {
		return llm.Speech{}, fmt.Errorf("%s cannot speak; pick a speech model under Admin → Settings → Voice", provider)
	}
	start := time.Now()
	out, err := sp.Speak(ctx, model, llm.SpeechRequest{Text: text, Voice: snd.Voice, Speed: snd.Speed, Language: snd.Language, Format: format})
	s.models.Record(botID, "speech", llm.Record{
		Provider: provider,
		Model:    model,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: text}},
		Text:     fmt.Sprintf("[%s audio, voice %q]", out.Format, snd.Voice),
		Error:    err,
		Duration: time.Since(start),
	})
	return out, err
}

// ListVoices is a speech model's voices for a Bot's picker, with the voice
// the Bot speaks with when it picks none.
func (s *Service) ListVoices(ctx context.Context, req *connect.Request[v1.ListVoicesRequest]) (*connect.Response[v1.ListVoicesResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	cfg := s.cfg()
	allowed := cfg.KindModels(llm.KindSpeech)
	snd := s.SoundOf(ctx, b)
	modelID := strings.TrimSpace(req.Msg.GetModel())
	if modelID == "" {
		modelID = snd.Model
	}
	out := &v1.ListVoicesResponse{Model: modelID, Models: models.Protos(s.models.Options(llm.KindSpeech)), DefaultModel: cfg.SpeechModel()}
	if modelID == "" {
		return connect.NewResponse(out), nil
	}
	if !llm.Allowed(modelID, allowed) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%q is not an allowed speech model", modelID))
	}
	voices, err := s.voices(ctx, modelID)
	if err != nil {
		// The picker still works: a voice can be typed.
		log.Printf("voice: list voices of %s: %v", modelID, err)
	}
	for _, v := range voices {
		out.Voices = append(out.Voices, &v1.Voice{Id: v, Label: speech.VoiceLabel(v), Language: speech.VoiceLanguage(v)})
	}
	out.DefaultVoice = speech.PickVoice(voices, snd.Language)
	return connect.NewResponse(out), nil
}

// SetBotVoice saves how a Bot sounds, in its own columns (the row is saved
// whole elsewhere).
func (s *Service) SetBotVoice(ctx context.Context, req *connect.Request[v1.SetBotVoiceRequest]) (*connect.Response[v1.Bot], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage)
	if err != nil {
		return nil, err
	}
	modelID := strings.TrimSpace(req.Msg.GetSpeechModel())
	if modelID != "" && !llm.Allowed(modelID, s.cfg().KindModels(llm.KindSpeech)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%q is not an allowed speech model", modelID))
	}
	speed := req.Msg.GetSpeed()
	if speed != 0 && (speed < 0.5 || speed > 2 || math.IsNaN(speed)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("speed must be between 0.5 and 2"))
	}
	voice := textx.CapRunes(strings.TrimSpace(req.Msg.GetVoice()), 200)
	if err := s.db.Model(&db.Bot{}).Where("id = ?", b.ID).Updates(map[string]any{"speech_model": modelID, "voice": voice, "speech_speed": speed}).Error; err != nil {
		return nil, err
	}
	b.SpeechModel, b.Voice, b.SpeechSpeed = modelID, voice, speed
	if s.host == nil {
		return connect.NewResponse(&v1.Bot{Id: b.ID, SpeechModel: modelID, Voice: voice, SpeechSpeed: speed}), nil
	}
	return connect.NewResponse(s.host.ViewBot(ctx, b)), nil
}

// ticket is one prepared Read aloud: who may play it, and what. The first
// request speaks it and keeps the audio, so a player that asks again (media
// elements send range requests) is served the same bytes without a second
// round of speech.
type ticket struct {
	user, bot string
	snd       Sound
	chunks    []string
	exp       time.Time

	mu      sync.Mutex
	started bool
	done    chan struct{}
	audio   []byte
	ctype   string
	failed  bool
}

// keepMax caps the audio a ticket keeps for a replay.
const keepMax = 24 << 20

type tickets struct {
	mu sync.Mutex
	m  map[string]*ticket
}

func (t *tickets) put(tk *ticket) string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]*ticket{}
	}
	now := time.Now()
	for k, v := range t.m {
		if now.After(v.exp) {
			delete(t.m, k)
		}
	}
	tk.done = make(chan struct{})
	t.m[id] = tk
	return id
}

// get finds a ticket that has not expired.
func (t *tickets) get(id string) *ticket {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk := t.m[id]
	if tk == nil || time.Now().After(tk.exp) {
		return nil
	}
	return tk
}

// PrepareSpeech readies text for Read aloud: it is made speakable and cut
// into sentences now, and the URL it answers streams them in the Bot's voice.
func (s *Service) PrepareSpeech(ctx context.Context, req *connect.Request[v1.PrepareSpeechRequest]) (*connect.Response[v1.PrepareSpeechResponse], error) {
	b, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use)
	if err != nil {
		return nil, err
	}
	if !s.cfg().Voice.Enabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoSpeech)
	}
	snd := s.SoundOf(ctx, b)
	preview := false
	if m := strings.TrimSpace(req.Msg.GetModel()); m != "" {
		if !llm.Allowed(m, s.cfg().KindModels(llm.KindSpeech)) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%q is not an allowed speech model", m))
		}
		snd.Model, preview = m, true
		if req.Msg.GetVoice() == "" {
			voices, _ := s.voices(ctx, m)
			snd.Voice = speech.PickVoice(voices, snd.Language)
		}
	}
	if v := strings.TrimSpace(req.Msg.GetVoice()); v != "" {
		snd.Voice, preview = textx.CapRunes(v, 200), true
	}
	if sp := req.Msg.GetSpeed(); sp >= 0.5 && sp <= 2 {
		snd.Speed, preview = sp, true
	}
	if snd.Model == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoSpeech)
	}
	limit := readMax
	if preview {
		limit = previewMax
	}
	text := speech.ForSpeech(textx.CapRunes(req.Msg.GetText(), limit), speech.PointersFor(snd.Language))
	if text == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("there is nothing to read aloud"))
	}
	user := access.User(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in"))
	}
	id := s.tickets.put(&ticket{user: user.ID, bot: b.ID, snd: snd, chunks: speech.Split(text), exp: time.Now().Add(ticketTTL)})
	return connect.NewResponse(&v1.PrepareSpeechResponse{Url: "/voice/speak?t=" + id}), nil
}

// ServeSpeak streams a prepared Read aloud: one speech request per sentence,
// in order, written as they come so playback starts on the first. MP3 frames
// follow one another as they are; PCM goes out as one WAV of unknown length.
func (s *Service) ServeSpeak(w http.ResponseWriter, r *http.Request) {
	u, err := siloauth.UserFromRequest(s.db, r, s.cfg().HostCookies())
	if err != nil {
		access.HTTPSessionError(w, err)
		return
	}
	tk := s.tickets.get(r.URL.Query().Get("t"))
	if tk == nil || tk.user != u.ID || !access.CanUse(s.db, tk.bot, u.ID) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	tk.mu.Lock()
	first := !tk.started
	tk.started = true
	tk.mu.Unlock()
	if !first {
		select {
		case <-tk.done:
		case <-ctx.Done():
			return
		}
		if tk.failed || len(tk.audio) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", tk.ctype)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "speech", time.Time{}, bytes.NewReader(tk.audio))
		return
	}
	defer close(tk.done)
	var kept bytes.Buffer
	defer func() {
		if kept.Len() > 0 && kept.Len() <= keepMax {
			tk.audio = kept.Bytes()
		} else {
			tk.failed = true
		}
	}()
	out := io.MultiWriter(flushWriter{w, nil}, &capped{&kept})
	flusher, _ := w.(http.Flusher)
	started := ""
	for i, chunk := range tk.chunks {
		cctx, cancel := context.WithTimeout(ctx, speakTimeout)
		// MP3 plays progressively in every browser and needs no header that
		// knows the length; a model that only makes PCM gets a WAV.
		sp, err := s.Speak(cctx, tk.bot, tk.snd, chunk, "mp3")
		if err != nil {
			cancel()
			if i == 0 {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			log.Printf("voice: read aloud bot=%s chunk %d: %v", tk.bot, i, err)
			return
		}
		if started == "" {
			started = sp.Format
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			tk.ctype = "audio/wav"
			if sp.Format == "mp3" {
				tk.ctype = "audio/mpeg"
			}
			w.Header().Set("Content-Type", tk.ctype)
			w.WriteHeader(http.StatusOK)
			if sp.Format == "pcm" {
				_, _ = out.Write(speech.WAVHeader(sp.SampleRate, -1))
			}
		}
		if sp.Format != started {
			// One answer is one container; a model does not switch midway.
			sp.Body.Close()
			cancel()
			log.Printf("voice: read aloud bot=%s: %s after %s", tk.bot, sp.Format, started)
			return
		}
		body := io.Reader(sp.Body)
		if sp.Format == "wav" && i > 0 {
			body = speech.SkipWAVHeader(sp.Body)
		}
		_, err = io.Copy(io.MultiWriter(flushWriter{w, flusher}, &capped{&kept}), body)
		sp.Body.Close()
		cancel()
		if err != nil {
			return
		}
	}
}

// capped keeps what is written up to keepMax and drops the rest.
type capped struct{ b *bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if c.b.Len() <= keepMax {
		c.b.Write(p)
	}
	return len(p), nil
}

type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if f.f != nil {
		f.f.Flush()
	}
	return n, err
}
