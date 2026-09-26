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
	// Auto routes the action through the approval model with the Bot's
	// auto-approval policy before it runs.
	Auto = "auto"
	Star = "*"
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
	Web      = "web"
	Artifact = "artifact"
	Channels = "channels"
	Chats    = "chats"
	Model    = "model"
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
	Python: true, Terminal: true, Files: true, Desktop: true, Bot: true, Secrets: true, Skills: true, Web: true, Artifact: true,
	Channels: true, Chats: true, Model: true,
}

var catalog = map[string]spec{
	"python.run":    {title: "Python", mode: Allow, summary: want("run Python")},
	"terminal.run":  {title: "Terminal", mode: Allow, summary: want("run a shell command")},
	"files.*":       {title: "Files", mode: Allow, summary: want("use workspace files")},
	"desktop.*":     {title: "Desktop", mode: Allow, summary: want("use the desktop"), hide: []string{"text"}},
	"bot.soul":      {title: "Soul", mode: Allow, summary: want("edit SOUL")},
	"bot.memory":    {title: "Memory", mode: Allow, summary: want("edit MEMORY")},
	"bot.remember":  {title: "Remember", mode: Allow, summary: want("save a long-term memory")},
	"bot.recall":    {title: "Recall", mode: Allow, summary: want("search its long-term memories")},
	"bot.forget":    {title: "Forget", mode: Allow, summary: want("delete a long-term memory")},
	"skills.load":   {title: "Load skill", mode: Allow, summary: want("load a skill")},
	"artifact.emit": {title: "Artifact", mode: Allow, summary: want("show an artifact")},
	"web.search":    {title: "Web search", mode: Allow, summary: want("search the web")},
	"chats.read":    {title: "Read chats", mode: Allow, summary: want("read chat messages")},
	"model.list":    {title: "List models", mode: Allow, summary: want("list the allowed models")},
	"model.switch":  {title: "Switch model", mode: Allow, summary: switchSummary, fields: switchFields},
	// One rule per channel: the action is the channel ID, so channels.* is the
	// mode for every channel until an individual rule overrides it.
	"channels.*": {title: "Channel", mode: Allow, summary: channelSummary, fields: channelFields},
}

func switchSummary(args map[string]string) string {
	if m := strings.TrimSpace(args["model"]); m != "" {
		return "This Bot wants to switch this conversation to the “" + m + "” model."
	}
	return "This Bot wants to switch to another model."
}

func switchFields(args map[string]string) []Field {
	if m := strings.TrimSpace(args["model"]); m != "" {
		return []Field{{Label: "Model", Value: m}}
	}
	return nil
}

func channelSummary(args map[string]string) string {
	if name := strings.TrimSpace(args["channel"]); name != "" {
		return "This Bot wants to send a message to the “" + name + "” channel."
	}
	return "This Bot wants to send a message to a channel."
}

func channelFields(args map[string]string) []Field {
	var out []Field
	if name := strings.TrimSpace(args["channel"]); name != "" {
		out = append(out, Field{Label: "Channel", Value: name})
	}
	if to := strings.TrimSpace(args["to"]); to != "" {
		out = append(out, Field{Label: "To", Value: to})
	}
	if text := args["text"]; text != "" {
		out = append(out, Field{Label: "Message", Value: text})
	}
	return out
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
		{Bot, "remember", "Remember"},
		{Bot, "recall", "Recall"},
		{Bot, "forget", "Forget"},
		{Skills, "load", "Load skill"},
		{Artifact, "emit", "Artifact"},
		{Web, "search", "Web search"},
		{Chats, "read", "Read chats"},
		{Model, "list", "List models"},
		{Model, "switch", "Switch model"},
	}
}

func Key(connector, action string) string {
	return connector + "." + action
}

func Rule(s string) string {
	switch s {
	case Allow, Deny, Auto:
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
