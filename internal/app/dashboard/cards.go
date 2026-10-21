package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/feed"
	"silo.agent/internal/app/quote"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

// CardKind is the run event that shows an ask in a chat's thread. Its body is
// the card's id; the thread is sent the card as it stands now (Proto), so a
// card answered on the Dashboard reads as answered in the chat too.
const CardKind = "card"

// CardStateKind is a transient event the chat's viewers get when a card in
// it changes (answered, dismissed, a setup done): body is the card's id.
const CardStateKind = "card_state"

// cleanBlocks is normalizeBlocks plus the checks a setup block needs.
func (s *Service) cleanBlocks(raw any) (string, error) {
	norm, err := normalizeBlocks(raw)
	if err != nil {
		return "", err
	}
	var blocks []map[string]any
	_ = json.Unmarshal([]byte(norm), &blocks)
	setups := false
	for _, blk := range blocks {
		if blk["type"] == "setup" {
			if err := s.checkSetup(blk); err != nil {
				return "", err
			}
			setups = true
		}
	}
	if !setups {
		return norm, nil
	}
	out, _ := json.Marshal(blocks)
	return string(out), nil
}

// askOwner is the ask_owner tool: a question for the owner, with an optional
// card (display blocks, form controls, setup offers). Asked in a web chat it
// shows in that chat. Asked from anywhere else (an automation, a channel) it
// goes to the main chat when nothing runs there, and always waits on the
// Dashboard under Needs you.
func (s *Service) askOwner(ctx context.Context, bot *db.Bot, runID string, args map[string]any) (string, error) {
	text := strings.TrimSpace(str(args, "text"))
	if text == "" {
		return "", errors.New("text required: the question, in a sentence or two")
	}
	title := strings.TrimSpace(str(args, "title"))
	if title == "" {
		title = textx.FirstLine(text)
	}
	chatID := s.host.ChatOfRun(runID)
	var ch db.Chat
	if chatID != "" {
		s.db.Where("id = ?", chatID).Limit(1).Find(&ch)
	}
	put := map[string]any{"title": title, "summary": text, "key": args["key"], "submit_label": args["submit_label"]}
	if b, ok := args["blocks"]; ok && b != nil {
		put["blocks"] = b
	} else {
		put["blocks"] = []any{}
	}
	if put["key"] == nil {
		delete(put, "key")
	}
	if put["submit_label"] == nil {
		delete(put, "submit_label")
	}
	raw, err := s.upsertAsk(bot.ID, chatID, runID, put)
	if err != nil {
		return "", err
	}
	var made struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(raw), &made)
	var c db.DashboardCard
	s.db.Where("id = ?", made.ID).Limit(1).Find(&c)
	if c.ID == "" {
		return "", errors.New("the card was not saved")
	}
	shown := "the Dashboard"
	switch {
	case webChat(&ch):
		s.db.Model(&db.DashboardCard{}).Where("id = ?", c.ID).Update("chat_id", ch.ID)
		s.host.Emit(bot.ID, ch.ID, runID, CardKind, c.ID, "")
		shown = "this chat"
	default:
		main, idle := chats.MainIdle(s.db, bot.ID, s.host.LiveRunID)
		if !idle || !s.host.Proactive(bot.ID) {
			break
		}
		ev, err := quote.Put(s.db, main, quote.Quote{From: quote.FromAsked, Title: c.Title, Body: text, Source: feed.SourceLabel(c.SourceKind, c.SourceName), Card: c.ID})
		if err != nil {
			return "", err
		}
		s.db.Model(&db.DashboardCard{}).Where("id = ?", c.ID).Update("chat_id", main.ID)
		s.host.Shown(bot.ID, main.ID, ev)
		shown = "the main chat"
	}
	out, _ := json.Marshal(map[string]any{"id": c.ID, "shown_in": shown, "note": "the owner's answer comes back as their next message"})
	return string(out), nil
}

// webChat is a conversation the owner reads in Silo: not an automation's or
// a subagent's log, not a channel conversation.
func webChat(c *db.Chat) bool {
	return c != nil && c.ID != "" && c.AutomationID == "" && c.SubagentID == "" && c.ChannelID == ""
}

// CardProto is a card as a thread shows it, nil when it is gone.
func (s *Service) CardProto(id string) *v1.DashboardCard {
	if id == "" {
		return nil
	}
	var c db.DashboardCard
	if s.db.Where("id = ?", id).Limit(1).Find(&c); c.ID == "" {
		return nil
	}
	return protoCard(&c)
}

// cardChanged tells the viewers of the chat a card is shown in that it moved.
func (s *Service) cardChanged(c *db.DashboardCard) {
	if c.ChatID != "" {
		s.host.CardChanged(c.BotID, c.ChatID, protoCard(c))
	}
}

// answerIn is the chat an answer to the card goes to directly, with no quote:
// the web chat it is shown in. "" when it lives only on the Dashboard (or in
// an automation's log), and the answer quotes it instead.
func (s *Service) answerIn(c *db.DashboardCard) string {
	if c.ChatID == "" {
		return ""
	}
	var ch db.Chat
	s.db.Where("id = ? AND bot_id = ?", c.ChatID, c.BotID).Limit(1).Find(&ch)
	if !webChat(&ch) {
		return ""
	}
	return ch.ID
}

// ResolveCardSetup records what the owner did with one setup block and tells
// the Bot, so it carries on: "I set up Email." starts (or joins) a run in the
// card's chat.
func (s *Service) ResolveCardSetup(ctx context.Context, req *connect.Request[v1.ResolveCardSetupRequest]) (*connect.Response[v1.DashboardCard], error) {
	c, err := s.ownCard(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	outcome := req.Msg.GetOutcome()
	if outcome != setupDone && outcome != setupSkipped {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("outcome is done or skipped"))
	}
	i := int(req.Msg.GetIndex())
	label, ok := setupLabel(c, i)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no setup block at that index"))
	}
	state := map[string]string{}
	_ = json.Unmarshal([]byte(c.SetupJSON), &state)
	key := strconv.Itoa(i)
	if state[key] == outcome {
		return connect.NewResponse(protoCard(c)), nil
	}
	state[key] = outcome
	raw, _ := json.Marshal(state)
	c.SetupJSON = string(raw)
	c.UpdatedAt = time.Now()
	s.db.Model(&db.DashboardCard{}).Where("id = ?", c.ID).Updates(map[string]any{"setup_json": c.SetupJSON, "updated_at": c.UpdatedAt})
	s.cardChanged(c)

	turn := fmt.Sprintf("I set up **%s**.", label)
	if outcome == setupSkipped {
		turn = fmt.Sprintf("I'll skip **%s** for now.", label)
	}
	if chat := s.answerIn(c); chat != "" {
		if _, err := s.host.StartOrInject(c.BotID, chat, turn, run.Extra{}, nil); err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
	} else {
		q := quote.Quote{From: quote.FromAsk, Title: c.Title, Body: cardQuote(c), Source: feed.SourceLabel(c.SourceKind, c.SourceName), At: c.CreatedAt}
		if _, _, err := s.quoteChat(c.BotID, c.Title, q, turn, false); err != nil {
			return nil, err
		}
	}
	return connect.NewResponse(protoCard(c)), nil
}
