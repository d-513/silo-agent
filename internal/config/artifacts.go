package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knadh/koanf/v2"
)

// Artifacts bounds the artifact store each Bot keeps on its machine: every
// version of what it handed the human, pinned as it was.
type Artifacts struct {
	// MaxMB is the store's budget per Bot; past it the oldest versions go.
	MaxMB int `koanf:"max_mb"`
}

const (
	// DefaultArtifactsMB and ArtifactsMaxMB bound artifacts.max_mb.
	DefaultArtifactsMB = 2048
	ArtifactsMaxMB     = 102400
)

// MaxBytes is the per-Bot store budget in bytes.
func (a Artifacts) MaxBytes() int64 {
	mb := a.MaxMB
	if mb < 1 {
		mb = DefaultArtifactsMB
	}
	return int64(min(mb, ArtifactsMaxMB)) << 20
}

func validateArtifacts(k *koanf.Koanf) error {
	if raw := strings.TrimSpace(k.String("artifacts.max_mb")); raw != "" {
		if n, err := strconv.Atoi(raw); err != nil || n < 1 || n > ArtifactsMaxMB {
			return fmt.Errorf("artifacts.max_mb must be a whole number from 1 to %d", ArtifactsMaxMB)
		}
	}
	return nil
}
