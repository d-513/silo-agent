package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	slackapi "github.com/slack-go/slack"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// retryWithin is the longest Retry-After a send waits out; past it the send
// fails, worded, rather than hold a run up.
const retryWithin = 30 * time.Second

// --- outbound ---

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, cfg channels.Config, msg channels.Outbound) error {
	external := strings.TrimSpace(msg.ExternalID)
	if external == "" {
		external = ch.ExternalID
	}
	target := targetID(external)
	if target == "" {
		return fmt.Errorf("slack channel %q is not a conversation id — pick it again in Set up", strings.TrimSpace(external))
	}
	c, err := a.session(ctx, ch, cfg)
	if err != nil {
		return err
	}
	return send(ctx, c.api, target, msg)
}

func send(ctx context.Context, api *slackapi.Client, target string, msg channels.Outbound) error {
	thread := strings.TrimSpace(msg.ReplyTo)
	for _, part := range channels.Chunk(msg.Text, maxMessageRunes) {
		opts := []slackapi.MsgOption{
			slackapi.MsgOptionText(toMrkdwn(part), false),
			// Plain @here or @channel stays plain: the model writes what it
			// likes, and a stray one must not page a workspace.
			slackapi.MsgOptionLinkNames(false),
			slackapi.MsgOptionParse(false),
		}
		if thread != "" {
			opts = append(opts, slackapi.MsgOptionTS(thread))
		}
		err := patiently(ctx, func() error {
			_, _, err := api.PostMessageContext(ctx, target, opts...)
			return err
		})
		if err != nil {
			return friendly(err)
		}
	}
	var files []slackapi.FileSummary
	for _, f := range msg.Files {
		if len(f.Data) == 0 {
			continue
		}
		sum, err := stage(ctx, api, f)
		if err != nil {
			return friendly(err)
		}
		files = append(files, sum)
	}
	// Staged files are shared in messages of up to ten, in the same thread.
	for rest := files; len(rest) > 0; {
		batch := rest[:min(len(rest), filesPerMessage)]
		rest = rest[len(batch):]
		err := patiently(ctx, func() error {
			_, err := api.CompleteUploadExternalContext(ctx, slackapi.CompleteUploadExternalParameters{
				Files: batch, Channel: target, ThreadTimestamp: thread,
			})
			return err
		})
		if err != nil {
			return friendly(err)
		}
	}
	return nil
}

// stage uploads one file's bytes (files.getUploadURLExternal, then the upload)
// without sharing it yet.
func stage(ctx context.Context, api *slackapi.Client, f channels.Attachment) (slackapi.FileSummary, error) {
	name := strings.TrimSpace(f.Name)
	if name == "" {
		name = "file"
	}
	var up *slackapi.GetUploadURLExternalResponse
	err := patiently(ctx, func() (err error) {
		up, err = api.GetUploadURLExternalContext(ctx, slackapi.GetUploadURLExternalParameters{FileName: name, FileSize: len(f.Data)})
		return err
	})
	if err != nil {
		return slackapi.FileSummary{}, err
	}
	if err := api.UploadToURL(ctx, slackapi.UploadToURLParameters{UploadURL: up.UploadURL, Reader: bytes.NewReader(f.Data), Filename: name}); err != nil {
		return slackapi.FileSummary{}, err
	}
	return slackapi.FileSummary{ID: up.FileID, Title: name}, nil
}

// patiently runs call, waiting out a rate limit Slack asks a short wait for.
// chat.postMessage allows about one message a second per conversation, so a
// long answer in many parts meets it.
func patiently(ctx context.Context, call func() error) error {
	for attempt := 0; ; attempt++ {
		err := call()
		var rl *slackapi.RateLimitedError
		if !errors.As(err, &rl) || attempt >= 3 || rl.RetryAfter > retryWithin {
			return err
		}
		wait := max(rl.RetryAfter, time.Second)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Typing marks the message the Bot is working on with 👀, and takes the mark
// off when it is done. Slack shows apps no typing indicator; a reaction is
// what people read as "on it". The host repeats on, so a message is marked
// once and every mark is taken off at the end.
func (a *Adapter) Typing(ctx context.Context, ch *db.Channel, externalID string, on bool) error {
	c := a.conn(ch.ID)
	if c == nil {
		return errors.New("slack channel is not connected")
	}
	if !c.mark {
		return nil
	}
	conversation := targetID(externalID)
	if conversation == "" {
		return fmt.Errorf("%q is not a Slack conversation id", externalID)
	}
	key := ch.ID + "/" + conversation
	a.mu.Lock()
	ts := a.last[key]
	marked := slices.Clone(a.marked[key])
	a.mu.Unlock()
	if on {
		if ts == "" || slices.Contains(marked, ts) {
			return nil
		}
		err := c.api.AddReactionContext(ctx, workingReaction, slackapi.NewRefToMessage(conversation, ts))
		if err != nil && !isCode(err, "already_reacted") {
			return friendly(err)
		}
		a.mu.Lock()
		a.marked[key] = append(a.marked[key], ts)
		a.mu.Unlock()
		return nil
	}
	var errs []error
	for _, m := range marked {
		err := c.api.RemoveReactionContext(ctx, workingReaction, slackapi.NewRefToMessage(conversation, m))
		if err != nil && !isCode(err, "no_reaction") && !isCode(err, "message_not_found") {
			errs = append(errs, friendly(err))
		}
	}
	a.mu.Lock()
	a.marked[key] = slices.DeleteFunc(a.marked[key], func(m string) bool { return slices.Contains(marked, m) })
	if len(a.marked[key]) == 0 {
		delete(a.marked, key)
	}
	a.mu.Unlock()
	return errors.Join(errs...)
}

func isCode(err error, code string) bool {
	var se slackapi.SlackErrorResponse
	return errors.As(err, &se) && se.Err == code
}

// --- history ---

// History reads the bound conversation's recent messages, oldest first, with
// the replies of its newest threads under their first message: answers go to
// threads, so a conversation lives mostly in them.
func (a *Adapter) History(ctx context.Context, ch *db.Channel, cfg channels.Config, externalID string, limit int) ([]channels.Message, error) {
	external := strings.TrimSpace(externalID)
	if external == "" {
		external = ch.ExternalID
	}
	target := targetID(external)
	if target == "" {
		return nil, fmt.Errorf("chat %q is not a Slack conversation id", external)
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 100)
	c, err := a.session(ctx, ch, cfg)
	if err != nil {
		return nil, err
	}
	res, err := c.api.GetConversationHistoryContext(ctx, &slackapi.GetConversationHistoryParameters{ChannelID: target, Limit: limit})
	if err != nil {
		return nil, friendly(err)
	}
	top := slices.Clone(res.Messages)
	slices.Reverse(top) // the API returns newest first

	open := map[string]bool{}
	for i := len(top) - 1; i >= 0 && len(open) < historyThreads; i-- {
		if top[i].ReplyCount > 0 {
			open[top[i].Timestamp] = true
		}
	}
	var out []channels.Message
	for _, m := range top {
		if !spoken[m.SubType] && m.SubType != "bot_message" {
			continue
		}
		out = append(out, a.historyMessage(ctx, c, ch.ID, m.Msg, false))
		if !open[m.Timestamp] {
			continue
		}
		replies, err := a.replies(ctx, c, target, m.Timestamp)
		if err != nil {
			out = append(out, channels.Message{Text: fmt.Sprintf("[a thread of %d replies here could not be read: %v]", m.ReplyCount, err)})
			continue
		}
		for _, r := range replies {
			out = append(out, a.historyMessage(ctx, c, ch.ID, r.Msg, true))
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// replies is a thread's newest replies, its first message left out.
func (a *Adapter) replies(ctx context.Context, c *conn, conversation, root string) ([]slackapi.Message, error) {
	var all []slackapi.Message
	cursor := ""
	for page := 0; page < 5; page++ {
		msgs, more, next, err := c.api.GetConversationRepliesContext(ctx, &slackapi.GetConversationRepliesParameters{
			ChannelID: conversation, Timestamp: root, Cursor: cursor, Limit: 200,
		})
		if err != nil {
			return nil, friendly(err)
		}
		all = append(all, msgs...)
		if !more || next == "" {
			break
		}
		cursor = next
	}
	all = slices.DeleteFunc(all, func(m slackapi.Message) bool {
		return m.Timestamp == root || (!spoken[m.SubType] && m.SubType != "bot_message")
	})
	return all, nil
}

func (a *Adapter) historyMessage(ctx context.Context, c *conn, channelID string, m slackapi.Msg, inThread bool) channels.Message {
	out := channels.Message{At: tsTime(m.Timestamp)}
	switch {
	case m.User == c.self || (m.BotID != "" && m.BotID == c.botID):
		out.Author, out.Out = "Bot", true
	case m.BotID != "":
		out.Author = m.Username
		if out.Author == "" && m.BotProfile != nil {
			out.Author = m.BotProfile.Name
		}
		if out.Author == "" {
			out.Author = "an app"
		}
	default:
		out.Author = a.userName(ctx, c, channelID, m.User)
	}
	var lines []string
	if t := strings.TrimSpace(a.readable(ctx, c, channelID, m.Text)); t != "" {
		lines = append(lines, t)
	}
	for _, f := range m.Files {
		kind := ""
		if f.Mimetype != "" {
			kind = " (" + f.Mimetype + ")"
		}
		lines = append(lines, fmt.Sprintf("[attachment: %s%s]", f.Name, kind))
	}
	out.Text = strings.Join(lines, "\n")
	if inThread && out.Text != "" {
		out.Text = "[in thread] " + out.Text
	}
	return out
}

// tsTime reads a Slack timestamp, seconds.micros.
func tsTime(ts string) time.Time {
	sec, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	us, _ := strconv.ParseInt((frac + "000000")[:6], 10, 64)
	return time.Unix(s, us*1000)
}

// --- setup actions ---

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, cfg channels.Config, action string, _ map[string]string) (channels.State, error) {
	c := a.conn(ch.ID)
	if c == nil {
		return channels.State{Kind: channels.StateError, Message: "not connected"}, nil
	}
	switch action {
	case "list_channels":
		return a.listConversations(ctx, ch, c)
	case "refresh":
		fresh, err := a.login(ctx, c.api)
		if err != nil {
			return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
		}
		return channels.State{Kind: channels.StateInfo, Message: connectedMessage(fresh)}, nil
	}
	return channels.State{}, fmt.Errorf("unknown action %q", action)
}

// listConversations offers what the app can talk in: the channels it is a
// member of, group DMs it is in, and DMs people opened with it.
func (a *Adapter) listConversations(ctx context.Context, ch *db.Channel, c *conn) (channels.State, error) {
	var options []channels.Option
	cursor := ""
	for page := 0; page < 20; page++ {
		convs, next, err := c.api.GetConversationsContext(ctx, &slackapi.GetConversationsParameters{
			Cursor: cursor, ExcludeArchived: true, Limit: 200,
			Types: []string{"public_channel", "private_channel", "mpim", "im"},
		})
		if err != nil {
			return channels.State{}, friendly(err)
		}
		for i := range convs {
			conv := &convs[i]
			switch {
			case conv.IsIM:
				if conv.User == slackbot || conv.User == c.self {
					continue
				}
			case conv.IsMpIM:
			case !conv.IsMember:
				continue // a public channel the app is not in reads and sends nothing
			}
			label := conversationTitle(conv, "", func(id string) string { return a.userName(ctx, c, ch.ID, id) })
			if conv.IsPrivate && !conv.IsMpIM {
				label += " (private)"
			}
			options = append(options, channels.Option{Value: conv.ID, Label: label})
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(options) == 0 {
		return channels.State{
			Kind:    channels.StateSelect,
			Message: "The app is in no conversation yet. Invite it to a channel with /invite @" + c.name + ", or open a DM with it from its Messages tab, then try again. You can also paste a channel id or link below.",
		}, nil
	}
	slices.SortFunc(options, func(x, y channels.Option) int {
		return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return channels.State{Kind: channels.StateSelect, Message: fmt.Sprintf("%d conversations", len(options)), Options: options}, nil
}

// --- target ---

var (
	conversationRe = regexp.MustCompile(`^[CGD][A-Z0-9]{6,}$`)
	channelMenRe   = regexp.MustCompile(`^<#([CGD][A-Z0-9]{6,})(?:\|[^>]*)?>$`)
)

// targetID reads a conversation id from what the owner entered or picked: a
// bare id, a <#C…|name> reference, or a Slack link (…slack.com/archives/C…,
// app.slack.com/client/T…/C…, slack://channel?id=C…, app_redirect?channel=C…).
func targetID(s string) string {
	s = strings.TrimSpace(s)
	if m := channelMenRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if conversationRe.MatchString(s) {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	for _, key := range []string{"id", "channel"} {
		if v := u.Query().Get(key); conversationRe.MatchString(v) {
			return v
		}
	}
	if u.Host == "" {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, seg := range segs {
		switch {
		case seg == "archives" && i+1 < len(segs) && conversationRe.MatchString(segs[i+1]):
			return segs[i+1]
		case seg == "client" && i+2 < len(segs) && conversationRe.MatchString(segs[i+2]):
			return segs[i+2]
		}
	}
	return ""
}
