package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"silo.agent/internal/builtin"
	"silo.agent/internal/builtin/calendar/caldavtest"
)

func setup(t *testing.T) (*caldavtest.Server, builtin.Config) {
	t.Helper()
	srv := caldavtest.Start(t)
	return srv, builtin.Resolve(connector{}.Descriptor(), srv.Config())
}

func call(t *testing.T, name string, cfg builtin.Config, args map[string]any) map[string]any {
	t.Helper()
	out, err := try(name, cfg, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

func try(name string, cfg builtin.Config, args map[string]any) (map[string]any, error) {
	tool, ok := connector{}.Descriptor().Tool(name)
	if !ok {
		return nil, errors.New("no tool " + name)
	}
	raw, _ := json.Marshal(args)
	v, err := tool.Run(context.Background(), nil, cfg, raw)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out, nil
}

func events(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, _ := out["events"].([]any)
	res := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		res = append(res, e.(map[string]any))
	}
	return res
}

func TestDescriptorRegisters(t *testing.T) {
	c, ok := builtin.Lookup("calendar")
	if !ok {
		t.Fatal("calendar not registered")
	}
	d := c.Descriptor()
	for _, name := range []string{"list_calendars", "list_events", "get_event", "free_busy", "create_event", "update_event", "delete_event"} {
		if _, ok := d.Tool(name); !ok {
			t.Errorf("missing tool %s", name)
		}
	}
}

func TestResolve(t *testing.T) {
	d := connector{}.Descriptor()
	s, err := resolve(builtin.Resolve(d, map[string]string{"provider": "icloud", "username": "a@b.c", "password": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if s.endpoint != "https://caldav.icloud.com/" || s.loc == nil {
		t.Fatalf("%+v", s)
	}
	if _, err := resolve(builtin.Resolve(d, map[string]string{"provider": "custom", "username": "a", "password": "x"})); err == nil ||
		!strings.Contains(err.Error(), "server URL") {
		t.Fatalf("custom without URL: %v", err)
	}
	if _, err := resolve(builtin.Resolve(d, map[string]string{"provider": "nextcloud", "username": "a", "password": "x"})); err == nil {
		t.Fatal("nextcloud without URL should fail")
	}
	if _, err := resolve(builtin.Resolve(d, map[string]string{"provider": "icloud", "username": "a", "password": "x", "timezone": "Mars/Base"})); err == nil ||
		!strings.Contains(err.Error(), "time zone") {
		t.Fatalf("bad zone: %v", err)
	}
	s, _ = resolve(builtin.Resolve(d, map[string]string{"provider": "custom", "url": "cal.example.org", "username": "a", "password": "x"}))
	if s.endpoint != "https://cal.example.org" {
		t.Fatalf("bare host gets https: %q", s.endpoint)
	}
}

func TestCheck(t *testing.T) {
	srv, cfg := setup(t)
	if err := (connector{}).Check(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	bad := srv.Config()
	bad["password"] = "nope"
	err := (connector{}).Check(context.Background(), builtin.Resolve(connector{}.Descriptor(), bad))
	if err == nil || !strings.Contains(err.Error(), "sign-in failed") || strings.Contains(err.Error(), "nope") {
		t.Fatalf("want sign-in failure, got %v", err)
	}
}

func TestWellKnownDiscovery(t *testing.T) {
	srv := caldavtest.Start(t)
	cfg := srv.Config()
	cfg["url"] = srv.URL + "/.well-known/caldav"
	out := call(t, "list_calendars", builtin.Resolve(connector{}.Descriptor(), cfg), nil)
	if len(out["calendars"].([]any)) != 2 {
		t.Fatalf("%v", out)
	}
}

func TestListCalendars(t *testing.T) {
	_, cfg := setup(t)
	out := call(t, "list_calendars", cfg, nil)
	cals := out["calendars"].([]any)
	if len(cals) != 2 {
		t.Fatalf("task-only calendars are hidden: %v", cals)
	}
	first := cals[0].(map[string]any)
	if first["name"] != "Personal" || first["path"] != caldavtest.Personal || first["description"] != "Home stuff" {
		t.Fatalf("%v", first)
	}
}

func TestListEventsRangeAllDayQuery(t *testing.T) {
	srv, cfg := setup(t)
	srv.Put(t, caldavtest.Personal, "a.ics", caldavtest.Event("a", "SUMMARY:Dentist", "LOCATION:Main St",
		"DTSTART;TZID=Europe/Warsaw:20261005T100000", "DTEND;TZID=Europe/Warsaw:20261005T110000"))
	srv.Put(t, caldavtest.Work, "b.ics", caldavtest.Event("b", "SUMMARY:Offsite",
		"DTSTART;VALUE=DATE:20261006", "DTEND;VALUE=DATE:20261008"))
	srv.Put(t, caldavtest.Work, "c.ics", caldavtest.Event("c", "SUMMARY:Too late",
		"DTSTART:20261020T090000Z", "DTEND:20261020T100000Z"))
	srv.Put(t, caldavtest.Work, "d.ics", caldavtest.Event("d", "SUMMARY:Standup",
		"DTSTART:20261005T070000Z", "DURATION:PT15M"))

	out := call(t, "list_events", cfg, map[string]any{"start": "2026-10-05", "end": "2026-10-12"})
	evs := events(t, out)
	if len(evs) != 3 {
		t.Fatalf("want 3 in range, got %v", evs)
	}
	// Sorted by start: Standup 09:00 local, Dentist 10:00, Offsite all-day 6th.
	if evs[0]["summary"] != "Standup" || evs[0]["start"] != "2026-10-05T09:00:00+02:00" || evs[0]["end"] != "2026-10-05T09:15:00+02:00" {
		t.Fatalf("standup: %v", evs[0])
	}
	if evs[1]["summary"] != "Dentist" || evs[1]["calendar"] != "Personal" || evs[1]["location"] != "Main St" ||
		evs[1]["href"] != caldavtest.Personal+"a.ics" {
		t.Fatalf("dentist: %v", evs[1])
	}
	if evs[2]["all_day"] != true || evs[2]["start"] != "2026-10-06" || evs[2]["end"] != "2026-10-07" {
		t.Fatalf("all-day end is the last day: %v", evs[2])
	}

	out = call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-10-31", "query": "dent"})
	if evs := events(t, out); len(evs) != 1 || evs[0]["uid"] != "a" {
		t.Fatalf("query: %v", evs)
	}
	out = call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-10-31", "calendar": "work", "limit": 1})
	if evs := events(t, out); len(evs) != 1 || out["truncated"] != true || evs[0]["uid"] != "d" {
		t.Fatalf("calendar by name + limit: %v", out)
	}
}

func TestRecurrenceExpansion(t *testing.T) {
	srv, cfg := setup(t)
	// Weekly on Mondays 09:00 Warsaw across the DST change (Oct 25, 2026),
	// one week excluded, one moved to 11:00 with a new title.
	raw := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:r1\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:Weekly\r\n" +
		"DTSTART;TZID=Europe/Warsaw:20261005T090000\r\nDTEND;TZID=Europe/Warsaw:20261005T093000\r\n" +
		"RRULE:FREQ=WEEKLY;COUNT=6\r\nEXDATE;TZID=Europe/Warsaw:20261012T090000\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:r1\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:Weekly (moved)\r\n" +
		"RECURRENCE-ID;TZID=Europe/Warsaw:20261019T090000\r\n" +
		"DTSTART;TZID=Europe/Warsaw:20261019T110000\r\nDTEND;TZID=Europe/Warsaw:20261019T113000\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	srv.Put(t, caldavtest.Personal, "r1.ics", raw)

	evs := events(t, call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-11-30"}))
	var got []string
	for _, e := range evs {
		got = append(got, e["start"].(string)+" "+e["summary"].(string))
		if e["recurring"] != true || e["recurrence_id"] == "" {
			t.Fatalf("instance flags: %v", e)
		}
	}
	want := []string{
		"2026-10-05T09:00:00+02:00 Weekly",
		"2026-10-19T11:00:00+02:00 Weekly (moved)",
		"2026-10-26T09:00:00+01:00 Weekly",
		"2026-11-02T09:00:00+01:00 Weekly",
		"2026-11-09T09:00:00+01:00 Weekly",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCreateGetUpdateDelete(t *testing.T) {
	srv, cfg := setup(t)
	out := call(t, "create_event", cfg, map[string]any{
		"calendar": "Work", "summary": "Planning", "start": "2026-10-07T14:00", "duration_minutes": 45,
		"location": "Room 1", "description": "Q4", "attendees": []string{"bob@example.org"}, "reminder_minutes": 10,
	})
	href, _ := out["href"].(string)
	if !strings.HasPrefix(href, caldavtest.Work) || !strings.HasSuffix(href, ".ics") || out["uid"] == "" {
		t.Fatalf("create: %v", out)
	}
	if srv.Object(href) == nil {
		t.Fatalf("not stored at %s: %v", href, srv.Paths())
	}

	ev := call(t, "get_event", cfg, map[string]any{"href": href})
	if ev["summary"] != "Planning" || ev["start"] != "2026-10-07T14:00:00+02:00" || ev["end"] != "2026-10-07T14:45:00+02:00" ||
		ev["location"] != "Room 1" || ev["description"] != "Q4" {
		t.Fatalf("get: %v", ev)
	}
	att := ev["attendees"].([]any)
	if len(att) != 1 || att[0].(map[string]any)["email"] != "bob@example.org" {
		t.Fatalf("attendees: %v", ev["attendees"])
	}
	if rem := ev["reminders"].([]any); len(rem) != 1 || rem[0].(float64) != 10 {
		t.Fatalf("reminders: %v", ev["reminders"])
	}

	call(t, "update_event", cfg, map[string]any{"href": href, "summary": "Planning v2", "start": "2026-10-07T15:00"})
	ev = call(t, "get_event", cfg, map[string]any{"href": href})
	if ev["summary"] != "Planning v2" || ev["start"] != "2026-10-07T15:00:00+02:00" || ev["end"] != "2026-10-07T15:45:00+02:00" ||
		ev["location"] != "Room 1" {
		t.Fatalf("update keeps duration and other fields: %v", ev)
	}

	call(t, "delete_event", cfg, map[string]any{"href": href})
	if srv.Object(href) != nil {
		t.Fatal("still there after delete")
	}
	if _, err := try("get_event", cfg, map[string]any{"href": href}); err == nil {
		t.Fatal("get after delete should fail")
	}
}

func TestCreateAllDayAndRecurring(t *testing.T) {
	srv, cfg := setup(t)
	out := call(t, "create_event", cfg, map[string]any{"summary": "Trip", "start": "2026-10-10", "end": "2026-10-12", "all_day": true})
	ev := call(t, "get_event", cfg, map[string]any{"href": out["href"]})
	if ev["all_day"] != true || ev["start"] != "2026-10-10" || ev["end"] != "2026-10-12" {
		t.Fatalf("all-day: %v", ev)
	}
	if !strings.HasPrefix(out["href"].(string), caldavtest.Personal) {
		t.Fatalf("default is the first event calendar: %v", out)
	}

	out = call(t, "create_event", cfg, map[string]any{"summary": "Gym", "start": "2026-10-05T18:00", "end": "2026-10-05T19:00",
		"rrule": "FREQ=WEEKLY;BYDAY=MO;COUNT=4"})
	obj := srv.Object(out["href"].(string))
	var hasTZ bool
	for _, c := range obj.Children {
		if c.Name == "VTIMEZONE" {
			hasTZ = true
		}
	}
	if !hasTZ {
		t.Fatal("a recurring event with a TZID ships its VTIMEZONE")
	}
	evs := events(t, call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-11-30", "query": "gym"}))
	if len(evs) != 4 || evs[3]["start"] != "2026-10-26T18:00:00+01:00" {
		t.Fatalf("recurring wall clock across DST: %v", evs)
	}

	if _, err := try("create_event", cfg, map[string]any{"summary": "x", "start": "2026-10-05T18:00", "rrule": "FREQ=SOMETIMES"}); err == nil {
		t.Fatal("bad rrule accepted")
	}
	if _, err := try("create_event", cfg, map[string]any{"summary": "x", "start": "2026-10-05T18:00", "end": "2026-10-05T17:00"}); err == nil {
		t.Fatal("end before start accepted")
	}
	if _, err := try("create_event", cfg, map[string]any{"summary": "x", "start": "2026-10-05", "calendar": "Reminders"}); err == nil {
		t.Fatal("task-only calendar accepted")
	}
}

func TestFreeBusy(t *testing.T) {
	srv, cfg := setup(t)
	srv.Put(t, caldavtest.Personal, "a.ics", caldavtest.Event("a", "SUMMARY:A",
		"DTSTART;TZID=Europe/Warsaw:20261005T100000", "DTEND;TZID=Europe/Warsaw:20261005T110000"))
	srv.Put(t, caldavtest.Work, "b.ics", caldavtest.Event("b", "SUMMARY:B",
		"DTSTART;TZID=Europe/Warsaw:20261005T103000", "DTEND;TZID=Europe/Warsaw:20261005T120000"))
	srv.Put(t, caldavtest.Work, "c.ics", caldavtest.Event("c", "SUMMARY:Free lunch", "TRANSP:TRANSPARENT",
		"DTSTART;TZID=Europe/Warsaw:20261005T130000", "DTEND;TZID=Europe/Warsaw:20261005T140000"))
	srv.Put(t, caldavtest.Work, "d.ics", caldavtest.Event("d", "SUMMARY:Cancelled", "STATUS:CANCELLED",
		"DTSTART;TZID=Europe/Warsaw:20261005T150000", "DTEND;TZID=Europe/Warsaw:20261005T160000"))
	srv.Put(t, caldavtest.Work, "e.ics", caldavtest.Event("e", "SUMMARY:Holiday",
		"DTSTART;VALUE=DATE:20261005", "DTEND;VALUE=DATE:20261006"))

	out := call(t, "free_busy", cfg, map[string]any{"start": "2026-10-05T09:00", "end": "2026-10-05T17:00", "min_minutes": 30})
	busy := out["busy"].([]any)
	if len(busy) != 1 {
		t.Fatalf("overlaps merge, transparent/cancelled/all-day skipped: %v", busy)
	}
	b := busy[0].(map[string]any)
	if b["start"] != "2026-10-05T10:00:00+02:00" || b["end"] != "2026-10-05T12:00:00+02:00" {
		t.Fatalf("%v", b)
	}
	free := out["free"].([]any)
	if len(free) != 2 || free[0].(map[string]any)["end"] != "2026-10-05T10:00:00+02:00" ||
		free[1].(map[string]any)["start"] != "2026-10-05T12:00:00+02:00" {
		t.Fatalf("free: %v", free)
	}

	out = call(t, "free_busy", cfg, map[string]any{"start": "2026-10-05", "end": "2026-10-07", "hours": "09:00-17:00"})
	free = out["free"].([]any)
	if len(free) != 3 || free[2].(map[string]any)["start"] != "2026-10-06T09:00:00+02:00" ||
		free[2].(map[string]any)["end"] != "2026-10-06T17:00:00+02:00" {
		t.Fatalf("hours clip free gaps per day: %v", free)
	}
}

func TestParseWhen(t *testing.T) {
	_, cfg := setup(t)
	s, _ := resolve(cfg)
	for in, want := range map[string]string{
		"2026-10-05T09:30":          "2026-10-05T09:30:00+02:00",
		"2026-10-05 09:30":          "2026-10-05T09:30:00+02:00",
		"2026-10-05T09:30:15":       "2026-10-05T09:30:15+02:00",
		"2026-10-05T07:30:00Z":      "2026-10-05T09:30:00+02:00",
		"2026-10-05T09:30:00-04:00": "2026-10-05T15:30:00+02:00",
	} {
		tm, date, err := parseWhen(in, s.loc)
		if err != nil || date || tm.In(s.loc).Format("2006-01-02T15:04:05Z07:00") != want {
			t.Errorf("%s: %v %v %v", in, tm, date, err)
		}
	}
	if _, date, err := parseWhen("2026-10-05", s.loc); err != nil || !date {
		t.Fatalf("date: %v %v", date, err)
	}
	if _, _, err := parseWhen("next tuesday", s.loc); err == nil {
		t.Fatal("prose accepted")
	}
}

// TestPartitionedHomeSet is iCloud's shape: calendars list on the front
// host, but events only come back from the home set's own host.
func TestPartitionedHomeSet(t *testing.T) {
	srv := caldavtest.StartPartitioned(t)
	cfg := builtin.Resolve(connector{}.Descriptor(), srv.Config())
	srv.Put(t, caldavtest.Work, "p.ics", caldavtest.Event("p", "SUMMARY:On the partition",
		"DTSTART:20261005T080000Z", "DTEND:20261005T090000Z"))
	evs := events(t, call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-10-31"}))
	if len(evs) != 1 || evs[0]["summary"] != "On the partition" {
		t.Fatalf("events must be read from the home set's host: %v", evs)
	}
	out := call(t, "create_event", cfg, map[string]any{"summary": "New", "start": "2026-10-06T10:00"})
	if srv.Object(out["href"].(string)) == nil {
		t.Fatalf("create lands on the partition: %v", out)
	}
}

// TestUnreadableRuleFallsBack keeps an event with a rule rrule-go cannot
// read: it lists as its first occurrence instead of vanishing.
func TestUnreadableRuleFallsBack(t *testing.T) {
	srv, cfg := setup(t)
	srv.Put(t, caldavtest.Personal, "ok.ics", caldavtest.Event("ok", "SUMMARY:Fine",
		"DTSTART:20261005T080000Z", "DTEND:20261005T090000Z"))
	srv.Put(t, caldavtest.Personal, "bad.ics", caldavtest.Event("bad", "SUMMARY:Weird rule", "RRULE:FREQ=FORTNIGHTLY",
		"DTSTART:20261005T100000Z", "DTEND:20261005T110000Z"))
	out := call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-10-31"})
	evs := events(t, out)
	if len(evs) != 2 || evs[1]["summary"] != "Weird rule" || evs[1]["recurring"] == true {
		t.Fatalf("an unreadable rule falls back to the single event: %v", evs)
	}
}

// TestDiscoveryIsCached: back-to-back tool calls reuse the account's
// discovery instead of repeating principal → home set → calendars.
func TestDiscoveryIsCached(t *testing.T) {
	srv, cfg := setup(t)
	call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-10-31"})
	before := srv.Requests()
	call(t, "list_events", cfg, map[string]any{"start": "2026-10-01", "end": "2026-10-31"})
	if got := srv.Requests() - before; got != 2 {
		t.Fatalf("second call made %d requests, want one REPORT per event calendar (2)", got)
	}
}
