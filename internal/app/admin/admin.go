// Package admin is the operator's side of the control plane: the settings form
// and YAML editor (config, providers, model allowlist, connector variables) and
// the audit log of what the gate decided.
package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/voice"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/search"
)

// Service serves the operator's settings and audit RPCs.
type Service struct {
	db     *gorm.DB
	store  *config.Store
	cfg    func() config.Config
	models *models.Service
	voice  *voice.Service
}

func New(gdb *gorm.DB, store *config.Store, cfg func() config.Config, m *models.Service, v *voice.Service) *Service {
	return &Service{db: gdb, store: store, cfg: cfg, models: m, voice: v}
}

func (s *Service) GetSettings(ctx context.Context, _ *connect.Request[v1.GetSettingsRequest]) (*connect.Response[v1.Settings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	cur, err := s.settings()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

func (s *Service) PutSettings(ctx context.Context, req *connect.Request[v1.PutSettingsRequest]) (*connect.Response[v1.Settings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("config store missing"))
	}
	yamlText := req.Msg.GetYaml()
	fields := req.Msg.GetFields()
	if yamlText != "" && len(fields) > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("set fields or yaml, not both"))
	}
	if yamlText != "" {
		if err := s.store.WriteYAML([]byte(yamlText)); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	} else if len(fields) > 0 {
		allowed := s.store.AllowedModels()
		for k, v := range fields {
			if !config.KnownKey(k) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown setting %q", k))
			}
			if k == "search.engine" && v != "" && !search.Known(v) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown search engine %q", v))
			}
			if k == "embedding_model" && v != "" {
				if err := s.models.CanEmbed(v); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}
			}
			if k == "transcribe_model" && v != "" && !strings.EqualFold(strings.TrimSpace(v), config.TranscribeOff) {
				if err := s.voice.CanTranscribe(v); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}
			}
			if (k == "model" || k == "model_title" || k == "model_approval" || k == "model_subagent" || k == "model_memory") && v != "" {
				if _, _, err := llm.Parse(v); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}
				if len(allowed) > 0 && !llm.Allowed(v, allowed) {
					return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("model %q is not in the allowed list", v))
				}
			}
		}
		if err := s.store.Patch(fields); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	cur, err := s.settings()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

// SetModels replaces the operator's model allowlist.
func (s *Service) SetModels(ctx context.Context, req *connect.Request[v1.SetModelsRequest]) (*connect.Response[v1.Settings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("config store missing"))
	}
	allow := make([]string, 0, len(req.Msg.GetModels()))
	for _, m := range req.Msg.GetModels() {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, _, err := llm.Parse(m); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		allow = append(allow, m)
	}
	if err := s.store.SetModels(allow); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	cur, err := s.settings()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

// ListProviderModels asks one provider which models its saved key can call,
// as full provider/model ids ready for the allowlist.
func (s *Service) ListProviderModels(ctx context.Context, req *connect.Request[v1.ListProviderModelsRequest]) (*connect.Response[v1.ListProviderModelsResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(req.Msg.GetProvider())
	d, ok := llm.Lookup(provider)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown model provider %q", provider))
	}
	name := d.Name
	if name == "" {
		name = provider
	}
	client, err := s.models.Provider(provider)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	lister, ok := client.(llm.ModelLister)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s cannot list its models; add them by id", name))
	}
	list, err := lister.ListModels(ctx)
	if err != nil {
		// Never Unavailable: the control plane is fine, the provider is not.
		// An upstream error body can echo the key back, so it is masked.
		msg := err.Error()
		if key := s.cfg().ProviderSettings(provider).Get("api_key"); key != "" {
			msg = strings.ReplaceAll(msg, key, "••••")
		}
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("%s did not list its models: %s", name, msg))
	}
	out := &v1.ListProviderModelsResponse{Models: make([]*v1.ProviderModel, 0, len(list))}
	for _, m := range list {
		out.Models = append(out.Models, &v1.ProviderModel{
			Id: provider + "/" + m.ID, Name: m.Name, ContextWindow: int32(m.ContextWindow),
		})
	}
	return connect.NewResponse(out), nil
}

// SetConnectorVars replaces the operator's connector variables.
func (s *Service) SetConnectorVars(ctx context.Context, req *connect.Request[v1.SetConnectorVarsRequest]) (*connect.Response[v1.Settings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("config store missing"))
	}
	vars := make([]config.NamedVar, 0, len(req.Msg.GetConnectorVars()))
	for _, v := range req.Msg.GetConnectorVars() {
		name := strings.TrimSpace(v.GetName())
		if name == "" {
			continue
		}
		if !config.ValidVarName(name) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid variable name %q", name))
		}
		vars = append(vars, config.NamedVar{Name: name, Value: v.GetValue()})
	}
	if err := s.store.SetConnectorVars(vars); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	cur, err := s.settings()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

func (s *Service) settings() (*v1.Settings, error) {
	out := &v1.Settings{SearchEngines: searchEngineProtos()}
	if s.store == nil {
		return out, nil
	}
	raw, err := s.store.YAML()
	if err != nil {
		return nil, err
	}
	for _, f := range s.store.Fields() {
		out.Fields = append(out.Fields, &v1.ConfigField{
			Key:             f.Key,
			Value:           f.Value,
			Source:          sourceProto(f.Source),
			EnvName:         f.EnvName,
			Secret:          f.Secret,
			RestartRequired: f.Restart,
			Type:            f.Type,
		})
	}
	out.Providers = providerProtos()
	out.Models = models.Protos(s.models.Allowed())
	for _, v := range s.store.ConnectorVars() {
		out.ConnectorVars = append(out.ConnectorVars, &v1.ConnectorVar{
			Name:    v.Name,
			Value:   v.Value,
			Source:  sourceProto(s.store.ConnectorVarSource(v.Name)),
			EnvName: config.EnvName("connector_vars." + v.Name),
		})
	}
	cfg := s.cfg()
	out.DefaultModel = cfg.Model
	out.TunnelsHost = cfg.TunnelHost()
	out.TitleModel = cfg.ModelTitle
	out.Yaml = string(raw)
	out.YamlPath = s.store.Path()
	return out, nil
}

func providerProtos() []*v1.Provider {
	out := make([]*v1.Provider, 0, len(llm.Descriptors()))
	for _, d := range llm.Descriptors() {
		p := &v1.Provider{
			Id: d.ID, Name: d.Name, Description: d.Description,
			SupportsCache: d.SupportsCache, CacheTtls: d.CacheTTLs,
		}
		for _, f := range d.Settings {
			p.Fields = append(p.Fields, &v1.ProviderField{
				Key: f.Key, Label: f.Label, Type: f.Type, Description: f.Description, Secret: f.Secret,
			})
		}
		out = append(out, p)
	}
	return out
}

func sourceProto(s config.Source) v1.ConfigSource {
	switch s {
	case config.SourceYAML:
		return v1.ConfigSource_CONFIG_SOURCE_YAML
	case config.SourceEnv:
		return v1.ConfigSource_CONFIG_SOURCE_ENV
	default:
		return v1.ConfigSource_CONFIG_SOURCE_DEFAULT
	}
}

func searchEngineProtos() []*v1.SearchEngine {
	out := make([]*v1.SearchEngine, 0, len(search.Descriptors()))
	for _, d := range search.Descriptors() {
		e := &v1.SearchEngine{Id: d.ID, Name: d.Name, Description: d.Description}
		for _, f := range d.Settings {
			e.Fields = append(e.Fields, &v1.SearchEngineField{
				Key: f.Key, Label: f.Label, Type: f.Type, Description: f.Description, Secret: f.Secret,
			})
		}
		out = append(out, e)
	}
	return out
}

func (s *Service) ListAudit(ctx context.Context, _ *connect.Request[v1.ListAuditRequest]) (*connect.Response[v1.ListAuditResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	var rows []db.Audit
	s.db.Order("created_at desc").Limit(100).Find(&rows)
	out := &v1.ListAuditResponse{}
	for _, r := range rows {
		out.Rows = append(out.Rows, &v1.AuditRow{
			Id: r.ID, At: r.CreatedAt.Format(time.RFC3339),
			BotId: r.BotID, BotName: r.BotName, Crest: int32(r.Crest),
			Actor: r.Actor, Action: r.Action, Decision: r.Decision,
		})
	}
	return connect.NewResponse(out), nil
}
