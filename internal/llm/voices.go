package llm

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Voice is one voice of a speech model. Name and Language are filled when
// the provider says them; an id alone is made readable by the caller.
type Voice struct {
	ID       string
	Name     string
	Language string
}

// VoiceLister is the optional interface a speech provider implements when it
// can say which voices a model has. An empty answer means it does not know.
type VoiceLister interface {
	Voices(ctx context.Context, model string) ([]Voice, error)
}

// voiceIDs are voices known by id only.
func voiceIDs(ids []string) []Voice {
	out := make([]Voice, 0, len(ids))
	for _, id := range ids {
		out = append(out, Voice{ID: id})
	}
	return out
}

// openAIVoices are the voices of OpenAI's own speech models (its API does not
// list them).
var openAIVoices = []string{"alloy", "ash", "ballad", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer", "verse"}

// voiceCache keeps a listing's voices per endpoint and model: a speech model's
// voices change rarely, and the picker asks on every open.
var voiceCache = struct {
	sync.Mutex
	at     map[string]time.Time
	voices map[string]map[string][]string
}{at: map[string]time.Time{}, voices: map[string]map[string][]string{}}

const voiceTTL = 6 * time.Hour

// Voices lists a speech model's voices: OpenAI's fixed set, or what the
// endpoint's model list says (OpenRouter's supported_voices). A server that
// lists none answers none.
func (c *openAICompatClient) Voices(ctx context.Context, model string) ([]Voice, error) {
	if c.openAI {
		return voiceIDs(openAIVoices), nil
	}
	voiceCache.Lock()
	byModel, fresh := voiceCache.voices[c.base], time.Since(voiceCache.at[c.base]) < voiceTTL
	voiceCache.Unlock()
	if !fresh {
		ctx, cancel := context.WithTimeout(ctx, listTimeout)
		defer cancel()
		list, err := fetchModels(ctx, c.base, c.key, "output_modalities=speech")
		if err != nil {
			return nil, err
		}
		byModel = map[string][]string{}
		for _, m := range list {
			if len(m.SupportedVoices) > 0 {
				byModel[m.ID] = m.SupportedVoices
			}
		}
		voiceCache.Lock()
		voiceCache.voices[c.base], voiceCache.at[c.base] = byModel, time.Now()
		voiceCache.Unlock()
	}
	return voiceIDs(byModel[strings.TrimSpace(model)]), nil
}
