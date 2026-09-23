package config

import (
	"regexp"
	"strings"
)

// NamedVar is one operator-defined connector variable.
type NamedVar struct {
	Name  string
	Value string
}

var varNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidVarName reports whether name can be a connector variable.
func ValidVarName(name string) bool { return varNameRE.MatchString(name) }

var varRefRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandVars replaces ${NAME} references in s with values from vars. A name is
// matched exactly first, then case-insensitively, so a variable supplied
// through the environment (whose key koanf lowercases) still resolves a
// reference written in caps. Unknown names are left untouched.
func ExpandVars(s string, vars map[string]string) string {
	if s == "" || len(vars) == 0 || !strings.Contains(s, "${") {
		return s
	}
	var lower map[string]string
	lookup := func(name string) (string, bool) {
		if v, ok := vars[name]; ok {
			return v, true
		}
		if lower == nil {
			lower = make(map[string]string, len(vars))
			for k, v := range vars {
				lk := strings.ToLower(k)
				if _, dup := lower[lk]; !dup {
					lower[lk] = v
				}
			}
		}
		v, ok := lower[strings.ToLower(name)]
		return v, ok
	}
	return varRefRE.ReplaceAllStringFunc(s, func(ref string) string {
		m := varRefRE.FindStringSubmatch(ref)
		if v, ok := lookup(m[1]); ok {
			return v
		}
		return ref
	})
}
