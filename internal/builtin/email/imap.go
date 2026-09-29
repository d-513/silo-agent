package email

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"silo.agent/internal/builtin"
)

const dialTimeout = 30 * time.Second

// mailbox is one logged-in IMAP connection. It closes itself if the call's
// context ends, which unblocks any command waiting on the server.
type mailbox struct {
	*imapclient.Client
	stop func() bool
}

func dialIMAP(ctx context.Context, s settings) (*mailbox, error) {
	d := &net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", s.imapAddr)
	if err != nil {
		return nil, fmt.Errorf("IMAP connect: %w", err)
	}
	opts := &imapclient.Options{}
	var c *imapclient.Client
	switch s.imapSec {
	case secTLS:
		c = imapclient.New(tls.Client(conn, &tls.Config{ServerName: s.imapHost}), opts)
	case secStartTLS:
		opts.TLSConfig = &tls.Config{ServerName: s.imapHost}
		c, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("IMAP STARTTLS: %w", err)
		}
	default:
		c = imapclient.New(conn, opts)
	}
	m := &mailbox{Client: c}
	m.stop = context.AfterFunc(ctx, func() { c.Close() })
	if err := c.WaitGreeting(); err != nil {
		m.close()
		return nil, fmt.Errorf("IMAP connect: %w", err)
	}
	if err := c.Login(s.username, s.password).Wait(); err != nil {
		m.close()
		return nil, fmt.Errorf("IMAP login failed: %w", err)
	}
	return m, nil
}

func (m *mailbox) close() {
	m.stop()
	_ = m.Logout().Wait()
	_ = m.Close()
}

// open resolves the config, logs in, and (when folder is set) selects it.
func open(ctx context.Context, cfg builtin.Config, folder string, readOnly bool) (*mailbox, settings, error) {
	s, err := resolve(cfg)
	if err != nil {
		return nil, s, err
	}
	m, err := dialIMAP(ctx, s)
	if err != nil {
		return nil, s, err
	}
	if folder != "" {
		if _, err := m.Select(folder, &imap.SelectOptions{ReadOnly: readOnly}).Wait(); err != nil {
			m.close()
			return nil, s, fmt.Errorf("open folder %q: %w", folder, err)
		}
	}
	return m, s, nil
}

func folderOf(f string) string {
	if strings.TrimSpace(f) == "" {
		return "INBOX"
	}
	return strings.TrimSpace(f)
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid args: %w", err)
	}
	return nil
}

// --- list_folders ---

type folderInfo struct {
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	Unseen uint32 `json:"unseen"`
	Total  uint32 `json:"total"`
}

func listFolders(ctx context.Context, _ builtin.Env, cfg builtin.Config, _ json.RawMessage) (any, error) {
	m, _, err := open(ctx, cfg, "", true)
	if err != nil {
		return nil, err
	}
	defer m.close()
	list, err := m.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		return nil, err
	}
	out := []folderInfo{}
	for _, l := range list {
		if hasAttr(l.Attrs, imap.MailboxAttrNoSelect) || hasAttr(l.Attrs, imap.MailboxAttrNonExistent) {
			continue
		}
		f := folderInfo{Name: l.Mailbox, Role: roleOf(l)}
		if st, err := m.Status(l.Mailbox, &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait(); err == nil {
			if st.NumMessages != nil {
				f.Total = *st.NumMessages
			}
			if st.NumUnseen != nil {
				f.Unseen = *st.NumUnseen
			}
		}
		out = append(out, f)
	}
	return map[string]any{"folders": out}, nil
}

var roles = map[imap.MailboxAttr]string{
	imap.MailboxAttrSent: "sent", imap.MailboxAttrTrash: "trash", imap.MailboxAttrDrafts: "drafts",
	imap.MailboxAttrJunk: "junk", imap.MailboxAttrArchive: "archive", imap.MailboxAttrAll: "all",
	imap.MailboxAttrFlagged: "flagged",
}

func roleOf(l *imap.ListData) string {
	if strings.EqualFold(l.Mailbox, "INBOX") {
		return "inbox"
	}
	for _, a := range l.Attrs {
		if r, ok := roles[a]; ok {
			return r
		}
	}
	return ""
}

func hasAttr(attrs []imap.MailboxAttr, want imap.MailboxAttr) bool {
	for _, a := range attrs {
		if strings.EqualFold(string(a), string(want)) {
			return true
		}
	}
	return false
}

// findRole returns the folder with a special-use attribute, falling back to a
// folder with a conventional name.
func (m *mailbox) findRole(attr imap.MailboxAttr, names ...string) string {
	list, err := m.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		return ""
	}
	for _, l := range list {
		if hasAttr(l.Attrs, attr) {
			return l.Mailbox
		}
	}
	for _, n := range names {
		for _, l := range list {
			if strings.EqualFold(l.Mailbox, n) {
				return l.Mailbox
			}
		}
	}
	return ""
}

// --- search ---

type searchArgs struct {
	Folder  string `json:"folder"`
	Query   string `json:"query"`
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Since   string `json:"since"`
	Before  string `json:"before"`
	Unseen  bool   `json:"unseen"`
	Limit   int    `json:"limit"`
}

type summary struct {
	UID            uint32   `json:"uid"`
	Date           string   `json:"date"`
	From           string   `json:"from"`
	To             []string `json:"to"`
	Subject        string   `json:"subject"`
	Flags          []string `json:"flags"`
	Size           int64    `json:"size"`
	HasAttachments bool     `json:"has_attachments"`
}

func search(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a searchArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	crit := &imap.SearchCriteria{}
	if q := strings.TrimSpace(a.Query); q != "" {
		crit.Text = []string{q}
	}
	for k, v := range map[string]string{"From": a.From, "To": a.To, "Subject": a.Subject} {
		if v = strings.TrimSpace(v); v != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: k, Value: v})
		}
	}
	var err error
	if crit.Since, err = day(a.Since); err != nil {
		return nil, err
	}
	if crit.Before, err = day(a.Before); err != nil {
		return nil, err
	}
	if a.Unseen {
		crit.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	folder := folderOf(a.Folder)
	m, _, err := open(ctx, cfg, folder, true)
	if err != nil {
		return nil, err
	}
	defer m.close()
	data, err := m.UIDSearch(crit, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	all := data.AllUIDs()
	sort.Slice(all, func(i, j int) bool { return all[i] > all[j] })
	total := len(all)
	if len(all) > limit {
		all = all[:limit]
	}
	out := []summary{}
	if len(all) > 0 {
		msgs, err := m.Fetch(imap.UIDSetNum(all...), &imap.FetchOptions{
			UID: true, Envelope: true, Flags: true, RFC822Size: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		}).Collect()
		if err != nil {
			return nil, fmt.Errorf("fetch: %w", err)
		}
		for _, msg := range msgs {
			out = append(out, summarize(msg))
		}
		sort.Slice(out, func(i, j int) bool { return out[i].UID > out[j].UID })
	}
	return map[string]any{"folder": folder, "total": total, "messages": out}, nil
}

func day(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q must be YYYY-MM-DD", s)
	}
	return t, nil
}

func summarize(msg *imapclient.FetchMessageBuffer) summary {
	s := summary{UID: uint32(msg.UID), Size: msg.RFC822Size, Flags: flagNames(msg.Flags)}
	if e := msg.Envelope; e != nil {
		s.Subject = clean(e.Subject)
		s.From = firstAddr(e.From)
		s.To = addrs(e.To)
		if !e.Date.IsZero() {
			s.Date = e.Date.Format(time.RFC3339)
		}
	}
	if s.Date == "" && !msg.InternalDate.IsZero() {
		s.Date = msg.InternalDate.Format(time.RFC3339)
	}
	if msg.BodyStructure != nil {
		msg.BodyStructure.Walk(func(_ []int, part imap.BodyStructure) bool {
			if d := part.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
				s.HasAttachments = true
			}
			if sp, ok := part.(*imap.BodyStructureSinglePart); ok && sp.Filename() != "" {
				s.HasAttachments = true
			}
			return !s.HasAttachments
		})
	}
	return s
}

func flagNames(fs []imap.Flag) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, strings.ToLower(strings.TrimPrefix(string(f), "\\")))
	}
	return out
}

func addrText(a imap.Address) string {
	addr := a.Addr()
	if a.Name != "" {
		return clean(a.Name) + " <" + addr + ">"
	}
	return addr
}

func firstAddr(xs []imap.Address) string {
	if len(xs) == 0 {
		return ""
	}
	return addrText(xs[0])
}

func addrs(xs []imap.Address) []string {
	out := []string{}
	for _, a := range xs {
		out = append(out, addrText(a))
	}
	return out
}

// --- read ---

type readArgs struct {
	Folder   string `json:"folder"`
	UID      uint32 `json:"uid"`
	MaxChars int    `json:"max_chars"`
	MarkRead bool   `json:"mark_read"`
}

// fetchRaw fetches one message's full source. peek leaves \Seen alone.
func (m *mailbox) fetchRaw(uid uint32, peek bool) (*imapclient.FetchMessageBuffer, []byte, error) {
	set := imap.UIDSetNum(imap.UID(uid))
	head, err := m.Fetch(set, &imap.FetchOptions{UID: true, RFC822Size: true}).Collect()
	if err != nil {
		return nil, nil, err
	}
	if len(head) == 0 {
		return nil, nil, fmt.Errorf("no message with uid %d", uid)
	}
	if head[0].RFC822Size > maxMessage {
		return nil, nil, fmt.Errorf("message is %d MB; over the %d MB limit", head[0].RFC822Size>>20, maxMessage>>20)
	}
	section := &imap.FetchItemBodySection{Peek: peek}
	msgs, err := m.Fetch(set, &imap.FetchOptions{
		UID: true, Envelope: true, Flags: true, RFC822Size: true, BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, nil, err
	}
	if len(msgs) == 0 {
		return nil, nil, fmt.Errorf("no message with uid %d", uid)
	}
	return msgs[0], msgs[0].FindBodySection(section), nil
}

func read(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a readArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.UID == 0 {
		return nil, errors.New("uid required")
	}
	folder := folderOf(a.Folder)
	m, _, err := open(ctx, cfg, folder, !a.MarkRead)
	if err != nil {
		return nil, err
	}
	defer m.close()
	msg, body, err := m.fetchRaw(a.UID, !a.MarkRead)
	if err != nil {
		return nil, err
	}
	p, err := parse(body, -1)
	if err != nil {
		return nil, err
	}
	max := a.MaxChars
	if max <= 0 {
		max = 20000
	}
	text, cut := truncate(p.text(), max)
	out := map[string]any{
		"folder": folder, "uid": a.UID, "message_id": p.messageID, "date": p.date,
		"from": p.from, "to": p.to, "cc": p.cc, "reply_to": p.replyTo, "subject": p.subject,
		"flags": flagNames(msg.Flags), "text": text, "truncated": cut, "attachments": p.attachments,
	}
	return out, nil
}

// --- move / set_flags / delete ---

type uidsArgs struct {
	Folder   string   `json:"folder"`
	UIDs     []uint32 `json:"uids"`
	ToFolder string   `json:"to_folder"`
	Seen     *bool    `json:"seen"`
	Flagged  *bool    `json:"flagged"`
}

func (a uidsArgs) set() (imap.UIDSet, error) {
	if len(a.UIDs) == 0 {
		return nil, errors.New("uids required")
	}
	var set imap.UIDSet
	for _, u := range a.UIDs {
		set.AddNum(imap.UID(u))
	}
	return set, nil
}

func move(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a uidsArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	set, err := a.set()
	if err != nil {
		return nil, err
	}
	to := strings.TrimSpace(a.ToFolder)
	if to == "" {
		return nil, errors.New("to_folder required")
	}
	folder := folderOf(a.Folder)
	m, _, err := open(ctx, cfg, folder, false)
	if err != nil {
		return nil, err
	}
	defer m.close()
	if _, err := m.Move(set, to).Wait(); err != nil {
		return nil, fmt.Errorf("move: %w", err)
	}
	return map[string]any{"moved": len(a.UIDs), "from": folder, "to": to}, nil
}

func setFlags(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a uidsArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	set, err := a.set()
	if err != nil {
		return nil, err
	}
	if a.Seen == nil && a.Flagged == nil {
		return nil, errors.New("set seen or flagged")
	}
	var add, del []imap.Flag
	for flag, v := range map[imap.Flag]*bool{imap.FlagSeen: a.Seen, imap.FlagFlagged: a.Flagged} {
		switch {
		case v == nil:
		case *v:
			add = append(add, flag)
		default:
			del = append(del, flag)
		}
	}
	folder := folderOf(a.Folder)
	m, _, err := open(ctx, cfg, folder, false)
	if err != nil {
		return nil, err
	}
	defer m.close()
	for op, flags := range map[imap.StoreFlagsOp][]imap.Flag{imap.StoreFlagsAdd: add, imap.StoreFlagsDel: del} {
		if len(flags) == 0 {
			continue
		}
		if err := m.Store(set, &imap.StoreFlags{Op: op, Silent: true, Flags: flags}, nil).Close(); err != nil {
			return nil, fmt.Errorf("store flags: %w", err)
		}
	}
	return map[string]any{"updated": len(a.UIDs)}, nil
}

func remove(ctx context.Context, _ builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a uidsArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	set, err := a.set()
	if err != nil {
		return nil, err
	}
	folder := folderOf(a.Folder)
	m, _, err := open(ctx, cfg, folder, false)
	if err != nil {
		return nil, err
	}
	defer m.close()
	trash := m.findRole(imap.MailboxAttrTrash, "Trash", "Deleted Items", "Deleted Messages")
	if trash != "" && !strings.EqualFold(trash, folder) {
		if _, err := m.Move(set, trash).Wait(); err != nil {
			return nil, fmt.Errorf("move to %s: %w", trash, err)
		}
		return map[string]any{"deleted": len(a.UIDs), "moved_to": trash}, nil
	}
	if err := m.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return nil, fmt.Errorf("delete: %w", err)
	}
	var exp *imapclient.ExpungeCommand
	if m.Caps().Has(imap.CapUIDPlus) {
		exp = m.UIDExpunge(set)
	} else {
		exp = m.Expunge()
	}
	if err := exp.Close(); err != nil {
		return nil, fmt.Errorf("expunge: %w", err)
	}
	return map[string]any{"deleted": len(a.UIDs), "permanent": true}, nil
}

// --- save_attachment ---

type saveArgs struct {
	Folder string `json:"folder"`
	UID    uint32 `json:"uid"`
	Index  *int   `json:"index"`
	Path   string `json:"path"`
}

func saveAttachment(ctx context.Context, env builtin.Env, cfg builtin.Config, raw json.RawMessage) (any, error) {
	var a saveArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.UID == 0 || a.Index == nil {
		return nil, errors.New("uid and index required")
	}
	folder := folderOf(a.Folder)
	m, _, err := open(ctx, cfg, folder, true)
	if err != nil {
		return nil, err
	}
	defer m.close()
	_, body, err := m.fetchRaw(a.UID, true)
	if err != nil {
		return nil, err
	}
	p, err := parse(body, *a.Index)
	if err != nil {
		return nil, err
	}
	if p.picked == nil {
		return nil, fmt.Errorf("message %d has no attachment %d (it has %d)", a.UID, *a.Index, len(p.attachments))
	}
	name := safeName(p.picked.meta.Filename, *a.Index)
	dst := strings.TrimSpace(a.Path)
	if dst == "" {
		dst = path.Join("mail", name)
	} else if strings.HasSuffix(dst, "/") {
		dst = path.Join(dst, name)
	}
	if err := env.WriteFile(ctx, dst, p.picked.data); err != nil {
		return nil, err
	}
	return map[string]any{"path": dst, "filename": p.picked.meta.Filename, "size": len(p.picked.data)}, nil
}

func safeName(name string, index int) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == "/" || name == ".." {
		return fmt.Sprintf("attachment-%d", index)
	}
	return name
}
