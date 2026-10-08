// Package connector is how a Bot reaches outside tools: the library of
// connector presets, each Bot's attached copies, their MCP sessions over HTTP or
// a STDIO sidecar, OAuth sign-ins, and connectors written in Go. It owns the
// tools the Bot finds under `import tools`; calling them (and gating each call)
// stays with the app's tool gateway.
package connector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/workspace"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/catalog"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/masker"
	"silo.agent/internal/mcpbridge"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

const (
	connTypeMCP    = "mcp"
	transportHTTP  = "http"
	TransportSTDIO = "stdio"
	// TransportBuiltin connectors run Go code on the CP (internal/builtin).
	TransportBuiltin = "builtin"
	authNone         = "none"
	AuthOAuth        = "oauth"
	statusNone       = "none"
	StatusInit       = "initializing"
	StatusNeedsAuth  = "needs_auth"
	StatusOK         = "authorized"
	StatusErr        = "error"
	imageMax         = 512 << 10

	// connectorInitWait bounds an async connect+tool-discovery job.
	connectorInitWait = 10 * time.Minute
)

// Host is what the connectors need from the App around them.
type Host interface {
	// PruneRules drops the rules of a connector's actions that no longer
	// exist, keeping those named (see rules).
	PruneRules(botID, slug string, keepActions []string)
}

// Service owns the connector library, each Bot's attached copies, their MCP
// sessions (HTTP and STDIO), OAuth sign-ins, built-in connectors and the STDIO
// sidecars with their reverse tunnels.
type Service struct {
	db     *gorm.DB
	docker dockerx.Host
	hub    *hub.Hub
	store  *config.Store
	cfg    func() config.Config
	ws     *workspace.Service
	mask   func(botID string) *masker.Masker
	host   Host

	// mu guards oauth (pending sign-ins by state) and mcp (live sessions).
	mu    sync.Mutex
	oauth map[string]*oauthWait
	mcp   map[string]*mcpx.Session
	// locks serializes session dials and sidecar starts per connector.
	locks sync.Map
	// bridges are the live reverse tunnels from STDIO sidecars.
	bridgesMu sync.Mutex
	bridges   map[string]*bridgeTunnel
}

func New(gdb *gorm.DB, d dockerx.Host, h *hub.Hub, store *config.Store, cfg func() config.Config, ws *workspace.Service, mask func(botID string) *masker.Masker, host Host) *Service {
	return &Service{
		db: gdb, docker: d, hub: h, store: store, cfg: cfg, ws: ws, mask: mask, host: host,
		oauth: map[string]*oauthWait{}, mcp: map[string]*mcpx.Session{}, bridges: map[string]*bridgeTunnel{},
	}
}

func (s *Service) ListConnectors(ctx context.Context, _ *connect.Request[v1.ListConnectorsRequest]) (*connect.Response[v1.ListConnectorsResponse], error) {
	admin := access.User(ctx) != nil && access.User(ctx).Admin
	var rows []db.Connector
	s.db.Where("kind = ?", catalog.KindLibrary).Order("name").Find(&rows)
	out := &v1.ListConnectorsResponse{}
	for i := range rows {
		out.Connectors = append(out.Connectors, s.protoLibrary(&rows[i], admin))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) CreateConnector(ctx context.Context, req *connect.Request[v1.CreateConnectorRequest]) (*connect.Response[v1.Connector], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	m := req.Msg
	row, err := parseConnector(connectorIn{
		Name: m.GetName(), Desc: m.GetDescription(), Category: m.GetCategory(), Transport: m.GetTransport(),
		HTTPURL: m.GetHttpUrl(), Auth: m.GetAuth(), Mode: m.GetDefaultMode(),
		Image: m.GetImage(), ImageType: m.GetImageType(), Headers: m.GetHeaders(),
		StdioCommand: m.GetStdioCommand(), StdioArgs: m.GetStdioArgs(), StdioImage: m.GetStdioImage(),
		Env: m.GetEnv(), Prompt: m.GetPrompt(), AllowImage: true, RequireComplete: true,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row.AutoAttach = m.GetAutoAttach()
	if row.Identifier, err = s.libraryIdentifier(m.GetIdentifier(), row.Name, row.ID); err != nil {
		return nil, err
	}
	applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), true)
	if row.Transport == TransportSTDIO {
		row.Auth = authNone
		row.OAuthClientID = ""
		row.OAuthClientSecret = ""
	}
	row.Kind = catalog.KindLibrary
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(s.protoLibrary(&row, true)), nil
}

func (s *Service) UpdateConnector(ctx context.Context, req *connect.Request[v1.UpdateConnectorRequest]) (*connect.Response[v1.Connector], error) {
	var row db.Connector
	if err := s.db.First(&row, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := s.editConnector(ctx, &row); err != nil {
		return nil, err
	}
	m := req.Msg
	if n := strings.TrimSpace(m.GetName()); n != "" {
		if row.Kind == catalog.KindCustom && s.slugTaken(row.BotID, n, row.ID) {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("a connector on this Bot already uses that name"))
		}
		if row.Kind == catalog.KindCustom && security.Reserved(toolsgen.Slug(n)) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("that name is reserved"))
		}
		row.Name = n
	}
	row.Description = strings.TrimSpace(m.GetDescription())
	row.Prompt = strings.TrimSpace(m.GetPrompt())
	if m.GetCategory() != "" {
		row.Category = strings.TrimSpace(m.GetCategory())
	}
	if row.Kind == catalog.KindLibrary {
		row.AutoAttach = m.GetAutoAttach()
		if strings.TrimSpace(m.GetIdentifier()) != "" {
			ident, err := s.libraryIdentifier(m.GetIdentifier(), row.Name, row.ID)
			if err != nil {
				return nil, err
			}
			row.Identifier = ident
		}
	}
	if row.Transport == TransportBuiltin {
		// A library preset holds no account: config lives on each Bot copy.
		if row.Kind == catalog.KindCustom {
			if err := applyBuiltinConfig(&row, m.GetConfig()); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
		}
		if m.GetDefaultMode() != "" {
			row.DefaultMode = security.Rule(m.GetDefaultMode())
		}
		if err := setConnectorImage(&row, m); err != nil {
			return nil, err
		}
		s.db.Save(&row)
		s.restartConnector(&row)
		return connect.NewResponse(s.protoLibrary(&row, true)), nil
	}
	if m.GetTransport() != "" {
		tr := strings.ToLower(m.GetTransport())
		if err := checkTransport(tr); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		row.Transport = tr
	}
	if row.Transport == TransportSTDIO {
		row.Auth = authNone
		row.HTTPURL = ""
		row.HeadersJSON = "{}"
		row.OAuthClientID = ""
		if m.GetStdioCommand() != "" {
			if err := mcpbridge.ValidCommand(m.GetStdioCommand()); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			row.StdioCommand = strings.TrimSpace(m.GetStdioCommand())
		}
		if len(m.GetStdioArgs()) > 0 || m.StdioArgs != nil {
			for _, a := range m.GetStdioArgs() {
				if err := mcpbridge.ValidArg(a); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}
			}
			row.StdioArgsJSON = mustJSON(m.GetStdioArgs())
		}
		if access.User(ctx) != nil && access.User(ctx).Admin {
			if err := mcpbridge.ValidImage(m.GetStdioImage()); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			if m.GetStdioImage() != "" || row.Kind == catalog.KindLibrary {
				row.StdioImage = strings.TrimSpace(m.GetStdioImage())
			}
		}
		if m.GetEnv() != nil {
			env, err := mergeEnv(row.EnvJSON, m.GetEnv(), false)
			if err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			row.EnvJSON = env
		}
	} else {
		if m.GetHttpUrl() != "" {
			row.HTTPURL = strings.TrimSpace(m.GetHttpUrl())
		}
		if m.GetAuth() != "" {
			au := strings.ToLower(m.GetAuth())
			if au != authNone && au != AuthOAuth {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("auth must be none or oauth"))
			}
			row.Auth = au
		}
		if m.GetHeaders() != nil {
			hdr, err := mergeHeaders(row.HeadersJSON, m.GetHeaders(), false)
			if err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			row.HeadersJSON = hdr
		}
	}
	if m.GetDefaultMode() != "" {
		row.DefaultMode = security.Rule(m.GetDefaultMode())
	}
	if row.Transport != TransportSTDIO {
		applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), false)
	} else {
		row.OAuthClientSecret = ""
	}
	if err := setConnectorImage(&row, m); err != nil {
		return nil, err
	}
	s.db.Save(&row)
	s.restartConnector(&row)
	return connect.NewResponse(s.protoLibrary(&row, true)), nil
}

func (s *Service) DeleteConnector(ctx context.Context, req *connect.Request[v1.DeleteConnectorRequest]) (*connect.Response[v1.DeleteConnectorResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	var row db.Connector
	if err := s.db.First(&row, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if row.Kind == catalog.KindCustom {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("remove a custom connector from the Bot"))
	}
	s.db.Delete(&row)
	return connect.NewResponse(&v1.DeleteConnectorResponse{}), nil
}

func (s *Service) ListBotConnectors(ctx context.Context, req *connect.Request[v1.ListBotConnectorsRequest]) (*connect.Response[v1.ListBotConnectorsResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.BotConnector
	s.db.Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListBotConnectorsResponse{}
	for i := range rows {
		var c db.Connector
		if s.db.First(&c, "id = ?", rows[i].ConnectorID).Error != nil {
			continue
		}
		out.Connectors = append(out.Connectors, protoBotConnector(&rows[i], &c, true))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) AttachConnector(ctx context.Context, req *connect.Request[v1.AttachConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	botID := req.Msg.GetBotId()
	if _, err := access.OwnBot(ctx, s.db, botID); err != nil {
		return nil, err
	}
	var lib db.Connector
	if err := s.db.First(&lib, "id = ? AND kind = ?", req.Msg.GetConnectorId(), catalog.KindLibrary).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	out, err := s.putBotConnector(ctx, botID, cloneLibrary(&lib, botID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (s *Service) CreateBotConnector(ctx context.Context, req *connect.Request[v1.CreateBotConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	m := req.Msg
	if _, err := access.OwnBot(ctx, s.db, m.GetBotId()); err != nil {
		return nil, err
	}
	admin := access.User(ctx) != nil && access.User(ctx).Admin
	from := strings.TrimSpace(m.GetSourceId())
	if lib := s.libraryBuiltin(from); lib != nil {
		out, err := s.attachBuiltin(ctx, m.GetBotId(), lib, m)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(out), nil
	}
	row, err := parseConnector(connectorIn{
		Name: m.GetName(), Desc: m.GetDescription(), Category: m.GetCategory(), Transport: m.GetTransport(),
		HTTPURL: m.GetHttpUrl(), Auth: m.GetAuth(), Mode: m.GetDefaultMode(),
		Image: m.GetImage(), ImageType: m.GetImageType(), Headers: m.GetHeaders(),
		StdioCommand: m.GetStdioCommand(), StdioArgs: m.GetStdioArgs(), StdioImage: m.GetStdioImage(),
		Env: m.GetEnv(), Prompt: m.GetPrompt(), AllowImage: admin, RequireComplete: from == "",
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if row.Transport != TransportSTDIO {
		applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), true)
	}
	if err := s.overlayLibrary(from, &row, m.GetHeaders(), m.GetEnv()); err != nil {
		return nil, err
	}
	out, err := s.putBotConnector(ctx, m.GetBotId(), row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (s *Service) overlayLibrary(src string, row *db.Connector, headers []*v1.HeaderInput, env []*v1.EnvInput) error {
	if src == "" {
		return nil
	}
	var lib db.Connector
	if err := s.db.First(&lib, "id = ? AND kind = ?", src, catalog.KindLibrary).Error; err != nil {
		return connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	row.SourceID = lib.ID
	if row.Category == "" {
		row.Category = lib.Category
	}
	if len(row.Image) == 0 {
		row.Image = append([]byte(nil), lib.Image...)
		row.ImageType = lib.ImageType
	}
	if row.OAuthClientID == "" {
		row.OAuthClientID = lib.OAuthClientID
	}
	if row.OAuthClientSecret == "" {
		row.OAuthClientSecret = lib.OAuthClientSecret
	}
	if row.Prompt == "" {
		row.Prompt = lib.Prompt
	}
	if row.Transport == "" {
		row.Transport = lib.Transport
	}
	if row.StdioCommand == "" {
		row.StdioCommand = lib.StdioCommand
		row.StdioArgsJSON = lib.StdioArgsJSON
	}
	if row.StdioImage == "" {
		row.StdioImage = lib.StdioImage
	}
	if len(headers) == 0 {
		row.HeadersJSON = lib.HeadersJSON
	} else {
		hdr, err := mergeHeaders(lib.HeadersJSON, headers, false)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		row.HeadersJSON = hdr
	}
	if len(env) == 0 {
		row.EnvJSON = lib.EnvJSON
	} else {
		merged, err := mergeEnv(lib.EnvJSON, env, false)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		row.EnvJSON = merged
	}
	if row.Transport == TransportSTDIO && row.StdioCommand == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("stdio_command required"))
	}
	return nil
}

func (s *Service) DetachConnector(ctx context.Context, req *connect.Request[v1.DetachConnectorRequest]) (*connect.Response[v1.DetachConnectorResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if err := s.db.First(&row, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	s.DropMCP(row.ID)
	s.DropStdio(&row)
	var c db.Connector
	if s.db.First(&c, "id = ?", row.ConnectorID).Error == nil {
		s.host.PruneRules(row.BotID, toolsgen.Slug(c.Name), nil)
		if c.Kind == catalog.KindCustom {
			s.db.Delete(&c)
		}
	}
	s.db.Delete(&row)
	go s.PushTools(row.BotID)
	return connect.NewResponse(&v1.DetachConnectorResponse{}), nil
}

func (s *Service) RefreshBotConnector(ctx context.Context, req *connect.Request[v1.RefreshBotConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if err := s.db.First(&row, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	var c db.Connector
	if s.db.First(&c, "id = ?", row.ConnectorID).Error != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	if c.Auth == AuthOAuth && row.AuthStatus == StatusNeedsAuth {
		return connect.NewResponse(&v1.BotConnector{
			Id: row.ID, BotId: row.BotID, AuthStatus: row.AuthStatus, LastError: row.LastError,
			StatusDetail: row.StatusDetail,
			Connector:    protoConnector(&c, false),
		}), nil
	}
	// Dropping the MCP session also closes the sidecar tunnel; its bridge
	// reconnects and spawns a fresh child, reusing the container's npm cache.
	s.DropMCP(row.ID)
	s.applyInit(&row, c.Transport)
	s.startRefresh(row.ID)
	return connect.NewResponse(&v1.BotConnector{
		Id: row.ID, BotId: row.BotID, AuthStatus: row.AuthStatus, LastError: row.LastError,
		StatusDetail: row.StatusDetail,
		Connector:    protoConnector(&c, false),
	}), nil
}

func (s *Service) ServeImage(w http.ResponseWriter, r *http.Request) {
	if _, err := siloauth.UserFromRequest(s.db, r); err != nil {
		access.HTTPSessionError(w, err)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/connectors/"), "/image")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	var c db.Connector
	if s.db.First(&c, "id = ?", id).Error != nil || len(c.Image) == 0 {
		http.NotFound(w, r)
		return
	}
	ct := c.ImageType
	if ct == "" {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(c.Image)
}

func (s *Service) FindBotConnector(botID, slug string) (*db.BotConnector, *db.Connector, error) {
	var rows []db.BotConnector
	s.db.Where("bot_id = ?", botID).Find(&rows)
	for i := range rows {
		var c db.Connector
		if s.db.First(&c, "id = ?", rows[i].ConnectorID).Error != nil {
			continue
		}
		if toolsgen.Slug(c.Name) == slug {
			return &rows[i], &c, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown connector %s", slug)
}

// AttachDefaults copies onto a newly created Bot every library preset a Bot
// starts with. It runs exactly once, from CreateBot, so a preset the human
// later removes from the Bot is never silently re-added.
func (s *Service) AttachDefaults(ctx context.Context, botID string) {
	if s.db == nil {
		return
	}
	libs := s.defaults()
	for i := range libs {
		if _, err := s.putBotConnector(ctx, botID, cloneLibrary(&libs[i], botID)); err != nil {
			log.Printf("default connector %s bot=%s: %v", libs[i].Name, botID, err)
		}
	}
}

// defaults are the library presets a new Bot starts with, by name. A preset
// gets in by its own auto_attach or by the operator naming its identifier in
// autoenable_connectors; one that is both is still a single copy.
func (s *Service) defaults() []db.Connector {
	named := s.autoenabled()
	var libs []db.Connector
	s.db.Where("kind = ? AND (auto_attach = ? OR identifier IN ?)", catalog.KindLibrary, true, named).Order("name").Find(&libs)
	for _, ident := range named {
		if !slices.ContainsFunc(libs, func(c db.Connector) bool { return c.Identifier == ident }) {
			log.Printf("autoenable_connectors: no library connector %q", ident)
		}
	}
	return libs
}

// autoenabled is the operator's autoenable_connectors as identifiers.
func (s *Service) autoenabled() []string {
	if s.cfg == nil {
		return nil
	}
	return catalog.Identifiers(s.cfg().AutoenableConnectors)
}

// protoLibrary is protoConnector for a row that may be a library preset: it
// marks the ones autoenable_connectors names.
func (s *Service) protoLibrary(c *db.Connector, admin bool) *v1.Connector {
	out := protoConnector(c, admin)
	out.Autoenabled = c.Identifier != "" && slices.Contains(s.autoenabled(), c.Identifier)
	return out
}

// libraryIdentifier settles a library preset's identifier. What the admin
// typed is normalised and refused when another preset holds it; with nothing
// typed it is made from the name, numbered if that one is taken.
func (s *Service) libraryIdentifier(typed, name, exceptID string) (string, error) {
	if strings.TrimSpace(typed) == "" {
		base := catalog.Identifier(name)
		if base == "" {
			base = "connector"
		}
		return catalog.FreeIdentifier(s.db, base, exceptID), nil
	}
	ident := catalog.Identifier(typed)
	if ident == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("an identifier needs a letter or a digit"))
	}
	if catalog.IdentifierTaken(s.db, ident, exceptID) {
		return "", connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("another library connector already uses the identifier %q", ident))
	}
	return ident, nil
}

func (s *Service) putBotConnector(ctx context.Context, botID string, c db.Connector) (*v1.BotConnector, error) {
	c.Name = s.uniqueName(botID, c.Name, c.ID)
	if security.Reserved(toolsgen.Slug(c.Name)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("that name is reserved"))
	}
	c.Kind = catalog.KindCustom
	c.BotID = botID
	if c.ID == "" {
		c.ID = ids.New()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	if err := s.db.Create(&c).Error; err != nil {
		return nil, err
	}
	st := StatusInit
	detail := initDetail(c.Transport)
	if c.Auth == AuthOAuth {
		st = StatusNeedsAuth
		detail = ""
	}
	row := db.BotConnector{
		ID: ids.New(), BotID: botID, ConnectorID: c.ID,
		AuthStatus: st, StatusDetail: detail, CreatedAt: time.Now(),
	}
	if err := s.db.Create(&row).Error; err != nil {
		s.db.Delete(&c)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("already attached"))
	}
	if c.Auth != AuthOAuth {
		s.startRefresh(row.ID)
	}
	return protoBotConnector(&row, &c, true), nil
}

func (s *Service) uniqueName(botID, name, exceptID string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Connector"
	}
	if !s.slugTaken(botID, name, exceptID) {
		return name
	}
	for n := 2; n < 10000; n++ {
		cand := fmt.Sprintf("%s %d", name, n)
		if !s.slugTaken(botID, cand, exceptID) {
			return cand
		}
	}
	return name + " " + ids.New()[:6]
}

func (s *Service) slugTaken(botID, name, exceptID string) bool {
	slug := toolsgen.Slug(name)
	var rows []db.Connector
	s.db.Where("bot_id = ?", botID).Find(&rows)
	for i := range rows {
		if rows[i].ID != exceptID && toolsgen.Slug(rows[i].Name) == slug {
			return true
		}
	}
	return false
}

func (s *Service) editConnector(ctx context.Context, row *db.Connector) error {
	if row.Kind == catalog.KindCustom && row.BotID != "" {
		_, err := access.OwnBot(ctx, s.db, row.BotID)
		return err
	}
	return access.RequireAdmin(ctx)
}

func (s *Service) SeedConnectors(ctx context.Context, _ *connect.Request[v1.SeedConnectorsRequest]) (*connect.Response[v1.SeedConnectorsResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := catalog.Seed(s.db); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.SeedConnectorsResponse{}), nil
}
