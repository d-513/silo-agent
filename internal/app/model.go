package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// resolveModel picks the model for a conversation: the chat's own model when it
// is still allowed, otherwise the operator default.
func (a *App) resolveModel(chatID string) string {
	cfg := a.cfg()
	if chatID != "" {
		var c db.Chat
		if a.DB.First(&c, "id = ?", chatID).Error == nil {
			if m := strings.TrimSpace(c.Model); m != "" && llm.Allowed(m, cfg.Models) {
				return m
			}
		}
	}
	if m := strings.TrimSpace(cfg.Model); m != "" {
		return m
	}
	return config.DefaultModel
}

// titleModel picks the model used to name a chat. The dedicated setting wins
// when it is allowed, otherwise the conversation's model is used.
func (a *App) titleModel(chatID string) string {
	cfg := a.cfg()
	if m := strings.TrimSpace(cfg.ModelTitle); m != "" && llm.Allowed(m, cfg.Models) {
		return m
	}
	return a.resolveModel(chatID)
}

// modelClient builds a provider client for a provider/model id.
func (a *App) modelClient(modelID string) (llm.Client, string, string, error) {
	provider, model, err := llm.Parse(modelID)
	if err != nil {
		return nil, "", "", err
	}
	settings := a.cfg().ProviderSettings(provider)
	if settings.Get("api_key") == "" {
		name := provider
		if d, ok := llm.Lookup(provider); ok {
			name = d.Name
		}
		return nil, provider, model, fmt.Errorf(
			"%s API key missing in operator config (silo.yaml / %s)",
			name, config.EnvName("providers."+provider+".api_key"))
	}
	client, err := llm.New(provider, settings)
	if err != nil {
		return nil, provider, model, err
	}
	return client, provider, model, nil
}

// cachePolicy turns a provider's settings into a request cache policy. Caching
// is off unless the operator enabled it.
func cachePolicy(s llm.Settings, botID string) llm.CachePolicy {
	enabled, ttl := llm.CacheSettings(s)
	if !enabled {
		return llm.CachePolicy{}
	}
	return llm.CachePolicy{Enabled: true, TTL: ttl, Key: "silo-bot-" + botID, Messages: true}
}

// emitUsage reports token accounting for a turn so the client can show a cache
// chip. It persists like any other run event.
func (a *App) emitUsage(botID, chatID, runID string, u llm.Usage) {
	body, _ := json.Marshal(map[string]int{
		"input":       u.InputTokens,
		"output":      u.OutputTokens,
		"cache_read":  u.CacheReadTokens,
		"cache_write": u.CacheWriteTokens,
	})
	a.emit(botID, chatID, runID, "usage", string(body), "")
}

// modelOptions is the model allowlist paired with provider metadata for the UI.
type modelOption struct {
	ID       string
	Provider string
	Label    string
}

func (a *App) allowedModels() []modelOption {
	cfg := a.cfg()
	out := make([]modelOption, 0, len(cfg.Models))
	for _, id := range cfg.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		provider, model, err := llm.Parse(id)
		if err != nil {
			continue
		}
		label := model
		if d, ok := llm.Lookup(provider); ok {
			label = d.Name + " · " + model
		}
		out = append(out, modelOption{ID: id, Provider: provider, Label: label})
	}
	return out
}

// listModelsTool is the bot-facing list_models tool.
func (a *App) listModelsTool(chatID string) (string, error) {
	cfg := a.cfg()
	models := make([]string, 0, len(cfg.Models))
	for _, o := range a.allowedModels() {
		models = append(models, o.ID)
	}
	body, err := json.Marshal(map[string]any{
		"current": a.resolveModel(chatID),
		"default": cfg.Model,
		"models":  models,
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// switchModelTool is the bot-facing switch_model tool. It persists the choice
// on the chat; the running loop re-resolves the model on its next turn.
func (a *App) switchModelTool(chatID, modelID string) (string, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return "", fmt.Errorf("model required")
	}
	if !llm.Allowed(modelID, a.cfg().Models) {
		return "", fmt.Errorf("model %q is not in the allowed list; call list_models", modelID)
	}
	if _, _, err := llm.Parse(modelID); err != nil {
		return "", err
	}
	if err := a.DB.Model(&db.Chat{}).Where("id = ?", chatID).Update("model", modelID).Error; err != nil {
		return "", err
	}
	return "Switched this conversation to " + modelID + ". It applies from the next model call.", nil
}
