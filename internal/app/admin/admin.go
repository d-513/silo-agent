// Package admin is the operator's side of the control plane: the settings form
// and YAML editor (config, providers, model allowlist, connector options) and
// the audit log of what the gate decided.
package admin

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"net/http"
	"strings"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/voice"
	"silo.agent/internal/catalog"
	"silo.agent/internal/config"
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
	// oidcRedirect is the OIDC callback address, as the request's origin sees it.
	oidcRedirect func(*http.Request) string
}

func New(gdb *gorm.DB, store *config.Store, cfg func() config.Config, m *models.Service, v *voice.Service, oidcRedirect func(*http.Request) string) *Service {
	return &Service{db: gdb, store: store, cfg: cfg, models: m, voice: v, oidcRedirect: oidcRedirect}
}

func (s *Service) GetSettings(ctx context.Context, _ *connect.Request[v1.GetSettingsRequest]) (*connect.Response[v1.Settings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	cur, err := s.settings(ctx)
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
			if err := s.checkRole(k, strings.TrimSpace(v)); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
		}
		if err := s.store.Patch(fields); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	cur, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

// modelRoles are the settings that pick a model for a job, and the kinds of
// model each may be given. Only the auto-approval model takes either: a
// decision model answers it a typed verdict, a text model one word.
var modelRoles = map[string][]string{
	"model":          {llm.KindText},
	"model_title":    {llm.KindText},
	"model_subagent": {llm.KindText},
	"model_memory":   {llm.KindText},
	"model_approval": {llm.KindText, llm.KindDecision},
	"model_filter":   {llm.KindText, llm.KindDecision},
	"model_decision": {llm.KindDecision},
	"model_voice":    {llm.KindText},
	// The roles of the other kinds pick from their kind's list.
	"transcribe_model":       {llm.KindTranscription},
	"voice.transcribe_model": {llm.KindTranscription},
	"voice.speech_model":     {llm.KindSpeech},
	"embedding_model":        {llm.KindEmbedding},
}

// kindWords name a kind in a refusal.
var kindWords = map[string]string{
	llm.KindText:          "a text model",
	llm.KindDecision:      "a decision model",
	llm.KindTranscription: "a transcription model",
	llm.KindSpeech:        "a speech model",
	llm.KindEmbedding:     "an embedding model",
}

// checkRole refuses a role given a model that is not allowed, or is of a kind
// the role cannot use. An empty value (follow another role) is always fine.
func (s *Service) checkRole(key, modelID string) error {
	kinds, ok := modelRoles[key]
	if !ok || modelID == "" {
		return nil
	}
	if key == "transcribe_model" && strings.EqualFold(modelID, config.TranscribeOff) {
		return nil
	}
	if _, _, err := llm.Parse(modelID); err != nil {
		return err
	}
	cfg := s.cfg()
	kind := cfg.ModelKind(modelID)
	takes := false
	for _, k := range kinds {
		takes = takes || k == kind
	}
	if !takes {
		// A model on no list is taken for text; one of another kind's
		// provider may still serve the role's kind (a new speech model).
		provider, _, _ := llm.Split(modelID)
		d, _ := llm.Lookup(provider)
		if kind == llm.KindText && len(kinds) == 1 && kinds[0] != llm.KindText && d.Serves(kinds[0]) {
			kind, takes = kinds[0], true
		}
	}
	if !takes {
		if kind == llm.KindDecision && len(kinds) == 1 && kinds[0] == llm.KindText {
			return fmt.Errorf("%q is a decision model: it answers typed questions and writes no text", modelID)
		}
		return fmt.Errorf("%q is %s, not %s", modelID, kindWords[kind], kindWords[kinds[0]])
	}
	allowed := cfg.KindModels(kind)
	if kind == llm.KindText && len(allowed) == 0 {
		// No text allowlist yet: any text model may be the default.
		return nil
	}
	if kind != llm.KindText && kind != llm.KindDecision {
		// The other kinds' roles may name any model of the kind: the list
		// is what the pickers offer, and a role's model counts as listed.
		return nil
	}
	if !llm.Allowed(modelID, allowed) {
		return fmt.Errorf("model %q is not in the allowed list", modelID)
	}
	return nil
}

// SetModels replaces one of the operator's model allowlists, one per kind
// (llm.Kinds).
func (s *Service) SetModels(ctx context.Context, req *connect.Request[v1.SetModelsRequest]) (*connect.Response[v1.Settings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("config store missing"))
	}
	kind := strings.TrimSpace(req.Msg.GetKind())
	if kind == "" {
		kind = llm.KindText
	}
	if _, ok := llm.KindOf(kind); !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown model kind %q", kind))
	}
	allow := make([]string, 0, len(req.Msg.GetModels()))
	for _, m := range req.Msg.GetModels() {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		provider, _, err := llm.Parse(m)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if d, _ := llm.Lookup(provider); !d.Serves(kind) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s has no %s models, so %q cannot be allowed as one", d.Name, kind, m))
		}
		allow = append(allow, m)
	}
	if err := s.store.SetKindModels(kind, allow); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	cur, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

// ListProviderModels asks one provider which models its saved key can call,
// as full provider/model ids ready for an allowlist, each with the kind that
// says which one.
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
		kind := m.Kind
		if kind == "" {
			kind = llm.KindText
		}
		caps := llm.CapsOf(client, m.ID)
		out.Models = append(out.Models, &v1.ProviderModel{
			Id: provider + "/" + m.ID, Name: m.Name, ContextWindow: int32(m.ContextWindow), Kind: kind, Voices: m.Voices,
			Live: caps.Live, Words: caps.Words, EndOfTurn: caps.EndOfTurn,
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
	cur, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

// SetAutoenableConnectors replaces autoenable_connectors, the library
// connectors every new Bot gets. An identifier need not be in the library yet.
func (s *Service) SetAutoenableConnectors(ctx context.Context, req *connect.Request[v1.SetAutoenableConnectorsRequest]) (*connect.Response[v1.Settings], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("config store missing"))
	}
	for _, raw := range req.Msg.GetIdentifiers() {
		if strings.TrimSpace(raw) != "" && catalog.Identifier(raw) == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid connector identifier %q", raw))
		}
	}
	if err := s.store.SetAutoenableConnectors(catalog.Identifiers(req.Msg.GetIdentifiers())); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	cur, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(cur), nil
}

func (s *Service) settings(ctx context.Context) (*v1.Settings, error) {
	out := &v1.Settings{SearchEngines: searchEngineProtos()}
	if s.oidcRedirect != nil {
		out.OidcRedirectUrl = s.oidcRedirect(access.Request(ctx))
	}
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
	out.DecisionModels = models.Protos(s.models.Deciders())
	for _, k := range llm.Kinds {
		out.Allowed = append(out.Allowed, &v1.KindModels{
			Kind: k.ID, Models: models.Protos(s.models.Options(k.ID)), Listed: s.store.StringList(k.List),
		})
	}
	for _, v := range s.store.ConnectorVars() {
		out.ConnectorVars = append(out.ConnectorVars, &v1.ConnectorVar{
			Name:    v.Name,
			Value:   v.Value,
			Source:  sourceProto(s.store.ConnectorVarSource(v.Name)),
			EnvName: config.EnvName("connector_vars." + v.Name),
		})
	}
	cfg := s.cfg()
	out.AutoenableConnectors = &v1.AutoenableConnectors{
		Identifiers: catalog.Identifiers(cfg.AutoenableConnectors),
		Source:      sourceProto(s.store.Source("autoenable_connectors")),
		EnvName:     config.EnvName("autoenable_connectors"),
	}
	out.DefaultModel = cfg.Model
	out.ContentHost = cfg.ContentHost()
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
		for _, kind := range llm.Kinds {
			if d.Serves(kind.ID) {
				p.Kinds = append(p.Kinds, kind.ID)
			}
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
