package config

import (
	"strings"
	"testing"
)

func TestAuditRetentionDefaultsAndBounds(t *testing.T) {
	s, err := FromYAML([]byte("model: openai/gpt-x\nmodels: [openai/gpt-x]\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if c.AuditDetailDays() != DefaultAuditDetailDays || c.AuditTrafficDays() != DefaultAuditTrafficDays {
		t.Fatalf("audit defaults = %d/%d days", c.AuditDetailDays(), c.AuditTrafficDays())
	}
	// A nonsense period falls back to the default rather than keeping nothing
	// or everything.
	if got := (Config{Audit: Audit{DetailDays: -1}}).AuditDetailDays(); got != DefaultAuditDetailDays {
		t.Fatalf("negative = %d days", got)
	}
	if got := (Config{Audit: Audit{TrafficDays: 1 << 20}}).AuditTrafficDays(); got != auditDaysMax {
		t.Fatalf("huge = %d days", got)
	}
	if got := (Config{Audit: Audit{DetailDays: 3}}).AuditDetailDays(); got != 3 {
		t.Fatalf("named = %d days", got)
	}
}

func TestAuditDaysAreValidatedAndEditable(t *testing.T) {
	for _, k := range []string{"audit.detail_days", "audit.traffic_days"} {
		if !KnownKey(k) {
			t.Errorf("%s is not a known setting", k)
		}
	}
	for yaml, want := range map[string]string{
		"audit:\n  detail_days: 7\n  traffic_days: 30\n": "",
		"audit:\n  detail_days: lots\n":                  "audit.detail_days",
		"audit:\n  traffic_days: [1]\n":                  "audit.traffic_days",
		"audit:\n  keep_forever: true\n":                 "unknown setting",
	} {
		err := validateYAML([]byte(yaml))
		if (err == nil) != (want == "") || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("%q: err = %v, want %q", yaml, err, want)
		}
	}
}
