package teams

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// outgoing is an activity the bot posts. It never carries entities, and Teams
// makes a mention only from an entity: whatever a model writes ("@everyone",
// "<at>General</at>") is shown as words and notifies nobody.
type outgoing struct {
	Type        string       `json:"type"`
	From        *account     `json:"from,omitempty"`
	Text        string       `json:"text,omitempty"`
	TextFormat  string       `json:"textFormat,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
}

// inlineKinds are the pictures Teams draws from a data: address.
var inlineKinds = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true}

// inline turns the files of a message into attachments, or says which one
// Teams will not take: any other file needs Teams' own consent card, which a
// person has to accept before the bot may upload.
func inline(files []channels.Attachment) ([]attachment, error) {
	var out []attachment
	for _, f := range files {
		kind, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(f.Mime)), ";")
		if !inlineKinds[kind] {
			return nil, fmt.Errorf("%s cannot be sent over Teams: a bot can only put small PNG, JPEG or GIF pictures in a message", nameOf(f))
		}
		if len(f.Data) > maxInlineImage {
			return nil, fmt.Errorf("%s is too large to send over Teams: a picture in a message can be at most %d KB", nameOf(f), maxInlineImage>>10)
		}
		out = append(out, attachment{
			ContentType: kind,
			ContentURL:  "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(f.Data),
			Name:        f.Name,
		})
	}
	return out, nil
}

func nameOf(f channels.Attachment) string {
	if f.Name == "" {
		return "the file"
	}
	return f.Name
}

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, cfg channels.Config, msg channels.Outbound) error {
	c := credsOf(cfg)
	a.keep(ch, c)
	target := msg.ExternalID
	if target == "" {
		target = ch.ExternalID
	}
	target, _ = splitConversation(target)
	if target == "" {
		return errors.New("teams channel has no conversation yet: pick one in Set up")
	}
	// Refused before anything is sent: the host says the text again with a
	// note that the file could not be attached.
	pictures, err := inline(msg.Files)
	if err != nil {
		return err
	}
	chunks := channels.Chunk(msg.Text, maxMessageRunes)
	if len(chunks) == 0 && len(pictures) == 0 {
		return nil
	}
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	for i, text := range chunks {
		out := outgoing{Type: "message", Text: text}
		if text != "" {
			out.TextFormat = "markdown"
		}
		if i == len(chunks)-1 {
			out.Attachments = pictures
		}
		if err := a.post(ctx, ch.ID, c, target, msg.ReplyTo, out); err != nil {
			return err
		}
	}
	return nil
}

// Typing shows the bot typing for a few seconds. Teams has no signal that
// ends it: the next message does, or it runs out by itself.
func (a *Adapter) Typing(ctx context.Context, ch *db.Channel, externalID string, on bool) error {
	if !on {
		return nil
	}
	c, ok := a.credsFor(ch.ID)
	if !ok {
		return errors.New("teams channel has not started")
	}
	conv, _ := splitConversation(externalID)
	return a.post(ctx, ch.ID, c, conv, a.thread(ch.ID, conv), outgoing{Type: "typing"})
}

// post sends one activity to a conversation, into a thread when root names
// its first message.
func (a *Adapter) post(ctx context.Context, channelID string, c credentials, conv, root string, out outgoing) error {
	ref, ok := a.conversation(channelID, conv)
	if !ok {
		return errors.New("the bot has not been written to in that Teams conversation yet, so Teams has not said where to reach it: message the bot there first")
	}
	if ref.Bot != "" {
		out.From = &account{ID: ref.Bot}
	}
	body, err := json.Marshal(out)
	if err != nil {
		return err
	}
	id := conv
	if root != "" {
		id += ";messageid=" + root
	}
	endpoint := strings.TrimRight(ref.ServiceURL, "/") + "/v3/conversations/" + url.PathEscape(id) + "/activities"
	for attempt := 0; ; attempt++ {
		wait, err := a.postOnce(ctx, channelID, c, endpoint, body)
		if wait <= 0 || attempt > 0 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// postOnce makes the call. A positive wait means Teams asked for a pause
// short enough to be worth sitting out.
func (a *Adapter) postOnce(ctx context.Context, channelID string, c credentials, endpoint string, body []byte) (wait time.Duration, err error) {
	tok, err := a.token(ctx, channelID, c)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("cannot reach Teams: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0, nil
	}
	var refusal struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&refusal)
	why := strings.TrimSpace(refusal.Error.Message)
	if why == "" {
		why = strings.TrimSpace(refusal.Message)
	}
	if why == "" {
		why = "HTTP " + strconv.Itoa(resp.StatusCode)
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		if s, convErr := strconv.Atoi(resp.Header.Get("Retry-After")); convErr == nil && s > 0 && s <= 10 {
			wait = time.Duration(s) * time.Second
		}
		return wait, errors.New("Teams is limiting how fast the bot may write: " + why)
	case http.StatusUnauthorized:
		return 0, errors.New("Teams refused the bot's token — check that the Microsoft App ID, tenant and client secret are the Azure Bot's: " + why)
	case http.StatusForbidden:
		return 0, errors.New("Teams does not let the bot write there — it may have been removed from the chat or team: " + why)
	case http.StatusNotFound:
		return 0, errors.New("Teams does not know that conversation any more: " + why)
	case http.StatusRequestEntityTooLarge:
		return 0, errors.New("the message is too large for Teams: " + why)
	}
	return 0, errors.New("Teams refused the message: " + why)
}
