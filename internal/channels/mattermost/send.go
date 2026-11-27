package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/url"
	"regexp"
	"slices"
	"strconv"
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
	target := targetID(external)
	if target == "" {
		return fmt.Errorf("mattermost channel %q is not a channel id — pick it again in Set up", strings.TrimSpace(external))
	}
	c, done, err := a.session(ctx, ch, cfg)
	if err != nil {
		return err
	}
	defer done()
	return send(ctx, c.api, target, msg)
}

// newPost is the body of a post being made.
type newPost struct {
	ChannelID string   `json:"channel_id"`
	Message   string   `json:"message"`
	RootID    string   `json:"root_id,omitempty"`
	FileIDs   []string `json:"file_ids,omitempty"`
}

// send posts the text as written (Mattermost renders Markdown itself), then
// the files in posts of their own, all in the same thread.
func send(ctx context.Context, cl *api, target string, msg channels.Outbound) error {
	root := strings.TrimSpace(msg.ReplyTo)
	for _, part := range channels.Chunk(quiet(msg.Text), maxMessageRunes) {
		if err := cl.post(ctx, "/posts", newPost{ChannelID: target, Message: part, RootID: root}, nil); err != nil {
			return friendly(err)
		}
	}
	var ids []string
	for _, f := range msg.Files {
		if len(f.Data) == 0 {
			continue
		}
		id, err := upload(ctx, cl, target, f)
		if err != nil {
			return friendly(err)
		}
		ids = append(ids, id)
	}
	for batch := range slices.Chunk(ids, filesPerPost) {
		if err := cl.post(ctx, "/posts", newPost{ChannelID: target, RootID: root, FileIDs: batch}, nil); err != nil {
			return friendly(err)
		}
	}
	return nil
}

// upload stores one file for the channel without posting it yet.
func upload(ctx context.Context, cl *api, target string, f channels.Attachment) (string, error) {
	name := strings.TrimSpace(f.Name)
	if name == "" {
		name = "file"
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("channel_id", target); err != nil {
		return "", err
	}
	part, err := form.CreateFormFile("files", name)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(f.Data); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	data, err := cl.call(ctx, "POST", "/files", form.FormDataContentType(), body.Bytes())
	if err != nil {
		return "", err
	}
	var res struct {
		Infos []fileInfo `json:"file_infos"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return "", err
	}
	if len(res.Infos) == 0 || res.Infos[0].ID == "" {
		return "", fmt.Errorf("Mattermost did not keep %s", name)
	}
	return res.Infos[0].ID, nil
}

// massMention is the three words that notify a whole channel.
var massMention = regexp.MustCompile(`(?i)@(channel|all|here)\b`)

// quiet keeps a model-written @channel, @all or @here from paging everyone:
// a zero-width space after the @ leaves it reading the same and notifying
// nobody. Code, where it would show, is left as written.
func quiet(text string) string {
	if !strings.Contains(text, "@") {
		return text
	}
	lines := strings.Split(text, "\n")
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		// Odd pieces sit between two backticks: inline code. A last odd piece
		// has no closing backtick, so it is ordinary text.
		pieces := strings.Split(line, "`")
		for j := range pieces {
			if j%2 == 0 || j == len(pieces)-1 {
				pieces[j] = massMention.ReplaceAllString(pieces[j], "@​$1")
			}
		}
		lines[i] = strings.Join(pieces, "`")
	}
	return strings.Join(lines, "\n")
}

// Typing shows the bot as typing, in the thread it is answering in. The
// indicator fades by itself a few seconds after the last signal, so there is
// nothing to send for off.
func (a *Adapter) Typing(ctx context.Context, ch *db.Channel, externalID string, on bool) error {
	if !on {
		return nil
	}
	c := a.conn(ch.ID)
	if c == nil {
		return errors.New("mattermost channel is not connected")
	}
	target := targetID(externalID)
	if target == "" {
		return fmt.Errorf("%q is not a Mattermost channel id", externalID)
	}
	a.mu.Lock()
	thread := a.thread[ch.ID+"/"+target]
	a.mu.Unlock()
	body := map[string]string{"channel_id": target}
	if thread != "" {
		body["parent_id"] = thread
	}
	return friendly(c.api.post(ctx, "/users/me/typing", body, nil))
}

// --- history ---

// History reads the bound channel's recent posts, oldest first, replies
// marked as such.
func (a *Adapter) History(ctx context.Context, ch *db.Channel, cfg channels.Config, externalID string, limit int) ([]channels.Message, error) {
	external := strings.TrimSpace(externalID)
	if external == "" {
		external = ch.ExternalID
	}
	target := targetID(external)
	if target == "" {
		return nil, fmt.Errorf("chat %q is not a Mattermost channel id", external)
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	c, done, err := a.session(ctx, ch, cfg)
	if err != nil {
		return nil, err
	}
	defer done()
	var res struct {
		Order []string        `json:"order"` // newest first
		Posts map[string]post `json:"posts"`
	}
	if err := c.api.get(ctx, "/channels/"+target+"/posts?per_page="+strconv.Itoa(limit), &res); err != nil {
		return nil, friendly(err)
	}
	var out []channels.Message
	for _, id := range slices.Backward(res.Order) {
		p, ok := res.Posts[id]
		if !ok || p.Type != "" {
			continue
		}
		m := channels.Message{At: time.UnixMilli(p.CreateAt)}
		if p.UserID == c.self {
			m.Author, m.Out = "Bot", true
		} else {
			m.Author = a.user(ctx, c, ch.ID, p.UserID).Username
			if m.Author == "" {
				m.Author = p.UserID
			}
		}
		var lines []string
		if t := strings.TrimSpace(p.Message); t != "" {
			lines = append(lines, t)
		}
		known := map[string]fileInfo{}
		for _, f := range p.Metadata.Files {
			known[f.ID] = f
		}
		for _, id := range p.FileIDs {
			f := known[id]
			if f.Name == "" {
				f.Name = "file"
			}
			kind := ""
			if f.MimeType != "" {
				kind = " (" + f.MimeType + ")"
			}
			lines = append(lines, fmt.Sprintf("[attachment: %s%s]", f.Name, kind))
		}
		m.Text = strings.Join(lines, "\n")
		if p.RootID != "" && m.Text != "" {
			m.Text = "[in thread] " + m.Text
		}
		out = append(out, m)
	}
	return out, nil
}

// --- setup actions ---

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, cfg channels.Config, action string, _ map[string]string) (channels.State, error) {
	if action != "list_channels" && action != "refresh" {
		return channels.State{}, fmt.Errorf("unknown action %q", action)
	}
	// Both are REST calls, so they work while the worker is down too.
	c, done, err := a.session(ctx, ch, cfg)
	if err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
	}
	defer done()
	if action == "list_channels" {
		return a.listChannels(ctx, ch, c)
	}
	fresh, err := login(ctx, c.api)
	if err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
	}
	return channels.State{Kind: channels.StateInfo, Message: connectedMessage(fresh)}, nil
}

// listChannels offers what the bot can talk in: the channels it is a member
// of in each of its teams, its group messages and its direct messages.
func (a *Adapter) listChannels(ctx context.Context, ch *db.Channel, c *conn) (channels.State, error) {
	var teams []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	if err := c.api.get(ctx, "/users/me/teams", &teams); err != nil {
		return channels.State{}, friendly(err)
	}
	var options []channels.Option
	listed := map[string]bool{} // direct and group messages come back with every team
	for _, team := range teams {
		var chans []struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
			DeleteAt    int64  `json:"delete_at"`
		}
		if err := c.api.get(ctx, "/users/me/teams/"+url.PathEscape(team.ID)+"/channels", &chans); err != nil {
			return channels.State{}, friendly(err)
		}
		for _, cn := range chans {
			if cn.DeleteAt != 0 || listed[cn.ID] {
				continue
			}
			label := cn.DisplayName
			switch cn.Type {
			case "D":
				// A direct channel is named by its two members' ids.
				first, second, _ := strings.Cut(cn.Name, "__")
				other := first
				if other == c.self {
					other = second
				}
				if other == "" || other == c.self {
					continue // the bot's notes to itself
				}
				u := a.user(ctx, c, ch.ID, other)
				label = "DM · " + u.display()
				if u.Username != "" && u.Username != u.display() {
					label += " (@" + u.Username + ")"
				}
			case "G":
				label = "Group message · " + cn.DisplayName
			case "P":
				label += " · " + team.DisplayName + " (private)"
			default:
				label += " · " + team.DisplayName
			}
			listed[cn.ID] = true
			options = append(options, channels.Option{Value: cn.ID, Label: label})
		}
	}
	if len(options) == 0 {
		return channels.State{
			Kind:    channels.StateSelect,
			Message: "The bot is in no channel yet. Add it to a team, then to a channel with /invite @" + c.name + ", or write it a direct message, and try again. You can also paste a channel id below.",
		}, nil
	}
	slices.SortFunc(options, func(x, y channels.Option) int {
		return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return channels.State{Kind: channels.StateSelect, Message: fmt.Sprintf("%d conversations", len(options)), Options: options}, nil
}

// --- target ---

// channelRe is a Mattermost id: 26 characters of lower-case letters and digits.
var channelRe = regexp.MustCompile(`^[a-z0-9]{26}$`)

// targetID reads a channel id from what the owner entered or picked, "" when
// it is not one.
func targetID(s string) string {
	s = strings.TrimSpace(s)
	if channelRe.MatchString(s) {
		return s
	}
	return ""
}
