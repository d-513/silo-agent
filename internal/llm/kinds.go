package llm

import "strings"

// The kinds of model besides text and decision (decide.go). A provider says
// which it serves in Descriptor.Kinds, and its client implements each one's
// interface (TestProviderKinds holds the two together).
const (
	// KindTranscription turns speech into text (Transcriber).
	KindTranscription = "transcription"
	// KindSpeech turns text into speech (Speaker).
	KindSpeech = "speech"
	// KindEmbedding turns text into vectors (Embedder).
	KindEmbedding = "embedding"
)

// Kind is one row of the registry: a kind of model and the allowlist in
// silo.yaml that holds the operator's models of it. Everything that treats
// the kinds alike (validation, Admin's lists, a listing's tabs) loops over
// Kinds, so a new kind is a row here, its interface, and its client methods.
type Kind struct {
	ID string
	// List is the silo.yaml key of the kind's allowlist.
	List string
}

// Kinds are every kind of model, in the order Admin shows them.
var Kinds = []Kind{
	{ID: KindText, List: "models"},
	{ID: KindDecision, List: "decision_models"},
	{ID: KindTranscription, List: "transcription_models"},
	{ID: KindSpeech, List: "speech_models"},
	{ID: KindEmbedding, List: "embedding_models"},
}

// KindOf finds a kind by id.
func KindOf(id string) (Kind, bool) {
	for _, k := range Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// Implements reports whether a client can serve a kind: whether it has the
// kind's interface.
func Implements(c Client, kind string) bool {
	switch kind {
	case KindText:
		return c != nil
	case KindDecision:
		_, ok := c.(Decider)
		return ok
	case KindTranscription:
		_, ok := c.(Transcriber)
		return ok
	case KindSpeech:
		_, ok := c.(Speaker)
		return ok
	case KindEmbedding:
		_, ok := c.(Embedder)
		return ok
	}
	return false
}

// GuessKind labels a model a listing did not label (OpenAI's /models, a local
// server) by the names the families go by. It is only a hint: the operator's
// allowlist is what decides a model's kind.
func GuessKind(id string) string {
	id = strings.ToLower(id)
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(id, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("embed", "bge-", "e5-", "nomic"):
		return KindEmbedding
	case has("whisper", "transcribe", "parakeet", "asr", "stt", "moonshine", "canary"):
		return KindTranscription
	case has("tts", "kokoro", "piper", "speech", "voice", "orpheus", "chatterbox"):
		return KindSpeech
	}
	return KindText
}
