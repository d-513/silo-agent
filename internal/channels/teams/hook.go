package teams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// fileDownload is the attachment Teams sends for a file someone shared with
// the bot: its content names an address the file can be read from.
const fileDownload = "application/vnd.microsoft.teams.file.download.info"

// account is a person or a bot as Teams names them.
type account struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AADObjectID string `json:"aadObjectId"`
	Role        string `json:"role"`
}

type attachment struct {
	ContentType string          `json:"contentType"`
	ContentURL  string          `json:"contentUrl,omitempty"`
	Name        string          `json:"name,omitempty"`
	Content     json.RawMessage `json:"content,omitempty"`
}

// activity is what Teams sends: a message, or something else that happened.
type activity struct {
	Type         string  `json:"type"`
	ID           string  `json:"id"`
	ServiceURL   string  `json:"serviceUrl"`
	ChannelID    string  `json:"channelId"`
	From         account `json:"from"`
	Recipient    account `json:"recipient"`
	Text         string  `json:"text"`
	Conversation struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"conversationType"`
	} `json:"conversation"`
	Entities []struct {
		Type      string  `json:"type"`
		Text      string  `json:"text"`
		Mentioned account `json:"mentioned"`
	} `json:"entities"`
	Attachments []attachment `json:"attachments"`
	ChannelData struct {
		Team    struct{ Name string } `json:"team"`
		Channel struct{ Name string } `json:"channel"`
	} `json:"channelData"`
}

// Hook takes one activity from Teams. The host has checked nothing: the token
// is verified before the body is read, and the body's service address must be
// the one the token was made for, since that is where answers (and the bot's
// own token) will go.
func (a *Adapter) Hook(w http.ResponseWriter, r *http.Request, ch *db.Channel, cfg channels.Config, host channels.Host) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c := credsOf(cfg)
	signedFor, err := a.verify(r.Context(), c.appID, bearer(r))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}
	var act activity
	if json.Unmarshal(body, &act) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !sameService(signedFor, act.ServiceURL) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !isHTTPS(act.ServiceURL) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	a.keep(ch, c)
	// The answer does not wait for the Bot, and the Bot must not stop when
	// this request ends.
	if err := a.onActivity(context.WithoutCancel(r.Context()), ch, cfg, host, act); err != nil {
		log.Printf("teams channel %s: inbound: %v", ch.ID, err)
	}
	w.WriteHeader(http.StatusOK)
}

func (a *Adapter) onActivity(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, act activity) error {
	if act.ChannelID != teamsChannel || act.Conversation.ID == "" {
		return nil
	}
	live := ch
	if cur, ok := host.CurrentChannel(ch.ID); ok {
		live = cur
	}
	conv, root := splitConversation(act.Conversation.ID)
	title := titleOf(act)
	// Any signed activity shows where a conversation is served: being added to
	// a team is how a channel becomes pickable before anyone writes.
	seen := conversation{ID: conv, ServiceURL: act.ServiceURL, Title: title, Kind: act.Conversation.Type, Bot: act.Recipient.ID}
	if values := a.remember(ch.ID, seen, live.ExternalID); values != nil {
		st := endpointState(cfg)
		st.Values = values
		host.PublishState(ch.ID, st)
	}

	if act.Type != "message" || act.From.Role == "bot" || act.From.ID == act.Recipient.ID {
		return nil
	}
	if live.ExternalID == "" || live.ExternalID != conv {
		return nil
	}
	if act.Conversation.Type != "personal" && cfg.Get("reply_mode") != "always" && !mentionsBot(act) {
		return nil
	}
	text := readText(act)
	files := a.mediaOf(ch.ID, credsOf(cfg), act)
	if text == "" && len(files) == 0 {
		return nil
	}
	if act.ID != "" && a.seen.Add(ch.ID+"/"+act.ID) {
		return nil
	}
	a.setThread(ch.ID, conv, root)
	author := act.From.AADObjectID
	if author == "" {
		author = act.From.ID
	}
	return host.DeliverInbound(ctx, live, channels.Inbound{
		ExternalID: conv,
		Title:      seen.label(),
		Author:     act.From.Name,
		AuthorID:   author,
		Text:       text,
		ReplyTo:    root,
		Files:      files,
		Raw:        map[string]any{"id": act.ID, "conversation_type": act.Conversation.Type},
	})
}

// titleOf names a conversation: the person in a personal chat, the team and
// channel where Teams says them (it often gives only ids, and then "").
func titleOf(act activity) string {
	switch act.Conversation.Type {
	case "personal":
		if act.From.Role != "bot" && act.From.Name != "" {
			return act.From.Name
		}
		return ""
	case "channel":
		team, channel := act.ChannelData.Team.Name, act.ChannelData.Channel.Name
		if channel == "" {
			channel = act.Conversation.Name
		}
		switch {
		case team != "" && channel != "":
			return team + " / " + channel
		case team != "":
			return team
		}
		return channel
	}
	return act.Conversation.Name
}

// mentionsBot reports whether the message @mentions the bot. The entities
// say so; the text is the sender's to write as they like.
func mentionsBot(act activity) bool {
	for _, e := range act.Entities {
		if e.Type == "mention" && e.Mentioned.ID != "" && e.Mentioned.ID == act.Recipient.ID {
			return true
		}
	}
	return false
}

var atTag = regexp.MustCompile(`(?s)<at[^>]*>(.*?)</at>`)

// readText is the message as its sender wrote it: the bot's own mention taken
// out, other people's kept as @Name, and Teams' HTML entities read back.
func readText(act activity) string {
	text := act.Text
	for _, e := range act.Entities {
		if e.Type == "mention" && e.Mentioned.ID == act.Recipient.ID && e.Text != "" {
			text = strings.ReplaceAll(text, e.Text, " ")
		}
	}
	// Tags go before the entities are decoded, so a typed "&lt;at&gt;" stays text.
	text = atTag.ReplaceAllStringFunc(text, func(tag string) string {
		name := strings.TrimPrefix(strings.TrimSpace(atTag.FindStringSubmatch(tag)[1]), "@")
		if name == "" {
			return ""
		}
		return "@" + name
	})
	text = strings.ReplaceAll(html.UnescapeString(text), " ", " ")
	return strings.TrimSpace(text)
}

// mediaOf lists the files a message came with. Cards and the HTML copy of the
// message Teams attaches beside the text are not files.
func (a *Adapter) mediaOf(channelID string, c credentials, act activity) []channels.Media {
	var out []channels.Media
	for _, att := range act.Attachments {
		kind := strings.ToLower(strings.TrimSpace(att.ContentType))
		switch {
		case kind == fileDownload:
			var info struct {
				DownloadURL string `json:"downloadUrl"`
				FileType    string `json:"fileType"`
			}
			if json.Unmarshal(att.Content, &info) != nil || !isHTTPS(info.DownloadURL) {
				continue
			}
			name := att.Name
			if name == "" {
				name = "file"
				if info.FileType != "" {
					name += "." + info.FileType
				}
			}
			out = append(out, channels.Media{
				Name: name,
				Mime: mime.TypeByExtension(strings.ToLower(path.Ext(name))),
				// The address carries its own short-lived permission.
				Fetch: func(ctx context.Context) ([]byte, error) {
					return a.download(ctx, info.DownloadURL, "")
				},
			})
		case strings.HasPrefix(kind, "image/") && isHTTPS(att.ContentURL):
			name := att.Name
			if name == "" {
				name = "image"
				if exts, _ := mime.ExtensionsByType(kind); len(exts) > 0 {
					name += exts[0]
				}
			}
			address, service := att.ContentURL, act.ServiceURL
			out = append(out, channels.Media{
				Name: name,
				Mime: kind,
				Fetch: func(ctx context.Context) ([]byte, error) {
					// A picture pasted into a message is served by the
					// connector, which wants the bot's token. The token goes
					// nowhere but the host the signed activity came from.
					tok := ""
					if sameHost(address, service) {
						t, err := a.token(ctx, channelID, c)
						if err != nil {
							return nil, err
						}
						tok = t
					}
					return a.download(ctx, address, tok)
				},
			})
		}
	}
	return out
}

func sameHost(x, y string) bool {
	ux, errX := url.Parse(x)
	uy, errY := url.Parse(y)
	return errX == nil && errY == nil && ux.Host != "" && strings.EqualFold(ux.Host, uy.Host)
}

// download reads a file from an https address, with the bot's token when one
// is given. A redirect is followed without it (net/http drops the header when
// the host changes).
func (a *Adapter) download(ctx context.Context, address, tok string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("the file's address could not be read")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		// The address can carry a permission: say what failed, not where.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("download from Teams failed: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, errors.New("Teams refused to hand over the file: its link expired, or Microsoft no longer accepts a bot's client secret for files")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("download from Teams failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("download from Teams failed: %w", err)
	}
	if len(data) > maxDownload {
		return nil, errors.New("the file is too large to download")
	}
	return data, nil
}
