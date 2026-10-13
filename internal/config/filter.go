package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knadh/koanf/v2"
)

// Filter configures the output filter: the model check a tool result goes
// through before a Bot reads it, for actions whose output rule is Check.
type Filter struct {
	// Enabled turns output rules on. Off, every result passes untouched,
	// whatever a Bot's rules say: the operator's switch for the model calls.
	Enabled bool `koanf:"enabled"`
	// MaxChars is the longest result the filter reads. A longer one is
	// passed on marked as unchecked.
	MaxChars int `koanf:"max_chars"`
}

const (
	// DefaultFilterMaxChars, FilterMinChars and FilterMaxChars bound
	// filter.max_chars.
	DefaultFilterMaxChars = 48000
	FilterMinChars        = 1000
	FilterMaxChars        = 400000
)

// Chars is the longest result the filter reads, in characters.
func (f Filter) Chars() int {
	if f.MaxChars < FilterMinChars {
		return DefaultFilterMaxChars
	}
	return min(f.MaxChars, FilterMaxChars)
}

// FilterModel returns the model that judges tool results, falling back to the
// auto-approval model. Like it, it may be of either kind.
func (c Config) FilterModel() string {
	if m := strings.TrimSpace(c.ModelFilter); m != "" {
		return m
	}
	return c.ApprovalModel()
}

// validateFilter checks the filter.* keys of a YAML document being written.
func validateFilter(k *koanf.Koanf) error {
	if raw := strings.TrimSpace(k.String("filter.max_chars")); raw != "" {
		if n, err := strconv.Atoi(raw); err != nil || n < FilterMinChars || n > FilterMaxChars {
			return fmt.Errorf("filter.max_chars must be a whole number from %d to %d", FilterMinChars, FilterMaxChars)
		}
	}
	return nil
}
