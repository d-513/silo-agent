package calendar

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

const (
	dateFmt     = "2006-01-02"
	icalDate    = "20060102"
	icalLocal   = "20060102T150405"
	icalUTC     = "20060102T150405Z"
	outTimeFmt  = time.RFC3339
	wallFmt     = "20060102T150405"
	productID   = "-//Silo//Calendar//EN"
	statusCxl   = "CANCELLED"
	transparent = "TRANSPARENT"
)

// parseWhen reads a tool time: RFC 3339, a naive date-time in loc, or a date
// (dateOnly, midnight in loc).
func parseWhen(s string, loc *time.Location) (t time.Time, dateOnly bool, err error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), false, nil
	}
	for _, f := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation(dateFmt, s, loc); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf("time %q: use RFC 3339, YYYY-MM-DDTHH:MM, or YYYY-MM-DD", s)
}

// window parses a start/end pair with defaults (now, +7 days) and bounds.
func window(start, end string, loc *time.Location) (time.Time, time.Time, error) {
	from := time.Now().In(loc)
	if start != "" {
		t, _, err := parseWhen(start, loc)
		if err != nil {
			return from, from, err
		}
		from = t
	}
	to := from.AddDate(0, 0, 7)
	if end != "" {
		t, _, err := parseWhen(end, loc)
		if err != nil {
			return from, to, err
		}
		to = t
	}
	if !to.After(from) {
		return from, to, fmt.Errorf("end must be after start")
	}
	if to.Sub(from) > maxRange {
		return from, to, fmt.Errorf("window is over a year; ask for a shorter one")
	}
	return from, to, nil
}

// dt parses a DTSTART-like property. An unknown TZID (a Windows zone name
// from Outlook, say) falls back to loc rather than failing the whole event.
func dt(p *ical.Prop, loc *time.Location) (t time.Time, dateOnly bool, err error) {
	v := strings.TrimSpace(p.Value)
	if p.ValueType() == ical.ValueDate || len(v) == len(icalDate) {
		t, err = time.ParseInLocation(icalDate, v, loc)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.Parse(icalUTC, v)
		return t, false, err
	}
	zone := loc
	if tzid := strings.TrimPrefix(p.Params.Get(ical.PropTimezoneID), "/"); tzid != "" {
		if z, err := time.LoadLocation(tzid); err == nil {
			zone = z
		}
	}
	t, err = time.ParseInLocation(icalLocal, v, zone)
	return t, false, err
}

// dtList parses a (possibly comma-separated) EXDATE/RDATE list.
func dtList(props []ical.Prop, loc *time.Location) []time.Time {
	var out []time.Time
	for _, p := range props {
		for _, v := range strings.Split(p.Value, ",") {
			q := p
			q.Value = v
			if t, _, err := dt(&q, loc); err == nil {
				out = append(out, t)
			}
		}
	}
	return out
}

func text(c *ical.Component, name string) string {
	if p := c.Props.Get(name); p != nil {
		if s, err := p.Text(); err == nil {
			return s
		}
		return p.Value
	}
	return ""
}

// span is an event component's start, exclusive end, and all-day flag.
func span(c *ical.Component, loc *time.Location) (start, end time.Time, allDay bool, err error) {
	sp := c.Props.Get(ical.PropDateTimeStart)
	if sp == nil {
		return start, end, false, fmt.Errorf("event has no start")
	}
	start, allDay, err = dt(sp, loc)
	if err != nil {
		return
	}
	switch {
	case c.Props.Get(ical.PropDateTimeEnd) != nil:
		end, _, err = dt(c.Props.Get(ical.PropDateTimeEnd), loc)
	case c.Props.Get(ical.PropDuration) != nil:
		var d time.Duration
		d, err = c.Props.Get(ical.PropDuration).Duration()
		end = start.Add(d)
	case allDay:
		end = start.AddDate(0, 0, 1)
	default:
		end = start
	}
	if err == nil && end.Before(start) {
		end = start
	}
	return
}

// instance is one occurrence in a window.
type instance struct {
	calendar     string
	href         string
	comp         *ical.Component
	start, end   time.Time
	allDay       bool
	recurring    bool
	recurrenceID time.Time
}

func (in instance) status() string { return strings.ToUpper(text(in.comp, ical.PropStatus)) }
func (in instance) transp() string { return strings.ToUpper(text(in.comp, ical.PropTransparency)) }

func overlaps(s, e, from, to time.Time) bool {
	if !e.After(s) {
		return !s.Before(from) && s.Before(to)
	}
	return s.Before(to) && e.After(from)
}

// days is a whole-day length, rounded so a DST day still counts as one.
func days(d time.Duration) int { return int(math.Round(d.Hours() / 24)) }

// expand lists the occurrences of one calendar object inside [from, to):
// RRULE/RDATE/EXDATE on the master, RECURRENCE-ID overrides replacing their
// slot (or moving into the window), cancelled occurrences dropped.
func expand(cal *ical.Calendar, calName, href string, loc *time.Location, from, to time.Time) ([]instance, error) {
	var master *ical.Component
	overrides := map[int64]*ical.Component{}
	for _, c := range cal.Children {
		if c.Name != ical.CompEvent {
			continue
		}
		if rid := c.Props.Get(ical.PropRecurrenceID); rid != nil {
			if t, _, err := dt(rid, loc); err == nil {
				overrides[t.Unix()] = c
			}
			continue
		}
		if master == nil {
			master = c
		}
	}
	var out []instance
	add := func(c *ical.Component, s, e time.Time, allDay, rec bool, rid time.Time) {
		if !overlaps(s, e, from, to) {
			return
		}
		if rec && strings.EqualFold(text(c, ical.PropStatus), statusCxl) {
			return
		}
		out = append(out, instance{calendar: calName, href: href, comp: c, start: s, end: e, allDay: allDay, recurring: rec, recurrenceID: rid})
	}
	addOverride := func(c *ical.Component, rid time.Time) {
		s, e, allDay, err := span(c, loc)
		if err == nil {
			add(c, s, e, allDay, true, rid)
		}
	}
	used := map[int64]bool{}
	if master != nil {
		start, end, allDay, err := span(master, loc)
		if err != nil {
			return nil, err
		}
		// A rule this code cannot read still leaves the first occurrence.
		set, _ := recurrence(master, start, loc)
		if set == nil {
			add(master, start, end, allDay, false, time.Time{})
		} else {
			dur := end.Sub(start)
			next := set.Iterator()
			for n := 0; n < maxInstances; n++ {
				t, ok := next()
				if !ok || !t.Before(to) {
					break
				}
				if o, ok := overrides[t.Unix()]; ok {
					used[t.Unix()] = true
					addOverride(o, t)
					continue
				}
				e := t.Add(dur)
				if allDay {
					e = t.AddDate(0, 0, days(dur))
				}
				add(master, t, e, allDay, true, t)
			}
		}
	}
	for k, o := range overrides {
		if !used[k] {
			addOverride(o, time.Unix(k, 0).In(loc))
		}
	}
	return out, nil
}

// recurrence builds the rule set of a master component, or nil if it does
// not recur.
func recurrence(c *ical.Component, start time.Time, loc *time.Location) (*rrule.Set, error) {
	rp := c.Props.Get(ical.PropRecurrenceRule)
	rdates := dtList(c.Props.Values(ical.PropRecurrenceDates), loc)
	if rp == nil && len(rdates) == 0 {
		return nil, nil
	}
	set := &rrule.Set{}
	set.DTStart(start)
	if rp != nil {
		rule, err := parseRule(rp.Value, start)
		if err != nil {
			return nil, err
		}
		set.RRule(rule)
	} else {
		set.RDate(start)
	}
	for _, t := range rdates {
		set.RDate(t)
	}
	for _, t := range dtList(c.Props.Values(ical.PropExceptionDates), loc) {
		set.ExDate(t)
	}
	return set, nil
}

// parseRule reads an RRULE value anchored at start. UNTIL written as a date
// or a UTC time both work.
func parseRule(v string, start time.Time) (*rrule.RRule, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "RRULE:")
	opt, err := rrule.StrToROptionInLocation(v, start.Location())
	if err != nil {
		return nil, fmt.Errorf("recurrence rule %q: %v", v, err)
	}
	opt.Dtstart = start
	rule, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, fmt.Errorf("recurrence rule %q: %v", v, err)
	}
	return rule, nil
}

// fmtTime prints a tool time: RFC 3339 in loc, or a date for all-day events
// (an end is shown as the last day, not the exclusive next one).
func fmtTime(t time.Time, allDay, isEnd bool, loc *time.Location) string {
	if allDay {
		if isEnd {
			t = t.AddDate(0, 0, -1)
		}
		return t.Format(dateFmt)
	}
	return t.In(loc).Format(outTimeFmt)
}

// setTimes writes DTSTART/DTEND. Timed events carry the zone's TZID (and the
// calendar gets a VTIMEZONE) so a recurrence keeps its wall-clock time.
func setTimes(cal *ical.Calendar, ev *ical.Component, start, end time.Time, allDay bool, loc *time.Location) {
	ev.Props.Del(ical.PropDuration)
	sp, ep := ical.NewProp(ical.PropDateTimeStart), ical.NewProp(ical.PropDateTimeEnd)
	if allDay {
		sp.SetDate(start)
		ep.SetDate(end)
	} else if loc == time.UTC || loc.String() == "Local" {
		sp.SetDateTime(start.UTC())
		ep.SetDateTime(end.UTC())
	} else {
		sp.SetDateTime(start.In(loc))
		ep.SetDateTime(end.In(loc))
		ensureTimezone(cal, loc, start.Year())
	}
	ev.Props.Set(sp)
	ev.Props.Set(ep)
}

func ensureTimezone(cal *ical.Calendar, loc *time.Location, year int) {
	for _, c := range cal.Children {
		if c.Name == ical.CompTimezone && text(c, ical.PropTimezoneID) == loc.String() {
			return
		}
	}
	tz := vtimezone(loc, year)
	cal.Children = append([]*ical.Component{tz}, cal.Children...)
}

// vtimezone describes loc as a VTIMEZONE from Go's zone data: each
// transition in year becomes a yearly STANDARD/DAYLIGHT rule (nth or last
// weekday of the month, the way tz rules are written).
func vtimezone(loc *time.Location, year int) *ical.Component {
	tz := ical.NewComponent(ical.CompTimezone)
	tz.Props.SetText(ical.PropTimezoneID, loc.String())
	t := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	for range 4 {
		_, end := t.ZoneBounds()
		if end.IsZero() || end.Year() > year {
			break
		}
		_, fromOff := end.Add(-time.Second).Zone()
		name, toOff := end.Zone()
		kind := ical.CompTimezoneStandard
		if end.IsDST() {
			kind = ical.CompTimezoneDaylight
		}
		wall := end.In(time.FixedZone("", fromOff))
		c := ical.NewComponent(kind)
		setRaw(c, ical.PropDateTimeStart, wall.Format(wallFmt))
		setRaw(c, ical.PropTimezoneOffsetFrom, offset(fromOff))
		setRaw(c, ical.PropTimezoneOffsetTo, offset(toOff))
		c.Props.SetText(ical.PropTimezoneName, name)
		nth := (wall.Day()-1)/7 + 1
		if wall.AddDate(0, 0, 7).Month() != wall.Month() {
			nth = -1
		}
		wd := strings.ToUpper(wall.Weekday().String()[:2])
		setRaw(c, ical.PropRecurrenceRule, fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%d%s", int(wall.Month()), nth, wd))
		tz.Children = append(tz.Children, c)
		t = end
	}
	if len(tz.Children) == 0 {
		name, off := t.Zone()
		c := ical.NewComponent(ical.CompTimezoneStandard)
		setRaw(c, ical.PropDateTimeStart, "19700101T000000")
		setRaw(c, ical.PropTimezoneOffsetFrom, offset(off))
		setRaw(c, ical.PropTimezoneOffsetTo, offset(off))
		c.Props.SetText(ical.PropTimezoneName, name)
		tz.Children = append(tz.Children, c)
	}
	return tz
}

func setRaw(c *ical.Component, name, v string) {
	p := ical.NewProp(name)
	p.Value = v
	c.Props.Set(p)
}

func offset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%02d%02d", sign, sec/3600, sec%3600/60)
}

// mailto strips the scheme from a CAL-ADDRESS.
func mailto(v string) string {
	if len(v) > 7 && strings.EqualFold(v[:7], "mailto:") {
		return v[7:]
	}
	return v
}

// setAttendees replaces the attendee list; an organizer is required with
// attendees, so the account itself is named when the event has none.
func setAttendees(ev *ical.Component, emails []string, organizer string) {
	ev.Props.Del(ical.PropAttendee)
	for _, e := range emails {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		p := ical.NewProp(ical.PropAttendee)
		p.Value = "mailto:" + e
		p.Params.Set("ROLE", "REQ-PARTICIPANT")
		p.Params.Set("PARTSTAT", "NEEDS-ACTION")
		p.Params.Set("RSVP", "TRUE")
		ev.Props.Add(p)
	}
	if len(ev.Props.Values(ical.PropAttendee)) > 0 && ev.Props.Get(ical.PropOrganizer) == nil && strings.Contains(organizer, "@") {
		setRaw(ev, ical.PropOrganizer, "mailto:"+organizer)
	}
}

// setReminder replaces the alarms with one display alert, or none for 0.
func setReminder(ev *ical.Component, minutes int, summary string) {
	kept := ev.Children[:0]
	for _, c := range ev.Children {
		if c.Name != ical.CompAlarm {
			kept = append(kept, c)
		}
	}
	ev.Children = kept
	if minutes <= 0 {
		return
	}
	a := ical.NewComponent(ical.CompAlarm)
	setRaw(a, ical.PropAction, "DISPLAY")
	a.Props.SetText(ical.PropDescription, or(summary, "Reminder"))
	trig := ical.NewProp(ical.PropTrigger)
	trig.SetDuration(-time.Duration(minutes) * time.Minute)
	a.Props.Set(trig)
	ev.Children = append(ev.Children, a)
}

// reminders lists relative alarm offsets in minutes before the start.
func reminders(ev *ical.Component) []int {
	out := []int{}
	for _, a := range ev.Children {
		if a.Name != ical.CompAlarm {
			continue
		}
		if p := a.Props.Get(ical.PropTrigger); p != nil && p.Params.Get("VALUE") != "DATE-TIME" {
			if d, err := p.Duration(); err == nil && d <= 0 {
				out = append(out, int(-d/time.Minute))
			}
		}
	}
	return out
}

func or(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// stamp sets DTSTAMP/LAST-MODIFIED to now.
func stamp(ev *ical.Component) {
	now := time.Now().UTC()
	ev.Props.SetDateTime(ical.PropDateTimeStamp, now)
	ev.Props.SetDateTime(ical.PropLastModified, now)
}
