package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/quote"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

// CardKind is the run event that shows an ask in a chat's thread. Its body is
// the card's id; the thread is sent the card as it stands now (Proto), so a
// card answered on the Dashboard reads as answered in the chat too.
const CardKind = "card"

// CardStateKind is a transient event the chat's viewers get when a card in
// it changes (answered or put away): body is the card's id.
const CardStateKind = "card_state"

// askOwner is the ask_owner tool: a question for the owner, with an optional
// card (display blocks and form controls). Asked in a web chat it shows in
// that chat and ends the turn. Asked from anywhere else (an automation, a
// channel) it goes to the main chat when the Bot may write there unasked and
// nothing runs there, and always waits on the Dashboard under Needs you.
func (s *Service) askOwner(ctx context.Context, bot *db.Bot, runID string, args map[string]any) (string, error) {
	text := strings.TrimSpace(toolarg.Str(args, "text"))
	if text == "" {
		return "", errors.New("text required: the question, in a sentence or two")
	}
	in := askArgs(args)
	in.ID = ""
	if in.Title = strings.TrimSpace(in.Title); in.Title == "" {
		in.Title = textx.FirstLine(text)
	}
	in.Summary = &text
	if in.Blocks == nil {
		in.Blocks = []any{}
	}
	chatID := s.host.ChatOfRun(runID)
	var ch db.Chat
	if chatID != "" {
		s.db.Where("id = ?", chatID).Limit(1).Find(&ch)
	}
	c, _, _, err := s.upsertAsk(bot.ID, chatID, runID, in)
	if err != nil {
		return "", err
	}
	shown, note := "the Dashboard", "it waits for the owner; carry on with your task"
	if chats.Web(&ch) {
		s.showIn(c, ch.ID)
		s.host.Emit(bot.ID, ch.ID, runID, CardKind, c.ID, "")
		// The question is the turn's last word: the owner answers it.
		s.host.EndTurn(runID)
		shown, note = "this chat", "shown to the owner; your turn ends here and their answer arrives as their next message"
	} else {
		q := quote.Quote{From: quote.FromAsked, Title: c.Title, Body: text, Source: chats.SourceLabel(c.SourceKind, c.SourceName), Card: c.ID}
		_, err := s.host.Place(run.Placement{BotID: bot.ID, Proactive: true, Write: func(main *db.Chat) (*db.RunEvent, error) {
			s.showIn(c, main.ID)
			return quote.Put(s.db, main, q)
		}})
		switch {
		case err == nil:
			shown = "the main chat"
		case !errors.Is(err, run.ErrNotMain):
			return "", err
		}
	}
	out, _ := json.Marshal(map[string]any{"id": c.ID, "shown_in": shown, "note": note})
	return string(out), nil
}

// showIn records the chat a card is shown in: where its answer goes.
func (s *Service) showIn(c *db.DashboardCard, chatID string) {
	c.ChatID = chatID
	s.db.Model(&db.DashboardCard{}).Where("id = ?", c.ID).Update("chat_id", chatID)
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
		s.host.Publish(c.BotID, &v1.RunEvent{ChatId: c.ChatID, Kind: CardStateKind, Body: c.ID, Card: protoCard(c)})
	}
}

// answerIn is the chat an answer to the card goes to directly, with no quote:
// the web chat it is shown in. "" when it lives only on the Dashboard, and
// the answer quotes it instead.
func (s *Service) answerIn(c *db.DashboardCard) string {
	if c.ChatID == "" {
		return ""
	}
	var ch db.Chat
	s.db.Where("id = ? AND bot_id = ?", c.ChatID, c.BotID).Limit(1).Find(&ch)
	if !chats.Web(&ch) {
		return ""
	}
	return ch.ID
}
