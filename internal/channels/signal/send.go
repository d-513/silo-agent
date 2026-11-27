package signal

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

const (
	// maxMessageRunes keeps a message under the size past which Signal turns
	// its text into an attached file.
	maxMessageRunes = 2000
	// maxOptions bounds the picker: an account's whole address book can be long.
	maxOptions = 300
)

// session is the channel's daemon: the live one, or one made from the config,
// since the RPC needs no open stream and a channel can send while its worker
// is down. done releases what was made.
func (a *Adapter) session(ch *db.Channel, cfg channels.Config) (d *daemon, done func(), err error) {
	if c := a.conn(ch.ID); c != nil {
		return c.d, func() {}, nil
	}
	if d, err = newDaemon(cfg); err != nil {
		return nil, nil, err
	}
	return d, d.close, nil
}

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, cfg channels.Config, msg channels.Outbound) error {
	external := strings.TrimSpace(msg.ExternalID)
	if external == "" {
		external = ch.ExternalID
	}
	tgt, ok := parseTarget(external)
	if !ok {
		return fmt.Errorf("signal channel %q is neither a phone number nor a group — pick it again in Set up", external)
	}
	d, done, err := a.session(ch, cfg)
	if err != nil {
		return err
	}
	defer done()
	return send(ctx, d, tgt, msg)
}

// send posts the text in pieces Signal shows whole, with the files on the
// last one.
func send(ctx context.Context, d *daemon, tgt target, msg channels.Outbound) error {
	var files []string
	for _, f := range msg.Files {
		files = append(files, dataURI(f))
	}
	var chunks []string
	if strings.TrimSpace(msg.Text) != "" {
		// Cut the Markdown, then style each piece: a style's range counts
		// from the start of the message it is in.
		chunks = channels.Chunk(msg.Text, maxMessageRunes)
	}
	if len(chunks) == 0 {
		if len(files) == 0 {
			return nil
		}
		chunks = []string{""}
	}
	for i, chunk := range chunks {
		params := tgt.address()
		if text, styles := format(chunk); text != "" {
			params["message"] = text
			if len(styles) > 0 {
				params["textStyle"] = styles
			}
		}
		if i == len(chunks)-1 && len(files) > 0 {
			params["attachments"] = files
		}
		timeout := rpcTimeout
		if params["attachments"] != nil {
			timeout = fileTimeout
		}
		if err := d.call(ctx, timeout, "send", params, nil); err != nil {
			return err
		}
	}
	return nil
}

// dataURI is a file as signal-cli's send takes one without a path on its
// disk: data:<type>;filename=<name>;base64,<bytes>.
func dataURI(f channels.Attachment) string {
	kind, _, _ := strings.Cut(f.Mime, ";")
	if kind = strings.TrimSpace(kind); kind == "" {
		kind = "application/octet-stream"
	}
	return "data:" + kind + ";filename=" + uriName(f.Name) + ";base64," + base64.StdEncoding.EncodeToString(f.Data)
}

// uriName keeps a file name to what cannot end the URI's parameter early.
func uriName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, strings.TrimSpace(name))
	if strings.Trim(name, "._") == "" {
		return "file"
	}
	return name
}

// Typing shows or clears "typing…" in the conversation.
func (a *Adapter) Typing(ctx context.Context, ch *db.Channel, externalID string, on bool) error {
	c := a.conn(ch.ID)
	if c == nil {
		return errors.New("signal channel is not connected")
	}
	tgt, ok := parseTarget(externalID)
	if !ok {
		return fmt.Errorf("%q is neither a phone number nor a group", externalID)
	}
	params := tgt.address()
	if !on {
		params["stop"] = true
	}
	return c.d.call(ctx, rpcTimeout, "sendTyping", params, nil)
}

// History has nothing to read: Signal's servers keep no messages, and the
// daemon hands each one over once.
func (a *Adapter) History(context.Context, *db.Channel, channels.Config, string, int) ([]channels.Message, error) {
	return nil, errors.New("Signal keeps no message history to read back")
}

// --- setup actions ---

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, cfg channels.Config, action string, _ map[string]string) (channels.State, error) {
	d, done, err := a.session(ch, cfg)
	if err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
	}
	defer done()
	switch action {
	case "list_chats":
		return listChats(ctx, d)
	case "refresh":
		if err := d.check(ctx); err != nil {
			return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
		}
		return channels.State{Kind: channels.StateInfo, Message: connectedMessage(d)}, nil
	}
	return channels.State{}, fmt.Errorf("unknown action %q", action)
}

// listChats offers the account's groups, then the people it knows.
func listChats(ctx context.Context, d *daemon) (channels.State, error) {
	var groups []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsMember  bool   `json:"isMember"`
		IsBlocked bool   `json:"isBlocked"`
	}
	if err := d.call(ctx, rpcTimeout, "listGroups", nil, &groups); err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
	}
	var contacts []contact
	if err := d.call(ctx, rpcTimeout, "listContacts", nil, &contacts); err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
	}
	var in, people []channels.Option
	for _, g := range groups {
		if g.ID == "" || !g.IsMember || g.IsBlocked {
			continue
		}
		name := strings.TrimSpace(g.Name)
		if name == "" {
			name = g.ID
		}
		in = append(in, channels.Option{Value: target{group: g.ID}.String(), Label: "Group · " + name})
	}
	for _, ct := range contacts {
		p := ct.person()
		id := p.number
		if id == "" {
			id = p.uuid
		}
		if id == "" || id == d.account || ct.IsBlocked {
			continue
		}
		label := id
		if p.name != "" {
			label = p.name + " · " + id
		}
		people = append(people, channels.Option{Value: id, Label: label})
	}
	byLabel := func(o []channels.Option) {
		sort.Slice(o, func(i, j int) bool { return strings.ToLower(o[i].Label) < strings.ToLower(o[j].Label) })
	}
	byLabel(in)
	byLabel(people)
	options := append(in, people...)
	if len(options) > maxOptions {
		options = options[:maxOptions]
	}
	msg := "Pick the group or person, or type a phone number (+491701234567) or a group id."
	if len(options) == 0 {
		msg = "The account has no groups or contacts yet. Type a phone number (+491701234567) or a group id."
	}
	return channels.State{Kind: channels.StateSelect, Message: msg, Options: options}, nil
}
