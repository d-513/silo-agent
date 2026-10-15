package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/v2"
)

const (
	// DefaultAuditDetailDays is how long an audit line is kept when the
	// operator names no period.
	DefaultAuditDetailDays = 7
	// DefaultAuditTrafficDays is how long a per-minute traffic summary is kept.
	DefaultAuditTrafficDays = 30
	// auditDaysMax bounds both periods: keeping detail forever is a leak, not
	// a policy.
	auditDaysMax = 3650
)

// AuditDetailDays is how long an audit line is kept.
func (c Config) AuditDetailDays() int {
	return auditDays(c.Audit.DetailDays, DefaultAuditDetailDays)
}

// AuditTrafficDays is how long a per-minute traffic summary is kept.
func (c Config) AuditTrafficDays() int {
	return auditDays(c.Audit.TrafficDays, DefaultAuditTrafficDays)
}

func auditDays(v, def int) int {
	if v <= 0 {
		return def
	}
	if v > auditDaysMax {
		return auditDaysMax
	}
	return v
}

// validateAudit checks the audit.* keys of a YAML document being written.
func validateAudit(k *koanf.Koanf) error {
	known := map[string]bool{"audit.detail_days": true, "audit.traffic_days": true}
	for _, key := range k.Keys() {
		if strings.HasPrefix(key, "audit.") && !known[key] {
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	for _, key := range []string{"audit.detail_days", "audit.traffic_days"} {
		switch v := k.Get(key).(type) {
		case nil, int, int64, float64:
		case string:
			if strings.TrimSpace(v) != "" && !isDigits(v) {
				return fmt.Errorf("%s must be a number of days", key)
			}
		default:
			return fmt.Errorf("%s must be a number of days", key)
		}
	}
	return nil
}

func isDigits(s string) bool {
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return false
		}
	}
	return strings.TrimSpace(s) != ""
}
