// Package caldavtest runs an in-memory CalDAV server on localhost for
// Calendar connector tests. It serves go-webdav's own caldav.Handler behind
// basic auth, with two event calendars and one task list.
package caldavtest

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

const (
	Username = "me@example.com"
	Password = "app-pass-5678"

	Principal = "/me/"
	Home      = "/me/calendars/"
	Personal  = "/me/calendars/personal/"
	Work      = "/me/calendars/work/"
	Tasks     = "/me/calendars/tasks/"
)

type Server struct {
	URL string

	mu       sync.Mutex
	requests int
	cals     []caldav.Calendar
	objects  map[string]caldav.CalendarObject
}

// Start runs the server until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	s := &Server{
		cals: []caldav.Calendar{
			{Path: Personal, Name: "Personal", Description: "Home stuff", SupportedComponentSet: []string{ical.CompEvent}},
			{Path: Work, Name: "Work", SupportedComponentSet: []string{ical.CompEvent, ical.CompToDo}},
			{Path: Tasks, Name: "Reminders", SupportedComponentSet: []string{ical.CompToDo}},
		},
		objects: map[string]caldav.CalendarObject{},
	}
	h := &caldav.Handler{Backend: s}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != Username || p != Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="caldavtest"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		s.requests++
		s.mu.Unlock()
		// go-webdav's client drops the endpoint's trailing slash and its
		// server matches collections exactly; real servers accept both.
		switch r.URL.Path + "/" {
		case Principal, Home, Personal, Work, Tasks:
			r.URL.Path += "/"
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// StartPartitioned mimics iCloud: the returned front host signs in and lists
// calendars, but its calendar-home-set is an absolute URL on a second
// "partition" host, and the front answers every REPORT with an empty
// multistatus. Only a client that follows the home host sees events.
func StartPartitioned(t testing.TB) *Server {
	t.Helper()
	s := Start(t)
	partition := s.URL
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "REPORT" {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><multistatus xmlns="DAV:"></multistatus>`)
			return
		}
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, partition+r.URL.RequestURI(), r.Body)
		req.Header = r.Header.Clone()
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if r.Method == "PROPFIND" && strings.TrimSuffix(r.URL.Path, "/")+"/" == Principal {
			body = bytes.ReplaceAll(body, []byte(">"+Home+"<"), []byte(">"+partition+Home+"<"))
		}
		for k, v := range resp.Header {
			if k != "Content-Length" {
				w.Header()[k] = v
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}))
	t.Cleanup(front.Close)
	s.URL = front.URL
	return s
}

// Config is the connector field values that reach this server.
func (s *Server) Config() map[string]string {
	return map[string]string{
		"provider": "custom", "url": s.URL, "username": Username, "password": Password, "timezone": "Europe/Warsaw",
	}
}

// Put stores a raw iCalendar object at calendar/name and returns its path.
func (s *Server) Put(t testing.TB, calendar, name, raw string) string {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	p := path.Join(calendar, name)
	if _, err := s.PutCalendarObject(context.Background(), p, cal, &caldav.PutCalendarObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	return p
}

// Requests counts the authorized requests served so far.
func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// Object returns the stored object at p, or nil.
func (s *Server) Object(p string) *ical.Calendar {
	s.mu.Lock()
	defer s.mu.Unlock()
	co, ok := s.objects[p]
	if !ok {
		return nil
	}
	return co.Data
}

// Paths lists every stored object path, sorted.
func (s *Server) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.objects))
	for p := range s.objects {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func notFound(what string) error {
	return webdav.NewHTTPError(http.StatusNotFound, errors.New(what+" not found"))
}

func (s *Server) CurrentUserPrincipal(context.Context) (string, error) { return Principal, nil }
func (s *Server) CalendarHomeSetPath(context.Context) (string, error)  { return Home, nil }

func (s *Server) CreateCalendar(context.Context, *caldav.Calendar) error {
	return webdav.NewHTTPError(http.StatusForbidden, errors.New("read-only home"))
}

func (s *Server) ListCalendars(context.Context) ([]caldav.Calendar, error) {
	return append([]caldav.Calendar(nil), s.cals...), nil
}

func (s *Server) GetCalendar(_ context.Context, p string) (*caldav.Calendar, error) {
	for _, c := range s.cals {
		if strings.TrimSuffix(c.Path, "/") == strings.TrimSuffix(p, "/") {
			return &c, nil
		}
	}
	return nil, notFound("calendar")
}

func (s *Server) GetCalendarObject(_ context.Context, p string, _ *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	co, ok := s.objects[p]
	if !ok {
		return nil, notFound("object")
	}
	return &co, nil
}

func (s *Server) ListCalendarObjects(_ context.Context, p string, _ *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := strings.TrimSuffix(p, "/") + "/"
	var out []caldav.CalendarObject
	for k, co := range s.objects {
		if strings.HasPrefix(k, dir) {
			out = append(out, co)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// QueryCalendarObjects returns every object of the queried component type.
// Like a lax real server it ignores the time range; the client filters.
func (s *Server) QueryCalendarObjects(ctx context.Context, p string, q *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	all, err := s.ListCalendarObjects(ctx, p, nil)
	if err != nil {
		return nil, err
	}
	if q == nil || len(q.CompFilter.Comps) == 0 {
		return all, nil
	}
	want := q.CompFilter.Comps[0].Name
	var out []caldav.CalendarObject
	for _, co := range all {
		for _, c := range co.Data.Children {
			if c.Name == want {
				out = append(out, co)
				break
			}
		}
	}
	return out, nil
}

func (s *Server) PutCalendarObject(_ context.Context, p string, cal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	if _, err := s.GetCalendar(context.Background(), path.Dir(p)); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.objects[p]
	if opts != nil {
		if opts.IfNoneMatch.IsWildcard() && exists {
			return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, errors.New("exists"))
		}
		if opts.IfMatch.IsSet() {
			if ok, _ := opts.IfMatch.MatchETag(old.ETag); !exists || !ok {
				return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, errors.New("etag mismatch"))
			}
		}
	}
	sum := sha1.Sum(buf.Bytes())
	co := caldav.CalendarObject{
		Path: p, ModTime: time.Now(), ContentLength: int64(buf.Len()),
		ETag: hex.EncodeToString(sum[:8]), Data: cal,
	}
	s.objects[p] = co
	return &co, nil
}

func (s *Server) DeleteCalendarObject(_ context.Context, p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[p]; !ok {
		return notFound("object")
	}
	delete(s.objects, p)
	return nil
}

var _ caldav.Backend = (*Server)(nil)

// Event builds a minimal VCALENDAR with one VEVENT from raw property lines.
func Event(uid string, lines ...string) string {
	return fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//caldavtest//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\n%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		uid, strings.Join(lines, "\r\n"))
}
