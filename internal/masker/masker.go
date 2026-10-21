package masker

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"sync"
)

const minLen = 8

type Masker struct {
	mu   sync.Mutex
	vals map[string]struct{}
	// rep replaces every value in one pass, longest first, so a secret that
	// contains another is masked whole and the output is the same each time
	// (history replay depends on that). Nil until built after a change.
	rep *strings.Replacer
}

func New() *Masker { return &Masker{vals: map[string]struct{}{}} }

func (m *Masker) Add(v string) {
	if len(v) < minLen {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addLocked(v)
}

func (m *Masker) AddMany(vs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range vs {
		if len(v) >= minLen {
			m.addLocked(v)
		}
	}
}

func (m *Masker) addLocked(v string) {
	m.rep = nil
	m.vals[v] = struct{}{}
	m.vals[base64.StdEncoding.EncodeToString([]byte(v))] = struct{}{}
	m.vals[url.QueryEscape(v)] = struct{}{}
	m.vals[hex.EncodeToString([]byte(v))] = struct{}{}
	esc, _ := strings.CutPrefix(toJSONString(v), `"`)
	esc, _ = strings.CutSuffix(esc, `"`)
	if esc != v && len(esc) >= minLen {
		m.vals[esc] = struct{}{}
	}
}

func toJSONString(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (m *Masker) Apply(s string) string {
	m.mu.Lock()
	if m.rep == nil {
		vals := make([]string, 0, len(m.vals))
		for v := range m.vals {
			if v != "" {
				vals = append(vals, v)
			}
		}
		sort.Slice(vals, func(i, j int) bool {
			if len(vals[i]) != len(vals[j]) {
				return len(vals[i]) > len(vals[j])
			}
			return vals[i] < vals[j]
		})
		pairs := make([]string, 0, 2*len(vals))
		for _, v := range vals {
			pairs = append(pairs, v, "***")
		}
		m.rep = strings.NewReplacer(pairs...)
	}
	rep := m.rep
	m.mu.Unlock()
	return rep.Replace(s)
}
