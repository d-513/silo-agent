// Package zone is the one place a stored time zone name becomes a location and
// a time becomes text for the model. A Bot's zone is an IANA name, and what the
// model reads is written in it rather than in the zone of the machine the
// Control Plane happens to run on.
package zone

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	// The zone database rides in the binary, so a Control Plane image without
	// /usr/share/zoneinfo still resolves every name.
	_ "time/tzdata"
)

// Resolve is the named IANA zone, or the host's when name is empty or unknown.
// It never answers "Local": the result always has a name that can be shown,
// stored and loaded again.
func Resolve(name string) *time.Location {
	if Check(name) == nil {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return host()
}

// Check reports whether name is an IANA zone a Bot may be set to.
func Check(name string) error {
	if name == "" {
		return errors.New("time zone required")
	}
	// LoadLocation reads a file by this name: keep it inside the database.
	if name == "Local" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || strings.TrimSpace(name) != name {
		return unknown(name)
	}
	if _, err := time.LoadLocation(name); err != nil {
		return unknown(name)
	}
	return nil
}

func unknown(name string) error {
	return fmt.Errorf("unknown time zone %q (use an IANA name such as Europe/Warsaw)", name)
}

// host names the zone of the machine this runs on: TZ, then where
// /etc/localtime points, then UTC.
func host() *time.Location {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); Check(tz) == nil {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			if name := link[i+len("zoneinfo/"):]; Check(name) == nil {
				if loc, err := time.LoadLocation(name); err == nil {
					return loc
				}
			}
		}
	}
	return time.UTC
}

// Stamp writes t for the model as wall-clock time in loc with its offset and
// the zone's name: "Sun, 2026-10-11 14:30:05 +02:00 Europe/Warsaw". The offset
// makes it unambiguous; the name says where the human is.
func Stamp(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon, 2006-01-02 15:04:05 -07:00") + " " + loc.String()
}
