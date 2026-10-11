package zone

import (
	"testing"
	"time"
)

func TestResolveNamed(t *testing.T) {
	if got := Resolve("Asia/Tokyo").String(); got != "Asia/Tokyo" {
		t.Fatalf("Resolve = %q", got)
	}
}

// An unset or unknown zone falls back to the host's, and always by a real
// name: "Local" can be neither shown nor stored.
func TestResolveFallsBackToTheHost(t *testing.T) {
	t.Setenv("TZ", "America/Lima")
	for _, name := range []string{"", "Not/AZone", "Local"} {
		if got := Resolve(name).String(); got != "America/Lima" {
			t.Fatalf("Resolve(%q) = %q, want the host's zone", name, got)
		}
	}
	t.Setenv("TZ", ":Europe/Warsaw")
	if got := Resolve("").String(); got != "Europe/Warsaw" {
		t.Fatalf("Resolve with a colon-led TZ = %q", got)
	}
}

func TestResolveNeverAnswersLocal(t *testing.T) {
	t.Setenv("TZ", "")
	if got := Resolve("").String(); got == "Local" || got == "" {
		t.Fatalf("Resolve = %q", got)
	}
}

func TestCheck(t *testing.T) {
	for _, ok := range []string{"UTC", "Europe/Warsaw", "America/Argentina/Buenos_Aires", "Etc/GMT+3"} {
		if err := Check(ok); err != nil {
			t.Errorf("Check(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Local", "Warsaw", "../etc/passwd", "/etc/localtime", "Europe/Warsaw ", "Europe/../UTC"} {
		if err := Check(bad); err == nil {
			t.Errorf("Check(%q) passed", bad)
		}
	}
}

func TestStamp(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 30, 5, 0, time.UTC)
	for name, want := range map[string]string{
		"Europe/Warsaw": "Sun, 2026-10-11 14:30:05 +02:00 Europe/Warsaw",
		"Asia/Kolkata":  "Sun, 2026-10-11 18:00:05 +05:30 Asia/Kolkata",
		"UTC":           "Sun, 2026-10-11 12:30:05 +00:00 UTC",
	} {
		if got := Stamp(at, Resolve(name)); got != want {
			t.Errorf("Stamp in %s = %q, want %q", name, got, want)
		}
	}
	// The offset is the one in force at that moment, not today's.
	winter := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if got, want := Stamp(winter, Resolve("Europe/Warsaw")), "Sun, 2026-10-25 13:00:00 +01:00 Europe/Warsaw"; got != want {
		t.Errorf("Stamp after the clocks go back = %q, want %q", got, want)
	}
}
