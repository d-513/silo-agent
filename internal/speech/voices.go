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

// PickVoice chooses a voice for a language from a model's list: one that
// names the language, preferring a neutral one, else the first. "" when the
// list is empty (the provider then picks, or refuses).
func PickVoice(voices []string, lang string) string {
	if len(voices) == 0 {
		return ""
	}
	lang = strings.ToLower(lang)
	var match []string
	for _, v := range voices {
		if lang != "" && VoiceLanguage(v) == lang {
			match = append(match, v)
		}
	}
	if len(match) == 0 {
		// A list with no languages in its ids: any is as good.
		match = voices
		if lang != "" && lang != "en" {
			// Prefer an English one over another language's.
			for _, v := range voices {
				if VoiceLanguage(v) == "en" {
					return v
				}
			}
		}
	}
	for _, v := range match {
		if strings.Contains(strings.ToLower(v), "neutral") {
			return v
		}
	}
	return match[0]
}
