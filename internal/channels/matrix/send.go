package matrix

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// --- outbound ---

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, cfg channels.Config, msg channels.Outbound) error {
	external := strings.TrimSpace(msg.ExternalID)
	if external == "" {
		external = ch.ExternalID
	}
	c, err := a.session(ctx, ch, cfg)
	if err != nil {
		return err
	}
	roomID, err := c.target(ctx, external)
	if err != nil {
		return err
	}
	// A plain event in an encrypted room would be the one message there that
	// anyone holding the server can read; refuse rather than leak.
	if enc, err := c.encrypted(ctx, roomID); err != nil {
		return err
	} else if enc {
		return errors.New(encryptedMessage)
	}
	thread := strings.TrimSpace(msg.ReplyTo)
	put := func(content map[string]any) error {
		if thread != "" {
			// m.in_reply_to is what a client without threads shows the
			// message as a reply to: the thread's newest message.
			reply := a.lastIn(ch.ID, roomID, thread)
			if reply == "" {
				reply = thread
			}
			content["m.relates_to"] = map[string]any{
				"rel_type": "m.thread", "event_id": thread, "is_falling_back": true,
				"m.in_reply_to": map[string]any{"event_id": reply},
			}
		}
		id, err := c.sendMessage(ctx, roomID, content)
		if err != nil {
			return friendly(err)
		}
		if thread != "" {
			a.joined.Add(threadKey(ch.ID, roomID, thread))
			a.setLast(ch.ID, roomID, thread, id)
		}
		return nil
	}
	for _, part := range channels.Chunk(msg.Text, maxMessageRunes) {
		err := put(map[string]any{
			"msgtype": "m.text", "body": part,
			"format": "org.matrix.custom.html", "formatted_body": toHTML(part),
			// Present and empty: the message mentions nobody, so a "@room"
			// the model happened to write pages no one.
			"m.mentions": map[string]any{},
		})
		if err != nil {
			return err
		}
	}
	for _, f := range msg.Files {
		if len(f.Data) == 0 {
			continue
		}
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = "file"
		}
		mime := strings.TrimSpace(f.Mime)
		if mime == "" {
			mime = "application/octet-stream"
		}
		uri, err := c.api.upload(ctx, name, mime, f.Data)
		if err != nil {
			return friendly(err)
		}
		err = put(map[string]any{
			"msgtype": msgType(mime), "body": name, "filename": name, "url": uri,
			"info": map[string]any{"mimetype": mime, "size": len(f.Data)},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// sendMessage puts one m.room.message and returns its event id. The
// transaction id makes a request repeated after a rate limit the same message.
func (c *conn) sendMessage(ctx context.Context, roomID string, content map[string]any) (string, error) {
	var txn [12]byte
	if _, err := rand.Read(txn[:]); err != nil {
		return "", err
	}
	var out struct {
		EventID string `json:"event_id"`
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/send/m.room.message/silo" + hex.EncodeToString(txn[:])
	err := c.api.call(ctx, "PUT", path, nil, content, &out)
	return out.EventID, err
}

// msgType is the message type clients show a file of this kind with. An SVG
// goes as a file: clients do not draw one inline.
func msgType(mime string) string {
	kind, sub, _ := strings.Cut(strings.ToLower(mime), "/")
	switch {
	case kind == "image" && !strings.HasPrefix(sub, "svg"):
		return "m.image"
	case kind == "audio":
		return "m.audio"
	case kind == "video":
		return "m.video"
	}
	return "m.file"
}

// Typing shows or clears the bot's typing notice. Shown, it lasts 20 seconds
// unless the host repeats it, which it does while the Bot works.
func (a *Adapter) Typing(ctx context.Context, ch *db.Channel, externalID string, on bool) error {
	c := a.conn(ch.ID)
	if c == nil {
		return errors.New("matrix channel is not connected")
	}
	roomID, err := c.target(ctx, externalID)
	if err != nil {
		return err
	}
	body := map[string]any{"typing": on}
	if on {
		body["timeout"] = 20000
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/typing/" + url.PathEscape(c.self)
	return friendly(c.api.call(ctx, "PUT", path, nil, body, nil))
}

// --- history ---

// History reads the bound room's recent messages, oldest first.
func (a *Adapter) History(ctx context.Context, ch *db.Channel, cfg channels.Config, externalID string, limit int) ([]channels.Message, error) {
	external := strings.TrimSpace(externalID)
	if external == "" {
		external = ch.ExternalID
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 100)
	c, err := a.session(ctx, ch, cfg)
	if err != nil {
		return nil, err
	}
	roomID, err := c.target(ctx, external)
	if err != nil {
		return nil, err
	}
	if enc, err := c.encrypted(ctx, roomID); err != nil {
		return nil, err
	} else if enc {
		return nil, errors.New(encryptedMessage)
	}
	filter, _ := json.Marshal(map[string]any{"types": []string{"m.room.message"}})
	q := url.Values{"dir": {"b"}, "limit": {fmt.Sprint(limit)}, "filter": {string(filter)}}
	var resp struct {
		Chunk []event `json:"chunk"`
	}
	if err := c.api.call(ctx, "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/messages", q, nil, &resp); err != nil {
		return nil, friendly(err)
	}
	slices.Reverse(resp.Chunk) // dir=b answers newest first
	var out []channels.Message
	for _, ev := range resp.Chunk {
		var m content
		if ev.Type != "m.room.message" || json.Unmarshal(ev.Content, &m) != nil || m.Relates.RelType == "m.replace" {
			continue
		}
		text := strings.TrimSpace(m.Body)
		if m.Relates.InReplyTo.EventID != "" {
			text = strings.TrimSpace(withoutReplyQuote(m.Body))
		}
		if fileTypes[m.MsgType] {
			name, caption := text, ""
			if m.Filename != "" && m.Filename != text {
				name, caption = m.Filename, text
			}
			kind := ""
			if m.Info.Mimetype != "" {
				kind = " (" + m.Info.Mimetype + ")"
			}
			text = strings.TrimSpace(caption + "\n" + fmt.Sprintf("[attachment: %s%s]", name, kind))
		}
		if text == "" {
			continue // redacted
		}
		if m.Relates.RelType == "m.thread" {
			text = "[in thread] " + text
		}
		msg := channels.Message{Text: text, At: time.UnixMilli(ev.TS)}
		if ev.Sender == c.self {
			msg.Author, msg.Out = "Bot", true
		} else {
			msg.Author = c.memberName(ctx, roomID, ev.Sender)
		}
		out = append(out, msg)
	}
	return out, nil
}

// --- setup actions ---

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, cfg channels.Config, action string, _ map[string]string) (channels.State, error) {
	switch action {
	case "list_rooms":
		c, err := a.session(ctx, ch, cfg)
		if err != nil {
			return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
		}
		return a.listRooms(ctx, c)
	case "refresh":
		// Asked anew, not from the live session: this is the owner checking
		// that the address and the token still work.
		c, err := login(ctx, cfg)
		if err != nil {
			return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
		}
		if strings.TrimSpace(ch.ExternalID) != "" {
			if roomID, err := c.target(ctx, ch.ExternalID); err != nil {
				return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
			} else if enc, _ := c.encrypted(ctx, roomID); enc {
				return channels.State{Kind: channels.StateError, Message: encryptedMessage}, nil
			}
		}
		return channels.State{Kind: channels.StateInfo, Message: connectedMessage(c)}, nil
	}
	return channels.State{}, fmt.Errorf("unknown action %q", action)
}

// listRooms offers the rooms the account has joined, after accepting the
// invitations that wait for it. An encrypted room is listed and marked, so its
// owner sees why it cannot be used rather than wonder where it went.
func (a *Adapter) listRooms(ctx context.Context, c *conn) (channels.State, error) {
	resp, err := c.sync(ctx, "")
	if err != nil {
		return channels.State{}, friendly(err)
	}
	a.accept(ctx, c, resp)
	c.learn(resp)
	ids := make([]string, 0, len(resp.Rooms.Join)+len(resp.Rooms.Invite))
	for id := range resp.Rooms.Join {
		ids = append(ids, id)
	}
	for id := range resp.Rooms.Invite {
		ids = append(ids, id)
	}
	var options []channels.Option
	for _, id := range ids {
		label := c.title(ctx, id)
		if c.knownEncrypted(id) {
			label += " (encrypted: Silo cannot read it)"
		}
		options = append(options, channels.Option{Value: id, Label: label})
	}
	if len(options) == 0 {
		return channels.State{
			Kind:    channels.StateSelect,
			Message: "The bot is in no room yet. Invite " + c.self + " to a room, or start a direct chat with it, then try again. You can also enter a room id (!…) or an alias (#room:server) below.",
		}, nil
	}
	slices.SortFunc(options, func(x, y channels.Option) int {
		return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return channels.State{Kind: channels.StateSelect, Message: fmt.Sprintf("%d rooms", len(options)), Options: options}, nil
}

// --- target ---

// target reads a room id from what the owner entered or picked: a room id
// (!…), an alias (#room:server), which the homeserver is asked to resolve, or
// a matrix.to link to either.
func (c *conn) target(ctx context.Context, s string) (string, error) {
	s = strings.TrimSpace(s)
	if _, rest, ok := strings.Cut(s, "matrix.to/#/"); ok {
		rest, _, _ = strings.Cut(rest, "?")
		rest, _, _ = strings.Cut(rest, "/")
		if un, err := url.PathUnescape(rest); err == nil {
			s = un
		}
	}
	switch {
	case strings.HasPrefix(s, "!") && len(s) > 1:
		return s, nil
	case strings.HasPrefix(s, "#") && strings.Contains(s, ":"):
		return c.resolve(ctx, s)
	}
	return "", fmt.Errorf("%q is not a Matrix room — enter a room id (!abc:example.org) or an alias (#room:example.org), or pick the room in Set up", s)
}

// resolve asks the homeserver which room an alias names.
func (c *conn) resolve(ctx context.Context, alias string) (string, error) {
	c.mu.Lock()
	id := c.aliases[alias]
	c.mu.Unlock()
	if id != "" {
		return id, nil
	}
	var out struct {
		RoomID string `json:"room_id"`
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	err := c.api.call(ctx, "GET", "/_matrix/client/v3/directory/room/"+url.PathEscape(alias), nil, nil, &out)
	var ae *apiError
	switch {
	case errors.As(err, &ae) && ae.Status == 404:
		return "", fmt.Errorf("no Matrix room is called %s — check the alias, or pick the room in Set up", alias)
	case err != nil:
		return "", friendly(err)
	case out.RoomID == "":
		return "", fmt.Errorf("the homeserver did not say which room %s is", alias)
	}
	c.mu.Lock()
	c.aliases[alias] = out.RoomID
	c.mu.Unlock()
	return out.RoomID, nil
}
