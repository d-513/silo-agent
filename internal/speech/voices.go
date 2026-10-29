package speech

import (
	"strings"
	"unicode"
)

// VoiceLanguage reads the language a voice id names, the ways providers write
// it: "en_paul_neutral", "en-US-Harper:MAI-Voice-2.1", "pl-PL-Zofia",
// Kokoro's "af_heart" (a = American English) and "pf_dora" (p = Portuguese).
// "" when it names none.
func VoiceLanguage(id string) string {
	low := strings.ToLower(id)
	if len(low) >= 3 && (low[2] == '_' || low[2] == '-') && isLetters(low[:2]) {
		return low[:2]
	}
	// Kokoro: a one-letter language and a one-letter gender.
	if len(low) >= 3 && low[2] == '_' && (low[1] == 'f' || low[1] == 'm') {
		if l, ok := kokoro[low[0]]; ok {
			return l
		}
	}
	return ""
}

var kokoro = map[byte]string{'a': "en", 'b': "en", 'e': "es", 'f': "fr", 'h': "hi", 'i': "it", 'j': "ja", 'p': "pt", 'z': "zh"}

func isLetters(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// VoiceLabel makes a voice id readable: "en_paul_neutral" → "Paul, neutral",
// "en-US-Harper:MAI-Voice-2.1" → "Harper (en-US)", "af_heart" → "Heart".
func VoiceLabel(id string) string {
	base := id
	if i := strings.IndexByte(base, ':'); i > 0 {
		base = base[:i]
	}
	// A locale and a name: en-US-Harper.
	if parts := strings.Split(base, "-"); len(parts) >= 3 && len(parts[0]) == 2 && len(parts[1]) == 2 {
		return title(strings.Join(parts[2:], " ")) + " (" + parts[0] + "-" + parts[1] + ")"
	}
	parts := strings.FieldsFunc(base, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) > 1 && VoiceLanguage(id) != "" {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return id
	}
	if len(parts) == 1 {
		return title(parts[0])
	}
	return title(parts[0]) + ", " + strings.Join(parts[1:], " ")
}

func title(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Voice is a voice as a picker shows it: the provider's id, and the name
// and language it gave (or that were read off the id).
type Voice struct {
	ID       string
	Name     string
	Language string
}

// Described fills in what a provider left out: a readable name and the
// language its id names.
func (v Voice) Described() Voice {
	if v.Name == "" {
		v.Name = VoiceLabel(v.ID)
	}
	if v.Language == "" {
		v.Language = VoiceLanguage(v.ID)
	}
	v.Language = strings.ToLower(v.Language)
	if i := strings.IndexAny(v.Language, "-_"); i > 0 {
		v.Language = v.Language[:i]
	}
	return v
}

// PickVoice chooses a voice for a language from a model's list: one in that
// language, preferring a neutral one, else an English one, else the first.
// "" when the list is empty (the provider then picks, or refuses).
func PickVoice(voices []Voice, lang string) string {
	if len(voices) == 0 {
		return ""
	}
	lang = strings.ToLower(lang)
	var match []Voice
	for _, v := range voices {
		if lang != "" && v.Described().Language == lang {
			match = append(match, v)
		}
	}
	if len(match) == 0 {
		// None in the language: any is as good, but English over another.
		match = voices
		if lang != "" && lang != "en" {
			for _, v := range voices {
				if v.Described().Language == "en" {
					return v.ID
				}
			}
		}
	}
	for _, v := range match {
		if strings.Contains(strings.ToLower(v.ID+" "+v.Name), "neutral") {
			return v.ID
		}
	}
	return match[0].ID
}
