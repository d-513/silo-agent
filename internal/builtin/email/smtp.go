package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"silo.agent/internal/builtin"
)

// strs accepts a JSON list or one comma-separated string: models pass both.
type strs []string

func (s *strs) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		for _, p := range strings.Split(one, ",") {
			if p = strings.TrimSpace(p); p != "" {
				*s = append(*s, p)
			}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

func dialSMTP(ctx context.Context, s settings) (*smtp.Client, error) {
	d := &net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", s.smtpAddr)
	if err != nil {
		return nil, fmt.Errorf("SMTP connect: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	var c *smtp.Client
	switch s.smtpSec {
	case secTLS:
		c = smtp.NewClient(tls.Client(conn, &tls.Config{ServerName: s.smtpHost}))
	case secStartTLS:
		c, err = smtp.NewClientStartTLS(conn, &tls.Config{ServerName: s.smtpHost})
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	default:
		c = smtp.NewClient(conn)
	}
	if err := c.Hello(domainOf(s.address)); err != nil {
		c.Close()
		return nil, fmt.Errorf("SMTP hello: %w", err)
	}
	if err := c.Auth(sasl.NewPlainClient("", s.username, s.password)); err != nil {
		c.Close()
		return nil, fmt.Errorf("SMTP login failed: %w", err)
	}
	return c, nil
}

type sendArgs struct {
	To          strs   `json:"to"`
	Cc          strs   `json:"cc"`
	Bcc         strs   `json:"bcc"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	HTML        bool   `json:"html"`
	Attachments strs   `json:"attachments"`

	// reply
	Folder   string `json:"folder"`
	UID      uint32 `json:"uid"`
	ReplyAll bool   `json:"reply_all"`
}

func send(ctx context.Context, env builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a sendArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	s, err := resolve(cfg)
	if err != nil {
		return nil, err
	}
	o := outgoing{subject: clean(a.Subject), body: a.Body, html: a.HTML}
	if o.to, err = parseAddrs(a.To); err != nil {
		return nil, err
	}
	if o.cc, err = parseAddrs(a.Cc); err != nil {
		return nil, err
	}
	if o.bcc, err = parseAddrs(a.Bcc); err != nil {
		return nil, err
	}
	return deliver(ctx, env, s, o, a.Attachments, nil)
}

func reply(ctx context.Context, env builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a sendArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.UID == 0 {
		return nil, errors.New("uid required")
	}
	folder := folderOf(a.Folder)
	m, s, err := open(ctx, cfg, folder, false)
	if err != nil {
		return nil, err
	}
	defer m.close()
	_, body, err := m.fetchRaw(a.UID, true)
	if err != nil {
		return nil, err
	}
	p, err := parse(body, -1)
	if err != nil {
		return nil, err
	}
	self := strings.ToLower(s.address)
	o := outgoing{body: a.Body, html: a.HTML, inReplyTo: p.messageID, references: p.references}
	o.subject = p.subject
	if !strings.HasPrefix(strings.ToLower(o.subject), "re:") {
		o.subject = "Re: " + o.subject
	}
	o.to = p.replyToAddr
	if len(o.to) == 0 && p.fromAddr != nil {
		o.to = []*mail.Address{p.fromAddr}
	}
	if a.ReplyAll {
		seen := map[string]bool{self: true}
		for _, x := range o.to {
			seen[strings.ToLower(x.Address)] = true
		}
		for _, x := range append(append([]*mail.Address{}, p.toAddrs...), p.ccAddrs...) {
			if k := strings.ToLower(x.Address); !seen[k] {
				seen[k] = true
				o.cc = append(o.cc, x)
			}
		}
	}
	o.body = quoteReply(a.Body, p, a.HTML)
	out, err := deliver(ctx, env, s, o, a.Attachments, m)
	if err != nil {
		return nil, err
	}
	_ = m.Store(imap.UIDSetNum(imap.UID(a.UID)), &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagAnswered},
	}, nil).Close()
	return out, nil
}

func quoteReply(body string, p *parsed, html bool) string {
	who := p.from
	if who == "" {
		who = "the sender"
	}
	intro := "On " + p.date + ", " + who + " wrote:"
	if p.date == "" {
		intro = who + " wrote:"
	}
	orig := p.text()
	if html {
		return body + "<br><br>" + intro + "<blockquote>" + strings.ReplaceAll(escapeHTML(orig), "\n", "<br>") + "</blockquote>"
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n" + intro + "\n")
	for _, l := range strings.Split(orig, "\n") {
		b.WriteString("> " + l + "\n")
	}
	return b.String()
}

func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// deliver reads the attachments from the workspace, composes, submits over
// SMTP, and files a copy in Sent. m is an open IMAP session to reuse, or nil.
func deliver(ctx context.Context, env builtin.Env, s settings, o outgoing, paths []string, m *mailbox) (any, error) {
	if len(o.to)+len(o.cc)+len(o.bcc) == 0 {
		return nil, errNoRecipients
	}
	total := 0
	for _, pth := range paths {
		name, data, err := env.ReadFile(ctx, pth)
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", pth, err)
		}
		if name == "" {
			name = path.Base(pth)
		}
		total += len(data)
		if total > maxAttach {
			return nil, fmt.Errorf("attachments exceed %d MB", maxAttach>>20)
		}
		o.files = append(o.files, file{name: name, data: data})
	}
	o.from = &mail.Address{Name: s.fromName, Address: s.address}
	msg, id, err := compose(o)
	if err != nil {
		return nil, err
	}
	c, err := dialSMTP(ctx, s)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var rcpt []string
	for _, x := range append(append(append([]*mail.Address{}, o.to...), o.cc...), o.bcc...) {
		rcpt = append(rcpt, x.Address)
	}
	if err := c.SendMail(s.address, rcpt, bytes.NewReader(msg)); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	_ = c.Quit()
	out := map[string]any{"sent": true, "message_id": id, "to": addrStrings(o.to), "subject": o.subject}
	if len(o.cc) > 0 {
		out["cc"] = addrStrings(o.cc)
	}
	if s.saveSent && !s.sentSaved {
		if saved, err := saveSent(ctx, s, m, msg); err != nil {
			out["sent_copy_error"] = err.Error()
		} else if saved != "" {
			out["saved_to"] = saved
		}
	}
	return out, nil
}

func saveSent(ctx context.Context, s settings, m *mailbox, msg []byte) (string, error) {
	if m == nil {
		var err error
		if m, err = dialIMAP(ctx, s); err != nil {
			return "", err
		}
		defer m.close()
	}
	sent := m.findRole(imap.MailboxAttrSent, "Sent", "Sent Items", "Sent Messages")
	if sent == "" {
		return "", nil
	}
	cmd := m.Append(sent, int64(len(msg)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}, Time: time.Now()})
	if _, err := cmd.Write(msg); err != nil {
		return "", err
	}
	if err := cmd.Close(); err != nil {
		return "", err
	}
	if _, err := cmd.Wait(); err != nil {
		return "", err
	}
	return sent, nil
}
