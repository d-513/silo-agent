// Package lang is the languages Silo knows by name. There are two lists. The
// console is translated into a few (a user's display language); a Bot can be
// told to write in many more, or in Mixed: whatever it is written to in.
package lang

import (
	"fmt"
	"sort"
)

// Default is the display language of a user who has picked none.
const Default = "en"

// Mixed is a Bot with no language of its own: it answers in the language of
// the message.
const Mixed = "mixed"

// ui is the languages the web console is translated into.
var ui = []string{"en", "pl", "de"}

// names is every language a Bot can be set to, by its BCP 47 code, with the
// English name the model is told.
var names = map[string]string{
	"ar":      "Arabic",
	"bg":      "Bulgarian",
	"bn":      "Bengali",
	"ca":      "Catalan",
	"cs":      "Czech",
	"da":      "Danish",
	"de":      "German",
	"el":      "Greek",
	"en":      "English",
	"es":      "Spanish",
	"et":      "Estonian",
	"fa":      "Persian",
	"fi":      "Finnish",
	"fr":      "French",
	"he":      "Hebrew",
	"hi":      "Hindi",
	"hr":      "Croatian",
	"hu":      "Hungarian",
	"id":      "Indonesian",
	"it":      "Italian",
	"ja":      "Japanese",
	"ko":      "Korean",
	"lt":      "Lithuanian",
	"lv":      "Latvian",
	"nb":      "Norwegian",
	"nl":      "Dutch",
	"pl":      "Polish",
	"pt":      "Portuguese",
	"pt-BR":   "Brazilian Portuguese",
	"ro":      "Romanian",
	"ru":      "Russian",
	"sk":      "Slovak",
	"sl":      "Slovenian",
	"sr":      "Serbian",
	"sv":      "Swedish",
	"th":      "Thai",
	"tr":      "Turkish",
	"uk":      "Ukrainian",
	"vi":      "Vietnamese",
	"zh-Hans": "Simplified Chinese",
	"zh-Hant": "Traditional Chinese",
}

// UICodes is the display languages, the default first.
func UICodes() []string {
	return append([]string(nil), ui...)
}

// Codes is every language a Bot can be set to, sorted. Mixed is not one of
// them: it is the absence of one.
func Codes() []string {
	out := make([]string, 0, len(names))
	for c := range names {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// UI is the display language stored as code: itself when the console speaks
// it, else the default. A user from before languages has none stored.
func UI(code string) string {
	if CheckUI(code) == nil {
		return code
	}
	return Default
}

// CheckUI reports whether code is a language the console is translated into.
func CheckUI(code string) error {
	for _, c := range ui {
		if c == code {
			return nil
		}
	}
	return fmt.Errorf("unknown display language %q (one of %v)", code, ui)
}

// Bot is the language a Bot is set to as stored: a known code, else Mixed. A
// Bot from before languages has none stored and answers as it always did.
func Bot(code string) string {
	if _, ok := names[code]; ok {
		return code
	}
	return Mixed
}

// Check reports whether code is something a Bot's language can be set to.
func Check(code string) error {
	if code == Mixed {
		return nil
	}
	if _, ok := names[code]; !ok {
		return fmt.Errorf("unknown language %q (a code such as en, pl or de, or %q)", code, Mixed)
	}
	return nil
}

// Prompt tells the model which language to write to people in. It is part of
// the Bot's session tier of the system prompt.
func Prompt(code string) string {
	code = Bot(code)
	if code == Mixed {
		return "Write to people in the language they write to you in."
	}
	return fmt.Sprintf("Write to people in %s: replies, chat messages, Feed posts and anything else a human will read, unless they ask you for another language. Code, commands, file contents and quoted text stay as they are.", names[code])
}

// Title is the sentence that tells the title model which language a chat's
// title is in.
func Title(code string) string {
	code = Bot(code)
	if code == Mixed {
		return "Write it in the language of the message."
	}
	return fmt.Sprintf("Write it in %s.", names[code])
}
