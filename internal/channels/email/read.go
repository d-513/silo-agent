package email

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"

	"silo.agent/internal/channels"
	"silo.agent/internal/mailin"
	"silo.agent/internal/textx"
)

const (
	// anchorRefs is how much of a thread's References chain an answer carries:
	// the newest ids are what a mail client threads by.
	anchorRefs = 20
	// msgIDMax is the longest Message-ID taken from a stranger's mail.
	msgIDMax = 250
	// subjectMax caps a subject carried into an answer.
	subjectMax = 200
)

// letter is one received message as the channel reads it.
type letter struct {
	header  mail.Header
	from    string // the single From address, lower-cased
	author  string // its display name, else the address
	id      string
	refs    []string
	subject string
	date    time.Time
	body    string
	files   []mailin.Attachment
	// starts is a message that answers nothing: the first of its thread.
	starts bool
}

// read takes a raw message apart. It is false for one that names no single
// author: several is how a forged sender hides behind a real one.
func read(raw []byte) (letter, bool) {
	p := mailin.Parse(raw)
	if p.FromAddr == "" {
		return letter{}, false
	}
	h := headerOf(raw)
	l := letter{
		header:  h,
		from:    p.FromAddr,
		author:  nameOf(h),
		id:      p.MessageID,
		subject: oneLine(p.Subject, subjectMax),
		date:    p.Date,
		body:    cutQuoted(p.Text),
		files:   p.Attachments,
	}
	if l.author == "" {
		l.author = l.from
	}
	if !validID(l.id) {
		l.id = ""
	}
	refs, _ := h.MsgIDList("References")
	parents, _ := h.MsgIDList("In-Reply-To")
	l.starts = len(refs) == 0 && len(parents) == 0
	for _, id := range append(refs, parents...) {
		if validID(id) && (len(l.refs) == 0 || l.refs[len(l.refs)-1] != id) {
			l.refs = append(l.refs, id)
		}
	}
	if len(l.refs) > anchorRefs {
		l.refs = l.refs[len(l.refs)-anchorRefs:]
	}
	return l, true
}

// text is what the Bot reads: the body, under the subject when the message
// opens a thread (a reply's subject only repeats it).
func (l letter) text() string {
	body := strings.TrimSpace(l.body)
	if l.subject == "" || (!l.starts && body != "") {
		return body
	}
	if body == "" {
		return "Subject: " + l.subject
	}
	return "Subject: " + l.subject + "\n\n" + body
}

func (l letter) inbound(bound string, raw []byte) channels.Inbound {
	in := channels.Inbound{
		ExternalID: bound,
		Title:      l.author,
		Author:     l.author,
		AuthorID:   l.from,
		Text:       l.text(),
		ReplyTo:    anchor{ID: l.id, Refs: l.refs, Subject: l.subject}.String(),
		Raw:        map[string]any{"message_id": l.id, "subject": l.subject},
	}
	for _, f := range l.files {
		index := f.Index
		in.Files = append(in.Files, channels.Media{
			Name: f.Name,
			Mime: f.Type,
			Size: int64(f.Size),
			Fetch: func(context.Context) ([]byte, error) {
				_, data, err := mailin.ReadAttachment(raw, index)
				return data, err
			},
		})
	}
	return in
}

// anchor is what an answer needs to land in the thread of the message it
// answers. It travels through the host as Inbound.ReplyTo.
type anchor struct {
	ID      string   `json:"id,omitempty"`
	Refs    []string `json:"refs,omitempty"`
	Subject string   `json:"subject,omitempty"`
}

func (a anchor) String() string {
	b, err := json.Marshal(a)
	if err != nil {
		return ""
	}
	return string(b)
}

// readAnchor reads an anchor back, believing none of it: the ids go into
// headers.
func readAnchor(s string) (anchor, bool) {
	var a anchor
	if s == "" || json.Unmarshal([]byte(s), &a) != nil {
		return anchor{}, false
	}
	if !validID(a.ID) {
		a.ID = ""
	}
	refs := a.Refs[:0]
	for _, id := range a.Refs {
		if validID(id) {
			refs = append(refs, id)
		}
	}
	a.Refs = refs
	a.Subject = oneLine(a.Subject, subjectMax)
	return a, true
}

// validID reports whether a Message-ID is safe to write between < and > in a
// header of our own.
func validID(id string) bool {
	if id == "" || len(id) > msgIDMax {
		return false
	}
	for _, r := range id {
		if r <= ' ' || r > '~' || r == '<' || r == '>' {
			return false
		}
	}
	return true
}

// oneLine makes a stranger's header text one line of at most n runes.
func oneLine(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, textx.ValidUTF8(s))
	return textx.ClipRunes(strings.Join(strings.Fields(s), " "), n)
}

// fromOf is a header's single From address, lower-cased, "" when it names
// none or several.
func fromOf(h mail.Header) string {
	xs, _ := h.AddressList("From")
	if len(xs) != 1 {
		return ""
	}
	return strings.ToLower(xs[0].Address)
}

// nameOf is the display name of a header's first author.
func nameOf(h mail.Header) string {
	xs, _ := h.AddressList("From")
	if len(xs) == 0 {
		return ""
	}
	return oneLine(xs[0].Name, 120)
}

// machine reports whether a message was written by a program, or by the Bot's
// own address. Such mail is never answered: an auto-responder that answers an
// auto-responder never stops. It errs toward silence.
func machine(h mail.Header, self string) bool {
	if v := token(h.Get("Auto-Submitted")); v != "" && v != "no" {
		return true
	}
	switch token(h.Get("Precedence")) {
	case "bulk", "list", "junk", "auto_reply":
		return true
	}
	for _, k := range []string{"List-Id", "List-Unsubscribe", "X-Autoreply", "X-Autorespond"} {
		if h.Has(k) {
			return true
		}
	}
	// An empty return path is a bounce.
	if rp := strings.TrimSpace(h.Get("Return-Path")); rp == "<>" {
		return true
	}
	from := fromOf(h)
	if from == "" {
		return false
	}
	if from == self {
		return true
	}
	local, _, _ := strings.Cut(from, "@")
	local, _, _ = strings.Cut(local, "+")
	local = strings.NewReplacer("-", "", "_", "", ".", "").Replace(local)
	for _, p := range []string{"mailerdaemon", "postmaster", "noreply", "donotreply", "bounce", "autoreply"} {
		if strings.HasPrefix(local, p) {
			return true
		}
	}
	return false
}

// token is a header value's first word, lower-cased, without parameters.
func token(v string) string {
	v, _, _ = strings.Cut(strings.ToLower(v), ";")
	if f := strings.Fields(v); len(f) > 0 {
		return f[0]
	}
	return ""
}

// cutQuoted drops the earlier mail a reply carries at its end: the trailing
// run of ">" lines and the "On … wrote:" line that introduces them. Only the
// tail goes, so an answer written between quoted lines stays whole, and a body
// that is nothing but a quote is kept as it is.
func cutQuoted(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	end := trimBlank(lines, len(lines))
	quoted := end
	for quoted > 0 {
		if l := strings.TrimSpace(lines[quoted-1]); l == "" || strings.HasPrefix(l, ">") {
			quoted--
			continue
		}
		break
	}
	// The run must hold a quote, not only blank lines.
	if !hasQuote(lines[quoted:end]) {
		return strings.Join(lines[:end], "\n")
	}
	end = trimBlank(lines, quoted)
	if end > 0 {
		last := strings.TrimSpace(lines[end-1])
		switch {
		case strings.HasPrefix(last, "On ") && strings.HasSuffix(last, "wrote:"):
			end--
		// Mail clients wrap a long attribution over two lines.
		case strings.HasSuffix(last, "wrote:") && end > 1 && strings.HasPrefix(strings.TrimSpace(lines[end-2]), "On "):
			end -= 2
		}
	}
	end = trimBlank(lines, end)
	if end == 0 {
		return text
	}
	return strings.Join(lines[:end], "\n")
}

func trimBlank(lines []string, end int) int {
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return end
}

func hasQuote(lines []string) bool {
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), ">") {
			return true
		}
	}
	return false
}
