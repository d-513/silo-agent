package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knadh/koanf/v2"
)

// Changes configures workspace change tracking: the snapshots a Bot's worker
// takes around every run, shown as diffs in the Changes pane.
type Changes struct {
	// Enabled turns the snapshots and the pane on. History already taken stays
	// on the Bot's disk either way.
	Enabled bool `koanf:"enabled"`
	// MaxFileMB is the size past which a file is recorded by size only, never
	// by content.
	MaxFileMB int `koanf:"max_file_mb"`
	// KeepDays is how long a snapshot is kept.
	KeepDays int `koanf:"keep_days"`
}

const (
	// DefaultChangesFileMB and ChangesMaxFileMB bound changes.max_file_mb.
	DefaultChangesFileMB = 8
	ChangesMaxFileMB     = 100
	// DefaultChangesKeepDays and ChangesMaxKeepDays bound changes.keep_days.
	DefaultChangesKeepDays = 30
	ChangesMaxKeepDays     = 365
)

// MaxFileBytes is the per-file content cap in bytes.
func (c Changes) MaxFileBytes() int64 {
	mb := c.MaxFileMB
	if mb < 1 {
		mb = DefaultChangesFileMB
	}
	return int64(min(mb, ChangesMaxFileMB)) << 20
}

// Days is how many days of snapshots are kept.
func (c Changes) Days() int {
	if c.KeepDays < 1 {
		return DefaultChangesKeepDays
	}
	return min(c.KeepDays, ChangesMaxKeepDays)
}

// validateChanges checks the changes.* keys of a YAML document being written.
func validateChanges(k *koanf.Koanf) error {
	for key, max := range map[string]int{"changes.max_file_mb": ChangesMaxFileMB, "changes.keep_days": ChangesMaxKeepDays} {
		if raw := strings.TrimSpace(k.String(key)); raw != "" {
			if n, err := strconv.Atoi(raw); err != nil || n < 1 || n > max {
				return fmt.Errorf("%s must be a whole number from 1 to %d", key, max)
			}
		}
	}
	return nil
}
