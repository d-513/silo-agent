package mailbox

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

const (
	// wakePerHour caps the chats one mailbox may start in an hour; mail past
	// it still lands in the inbox.
	wakePerHour = 10
	// wakeFromMax caps the senders on a wake list.
	wakeFromMax = 50
	// wakeBodyMax is how much of the body the wake message quotes.
	wakeBodyMax = 6000
)

// NormalizeWakeFrom cleans a wake list: one sender per line, each an address
// (ada@example.com) or a whole domain (@example.com), lower-cased, without
// repeats.
func NormalizeWakeFrom(raw string) (string, error) {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ';' }) {
		e := strings.ToLower(strings.TrimSpace(line))
		if e == "" {
			continue
		}
		if !strings.Contains(e, "@") {
			e = "@" + e // a bare domain
		}
		_, domain, _ := strings.Cut(e, "@")
		if strings.ContainsAny(e, " \t<>\"") || strings.Contains(domain, "@") || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
			return "", fmt.Errorf("%q is not an address or a domain: write ada@example.com or @example.com", line)
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	if len(out) > wakeFromMax {
		return "", fmt.Errorf("a wake list holds at most %d senders", wakeFromMax)
	}
	return strings.Join(out, "\n"), nil
}

// onWakeList reports whether addr is a sender the owner listed.
func onWakeList(list, addr string) bool {
	addr = strings.ToLower(strings.TrimSpace(addr))
	_, domain, ok := strings.Cut(addr, "@")
	if !ok || domain == "" {
		return false
	}
	for _, e := range strings.Split(list, "\n") {
		if e != "" && (e == addr || e == "@"+domain) {
			return true
		}
	}
	return false
}

// wake starts a chat for a message the owner asked to be woken for: wake is
// on, the sender is on the list, and the sender's domain vouched for the
// message. An unverified From address wakes nobody, because writing one is
// free; the message is in the inbox either way.
func (s *Service) wake(box *db.Mailbox, m *db.MailMessage) {
	if !box.Wake || !m.Verified || !onWakeList(box.WakeFrom, m.SenderAddr) {
		return
	}
	var recent int64
	s.db.Model(&db.MailMessage{}).Where("bot_id = ? AND chat_id <> '' AND created_at > ?", m.BotID, time.Now().Add(-time.Hour)).Count(&recent)
	if recent >= wakePerHour {
		log.Printf("mail: %s not woken for %s: %d chats started in the last hour", m.BotID, m.ID, recent)
		return
	}
	subject := m.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	now := time.Now()
	chat := db.Chat{ID: ids.New(), BotID: m.BotID, Title: textx.ClipRunes("Mail: "+subject, 80), CreatedAt: now, UpdatedAt: now}
	if err := s.db.Create(&chat).Error; err != nil {
		log.Printf("mail: wake chat for %s: %v", m.ID, err)
		return
	}
	if _, err := s.engine.StartRun(run.Request{BotID: m.BotID, ChatID: chat.ID, Text: wakeText(m), From: "mail"}); err != nil {
		// A disabled owner, a Bot that is gone: the mail stays in the inbox.
		s.db.Delete(&chat)
		if !errors.Is(err, run.ErrBusy) {
			log.Printf("mail: wake run for %s: %v", m.ID, err)
		}
		return
	}
	s.db.Model(m).Update("chat_id", chat.ID)
	m.ChatID = chat.ID
}

// wakeText is the opening message of a woken chat: who wrote, what, and that
// the answer goes here, since the mailbox cannot reply.
func wakeText(m *db.MailMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "New mail in your mailbox from %s. The sender is verified and on my wake list, so I asked you to act on it.\n\n", m.Sender)
	fmt.Fprintf(&b, "Subject: %s\n", orNone(m.Subject))
	if n := len(attachments(m)); n > 0 {
		fmt.Fprintf(&b, "Attachments: %d (read_mail with save_attachments=true writes them to the workspace)\n", n)
	}
	fmt.Fprintf(&b, "Message id: %s\n\n", m.ID)
	body := strings.TrimSpace(m.Text)
	if body == "" {
		body = "(no text)"
	}
	for _, line := range strings.Split(textx.CapRunes(body, wakeBodyMax), "\n") {
		b.WriteString("> " + line + "\n")
	}
	b.WriteString("\nDo what it asks if that is something I would want, and answer here: the mailbox only receives, you cannot reply by email.")
	return b.String()
}
