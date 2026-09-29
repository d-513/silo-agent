package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// thinkingLevels is what a model accepts, least reasoning first: an operator
// per-model override (thinking.levels), then the provider's report
// (llm.Thinker: OpenRouter /models, the Anthropic Models API, OpenAI model
// families). A model neither can describe has no levels, so the picker hides
// and nothing is sent.
func (a *App) thinkingLevels(ctx context.Context, modelID string) []string {
	if levels, ok := a.cfg().Thinking.LevelsFor(modelID); ok {
		return llm.SortThinking(levels)
	}
	client, _, model, err := a.providerClient(modelID)
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

// chatThinking is the chat's chosen level fitted to modelID: the nearest level
// the model accepts, or "" (the model default) when it has none or the chat
// never chose one.
func (a *App) chatThinking(ctx context.Context, chatID, modelID string) string {
	if chatID == "" {
		return ""
	}
	var c db.Chat
	if err := a.DB.Select("thinking").Find(&c, "id = ?", chatID).Error; err != nil || c.Thinking == "" {
		return ""
	}
	return llm.NearestThinking(c.Thinking, a.thinkingLevels(ctx, modelID))
}

// withThinking fills each model option's levels. Lookups run in parallel and
// are bounded so a slow provider cannot hold up the composer; providers cache
// their answers, so this is cheap after the first call.
func (a *App) withThinking(ctx context.Context, opts []modelOption) []modelOption {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range opts {
		wg.Add(1)
		go func(o *modelOption) {
			defer wg.Done()
			o.Thinking = a.thinkingLevels(ctx, o.ID)
		}(&opts[i])
	}
	wg.Wait()
	return opts
}

// SetChatThinking persists a chat's thinking level. Any known level is kept,
// even one the current model lacks: each turn fits it to the model, so the
// choice survives a model switch.
func (a *App) SetChatThinking(ctx context.Context, req *connect.Request[v1.SetChatThinkingRequest]) (*connect.Response[v1.Chat], error) {
	c, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId())
	if err != nil {
		return nil, err
	}
	level := strings.ToLower(strings.TrimSpace(req.Msg.GetThinking()))
	if !llm.ValidThinking(level) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown thinking level"))
	}
	c.Thinking = level
	if err := a.DB.Model(&db.Chat{}).Where("id = ?", c.ID).Update("thinking", level).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(protoChat(c)), nil
}
