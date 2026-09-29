package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"

	"silo.agent/internal/builtin"
)

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("bad arguments: %w", err)
	}
	return nil
}

func connect(ctx context.Context, cfg builtin.Config) (*session, error) {
	s, err := resolve(cfg)
	if err != nil {
		return nil, err
	}
	return cached(ctx, s)
}

func listCalendars(ctx context.Context, _ builtin.Env, cfg builtin.Config, _ json.RawMessage) (any, error) {
	sess, err := connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	type row struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Description string `json:"description,omitempty"`
	}
	out := make([]row, 0, len(sess.cals))
	for _, c := range sess.cals {
		out = append(out, row{Name: or(c.Name, c.Path), Path: c.Path, Description: c.Description})
	}
	return map[string]any{"calendars": out, "timezone": sess.s.loc.String()}, nil
}

type eventRow struct {
	Calendar     string `json:"calendar"`
	Href         string `json:"href"`
	UID          string `json:"uid"`
	Summary      string `json:"summary"`
	Start        string `json:"start"`
	End          string `json:"end"`
	AllDay       bool   `json:"all_day"`
	Location     string `json:"location,omitempty"`
	Status       string `json:"status,omitempty"`
	Recurring    bool   `json:"recurring,omitempty"`
	RecurrenceID string `json:"recurrence_id,omitempty"`
}

// collect expands every event in the calendars over [from, to), sorted.
// skipped counts objects that could not be read; one malformed event must not
// hide the rest, but it must not vanish silently either.
func collect(ctx context.Context, sess *session, names []string, from, to time.Time) (all []instance, skipped int, err error) {
	cals, err := sess.pick(names)
	if err != nil {
		return nil, 0, err
	}
	for _, c := range cals {
		objs, bad, err := sess.events(ctx, c, from, to)
		if err != nil {
			return nil, 0, err
		}
		skipped += bad
		for _, o := range objs {
			if o.Data == nil {
				skipped++
				continue
			}
			in, err := expand(o.Data, or(c.Name, c.Path), o.Path, sess.s.loc, from, to)
			if err != nil {
				skipped++
				continue
			}
			all = append(all, in...)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].start.Equal(all[j].start) {
			return all[i].start.Before(all[j].start)
		}
		return text(all[i].comp, ical.PropSummary) < text(all[j].comp, ical.PropSummary)
	})
	return all, skipped, nil
}

func listEvents(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a struct {
		Calendar, Start, End, Query string
		Limit                       int
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	loc := sess.s.loc
	from, to, err := window(a.Start, a.End, loc)
	if err != nil {
		return nil, err
	}
	var names []string
	if c := strings.TrimSpace(a.Calendar); c != "" && !strings.EqualFold(c, "all") {
		names = []string{c}
	}
	all, skipped, err := collect(ctx, sess, names, from, to)
	if err != nil {
		return nil, err
	}
	limit := a.Limit
	if limit <= 0 {
		limit = defLimit
	}
	limit = min(limit, maxLimit)
	q := strings.ToLower(strings.TrimSpace(a.Query))
	rows := []eventRow{}
	total := 0
	for _, in := range all {
		c := in.comp
		if q != "" && !strings.Contains(strings.ToLower(text(c, ical.PropSummary)+"\n"+text(c, ical.PropLocation)+"\n"+text(c, ical.PropDescription)), q) {
			continue
		}
		total++
		if len(rows) >= limit {
			continue
		}
		r := eventRow{
			Calendar: in.calendar, Href: in.href, UID: text(c, ical.PropUID), Summary: text(c, ical.PropSummary),
			Start: fmtTime(in.start, in.allDay, false, loc), End: fmtTime(in.end, in.allDay, true, loc),
			AllDay: in.allDay, Location: text(c, ical.PropLocation), Status: strings.ToLower(in.status()),
			Recurring: in.recurring,
		}
		if in.recurring {
			r.RecurrenceID = fmtTime(in.recurrenceID, in.allDay, false, loc)
		}
		rows = append(rows, r)
	}
	out := map[string]any{
		"events": rows, "total": total, "truncated": total > len(rows),
		"start": fmtTime(from, false, false, loc), "end": fmtTime(to, false, false, loc),
	}
	if skipped > 0 {
		out["skipped_unreadable"] = skipped
	}
	return out, nil
}

// master is the series (or only) VEVENT of an object.
func master(cal *ical.Calendar) *ical.Component {
	var first *ical.Component
	for _, c := range cal.Children {
		if c.Name != ical.CompEvent {
			continue
		}
		if c.Props.Get(ical.PropRecurrenceID) == nil {
			return c
		}
		if first == nil {
			first = c
		}
	}
	return first
}

func getEvent(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a struct{ Href string }
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	s, err := resolve(cfg)
	if err != nil {
		return nil, err
	}
	sess, err := dial(s)
	if err != nil {
		return nil, err
	}
	co, err := sess.get(ctx, a.Href)
	if err != nil {
		return nil, err
	}
	ev := master(co.Data)
	if ev == nil {
		return nil, errors.New("that object holds no event")
	}
	loc := s.loc
	start, end, allDay, err := span(ev, loc)
	if err != nil {
		return nil, err
	}
	type attendee struct {
		Email  string `json:"email"`
		Name   string `json:"name,omitempty"`
		Status string `json:"status,omitempty"`
		Role   string `json:"role,omitempty"`
	}
	atts := []attendee{}
	for _, p := range ev.Props.Values(ical.PropAttendee) {
		atts = append(atts, attendee{
			Email: mailto(p.Value), Name: p.Params.Get("CN"),
			Status: strings.ToLower(p.Params.Get("PARTSTAT")), Role: strings.ToLower(p.Params.Get("ROLE")),
		})
	}
	out := map[string]any{
		"href": co.Path, "uid": text(ev, ical.PropUID), "summary": text(ev, ical.PropSummary),
		"description": text(ev, ical.PropDescription), "location": text(ev, ical.PropLocation),
		"start": fmtTime(start, allDay, false, loc), "end": fmtTime(end, allDay, true, loc), "all_day": allDay,
		"status": strings.ToLower(text(ev, ical.PropStatus)), "transparency": strings.ToLower(text(ev, ical.PropTransparency)),
		"url": text(ev, ical.PropURL), "attendees": atts, "reminders": reminders(ev),
	}
	if p := ev.Props.Get(ical.PropOrganizer); p != nil {
		out["organizer"] = mailto(p.Value)
	}
	if p := ev.Props.Get(ical.PropDateTimeStart); p != nil && p.Params.Get(ical.PropTimezoneID) != "" {
		out["timezone"] = p.Params.Get(ical.PropTimezoneID)
	}
	if p := ev.Props.Get(ical.PropRecurrenceRule); p != nil {
		out["rrule"] = p.Value
		var ex []string
		for _, t := range dtList(ev.Props.Values(ical.PropExceptionDates), loc) {
			ex = append(ex, fmtTime(t, allDay, false, loc))
		}
		if len(ex) > 0 {
			out["exdates"] = ex
		}
	}
	var moved []map[string]any
	for _, c := range co.Data.Children {
		rid := c.Props.Get(ical.PropRecurrenceID)
		if c.Name != ical.CompEvent || rid == nil {
			continue
		}
		t, _, _ := dt(rid, loc)
		s, e, ad, err := span(c, loc)
		if err != nil {
			continue
		}
		moved = append(moved, map[string]any{
			"recurrence_id": fmtTime(t, ad, false, loc), "start": fmtTime(s, ad, false, loc), "end": fmtTime(e, ad, true, loc),
			"summary": text(c, ical.PropSummary), "status": strings.ToLower(text(c, ical.PropStatus)),
		})
	}
	if len(moved) > 0 {
		out["overrides"] = moved
	}
	return out, nil
}

// hours parses "HH:MM-HH:MM" into minutes after midnight.
func hours(s string) (from, to int, err error) {
	a, b, ok := strings.Cut(strings.ReplaceAll(s, " ", ""), "-")
	parse := func(v string) (int, error) {
		h, m, ok := strings.Cut(v, ":")
		hh, e1 := strconv.Atoi(h)
		mm, e2 := strconv.Atoi(m)
		if !ok || e1 != nil || e2 != nil || hh < 0 || hh > 24 || mm < 0 || mm > 59 {
			return 0, fmt.Errorf("hours %q: use HH:MM-HH:MM", s)
		}
		return hh*60 + mm, nil
	}
	if !ok {
		return 0, 0, fmt.Errorf("hours %q: use HH:MM-HH:MM", s)
	}
	if from, err = parse(a); err != nil {
		return
	}
	if to, err = parse(b); err != nil {
		return
	}
	if to <= from {
		err = fmt.Errorf("hours %q: the end must be after the start", s)
	}
	return
}

type interval struct{ start, end time.Time }

func freeBusy(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a struct {
		Start, End, Hours string
		Calendars         []string
		MinMinutes        *int `json:"min_minutes"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	loc := sess.s.loc
	from, to, err := window(a.Start, a.End, loc)
	if err != nil {
		return nil, err
	}
	minGap := 30 * time.Minute
	if a.MinMinutes != nil && *a.MinMinutes >= 0 {
		minGap = time.Duration(*a.MinMinutes) * time.Minute
	}
	all, _, err := collect(ctx, sess, a.Calendars, from, to)
	if err != nil {
		return nil, err
	}
	var busy []interval
	for _, in := range all {
		if in.status() == statusCxl || in.transp() == transparent || (in.allDay && in.transp() != "OPAQUE") || !in.end.After(in.start) {
			continue
		}
		s, e := in.start, in.end
		if s.Before(from) {
			s = from
		}
		if e.After(to) {
			e = to
		}
		busy = append(busy, interval{s, e})
	}
	sort.Slice(busy, func(i, j int) bool { return busy[i].start.Before(busy[j].start) })
	var merged []interval
	for _, b := range busy {
		if n := len(merged); n > 0 && !b.start.After(merged[n-1].end) {
			if b.end.After(merged[n-1].end) {
				merged[n-1].end = b.end
			}
			continue
		}
		merged = append(merged, b)
	}

	windows := []interval{{from, to}}
	if strings.TrimSpace(a.Hours) != "" {
		h1, h2, err := hours(a.Hours)
		if err != nil {
			return nil, err
		}
		windows = nil
		for d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc); d.Before(to); d = d.AddDate(0, 0, 1) {
			s := d.Add(time.Duration(h1) * time.Minute)
			e := d.Add(time.Duration(h2) * time.Minute)
			if s.Before(from) {
				s = from
			}
			if e.After(to) {
				e = to
			}
			if e.After(s) {
				windows = append(windows, interval{s, e})
			}
		}
	}
	type row struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	free := []row{}
	for _, w := range windows {
		cur := w.start
		for _, b := range merged {
			if !b.end.After(cur) || !b.start.Before(w.end) {
				continue
			}
			if b.start.Sub(cur) >= minGap && b.start.After(cur) {
				free = append(free, row{fmtTime(cur, false, false, loc), fmtTime(b.start, false, false, loc)})
			}
			if b.end.After(cur) {
				cur = b.end
			}
		}
		if w.end.Sub(cur) >= minGap && w.end.After(cur) {
			free = append(free, row{fmtTime(cur, false, false, loc), fmtTime(w.end, false, false, loc)})
		}
	}
	busyRows := make([]row, 0, len(merged))
	for _, b := range merged {
		busyRows = append(busyRows, row{fmtTime(b.start, false, false, loc), fmtTime(b.end, false, false, loc)})
	}
	return map[string]any{
		"busy": busyRows, "free": free, "timezone": loc.String(),
		"start": fmtTime(from, false, false, loc), "end": fmtTime(to, false, false, loc),
	}, nil
}

// eventArgs is the shared create/update input. Pointers tell "omitted" from
// "cleared" on update.
type eventArgs struct {
	Href            string
	Calendar        string
	Summary         *string
	Start           *string
	End             *string
	DurationMinutes *int  `json:"duration_minutes"`
	AllDay          *bool `json:"all_day"`
	Location        *string
	Description     *string
	Attendees       *[]string
	RRule           *string `json:"rrule"`
	ReminderMinutes *int    `json:"reminder_minutes"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// times works out the new start/end from the arguments over the current
// span (zero for a new event).
func (a eventArgs) times(loc *time.Location, curStart, curEnd time.Time, curAllDay bool) (start, end time.Time, allDay bool, err error) {
	start, end, allDay = curStart, curEnd, curAllDay
	dateOnly := false
	if a.Start != nil {
		if start, dateOnly, err = parseWhen(*a.Start, loc); err != nil {
			return
		}
		if dateOnly {
			allDay = true
		} else if a.AllDay == nil {
			allDay = false
		}
	}
	if a.AllDay != nil {
		allDay = *a.AllDay
	}
	if allDay {
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	}
	switch {
	case a.End != nil && str(a.End) != "":
		var t time.Time
		var endDate bool
		if t, endDate, err = parseWhen(*a.End, loc); err != nil {
			return
		}
		if allDay {
			// All-day ends are the last day; iCalendar wants the next one.
			end = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
		} else if endDate {
			err = errors.New("a timed event needs an end time, not a date")
			return
		} else {
			end = t
		}
	case a.DurationMinutes != nil && *a.DurationMinutes > 0:
		d := time.Duration(*a.DurationMinutes) * time.Minute
		if allDay {
			end = start.AddDate(0, 0, max(1, int(d/(24*time.Hour))))
		} else {
			end = start.Add(d)
		}
	case curStart.IsZero() || (a.AllDay != nil && *a.AllDay != curAllDay):
		if allDay {
			end = start.AddDate(0, 0, 1)
		} else {
			end = start.Add(time.Hour)
		}
	case a.Start != nil:
		// Moving the start keeps the length.
		if allDay && curAllDay {
			end = start.AddDate(0, 0, max(1, days(curEnd.Sub(curStart))))
		} else {
			end = start.Add(curEnd.Sub(curStart))
		}
	}
	if !end.After(start) {
		err = errors.New("end must be after start")
	}
	return
}

// apply writes the non-time fields present in a onto ev.
func (a eventArgs) apply(ev *ical.Component, organizer string) error {
	for name, v := range map[string]*string{ical.PropSummary: a.Summary, ical.PropLocation: a.Location, ical.PropDescription: a.Description} {
		if v == nil {
			continue
		}
		if s := strings.TrimSpace(*v); s != "" {
			ev.Props.SetText(name, s)
		} else {
			ev.Props.Del(name)
		}
	}
	if a.Attendees != nil {
		setAttendees(ev, *a.Attendees, organizer)
	}
	if a.RRule != nil {
		v := strings.TrimPrefix(str(a.RRule), "RRULE:")
		if v == "" {
			ev.Props.Del(ical.PropRecurrenceRule)
			ev.Props.Del(ical.PropExceptionDates)
		} else {
			start, _, _, err := span(ev, time.UTC)
			if err != nil {
				return err
			}
			if _, err := parseRule(v, start); err != nil {
				return err
			}
			p := ical.NewProp(ical.PropRecurrenceRule)
			p.SetValueType(ical.ValueRecurrence)
			p.Value = v
			ev.Props.Set(p)
		}
	}
	if a.ReminderMinutes != nil {
		setReminder(ev, *a.ReminderMinutes, text(ev, ical.PropSummary))
	}
	return nil
}

func createEvent(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a eventArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if str(a.Summary) == "" {
		return nil, errors.New("summary is required")
	}
	if str(a.Start) == "" {
		return nil, errors.New("start is required")
	}
	sess, err := connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	target, err := sess.calendar(a.Calendar)
	if err != nil {
		return nil, err
	}
	loc := sess.s.loc
	start, end, allDay, err := a.times(loc, time.Time{}, time.Time{}, false)
	if err != nil {
		return nil, err
	}
	uid := uuid.NewString()
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, productID)
	ev := ical.NewComponent(ical.CompEvent)
	ev.Props.SetText(ical.PropUID, uid)
	stamp(ev)
	ev.Props.SetDateTime(ical.PropCreated, time.Now().UTC())
	cal.Children = append(cal.Children, ev)
	setTimes(cal, ev, start, end, allDay, loc)
	if err := a.apply(ev, sess.s.username); err != nil {
		return nil, err
	}
	p := strings.TrimSuffix(target.Path, "/") + "/" + uid + ".ics"
	if err := sess.put(ctx, p, cal, "", true); err != nil {
		return nil, err
	}
	return map[string]any{
		"href": p, "uid": uid, "calendar": or(target.Name, target.Path),
		"start": fmtTime(start, allDay, false, loc), "end": fmtTime(end, allDay, true, loc), "all_day": allDay,
	}, nil
}

func updateEvent(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a eventArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.Summary != nil && str(a.Summary) == "" {
		return nil, errors.New("summary cannot be blank")
	}
	s, err := resolve(cfg)
	if err != nil {
		return nil, err
	}
	sess, err := dial(s)
	if err != nil {
		return nil, err
	}
	co, err := sess.get(ctx, a.Href)
	if err != nil {
		return nil, err
	}
	ev := master(co.Data)
	if ev == nil {
		return nil, errors.New("that object holds no event")
	}
	curStart, curEnd, curAllDay, err := span(ev, s.loc)
	if err != nil {
		return nil, err
	}
	if a.Start != nil || a.End != nil || a.DurationMinutes != nil || a.AllDay != nil {
		start, end, allDay, err := a.times(s.loc, curStart, curEnd, curAllDay)
		if err != nil {
			return nil, err
		}
		setTimes(co.Data, ev, start, end, allDay, s.loc)
	}
	if err := a.apply(ev, s.username); err != nil {
		return nil, err
	}
	seq := 0
	if p := ev.Props.Get(ical.PropSequence); p != nil {
		seq, _ = p.Int()
	}
	setRaw(ev, ical.PropSequence, strconv.Itoa(seq+1))
	stamp(ev)
	if err := sess.put(ctx, co.Path, co.Data, co.ETag, false); err != nil {
		return nil, err
	}
	start, end, allDay, _ := span(ev, s.loc)
	return map[string]any{
		"href": co.Path, "uid": text(ev, ical.PropUID), "summary": text(ev, ical.PropSummary),
		"start": fmtTime(start, allDay, false, s.loc), "end": fmtTime(end, allDay, true, s.loc), "all_day": allDay,
	}, nil
}

func deleteEvent(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a struct{ Href string }
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	s, err := resolve(cfg)
	if err != nil {
		return nil, err
	}
	sess, err := dial(s)
	if err != nil {
		return nil, err
	}
	p, err := sess.remove(ctx, a.Href)
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": p}, nil
}
