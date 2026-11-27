package email

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"

	"silo.agent/internal/channels"
)

// logoutWait is how long close waits for the server's goodbye.
const logoutWait = 5 * time.Second

// box is one signed-in IMAP connection. It closes itself when the context it
// was dialed under ends, which unblocks any command waiting on the server.
type box struct {
	c    *imapclient.Client
	conn net.Conn
	stop func() bool
	// news is signalled when the server says the mailbox has grown.
	news chan struct{}
	// count is how many messages the opened mailbox held.
	count uint32
}

// head is what is learned about a message before deciding to download it.
type head struct {
	uid    uint32
	size   int64
	header mail.Header
}

func (a *Adapter) dialIMAP(ctx context.Context, s settings) (*box, error) {
	conn, err := s.net.DialContext(ctx, "tcp", s.imapAddr)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the IMAP server %s: %w", s.imapAddr, err)
	}
	b := &box{conn: conn, news: make(chan struct{}, 1)}
	b.stop = context.AfterFunc(ctx, func() { conn.Close() })
	b.touch()
	opts := &imapclient.Options{UnilateralDataHandler: &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages == nil {
				return
			}
			select {
			case b.news <- struct{}{}:
			default:
			}
		},
	}}
	switch {
	case a.plain:
		b.c = imapclient.New(conn, opts)
	case s.imapTLS:
		b.c = imapclient.New(tls.Client(conn, &tls.Config{ServerName: s.imapHost}), opts)
	default:
		opts.TLSConfig = &tls.Config{ServerName: s.imapHost}
		if b.c, err = imapclient.NewStartTLS(conn, opts); err != nil {
			conn.Close()
			b.stop()
			return nil, fmt.Errorf("the IMAP server %s did not start TLS (port 993 is TLS from the start): %w", s.imapAddr, err)
		}
	}
	if err := b.c.WaitGreeting(); err != nil {
		b.close()
		return nil, fmt.Errorf("the IMAP server %s did not answer: %w", s.imapAddr, err)
	}
	if err := b.c.Login(s.username, s.password).Wait(); err != nil {
		b.close()
		return nil, loginError(err)
	}
	return b, nil
}

// loginError words a failed IMAP login. A refusal is permanent: trying the
// same password again only gets the account locked. A server that says it is
// busy or broken is not a refusal.
func loginError(err error) error {
	var ie *imap.Error
	if !errors.As(err, &ie) {
		return fmt.Errorf("IMAP sign-in: %w", err)
	}
	switch ie.Code {
	case imap.ResponseCodeUnavailable, imap.ResponseCodeServerBug, imap.ResponseCodeLimit, imap.ResponseCodeInUse:
		return fmt.Errorf("the IMAP server cannot sign in right now: %s", ie.Text)
	}
	return channels.Permanent(fmt.Errorf("the IMAP server refused the username or password (%s): check them, and use an app password where the provider has them", ie.Text))
}

// touch gives the next burst of commands its time.
func (b *box) touch() { _ = b.conn.SetDeadline(time.Now().Add(opTimeout)) }

func (b *box) close() {
	b.touch()
	done := make(chan struct{})
	go func() {
		_ = b.c.Logout().Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(logoutWait):
	}
	_ = b.c.Close()
	b.stop()
}

// open selects INBOX.
func (b *box) open(readOnly bool) (*imap.SelectData, error) {
	b.touch()
	sel, err := b.c.Select("INBOX", &imap.SelectOptions{ReadOnly: readOnly}).Wait()
	if err != nil {
		return nil, fmt.Errorf("open the inbox: %w", err)
	}
	b.count = sel.NumMessages
	return sel, nil
}

// await returns when the mailbox may have new mail: the server said so (IDLE),
// or it is time to look anyway.
func (b *box) await(ctx context.Context, idle, poll time.Duration) error {
	closed := errors.New("the mail server closed the connection")
	if !b.c.Caps().Has(imap.CapIdle) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.c.Closed():
			return closed
		case <-time.After(poll):
			return nil
		}
	}
	_ = b.conn.SetDeadline(time.Now().Add(idle + opTimeout))
	cmd, err := b.c.Idle()
	if err != nil {
		return fmt.Errorf("wait for mail: %w", err)
	}
	select {
	case <-ctx.Done():
	case <-b.c.Closed():
	case <-b.news:
	case <-time.After(idle):
	}
	b.touch()
	err = cmd.Close()
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		return fmt.Errorf("wait for mail: %w", err)
	}
	return nil
}

// headsFrom lists the messages whose UID is next or higher.
func (b *box) headsFrom(next uint32) ([]head, error) {
	b.touch()
	data, err := b.c.Search(&imap.SearchCriteria{
		UID: []imap.UIDSet{{imap.UIDRange{Start: imap.UID(next)}}},
	}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("look for new mail: %w", err)
	}
	heads, err := b.heads(data.AllSeqNums())
	if err != nil {
		return nil, err
	}
	// "n:*" always names the newest message, even one below n.
	out := heads[:0]
	for _, h := range heads {
		if h.uid >= next {
			out = append(out, h)
		}
	}
	return out, nil
}

// headsOf lists the newest limit messages whose From header mentions addr.
func (b *box) headsOf(addr string, limit int) ([]head, error) {
	b.touch()
	data, err := b.c.Search(&imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "From", Value: addr}},
	}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search the inbox: %w", err)
	}
	seqs := data.AllSeqNums()
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] > seqs[j] })
	if len(seqs) > limit {
		seqs = seqs[:limit]
	}
	return b.heads(seqs)
}

// newest lists the n newest messages of the opened mailbox.
func (b *box) newest(n uint32) ([]head, error) {
	if b.count == 0 {
		return nil, nil
	}
	first := uint32(1)
	if b.count > n {
		first = b.count - n + 1
	}
	var seqs []uint32
	for i := first; i <= b.count; i++ {
		seqs = append(seqs, i)
	}
	return b.heads(seqs)
}

// heads fetches by sequence number and asks for the UID as an item: go-imap
// matches a UID FETCH to its answer by the UID inside it and drops one that
// lacks it, which loses every message on a server that leaves it out.
func (b *box) heads(seqs []uint32) ([]head, error) {
	if len(seqs) == 0 {
		return nil, nil
	}
	b.touch()
	section := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
	msgs, err := b.c.Fetch(imap.SeqSetNum(seqs...), &imap.FetchOptions{
		UID: true, RFC822Size: true, BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("read the inbox: %w", err)
	}
	out := make([]head, 0, len(msgs))
	for _, m := range msgs {
		if m.UID == 0 {
			continue
		}
		out = append(out, head{uid: uint32(m.UID), size: m.RFC822Size, header: headerOf(m.FindBodySection(section))})
	}
	return out, nil
}

// fetch downloads one message whole without marking it read. It answers nil
// for a message that is no longer there.
func (b *box) fetch(uid uint32) ([]byte, error) {
	b.touch()
	data, err := b.c.Search(&imap.SearchCriteria{UID: []imap.UIDSet{imap.UIDSetNum(imap.UID(uid))}}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("read a message: %w", err)
	}
	seqs := data.AllSeqNums()
	if len(seqs) == 0 {
		return nil, nil
	}
	_ = b.conn.SetDeadline(time.Now().Add(5 * opTimeout))
	defer b.touch()
	section := &imap.FetchItemBodySection{Peek: true}
	msgs, err := b.c.Fetch(imap.SeqSetNum(seqs[0]), &imap.FetchOptions{
		UID: true, BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("read a message: %w", err)
	}
	if len(msgs) == 0 || uint32(msgs[0].UID) != uid {
		return nil, nil
	}
	return msgs[0].FindBodySection(section), nil
}

func (b *box) markSeen(uid uint32) error {
	b.touch()
	err := b.c.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen},
	}, nil).Close()
	if err != nil {
		return fmt.Errorf("mark a message read: %w", err)
	}
	return nil
}

// headerOf reads a message's header block; one that does not parse is empty,
// which names no sender and so is passed over.
func headerOf(raw []byte) mail.Header {
	h, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return mail.Header{}
	}
	return mail.Header{Header: message.Header{Header: h}}
}
