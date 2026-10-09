package email

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/emersion/go-message/charset" // decode non-UTF-8 charsets
	"github.com/emersion/go-message/mail"

	"silo.agent/internal/htmltext"
)

type attachmentMeta struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

type picked struct {
	meta attachmentMeta
	data []byte
}

// parsed is a message read for the model: headers, the first plain and HTML
// body, and every attachment in order. Attachment indexes are stable for a
// message, so read and save_attachment agree.
type parsed struct {
	messageID   string
	references  []string
	date        string
	from        string
	fromAddr    *mail.Address
	replyToAddr []*mail.Address
	toAddrs     []*mail.Address
	ccAddrs     []*mail.Address
	to, cc      []string
	replyTo     []string
	subject     string
	plain, html string
	attachments []attachmentMeta
	picked      *picked
}

func (p *parsed) text() string {
	if strings.TrimSpace(p.plain) != "" {
		return p.plain
	}
	if p.html != "" {
		return htmlText(p.html)
	}
	return ""
}

// parse reads a raw RFC 5322 message. keep is the attachment index whose
// bytes to hold on to (-1 for none).
func parse(raw []byte, keep int) (*parsed, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && r == nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	defer r.Close()
	p := &parsed{attachments: []attachmentMeta{}}
	h := r.Header
	p.messageID, _ = h.MessageID()
	p.references, _ = h.MsgIDList("References")
	if d, err := h.Date(); err == nil && !d.IsZero() {
		p.date = d.Format(time.RFC3339)
	}
	p.subject, _ = h.Subject()
	p.subject = clean(p.subject)
	if xs, _ := h.AddressList("From"); len(xs) > 0 {
		p.fromAddr = xs[0]
		p.from = xs[0].String()
	}
	p.toAddrs, _ = h.AddressList("To")
	p.ccAddrs, _ = h.AddressList("Cc")
	p.replyToAddr, _ = h.AddressList("Reply-To")
	p.to, p.cc, p.replyTo = addrStrings(p.toAddrs), addrStrings(p.ccAddrs), addrStrings(p.replyToAddr)
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A malformed trailing part should not hide what was read.
			if p.plain != "" || p.html != "" || len(p.attachments) > 0 {
				break
			}
			return nil, fmt.Errorf("parse message: %w", err)
		}
		switch ph := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			_, dispParams, _ := ph.ContentDisposition()
			name := dispParams["filename"]
			if name == "" {
				_, ctParams, _ := ph.ContentType()
				name = ctParams["name"]
			}
			if name == "" && (ct == "text/plain" || ct == "text/html" || ct == "") {
				b, _ := io.ReadAll(io.LimitReader(part.Body, maxMessage))
				switch {
				case ct == "text/html" && p.html == "":
					p.html = clean(string(b))
				case ct != "text/html" && p.plain == "":
					p.plain = clean(string(b))
				}
				continue
			}
			p.attach(name, ct, part.Body, keep)
		case *mail.AttachmentHeader:
			name, _ := ph.Filename()
			ct, _, _ := ph.ContentType()
			p.attach(name, ct, part.Body, keep)
		}
	}
	return p, nil
}

func (p *parsed) attach(name, ct string, body io.Reader, keep int) {
	i := len(p.attachments)
	b, _ := io.ReadAll(io.LimitReader(body, maxMessage))
	meta := attachmentMeta{Index: i, Filename: clean(name), ContentType: ct, Size: len(b)}
	if meta.Filename == "" {
		meta.Filename = fmt.Sprintf("attachment-%d", i)
		if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
			meta.Filename += exts[0]
		}
	}
	p.attachments = append(p.attachments, meta)
	if i == keep {
		p.picked = &picked{meta: meta, data: b}
	}
}

func addrStrings(xs []*mail.Address) []string {
	out := []string{}
	for _, a := range xs {
		out = append(out, a.String())
	}
	return out
}

// htmlText flattens an HTML body into readable text.
func htmlText(src string) string { return htmltext.Flatten(src) }

// clean makes a header or body string valid UTF-8 without NULs.
func clean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	return strings.ReplaceAll(s, "\x00", "")
}

func truncate(s string, max int) (string, bool) {
	if utf8.RuneCountInString(s) <= max {
		return s, false
	}
	r := []rune(s)
	return string(r[:max]) + "\n…truncated", true
}

// --- compose ---

type outgoing struct {
	from       *mail.Address
	to, cc     []*mail.Address
	bcc        []*mail.Address
	subject    string
	body       string
	html       bool
	inReplyTo  string
	references []string
	files      []file
}

type file struct {
	name string
	data []byte
}

// compose writes the message. Bcc is not written as a header; the caller
// adds those recipients to the SMTP envelope.
func compose(o outgoing) ([]byte, string, error) {
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", []*mail.Address{o.from})
	h.SetAddressList("To", o.to)
	if len(o.cc) > 0 {
		h.SetAddressList("Cc", o.cc)
	}
	h.SetSubject(o.subject)
	if err := h.GenerateMessageIDWithHostname(domainOf(o.from.Address)); err != nil {
		return nil, "", err
	}
	id, _ := h.MessageID()
	if o.inReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{o.inReplyTo})
		h.SetMsgIDList("References", append(append([]string{}, o.references...), o.inReplyTo))
	}
	var buf bytes.Buffer
	ct := "text/plain"
	if o.html {
		ct = "text/html"
	}
	var ih mail.InlineHeader
	ih.SetContentType(ct, map[string]string{"charset": "utf-8"})
	if len(o.files) == 0 {
		// A single-part message: merge the body header into the top level.
		h.SetContentType(ct, map[string]string{"charset": "utf-8"})
		w, err := mail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.WriteString(w, o.body); err != nil {
			return nil, "", err
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), id, nil
	}
	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, "", err
	}
	tw, err := mw.CreateSingleInline(ih)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.WriteString(tw, o.body); err != nil {
		return nil, "", err
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	for _, f := range o.files {
		var ah mail.AttachmentHeader
		// TypeByExtension returns "text/plain; charset=utf-8"; SetContentType
		// wants the bare type and formats an empty header for one with params.
		typ, params, err := mime.ParseMediaType(mime.TypeByExtension(extOf(f.name)))
		if err != nil || typ == "" {
			typ, params = "application/octet-stream", nil
		}
		ah.SetContentType(typ, params)
		ah.SetFilename(f.name)
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, "", err
		}
		if _, err := aw.Write(f.data); err != nil {
			return nil, "", err
		}
		if err := aw.Close(); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), id, nil
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return strings.ToLower(name[i:])
	}
	return ""
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 && i < len(addr)-1 {
		return addr[i+1:]
	}
	return "localhost"
}

// parseAddrs accepts "a@x, B <b@y>" style entries.
func parseAddrs(xs []string) ([]*mail.Address, error) {
	var out []*mail.Address
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		list, err := mail.ParseAddressList(x)
		if err != nil {
			return nil, fmt.Errorf("bad address %q", x)
		}
		out = append(out, list...)
	}
	return out, nil
}

var errNoRecipients = errors.New("at least one recipient required")
