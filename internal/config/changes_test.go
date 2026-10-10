package config

import (
	"strings"
	"testing"
)

func TestChangesDefaultsAndBounds(t *testing.T) {
	var c Changes
	if c.MaxFileBytes() != DefaultChangesFileMB<<20 || c.Days() != DefaultChangesKeepDays {
		t.Fatalf("zero value = %d bytes, %d days", c.MaxFileBytes(), c.Days())
	}
	c = Changes{MaxFileMB: 100000, KeepDays: 100000}
	if c.MaxFileBytes() != ChangesMaxFileMB<<20 || c.Days() != ChangesMaxKeepDays {
		t.Fatalf("over the bounds = %d bytes, %d days", c.MaxFileBytes(), c.Days())
	}
	c = Changes{MaxFileMB: 2, KeepDays: 7}
	if c.MaxFileBytes() != 2<<20 || c.Days() != 7 {
		t.Fatalf("set = %d bytes, %d days", c.MaxFileBytes(), c.Days())
	}
}

func TestChangesYAMLIsValidated(t *testing.T) {
	for raw, want := range map[string]string{
		"changes:\n  max_file_mb: 16\n  keep_days: 90\n": "",
		"changes:\n  max_file_mb: 0\n":                   "changes.max_file_mb",
		"changes:\n  max_file_mb: lots\n":                "changes.max_file_mb",
		"changes:\n  keep_days: 4000\n":                  "changes.keep_days",
	} {
		err := validateYAML([]byte(raw))
		if want == "" && err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Fatalf("%q: err = %v, want one naming %s", raw, err, want)
		}
	}
}

func TestChangesAreOnByDefault(t *testing.T) {
	s, err := FromYAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Config().Changes; !c.Enabled || c.MaxFileMB != DefaultChangesFileMB || c.KeepDays != DefaultChangesKeepDays {
		t.Fatalf("defaults = %+v", c)
	}
}
