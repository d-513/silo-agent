package drives

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Values are the answers for one drive, split by kind. Maps may be nil.
type Values struct {
	User    map[string]string
	System  map[string]string
	Dynamic map[string]string
}

func (v Values) raw(kind, key string) string {
	switch kind {
	case KindUser:
		return v.User[key]
	case KindSystem:
		return v.System[key]
	case KindDynamic:
		return v.Dynamic[key]
	}
	return ""
}

// Value is a var's effective value: its stored answer, else its default.
func (t *Template) Value(v Values, kind, key string) string {
	if s := v.raw(kind, key); s != "" {
		return s
	}
	if d, ok := t.Var(kind, key); ok {
		return d.Default
	}
	return ""
}

// Visible is false when a var's visible_if does not hold; a hidden var counts
// as empty and never as missing.
func (t *Template) Visible(d Var, v Values) bool {
	for k, want := range d.VisibleIf {
		if t.Value(v, KindUser, k) != want {
			return false
		}
	}
	return true
}

// Ref names one var inside an expression: {{kind.key|filter|filter}}.
type Ref struct {
	Kind    string
	Key     string
	Filters []string
}

var refRe = regexp.MustCompile(`\{\{\s*([a-z]+)\.([a-z0-9_]+)((?:\s*\|\s*[a-z]+)*)\s*\}\}`)

// Filters usable in expressions. They exist so templates never need Go code
// for trivial shaping (a server URL with or without a trailing slash).
var filters = map[string]func(string) string{
	"trimslash": func(s string) string { return strings.TrimRight(s, "/") },
	"trimlead":  func(s string) string { return strings.TrimLeft(s, "/") },
	"pathescape": func(s string) string {
		return url.PathEscape(s)
	},
	"lower": strings.ToLower,
}

// Refs parses every reference in expr.
func Refs(expr string) []Ref {
	var out []Ref
	for _, m := range refRe.FindAllStringSubmatch(expr, -1) {
		r := Ref{Kind: m[1], Key: m[2]}
		for f := range strings.SplitSeq(m[3], "|") {
			if f = strings.TrimSpace(f); f != "" {
				r.Filters = append(r.Filters, f)
			}
		}
		out = append(out, r)
	}
	return out
}

// Missing is one required var with no value.
type Missing struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
}

// MissingError lists every required var that was empty. State maps it to what
// the human should do next.
type MissingError struct{ Missing []Missing }

func (e *MissingError) Error() string {
	parts := make([]string, 0, len(e.Missing))
	for _, m := range e.Missing {
		parts = append(parts, m.Kind+"."+m.Key)
	}
	return "missing " + strings.Join(parts, ", ")
}

// Drive states derived from what is missing, in priority order: an admin must
// act before the owner can connect, and connecting comes before filling fields.
const (
	StateNeedsSetup = "needs_setup"
	StateNeedsAuth  = "needs_auth"
	StateNeedsInput = "needs_input"
)

func (e *MissingError) State() string {
	has := map[string]bool{}
	for _, m := range e.Missing {
		has[m.Kind] = true
	}
	switch {
	case has[KindSystem]:
		return StateNeedsSetup
	case has[KindDynamic]:
		return StateNeedsAuth
	default:
		return StateNeedsInput
	}
}

// AsMissing unwraps a MissingError.
func AsMissing(err error) (*MissingError, bool) {
	var m *MissingError
	ok := errors.As(err, &m)
	return m, ok
}

// Check reports every required, visible var that has no value. An oauth2
// template always requires its token.
func (t *Template) Check(v Values) error {
	var miss []Missing
	for _, d := range t.Vars {
		if !t.Visible(d, v) {
			continue
		}
		need := d.Required || (d.Kind == KindDynamic && d.Source == SourceOAuth)
		if need && t.Value(v, d.Kind, d.Key) == "" {
			miss = append(miss, Missing{Kind: d.Kind, Key: d.Key})
		}
	}
	if len(miss) > 0 {
		return &MissingError{Missing: miss}
	}
	return nil
}

// Expand substitutes every reference in expr. Unknown filters and vars are a
// template bug (the registry rejects them at load), so they expand to "".
func (t *Template) Expand(expr string, v Values) string {
	return refRe.ReplaceAllStringFunc(expr, func(m string) string {
		r := Refs(m)[0]
		d, ok := t.Var(r.Kind, r.Key)
		if !ok || !t.Visible(d, v) {
			return ""
		}
		s := t.Value(v, r.Kind, r.Key)
		for _, f := range r.Filters {
			if fn := filters[f]; fn != nil {
				s = fn(s)
			}
		}
		if d.Obscure && s != "" {
			if o, err := obscure(s); err == nil {
				s = o
			}
		}
		return s
	})
}

// obscure is swapped in tests: real obscuring uses a random IV.
var obscure = Obscure

// Remote is what the sidecar runs for one drive.
type Remote struct {
	Name  string            // rclone remote name
	Env   map[string]string // RCLONE_CONFIG_<NAME>_<OPTION>
	Path  string            // after the colon: <name>:<path>
	Flags []string
}

var remoteRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

// RemoteName maps a drive name to an rclone remote name. rclone reads a
// remote's env vars by upper-casing its name, so only [A-Za-z0-9_] survive.
func RemoteName(name string) string {
	s := strings.ToLower(remoteRe.ReplaceAllString(name, "_"))
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "d_" + s
	}
	return s
}

// EnvKey is rclone's environment variable for one remote option.
func EnvKey(remote, option string) string {
	return "RCLONE_CONFIG_" + strings.ToUpper(remote) + "_" + strings.ToUpper(strings.ReplaceAll(option, "-", "_"))
}

// Render builds the rclone environment for one drive. It fails with a
// MissingError when a required value is empty; options that render empty are
// left out so rclone keeps its own default.
func (t *Template) Render(v Values, name string) (Remote, error) {
	if err := t.Check(v); err != nil {
		return Remote{}, err
	}
	remote := RemoteName(name)
	env := map[string]string{EnvKey(remote, "type"): t.RcloneType}
	for opt, expr := range t.Rclone {
		if s := t.Expand(expr, v); s != "" {
			env[EnvKey(remote, opt)] = s
		}
	}
	r := Remote{Name: remote, Env: env, Path: strings.Trim(t.Expand(t.Mount.Path, v), "/")}
	for _, f := range t.Mount.Flags {
		if s := t.Expand(f, v); s != "" {
			r.Flags = append(r.Flags, s)
		}
	}
	return r, nil
}

// EnvList is Env as sorted KEY=VALUE pairs.
func (r Remote) EnvList() []string {
	out := make([]string, 0, len(r.Env))
	for k, val := range r.Env {
		out = append(out, k+"="+val)
	}
	sort.Strings(out)
	return out
}

func (r Remote) String() string { return fmt.Sprintf("%s:%s", r.Name, r.Path) }
