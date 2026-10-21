// Package feed is the owner's read-only Feed inbox per Bot: posts the Bot
// writes from any run, the unread count, and quoting a post into a new chat.
package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
	"silo.agent/internal/zone"
)

const (
	feedTitleMax = 120
	feedBodyMax  = 20000
	// feedMax keeps a Bot's Feed bounded; the oldest posts fall off.
	feedMax = 500

	// QuoteKind is the run event a quoted Feed post or submitted Dashboard card
	// becomes in a new chat. Body is the post/card; Tool carries the source label
	// for the UI.
	QuoteKind = "feed_quote"
)

// Host is what the Feed needs from the App around them.
type Host interface {
	host.Authorizer
	host.Runs
	run.Engine
}

// Service owns the Feed: its tool, unread count and RPCs.
type Service struct {
	db   *gorm.DB
	host Host
}

func New(gdb *gorm.DB, h Host) *Service {
	return &Service{db: gdb, host: h}
}

// post adds one post to a Bot's Feed from the run in chatID.
func (s *Service) post(botID, chatID, runID, title, body string) (*db.FeedPost, error) {
	title = textx.ClipRunes(strings.ReplaceAll(title, "\n", " "), feedTitleMax)
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("text required")
	}
	if n := utf8.RuneCountInString(body); n > feedBodyMax {
		return nil, fmt.Errorf("post is %d characters; cap is %d. Put the long part in a workspace file and link it.", n, feedBodyMax)
	}
	kind, name := chats.Source(s.db, chatID)
	p := db.FeedPost{
		ID: ids.New(), BotID: botID, Title: title, Body: body, SourceKind: kind, SourceName: name,
		ChatID: chatID, RunID: runID, CreatedAt: time.Now(),
	}
	if err := s.db.Create(&p).Error; err != nil {
		return nil, err
	}
	s.db.Where("bot_id = ? AND id NOT IN (?)", botID,
		s.db.Model(&db.FeedPost{}).Select("id").Where("bot_id = ?", botID).Order("created_at desc").Limit(feedMax),
	).Delete(&db.FeedPost{})
	return &p, nil
}

// Tool implements the `feed` chat tool and the Python `silo_runtime.feed`
// path. Both gate on bot.feed here; the source is the run's conversation.
func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID string, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	title, _ := args["title"].(string)
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text required")
	}
	argsJSON, _ := json.Marshal(args)
	if _, err := s.host.AuthorizeAction(ctx, bot, runID, security.Bot, "feed", string(argsJSON), ""); err != nil {
		return "", err
	}
	p, err := s.post(bot.ID, s.host.ChatOfRun(runID), runID, title, text)
	if err != nil {
		return "", err
	}
	return "posted to the Feed (" + p.ID + ")", nil
}

// Unread is how many Feed posts the owner has not read.
func (s *Service) Unread(botID string) int32 {
	var posts int64
	s.db.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", botID).Count(&posts)
	return int32(posts)
}

// Proto is a post as the UI reads it.
func Proto(p *db.FeedPost) *v1.FeedPost {
	return &v1.FeedPost{
		Id: p.ID, Title: p.Title, Body: p.Body, SourceKind: p.SourceKind, SourceName: p.SourceName,
		ChatId: p.ChatID, CreatedAt: p.CreatedAt.Format(time.RFC3339), Read: p.ReadAt != nil,
	}
}

// feedLabel is the human line for where a post came from.
func feedLabel(p *db.FeedPost) string {
	return SourceLabel(p.SourceKind, p.SourceName)
}

// SourceLabel is the human line for a run source: kind and name.
func SourceLabel(kind, name string) string {
	switch kind {
	case "automation":
		return "automation “" + name + "”"
	case "channel":
		return "channel “" + name + "”"
	case "chat":
		return "chat “" + name + "”"
	}
	return ""
}

// QuoteText is what the model sees for a quoted post: a user turn that
// frames the post, so the human's next message reads as a reply to it.
// zoneName is the Bot's zone when the post was quoted, kept on the event so
// the turn replays as the same text wherever the Bot is later.
func QuoteText(label, body string, posted time.Time, zoneName string) string {
	var b strings.Builder
	b.WriteString("I'm quoting a post you made to my Feed")
	switch {
	case posted.IsZero():
	case zoneName == "":
		// A quote from before Bots had a zone: the bytes it always had.
		fmt.Fprintf(&b, " on %s", posted.Local().Format("Mon 2006-01-02 15:04 MST"))
	default:
		fmt.Fprintf(&b, " on %s", zone.Stamp(posted, zone.Resolve(zoneName)))
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

func (s *Service) ListFeed(ctx context.Context, req *connect.Request[v1.ListFeedRequest]) (*connect.Response[v1.ListFeedResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.FeedPost
	s.db.Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListFeedResponse{}
	for i := range rows {
		out.Posts = append(out.Posts, Proto(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) MarkFeedRead(ctx context.Context, req *connect.Request[v1.MarkFeedReadRequest]) (*connect.Response[v1.MarkFeedReadResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	now := time.Now()
	s.MarkRead(req.Msg.GetBotId(), now)
	return connect.NewResponse(&v1.MarkFeedReadResponse{}), nil
}

// MarkRead counts every post of the Bot read as of now.
func (s *Service) MarkRead(botID string, now time.Time) {
	s.db.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", botID).Update("read_at", now)
}

// UnreadPosts are the newest posts the owner has not read, at most n.
func (s *Service) UnreadPosts(botID string, n int) []*v1.FeedPost {
	var posts []db.FeedPost
	s.db.Where("bot_id = ? AND read_at IS NULL", botID).Order("created_at desc").Limit(n).Find(&posts)
	out := make([]*v1.FeedPost, len(posts))
	for i := range posts {
		out[i] = Proto(&posts[i])
	}
	return out
}

func (s *Service) ownPost(ctx context.Context, botID, id string) (*db.FeedPost, error) {
	return access.OwnBotRow[db.FeedPost](ctx, s.db, botID, id, "feed post")
}

func (s *Service) DeleteFeedPost(ctx context.Context, req *connect.Request[v1.DeleteFeedPostRequest]) (*connect.Response[v1.DeleteFeedPostResponse], error) {
	p, err := s.ownPost(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.db.Delete(p).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.DeleteFeedPostResponse{}), nil
}

// QuoteFeedPost starts a web chat whose only context is the quoted post, so
// the human can reply to it. The post stays in the Feed.
func (s *Service) QuoteFeedPost(ctx context.Context, req *connect.Request[v1.QuoteFeedPostRequest]) (*connect.Response[v1.QuoteFeedPostResponse], error) {
	p, err := s.ownPost(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	title := p.Title
	if title == "" {
		title = textx.FirstLine(p.Body)
	}
	body := p.Body
	if p.Title != "" {
		body = "**" + p.Title + "**\n\n" + body
	}
	// The quote keeps the post's time: history frames it with when it was posted.
	c, err := QuoteChat(s.db, p.BotID, "Re: "+strings.TrimLeft(title, "# "), body, feedLabel(p), p.CreatedAt)
	if err != nil {
		return nil, err
	}
	s.db.Model(p).Update("read_at", time.Now())
	return connect.NewResponse(&v1.QuoteFeedPostResponse{Chat: chats.Proto(c)}), nil
}

// QuoteChat opens a web chat whose only context is a quote: one finished run
// holding a QuoteKind event (body = what is quoted, tool = where it came from,
// at = when it was written). The human's next message reads as a reply to it.
// The Feed and the Dashboard both open chats this way.
func QuoteChat(gdb *gorm.DB, botID, title, quote, label string, at time.Time) (*db.Chat, error) {
	now := time.Now()
	c := db.Chat{ID: ids.New(), BotID: botID, Title: textx.ClipRunes(strings.TrimLeft(title, "# "), feedTitleMax), CreatedAt: now, UpdatedAt: now}
	if err := gdb.Create(&c).Error; err != nil {
		return nil, err
	}
	r := db.Run{ID: ids.New(), BotID: botID, ChatID: c.ID, Origin: "feed", Status: "done", CreatedAt: now}
	if err := gdb.Create(&r).Error; err != nil {
		return nil, err
	}
	ev := db.RunEvent{ID: ids.New(), RunID: r.ID, Kind: QuoteKind, Body: quote, Tool: label, Zone: zone.OfBot(gdb, botID).String(), CreatedAt: at}
	if err := gdb.Create(&ev).Error; err != nil {
		return nil, err
	}
	return &c, nil
}
