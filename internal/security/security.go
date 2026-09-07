package security

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

const (
	Allow = "allow"
	Ask   = "ask"
	Deny  = "deny"
	Star  = "*"
)

const (
	Once   = "allow_once"
	Always = "always"
)

const (
	Python   = "python"
	Terminal = "terminal"
	Files    = "files"
	Desktop  = "desktop"
	Bot      = "bot"
	Secrets  = "secrets"
	Skills   = "skills"
)

type Field struct {
	Label string
	Value string
}

type Prompt struct {
	Title   string
	Summary string
	Fields  []Field
}

type Row struct {
	Connector string
	Action    string
	Title     string
}

type spec struct {
	title   string
	mode    string
	hide    []string
	summary func(map[string]string) string
	fields  func(map[string]string) []Field
}

var reserved = map[string]bool{
	Python: true, Terminal: true, Files: true, Desktop: true, Bot: true, Secrets: true, Skills: true,
}

var catalog = map[string]spec{
	"python.run":     {title: "Python", mode: Allow, summary: want("run Python")},
	"terminal.run":   {title: "Terminal", mode: Allow, summary: want("run a shell command")},
	"files.*":        {title: "Files", mode: Allow, summary: want("use workspace files")},
	"desktop.*":      {title: "Desktop", mode: Allow, summary: want("use the desktop"), hide: []string{"text"}},
	"bot.soul":       {title: "Soul", mode: Allow, summary: want("edit SOUL")},
	"bot.memory":     {title: "Memory", mode: Allow, summary: want("edit MEMORY")},
	"skills.load":    {title: "Load skill", mode: Allow, summary: want("load a skill")},
	"skills.propose": {title: "Propose skill", mode: Allow, summary: want("show a skill artifact")},
}

func want(s string) func(map[string]string) string {
	return func(map[string]string) string { return "This Bot wants to " + s + "." }
}

func Reserved(slug string) bool {
	return reserved[slug]
}

func BuiltinRows() []Row {
	return []Row{
		{Python, "run", "Python"},
		{Terminal, "run", "Terminal"},
		{Files, Star, "Files"},
		{Desktop, Star, "Desktop"},
		{Bot, "soul", "Soul"},
		{Bot, "memory", "Memory"},
		{Skills, "load", "Load skill"},
		{Skills, "propose", "Propose skill"},
	}
}

func Key(connector, action string) string {
	return connector + "." + action
}

func Rule(s string) string {
	switch s {
	case Allow, Deny:
		return s
	default:
		return Ask
	}
}

func Vote(s string) string {
	switch s {
	case "allow", Once:
		return Once
	case Always:
		return Always
	case Deny:
		return Deny
	default:
		return ""
	}
}

func Granted(vote string) bool {
	return vote != Deny && vote != "canceled" && vote != ""
}

func Default(connector, action string) (string, bool) {
	if s, ok := lookup(connector, action); ok && s.mode != "" {
		return s.mode, true
	}
	if connector == Secrets {
		return Ask, true
	}
	return "", false
}

func Redact(connector, action, argsJSON string) string {
	s, ok := lookup(connector, action)
	if !ok || len(s.hide) == 0 {
		return argsJSON
	}
	args := parseArgs(argsJSON)
	for _, k := range s.hide {
		delete(args, k)
	}
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func Describe(connector, action, argsJSON string) Prompt {
	args := parseArgs(argsJSON)
	if connector == Secrets {
		name := action
		if name == "" || name == "get" {
			name = args["name"]
		} else if args["name"] == "" {
			args["name"] = name
		}
		return secretPrompt(name)
	}
	if k, ok := lookup(connector, action); ok {
		p := Prompt{Title: k.title, Summary: k.summary(args)}
		if k.fields != nil {
			p.Fields = k.fields(args)
		} else {
			p.Fields = visibleFields(args, k.hide)
		}
		return p
	}
	return generic(connector, action, args)
}

func lookup(connector, action string) (spec, bool) {
	if k, ok := catalog[Key(connector, action)]; ok {
		return k, true
	}
	if action != Star {
		if k, ok := catalog[Key(connector, Star)]; ok {
			return k, true
		}
	}
	return spec{}, false
}

func secretPrompt(name string) Prompt {
	p := Prompt{
		Title:   "Read a secret",
		Summary: "This Bot wants a stored secret. The value is not shown here.",
	}
	if name != "" {
		p.Summary = "This Bot wants the stored secret “" + name + "”. The value is not shown here."
		p.Fields = []Field{{Label: "Secret", Value: name}}
	}
	return p
}

func generic(connector, action string, args map[string]string) Prompt {
	p := Prompt{
		Title:   phrase(action) + " · " + phrase(connector),
		Summary: "This Bot wants to " + strings.ToLower(phrase(action)) + " (" + phrase(connector) + ").",
		Fields:  visibleFields(args, nil),
	}
	return p
}

func visibleFields(args map[string]string, hide []string) []Field {
	skip := map[string]bool{}
	for _, k := range hide {
		skip[k] = true
	}
	var out []Field
	for _, key := range sortedKeys(args) {
		if skip[key] {
			continue
		}
		v := args[key]
		if v == "" {
			continue
		}
		out = append(out, Field{Label: phrase(key), Value: v})
	}
	return out
}

func parseArgs(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return out
	}
	for k, v := range obj {
		if v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64, bool:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out
}

func phrase(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	parts := strings.Fields(s)
	for i, p := range parts {
		r := []rune(strings.ToLower(p))
		if len(r) == 0 {
			continue
		}
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
