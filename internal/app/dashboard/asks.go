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
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/quote"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/toolarg"
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

// askInput is an ask as a tool gives it. A nil field is not given: on an
// update it stays as it is.
type askInput struct {
	ID, Key, Title string
	Summary        *string
	Priority       *int
	SubmitLabel    *string
	// Blocks is a list of block objects or that list as JSON text.
	Blocks any
}

func askArgs(args map[string]any) askInput {
	in := askInput{ID: toolarg.Str(args, "id"), Key: toolarg.Str(args, "key"), Title: toolarg.Str(args, "title"), Blocks: args["blocks"]}
	if v, ok := args["summary"].(string); ok {
		in.Summary = &v
	}
	if p := toolarg.Int(args, "priority"); p != 0 {
		in.Priority = &p
	}
	if v, ok := args["submit_label"].(string); ok {
		in.SubmitLabel = &v
	}
	return in
}

// upsertAsk creates or updates an open ask by id or key: only the fields
// given change. chatID and runID are where it was asked from; the chat it is
// shown in, if any, is the caller's to record (showIn).
func (s *Service) upsertAsk(botID, chatID, runID string, in askInput) (c *db.DashboardCard, created, changed bool, err error) {
	id := strings.TrimSpace(in.ID)
	key := strings.TrimSpace(in.Key)

	c = &db.DashboardCard{}
	if id != "" {
		s.db.Where("bot_id = ? AND id = ? AND status = 'open'", botID, id).Limit(1).Find(c)
	}
	if c.ID == "" && key != "" {
		s.db.Where("bot_id = ? AND key = ? AND status = 'open'", botID, key).Order("updated_at desc").Limit(1).Find(c)
	}
	created = c.ID == ""
	now := time.Now()
	if created {
		kind, name := chats.Source(s.db, chatID)
		*c = db.DashboardCard{
			ID: ids.New(), BotID: botID, Key: textx.ClipRunes(key, 80), Priority: 2, BlocksJSON: "[]",
			SubmitLabel: "Send", Status: "open", SourceKind: kind, SourceName: name,
			RunID: runID, CreatedAt: now,
		}
	}
	changed = created
	set := func(dst *string, v string) {
		if *dst != v {
			*dst = v
			changed = true
		}
	}
	if key != "" {
		set(&c.Key, textx.ClipRunes(key, 80))
	}
	if v := strings.TrimSpace(in.Title); v != "" {
		set(&c.Title, textx.ClipRunes(strings.ReplaceAll(v, "\n", " "), titleMax))
	}
	if c.Title == "" {
		return nil, false, false, errors.New("title required")
	}
	if in.Summary != nil {
		set(&c.Summary, textx.ClipRunes(strings.TrimSpace(*in.Summary), summaryMax))
	}
	if in.Priority != nil {
		if p := min(max(*in.Priority, 1), 3); c.Priority != p {
			c.Priority = p
			changed = true
		}
	}
	if in.SubmitLabel != nil {
		lbl := textx.ClipRunes(strings.TrimSpace(*in.SubmitLabel), 48)
		if lbl == "" {
			lbl = "Send"
		}
		set(&c.SubmitLabel, lbl)
	}
	if in.Blocks != nil {
		norm, err := normalizeBlocks(in.Blocks)
		if err != nil {
			return nil, false, false, err
		}
		if utf8.RuneCountInString(norm) > blocksMax {
			return nil, false, false, fmt.Errorf("blocks is over %d characters", blocksMax)
		}
		set(&c.BlocksJSON, norm)
	}
	c.UpdatedAt = now
	if created {
		if err := s.db.Create(c).Error; err != nil {
			return nil, false, false, err
		}
		s.db.Where("bot_id = ? AND status = 'open' AND id NOT IN (?)", botID,
			s.db.Model(&db.DashboardCard{}).Select("id").Where("bot_id = ? AND status = 'open'", botID).Order("updated_at desc").Limit(askMax),
		).Delete(&db.DashboardCard{})
		return c, true, true, nil
	}
	if !changed {
		return c, false, false, nil
	}
	// Only what the Bot sets, and only while the card is open: the owner may
	// answer it at the same moment, and that answer stands.
	res := s.db.Model(&db.DashboardCard{}).Where("id = ? AND status = 'open'", c.ID).Updates(map[string]any{
		"key": c.Key, "title": c.Title, "summary": c.Summary, "priority": c.Priority, "submit_label": c.SubmitLabel,
		"blocks_json": c.BlocksJSON, "read_at": nil, "updated_at": now,
	})
	if res.Error != nil {
		return nil, false, false, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, false, false, errors.New("the owner answered that card meanwhile; ask again if it still matters")
	}
	return c, false, true, nil
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
	suppress := req.Msg.GetSuppressKind()
	if err := s.claim(c, "dismissed", map[string]any{"suppressed": suppress, "dismissed_at": time.Now()}); err != nil {
		return nil, err
	}
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
func (s *Service) quoteChat(botID, title string, q quote.Quote, message string, own bool) (run.Placed, error) {
	if title == "" {
		title = "Dashboard"
	}
	return s.host.Place(run.Placement{
		BotID: botID, Title: title, Own: own, Message: message,
		Write: func(ch *db.Chat) (*db.RunEvent, error) { return quote.Put(s.db, ch, q) },
	})
}

// claim moves an open card to status (with more columns), refusing one the
// owner already answered: a second press must not answer it twice.
func (s *Service) claim(c *db.DashboardCard, status string, more map[string]any) error {
	now := time.Now()
	upd := map[string]any{"status": status, "read_at": now, "updated_at": now}
	for k, v := range more {
		upd[k] = v
	}
	res := s.db.Model(&db.DashboardCard{}).Where("id = ? AND status = 'open'", c.ID).Updates(upd)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("this card is answered already"))
	}
	c.Status = status
	return nil
}

// SubmitDashboardCard opens a chat quoting the ask and starts a run with the
// owner's chosen values as the message.
func (s *Service) SubmitDashboardCard(ctx context.Context, req *connect.Request[v1.SubmitDashboardCardRequest]) (*connect.Response[v1.SubmitDashboardCardResponse], error) {
	c, err := s.ownCard(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.claim(c, "submitted", nil); err != nil {
		return nil, err
	}
	turn := submittedTurn(c, req.Msg.GetValuesJson(), req.Msg.GetNote())
	var ch *db.Chat
	var runID string
	if in := s.answerIn(c); in != "" && !req.Msg.GetNewChat() {
		// The card is in that chat already: the answer follows it there.
		runID, err = s.host.StartOrInject(c.BotID, in, turn, run.Extra{}, nil)
		ch = &db.Chat{}
		s.db.Where("id = ?", in).Limit(1).Find(ch)
	} else {
		q := quote.Quote{From: quote.FromAsk, Title: c.Title, Body: cardQuote(c), Source: chats.SourceLabel(c.SourceKind, c.SourceName), At: c.CreatedAt}
		var placed run.Placed
		placed, err = s.quoteChat(c.BotID, c.Title, q, turn, req.Msg.GetNewChat())
		ch, runID = placed.Chat, placed.RunID
	}
	if err != nil {
		// Nothing heard it: the card is open again for another try.
		s.db.Model(&db.DashboardCard{}).Where("id = ?", c.ID).Updates(map[string]any{"status": "open", "updated_at": time.Now()})
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	s.cardChanged(c)
	return connect.NewResponse(&v1.SubmitDashboardCardResponse{Chat: chats.Proto(ch), RunId: runID}), nil
}
