package lang

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestUI(t *testing.T) {
	for in, want := range map[string]string{"": "en", "en": "en", "pl": "pl", "de": "de", "fr": "en", "PL": "en", "mixed": "en"} {
		if got := UI(in); got != want {
			t.Errorf("UI(%q) = %q, want %q", in, got, want)
		}
	}
	for _, ok := range []string{"en", "pl", "de"} {
		if err := CheckUI(ok); err != nil {
			t.Errorf("CheckUI(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "fr", "mixed", " pl"} {
		if CheckUI(bad) == nil {
			t.Errorf("CheckUI(%q) passed", bad)
		}
	}
}

func TestBot(t *testing.T) {
	// A Bot from before languages has none, and answers as it always did: in
	// whatever language it is written to.
	for in, want := range map[string]string{"": Mixed, "mixed": Mixed, "pl": "pl", "pt-BR": "pt-BR", "xx": Mixed} {
		if got := Bot(in); got != want {
			t.Errorf("Bot(%q) = %q, want %q", in, got, want)
		}
	}
	for _, ok := range []string{"mixed", "en", "pl", "de", "ja", "zh-Hant"} {
		if err := Check(ok); err != nil {
			t.Errorf("Check(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "xx", "Polish", "PL", "pl ", "../pl"} {
		if Check(bad) == nil {
			t.Errorf("Check(%q) passed", bad)
		}
	}
	// Every language the console itself speaks is one a Bot can be set to.
	for _, code := range UICodes() {
		if err := Check(code); err != nil {
			t.Errorf("UI language %q is not a Bot language: %v", code, err)
		}
	}
}

func TestPromptNamesTheLanguage(t *testing.T) {
	if got := Prompt("pl"); !strings.Contains(got, "Polish") {
		t.Errorf("Prompt(pl) = %q", got)
	}
	mixed := Prompt(Mixed)
	if mixed == "" || mixed != Prompt("") || strings.Contains(mixed, "Polish") {
		t.Errorf("Prompt(mixed) = %q, Prompt(\"\") = %q", mixed, Prompt(""))
	}
	if got := Title("de"); !strings.Contains(got, "German") {
		t.Errorf("Title(de) = %q", got)
	}
	if Title("") != Title(Mixed) || Title("") == "" {
		t.Errorf("Title(\"\") = %q, Title(mixed) = %q", Title(""), Title(Mixed))
	}
}

// The web console lists the same languages (web/src/languages.ts): a code only
// one side knows would be offered and then refused, or never offered.
func TestWebListsTheSameLanguages(t *testing.T) {
	src, err := os.ReadFile("../../web/src/languages.ts")
	if err != nil {
		t.Fatal(err)
	}
	list := func(name string) []string {
		m := regexp.MustCompile(`(?s)export const ` + name + ` = \[(.*?)\] as const`).FindSubmatch(src)
		if m == nil {
			t.Fatalf("languages.ts has no %s list", name)
		}
		var out []string
		for _, c := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(m[1], -1) {
			out = append(out, string(c[1]))
		}
		sort.Strings(out)
		return out
	}
	same := func(what string, got, want []string) {
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: web has %v, the server %v", what, got, want)
		}
	}
	same("display languages", list("uiLanguages"), UICodes())
	same("Bot languages", list("botLanguages"), Codes())
}
