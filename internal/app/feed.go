package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
)

const (
	feedTitleMax = 120
	feedBodyMax  = 20000
	// feedMax keeps a Bot's Feed bounded; the oldest posts fall off.
	feedMax = 500

	// feedQuoteKind is the run event a quoted Feed post becomes in a new chat.
	// Body is the post; Tool carries the source label for the UI.
	feedQuoteKind = "feed_quote"
)

// feedSource names where a run lives: a web chat, an automation's log, or a
// channel conversation.
func (a *App) feedSource(chatID string) (kind, name string) {
	var c db.Chat
	a.DB.Where("id = ?", chatID).Limit(1).Find(&c)
	switch {
	case c.ID == "":
		return "", ""
	case c.AutomationID != "":
		var au db.Automation
		a.DB.Select("name").Where("id = ?", c.AutomationID).Limit(1).Find(&au)
		return "automation", au.Name
	case c.ChannelID != "":
		var ch db.Channel
		a.DB.Select("name").Where("id = ?", c.ChannelID).Limit(1).Find(&ch)
		return "channel", ch.Name
	}
	title := c.Title
	if untitledTitle(title) {
		title = "New chat"
	}
	return "chat", title
}

// postFeed adds one post to a Bot's Feed from the run in chatID.
func (a *App) postFeed(botID, chatID, runID, title, body string) (*db.FeedPost, error) {
	title = clipRunes(strings.ReplaceAll(title, "\n", " "), feedTitleMax)
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("text required")
	}
	if n := utf8.RuneCountInString(body); n > feedBodyMax {
		return nil, fmt.Errorf("post is %d characters; cap is %d. Put the long part in a workspace file and link it.", n, feedBodyMax)
	}
	kind, name := a.feedSource(chatID)
	p := db.FeedPost{
		ID: ids.New(), BotID: botID, Title: title, Body: body, SourceKind: kind, SourceName: name,
		ChatID: chatID, RunID: runID, CreatedAt: time.Now(),
	}
	if err := a.DB.Create(&p).Error; err != nil {
		return nil, err
	}
	a.DB.Where("bot_id = ? AND id NOT IN (?)", botID,
		a.DB.Model(&db.FeedPost{}).Select("id").Where("bot_id = ?", botID).Order("created_at desc").Limit(feedMax),
	).Delete(&db.FeedPost{})
	return &p, nil
}

// feedTool implements the `feed` chat tool and the Python `silo_runtime.feed`
// path. Both gate on bot.feed here; the source is the run's conversation.
func (a *App) feedTool(ctx context.Context, bot *db.Bot, runID string, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	title, _ := args["title"].(string)
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text required")
	}
	argsJSON, _ := json.Marshal(args)
	if _, err := a.authorizeAction(ctx, bot, runID, security.Bot, "feed", string(argsJSON), ""); err != nil {
		return "", err
	}
	p, err := a.postFeed(bot.ID, a.chatOfRun(runID), runID, title, text)
	if err != nil {
		return "", err
	}
	return "posted to the Feed (" + p.ID + ")", nil
}

func (a *App) feedUnread(botID string) int32 {
	var n int64
	a.DB.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", botID).Count(&n)
	return int32(n)
}

func protoFeedPost(p *db.FeedPost) *v1.FeedPost {
	return &v1.FeedPost{
		Id: p.ID, Title: p.Title, Body: p.Body, SourceKind: p.SourceKind, SourceName: p.SourceName,
		ChatId: p.ChatID, CreatedAt: p.CreatedAt.Format(time.RFC3339), Read: p.ReadAt != nil,
	}
}

// feedLabel is the human line for where a post came from.
func feedLabel(p *db.FeedPost) string {
	switch p.SourceKind {
	case "automation":
		return "automation “" + p.SourceName + "”"
	case "channel":
		return "channel “" + p.SourceName + "”"
	case "chat":
		return "chat “" + p.SourceName + "”"
	}
	return ""
}

// feedQuoteText is what the model sees for a quoted post: a user turn that
// frames the post, so the human's next message reads as a reply to it.
func feedQuoteText(label, body string, posted time.Time) string {
	var b strings.Builder
	b.WriteString("I'm quoting a post you made to my Feed")
	if !posted.IsZero() {
		fmt.Fprintf(&b, " on %s", posted.Local().Format("Mon 2006-01-02 15:04 MST"))
	}
	if label != "" {
		b.WriteString(" from " + label)
	}
	b.WriteString(":\n\n")
	for _, line := range strings.Split(body, "\n") {
		b.WriteString("> " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *App) ListFeed(ctx context.Context, req *connect.Request[v1.ListFeedRequest]) (*connect.Response[v1.ListFeedResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.FeedPost
	a.DB.Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListFeedResponse{}
	for i := range rows {
		out.Posts = append(out.Posts, protoFeedPost(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (a *App) MarkFeedRead(ctx context.Context, req *connect.Request[v1.MarkFeedReadRequest]) (*connect.Response[v1.MarkFeedReadResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	a.DB.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", req.Msg.GetBotId()).Update("read_at", time.Now())
	return connect.NewResponse(&v1.MarkFeedReadResponse{}), nil
}

func (a *App) ownFeedPost(ctx context.Context, botID, id string) (*db.FeedPost, error) {
	if _, err := a.ownBot(ctx, botID); err != nil {
		return nil, err
	}
	var p db.FeedPost
	a.DB.Where("bot_id = ? AND id = ?", botID, id).Limit(1).Find(&p)
	if p.ID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("feed post not found"))
	}
	return &p, nil
}

func (a *App) DeleteFeedPost(ctx context.Context, req *connect.Request[v1.DeleteFeedPostRequest]) (*connect.Response[v1.DeleteFeedPostResponse], error) {
	p, err := a.ownFeedPost(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := a.DB.Delete(p).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.DeleteFeedPostResponse{}), nil
}

// QuoteFeedPost starts a web chat whose only context is the quoted post, so
// the human can reply to it. The post stays in the Feed.
func (a *App) QuoteFeedPost(ctx context.Context, req *connect.Request[v1.QuoteFeedPostRequest]) (*connect.Response[v1.QuoteFeedPostResponse], error) {
	p, err := a.ownFeedPost(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	title := p.Title
	if title == "" {
		title = firstLine(p.Body)
	}
	now := time.Now()
	c := db.Chat{ID: ids.New(), BotID: p.BotID, Title: clipRunes("Re: "+strings.TrimLeft(title, "# "), feedTitleMax), CreatedAt: now, UpdatedAt: now}
	if err := a.DB.Create(&c).Error; err != nil {
		return nil, err
	}
	run := db.Run{ID: ids.New(), BotID: p.BotID, ChatID: c.ID, Origin: "feed", Status: "done", CreatedAt: now}
	if err := a.DB.Create(&run).Error; err != nil {
		return nil, err
	}
	body := p.Body
	if p.Title != "" {
		body = "**" + p.Title + "**\n\n" + body
	}
	// CreatedAt is the post's time: history frames the quote with when it was posted.
	ev := db.RunEvent{ID: ids.New(), RunID: run.ID, Kind: feedQuoteKind, Body: body, Tool: feedLabel(p), CreatedAt: p.CreatedAt}
	if err := a.DB.Create(&ev).Error; err != nil {
		return nil, err
	}
	a.DB.Model(p).Update("read_at", now)
	return connect.NewResponse(&v1.QuoteFeedPostResponse{Chat: protoChat(&c)}), nil
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
