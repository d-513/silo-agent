package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

// firstSubjectMax caps a subject made from the Bot's own first line.
const firstSubjectMax = 78

// markdown renders the Bot's Markdown for the HTML part. Raw HTML in the text
// is left out (goldmark's default), so the part holds only what was rendered.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

func (a *Adapter) dialSMTP(ctx context.Context, s settings) (*smtp.Client, error) {
	conn, err := s.net.DialContext(ctx, "tcp", s.smtpAddr)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the SMTP server %s: %w", s.smtpAddr, err)
	}
	// The dial's context covers the greeting and sign-in only; what follows is
	// bounded by the deadline.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(5 * opTimeout))
	var c *smtp.Client
	switch {
	case a.plain:
		c = smtp.NewClient(conn)
	case s.smtpTLS:
		c = smtp.NewClient(tls.Client(conn, &tls.Config{ServerName: s.smtpHost}))
	default:
		if c, err = smtp.NewClientStartTLS(conn, &tls.Config{ServerName: s.smtpHost}); err != nil {
			conn.Close()
			return nil, fmt.Errorf("the SMTP server %s did not start TLS (port 465 is TLS from the start): %w", s.smtpAddr, err)
		}
	}
	if err := c.Hello(domainOf(s.from.Address)); err != nil {
		c.Close()
		return nil, fmt.Errorf("the SMTP server %s did not answer: %w", s.smtpAddr, err)
	}
	if err := c.Auth(sasl.NewPlainClient("", s.username, s.password)); err != nil {
		c.Close()
		var se *smtp.SMTPError
		if errors.As(err, &se) && se.Code >= 500 {
			return nil, fmt.Errorf("the SMTP server refused the username or password (%s): check them, and use an app password where the provider has them", se.Message)
		}
		return nil, fmt.Errorf("SMTP sign-in: %w", err)
	}
	return c, nil
}

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, cfg channels.Config, msg channels.Outbound) error {
	s, err := resolve(cfg)
	if err != nil {
		return err
	}
	to := targetAddr(ch.ExternalID)
	if to == "" {
		return errors.New("the channel has no correspondent yet: pick one in Set up")
	}
	if want := strings.TrimSpace(msg.ExternalID); want != "" && targetAddr(want) != to {
		return errors.New("this channel only writes to " + to)
	}
	if to == s.self {
		return errors.New("the channel is bound to the Bot's own address")
	}
	if strings.TrimSpace(msg.Text) == "" && len(msg.Files) == 0 {
		return nil
	}
	raw, err := compose(s, to, ch.Name, msg)
	if err != nil {
		return err
	}
	c, err := a.dialSMTP(ctx, s)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.SendMail(s.from.Address, []string{to}, bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("the SMTP server did not take the message: %w", err)
	}
	_ = c.Quit()
	return nil
}

// compose writes one mail: the text as plain Markdown and as HTML, then the
// files. Every mail says it was written by a program, so an auto-responder on
// the other side stays quiet.
func compose(s settings, to, channelName string, msg channels.Outbound) ([]byte, error) {
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", []*mail.Address{{Name: s.from.Name, Address: s.from.Address}})
	h.SetAddressList("To", []*mail.Address{{Address: to}})
	if err := h.GenerateMessageIDWithHostname(domainOf(s.from.Address)); err != nil {
		return nil, err
	}
	h.Set("X-Auto-Response-Suppress", "All")
	if an, ok := readAnchor(msg.ReplyTo); ok {
		h.SetSubject(replySubject(an.Subject, channelName))
		h.Set("Auto-Submitted", "auto-replied")
		if an.ID != "" {
			h.SetMsgIDList("In-Reply-To", []string{an.ID})
			refs := an.Refs
			if len(refs) == 0 || refs[len(refs)-1] != an.ID {
				refs = append(append([]string{}, refs...), an.ID)
			}
			h.SetMsgIDList("References", refs)
		}
	} else {
		h.SetSubject(firstSubject(msg.Text, channelName))
		h.Set("Auto-Submitted", "auto-generated")
	}

	var buf bytes.Buffer
	if len(msg.Files) == 0 {
		iw, err := mail.CreateInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if err := writeBodies(iw, msg.Text); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	iw, err := mw.CreateInline()
	if err != nil {
		return nil, err
	}
	if err := writeBodies(iw, msg.Text); err != nil {
		return nil, err
	}
	for i, f := range msg.Files {
		var ah mail.AttachmentHeader
		typ, params := mediaType(f)
		ah.SetContentType(typ, params)
		ah.SetFilename(fileName(f.Name, i))
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(f.Data); err != nil {
			return nil, err
		}
		if err := aw.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeBodies writes the two renderings of the text and closes the part.
func writeBodies(iw *mail.InlineWriter, text string) error {
	for _, body := range []struct{ typ, text string }{
		{"text/plain", text},
		{"text/html", renderHTML(text)},
	} {
		var ih mail.InlineHeader
		ih.SetContentType(body.typ, map[string]string{"charset": "utf-8"})
		w, err := iw.CreatePart(ih)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, body.text); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
	}
	return iw.Close()
}

func renderHTML(text string) string {
	var out bytes.Buffer
	if err := markdown.Convert([]byte(text), &out); err != nil {
		out.Reset()
		out.WriteString("<pre>" + html.EscapeString(text) + "</pre>")
	}
	return "<!DOCTYPE html>\n<html><body>\n" + out.String() + "</body></html>\n"
}

// replySubject is the thread's subject under one "Re:".
func replySubject(subject, channelName string) string {
	if subject == "" {
		return firstSubject("", channelName)
	}
	if len(subject) >= 3 && strings.EqualFold(subject[:3], "re:") {
		return subject
	}
	return "Re: " + subject
}

// firstSubject names a mail the Bot writes first: its opening line without the
// Markdown around it, else the channel's name.
func firstSubject(text, channelName string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimLeft(strings.TrimSpace(line), "#>-*+ ")
		line = strings.NewReplacer("**", "", "`", "").Replace(line)
		line = oneLine(strings.Trim(line, "*_~ "), firstSubjectMax+1)
		if line == "" {
			continue
		}
		if clipped := textx.ClipRunes(line, firstSubjectMax); clipped != line {
			return clipped + "…"
		}
		return line
	}
	if name := oneLine(channelName, firstSubjectMax); name != "" {
		return name
	}
	return "Message"
}

// mediaType is a file's type as the host named it, else by its extension.
func mediaType(f channels.Attachment) (string, map[string]string) {
	for _, v := range []string{f.Mime, mime.TypeByExtension(strings.ToLower(path.Ext(f.Name)))} {
		if typ, params, err := mime.ParseMediaType(v); err == nil && typ != "" {
			return typ, params
		}
	}
	return "application/octet-stream", nil
}

func fileName(name string, i int) string {
	name = oneLine(path.Base(strings.ReplaceAll(name, `\`, "/")), 120)
	if name == "" || name == "." || name == "/" {
		return fmt.Sprintf("attachment-%d", i+1)
	}
	return name
}
