package toolsgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	v1 "silo.agent/gen/silo/v1"
)

func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prev := false
	for _, r := range s {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r)
		if ok {
			b.WriteRune(r)
			prev = false
			continue
		}
		if !prev && b.Len() > 0 {
			b.WriteByte('_')
			prev = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "connector"
	}
	if unicode.IsDigit(rune(out[0])) {
		return "c_" + out
	}
	return out
}

func PyName(action string) string {
	var b strings.Builder
	prevLower := false
	for i, r := range action {
		if unicode.IsUpper(r) {
			if i > 0 && prevLower {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			prevLower = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('_')
		}
		prevLower = false
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "action"
	}
	if unicode.IsDigit(rune(out[0])) {
		out = "t_" + out
	}
	switch out {
	case "from", "import", "def", "class", "return", "call", "none", "true", "false", "in", "is", "and", "or", "not", "lambda", "yield", "with", "as", "pass", "global", "async", "await":
		out += "_"
	}
	return out
}

func Write(dir string, stubs []*v1.ToolStub) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "__init__.py"), []byte(""), 0o644); err != nil {
		return err
	}
	pkgs := map[string][]string{}
	for _, s := range stubs {
		if s == nil {
			continue
		}
		slug := Slug(s.GetConnector())
		fn := PyName(s.GetAction())
		pkg := filepath.Join(dir, slug)
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			return err
		}
		body, err := fileBody(s, slug, fn)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(pkg, fn+".py"), []byte(body), 0o644); err != nil {
			return err
		}
		pkgs[slug] = append(pkgs[slug], fn)
	}
	for slug, fns := range pkgs {
		var b strings.Builder
		for _, fn := range fns {
			fmt.Fprintf(&b, "from .%s import %s\n", fn, fn)
		}
		if err := os.WriteFile(filepath.Join(dir, slug, "__init__.py"), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func fileBody(s *v1.ToolStub, slug, fn string) (string, error) {
	sch, err := parseSchema(s.GetArgsSchemaJson())
	if err != nil {
		return "", err
	}
	args, pass := pyArgs(sch)
	doc := funcDoc(s.GetDescription(), s.GetAction(), sch)
	return fmt.Sprintf("from silo_runtime import call\n\ndef %s(%s) -> dict:\n    \"\"\"%s\"\"\"\n    return call(%q, %q, {k: v for k, v in (%s).items() if v is not None})\n",
		fn, args, doc, slug, s.GetAction(), pass), nil
}

type prop struct {
	Type        any    `json:"type"`
	Description string `json:"description"`
	Enum        []any  `json:"enum"`
}

type jsonSchema struct {
	Properties map[string]prop `json:"properties"`
	Required   []string        `json:"required"`
}

func parseSchema(raw string) (jsonSchema, error) {
	var sch jsonSchema
	if strings.TrimSpace(raw) == "" {
		return sch, nil
	}
	if err := json.Unmarshal([]byte(raw), &sch); err != nil {
		return sch, err
	}
	return sch, nil
}

func pyArgs(sch jsonSchema) (sig, pass string) {
	if len(sch.Properties) == 0 {
		return "**kwargs", "kwargs"
	}
	need := map[string]bool{}
	for _, r := range sch.Required {
		need[r] = true
	}
	names := orderedNames(sch)
	var parts []string
	parts = append(parts, "*")
	for _, n := range names {
		id := pyIdent(n)
		ann := pyType(sch.Properties[n].Type)
		if need[n] {
			parts = append(parts, id+": "+ann)
		} else {
			parts = append(parts, id+": "+ann+" | None = None")
		}
	}
	var assigns []string
	for _, n := range names {
		id := pyIdent(n)
		assigns = append(assigns, fmt.Sprintf("%q: %s", n, id))
	}
	return strings.Join(parts, ", "), "{" + strings.Join(assigns, ", ") + "}"
}

func orderedNames(sch jsonSchema) []string {
	need := map[string]bool{}
	for _, r := range sch.Required {
		need[r] = true
	}
	var req, opt []string
	for n := range sch.Properties {
		if need[n] {
			req = append(req, n)
		} else {
			opt = append(opt, n)
		}
	}
	slices.Sort(req)
	slices.Sort(opt)
	return append(req, opt...)
}

func funcDoc(desc, action string, sch jsonSchema) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		desc = action
	}
	var b strings.Builder
	b.WriteString(desc)
	for _, n := range orderedNames(sch) {
		p := sch.Properties[n]
		fmt.Fprintf(&b, "\n%s (%s)", n, pyType(p.Type))
		if !slices.Contains(sch.Required, n) {
			b.WriteString(", optional")
		}
		if d := strings.TrimSpace(p.Description); d != "" {
			fmt.Fprintf(&b, ": %s", d)
		}
		if e := enumNote(p.Enum); e != "" {
			fmt.Fprintf(&b, " one of: %s", e)
		}
	}
	out := b.String()
	if len(out) > 2500 {
		cut := 2500
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out = out[:cut] + "…"
	}
	// Escape for a """…""" literal: a lone trailing `"` or an embedded `"""`
	// (or a stray backslash) is a SyntaxError that kills the whole package
	// import. Escaped quotes render identically in help()/getdoc.
	out = strings.ReplaceAll(out, `\`, `\\`)
	return strings.ReplaceAll(out, `"`, `\"`)
}

func enumNote(xs []any) string {
	if len(xs) == 0 {
		return ""
	}
	var parts []string
	for i, x := range xs {
		if i == 16 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, fmt.Sprint(x))
	}
	return strings.Join(parts, ", ")
}

func pyIdent(n string) string {
	s := PyName(n)
	if s == "args" {
		return "args_"
	}
	return s
}

func pyType(t any) string {
	switch v := t.(type) {
	case string:
		switch v {
		case "string":
			return "str"
		case "integer":
			return "int"
		case "number":
			return "float"
		case "boolean":
			return "bool"
		case "array":
			return "list"
		case "object":
			return "dict"
		}
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && s != "null" {
				return pyType(s)
			}
		}
	}
	return "object"
}
