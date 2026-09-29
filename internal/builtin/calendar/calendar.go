// Package calendar is the built-in Calendar connector: CalDAV (iCloud,
// Fastmail, Nextcloud, Radicale, …) with an app password. The password stays
// on the Control Plane; the Bot only sees `tools.<slug>` functions.
package calendar

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"silo.agent/internal/builtin"
	"silo.agent/internal/security"
)

//go:embed GUIDE.md
var guide string

const (
	// maxRange bounds one list/free-busy window; maxInstances bounds how many
	// occurrences one recurring event may expand to.
	maxRange     = 366 * 24 * time.Hour
	maxInstances = 1000
	maxLimit     = 250
	defLimit     = 50
)

// providers maps a preset to its CalDAV endpoint; "" means the human must
// give the URL.
var providers = map[string]string{
	"icloud":    "https://caldav.icloud.com/",
	"fastmail":  "https://caldav.fastmail.com/dav/",
	"yahoo":     "https://caldav.calendar.yahoo.com/",
	"nextcloud": "",
	"custom":    "",
}

type connector struct{}

func init() { builtin.Register(connector{}) }

func (connector) Descriptor() builtin.Descriptor {
	cal := builtin.Prop("string", "Calendar name or path from list_calendars.")
	href := builtin.Prop("string", "Event href from list_events.")
	when := "RFC 3339, YYYY-MM-DDTHH:MM (the calendar's time zone), or YYYY-MM-DD (all-day)."
	eventProps := func() map[string]any {
		return map[string]any{
			"summary":          builtin.Prop("string", "Title."),
			"start":            builtin.Prop("string", "Start: "+when),
			"end":              builtin.Prop("string", "End (exclusive for timed events; the last day for all-day ones)."),
			"duration_minutes": builtin.Prop("integer", "Length when end is omitted (default 60)."),
			"all_day":          builtin.Prop("boolean", "An all-day event (a date-only start implies it)."),
			"location":         builtin.Prop("string", "Location."),
			"description":      builtin.Prop("string", "Notes."),
			"attendees":        builtin.List("string", "Attendee email addresses. Whether they get an invite depends on the server."),
			"rrule":            builtin.Prop("string", "Recurrence rule, e.g. FREQ=WEEKLY;BYDAY=MO,WE;COUNT=10."),
			"reminder_minutes": builtin.Prop("integer", "Alert this many minutes before (0 = none)."),
		}
	}
	create := eventProps()
	create["calendar"] = cal
	update := eventProps()
	update["href"] = href
	return builtin.Descriptor{
		Key:         "calendar",
		Name:        "Calendar",
		Description: "Read and manage events on any CalDAV calendar: iCloud, Fastmail, Nextcloud, and more.",
		Category:    "Productivity",
		Guide:       guide,
		Prompt: "Call by bare name: `tools.{slug}.list_events(start=\"2026-10-01\", end=\"2026-11-01\")` → `{events, total}`; " +
			"window ≤ 1 year (default next 7 days); omit `calendar` to read all. Events are keyed by `href`; " +
			"naive times are in the calendar's zone; an all-day `end` is its last day. Check `free_busy` before proposing a slot.",
		Fields: []builtin.Field{
			{Key: "provider", Label: "Provider", Type: builtin.FieldSelect, Default: "icloud",
				Description: "Fills in the server. Pick Other for any CalDAV server.",
				Options: []builtin.Option{
					{Value: "icloud", Label: "iCloud"}, {Value: "fastmail", Label: "Fastmail"},
					{Value: "nextcloud", Label: "Nextcloud"}, {Value: "yahoo", Label: "Yahoo"},
					{Value: "custom", Label: "Other"},
				}},
			{Key: "url", Label: "Server URL", Type: builtin.FieldText,
				Description: "Needed for Nextcloud (https://your.host/remote.php/dav) and Other. Leave blank for the provider's."},
			{Key: "username", Label: "Username", Type: builtin.FieldText, Required: true,
				Description: "Usually your email address (your Apple ID for iCloud)."},
			{Key: "password", Label: "App password", Type: builtin.FieldSecret, Required: true,
				Description: "An app password, not your account password. See the guide for your provider."},
			{Key: "timezone", Label: "Time zone", Type: builtin.FieldText,
				Description: "IANA name such as Europe/Warsaw. Leave blank for the server's."},
			{Key: "default_calendar", Label: "Default calendar", Advanced: true, Type: builtin.FieldText,
				Description: "Where new events go when none is named. Leave blank for the first one."},
		},
		Tools: []builtin.Tool{
			{Name: "list_calendars", Mode: security.Allow,
				Description: "List event calendars (name, path, description) and the time zone times are shown in.",
				Params:      builtin.Object(nil), Run: listCalendars},
			{Name: "list_events", Mode: security.Allow,
				Description: "Events overlapping a window, recurring ones expanded, sorted by start. Returns href, uid, summary, start, end, all_day, location, calendar.",
				Params: builtin.Object(map[string]any{
					"calendar": builtin.Prop("string", "Calendar name or path; default all."),
					"start":    builtin.Prop("string", "Window start (default now): "+when),
					"end":      builtin.Prop("string", "Window end, exclusive (default start + 7 days)."),
					"query":    builtin.Prop("string", "Only events whose title, location, or notes contain this."),
					"limit":    builtin.Prop("integer", "Most events to return (default 50, max 250)."),
				}), Run: listEvents},
			{Name: "get_event", Mode: security.Allow,
				Description: "One event in full: notes, attendees, organizer, recurrence rule, reminders, moved occurrences.",
				Params:      builtin.Object(map[string]any{"href": href}, "href"), Run: getEvent},
			{Name: "free_busy", Mode: security.Allow,
				Description: "Busy intervals (merged across calendars) and free gaps in a window. Transparent, cancelled, and all-day events do not count as busy.",
				Params: builtin.Object(map[string]any{
					"start":       builtin.Prop("string", "Window start (default now): "+when),
					"end":         builtin.Prop("string", "Window end (default start + 7 days)."),
					"calendars":   builtin.List("string", "Calendar names or paths; default all."),
					"min_minutes": builtin.Prop("integer", "Shortest free gap to report (default 30)."),
					"hours":       builtin.Prop("string", "Only report free time inside these daily hours, e.g. 09:00-17:00."),
				}), Run: freeBusy},
			{Name: "create_event", Mode: security.Ask,
				Description: "Create an event. Returns its href and uid.",
				Params:      builtin.Object(create, "summary", "start"), Run: createEvent},
			{Name: "update_event", Mode: security.Ask,
				Description: "Change an event; omitted fields stay. Moving start keeps the length unless end is given. rrule \"\" removes recurrence. Applies to the whole series.",
				Params:      builtin.Object(update, "href"), Run: updateEvent},
			{Name: "delete_event", Mode: security.Ask,
				Description: "Delete an event (the whole series if it recurs).",
				Params:      builtin.Object(map[string]any{"href": href}, "href"), Run: deleteEvent},
		},
	}
}

func (connector) Check(ctx context.Context, cfg builtin.Config) error {
	s, err := resolve(cfg)
	if err != nil {
		return err
	}
	sess, err := open(ctx, s)
	if err != nil {
		return err
	}
	if len(sess.cals) == 0 {
		return errors.New("signed in, but found no event calendars")
	}
	return nil
}

// settings is a resolved config: provider defaults filled in.
type settings struct {
	endpoint   string
	username   string
	password   string
	loc        *time.Location
	defaultCal string
}

func resolve(cfg builtin.Config) (settings, error) {
	s := settings{
		username:   cfg.Get("username"),
		password:   cfg.Values["password"],
		defaultCal: cfg.Get("default_calendar"),
	}
	provider := cfg.Get("provider")
	def, known := providers[provider]
	if !known {
		return s, fmt.Errorf("unknown provider %q", provider)
	}
	s.endpoint = cfg.Get("url")
	if s.endpoint == "" {
		s.endpoint = def
	}
	if s.endpoint == "" {
		return s, errors.New("set the server URL")
	}
	if !strings.Contains(s.endpoint, "://") {
		s.endpoint = "https://" + s.endpoint
	}
	if s.username == "" {
		return s, errors.New("set the username")
	}
	loc, err := zone(cfg.Get("timezone"))
	if err != nil {
		return s, err
	}
	s.loc = loc
	return s, nil
}

// zone loads the configured IANA zone, or the host's. A named zone matters:
// events are written with its TZID so recurrences keep their wall-clock time.
func zone(name string) (*time.Location, error) {
	if name != "" {
		loc, err := time.LoadLocation(name)
		if err != nil {
			return nil, fmt.Errorf("unknown time zone %q (use an IANA name such as Europe/Warsaw)", name)
		}
		return loc, nil
	}
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc, nil
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			if loc, err := time.LoadLocation(link[i+len("zoneinfo/"):]); err == nil {
				return loc, nil
			}
		}
	}
	return time.UTC, nil
}
