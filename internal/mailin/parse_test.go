package mailin

import (
	"strings"
	"testing"
)

func crlf(s string) []byte { return []byte(strings.ReplaceAll(s, "\n", "\r\n")) }

func TestParsePlainMessage(t *testing.T) {
	m := Parse(crlf(`Received: from a (1.2.3.4) by bots.test with ESMTP; Mon, 05 Oct 2026 10:00:00 +0000
From: "Ada Lovelace" <Ada@Example.com>
To: quiet-amber-heron@bots.test
Cc: Bob <bob@example.com>
Subject: =?UTF-8?B?WsW8w7PFgsSHIGfEmcWbbMSF?=
Date: Mon, 05 Oct 2026 09:59:58 +0200
Message-ID: <abc@example.com>

Your code is 482913.
`))
	if m.From != `"Ada Lovelace" <Ada@Example.com>` || m.FromAddr != "ada@example.com" {
		t.Fatalf("from %q addr %q", m.From, m.FromAddr)
	}
	if m.To != "<quiet-amber-heron@bots.test>" || m.Cc != `"Bob" <bob@example.com>` {
		t.Fatalf("to %q cc %q", m.To, m.Cc)
	}
	if m.Subject != "Zżółć gęślą" {
		t.Fatalf("subject %q", m.Subject)
	}
	if m.Date.IsZero() || m.Date.UTC().Format("2006-01-02 15:04:05") != "2026-10-05 07:59:58" {
		t.Fatalf("date %v", m.Date)
	}
	// The wire's CRLF does not reach the reader.
	if m.MessageID != "abc@example.com" || m.Text != "Your code is 482913.\n" || len(m.Attachments) != 0 {
		t.Fatalf("message = %+v", m)
	}
}

const multipart = `From: shop@example.com
To: a@bots.test
Subject: Receipt
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=outer

--outer
Content-Type: multipart/alternative; boundary=inner

--inner
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Total: 12,50 =A3
--inner
Content-Type: text/html; charset=utf-8

<p>Total: <b>12,50 £</b></p>
--inner--
--outer
Content-Type: application/pdf; name="receipt.pdf"
Content-Disposition: attachment; filename="receipt.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQKJWZha2UK
--outer
Content-Type: image/png
Content-Disposition: inline
Content-Transfer-Encoding: base64

iVBORw0KGgo=
--outer--
`

func TestParseMultipartPrefersPlainAndListsAttachments(t *testing.T) {
	m := Parse(crlf(multipart))
	// The plain part wins over the HTML one, decoded from its charset.
	if strings.TrimSpace(m.Text) != "Total: 12,50 £" {
		t.Fatalf("text %q", m.Text)
	}
	if len(m.Attachments) != 2 {
		t.Fatalf("attachments = %+v", m.Attachments)
	}
	a, b := m.Attachments[0], m.Attachments[1]
	if a.Index != 0 || a.Name != "receipt.pdf" || a.Type != "application/pdf" || a.Size != 15 {
		t.Fatalf("first = %+v", a)
	}
	// An unnamed part gets a name from its type.
	if b.Index != 1 || b.Name != "attachment-1.png" || b.Type != "image/png" {
		t.Fatalf("second = %+v", b)
	}

	got, data, err := ReadAttachment(crlf(multipart), 0)
	if err != nil || got.Name != "receipt.pdf" || string(data) != "%PDF-1.4\n%fake\n" {
		t.Fatalf("attachment 0 = %+v %q %v", got, data, err)
	}
	if _, _, err := ReadAttachment(crlf(multipart), 2); err == nil {
		t.Fatal("an attachment that is not there was read")
	}
}

func TestParseHTMLOnlyIsFlattened(t *testing.T) {
	m := Parse(crlf(`From: a@example.com
Subject: Hi
Content-Type: text/html; charset=utf-8

<html><body><h1>Welcome</h1><p>Click <a href="https://example.com/v?t=1">here</a>.</p><script>x()</script></body></html>
`))
	if m.Text != "Welcome\nClick here (https://example.com/v?t=1)." {
		t.Fatalf("text %q", m.Text)
	}
}

// A file name is chosen by a stranger: it must never carry a path.
func TestParseAttachmentNamesCannotEscape(t *testing.T) {
	m := Parse(crlf(`From: a@example.com
Content-Type: multipart/mixed; boundary=b

--b
Content-Type: text/plain

hi
--b
Content-Disposition: attachment; filename="../../etc/passwd"

x
--b
Content-Disposition: attachment; filename="..\\..\\boot.ini"

x
--b
Content-Disposition: attachment; filename=".."

x
--b--
`))
	if len(m.Attachments) != 3 {
		t.Fatalf("attachments = %+v", m.Attachments)
	}
	for _, a := range m.Attachments {
		if strings.ContainsAny(a.Name, `/\`) || a.Name == ".." || a.Name == "." || a.Name == "" {
			t.Fatalf("unsafe name %q", a.Name)
		}
	}
	if m.Attachments[0].Name != "passwd" || m.Attachments[1].Name != "boot.ini" {
		t.Fatalf("names = %+v", m.Attachments)
	}
}

// Whatever arrives is stored: a message that is not MIME at all still reads.
func TestParseGarbageDoesNotFail(t *testing.T) {
	for _, raw := range []string{"", "not a message at all", "Subject: only a header", "\x00\xff\xfe binary \x80"} {
		m := Parse([]byte(raw))
		if strings.ContainsRune(m.Text+m.Subject+m.From, 0) {
			t.Fatalf("%q: NUL survived", raw)
		}
	}
	if m := Parse([]byte("not a message at all")); !strings.Contains(m.Text, "not a message") {
		t.Fatalf("unparseable text lost: %+v", m)
	}
}
