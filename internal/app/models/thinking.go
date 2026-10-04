package models

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// ThinkingLevels is what a model accepts, least reasoning first: an operator
// per-model override (thinking.levels), then the provider's report
// (llm.Thinker: OpenRouter /models, the Anthropic Models API, OpenAI model
// families). A model neither can describe has no levels, so the picker hides
// and nothing is sent.
func (s *Service) ThinkingLevels(ctx context.Context, modelID string) []string {
	if levels, ok := s.cfg().Thinking.LevelsFor(modelID); ok {
		return llm.SortThinking(levels)
	}
	client, _, model, err := s.Client(modelID)
	if err != nil {
		return nil
	}
	th, ok := client.(llm.Thinker)
	if !ok {
		return nil
	}
	levels, err := th.ThinkingLevels(ctx, model)
	if err != nil {
		return nil
	}
	return llm.SortThinking(levels)
}

// ChatThinking is the chat's chosen level fitted to modelID: the nearest level
// the model accepts, or "" (the model default) when it has none or the chat
// never chose one.
func (s *Service) ChatThinking(ctx context.Context, chatID, modelID string) string {
	if chatID == "" {
		return ""
	}
	var c db.Chat
	if err := s.db.Select("thinking").Find(&c, "id = ?", chatID).Error; err != nil || c.Thinking == "" {
		return ""
	}
	return llm.NearestThinking(c.Thinking, s.ThinkingLevels(ctx, modelID))
}

// WithThinking fills each model option's levels. Lookups run in parallel and
// are bounded so a slow provider cannot hold up the composer; providers cache
// their answers, so this is cheap after the first call.
func (s *Service) WithThinking(ctx context.Context, opts []Option) []Option {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range opts {
		wg.Add(1)
		go func(o *Option) {
			defer wg.Done()
			o.Thinking = s.ThinkingLevels(ctx, o.ID)
		}(&opts[i])
	}
	wg.Wait()
	return opts
}

// SetChatThinking persists a chat's thinking level. Any known level is kept,
// even one the current model lacks: each turn fits it to the model, so the
// choice survives a model switch.
func (s *Service) SetChatThinking(ctx context.Context, req *connect.Request[v1.SetChatThinkingRequest]) (*connect.Response[v1.Chat], error) {
	c, err := access.OwnBotRow[db.Chat](ctx, s.db, req.Msg.GetBotId(), req.Msg.GetChatId(), "chat")
	if err != nil {
		return nil, err
	}
	level := strings.ToLower(strings.TrimSpace(req.Msg.GetThinking()))
	if !llm.ValidThinking(level) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown thinking level"))
	}
	c.Thinking = level
	if err := s.db.Model(&db.Chat{}).Where("id = ?", c.ID).Update("thinking", level).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(chats.Proto(c)), nil
}
