package voice

import "strings"

// phrase is fixed text a call says on its own, never a model's: what it says
// when something needs the human, in the Bot's language.
type phrase int

const (
	sayFailed phrase = iota
	sayApproval
	sayBusy
	sayNotHeard
	sayStillWorking
)

var phrases = map[string][]string{
	"en": {
		sayFailed:       "Sorry, that didn't work. The details are in the chat.",
		sayApproval:     "I need your OK for that. It's on your screen.",
		sayBusy:         "One moment, I'm still on the last thing.",
		sayNotHeard:     "Sorry, I didn't catch that.",
		sayStillWorking: "Still working on it.",
	},
	"pl": {
		sayFailed:       "Przepraszam, nie wyszło. Szczegóły są w czacie.",
		sayApproval:     "Potrzebuję na to twojej zgody. Jest na ekranie.",
		sayBusy:         "Chwileczkę, kończę poprzednią rzecz.",
		sayNotHeard:     "Przepraszam, nie dosłyszałem.",
		sayStillWorking: "Wciąż nad tym pracuję.",
	},
	"de": {
		sayFailed:       "Das hat leider nicht geklappt. Die Details stehen im Chat.",
		sayApproval:     "Dafür brauche ich dein OK. Es ist auf deinem Bildschirm.",
		sayBusy:         "Einen Moment, ich bin noch mit dem Letzten beschäftigt.",
		sayNotHeard:     "Entschuldigung, das habe ich nicht verstanden.",
		sayStillWorking: "Ich bin noch dran.",
	},
}

func (p phrase) in(lang string) string {
	if l, ok := phrases[strings.ToLower(lang)]; ok {
		return l[p]
	}
	return phrases["en"][p]
}
