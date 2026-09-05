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
)

const (
	Once   = "allow_once"
	Always = "always"
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

type kind struct {
	title   string
	summary func(map[string]string) string
	fields  func(map[string]string) []Field
}

var catalog = map[string]kind{
	"secrets.get": {
		title: "Read a secret",
		summary: func(args map[string]string) string {
			if n := args["name"]; n != "" {
				return "This Bot wants the stored secret “" + n + "”. The value is not shown here."
			}
			return "This Bot wants a stored secret. The value is not shown here."
		},
		fields: func(args map[string]string) []Field {
			if n := args["name"]; n != "" {
				return []Field{{Label: "Secret", Value: n}}
			}
			return nil
		},
	},
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

func Describe(connector, action, argsJSON string) Prompt {
	args := parseArgs(argsJSON)
	if k, ok := catalog[Key(connector, action)]; ok {
		return Prompt{Title: k.title, Summary: k.summary(args), Fields: k.fields(args)}
	}
	return generic(connector, action, args)
}

func generic(connector, action string, args map[string]string) Prompt {
	p := Prompt{
		Title:   phrase(action) + " · " + phrase(connector),
		Summary: "This Bot wants to " + strings.ToLower(phrase(action)) + " (" + phrase(connector) + ").",
	}
	for _, key := range sortedKeys(args) {
		v := args[key]
		if v == "" {
			continue
		}
		p.Fields = append(p.Fields, Field{Label: phrase(key), Value: v})
	}
	return p
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
