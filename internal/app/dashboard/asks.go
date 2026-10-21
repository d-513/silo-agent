package dashboard

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

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/feed"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/quote"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

const (
	titleMax      = 120
	summaryMax    = 4000
	blocksMax     = 32000
	askMax        = 100
	dismissalsMax = 25
)

// upsertAsk creates or updates an open ask by id or key: only the fields
// given change.
func (s *Service) upsertAsk(botID, chatID, runID string, args map[string]any) (string, error) {
	id := strings.TrimSpace(str(args, "id"))
	key := strings.TrimSpace(str(args, "key"))

	var c db.DashboardCard
	if id != "" {
		s.db.Where("bot_id = ? AND id = ? AND status = 'open'", botID, id).Limit(1).Find(&c)
	}
	if c.ID == "" && key != "" {
		s.db.Where("bot_id = ? AND key = ? AND status = 'open'", botID, key).Order("updated_at desc").Limit(1).Find(&c)
	}
	creating := c.ID == ""
	now := time.Now()
	if creating {
		kind, name := chats.Source(s.db, chatID)
		c = db.DashboardCard{
			ID: ids.New(), BotID: botID, Key: textx.ClipRunes(key, 80), Priority: 2, BlocksJSON: "[]",
			SubmitLabel: "Send", Status: "open", SourceKind: kind, SourceName: name,
			ChatID: chatID, RunID: runID, CreatedAt: now,
		}
	}
	changed := creating
	set := func(dst *string, v string) {
		if *dst != v {
			*dst = v
			changed = true
		}
	}
	if key != "" {
		set(&c.Key, textx.ClipRunes(key, 80))
	}
	if v := strings.TrimSpace(str(args, "title")); v != "" {
		set(&c.Title, textx.ClipRunes(strings.ReplaceAll(v, "\n", " "), titleMax))
	}
	if c.Title == "" {
		return "", errors.New("title required")
	}
	if v, ok := args["summary"].(string); ok {
		set(&c.Summary, textx.ClipRunes(strings.TrimSpace(v), summaryMax))
	}
	if v, ok := args["priority"].(float64); ok {
		p := min(max(int(v), 1), 3)
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
		set(&c.SubmitLabel, lbl)
	}
	if raw, ok := args["blocks"]; ok && raw != nil {
		norm, err := s.cleanBlocks(raw)
		if err != nil {
			return "", err
		}
		if utf8.RuneCountInString(norm) > blocksMax {
			return "", fmt.Errorf("blocks is over %d characters", blocksMax)
		}
		set(&c.BlocksJSON, norm)
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
			s.db.Model(&db.DashboardCard{}).Select("id").Where("bot_id = ? AND status = 'open'", botID).Order("updated_at desc").Limit(askMax),
		).Delete(&db.DashboardCard{})
	} else if err := s.db.Save(&c).Error; err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]any{"id": c.ID, "key": c.Key, "title": c.Title, "created": creating, "updated": !creating && changed})
	return string(out), nil
}

// openAsks are the asks waiting for the owner, most pressing first.
func (s *Service) openAsks(botID string) []db.DashboardCard {
	var out []db.DashboardCard
	s.db.Where("bot_id = ? AND status = 'open'", botID).Order("priority asc, updated_at desc").Find(&out)
	return out
}

// normalizeBlocks accepts a list of block objects, or that list as JSON text.
func normalizeBlocks(raw any) (string, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	if s, ok := raw.(string); ok {
		if strings.TrimSpace(s) == "" {
			return "[]", nil
		}
		b = []byte(s)
	}
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return "", fmt.Errorf("blocks must be a list of block objects: %w", err)
	}
	norm, _ := json.Marshal(arr)
	return string(norm), nil
}

func (s *Service) removeAsk(botID, ref string) (string, error) {
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

func protoCard(c *db.DashboardCard) *v1.DashboardCard {
	blocks := c.BlocksJSON
	if blocks == "" {
		blocks = "[]"
	}
	submit := c.SubmitLabel
	if submit == "" {
		submit = "Send"
	}
	return &v1.DashboardCard{
		Id: c.ID, BotId: c.BotID, Key: c.Key, Title: c.Title, Summary: c.Summary,
		Priority: int32(c.Priority), BlocksJson: blocks, SubmitLabel: submit, Status: c.Status,
		SourceKind: c.SourceKind, SourceName: c.SourceName, ChatId: c.ChatID,
		CreatedAt: c.CreatedAt.Format(time.RFC3339), UpdatedAt: c.UpdatedAt.Format(time.RFC3339), Read: c.ReadAt != nil,
	}
}

func (s *Service) ownCard(ctx context.Context, botID, id string) (*db.DashboardCard, error) {
	return access.OwnBotRow[db.DashboardCard](ctx, s.db, botID, id, "dashboard card")
}

// DismissDashboardCard puts an ask away; suppress_kind also saves a memory so
// the Bot does not propose that topic again.
func (s *Service) DismissDashboardCard(ctx context.Context, req *connect.Request[v1.DismissDashboardCardRequest]) (*connect.Response[v1.DismissDashboardCardResponse], error) {
	c, err := s.ownCard(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	suppress := req.Msg.GetSuppressKind()
	if err := s.db.Model(c).Updates(map[string]any{
		"status": "dismissed", "suppressed": suppress, "dismissed_at": now, "read_at": now, "updated_at": now,
	}).Error; err != nil {
		return nil, err
	}
	c.Status = "dismissed"
	s.cardChanged(c)
	// A question put aside in a chat is an answer too: the Bot carries on.
	if in := s.answerIn(c); in != "" {
		turn := fmt.Sprintf("Not now for your question “%s”.", c.Title)
		if suppress {
			turn = fmt.Sprintf("Please don't ask me about “%s” again.", c.Title)
		}
		if _, err := s.host.StartOrInject(c.BotID, in, turn, run.Extra{}, nil); err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
	}
	if suppress && s.memory != nil {
		topic := c.Title
		if c.Key != "" {
			topic = fmt.Sprintf("%s (%s)", c.Title, c.Key)
		}
		note := fmt.Sprintf("Owner dismissed Dashboard card %q with 'Don't show this kind'. Do not propose Dashboard cards for this topic again unless explicitly asked.", topic)
		_, _ = s.memory.Save(ctx, memory.Entry{BotID: c.BotID, Content: textx.ClipRunes(note, memory.RememberMax)})
	}
	return connect.NewResponse(&v1.DismissDashboardCardResponse{}), nil
}

func cardQuote(c *db.DashboardCard) string {
	var b strings.Builder
	b.WriteString(c.Summary)
	var blocks []map[string]any
	_ = json.Unmarshal([]byte(c.BlocksJSON), &blocks)
	for _, blk := range blocks {
		switch blk["type"] {
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

// valuesText lists submitted form values for the model, sorted by name.
func valuesText(valuesJSON string) string {
	var vals map[string]any
	_ = json.Unmarshal([]byte(valuesJSON), &vals)
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		switch v := vals[k].(type) {
		case string:
			fmt.Fprintf(&b, "- `%s`: %s\n", k, v)
		case bool:
			fmt.Fprintf(&b, "- `%s`: %t\n", k, v)
		default:
			raw, _ := json.Marshal(v)
			fmt.Fprintf(&b, "- `%s`: %s\n", k, raw)
		}
	}
	return b.String()
}

// submittedTurn is the owner's answer to an ask, in their own voice: the
// quote above it says which card it answers.
func submittedTurn(c *db.DashboardCard, valuesJSON, note string) string {
	var b strings.Builder
	label := c.SubmitLabel
	if label == "" {
		label = "Send"
	}
	if vals := valuesText(valuesJSON); vals != "" {
		fmt.Fprintf(&b, "I chose **%s** with:\n%s", label, vals)
	} else {
		fmt.Fprintf(&b, "I chose **%s**.", label)
	}
	if strings.TrimSpace(note) != "" {
		b.WriteString("\n\n" + strings.TrimSpace(note))
	}
	b.WriteString("\n\nPlease go ahead.")
	return strings.TrimSpace(b.String())
}

// quoteChat puts q in the main chat (or, asked for or with a reply running
// there, a chat of its own) and starts a run with the owner's message.
func (s *Service) quoteChat(botID, title string, q quote.Quote, message string, own bool) (*db.Chat, string, error) {
	if title == "" {
		title = "Dashboard"
	}
	main, ok := chats.MainIdle(s.db, botID, s.host.LiveRunID)
	if own || !ok {
		ch, err := quote.Open(s.db, botID, title, q)
		if err != nil {
			return nil, "", err
		}
		runID, err := s.host.StartOrInject(botID, ch.ID, message, run.Extra{}, nil)
		if err != nil {
			chats.Drop(s.db, ch.ID)
			return nil, "", connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return ch, runID, nil
	}
	if err := quote.Into(s.db, main, q); err != nil {
		return nil, "", err
	}
	runID, err := s.host.StartOrInject(botID, main.ID, message, run.Extra{}, nil)
	if err != nil {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return main, runID, nil
}

// SubmitDashboardCard opens a chat quoting the ask and starts a run with the
// owner's chosen values as the message.
func (s *Service) SubmitDashboardCard(ctx context.Context, req *connect.Request[v1.SubmitDashboardCardRequest]) (*connect.Response[v1.SubmitDashboardCardResponse], error) {
	c, err := s.ownCard(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	turn := submittedTurn(c, req.Msg.GetValuesJson(), req.Msg.GetNote())
	var ch *db.Chat
	var runID string
	if in := s.answerIn(c); in != "" && !req.Msg.GetNewChat() {
		// The card is in that chat already: the answer follows it there.
		runID, err = s.host.StartOrInject(c.BotID, in, turn, run.Extra{}, nil)
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		ch = &db.Chat{}
		s.db.Where("id = ?", in).Limit(1).Find(ch)
	} else {
		q := quote.Quote{From: quote.FromAsk, Title: c.Title, Body: cardQuote(c), Source: feed.SourceLabel(c.SourceKind, c.SourceName), At: c.CreatedAt}
		ch, runID, err = s.quoteChat(c.BotID, c.Title, q, turn, req.Msg.GetNewChat())
		if err != nil {
			return nil, err
		}
	}
	now := time.Now()
	_ = s.db.Model(c).Updates(map[string]any{"status": "submitted", "read_at": now, "updated_at": now}).Error
	c.Status = "submitted"
	s.cardChanged(c)
	return connect.NewResponse(&v1.SubmitDashboardCardResponse{Chat: chats.Proto(ch), RunId: runID}), nil
}

func str(args map[string]any, k string) string {
	v, _ := args[k].(string)
	return v
}
