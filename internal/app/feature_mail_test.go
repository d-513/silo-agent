package app_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/emersion/go-smtp"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/mailbox"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm/dummy"
)

const mailTestDomain = "bots.test"

// mailDNS answers the sender checks from a map, so no test asks the network.
type mailDNS struct{ txt map[string][]string }

func mailNotFound(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (d mailDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	if v, ok := d.txt[strings.TrimSuffix(strings.ToLower(name), ".")]; ok {
		return v, nil
	}
	return nil, mailNotFound(name)
}
func (mailDNS) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return nil, mailNotFound(name)
}
func (mailDNS) LookupIPAddr(_ context.Context, name string) ([]net.IPAddr, error) {
	return nil, mailNotFound(name)
}
func (mailDNS) LookupAddr(_ context.Context, addr string) ([]string, error) {
	return nil, mailNotFound(addr)
}

// mailHarness is a control plane that receives mail for @bots.test on a free
// local port.
func mailHarness(t *testing.T, extraYAML string) *apptest.H {
	t.Helper()
	dummy.Reset()
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithDataDir(dir),
		apptest.WithYAML(apptest.DefaultYAML(dir)+"mail:\n  domain: "+mailTestDomain+"\n  addr: 127.0.0.1:0\n"+extraYAML))
	h.App.Mail.DNS = mailDNS{}
	return h
}

func mailAddress(t *testing.T, h *apptest.H, botID string) string {
	t.Helper()
	res, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetMailAddress()
}

func mailBody(from, to, subject, text string) string {
	return "From: " + from + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nMessage-ID: <" + ids.New() + "@example.com>\r\n\r\n" + text + "\r\n"
}

// sendMail hands one message to the harness's SMTP listener, like any mail
// server on the internet would.
func sendMail(h *apptest.H, envelopeFrom, to, raw string) error {
	addr := h.App.Mail.Addr()
	if addr == "" {
		return errors.New("the mail listener is not running")
	}
	// In the clear: senders take STARTTLS without checking the certificate,
	// which this client cannot be told to do.
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("mx.example.com"); err != nil {
		return err
	}
	if err := c.Mail(envelopeFrom, nil); err != nil {
		return err
	}
	if err := c.Rcpt(to, nil); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(raw)); err != nil {
		return err
	}
	return w.Close()
}

func mustSendMail(t *testing.T, h *apptest.H, to, subject, text string) {
	t.Helper()
	if err := sendMail(h, "ada@example.com", to, mailBody("Ada <ada@example.com>", to, subject, text)); err != nil {
		t.Fatalf("send to %s: %v", to, err)
	}
}

func mailList(t *testing.T, h *apptest.H, botID string) *v1.ListMailResponse {
	t.Helper()
	res, err := h.Client.ListMail(h.Ctx(), connect.NewRequest(&v1.ListMailRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

var mailAddrRe = regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+@bots\.test$`)

func TestMailEveryBotHasItsOwnAddress(t *testing.T) {
	h := mailHarness(t, "")
	a, b := h.CreateBot("One"), h.CreateBot("Two")
	addrA, addrB := mailAddress(t, h, a.GetId()), mailAddress(t, h, b.GetId())
	if !mailAddrRe.MatchString(addrA) || !mailAddrRe.MatchString(addrB) || addrA == addrB {
		t.Fatalf("addresses %q %q", addrA, addrB)
	}
	// It is the Bot's for good, not a new one per look.
	if again := mailAddress(t, h, a.GetId()); again != addrA {
		t.Fatalf("address changed: %q then %q", addrA, again)
	}
	bots, err := h.Client.ListBots(h.Ctx(), connect.NewRequest(&v1.ListBotsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, bot := range bots.Msg.GetBots() {
		if bot.GetMailAddress() == "" {
			t.Fatalf("ListBots left %s without its address", bot.GetName())
		}
	}
	box := mailList(t, h, a.GetId()).GetMailbox()
	if box.GetState() != "ok" || box.GetAddress() != addrA || box.GetKeep() != mailbox.Keep || box.GetWake() {
		t.Fatalf("mailbox = %+v", box)
	}
}

// Without a domain there is nothing to receive for: no address, no listener,
// and the Bot is offered no mail tools.
func TestMailNeedsADomain(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Quiet")
	if got := mailAddress(t, h, bot.GetId()); got != "" {
		t.Fatalf("address %q with no mail.domain", got)
	}
	if addr := h.App.Mail.Addr(); addr != "" {
		t.Fatalf("listener on %s with no mail.domain", addr)
	}
	if st := mailList(t, h, bot.GetId()).GetMailbox().GetState(); st != "no_domain" {
		t.Fatalf("state %q", st)
	}
	dummy.Script("Test_mail_none", dummy.Turn{Text: "done"})
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_mail_none_Input")
	h.WaitRun(run)
	names := toolNames("")
	if has(names, "list_mail") || has(names, "read_mail") {
		t.Fatalf("mail tools offered without mail: %v", names)
	}
	if reqs := dummy.Streamed(); strings.Contains(systemText(reqs[len(reqs)-1]), "receive-only") {
		t.Fatal("the prompt mentions a mailbox the Bot does not have")
	}
}

func TestMailOffByTheOperator(t *testing.T) {
	h := mailHarness(t, "  enabled: false\n")
	bot := h.CreateBot("Off")
	if got := mailAddress(t, h, bot.GetId()); got != "" {
		t.Fatalf("address %q with mail off", got)
	}
	if st := mailList(t, h, bot.GetId()).GetMailbox(); st.GetState() != "off" || st.GetAddress() != "" {
		t.Fatalf("mailbox = %+v", st)
	}
	if h.App.Mail.Addr() != "" {
		t.Fatal("listener is up with mail off")
	}
}

func TestMailDeliveredOverSMTPLandsInTheInbox(t *testing.T) {
	h := mailHarness(t, "")
	bot := h.CreateBot("Reader")
	other := h.CreateBot("Other")
	id := bot.GetId()
	addr := mailAddress(t, h, id)

	mustSendMail(t, h, addr, "Your code", "Your code is 482913.\nIt expires in ten minutes.")
	// Upper case and a +tag are still this mailbox.
	local, _, _ := strings.Cut(addr, "@")
	mustSendMail(t, h, strings.ToUpper(local)+"+Shop@"+mailTestDomain, "Receipt", "Thanks for your order.")

	got := mailList(t, h, id).GetMessages()
	if len(got) != 2 {
		t.Fatalf("%d messages", len(got))
	}
	// Newest first.
	if got[0].GetSubject() != "Receipt" || got[1].GetSubject() != "Your code" {
		t.Fatalf("order: %q, %q", got[0].GetSubject(), got[1].GetSubject())
	}
	m := got[1]
	if m.GetFrom() != `"Ada" <ada@example.com>` || m.GetFromAddress() != "ada@example.com" || m.GetRead() || m.GetVerified() {
		t.Fatalf("message = %+v", m)
	}
	if m.GetPreview() != "Your code is 482913. It expires in ten minutes." || m.GetText() != "" || m.GetSize() == 0 || m.GetReceivedAt() == "" {
		t.Fatalf("list row = %+v", m)
	}
	if !strings.Contains(m.GetAuthDetail(), "dkim=none") {
		t.Fatalf("auth detail %q", m.GetAuthDetail())
	}

	full, err := h.Client.GetMail(h.Ctx(), connect.NewRequest(&v1.GetMailRequest{BotId: id, Id: m.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if full.Msg.GetText() != "Your code is 482913.\nIt expires in ten minutes." {
		t.Fatalf("text %q", full.Msg.GetText())
	}
	// The owner looking is not the Bot reading.
	if mailList(t, h, id).GetMessages()[1].GetRead() {
		t.Fatal("the owner opening a message marked it read")
	}
	// Another Bot's mailbox holds nothing of this, and cannot fetch it.
	if n := len(mailList(t, h, other.GetId()).GetMessages()); n != 0 {
		t.Fatalf("the other Bot sees %d messages", n)
	}
	if _, err := h.Client.GetMail(h.Ctx(), connect.NewRequest(&v1.GetMailRequest{BotId: other.GetId(), Id: m.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("cross-Bot read: %v", err)
	}
}

func TestMailToNobodyIsRefused(t *testing.T) {
	h := mailHarness(t, "")
	bot := h.CreateBot("Reader")
	addr := mailAddress(t, h, bot.GetId())
	local, _, _ := strings.Cut(addr, "@")
	for _, to := range []string{"nobody-at-all@" + mailTestDomain, local + "@elsewhere.example", "ada@example.com"} {
		err := sendMail(h, "ada@example.com", to, mailBody("ada@example.com", to, "x", "x"))
		var se *smtp.SMTPError
		if !errors.As(err, &se) || se.Code != 550 {
			t.Fatalf("%s: %v, want 550", to, err)
		}
	}
	if n := len(mailList(t, h, bot.GetId()).GetMessages()); n != 0 {
		t.Fatalf("%d messages", n)
	}
}

// Mail servers retry. The same message twice is one message.
func TestMailRetriedDeliveryIsStoredOnce(t *testing.T) {
	h := mailHarness(t, "")
	bot := h.CreateBot("Reader")
	addr := mailAddress(t, h, bot.GetId())
	raw := mailBody("ada@example.com", addr, "Once", "Only once.")
	for range 3 {
		if err := sendMail(h, "ada@example.com", addr, raw); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(mailList(t, h, bot.GetId()).GetMessages()); n != 1 {
		t.Fatalf("%d messages", n)
	}
}

func TestMailboxKeepsOnlyTheNewest(t *testing.T) {
	h := mailHarness(t, "")
	bot := h.CreateBot("Hoarder")
	id := bot.GetId()
	addr := mailAddress(t, h, id)
	mustSendMail(t, h, addr, "oldest", "first in, first out")
	// Fill it to the brim behind the listener's back.
	var first db.MailMessage
	h.DB.First(&first, "bot_id = ?", id)
	for i := range mailbox.Keep - 1 {
		row := db.MailMessage{ID: ids.New(), BotID: id, Hash: ids.New(), Subject: "filler", CreatedAt: first.CreatedAt.Add(time.Duration(i+1) * time.Microsecond)}
		h.DB.Create(&row)
		h.DB.Create(&db.MailBody{ID: row.ID, BotID: id, Data: []byte("x")})
	}
	mustSendMail(t, h, addr, "newest", "one more")

	var n, bodies int64
	h.DB.Model(&db.MailMessage{}).Where("bot_id = ?", id).Count(&n)
	h.DB.Model(&db.MailBody{}).Where("bot_id = ?", id).Count(&bodies)
	if n != mailbox.Keep || bodies != mailbox.Keep {
		t.Fatalf("%d messages, %d bodies; want %d", n, bodies, mailbox.Keep)
	}
	var gone int64
	h.DB.Model(&db.MailMessage{}).Where("id = ?", first.ID).Count(&gone)
	if gone != 0 {
		t.Fatal("the oldest message is still there")
	}
	if got := mailList(t, h, id).GetMessages(); got[0].GetSubject() != "newest" {
		t.Fatalf("newest = %q", got[0].GetSubject())
	}
}

func TestMailDeleteAndRotate(t *testing.T) {
	h := mailHarness(t, "")
	bot := h.CreateBot("Mover")
	id := bot.GetId()
	old := mailAddress(t, h, id)
	mustSendMail(t, h, old, "keep", "kept")
	mustSendMail(t, h, old, "drop", "dropped")
	msgs := mailList(t, h, id).GetMessages()
	if _, err := h.Client.DeleteMail(h.Ctx(), connect.NewRequest(&v1.DeleteMailRequest{BotId: id, Id: msgs[0].GetId()})); err != nil {
		t.Fatal(err)
	}
	var bodies int64
	h.DB.Model(&db.MailBody{}).Where("bot_id = ?", id).Count(&bodies)
	if left := mailList(t, h, id).GetMessages(); len(left) != 1 || left[0].GetSubject() != "keep" || bodies != 1 {
		t.Fatalf("after delete: %d messages, %d bodies", len(left), bodies)
	}

	res, err := h.Client.RotateMailbox(h.Ctx(), connect.NewRequest(&v1.RotateMailboxRequest{BotId: id}))
	if err != nil {
		t.Fatal(err)
	}
	fresh := res.Msg.GetAddress()
	if fresh == old || !mailAddrRe.MatchString(fresh) || mailAddress(t, h, id) != fresh {
		t.Fatalf("rotated %q to %q", old, fresh)
	}
	// The old address is nobody now; what it received stays.
	err = sendMail(h, "ada@example.com", old, mailBody("ada@example.com", old, "late", "late"))
	var se *smtp.SMTPError
	if !errors.As(err, &se) || se.Code != 550 {
		t.Fatalf("mail to the old address: %v", err)
	}
	mustSendMail(t, h, fresh, "new home", "arrived")
	if left := mailList(t, h, id).GetMessages(); len(left) != 2 || left[0].GetSubject() != "new home" {
		t.Fatalf("after rotate: %+v", left)
	}
}

func TestMailRawDownloadIsTheOwnersOnly(t *testing.T) {
	h := mailHarness(t, "")
	bot := h.CreateBot("Keeper")
	id := bot.GetId()
	addr := mailAddress(t, h, id)
	mustSendMail(t, h, addr, "Original", "Bytes as they came.")
	m := mailList(t, h, id).GetMessages()[0]
	url := h.URL + "/mail/raw?bot_id=" + id + "&id=" + m.GetId()

	res, err := h.HTTP.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "message/rfc822" || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("status %d headers %v", res.StatusCode, res.Header)
	}
	// As it arrived, under the trace line the listener added.
	if !strings.HasPrefix(string(body), "Received: from ") || !strings.Contains(string(body), "by "+mailTestDomain) || !strings.HasSuffix(string(body), "Bytes as they came.\r\n") {
		t.Fatalf("raw = %q", body)
	}

	_, stranger := h.SignedInUser("eve@test.local")
	if res, err := stranger.Get(url); err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("another user: %v %v", res.StatusCode, err)
	}
	if res, err := http.Get(url); err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed out: %v %v", res.StatusCode, err)
	}
}

func TestDeleteBotDropsItsMailbox(t *testing.T) {
	h := mailHarness(t, "")
	bot := h.CreateBot("Doomed")
	id := bot.GetId()
	addr := mailAddress(t, h, id)
	mustSendMail(t, h, addr, "last words", "bye")
	if _, err := h.Client.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id})); err != nil {
		t.Fatal(err)
	}
	for name, model := range map[string]any{"mailboxes": &db.Mailbox{}, "messages": &db.MailMessage{}, "bodies": &db.MailBody{}} {
		var n int64
		h.DB.Model(model).Where("bot_id = ?", id).Count(&n)
		if n != 0 {
			t.Errorf("%d %s left", n, name)
		}
	}
	err := sendMail(h, "ada@example.com", addr, mailBody("ada@example.com", addr, "x", "x"))
	var se *smtp.SMTPError
	if !errors.As(err, &se) || se.Code != 550 {
		t.Fatalf("mail to a deleted Bot: %v", err)
	}
}

// The listener follows Admin → Settings: no restart to turn mail on or off.
func TestMailListenerFollowsTheSettings(t *testing.T) {
	dummy.Reset()
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)+"mail:\n  addr: 127.0.0.1:0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	h.App.Mail.DNS = mailDNS{}
	bot := h.CreateBot("Late")
	if h.App.Mail.Addr() != "" {
		t.Fatal("listening before a domain is set")
	}
	put := func(fields map[string]string) {
		t.Helper()
		if _, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: fields})); err != nil {
			t.Fatal(err)
		}
		h.App.Mail.Sync() // what the tick does every few seconds
	}
	put(map[string]string{"mail.domain": mailTestDomain})
	if h.App.Mail.Addr() == "" {
		t.Fatal("not listening after mail.domain was set")
	}
	addr := mailAddress(t, h, bot.GetId())
	mustSendMail(t, h, addr, "hello", "now it works")

	// A new domain moves every address at once; the old one is refused.
	put(map[string]string{"mail.domain": "moved.test"})
	moved := mailAddress(t, h, bot.GetId())
	if !strings.HasSuffix(moved, "@moved.test") || strings.Split(moved, "@")[0] != strings.Split(addr, "@")[0] {
		t.Fatalf("%q after the domain moved from %q", moved, addr)
	}
	if err := sendMail(h, "ada@example.com", addr, mailBody("ada@example.com", addr, "x", "x")); err == nil {
		t.Fatal("the old domain still receives")
	}
	mustSendMail(t, h, moved, "moved", "still here")

	put(map[string]string{"mail.enabled": "false"})
	if h.App.Mail.Addr() != "" {
		t.Fatal("still listening with mail off")
	}
	if n := len(mailList(t, h, bot.GetId()).GetMessages()); n != 2 {
		t.Fatalf("turning mail off lost mail: %d messages", n)
	}
}

func TestPutSettingsRefusesABadMailDomain(t *testing.T) {
	dummy.Reset()
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	for _, fields := range []map[string]string{
		{"mail.domain": "bots.example.com:25"},
		{"mail.addr": "nonsense"},
		{"mail.max_size_mb": "9000"},
		{"mail.tls_cert": "/only/half.crt"},
	} {
		if _, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: fields})); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%v: %v, want InvalidArgument", fields, err)
		}
	}
}
