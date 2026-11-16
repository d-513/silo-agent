package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knadh/koanf/v2"
)

// BotLimits caps what one Bot's box may take from the host, so a runaway
// install or a fork bomb in one Bot does not starve the others and the
// control plane. Zero is no limit.
type BotLimits struct {
	// MemoryMB is the box's memory in MB, Chromium and everything the Bot
	// runs included.
	MemoryMB int `koanf:"memory_mb"`
	// Pids is how many processes and threads the box may hold at once.
	Pids int `koanf:"pids"`
}

const (
	// DefaultBotPids leaves room for Chromium, a dev server and a build, and
	// stops a fork bomb.
	DefaultBotPids = 4096
	// BotMemoryMinMB is the least memory a box can be given: the desktop and
	// Chromium need about this much to start.
	BotMemoryMinMB = 512
)

// MemoryBytes is the box's memory limit in bytes, 0 for none.
func (l BotLimits) MemoryBytes() int64 {
	if l.MemoryMB <= 0 {
		return 0
	}
	return int64(l.MemoryMB) << 20
}

// PidsLimit is the box's process limit, 0 for none.
func (l BotLimits) PidsLimit() int64 {
	return int64(max(l.Pids, 0))
}

func validateBotLimits(k *koanf.Koanf) error {
	if raw := strings.TrimSpace(k.String("bot_limits.memory_mb")); raw != "" {
		if n, err := strconv.Atoi(raw); err != nil || n < 0 || (n > 0 && n < BotMemoryMinMB) {
			return fmt.Errorf("bot_limits.memory_mb must be 0 (no limit) or a whole number of at least %d", BotMemoryMinMB)
		}
	}
	if raw := strings.TrimSpace(k.String("bot_limits.pids")); raw != "" {
		if n, err := strconv.Atoi(raw); err != nil || n < 0 || (n > 0 && n < 256) {
			return fmt.Errorf("bot_limits.pids must be 0 (no limit) or a whole number of at least 256")
		}
	}
	return nil
}
