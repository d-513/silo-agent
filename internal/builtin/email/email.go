// Package email is the built-in Email connector: IMAP for reading, SMTP for
// sending. The password stays on the Control Plane; the Bot only sees
// `tools.<slug>` functions.
package email

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"silo.agent/internal/builtin"
	"silo.agent/internal/security"
)

//go:embed GUIDE.md
var guide string

const (
	secTLS      = "tls"
	secStartTLS = "starttls"
	secNone     = "none"

	// maxMessage bounds one fetched message; maxAttach bounds outgoing files.
	maxMessage = 50 << 20
	maxAttach  = 25 << 20
	maxLimit   = 100
)

// preset is a mail provider's well-known endpoints. sentSaved means the
// provider files SMTP submissions in Sent itself, so an APPEND would
// duplicate them.
type preset struct {
	imapHost  string
	imapPort  int
	imapSec   string
	smtpHost  string
	smtpPort  int
	smtpSec   string
	sentSaved bool
}

var presets = map[string]preset{
	"gmail":    {"imap.gmail.com", 993, secTLS, "smtp.gmail.com", 465, secTLS, true},
	"outlook":  {"outlook.office365.com", 993, secTLS, "smtp.office365.com", 587, secStartTLS, true},
	"icloud":   {"imap.mail.me.com", 993, secTLS, "smtp.mail.me.com", 587, secStartTLS, false},
	"fastmail": {"imap.fastmail.com", 993, secTLS, "smtp.fastmail.com", 465, secTLS, false},
	"yahoo":    {"imap.mail.yahoo.com", 993, secTLS, "smtp.mail.yahoo.com", 465, secTLS, false},
}

type connector struct{}

func init() { builtin.Register(connector{}) }

var securityOptions = []builtin.Option{
	{Value: "", Label: "Provider default"},
	{Value: secTLS, Label: "TLS"},
	{Value: secStartTLS, Label: "STARTTLS"},
	{Value: secNone, Label: "None (plain text, local servers only)"},
}

func (connector) Descriptor() builtin.Descriptor {
	folder := builtin.Prop("string", "Mailbox folder; default INBOX.")
	uid := builtin.Prop("integer", "Message UID from search.")
	uids := builtin.List("integer", "Message UIDs from search.")
	return builtin.Descriptor{
		Key:         "email",
		Name:        "Email",
		Description: "Read, search, and send mail over IMAP and SMTP.",
		Category:    "Email",
		Guide:       guide,
		Prompt: "Mail is addressed by `folder` + `uid` (from `search`). `read` returns plain text and an attachment list; " +
			"`save_attachment` writes one into /workspace. `send`/`reply` take `attachments` as /workspace paths. " +
			"Draft the message in the chat and let the human confirm before sending.",
		Fields: []builtin.Field{
			{Key: "provider", Label: "Provider", Type: builtin.FieldSelect, Default: "gmail",
				Description: "Fills in the server settings. Pick Other for anything else.",
				Options: []builtin.Option{
					{Value: "gmail", Label: "Gmail"}, {Value: "outlook", Label: "Outlook / Microsoft 365"},
					{Value: "icloud", Label: "iCloud"}, {Value: "fastmail", Label: "Fastmail"},
					{Value: "yahoo", Label: "Yahoo"}, {Value: "custom", Label: "Other"},
				}},
			{Key: "address", Label: "Email address", Type: builtin.FieldText, Required: true},
			{Key: "password", Label: "App password", Type: builtin.FieldSecret, Required: true,
				Description: "An app password, not your account password. See the guide for your provider."},
			{Key: "username", Label: "Username", Advanced: true, Type: builtin.FieldText,
				Description: "Leave blank to sign in with the email address."},
			{Key: "from_name", Label: "Sender name", Type: builtin.FieldText,
				Description: "Shown next to the address on sent mail."},
			{Key: "imap_host", Label: "IMAP server", Advanced: true, Type: builtin.FieldText,
				Description: "Leave blank to use the provider's."},
			{Key: "imap_port", Label: "IMAP port", Advanced: true, Type: builtin.FieldNumber},
			{Key: "imap_security", Label: "IMAP security", Advanced: true, Type: builtin.FieldSelect, Options: securityOptions},
			{Key: "smtp_host", Label: "SMTP server", Advanced: true, Type: builtin.FieldText,
				Description: "Leave blank to use the provider's."},
			{Key: "smtp_port", Label: "SMTP port", Advanced: true, Type: builtin.FieldNumber},
			{Key: "smtp_security", Label: "SMTP security", Advanced: true, Type: builtin.FieldSelect, Options: securityOptions},
			{Key: "save_sent", Label: "Save a copy in Sent", Advanced: true, Type: builtin.FieldToggle, Default: "true",
				Description: "Gmail and Outlook do this themselves; it is skipped for them."},
		},
		Tools: []builtin.Tool{
			{Name: "list_folders", Mode: security.Allow,
				Description: "List mailbox folders with their role (sent, trash, …) and unseen/total counts.",
				Params:      builtin.Object(nil), Run: listFolders},
			{Name: "search", Mode: security.Allow,
				Description: "Find messages in a folder, newest first. Returns uid, date, from, to, subject, flags, has_attachments.",
				Params: builtin.Object(map[string]any{
					"folder":  folder,
					"query":   builtin.Prop("string", "Full-text search over headers and body."),
					"from":    builtin.Prop("string", "Sender contains."),
					"to":      builtin.Prop("string", "Recipient contains."),
					"subject": builtin.Prop("string", "Subject contains."),
					"since":   builtin.Prop("string", "On or after this date (YYYY-MM-DD)."),
					"before":  builtin.Prop("string", "Before this date (YYYY-MM-DD)."),
					"unseen":  builtin.Prop("boolean", "Only unread messages."),
					"limit":   builtin.Prop("integer", "Most results to return (default 20, max 100)."),
				}), Run: search},
			{Name: "read", Mode: security.Allow,
				Description: "Read one message: headers, plain-text body, and its attachments (index, filename, size).",
				Params: builtin.Object(map[string]any{
					"folder": folder, "uid": uid,
					"max_chars": builtin.Prop("integer", "Cap on the body text (default 20000)."),
					"mark_read": builtin.Prop("boolean", "Mark it read (default false)."),
				}, "uid"), Run: read},
			{Name: "send", Mode: security.Ask,
				Description: "Send a new message. attachments are /workspace paths.",
				Params: builtin.Object(map[string]any{
					"to":          builtin.List("string", "Recipients."),
					"cc":          builtin.List("string", "Cc recipients."),
					"bcc":         builtin.List("string", "Bcc recipients."),
					"subject":     builtin.Prop("string", "Subject line."),
					"body":        builtin.Prop("string", "Message body."),
					"html":        builtin.Prop("boolean", "body is HTML (default plain text)."),
					"attachments": builtin.List("string", "Workspace file paths to attach."),
				}, "to", "subject", "body"), Run: send},
			{Name: "reply", Mode: security.Ask,
				Description: "Reply to a message, threaded, quoting the original. attachments are /workspace paths.",
				Params: builtin.Object(map[string]any{
					"folder": folder, "uid": uid,
					"body":        builtin.Prop("string", "Reply text (the original is quoted below it)."),
					"reply_all":   builtin.Prop("boolean", "Also reply to the other recipients."),
					"html":        builtin.Prop("boolean", "body is HTML (default plain text)."),
					"attachments": builtin.List("string", "Workspace file paths to attach."),
				}, "uid", "body"), Run: reply},
			{Name: "move", Mode: security.Ask,
				Description: "Move messages to another folder.",
				Params: builtin.Object(map[string]any{
					"folder": folder, "uids": uids,
					"to_folder": builtin.Prop("string", "Destination folder."),
				}, "uids", "to_folder"), Run: move},
			{Name: "set_flags", Mode: security.Allow,
				Description: "Mark messages read/unread or flagged/unflagged. Omit a flag to leave it.",
				Params: builtin.Object(map[string]any{
					"folder": folder, "uids": uids,
					"seen":    builtin.Prop("boolean", "true = read, false = unread."),
					"flagged": builtin.Prop("boolean", "true = flagged/starred."),
				}, "uids"), Run: setFlags},
			{Name: "delete", Mode: security.Ask,
				Description: "Move messages to Trash (removes them for good only if there is no Trash folder or they are already in it).",
				Params:      builtin.Object(map[string]any{"folder": folder, "uids": uids}, "uids"), Run: remove},
			{Name: "save_attachment", Mode: security.Allow,
				Description: "Save one attachment of a message into /workspace. Returns the path.",
				Params: builtin.Object(map[string]any{
					"folder": folder, "uid": uid,
					"index": builtin.Prop("integer", "Attachment index from read."),
					"path":  builtin.Prop("string", "Workspace path to write (default mail/<filename>)."),
				}, "uid", "index"), Run: saveAttachment},
		},
	}
}

func (connector) Check(ctx context.Context, cfg builtin.Config) error {
	s, err := resolve(cfg)
	if err != nil {
		return err
	}
	c, err := dialIMAP(ctx, s)
	if err != nil {
		return err
	}
	c.close()
	sc, err := dialSMTP(ctx, s)
	if err != nil {
		return err
	}
	_ = sc.Quit()
	return nil
}

// settings is a resolved config: provider defaults filled in.
type settings struct {
	provider  string
	address   string
	username  string
	password  string
	fromName  string
	imapAddr  string
	imapHost  string
	imapSec   string
	smtpAddr  string
	smtpHost  string
	smtpSec   string
	saveSent  bool
	sentSaved bool
}

func resolve(cfg builtin.Config) (settings, error) {
	p := presets[cfg.Get("provider")]
	s := settings{
		provider: cfg.Get("provider"),
		address:  cfg.Get("address"),
		username: cfg.Get("username"),
		password: cfg.Values["password"],
		fromName: cfg.Get("from_name"),
		saveSent: cfg.Bool("save_sent"),
	}
	s.sentSaved = p.sentSaved
	if s.address == "" {
		return s, errors.New("set the email address")
	}
	if s.username == "" {
		s.username = s.address
	}
	s.imapHost = or(cfg.Get("imap_host"), p.imapHost)
	s.smtpHost = or(cfg.Get("smtp_host"), p.smtpHost)
	if s.imapHost == "" {
		return s, errors.New("set the IMAP server")
	}
	if s.smtpHost == "" {
		return s, errors.New("set the SMTP server")
	}
	s.imapSec = or(cfg.Get("imap_security"), p.imapSec, secTLS)
	s.smtpSec = or(cfg.Get("smtp_security"), p.smtpSec, secStartTLS)
	imapPort := cfg.Int("imap_port", p.imapPort)
	if imapPort == 0 {
		imapPort = 993
		if s.imapSec != secTLS {
			imapPort = 143
		}
	}
	smtpPort := cfg.Int("smtp_port", p.smtpPort)
	if smtpPort == 0 {
		smtpPort = 587
		if s.smtpSec == secTLS {
			smtpPort = 465
		}
	}
	for _, v := range []string{s.imapSec, s.smtpSec} {
		if v != secTLS && v != secStartTLS && v != secNone {
			return s, fmt.Errorf("unknown security %q", v)
		}
	}
	s.imapAddr = net.JoinHostPort(s.imapHost, strconv.Itoa(imapPort))
	s.smtpAddr = net.JoinHostPort(s.smtpHost, strconv.Itoa(smtpPort))
	return s, nil
}

func or(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}
