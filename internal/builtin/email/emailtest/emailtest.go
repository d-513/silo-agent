// Package emailtest runs an in-memory IMAP server and a capturing SMTP server
// on localhost for Email connector tests.
package emailtest

import (
	"bytes"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

const (
	Address  = "me@example.com"
	Password = "app-pass-1234"
)

// Mail is one message the SMTP server accepted.
type Mail struct {
	From string
	To   []string
	Data []byte
}

type Server struct {
	User     *imapmemserver.User
	IMAPPort int
	SMTPPort int

	mu   sync.Mutex
	sent []Mail
}

// Start runs both servers until the test ends. The mailbox has INBOX, Sent,
// Trash, and Archive.
func Start(t testing.TB) *Server {
	t.Helper()
	s := &Server{User: imapmemserver.NewUser(Address, Password)}
	for _, name := range []string{"INBOX", "Sent", "Trash", "Archive"} {
		if err := s.User.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem := imapmemserver.New()
	mem.AddUser(s.User)
	is := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true,
	})
	iln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = is.Serve(iln) }()
	t.Cleanup(func() { _ = is.Close() })
	s.IMAPPort = iln.Addr().(*net.TCPAddr).Port

	ss := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) {
		return &session{srv: s}, nil
	}))
	ss.Domain = "localhost"
	ss.AllowInsecureAuth = true
	ss.ReadTimeout = 10 * time.Second
	ss.WriteTimeout = 10 * time.Second
	sln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ss.Serve(sln) }()
	t.Cleanup(func() { _ = ss.Close() })
	s.SMTPPort = sln.Addr().(*net.TCPAddr).Port
	return s
}

// Config is the connector config that points at these servers.
func (s *Server) Config() map[string]string {
	return map[string]string{
		"provider": "custom", "address": Address, "password": Password, "from_name": "Me",
		"imap_host": "127.0.0.1", "imap_port": strconv.Itoa(s.IMAPPort), "imap_security": "none",
		"smtp_host": "127.0.0.1", "smtp_port": strconv.Itoa(s.SMTPPort), "smtp_security": "none",
		"save_sent": "true",
	}
}

// Deliver appends a raw message to a folder and returns its UID.
func (s *Server) Deliver(t testing.TB, folder, raw string, flags ...imap.Flag) uint32 {
	t.Helper()
	data, err := s.User.Append(folder, bytes.NewReader([]byte(raw)), &imap.AppendOptions{Flags: flags, Time: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return uint32(data.UID)
}

// Sent returns what the SMTP server accepted so far.
func (s *Server) Sent() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.sent...)
}

type session struct {
	srv  *Server
	auth bool
	cur  Mail
}

func (s *session) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *session) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, user, pass string) error {
		if user != Address || pass != Password {
			return smtp.ErrAuthFailed
		}
		s.auth = true
		return nil
	}), nil
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	if !s.auth {
		return smtp.ErrAuthRequired
	}
	s.cur = Mail{From: from}
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.cur.To = append(s.cur.To, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.cur.Data = b
	s.srv.mu.Lock()
	s.srv.sent = append(s.srv.sent, s.cur)
	s.srv.mu.Unlock()
	return nil
}

func (s *session) Reset()        { s.cur = Mail{} }
func (s *session) Logout() error { return nil }
