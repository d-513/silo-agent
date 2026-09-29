package calendar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

const (
	httpTimeout = 60 * time.Second
	// maxReport bounds one REPORT body; maxFetch bounds the GETs for hrefs a
	// server listed without their data.
	maxReport = 32 << 20
	maxFetch  = 200
)

var errSignIn = errors.New("sign-in failed: check the username and app password")

// authClient adds basic auth and turns a 401 into errSignIn, so a wrong
// password reads the same whichever request hit it first.
type authClient struct {
	c          *http.Client
	user, pass string
}

func (a authClient) Do(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(a.user, a.pass)
	resp, err := a.c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, errSignIn
	}
	return resp, nil
}

// session is one signed-in CalDAV account with its event calendars.
type session struct {
	s    settings
	http authClient
	base *url.URL
	dav  *caldav.Client
	cals []caldav.Calendar
}

// dial makes a client without touching the network.
func dial(s settings) (*session, error) {
	base, err := url.Parse(s.endpoint)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("server URL %q is not a URL", s.endpoint)
	}
	hc := authClient{c: &http.Client{Timeout: httpTimeout}, user: s.username, pass: s.password}
	sess := &session{s: s, http: hc}
	if err := sess.rebase(base); err != nil {
		return nil, err
	}
	return sess, nil
}

func (sess *session) rebase(u *url.URL) error {
	dav, err := caldav.NewClient(sess.http, u.String())
	if err != nil {
		return err
	}
	sess.base, sess.dav = u, dav
	return nil
}

// open signs in and lists the event calendars: principal → home set →
// calendars, with /.well-known/caldav discovery when the URL is only a host.
func open(ctx context.Context, s settings) (*session, error) {
	sess, err := dial(s)
	if err != nil {
		return nil, err
	}
	p := strings.TrimSuffix(sess.base.Path, "/")
	if p == "/.well-known/caldav" {
		if err := sess.wellKnown(ctx); err != nil {
			return nil, err
		}
	}
	principal, err := sess.dav.FindCurrentUserPrincipal(ctx)
	if err != nil && !errors.Is(err, errSignIn) && p == "" {
		if werr := sess.wellKnown(ctx); werr == nil {
			principal, err = sess.dav.FindCurrentUserPrincipal(ctx)
		}
	}
	if err != nil {
		return nil, friendly("find your account", err)
	}
	if principal == "" {
		principal = sess.base.Path
	}
	home, err := sess.homeSet(ctx, principal)
	if err != nil {
		return nil, friendly("find your calendars", err)
	}
	if home == "" {
		home = principal
	}
	all, err := sess.dav.FindCalendars(ctx, home)
	if err != nil {
		return nil, friendly("list calendars", err)
	}
	for _, c := range all {
		if len(c.SupportedComponentSet) == 0 || slices.Contains(c.SupportedComponentSet, ical.CompEvent) {
			sess.cals = append(sess.cals, c)
		}
	}
	discovery.Lock()
	discovery.m[accountKey(s)] = found{base: sess.base, cals: sess.cals, at: time.Now()}
	discovery.Unlock()
	return sess, nil
}

// homeSet reads calendar-home-set itself: go-webdav keeps only the href's
// path, but iCloud answers with an absolute URL on a partition host
// (pNN-caldav.icloud.com), and the front host returns no events to a
// calendar-query. The session moves to that host.
func (sess *session) homeSet(ctx context.Context, principal string) (string, error) {
	body := `<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` +
		`<D:prop><C:calendar-home-set/></D:prop></D:propfind>`
	ms, err := sess.xmlRequest(ctx, "PROPFIND", principal, "0", body)
	if err != nil {
		return "", err
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			for _, h := range ps.Prop.Home {
				h = strings.TrimSpace(h)
				if h == "" {
					continue
				}
				u, err := sess.base.Parse(h)
				if err != nil {
					continue
				}
				if u.Host != sess.base.Host || u.Scheme != sess.base.Scheme {
					if err := sess.rebase(&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}); err != nil {
						return "", err
					}
				}
				return u.Path, nil
			}
		}
	}
	return "", nil
}

// multistatus is the part of a WebDAV 207 body the connector reads. Tags
// match by local name, whatever prefix the server picked.
type multistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Status string `xml:"status"`
			Prop   struct {
				ETag string   `xml:"getetag"`
				Data string   `xml:"calendar-data"`
				Home []string `xml:"calendar-home-set>href"`
			} `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

// xmlRequest sends a PROPFIND/REPORT to a path on the current host and
// decodes the 207.
func (sess *session) xmlRequest(ctx context.Context, method, p, depth, body string) (*multistatus, error) {
	u := *sess.base
	u.Path, u.RawQuery = p, ""
	req, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)
	req.Header.Set("Depth", depth)
	resp, err := sess.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("%s %s: %s", method, p, resp.Status)
	}
	var ms multistatus
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxReport)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("%s %s: unreadable reply: %w", method, p, err)
	}
	return &ms, nil
}

// discovery caches what open found (host after redirects, event calendars)
// per account, so a run of tool calls does not repeat three PROPFINDs each.
var discovery = struct {
	sync.Mutex
	m map[string]found
}{m: map[string]found{}}

type found struct {
	base *url.URL
	cals []caldav.Calendar
	at   time.Time
}

const discoveryTTL = 5 * time.Minute

func accountKey(s settings) string {
	sum := sha256.Sum256([]byte(s.endpoint + "\x00" + s.username + "\x00" + s.password))
	return hex.EncodeToString(sum[:])
}

// cached is open with a recent discovery reused. Check always calls open.
func cached(ctx context.Context, s settings) (*session, error) {
	key := accountKey(s)
	discovery.Lock()
	f, ok := discovery.m[key]
	discovery.Unlock()
	if ok && time.Since(f.at) < discoveryTTL {
		sess, err := dial(s)
		if err != nil {
			return nil, err
		}
		if err := sess.rebase(f.base); err != nil {
			return nil, err
		}
		sess.cals = f.cals
		return sess, nil
	}
	return open(ctx, s)
}

// forget drops a cached discovery, after a failure that may mean it is stale.
func (sess *session) forget() {
	discovery.Lock()
	delete(discovery.m, accountKey(sess.s))
	discovery.Unlock()
}

// wellKnown follows /.well-known/caldav by hand: Go's client drops the
// PROPFIND body on a 301/302, which turns it into an allprop request.
func (sess *session) wellKnown(ctx context.Context) error {
	u := *sess.base
	u.Path, u.RawQuery = "/.well-known/caldav", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	c := *sess.http.c
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := authClient{c: &c, user: sess.http.user, pass: sess.http.pass}.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	loc, err := resp.Location()
	if err != nil {
		return errors.New("no CalDAV service at /.well-known/caldav; give the full server URL")
	}
	return sess.rebase(loc)
}

// friendly keeps the sign-in error as is and names the step otherwise.
func friendly(step string, err error) error {
	if errors.Is(err, errSignIn) {
		return errSignIn
	}
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Timeout() {
		return fmt.Errorf("could not %s: the server did not answer within %s", step, httpTimeout)
	}
	return fmt.Errorf("could not %s: %w", step, err)
}

// calendar finds one event calendar by path or (case-insensitive) name.
func (sess *session) calendar(name string) (caldav.Calendar, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = sess.s.defaultCal
	}
	if name == "" {
		if len(sess.cals) == 0 {
			return caldav.Calendar{}, errors.New("this account has no event calendars")
		}
		return sess.cals[0], nil
	}
	for _, c := range sess.cals {
		if trimSlash(c.Path) == trimSlash(name) || strings.EqualFold(c.Name, name) {
			return c, nil
		}
	}
	names := make([]string, 0, len(sess.cals))
	for _, c := range sess.cals {
		names = append(names, c.Name)
	}
	return caldav.Calendar{}, fmt.Errorf("no event calendar %q; have: %s", name, strings.Join(names, ", "))
}

// pick resolves a list of names; none means every event calendar.
func (sess *session) pick(names []string) ([]caldav.Calendar, error) {
	if len(names) == 0 {
		return sess.cals, nil
	}
	var out []caldav.Calendar
	for _, n := range names {
		c, err := sess.calendar(n)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func trimSlash(p string) string { return strings.TrimSuffix(p, "/") }

// events fetches the objects in a calendar with a VEVENT touching the window.
// Servers may return more (the filter is advisory); the expansion re-checks.
// It asks for plain <calendar-data/> (not go-webdav's allprop/allcomp form,
// which not every server honors) and GETs any href that came back without
// data. bad counts objects that could not be read.
func (sess *session) events(ctx context.Context, cal caldav.Calendar, from, to time.Time) (objs []caldav.CalendarObject, bad int, err error) {
	const stamp = "20060102T150405Z"
	body := `<?xml version="1.0" encoding="utf-8"?><C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` +
		`<D:prop><D:getetag/><C:calendar-data/></D:prop><C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT">` +
		`<C:time-range start="` + from.UTC().Format(stamp) + `" end="` + to.UTC().Format(stamp) + `"/>` +
		`</C:comp-filter></C:comp-filter></C:filter></C:calendar-query>`
	ms, err := sess.xmlRequest(ctx, "REPORT", cal.Path, "1", body)
	if err != nil {
		sess.forget()
		return nil, 0, friendly("read "+or(cal.Name, cal.Path), err)
	}
	var missing []string
	for _, r := range ms.Responses {
		u, err := sess.base.Parse(strings.TrimSpace(r.Href))
		if err != nil || trimSlash(u.Path) == trimSlash(cal.Path) {
			continue
		}
		var data, etag string
		for _, ps := range r.Propstat {
			if ps.Status == "" || strings.Contains(ps.Status, " 200") {
				data, etag = or(data, ps.Prop.Data), or(etag, ps.Prop.ETag)
			}
		}
		if strings.TrimSpace(data) == "" {
			missing = append(missing, u.Path)
			continue
		}
		c, err := ical.NewDecoder(strings.NewReader(data)).Decode()
		if err != nil {
			bad++
			continue
		}
		objs = append(objs, caldav.CalendarObject{Path: u.Path, ETag: strings.Trim(etag, `"`), Data: c})
	}
	for i, p := range missing {
		if i >= maxFetch {
			bad += len(missing) - i
			break
		}
		co, err := sess.dav.GetCalendarObject(ctx, p)
		if err != nil {
			bad++
			continue
		}
		co.Path = p
		objs = append(objs, *co)
	}
	return objs, bad, nil
}

// href normalizes an event reference to a server path.
func href(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if u, err := url.Parse(ref); err == nil && u.Host != "" {
		ref = u.Path
	}
	if !strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") {
		return "", fmt.Errorf("href %q is not an event path from list_events", ref)
	}
	return ref, nil
}

func (sess *session) get(ctx context.Context, ref string) (*caldav.CalendarObject, error) {
	p, err := href(ref)
	if err != nil {
		return nil, err
	}
	co, err := sess.dav.GetCalendarObject(ctx, p)
	if err != nil {
		return nil, friendly("read the event", err)
	}
	co.Path = p
	return co, nil
}

// put writes an object. go-webdav's PutCalendarObject has no preconditions,
// so this sends If-None-Match (create) / If-Match (update) itself.
func (sess *session) put(ctx context.Context, p string, cal *ical.Calendar, etag string, create bool) error {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return err
	}
	u := *sess.base
	u.Path, u.RawQuery = p, ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ical.MIMEType+"; charset=utf-8")
	if create {
		req.Header.Set("If-None-Match", "*")
	} else if etag != "" {
		req.Header.Set("If-Match", quoteETag(etag))
	}
	resp, err := sess.http.Do(req)
	if err != nil {
		return friendly("save the event", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusPreconditionFailed {
		return errors.New("the event changed on the server since it was read; read it again and retry")
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 || strings.HasPrefix(msg, "<") {
		msg = ""
	}
	return fmt.Errorf("could not save the event: %s %s", resp.Status, msg)
}

func quoteETag(e string) string {
	if strings.HasPrefix(e, `"`) || strings.HasPrefix(e, `W/`) {
		return e
	}
	return `"` + e + `"`
}

func (sess *session) remove(ctx context.Context, ref string) (string, error) {
	p, err := href(ref)
	if err != nil {
		return "", err
	}
	if err := sess.dav.RemoveAll(ctx, p); err != nil {
		return "", friendly("delete the event", err)
	}
	return p, nil
}
