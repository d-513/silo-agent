package tunnels

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
)

const (
	cookieName = "silo_tunnel"
	// authPath is served on the tunnel's own origin to redeem a ticket; the
	// dot-prefix keeps it out of any app's way.
	authPath    = "/.silo/auth"
	grantTTL    = 12 * time.Hour
	ticketTTL   = 30 * time.Second
	usedEvery   = time.Minute
	dialHostTLD = ".tunnel"
)

type tunnelKey struct{}

type ticket struct {
	tunnelID, userID, sessionID string
	exp                         time.Time
}

// proxyState is the per-Service part of serving tunnels.
type proxyState struct {
	once    sync.Once
	rp      *httputil.ReverseProxy
	tmu     sync.Mutex
	tickets map[string]ticket
	umu     sync.Mutex
	used    map[string]time.Time
}

// Route sends a request for <name>.<tunnels.host> to the tunnel and passes
// everything else on. It matches nothing while tunnels are off or have no
// domain, so the domain then belongs to whatever else answers it.
func (s *Service) Route(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := s.cfg().TunnelNameFromHost(r.Host); ok {
			s.serve(w, r, name)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request, name string) {
	t, err := s.ByName(name)
	if err != nil {
		log.Printf("tunnel %s: lookup: %v", name, err)
		page(w, http.StatusInternalServerError, "Something went wrong", "The control plane could not look this tunnel up.")
		return
	}
	if t == nil {
		page(w, http.StatusNotFound, "No tunnel here", "There is no tunnel with this name. It may have been deleted.")
		return
	}
	if r.URL.Path == authPath {
		s.redeem(w, r, t)
		return
	}
	if !t.Public && !s.granted(r, t) {
		s.askToSignIn(w, r, t)
		return
	}
	if s.hub.Get(t.BotID) == nil {
		page(w, http.StatusServiceUnavailable, "The Bot is offline", "This Bot's machine is not running, so nothing can answer. Start the Bot in Silo and reload.")
		return
	}
	s.touch(t)
	s.reverseProxy().ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tunnelKey{}, t)))
}

// granted reports whether the request carries a live grant for this tunnel
// from the Bot's current owner, made in a Silo session that is still live.
func (s *Service) granted(r *http.Request, t *db.Tunnel) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	var g db.TunnelGrant
	res := s.db.Where("id = ? AND tunnel_id = ? AND expires_at > ?", ids.Hash(c.Value), t.ID, time.Now()).Limit(1).Find(&g)
	if res.Error != nil || res.RowsAffected == 0 {
		return false
	}
	// The grant lives only as long as the Silo session it came from: signing
	// out, or that session expiring, ends access immediately.
	var live int64
	s.db.Model(&db.Session{}).Where("id = ? AND user_id = ? AND expires_at > ?", g.SessionID, g.UserID, time.Now()).Count(&live)
	if live != 1 {
		return false
	}
	var n int64
	s.db.Model(&db.Bot{}).Where("id = ? AND user_id = ?", t.BotID, g.UserID).Count(&n)
	return n == 1
}

// askToSignIn answers a private tunnel's request that has no grant: a page load
// goes to the control plane (which knows who is signed in) and back; anything
// else, an API call or an image, is a plain 401 that does not redirect.
func (s *Service) askToSignIn(w http.ResponseWriter, r *http.Request, t *db.Tunnel) {
	navigation := (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.Contains(r.Header.Get("Accept"), "text/html")
	pub := strings.TrimRight(strings.TrimSpace(s.cfg().PublicURL), "/")
	if !navigation {
		http.Error(w, "this tunnel is private: sign in to Silo and open it from there", http.StatusUnauthorized)
		return
	}
	if pub == "" {
		page(w, http.StatusUnauthorized, "This tunnel is private", "Sign in to Silo, then open this address again. (The operator has not set public_url, so Silo cannot send you there automatically.)")
		return
	}
	q := url.Values{"name": {t.Name}, "rd": {r.URL.RequestURI()}}
	http.Redirect(w, r, pub+"/tunnels/auth?"+q.Encode(), http.StatusFound)
}

// ServeAuth is the control plane's half of the sign-in handoff: the signed-in
// owner of the tunnel's Bot is redirected to the tunnel's origin with a
// single-use ticket. The CP session cookie never leaves the CP's origin.
func (s *Service) ServeAuth(w http.ResponseWriter, r *http.Request) {
	sess, u, err := auth.SessionFromRequest(s.db, r)
	if err != nil {
		if errors.Is(err, auth.ErrAuth) {
			// Sign in, then come straight back here: the UI honors `next` for this
			// path only. The redirect is relative, so it stays on whichever origin
			// (public_url) the browser used to reach us.
			http.Redirect(w, r, "/signin?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		log.Printf("tunnel auth: session lookup: %v", err)
		page(w, http.StatusServiceUnavailable, "Try again", "Silo could not check your session just now.")
		return
	}
	t, err := s.ByName(r.URL.Query().Get("name"))
	if err != nil || t == nil || !s.owns(u.ID, t) || s.usable() != nil {
		page(w, http.StatusNotFound, "No such tunnel", "This tunnel does not exist, or it is not on one of your Bots.")
		return
	}
	tk := s.mintTicket(t.ID, u.ID, sess.ID)
	q := url.Values{"ticket": {tk}, "rd": {safeReturn(r.URL.Query().Get("rd"))}}
	http.Redirect(w, r, s.cfg().TunnelURL(t.Name)+authPath+"?"+q.Encode(), http.StatusFound)
}

func (s *Service) owns(userID string, t *db.Tunnel) bool {
	var n int64
	s.db.Model(&db.Bot{}).Where("id = ? AND user_id = ?", t.BotID, userID).Count(&n)
	return n == 1
}

// redeem is the tunnel origin's half: it trades a ticket for a grant cookie.
func (s *Service) redeem(w http.ResponseWriter, r *http.Request, t *db.Tunnel) {
	tk, ok := s.takeTicket(r.URL.Query().Get("ticket"))
	if !ok || tk.tunnelID != t.ID {
		page(w, http.StatusForbidden, "Link expired", "This sign-in link is no longer valid. Open the tunnel from Silo again.")
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "entropy", http.StatusInternalServerError)
		return
	}
	value := hex.EncodeToString(raw)
	now := time.Now()
	s.db.Where("expires_at < ?", now).Delete(&db.TunnelGrant{})
	if err := s.db.Create(&db.TunnelGrant{ID: ids.Hash(value), TunnelID: t.ID, UserID: tk.userID, SessionID: tk.sessionID, ExpiresAt: now.Add(grantTTL)}).Error; err != nil {
		log.Printf("tunnel grant: %v", err)
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.cfg().TunnelScheme() == "https", Expires: now.Add(grantTTL),
	})
	http.Redirect(w, r, safeReturn(r.URL.Query().Get("rd")), http.StatusFound)
}

func (s *Service) mintTicket(tunnelID, userID, sessionID string) string {
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	id := hex.EncodeToString(raw)
	p := &s.px
	p.tmu.Lock()
	defer p.tmu.Unlock()
	if p.tickets == nil {
		p.tickets = map[string]ticket{}
	}
	now := time.Now()
	for k, v := range p.tickets {
		if now.After(v.exp) {
			delete(p.tickets, k)
		}
	}
	p.tickets[id] = ticket{tunnelID: tunnelID, userID: userID, sessionID: sessionID, exp: now.Add(ticketTTL)}
	return id
}

// takeTicket redeems a ticket once.
func (s *Service) takeTicket(id string) (ticket, bool) {
	p := &s.px
	p.tmu.Lock()
	defer p.tmu.Unlock()
	t, ok := p.tickets[id]
	delete(p.tickets, id)
	if !ok || time.Now().After(t.exp) {
		return ticket{}, false
	}
	return t, true
}

// safeReturn keeps the post-sign-in redirect on the tunnel's own origin: only a
// plain absolute path survives.
func safeReturn(rd string) string {
	if !strings.HasPrefix(rd, "/") || strings.HasPrefix(rd, "//") || strings.ContainsAny(rd, "\\\r\n") {
		return "/"
	}
	return rd
}

// touch records use, at most once a minute per tunnel.
func (s *Service) touch(t *db.Tunnel) {
	p := &s.px
	p.umu.Lock()
	if p.used == nil {
		p.used = map[string]time.Time{}
	}
	last := p.used[t.ID]
	now := time.Now()
	if now.Sub(last) < usedEvery {
		p.umu.Unlock()
		return
	}
	p.used[t.ID] = now
	p.umu.Unlock()
	s.db.Model(&db.Tunnel{}).Where("id = ?", t.ID).Update("last_used_at", now)
}

// reverseProxy builds the one proxy every tunnel shares. Its transport's dial
// is the whole trick: it returns a net.Conn that is a stream through the
// worker, so HTTP keep-alive, SSE and WebSocket upgrades all behave as on a
// socket. Requests are addressed to <tunnel id>.tunnel and the dial turns that
// id into a Bot and a port.
func (s *Service) reverseProxy() *httputil.ReverseProxy {
	s.px.once.Do(func() {
		tr := &http.Transport{
			DialContext:         s.dial,
			DisableCompression:  true, // pass the app's encoding through untouched
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     60 * time.Second,
		}
		s.px.rp = &httputil.ReverseProxy{
			Transport:     tr,
			FlushInterval: -1,
			Rewrite: func(pr *httputil.ProxyRequest) {
				t := pr.In.Context().Value(tunnelKey{}).(*db.Tunnel)
				pr.Out.URL.Scheme = "http"
				pr.Out.URL.Host = t.ID + dialHostTLD
				pr.Out.Host = pr.In.Host // dev servers check Host; keep the one the browser used
				pr.SetXForwarded()
				pr.Out.Header.Set("X-Forwarded-Proto", s.cfg().TunnelScheme())
				stripCookie(pr.Out.Header, cookieName)
			},
			ErrorHandler: s.proxyError,
		}
	})
	return s.px.rp
}

func (s *Service) dial(ctx context.Context, _, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	id, ok := strings.CutSuffix(host, dialHostTLD)
	if !ok {
		return nil, fmt.Errorf("tunnel proxy cannot dial %q", addr)
	}
	var t db.Tunnel
	if res := s.db.Where("id = ?", id).Limit(1).Find(&t); res.Error != nil || res.RowsAffected == 0 {
		return nil, errors.New("tunnel was deleted")
	}
	return s.hub.DialTunnel(ctx, t.BotID, t.Port)
}

func (s *Service) proxyError(w http.ResponseWriter, r *http.Request, err error) {
	t, _ := r.Context().Value(tunnelKey{}).(*db.Tunnel)
	switch {
	case errors.Is(err, context.Canceled):
		// The browser went away; there is nobody to tell.
	case errors.Is(err, hub.ErrNoWorker), errors.Is(err, hub.ErrClosed):
		page(w, http.StatusServiceUnavailable, "The Bot is offline", "This Bot's machine stopped while the request was open. Start the Bot in Silo and reload.")
	case errors.Is(err, hub.ErrTunnelTimeout):
		page(w, http.StatusGatewayTimeout, "The Bot did not answer", "The Bot's machine did not respond in time. Try again in a moment.")
	default:
		port := 0
		if t != nil {
			port = t.Port
		}
		page(w, http.StatusBadGateway, fmt.Sprintf("Nothing is answering on port %d", port),
			fmt.Sprintf("The Bot's machine is running, but port %d did not accept the connection (%s). Start the service, and make it listen on 127.0.0.1 or 0.0.0.0.", port, err))
	}
}

func stripCookie(h http.Header, name string) {
	var kept []string
	for _, line := range h.Values("Cookie") {
		var parts []string
		for _, c := range strings.Split(line, ";") {
			if k, _, _ := strings.Cut(strings.TrimSpace(c), "="); k != name && strings.TrimSpace(c) != "" {
				parts = append(parts, strings.TrimSpace(c))
			}
		}
		if len(parts) > 0 {
			kept = append(kept, strings.Join(parts, "; "))
		}
	}
	h.Del("Cookie")
	for _, k := range kept {
		h.Add("Cookie", k)
	}
}

// page writes a small self-contained HTML message in the status's place.
func page(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:34rem;margin:15vh auto;padding:0 1.25rem;color:#1c1c1a;background:#f7f5ef}
@media(prefers-color-scheme:dark){body{color:#ecebe6;background:#1b1b19}}h1{font-size:1.25rem}p{opacity:.8}</style>
<h1>%s</h1><p>%s</p>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(msg))
}
