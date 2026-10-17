// Package feed is the owner's Dashboard and read-only Feed inbox per Bot:
// modular form cards and posts the Bot writes from any run, the unread count,
// and submitting a card's form or quoting a post into a new chat.
package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
	"silo.agent/internal/zone"
)

const (
	feedTitleMax   = 120
	feedBodyMax    = 20000
	cardSummaryMax = 4000
	cardBlocksMax  = 32000
	// feedMax keeps a Bot's Feed bounded; the oldest posts fall off.
	feedMax = 500
	cardMax = 100

	// QuoteKind is the run event a quoted Feed post or submitted Dashboard card
	// becomes in a new chat. Body is the post/card; Tool carries the source label
	// for the UI.
	QuoteKind = "feed_quote"
)

// Host is what the Feed and Dashboard need from the App around them.
type Host interface {
	host.Authorizer
	host.Runs
	run.Engine
}

// Service owns the Feed and Dashboard: their tools, unread count and RPCs.
type Service struct {
	db     *gorm.DB
	host   Host
	memory *memory.Service
}

func New(gdb *gorm.DB, h Host, m *memory.Service) *Service {
	return &Service{db: gdb, host: h, memory: m}
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

// DashboardTool implements `list_dashboard`, `upsert_dashboard_card`, and
// `remove_dashboard_card` for reading and gradually editing Dashboard state.
func (s *Service) DashboardTool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any) (string, error) {
	action := map[string]string{
		"list_dashboard":        "dashboard_list",
		"upsert_dashboard_card": "dashboard_upsert",
		"remove_dashboard_card": "dashboard_remove",
	}[name]
	if action == "" {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	argsJSON, _ := json.Marshal(args)
	if _, err := s.host.AuthorizeAction(ctx, bot, runID, security.Bot, action, string(argsJSON), ""); err != nil {
		return "", err
	}
	switch name {
	case "list_dashboard":
		return s.listDashboardTool(bot.ID)
	case "upsert_dashboard_card":
		return s.upsertCardTool(bot.ID, s.host.ChatOfRun(runID), runID, args)
	case "remove_dashboard_card":
		ref, _ := args["id_or_key"].(string)
		if strings.TrimSpace(ref) == "" {
			ref, _ = args["id"].(string)
		}
		if strings.TrimSpace(ref) == "" {
			ref, _ = args["key"].(string)
		}
		return s.removeCardTool(bot.ID, ref)
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func (s *Service) listDashboardTool(botID string) (string, error) {
	var open []db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'open'", botID).
		Order("priority asc, updated_at desc").Find(&open)

	var dismissed []db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'dismissed'", botID).
		Order("dismissed_at desc").Limit(25).Find(&dismissed)

	type cardView struct {
		ID           string `json:"id"`
		Key          string `json:"key,omitempty"`
		Title        string `json:"title"`
		Summary      string `json:"summary,omitempty"`
		Priority     int    `json:"priority"`
		Blocks       any    `json:"blocks"`
		SubmitLabel  string `json:"submit_label,omitempty"`
		DraftState   string `json:"draft_state"`
		DraftContext string `json:"draft_context,omitempty"`
		UpdatedAt    string `json:"updated_at"`
	}
	type dismissalView struct {
		ID          string `json:"id"`
		Key         string `json:"key,omitempty"`
		Title       string `json:"title"`
		Suppressed  bool   `json:"suppressed"`
		DismissedAt string `json:"dismissed_at,omitempty"`
	}
	out := struct {
		Cards            []cardView      `json:"cards"`
		RecentDismissals []dismissalView `json:"recent_dismissals"`
	}{
		Cards:            make([]cardView, 0, len(open)),
		RecentDismissals: make([]dismissalView, 0, len(dismissed)),
	}
	for _, c := range open {
		var blocks any = []any{}
		if c.BlocksJSON != "" {
			_ = json.Unmarshal([]byte(c.BlocksJSON), &blocks)
		}
		out.Cards = append(out.Cards, cardView{
			ID:           c.ID,
			Key:          c.Key,
			Title:        c.Title,
			Summary:      c.Summary,
			Priority:     c.Priority,
			Blocks:       blocks,
			SubmitLabel:  c.SubmitLabel,
			DraftState:   c.DraftState,
			DraftContext: c.DraftContext,
			UpdatedAt:    c.UpdatedAt.Format(time.RFC3339),
		})
	}
	for _, d := range dismissed {
		at := ""
		if d.DismissedAt != nil {
			at = d.DismissedAt.Format(time.RFC3339)
		}
		out.RecentDismissals = append(out.RecentDismissals, dismissalView{
			ID:          d.ID,
			Key:         d.Key,
			Title:       d.Title,
			Suppressed:  d.Suppressed,
			DismissedAt: at,
		})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func normalizeBlocksJSON(raw any) (string, bool, error) {
	if raw == nil {
		return "", false, nil
	}
	switch v := raw.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return "[]", true, nil
		}
		var arr []map[string]any
		if err := json.Unmarshal([]byte(v), &arr); err != nil {
			return "", false, fmt.Errorf("blocks must be a JSON array of block objects: %w", err)
		}
		b, _ := json.Marshal(arr)
		return string(b), len(arr) > 0, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false, err
		}
		var arr []map[string]any
		if err := json.Unmarshal(b, &arr); err != nil {
			return "", false, fmt.Errorf("blocks must be a list of block dicts: %w", err)
		}
		norm, _ := json.Marshal(arr)
		return string(norm), len(arr) > 0, nil
	}
}

func (s *Service) upsertCardTool(botID, chatID, runID string, args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	key, _ := args["key"].(string)
	id = strings.TrimSpace(id)
	key = strings.TrimSpace(key)

	var existing db.DashboardCard
	if id != "" {
		s.db.Where("bot_id = ? AND id = ? AND status = 'open'", botID, id).Limit(1).Find(&existing)
	}
	if existing.ID == "" && key != "" {
		s.db.Where("bot_id = ? AND key = ? AND status = 'open'", botID, key).Order("updated_at desc").Limit(1).Find(&existing)
	}

	creating := existing.ID == ""
	now := time.Now()
	c := existing
	if creating {
		kind, sname := chats.Source(s.db, chatID)
		c = db.DashboardCard{
			ID:          ids.New(),
			BotID:       botID,
			Key:         textx.ClipRunes(key, 80),
			Priority:    2,
			BlocksJSON:  "[]",
			SubmitLabel: "Send",
			DraftState:  "ready",
			Status:      "open",
			SourceKind:  kind,
			SourceName:  sname,
			ChatID:      chatID,
			RunID:       runID,
			CreatedAt:   now,
		}
	}

	changed := creating
	if key != "" && c.Key != key {
		c.Key = textx.ClipRunes(key, 80)
		changed = true
	}
	if v, ok := args["title"].(string); ok && strings.TrimSpace(v) != "" {
		t := textx.ClipRunes(strings.ReplaceAll(strings.TrimSpace(v), "\n", " "), feedTitleMax)
		if c.Title != t {
			c.Title = t
			changed = true
		}
	}
	if c.Title == "" {
		return "", errors.New("title required")
	}
	if v, ok := args["summary"].(string); ok {
		sum := textx.ClipRunes(strings.TrimSpace(v), cardSummaryMax)
		if c.Summary != sum {
			c.Summary = sum
			changed = true
		}
	}
	if v, ok := args["priority"]; ok && v != nil {
		var p int
		switch n := v.(type) {
		case float64:
			p = int(n)
		case int:
			p = n
		}
		if p < 1 {
			p = 1
		}
		if p > 3 {
			p = 3
		}
		if c.Priority != p {
			c.Priority = p
			changed = true
		}
	}
	if v, ok := args["submit_label"].(string); ok {
		lbl := textx.ClipRunes(strings.TrimSpace(v), 48)
		if lbl == "" {
			lbl = "Send"
		}
		if c.SubmitLabel != lbl {
			c.SubmitLabel = lbl
			changed = true
		}
	}
	if v, ok := args["draft_context"].(string); ok {
		dc := textx.ClipRunes(strings.TrimSpace(v), cardSummaryMax)
		if c.DraftContext != dc {
			c.DraftContext = dc
			changed = true
		}
	}
	if rawBlocks, hasBlocks := args["blocks"]; hasBlocks && rawBlocks != nil {
		norm, nonEmpty, err := normalizeBlocksJSON(rawBlocks)
		if err != nil {
			return "", err
		}
		if utf8.RuneCountInString(norm) > cardBlocksMax {
			return "", fmt.Errorf("blocks is over %d characters", cardBlocksMax)
		}
		if c.BlocksJSON != norm {
			c.BlocksJSON = norm
			changed = true
		}
		if nonEmpty {
			c.DraftState = "ready"
		}
	} else if creating && c.DraftContext != "" && (c.BlocksJSON == "" || c.BlocksJSON == "[]") {
		c.DraftState = "lazy"
	}
	if v, ok := args["draft_state"].(string); ok && (v == "ready" || v == "lazy") {
		c.DraftState = v
	}

	c.UpdatedAt = now
	if changed && !creating {
		c.ReadAt = nil
	}
	if creating {
		if err := s.db.Create(&c).Error; err != nil {
			return "", err
		}
		s.db.Where("bot_id = ? AND status = 'open' AND id NOT IN (?)", botID,
			s.db.Model(&db.DashboardCard{}).Select("id").Where("bot_id = ? AND status = 'open'", botID).Order("updated_at desc").Limit(cardMax),
		).Delete(&db.DashboardCard{})
	} else {
		if err := s.db.Save(&c).Error; err != nil {
			return "", err
		}
	}
	out, _ := json.Marshal(map[string]any{
		"id":      c.ID,
		"key":     c.Key,
		"title":   c.Title,
		"created": creating,
		"updated": !creating && changed,
	})
	return string(out), nil
}

func (s *Service) removeCardTool(botID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("id_or_key required")
	}
	var c db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'open' AND (id = ? OR key = ?)", botID, ref, ref).Limit(1).Find(&c)
	if c.ID == "" {
		return "", fmt.Errorf("no open dashboard card matching %q", ref)
	}
	if err := s.db.Delete(&c).Error; err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]any{"removed": c.ID, "key": c.Key, "title": c.Title})
	return string(out), nil
}

func (s *Service) Unread(botID string) int32 {
	var posts, cards int64
	s.db.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", botID).Count(&posts)
	s.db.Model(&db.DashboardCard{}).Where("bot_id = ? AND status = 'open' AND read_at IS NULL", botID).Count(&cards)
	return int32(posts + cards)
}

func protoFeedPost(p *db.FeedPost) *v1.FeedPost {
	return &v1.FeedPost{
		Id: p.ID, Title: p.Title, Body: p.Body, SourceKind: p.SourceKind, SourceName: p.SourceName,
		ChatId: p.ChatID, CreatedAt: p.CreatedAt.Format(time.RFC3339), Read: p.ReadAt != nil,
	}
}

func protoDashboardCard(c *db.DashboardCard) *v1.DashboardCard {
	blocks := c.BlocksJSON
	if blocks == "" {
		blocks = "[]"
	}
	submit := c.SubmitLabel
	if submit == "" {
		submit = "Send"
	}
	state := c.DraftState
	if state == "" {
		state = "ready"
	}
	return &v1.DashboardCard{
		Id:           c.ID,
		BotId:        c.BotID,
		Key:          c.Key,
		Title:        c.Title,
		Summary:      c.Summary,
		Priority:     int32(c.Priority),
		BlocksJson:   blocks,
		SubmitLabel:  submit,
		DraftState:   state,
		DraftContext: c.DraftContext,
		Status:       c.Status,
		SourceKind:   c.SourceKind,
		SourceName:   c.SourceName,
		ChatId:       c.ChatID,
		CreatedAt:    c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.Format(time.RFC3339),
		Read:         c.ReadAt != nil,
	}
}

// feedLabel is the human line for where a post came from.
func feedLabel(p *db.FeedPost) string {
	return sourceLabel(p.SourceKind, p.SourceName)
}

func sourceLabel(kind, name string) string {
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
		out.Posts = append(out.Posts, protoFeedPost(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) MarkFeedRead(ctx context.Context, req *connect.Request[v1.MarkFeedReadRequest]) (*connect.Response[v1.MarkFeedReadResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	now := time.Now()
	s.db.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", req.Msg.GetBotId()).Update("read_at", now)
	s.db.Model(&db.DashboardCard{}).Where("bot_id = ? AND status = 'open' AND read_at IS NULL", req.Msg.GetBotId()).Update("read_at", now)
	return connect.NewResponse(&v1.MarkFeedReadResponse{}), nil
}

func (s *Service) ownPost(ctx context.Context, botID, id string) (*db.FeedPost, error) {
	return access.OwnBotRow[db.FeedPost](ctx, s.db, botID, id, "feed post")
}

func (s *Service) ownCard(ctx context.Context, botID, id string) (*db.DashboardCard, error) {
	return access.OwnBotRow[db.DashboardCard](ctx, s.db, botID, id, "dashboard card")
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
	now := time.Now()
	c := db.Chat{ID: ids.New(), BotID: p.BotID, Title: textx.ClipRunes("Re: "+strings.TrimLeft(title, "# "), feedTitleMax), CreatedAt: now, UpdatedAt: now}
	if err := s.db.Create(&c).Error; err != nil {
		return nil, err
	}
	r := db.Run{ID: ids.New(), BotID: p.BotID, ChatID: c.ID, Origin: "feed", Status: "done", CreatedAt: now}
	if err := s.db.Create(&r).Error; err != nil {
		return nil, err
	}
	body := p.Body
	if p.Title != "" {
		body = "**" + p.Title + "**\n\n" + body
	}
	// CreatedAt is the post's time: history frames the quote with when it was posted.
	ev := db.RunEvent{ID: ids.New(), RunID: r.ID, Kind: QuoteKind, Body: body, Tool: feedLabel(p), Zone: zone.OfBot(s.db, p.BotID).String(), CreatedAt: p.CreatedAt}
	if err := s.db.Create(&ev).Error; err != nil {
		return nil, err
	}
	s.db.Model(p).Update("read_at", now)
	return connect.NewResponse(&v1.QuoteFeedPostResponse{Chat: chats.Proto(&c)}), nil
}

// --- Dashboard RPCs ---

func (s *Service) ListDashboard(ctx context.Context, req *connect.Request[v1.ListDashboardRequest]) (*connect.Response[v1.ListDashboardResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
		return nil, err
	}
	var cards []db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'open'", botID).
		Order("priority asc, updated_at desc").Find(&cards)
	var posts []db.FeedPost
	s.db.Where("bot_id = ?", botID).Order("created_at desc").Find(&posts)
	out := &v1.ListDashboardResponse{}
	for i := range cards {
		out.Cards = append(out.Cards, protoDashboardCard(&cards[i]))
	}
	for i := range posts {
		out.Posts = append(out.Posts, protoFeedPost(&posts[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) MarkDashboardRead(ctx context.Context, req *connect.Request[v1.MarkDashboardReadRequest]) (*connect.Response[v1.MarkDashboardReadResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
		return nil, err
	}
	now := time.Now()
	s.db.Model(&db.DashboardCard{}).Where("bot_id = ? AND status = 'open' AND read_at IS NULL", botID).Update("read_at", now)
	s.db.Model(&db.FeedPost{}).Where("bot_id = ? AND read_at IS NULL", botID).Update("read_at", now)
	return connect.NewResponse(&v1.MarkDashboardReadResponse{}), nil
}

func (s *Service) DismissDashboardCard(ctx context.Context, req *connect.Request[v1.DismissDashboardCardRequest]) (*connect.Response[v1.DismissDashboardCardResponse], error) {
	c, err := s.ownCard(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	suppress := req.Msg.GetSuppressKind()
	if err := s.db.Model(c).Updates(map[string]any{
		"status":       "dismissed",
		"suppressed":   suppress,
		"dismissed_at": now,
		"read_at":      now,
		"updated_at":   now,
	}).Error; err != nil {
		return nil, err
	}
	if suppress && s.memory != nil {
		topic := c.Title
		if c.Key != "" {
			topic = fmt.Sprintf("%s (%s)", c.Title, c.Key)
		}
		lesson := fmt.Sprintf("Owner dismissed Dashboard card %q with 'Don't show this kind'. Do not propose Dashboard cards for this topic again unless explicitly asked.", topic)
		_, _ = s.memory.Save(ctx, memory.Entry{
			BotID:   c.BotID,
			Kind:    memory.Lesson,
			Content: textx.ClipRunes(lesson, memory.RememberMax),
		})
	}
	return connect.NewResponse(&v1.DismissDashboardCardResponse{}), nil
}

func formatCardQuoteBody(c *db.DashboardCard) string {
	var b strings.Builder
	if c.Title != "" {
		b.WriteString("**" + c.Title + "**")
	}
	if c.Summary != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(c.Summary)
	}
	var blocks []map[string]any
	if c.BlocksJSON != "" {
		_ = json.Unmarshal([]byte(c.BlocksJSON), &blocks)
	}
	for _, blk := range blocks {
		t, _ := blk["type"].(string)
		switch t {
		case "markdown":
			if md, _ := blk["text"].(string); strings.TrimSpace(md) != "" {
				b.WriteString("\n\n" + strings.TrimSpace(md))
			}
		case "diff":
			if p, _ := blk["patch"].(string); strings.TrimSpace(p) != "" {
				b.WriteString("\n\n```diff\n" + strings.TrimSpace(p) + "\n```")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func formatSubmittedTurn(c *db.DashboardCard, valuesJSON, note string) string {
	var b strings.Builder
	label := c.SubmitLabel
	if label == "" {
		label = "Send"
	}
	var vals map[string]any
	_ = json.Unmarshal([]byte(valuesJSON), &vals)
	if len(vals) > 0 {
		b.WriteString(fmt.Sprintf("Submitted **%s** on Dashboard card **%s** with options:\n", label, c.Title))
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := vals[k]
			switch tv := v.(type) {
			case string:
				b.WriteString(fmt.Sprintf("- `%s`: %s\n", k, tv))
			case bool:
				b.WriteString(fmt.Sprintf("- `%s`: %t\n", k, tv))
			default:
				raw, _ := json.Marshal(tv)
				b.WriteString(fmt.Sprintf("- `%s`: %s\n", k, string(raw)))
			}
		}
	} else {
		b.WriteString(fmt.Sprintf("Submitted **%s** on Dashboard card **%s**.", label, c.Title))
	}
	if strings.TrimSpace(note) != "" {
		b.WriteString("\n\n" + strings.TrimSpace(note))
	}
	b.WriteString("\n\nPlease carry this out and update `bot/DASHBOARD.md` if needed.")
	return strings.TrimSpace(b.String())
}

// SubmitDashboardCard creates a web chat quoting the Dashboard card, marks the
// card submitted, and immediately starts a run with the user's structured form
// values as the user message.
func (s *Service) SubmitDashboardCard(ctx context.Context, req *connect.Request[v1.SubmitDashboardCardRequest]) (*connect.Response[v1.SubmitDashboardCardResponse], error) {
	c, err := s.ownCard(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	title := c.Title
	if title == "" {
		title = "Dashboard card"
	}
	ch := db.Chat{
		ID:        ids.New(),
		BotID:     c.BotID,
		Title:     textx.ClipRunes(strings.TrimLeft(title, "# "), feedTitleMax),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.db.Create(&ch).Error; err != nil {
		return nil, err
	}
	qRun := db.Run{ID: ids.New(), BotID: c.BotID, ChatID: ch.ID, Origin: "feed", Status: "done", CreatedAt: now}
	if err := s.db.Create(&qRun).Error; err != nil {
		return nil, err
	}
	ev := db.RunEvent{
		ID:        ids.New(),
		RunID:     qRun.ID,
		Kind:      QuoteKind,
		Body:      formatCardQuoteBody(c),
		Tool:      sourceLabel(c.SourceKind, c.SourceName),
		Zone:      zone.OfBot(s.db, c.BotID).String(),
		CreatedAt: c.CreatedAt,
	}
	if err := s.db.Create(&ev).Error; err != nil {
		return nil, err
	}
	_ = s.db.Model(c).Updates(map[string]any{
		"status":     "submitted",
		"read_at":    now,
		"updated_at": now,
	}).Error

	userText := formatSubmittedTurn(c, req.Msg.GetValuesJson(), req.Msg.GetNote())
	runID, err := s.host.StartOrInject(c.BotID, ch.ID, userText, run.Extra{}, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.SubmitDashboardCardResponse{
		Chat:  chats.Proto(&ch),
		RunId: runID,
	}), nil
}

// HydrateDashboardCard triggers a background run in the card's automation or
// a hidden run to flesh out a lazy card's blocks, or marks it ready if context
// is already present.
func (s *Service) HydrateDashboardCard(ctx context.Context, req *connect.Request[v1.HydrateDashboardCardRequest]) (*connect.Response[v1.DashboardCard], error) {
	c, err := s.ownCard(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	var blocks []map[string]any
	_ = json.Unmarshal([]byte(c.BlocksJSON), &blocks)
	if len(blocks) == 0 && strings.TrimSpace(c.DraftContext) != "" {
		blocks = []map[string]any{
			{"type": "markdown", "text": c.DraftContext},
		}
		b, _ := json.Marshal(blocks)
		c.BlocksJSON = string(b)
	}
	c.DraftState = "ready"
	c.UpdatedAt = time.Now()
	_ = s.db.Save(c).Error
	return connect.NewResponse(protoDashboardCard(c)), nil
}
