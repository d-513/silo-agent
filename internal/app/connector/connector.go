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
	// RenameRules moves a Bot's rules of one connector slug to another.
	RenameRules(botID, from, to string)
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
	// closed is set by CloseAll: a session a background refresh opens after
	// it is closed at once instead of being kept (and holding its stream).
	closed bool
	// locks serializes session dials and sidecar starts per connector.
	locks sync.Map
	// tokens holds each connection's OAuth token in memory (*tokenCell),
	// shared by every session on it.
	tokens sync.Map
	// bridges are the live reverse tunnels from STDIO sidecars.
	bridgesMu sync.Mutex
	bridges   map[string]*bridgeTunnel

	// ClientNet, when set, gives a STDIO sidecar its own internal network and
	// the gateway it reaches the internet through (the network filter).
	ClientNet func(ctx context.Context, botID, connectorID, name string) (*dockerx.ClientNet, error)

	// StaleCA, when set, reports whether a sidecar was made trusting a
	// network filter certificate the gateway no longer signs with.
	StaleCA func(st dockerx.State, botID, connectorID string) bool

	// ClientNetDrop reclaims a detached STDIO sidecar's egress network.
	ClientNetDrop func(ctx context.Context, connectorID string)
}

// clientNet is the sidecar's egress network, or nil when the filter is off.
func (s *Service) clientNet(ctx context.Context, botID, connectorID, name string) (*dockerx.ClientNet, error) {
	if s.ClientNet == nil {
		return nil, nil
	}
	return s.ClientNet(ctx, botID, connectorID, name)
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
	row.ExposeCompletion = m.GetExposeCompletion()
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
	before := row
	if n := strings.TrimSpace(m.GetName()); n != "" {
		if row.Kind == catalog.KindCustom && s.slugTaken(row.SpaceID, n, row.ID) {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("a connector in this workspace already uses that name"))
		}
		if row.Kind == catalog.KindCustom && security.Reserved(toolsgen.Slug(n)) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("that name is reserved"))
		}
		row.Name = n
	}
	row.Description = strings.TrimSpace(m.GetDescription())
	row.Prompt = strings.TrimSpace(m.GetPrompt())
	if m.ExposeCompletion != nil {
		row.ExposeCompletion = m.GetExposeCompletion()
	}
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
			// A password typed in now is the editor's to lend.
			if row.SecretsJSON != before.SecretsJSON && len(decodeStrMap(row.SecretsJSON)) > 0 {
				row.LentBy = access.User(ctx).ID
			}
		}
		if m.GetDefaultMode() != "" {
			row.DefaultMode = security.Rule(m.GetDefaultMode())
		}
		if err := setConnectorImage(&row, m); err != nil {
			return nil, err
		}
		s.db.Save(&row)
		s.renamed(&before, &row)
		s.restartConnector(&row)
		return connect.NewResponse(s.editedProto(ctx, &row)), nil
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
	if row.Kind == catalog.KindCustom && row.SourceID != "" && !sameTarget(&before, &row) {
		if err := keepOnlyGiven(&row, m.GetHeaders(), m.GetEnv(), m.GetOauthClientSecret()); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	// A credential typed in now is the editor's to lend.
	if row.Kind == catalog.KindCustom && typedCredential(&row) && !typedCredential(&before) {
		row.LentBy = access.User(ctx).ID
	}
	s.db.Save(&row)
	s.renamed(&before, &row)
	s.restartConnector(&row)
	return connect.NewResponse(s.editedProto(ctx, &row)), nil
}

// editedProto is the answer to an edit: a library preset as the library
// shows it, a Workspace connection as its hub does.
func (s *Service) editedProto(ctx context.Context, c *db.Connector) *v1.Connector {
	if c.Kind == catalog.KindCustom {
		return s.spaceProto(ctx, c)
	}
	return s.protoLibrary(c, true)
}

// renamed moves every granted Bot's rules for a connection to its new slug:
// a rename must not cost the Bots their rules.
func (s *Service) renamed(before, after *db.Connector) {
	from, to := toolsgen.Slug(before.Name), toolsgen.Slug(after.Name)
	if after.Kind != catalog.KindCustom || from == to {
		return
	}
	for _, g := range s.grants(after.ID) {
		s.host.RenameRules(g.BotID, from, to)
	}
}

// sameTarget reports whether two connectors talk to the same thing: the same
// address, or the same program with the same arguments in the same image.
func sameTarget(a, b *db.Connector) bool {
	if a.Transport != b.Transport {
		return false
	}
	if a.Transport == TransportSTDIO {
		return strings.TrimSpace(a.StdioCommand) == strings.TrimSpace(b.StdioCommand) &&
			strings.TrimSpace(a.StdioImage) == strings.TrimSpace(b.StdioImage) &&
			slices.Equal(mcpbridge.ParseArgs(a.StdioArgsJSON), mcpbridge.ParseArgs(b.StdioArgsJSON))
	}
	return strings.TrimSpace(a.HTTPURL) == strings.TrimSpace(b.HTTPURL)
}

// keepOnlyGiven strips a Bot's copy of a library preset down to the header
// values, environment and OAuth client secret this very request supplied. It
// runs when the copy stops talking to what the preset talks to. Those values
// came from the operator, who chose where they are sent; the Bot's owner never
// sees them, and must not be able to read them by pointing the copy at a
// server or a program of their own.
func keepOnlyGiven(row *db.Connector, headers []*v1.HeaderInput, env []*v1.EnvInput, oauthSecret string) error {
	hdr, err := mergeHeaders("", headers, false)
	if err != nil {
		return err
	}
	vars, err := mergeEnv("", env, false)
	if err != nil {
		return err
	}
	row.HeadersJSON, row.EnvJSON, row.OAuthClientSecret = hdr, vars, oauthSecret
	if row.Transport == TransportSTDIO {
		row.HeadersJSON, row.OAuthClientSecret = "{}", ""
	}
	return nil
}

// DeleteConnector deletes a library preset (an admin), or a Workspace's
// connection (who added it, or a Workspace admin), which first goes from
// every Bot it is granted to.
func (s *Service) DeleteConnector(ctx context.Context, req *connect.Request[v1.DeleteConnectorRequest]) (*connect.Response[v1.DeleteConnectorResponse], error) {
	var row db.Connector
	if err := s.db.First(&row, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := s.editConnector(ctx, &row); err != nil {
		return nil, err
	}
	if row.Kind == catalog.KindCustom {
		s.deleteConnection(ctx, &row)
	} else {
		s.db.Delete(&row)
	}
	return connect.NewResponse(&v1.DeleteConnectorResponse{}), nil
}

func (s *Service) ListBotConnectors(ctx context.Context, req *connect.Request[v1.ListBotConnectorsRequest]) (*connect.Response[v1.ListBotConnectorsResponse], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use); err != nil {
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
		out.Connectors = append(out.Connectors, s.grantProto(ctx, &rows[i], &c))
	}
	return connect.NewResponse(out), nil
}

// AttachConnector gives a Bot a connector: a Workspace connection (a grant),
// or a library preset, which first becomes a new connection of the Bot's
// Workspace.
func (s *Service) AttachConnector(ctx context.Context, req *connect.Request[v1.AttachConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	botID := req.Msg.GetBotId()
	b, err := access.Bot(ctx, s.db, botID, access.Manage)
	if err != nil {
		return nil, err
	}
	var src db.Connector
	if err := s.db.First(&src, "id = ?", req.Msg.GetConnectorId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	c := &src
	switch {
	case src.Kind == catalog.KindLibrary:
		if c, err = s.createConnection(b.SpaceID, access.User(ctx).ID, cloneLibrary(&src)); err != nil {
			return nil, err
		}
	case src.SpaceID == "":
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	row, err := s.grant(botID, c)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.grantProto(ctx, row, c)), nil
}

// CreateBotConnector adds a connection to a Workspace (bot_id's, or space_id
// from the hub) and grants it to bot_id and to every Bot in bot_ids.
func (s *Service) CreateBotConnector(ctx context.Context, req *connect.Request[v1.CreateBotConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	m := req.Msg
	spaceID, grantTo, err := s.addTargets(ctx, m.GetBotId(), m.GetSpaceId(), m.GetBotIds())
	if err != nil {
		return nil, err
	}
	userID := access.User(ctx).ID
	admin := access.User(ctx).Admin
	from := strings.TrimSpace(m.GetSourceId())
	if lib := s.libraryBuiltin(from); lib != nil {
		c, err := s.builtinConnection(spaceID, userID, lib, m)
		if err != nil {
			return nil, err
		}
		return s.grantAll(ctx, c, m.GetBotId(), grantTo)
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
	row.ExposeCompletion = m.GetExposeCompletion()
	if err := s.overlayLibrary(from, &row, m.GetHeaders(), m.GetEnv(), m.ExposeCompletion == nil); err != nil {
		return nil, err
	}
	c, err := s.createConnection(spaceID, userID, row)
	if err != nil {
		return nil, err
	}
	return s.grantAll(ctx, c, m.GetBotId(), grantTo)
}

// addTargets is where a new connection goes and which Bots get it: a Bot's
// Workspace when it is added from the Bot (which the caller manages), else
// the Workspace named (where the caller may build). Every Bot it is also
// granted to is one the caller manages in that Workspace.
func (s *Service) addTargets(ctx context.Context, botID, spaceID string, botIDs []string) (string, []string, error) {
	if botID != "" {
		b, err := access.Bot(ctx, s.db, botID, access.Manage)
		if err != nil {
			return "", nil, err
		}
		spaceID = b.SpaceID
	} else if _, _, err := access.Space(ctx, s.db, spaceID, access.Build); err != nil {
		return "", nil, err
	}
	var out []string
	for _, id := range botIDs {
		if id == "" || id == botID || slices.Contains(out, id) {
			continue
		}
		b, err := access.Bot(ctx, s.db, id, access.Manage)
		if err != nil {
			return "", nil, err
		}
		if b.SpaceID != spaceID {
			return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the Bot and the connector are in different workspaces"))
		}
		out = append(out, id)
	}
	return spaceID, out, nil
}

// grantAll grants a new connection to the Bot it was added from and to the
// others chosen, and answers with the first Bot's grant (or the connection
// alone, from the hub).
func (s *Service) grantAll(ctx context.Context, c *db.Connector, botID string, others []string) (*connect.Response[v1.BotConnector], error) {
	out := &v1.BotConnector{Connector: s.spaceProto(ctx, c)}
	for i, id := range append([]string{botID}, others...) {
		if id == "" {
			continue
		}
		row, err := s.grant(id, c)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			out = s.grantProto(ctx, row, c)
		}
	}
	if out.Id == "" {
		out.Connector = s.spaceProto(ctx, c)
	}
	return connect.NewResponse(out), nil
}

// overlayLibrary fills what the add form left blank from the library preset it
// started from. inheritExpose is true when the form did not say whether the
// tools go to the model directly, so the preset's choice stands.
func (s *Service) overlayLibrary(src string, row *db.Connector, headers []*v1.HeaderInput, env []*v1.EnvInput, inheritExpose bool) error {
	if src == "" {
		return nil
	}
	var lib db.Connector
	if err := s.db.First(&lib, "id = ? AND kind = ?", src, catalog.KindLibrary).Error; err != nil {
		return connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	row.SourceID = lib.ID
	if inheritExpose {
		row.ExposeCompletion = lib.ExposeCompletion
	}
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
	if row.Prompt == "" {
		row.Prompt = lib.Prompt
	}
	if row.Transport == "" {
		row.Transport = lib.Transport
	}
	if row.Transport == lib.Transport && row.Transport != TransportSTDIO && row.HTTPURL == "" {
		row.HTTPURL = lib.HTTPURL
	}
	if row.StdioCommand == "" {
		row.StdioCommand = lib.StdioCommand
		row.StdioArgsJSON = lib.StdioArgsJSON
	}
	if row.StdioImage == "" {
		row.StdioImage = lib.StdioImage
	}
	// The preset's header values, environment and OAuth client secret are the
	// operator's, sent where the operator pointed the preset. A copy that
	// starts out pointed elsewhere gets only what its own form supplied.
	if !sameTarget(row, &lib) {
		if err := keepOnlyGiven(row, headers, env, row.OAuthClientSecret); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		if row.Transport == TransportSTDIO && row.StdioCommand == "" {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("stdio_command required"))
		}
		return nil
	}
	if row.OAuthClientSecret == "" {
		row.OAuthClientSecret = lib.OAuthClientSecret
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
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Administer); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if err := s.db.First(&row, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	s.revoke(ctx, &row)
	return connect.NewResponse(&v1.DetachConnectorResponse{}), nil
}

func (s *Service) RefreshBotConnector(ctx context.Context, req *connect.Request[v1.RefreshBotConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage); err != nil {
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
	if c.Auth == AuthOAuth && !s.signedIn(&c) {
		return connect.NewResponse(s.grantProto(ctx, &row, &c)), nil
	}
	// Dropping the MCP session also closes the sidecar tunnel; its bridge
	// reconnects and spawns a fresh child, reusing the container's npm cache.
	s.DropMCP(row.ID)
	s.applyInit(&row, c.Transport)
	s.startRefresh(row.ID)
	return connect.NewResponse(s.grantProto(ctx, &row, &c)), nil
}

func (s *Service) ServeImage(w http.ResponseWriter, r *http.Request) {
	if _, err := siloauth.UserFromRequest(s.db, r, s.cfg().HostCookies()); err != nil {
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
	// The stored type is what an upload claimed in rows from before uploads
	// were checked, so the bytes are asked again: an image, or a download.
	ct := imageType(c.Image, c.ImageType)
	if ct == "" {
		ct = "application/octet-stream"
		w.Header().Set("Content-Disposition", "attachment")
	}
	// An SVG opened as a page of its own is a document: sandboxed, it has no
	// origin and runs nothing. As an <img> the policy changes nothing.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
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

// AttachDefaults grants a newly created Bot every library preset a Bot starts
// with: the Workspace's connection made from that preset when it has one
// (two Bots with Lightpanda share the connection and keep a session each),
// else a new one. It runs exactly once, from CreateBot, so a preset the human
// later removes from the Bot is never silently re-added.
func (s *Service) AttachDefaults(ctx context.Context, botID string) {
	if s.db == nil {
		return
	}
	var b db.Bot
	if s.db.Where("id = ?", botID).Limit(1).Find(&b); b.ID == "" {
		return
	}
	libs := s.defaults()
	for i := range libs {
		var c db.Connector
		s.db.Where("space_id = ? AND kind = ? AND source_id = ?", b.SpaceID, catalog.KindCustom, libs[i].ID).Order("created_at").Limit(1).Find(&c)
		conn := &c
		if c.ID == "" {
			made, err := s.createConnection(b.SpaceID, b.CreatedBy, cloneLibrary(&libs[i]))
			if err != nil {
				log.Printf("default connector %s bot=%s: %v", libs[i].Name, botID, err)
				continue
			}
			conn = made
		}
		if _, err := s.grant(botID, conn); err != nil {
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

// uniqueName is name, numbered ("GitHub 2") if its slug is taken in the
// Workspace: a connection's slug is its `tools.<slug>` in every Bot.
func (s *Service) uniqueName(spaceID, name, exceptID string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Connector"
	}
	if !s.slugTaken(spaceID, name, exceptID) {
		return name
	}
	for n := 2; n < 10000; n++ {
		cand := fmt.Sprintf("%s %d", name, n)
		if !s.slugTaken(spaceID, cand, exceptID) {
			return cand
		}
	}
	return name + " " + ids.New()[:6]
}

func (s *Service) slugTaken(spaceID, name, exceptID string) bool {
	slug := toolsgen.Slug(name)
	var rows []db.Connector
	s.db.Where("space_id = ? AND kind = ?", spaceID, catalog.KindCustom).Find(&rows)
	for i := range rows {
		if rows[i].ID != exceptID && toolsgen.Slug(rows[i].Name) == slug {
			return true
		}
	}
	return false
}

// editConnector says whether the caller may change or delete a row: a
// library preset is an instance admin's, a Workspace connection is whoever
// added it or a Workspace admin's.
func (s *Service) editConnector(ctx context.Context, row *db.Connector) error {
	if row.Kind == catalog.KindCustom {
		return s.canEdit(ctx, row)
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
