// Package mailin receives mail and does nothing else: an SMTP listener that
// accepts messages for mailboxes it is told exist, hands each to a Handler,
// and refuses everything else inside the session. It never sends, relays or
// bounces. Reading a message (MIME to text, attachments) and judging who sent
// it (DKIM, SPF) live here too.
package mailin

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-smtp"
	"golang.org/x/net/netutil"
)

// Envelope is what SMTP said about one message, for one recipient.
type Envelope struct {
	// From is the MAIL FROM address; empty for a bounce.
	From string
	// To is the mailbox this copy is for, lower-cased.
	To     string
	Remote net.IP
	Helo   string
	TLS    bool
	// By is the receiving server's name and At when the message arrived: the
	// two halves of the trace line a store puts on top of what it keeps.
	By string
	At time.Time
}

// Handler owns the mailboxes.
type Handler interface {
	// Accept reports whether rcpt (lower-cased) is a mailbox here.
	Accept(rcpt string) bool
	// Deliver files one message, exactly as it was sent, for one accepted
	// recipient. An error is answered as a temporary failure, so the sender
	// tries again later.
	Deliver(ctx context.Context, env Envelope, raw []byte) error
}

// Config is one listener.
type Config struct {
	// Addr is host:port to bind.
	Addr string
	// Domain is the name the server greets with.
	Domain string
	// MaxBytes caps one message; 0 is 10 MB.
	MaxBytes int64
	// TLS enables STARTTLS when set.
	TLS *tls.Config
}

const (
	// maxConns caps open connections, so idle ones cannot use the process up.
	maxConns      = 200
	maxRecipients = 10
	ioTimeout     = 60 * time.Second
	deliverWithin = 30 * time.Second
	// probeLimit is how many unknown recipients one address may name per
	// probeWindow before it is refused outright.
	probeLimit  = 10
	probeWindow = 10 * time.Minute
	// rateLimit is how many messages one address may hand over per rateWindow.
	rateLimit  = 60
	rateWindow = time.Minute
)

var (
	errNoMailbox = &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "No such mailbox here"}
	errSlowDown  = &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 7, 1}, Message: "Too many attempts, try again later"}
	errTryLater  = &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "Could not store the message, try again later"}
)

// Server is a running listener.
type Server struct {
	srv    *smtp.Server
	ln     net.Listener
	h      Handler
	domain string
	probes *window
	rate   *window
}

// Listen binds cfg.Addr and serves until Close. A port that cannot be bound
// is an error here, not a log line later.
func Listen(cfg Config, h Handler) (*Server, error) {
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	ln = netutil.LimitListener(ln, maxConns)
	s := &Server{ln: ln, h: h, domain: cfg.Domain, probes: newWindow(probeLimit, probeWindow), rate: newWindow(rateLimit, rateWindow)}
	srv := smtp.NewServer(smtp.BackendFunc(func(c *smtp.Conn) (smtp.Session, error) {
		return &session{s: s, c: c}, nil
	}))
	srv.Domain = cfg.Domain
	srv.MaxRecipients = maxRecipients
	srv.MaxMessageBytes = cfg.MaxBytes
	if srv.MaxMessageBytes <= 0 {
		srv.MaxMessageBytes = 10 << 20
	}
	srv.ReadTimeout = ioTimeout
	srv.WriteTimeout = ioTimeout
	srv.TLSConfig = cfg.TLS
	srv.ErrorLog = quietLog{}
	s.srv = srv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
			log.Printf("mail: listener %s stopped: %v", ln.Addr(), err)
		}
	}()
	return s, nil
}

// Addr is the bound address (the real port when Config.Addr asked for :0).
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Close stops the listener and drops its connections.
func (s *Server) Close() error { return s.srv.Close() }

// quietLog drops go-smtp's per-connection noise (scanners hang up mid-line
// all day); real failures are logged where they happen.
type quietLog struct{}

func (quietLog) Printf(string, ...any) {}
func (quietLog) Println(...any)        {}

type session struct {
	s     *Server
	c     *smtp.Conn
	from  string
	rcpts []string
}

func (ss *session) remote() net.IP {
	if a, ok := ss.c.Conn().RemoteAddr().(*net.TCPAddr); ok {
		return a.IP
	}
	return nil
}

func (ss *session) Reset() { ss.from, ss.rcpts = "", nil }

func (ss *session) Logout() error { return nil }

func (ss *session) Mail(from string, _ *smtp.MailOptions) error {
	ss.from = strings.TrimSpace(from)
	return nil
}

func (ss *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	ip := ss.remote().String()
	if ss.s.probes.over(ip) {
		return errSlowDown
	}
	rcpt := strings.ToLower(strings.TrimSpace(to))
	if !ss.s.h.Accept(rcpt) {
		ss.s.probes.add(ip)
		return errNoMailbox
	}
	for _, have := range ss.rcpts {
		if have == rcpt {
			return nil
		}
	}
	ss.rcpts = append(ss.rcpts, rcpt)
	return nil
}

func (ss *session) Data(r io.Reader) error {
	ip := ss.remote()
	if ss.s.rate.over(ip.String()) {
		return errSlowDown
	}
	ss.s.rate.add(ip.String())
	// The reader stops with 552 at MaxMessageBytes, so this cannot run away.
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	_, secure := ss.c.TLSConnectionState()
	env := Envelope{From: ss.from, Remote: ip, Helo: ss.c.Hostname(), TLS: secure, By: ss.s.domain, At: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), deliverWithin)
	defer cancel()
	for _, rcpt := range ss.rcpts {
		env.To = rcpt
		if err := ss.s.h.Deliver(ctx, env, raw); err != nil {
			// What went wrong is ours to read, not the sender's.
			log.Printf("mail: deliver to %s: %v", rcpt, err)
			return errTryLater
		}
	}
	return nil
}

// Received is the trace line a receiving server adds on top of a message.
func (env Envelope) Received() []byte {
	with := "ESMTP"
	if env.TLS {
		with = "ESMTPS"
	}
	helo := strings.Map(func(r rune) rune {
		if r <= ' ' || r > '~' || r == '(' || r == ')' {
			return -1
		}
		return r
	}, env.Helo)
	if helo == "" {
		helo = "unknown"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Received: from %s (%s)\r\n\tby %s with %s;\r\n\t%s\r\n", helo, env.Remote, env.By, with, env.At.Format(time.RFC1123Z))
	return b.Bytes()
}

// window counts events per key inside a sliding period.
type window struct {
	mu     sync.Mutex
	limit  int
	period time.Duration
	hits   map[string][]time.Time
}

func newWindow(limit int, period time.Duration) *window {
	return &window{limit: limit, period: period, hits: map[string][]time.Time{}}
}

func (w *window) add(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	w.hits[key] = append(w.live(key, now), now)
	// Other keys' stale entries are swept when the map grows.
	if len(w.hits) > 4096 {
		for k := range w.hits {
			if len(w.live(k, now)) == 0 {
				delete(w.hits, k)
			}
		}
	}
}

func (w *window) over(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.live(key, time.Now())) >= w.limit
}

// live trims key's hits to the ones still inside the period.
func (w *window) live(key string, now time.Time) []time.Time {
	xs := w.hits[key]
	i := 0
	for i < len(xs) && now.Sub(xs[i]) > w.period {
		i++
	}
	xs = xs[i:]
	if len(xs) == 0 {
		delete(w.hits, key)
		return nil
	}
	w.hits[key] = xs
	return xs
}
