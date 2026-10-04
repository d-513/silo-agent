// Package models is how the control plane reaches language models: resolving
// which model a conversation uses, building provider clients (bare, observed
// for the debug log, or probed for capabilities), the embedding and thinking
// helpers, and the debug log of every model call.
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// Service reads the operator's config and the Bot and chat rows to answer
// "which model, and how do I call it".
type Service struct {
	db  *gorm.DB
	cfg func() config.Config
}

func New(gdb *gorm.DB, cfg func() config.Config) *Service { return &Service{db: gdb, cfg: cfg} }

// Resolve picks the model for a conversation: the chat's own model when
// it is still allowed, then the Bot's default, otherwise the operator default.
func (s *Service) Resolve(botID, chatID string) string {
	if chatID != "" {
		var c db.Chat
		if s.db.First(&c, "id = ?", chatID).Error == nil {
			if m := strings.TrimSpace(c.Model); m != "" && llm.Allowed(m, s.cfg().Models) {
				return m
			}
		}
	}
	return s.BotDefault(botID)
}

// Title picks the model used to name a chat. The dedicated setting wins
// when it is allowed, otherwise the conversation's model is used.
func (s *Service) Title(botID, chatID string) string {
	cfg := s.cfg()
	if m := strings.TrimSpace(cfg.ModelTitle); m != "" && llm.Allowed(m, cfg.Models) {
		return m
	}
	return s.Resolve(botID, chatID)
}

// Observed builds a provider client for a provider/model id. Every client
// is wrapped by the engine's observer so the debug log sees every model call
// (chat, title, auto-approval) with the label naming its feature.
func (s *Service) Observed(modelID, botID, label string) (llm.Client, string, string, error) {
	client, provider, model, err := s.Client(modelID)
	if err != nil {
		return nil, provider, model, err
	}
	return llm.Observe(client, provider, func(rec llm.Record) { s.Record(botID, label, rec) }), provider, model, nil
}

// Client builds the bare provider client for a provider/model id, so
// optional interfaces such as llm.Embedder stay reachable.
func (s *Service) Client(modelID string) (llm.Client, string, string, error) {
	provider, model, err := llm.Parse(modelID)
	if err != nil {
		return nil, "", "", err
	}
	settings := s.cfg().ProviderSettings(provider)
	d, _ := llm.Lookup(provider)
	if settings.Get("api_key") == "" && !d.KeyOptional {
		name := provider
		if d.Name != "" {
			name = d.Name
		}
		return nil, provider, model, fmt.Errorf(
			"%s API key missing in operator config (silo.yaml / %s)",
			name, config.EnvName("providers."+provider+".api_key"))
	}
	if d.BaseURLRequired && settings.Get("base_url") == "" {
		return nil, provider, model, fmt.Errorf(
			"%s base URL missing in operator config (silo.yaml / %s)",
			d.Name, config.EnvName("providers."+provider+".base_url"))
	}
	client, err := llm.New(provider, settings)
	if err != nil {
		return nil, provider, model, err
	}
	return client, provider, model, nil
}

// Probe builds a throwaway client for provider so a caller can check which
// optional interfaces (llm.Embedder, llm.Transcriber) it implements. Missing
// credentials are filled in and nothing is called.
func (s *Service) Probe(modelID string) (llm.Client, string, error) {
	provider, _, err := llm.Parse(modelID)
	if err != nil {
		return nil, "", err
	}
	settings := llm.Settings{}
	maps.Copy(settings, s.cfg().ProviderSettings(provider))
	if settings.Get("api_key") == "" {
		settings["api_key"] = "probe"
	}
	if settings.Get("base_url") == "" {
		if d, _ := llm.Lookup(provider); d.BaseURLRequired {
			settings["base_url"] = "http://probe.invalid"
		}
	}
	client, err := llm.New(provider, settings)
	return client, provider, err
}

// CachePolicy turns a provider's settings into a request cache policy. Caching
// is off unless the operator enabled it.
func CachePolicy(s llm.Settings, botID string) llm.CachePolicy {
	enabled, ttl := llm.CacheSettings(s)
	if !enabled {
		return llm.CachePolicy{}
	}
	return llm.CachePolicy{Enabled: true, TTL: ttl, Key: "silo-bot-" + botID, Messages: true}
}

// Option is the model allowlist paired with provider metadata for the UI.
type Option struct {
	ID       string
	Provider string
	Label    string
	// Thinking is filled only where the UI needs levels (ListModels).
	Thinking []string
}

func (s *Service) Allowed() []Option {
	cfg := s.cfg()
	out := make([]Option, 0, len(cfg.Models))
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
		out = append(out, Option{ID: id, Provider: provider, Label: label})
	}
	return out
}

// Protos is the wire form of a list of options.
func Protos(opts []Option) []*v1.ModelOption {
	out := make([]*v1.ModelOption, 0, len(opts))
	for _, o := range opts {
		out = append(out, &v1.ModelOption{Id: o.ID, Provider: o.Provider, Label: o.Label, ThinkingLevels: o.Thinking})
	}
	return out
}

// ListTool is the bot-facing list_models tool.
func (s *Service) ListTool(botID, chatID string) (string, error) {
	cfg := s.cfg()
	models := make([]string, 0, len(cfg.Models))
	for _, o := range s.Allowed() {
		models = append(models, o.ID)
	}
	body, err := json.Marshal(map[string]any{
		"current": s.Resolve(botID, chatID),
		"default": s.BotDefault(botID),
		"models":  models,
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// BotDefault is the provider/model a Bot uses when a chat has no override:
// the Bot's own setting when still allowed, otherwise the operator default.
func (s *Service) BotDefault(botID string) string {
	cfg := s.cfg()
	if botID != "" {
		var b db.Bot
		if s.db.First(&b, "id = ?", botID).Error == nil {
			if m := strings.TrimSpace(b.Model); m != "" && llm.Allowed(m, cfg.Models) {
				return m
			}
		}
	}
	if m := strings.TrimSpace(cfg.Model); m != "" {
		return m
	}
	return config.DefaultModel
}

// SwitchTool is the bot-facing switch_model tool. It persists the choice
// on the chat; the running loop re-resolves the model on its next turn.
func (s *Service) SwitchTool(chatID, modelID string) (string, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return "", fmt.Errorf("model required")
	}
	if !llm.Allowed(modelID, s.cfg().Models) {
		return "", fmt.Errorf("model %q is not in the allowed list; call list_models", modelID)
	}
	if _, _, err := llm.Parse(modelID); err != nil {
		return "", err
	}
	if err := s.db.Model(&db.Chat{}).Where("id = ?", chatID).Update("model", modelID).Error; err != nil {
		return "", err
	}
	return "Switched this conversation to " + modelID + ". It applies from the next model call.", nil
}

// Can reports whether modelID names a provider whose client implements the
// capability interface T; verb names what it cannot do in the error.
func Can[T any](s *Service, modelID, verb string) error {
	client, provider, err := s.Probe(modelID)
	if err != nil {
		return err
	}
	if _, ok := client.(T); !ok {
		return fmt.Errorf("%s cannot %s; pick an OpenAI-compatible provider", provider, verb)
	}
	return nil
}

// EmbedModelID is the operator's embedding model, or the default.
func (s *Service) EmbedModelID() string {
	if m := strings.TrimSpace(s.cfg().EmbedModel); m != "" {
		return m
	}
	return config.DefaultEmbeddingModel
}

// Embed turns texts into vectors with the operator's embedding model.
func (s *Service) Embed(ctx context.Context, texts []string) ([]pgvector.Vector, string, error) {
	modelID := s.EmbedModelID()
	client, provider, model, err := s.Client(modelID)
	if err != nil {
		return nil, modelID, err
	}
	e, ok := client.(llm.Embedder)
	if !ok {
		return nil, modelID, fmt.Errorf("%s cannot embed; set embedding_model to an OpenAI-compatible model", provider)
	}
	raw, err := e.Embed(ctx, model, texts)
	if err != nil {
		return nil, modelID, fmt.Errorf("embed: %w", err)
	}
	out := make([]pgvector.Vector, len(raw))
	for i, v := range raw {
		out[i] = pgvector.NewVector(v)
	}
	return out, modelID, nil
}

// CanEmbed reports whether modelID names a provider that implements
// llm.Embedder, without needing its API key.
func (s *Service) CanEmbed(modelID string) error {
	return Can[llm.Embedder](s, modelID, "embed")
}
