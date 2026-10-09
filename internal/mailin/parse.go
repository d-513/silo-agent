package mailin

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset" // decode non-UTF-8 charsets
	"github.com/emersion/go-message/mail"

	"silo.agent/internal/htmltext"
	"silo.agent/internal/textx"
)

// Attachment names one file a message carries. Index is its position among
// the message's attachments and stays the same on every read.
type Attachment struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int    `json:"size"`
}

// Message is a received message read for a person or a model: its headers,
// one body as text, and what is attached.
type Message struct {
	// From is the From header as written. FromAddr is its address, lower-cased,
	// and empty unless the header names exactly one author: several is how a
	// forged sender hides behind a real one.
	From      string
	FromAddr  string
	To, Cc    string
	Subject   string
	Date      time.Time
	MessageID string
	// Text is the plain body, or the HTML one flattened when that is all
	// there is.
	Text        string
	Attachments []Attachment
}

// Parse reads a raw message. It never fails: mail comes from strangers, and a
// message that is not well-formed is still worth showing as far as it reads.
func Parse(raw []byte) Message {
	var m Message
	var plain, html string
	hdr, err := walk(raw, func(p part) bool {
		switch {
		case p.attachment:
			m.Attachments = append(m.Attachments, p.meta(len(m.Attachments)))
		case p.ctype == "text/html":
			if html == "" {
				html = p.text()
			}
		case plain == "":
			plain = p.text()
		}
		return true
	})
	if err != nil {
		// Not a message we can take apart: show the bytes as the body.
		m.Text = textx.ValidUTF8(string(raw))
		return m
	}
	m.Subject, _ = hdr.Subject()
	m.Subject = textx.ValidUTF8(m.Subject)
	if d, err := hdr.Date(); err == nil {
		m.Date = d
	}
	m.MessageID, _ = hdr.MessageID()
	if xs, _ := hdr.AddressList("From"); len(xs) > 0 {
		m.From = addresses(hdr, "From")
		if len(xs) == 1 {
			m.FromAddr = strings.ToLower(xs[0].Address)
		}
	} else {
		m.From = textx.ValidUTF8(hdr.Get("From"))
	}
	m.To = addresses(hdr, "To")
	m.Cc = addresses(hdr, "Cc")
	m.Text = plain
	if strings.TrimSpace(plain) == "" && html != "" {
		m.Text = htmltext.Flatten(html)
	}
	return m
}

// ReadAttachment returns the bytes of the message's index-th attachment.
func ReadAttachment(raw []byte, index int) (Attachment, []byte, error) {
	var out Attachment
	var data []byte
	found := false
	n := 0
	_, err := walk(raw, func(p part) bool {
		if !p.attachment {
			return true
		}
		if n == index {
			out, data, found = p.meta(n), p.body, true
			return false
		}
		n++
		return true
	})
	if err != nil {
		return out, nil, err
	}
	if !found {
		return out, nil, fmt.Errorf("the message has no attachment %d", index)
	}
	return out, data, nil
}

func addresses(h mail.Header, key string) string {
	xs, _ := h.AddressList(key)
	out := make([]string, 0, len(xs))
	for _, a := range xs {
		out = append(out, a.String())
	}
	return textx.ValidUTF8(strings.Join(out, ", "))
}

// part is one leaf of a message: a body or an attachment.
type part struct {
	attachment bool
	ctype      string
	name       string
	body       []byte
}

// text is a body part as text, with the wire's CRLF line ends made plain.
func (p part) text() string {
	return strings.ReplaceAll(textx.ValidUTF8(string(p.body)), "\r\n", "\n")
}

func (p part) meta(i int) Attachment {
	name := safeName(p.name)
	if name == "" {
		name = fmt.Sprintf("attachment-%d", i)
		if exts, _ := mime.ExtensionsByType(p.ctype); len(exts) > 0 {
			name += exts[0]
		}
	}
	return Attachment{Index: i, Name: name, Type: p.ctype, Size: len(p.body)}
}

// safeName reduces a sender-chosen file name to a bare file name.
func safeName(s string) string {
	s = strings.ReplaceAll(textx.ValidUTF8(s), `\`, "/")
	s = strings.TrimSpace(path.Base(path.Clean("/" + s)))
	if s == "/" || s == "." || s == ".." {
		return ""
	}
	return textx.ClipRunes(s, 120)
}

// walk calls fn for each leaf part in order until it returns false, and
// returns the message's own header.
func walk(raw []byte, fn func(part) bool) (mail.Header, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && r == nil {
		return mail.Header{}, fmt.Errorf("parse message: %w", err)
	}
	defer r.Close()
	for {
		p, err := r.NextPart()
		if err != nil {
			// io.EOF ends it; a broken trailing part should not hide what
			// was read before it.
			break
		}
		// An unknown charset or encoding still yields the bytes it could read.
		body, _ := io.ReadAll(p.Body)
		var leaf part
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, params, _ := h.ContentType()
			_, disp, _ := h.ContentDisposition()
			name := disp["filename"]
			if name == "" {
				name = params["name"]
			}
			text := ct == "" || ct == "text/plain" || ct == "text/html"
			leaf = part{attachment: name != "" || !text, ctype: ct, name: name, body: body}
		case *mail.AttachmentHeader:
			name, _ := h.Filename()
			ct, _, _ := h.ContentType()
			leaf = part{attachment: true, ctype: ct, name: name, body: body}
		default:
			continue
		}
		if !fn(leaf) {
			break
		}
	}
	return r.Header, nil
}
