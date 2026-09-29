package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/mail"

	"silo.agent/internal/builtin"
	"silo.agent/internal/builtin/email/emailtest"
)

type memEnv struct{ files map[string][]byte }

func (e *memEnv) ReadFile(_ context.Context, rel string) (string, []byte, error) {
	b, ok := e.files[rel]
	if !ok {
		return "", nil, errors.New("no such file")
	}
	return rel[strings.LastIndex(rel, "/")+1:], b, nil
}

func (e *memEnv) WriteFile(_ context.Context, rel string, data []byte) error {
	e.files[rel] = data
	return nil
}

func setup(t *testing.T) (*emailtest.Server, builtin.Config, *memEnv) {
	t.Helper()
	srv := emailtest.Start(t)
	d := connector{}.Descriptor()
	return srv, builtin.Resolve(d, srv.Config()), &memEnv{files: map[string][]byte{}}
}

func call(t *testing.T, name string, env builtin.Env, cfg builtin.Config, args map[string]any) map[string]any {
	t.Helper()
	out, err := try(name, env, cfg, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

func try(name string, env builtin.Env, cfg builtin.Config, args map[string]any) (map[string]any, error) {
	tool, ok := connector{}.Descriptor().Tool(name)
	if !ok {
		return nil, errors.New("no tool " + name)
	}
	raw, _ := json.Marshal(args)
	v, err := tool.Run(context.Background(), env, cfg, raw)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out, nil
}

const plainMsg = "From: Alice <alice@example.org>\r\nTo: me@example.com\r\nSubject: Lunch plans\r\n" +
	"Message-ID: <lunch-1@example.org>\r\nDate: Mon, 02 Mar 2026 10:00:00 +0000\r\n\r\nPizza at noon?\r\n"

const htmlMsg = "From: News <news@example.net>\r\nTo: me@example.com\r\nSubject: Weekly digest\r\n" +
	"Date: Tue, 03 Mar 2026 10:00:00 +0000\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
	"<html><head><style>p{}</style></head><body><p>Hello <b>there</b></p><p><a href=\"https://x.test/a\">Read more</a></p></body></html>\r\n"

const attachMsg = "From: Bob <bob@example.org>\r\nTo: me@example.com\r\nCc: carol@example.org\r\nSubject: Report\r\n" +
	"Message-ID: <report-1@example.org>\r\nDate: Wed, 04 Mar 2026 10:00:00 +0000\r\n" +
	"Content-Type: multipart/mixed; boundary=XX\r\n\r\n" +
	"--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSee attached.\r\n" +
	"--XX\r\nContent-Type: text/csv\r\nContent-Disposition: attachment; filename=\"q1.csv\"\r\n\r\na,b\r\n1,2\r\n" +
	"--XX--\r\n"

func TestCheck(t *testing.T) {
	srv, cfg, _ := setup(t)
	if err := (connector{}).Check(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	bad := srv.Config()
	bad["password"] = "nope"
	err := (connector{}).Check(context.Background(), builtin.Resolve(connector{}.Descriptor(), bad))
	if err == nil || !strings.Contains(err.Error(), "IMAP login failed") {
		t.Fatalf("want login failure, got %v", err)
	}
}

func TestResolveProviderDefaults(t *testing.T) {
	d := connector{}.Descriptor()
	s, err := resolve(builtin.Resolve(d, map[string]string{"provider": "outlook", "address": "a@b.c", "password": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if s.imapAddr != "outlook.office365.com:993" || s.smtpAddr != "smtp.office365.com:587" || s.smtpSec != secStartTLS || !s.sentSaved || s.username != "a@b.c" {
		t.Fatalf("%+v", s)
	}
	if _, err := resolve(builtin.Resolve(d, map[string]string{"provider": "custom", "address": "a@b.c"})); err == nil {
		t.Fatal("custom without hosts should fail")
	}
}

func TestSearchAndRead(t *testing.T) {
	srv, cfg, env := setup(t)
	u1 := srv.Deliver(t, "INBOX", plainMsg)
	u2 := srv.Deliver(t, "INBOX", htmlMsg, imap.FlagSeen)
	u3 := srv.Deliver(t, "INBOX", attachMsg)

	out := call(t, "search", env, cfg, nil)
	msgs := out["messages"].([]any)
	if len(msgs) != 3 || out["total"].(float64) != 3 {
		t.Fatalf("search all: %v", out)
	}
	first := msgs[0].(map[string]any)
	if uint32(first["uid"].(float64)) != u3 || first["has_attachments"] != true || first["subject"] != "Report" {
		t.Fatalf("newest first with attachment flag: %v", first)
	}
	if !strings.Contains(first["from"].(string), "bob@example.org") {
		t.Fatalf("from: %v", first["from"])
	}

	out = call(t, "search", env, cfg, map[string]any{"unseen": true, "limit": 1})
	if len(out["messages"].([]any)) != 1 || out["total"].(float64) != 2 {
		t.Fatalf("unseen+limit: %v", out)
	}
	out = call(t, "search", env, cfg, map[string]any{"subject": "lunch"})
	if got := out["messages"].([]any); len(got) != 1 || uint32(got[0].(map[string]any)["uid"].(float64)) != u1 {
		t.Fatalf("subject: %v", out)
	}

	r := call(t, "read", env, cfg, map[string]any{"uid": u1})
	if r["text"] != "Pizza at noon?\r\n" && strings.TrimSpace(r["text"].(string)) != "Pizza at noon?" {
		t.Fatalf("plain body: %q", r["text"])
	}
	if r["message_id"] != "lunch-1@example.org" {
		t.Fatalf("message id: %v", r["message_id"])
	}
	// Peek: reading did not mark it seen.
	if out := call(t, "search", env, cfg, map[string]any{"unseen": true}); out["total"].(float64) != 2 {
		t.Fatalf("read should not mark seen: %v", out)
	}

	r = call(t, "read", env, cfg, map[string]any{"uid": u2})
	if txt := r["text"].(string); !strings.Contains(txt, "Hello there") || !strings.Contains(txt, "Read more (https://x.test/a)") || strings.Contains(txt, "p{}") {
		t.Fatalf("html body: %q", txt)
	}

	r = call(t, "read", env, cfg, map[string]any{"uid": u3, "mark_read": true, "max_chars": 5})
	atts := r["attachments"].([]any)
	if len(atts) != 1 || atts[0].(map[string]any)["filename"] != "q1.csv" || r["truncated"] != true {
		t.Fatalf("attachments/truncate: %v", r)
	}
	if out := call(t, "search", env, cfg, map[string]any{"unseen": true}); out["total"].(float64) != 1 {
		t.Fatalf("mark_read: %v", out)
	}

	sv := call(t, "save_attachment", env, cfg, map[string]any{"uid": u3, "index": 0})
	if sv["path"] != "mail/q1.csv" || string(env.files["mail/q1.csv"]) != "a,b\r\n1,2" {
		t.Fatalf("save: %v %q", sv, env.files["mail/q1.csv"])
	}
	if _, err := try("save_attachment", env, cfg, map[string]any{"uid": u3, "index": 3}); err == nil {
		t.Fatal("missing index should fail")
	}
}

func TestFlagsMoveDelete(t *testing.T) {
	srv, cfg, env := setup(t)
	u1 := srv.Deliver(t, "INBOX", plainMsg)
	u2 := srv.Deliver(t, "INBOX", htmlMsg)

	call(t, "set_flags", env, cfg, map[string]any{"uids": []uint32{u1}, "seen": true, "flagged": true})
	r := call(t, "read", env, cfg, map[string]any{"uid": u1})
	flags := strings.Join(toStrings(r["flags"]), ",")
	if !strings.Contains(flags, "seen") || !strings.Contains(flags, "flagged") {
		t.Fatalf("flags: %v", flags)
	}
	call(t, "set_flags", env, cfg, map[string]any{"uids": []uint32{u1}, "flagged": false})
	if flags := strings.Join(toStrings(call(t, "read", env, cfg, map[string]any{"uid": u1})["flags"]), ","); strings.Contains(flags, "flagged") {
		t.Fatalf("unflag: %v", flags)
	}

	call(t, "move", env, cfg, map[string]any{"uids": []uint32{u1}, "to_folder": "Archive"})
	if out := call(t, "search", env, cfg, map[string]any{"folder": "Archive"}); out["total"].(float64) != 1 {
		t.Fatalf("archive: %v", out)
	}

	del := call(t, "delete", env, cfg, map[string]any{"uids": []uint32{u2}})
	if del["moved_to"] != "Trash" {
		t.Fatalf("delete should move to Trash: %v", del)
	}
	if out := call(t, "search", env, cfg, nil); out["total"].(float64) != 0 {
		t.Fatalf("inbox empty: %v", out)
	}
	trash := call(t, "search", env, cfg, map[string]any{"folder": "Trash"})
	uid := uint32(trash["messages"].([]any)[0].(map[string]any)["uid"].(float64))
	del = call(t, "delete", env, cfg, map[string]any{"folder": "Trash", "uids": []uint32{uid}})
	if del["permanent"] != true {
		t.Fatalf("delete in Trash is permanent: %v", del)
	}
	if out := call(t, "search", env, cfg, map[string]any{"folder": "Trash"}); out["total"].(float64) != 0 {
		t.Fatalf("trash empty: %v", out)
	}

	out := call(t, "list_folders", env, cfg, nil)
	names := []string{}
	for _, f := range out["folders"].([]any) {
		names = append(names, f.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") == "" || !strings.Contains(strings.Join(names, ","), "Archive") {
		t.Fatalf("folders: %v", names)
	}
}

func TestSendWithAttachmentSavesSent(t *testing.T) {
	srv, cfg, env := setup(t)
	env.files["reports/q1.pdf"] = []byte("%PDF-1.4 fake")
	out := call(t, "send", env, cfg, map[string]any{
		"to": "dave@example.org, Eve <eve@example.org>", "bcc": []string{"boss@example.org"},
		"subject": "Q1 numbers", "body": "Attached.", "attachments": []string{"reports/q1.pdf"},
	})
	if out["sent"] != true || out["saved_to"] != "Sent" {
		t.Fatalf("send: %v", out)
	}
	sent := srv.Sent()
	if len(sent) != 1 {
		t.Fatalf("smtp got %d", len(sent))
	}
	m := sent[0]
	if m.From != emailtest.Address || strings.Join(m.To, ",") != "dave@example.org,eve@example.org,boss@example.org" {
		t.Fatalf("envelope: %+v", m)
	}
	if bytes.Contains(m.Data, []byte("boss@example.org")) {
		t.Fatal("bcc leaked into headers")
	}
	r, err := mail.CreateReader(bytes.NewReader(m.Data))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := r.Header.Subject(); s != "Q1 numbers" {
		t.Fatalf("subject %q", s)
	}
	var gotFile string
	for {
		p, err := r.NextPart()
		if err != nil {
			break
		}
		if ah, ok := p.Header.(*mail.AttachmentHeader); ok {
			gotFile, _ = ah.Filename()
		}
	}
	if gotFile != "q1.pdf" {
		t.Fatalf("attachment %q", gotFile)
	}
	if out := call(t, "search", env, cfg, map[string]any{"folder": "Sent"}); out["total"].(float64) != 1 {
		t.Fatalf("sent copy: %v", out)
	}

	if _, err := try("send", env, cfg, map[string]any{"to": "x@y.z", "subject": "s", "body": "b", "attachments": []string{"nope"}}); err == nil {
		t.Fatal("missing attachment should fail")
	}
	if _, err := try("send", env, cfg, map[string]any{"subject": "s", "body": "b"}); err == nil {
		t.Fatal("no recipients should fail")
	}
}

// A .txt attachment's registered type carries params ("text/plain;
// charset=utf-8"); that once produced an empty Content-Type header, which
// smtp4dev could not parse and stalled BODYSTRUCTURE forever.
func TestAttachmentContentTypeNeverEmpty(t *testing.T) {
	srv, cfg, env := setup(t)
	env.files["note.txt"] = []byte("hello")
	call(t, "send", env, cfg, map[string]any{
		"to": "dave@example.org", "subject": "s", "body": "b", "attachments": []string{"note.txt"},
	})
	data := srv.Sent()[0].Data
	if bytes.Contains(data, []byte("Content-Type: \r\n")) || !bytes.Contains(data, []byte("text/plain")) {
		t.Fatalf("attachment content type:\n%s", data)
	}
}

func TestReplyThreadsAndQuotes(t *testing.T) {
	srv, cfg, env := setup(t)
	uid := srv.Deliver(t, "INBOX", attachMsg)
	out := call(t, "reply", env, cfg, map[string]any{"uid": uid, "body": "Thanks!", "reply_all": true})
	if out["subject"] != "Re: Report" {
		t.Fatalf("reply: %v", out)
	}
	m := srv.Sent()[0]
	if strings.Join(m.To, ",") != "bob@example.org,carol@example.org" {
		t.Fatalf("reply-all rcpts (self excluded): %v", m.To)
	}
	r, err := mail.CreateReader(bytes.NewReader(m.Data))
	if err != nil {
		t.Fatal(err)
	}
	irt, _ := r.Header.MsgIDList("In-Reply-To")
	refs, _ := r.Header.MsgIDList("References")
	if len(irt) != 1 || irt[0] != "report-1@example.org" || len(refs) != 1 {
		t.Fatalf("threading: %v %v", irt, refs)
	}
	p, _ := r.NextPart()
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(p.Body)
	if !strings.HasPrefix(body.String(), "Thanks!") || !strings.Contains(body.String(), "> See attached.") {
		t.Fatalf("quote: %q", body.String())
	}
	rd := call(t, "read", env, cfg, map[string]any{"uid": uid})
	if !strings.Contains(strings.Join(toStrings(rd["flags"]), ","), "answered") {
		t.Fatalf("answered flag: %v", rd["flags"])
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"../../etc/passwd": "passwd", "": "attachment-2", "a\\b.txt": "b.txt", "..": "attachment-2"} {
		if got := safeName(in, 2); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
