package search

import (
	"strings"
	"time"
)

// SourceStatus is one source of the Built-in Scraper as an operator sees it.
type SourceStatus struct {
	Name string
	// API marks a public API; the others are scraped pages.
	API bool
	// On reports whether searches ask it under the current settings; Off
	// says why not.
	On  bool
	Off string
	// State is "ok" (it answered last), "resting" (left alone after a
	// failure until Until), "failing" (its rest is over, the next search
	// tries it) or "" (not asked yet since the server started).
	State  string
	Reason string
	Until  time.Time
	// LastOK and LastFailed are when it last answered and last failed.
	LastOK, LastFailed time.Time
}

// Status is the health of every source of the Built-in Scraper under s. It is
// what this process has seen: empty after a restart.
func Status(s Settings) []SourceStatus {
	out := make([]SourceStatus, 0, len(sources))
	for _, src := range sources {
		st := SourceStatus{Name: src.name, API: src.api}
		st.On, st.Off = sourceOn(src, s, sources)
		h, resting := sharedHealth.look(src.name)
		st.LastOK, st.LastFailed = h.okAt, h.failedAt
		switch {
		case resting:
			st.State, st.Reason, st.Until = "resting", h.reason, h.until
		case h.fails > 0 || h.soft > 0:
			st.State, st.Reason = "failing", h.reason
		case !h.okAt.IsZero():
			st.State = "ok"
		}
		out = append(out, st)
	}
	return out
}

// Configured is the engine an operator picked and its settings, read with
// get ("search.<engine>.<key>"). An unknown or empty id is the default.
func Configured(engine string, get func(key string) string) (id string, s Settings) {
	id = ID(engine)
	if !Known(id) {
		id = DefaultEngine
	}
	d, _ := Lookup(id)
	s = Settings{}
	for _, f := range d.Settings {
		if v := strings.TrimSpace(get("search." + id + "." + f.Key)); v != "" {
			s[f.Key] = v
		}
	}
	return id, s
}
