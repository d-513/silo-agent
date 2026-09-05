package masker

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"sync"
)

const minLen = 8

type Masker struct {
	mu   sync.Mutex
	vals map[string]struct{}
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
	defer m.mu.Unlock()
	out := s
	for v := range m.vals {
		if v == "" {
			continue
		}
		out = strings.ReplaceAll(out, v, "***")
	}
	return out
}
