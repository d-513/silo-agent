package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpbridge"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

const (
	connTypeMCP    = "mcp"
	transportHTTP  = "http"
	transportSTDIO = "stdio"
	// transportBuiltin connectors run Go code on the CP (internal/builtin).
	transportBuiltin = "builtin"
	authNone         = "none"
	authOAuth        = "oauth"
	statusNone       = "none"
	statusInit       = "initializing"
	statusNeedsAuth  = "needs_auth"
	statusOK         = "authorized"
	statusErr        = "error"
	imageMax         = 512 << 10

	// connectorInitWait bounds an async connect+tool-discovery job.
	connectorInitWait = 10 * time.Minute
)

func (a *App) ListConnectors(ctx context.Context, _ *connect.Request[v1.ListConnectorsRequest]) (*connect.Response[v1.ListConnectorsResponse], error) {
	admin := access.User(ctx) != nil && access.User(ctx).Admin
	var rows []db.Connector
	a.DB.Where("kind = ?", catalog.KindLibrary).Order("name").Find(&rows)
	out := &v1.ListConnectorsResponse{}
	for i := range rows {
		out.Connectors = append(out.Connectors, protoConnector(&rows[i], admin))
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateConnector(ctx context.Context, req *connect.Request[v1.CreateConnectorRequest]) (*connect.Response[v1.Connector], error) {
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
	applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), true)
	if row.Transport == transportSTDIO {
		row.Auth = authNone
		row.OAuthClientID = ""
		row.OAuthClientSecret = ""
	}
	row.Kind = catalog.KindLibrary
	if err := a.DB.Create(&row).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(protoConnector(&row, true)), nil
}

func (a *App) UpdateConnector(ctx context.Context, req *connect.Request[v1.UpdateConnectorRequest]) (*connect.Response[v1.Connector], error) {
	var row db.Connector
	if err := a.DB.First(&row, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := a.editConnector(ctx, &row); err != nil {
		return nil, err
	}
	m := req.Msg
	if n := strings.TrimSpace(m.GetName()); n != "" {
		if row.Kind == catalog.KindCustom && a.slugTaken(row.BotID, n, row.ID) {
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
	}
	if row.Transport == transportBuiltin {
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
		a.DB.Save(&row)
		a.restartConnector(&row)
		return connect.NewResponse(protoConnector(&row, true)), nil
	}
	if m.GetTransport() != "" {
		tr := strings.ToLower(m.GetTransport())
		if err := checkTransport(tr); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		row.Transport = tr
	}
	if row.Transport == transportSTDIO {
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
			if au != authNone && au != authOAuth {
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
	if row.Transport != transportSTDIO {
		applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), false)
	} else {
		row.OAuthClientSecret = ""
	}
	if err := setConnectorImage(&row, m); err != nil {
		return nil, err
	}
	a.DB.Save(&row)
	a.restartConnector(&row)
	return connect.NewResponse(protoConnector(&row, true)), nil
}

func (a *App) DeleteConnector(ctx context.Context, req *connect.Request[v1.DeleteConnectorRequest]) (*connect.Response[v1.DeleteConnectorResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	var row db.Connector
	if err := a.DB.First(&row, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if row.Kind == catalog.KindCustom {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("remove a custom connector from the Bot"))
	}
	a.DB.Delete(&row)
	return connect.NewResponse(&v1.DeleteConnectorResponse{}), nil
}

func (a *App) ListBotConnectors(ctx context.Context, req *connect.Request[v1.ListBotConnectorsRequest]) (*connect.Response[v1.ListBotConnectorsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.BotConnector
	a.DB.Where("bot_id = ?", req.Msg.GetBotId()).Order("created_at desc").Find(&rows)
	out := &v1.ListBotConnectorsResponse{}
	for i := range rows {
		var c db.Connector
		if a.DB.First(&c, "id = ?", rows[i].ConnectorID).Error != nil {
			continue
		}
		out.Connectors = append(out.Connectors, protoBotConnector(&rows[i], &c, true))
	}
	return connect.NewResponse(out), nil
}

func (a *App) AttachConnector(ctx context.Context, req *connect.Request[v1.AttachConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	botID := req.Msg.GetBotId()
	if _, err := a.ownBot(ctx, botID); err != nil {
		return nil, err
	}
	var lib db.Connector
	if err := a.DB.First(&lib, "id = ? AND kind = ?", req.Msg.GetConnectorId(), catalog.KindLibrary).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	out, err := a.putBotConnector(ctx, botID, cloneLibrary(&lib, botID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateBotConnector(ctx context.Context, req *connect.Request[v1.CreateBotConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	m := req.Msg
	if _, err := a.ownBot(ctx, m.GetBotId()); err != nil {
		return nil, err
	}
	admin := access.User(ctx) != nil && access.User(ctx).Admin
	from := strings.TrimSpace(m.GetSourceId())
	if lib := a.libraryBuiltin(from); lib != nil {
		out, err := a.attachBuiltin(ctx, m.GetBotId(), lib, m)
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
	if row.Transport != transportSTDIO {
		applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), true)
	}
	if err := a.overlayLibrary(from, &row, m.GetHeaders(), m.GetEnv()); err != nil {
		return nil, err
	}
	out, err := a.putBotConnector(ctx, m.GetBotId(), row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (a *App) overlayLibrary(src string, row *db.Connector, headers []*v1.HeaderInput, env []*v1.EnvInput) error {
	if src == "" {
		return nil
	}
	var lib db.Connector
	if err := a.DB.First(&lib, "id = ? AND kind = ?", src, catalog.KindLibrary).Error; err != nil {
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
	if row.Transport == transportSTDIO && row.StdioCommand == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("stdio_command required"))
	}
	return nil
}

func (a *App) DetachConnector(ctx context.Context, req *connect.Request[v1.DetachConnectorRequest]) (*connect.Response[v1.DetachConnectorResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if err := a.DB.First(&row, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	a.dropMCP(row.ID)
	a.dropStdio(&row)
	var c db.Connector
	if a.DB.First(&c, "id = ?", row.ConnectorID).Error == nil {
		a.pruneConnectorRules(row.BotID, toolsgen.Slug(c.Name), nil)
		if c.Kind == catalog.KindCustom {
			a.DB.Delete(&c)
		}
	}
	a.DB.Delete(&row)
	go a.pushTools(row.BotID)
	return connect.NewResponse(&v1.DetachConnectorResponse{}), nil
}

func (a *App) RefreshBotConnector(ctx context.Context, req *connect.Request[v1.RefreshBotConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if err := a.DB.First(&row, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	var c db.Connector
	if a.DB.First(&c, "id = ?", row.ConnectorID).Error != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	if c.Auth == authOAuth && row.AuthStatus == statusNeedsAuth {
		return connect.NewResponse(&v1.BotConnector{
			Id: row.ID, BotId: row.BotID, AuthStatus: row.AuthStatus, LastError: row.LastError,
			StatusDetail: row.StatusDetail,
			Connector:    protoConnector(&c, false),
		}), nil
	}
	// Dropping the MCP session also closes the sidecar tunnel; its bridge
	// reconnects and spawns a fresh child, reusing the container's npm cache.
	a.dropMCP(row.ID)
	a.applyInit(&row, c.Transport)
	a.startRefresh(row.ID)
	return connect.NewResponse(&v1.BotConnector{
		Id: row.ID, BotId: row.BotID, AuthStatus: row.AuthStatus, LastError: row.LastError,
		StatusDetail: row.StatusDetail,
		Connector:    protoConnector(&c, false),
	}), nil
}

func (a *App) handleConnectorImage(w http.ResponseWriter, r *http.Request) {
	if _, err := siloauth.UserFromRequest(a.DB, r); err != nil {
		httpSessionError(w, err)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/connectors/"), "/image")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	var c db.Connector
	if a.DB.First(&c, "id = ?", id).Error != nil || len(c.Image) == 0 {
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

func (a *App) findBotConnector(botID, slug string) (*db.BotConnector, *db.Connector, error) {
	var rows []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Find(&rows)
	for i := range rows {
		var c db.Connector
		if a.DB.First(&c, "id = ?", rows[i].ConnectorID).Error != nil {
			continue
		}
		if toolsgen.Slug(c.Name) == slug {
			return &rows[i], &c, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown connector %s", slug)
}

// attachDefaultConnectors copies every library preset flagged auto_attach onto
// a newly created Bot. It runs exactly once, from CreateBot, so a preset the
// human later removes from the Bot is never silently re-added.
func (a *App) attachDefaultConnectors(ctx context.Context, botID string) {
	if a.DB == nil {
		return
	}
	var libs []db.Connector
	a.DB.Where("kind = ? AND auto_attach = ?", catalog.KindLibrary, true).Order("name").Find(&libs)
	for i := range libs {
		if _, err := a.putBotConnector(ctx, botID, cloneLibrary(&libs[i], botID)); err != nil {
			log.Printf("default connector %s bot=%s: %v", libs[i].Name, botID, err)
		}
	}
}

func (a *App) putBotConnector(ctx context.Context, botID string, c db.Connector) (*v1.BotConnector, error) {
	c.Name = a.uniqueName(botID, c.Name, c.ID)
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
	if err := a.DB.Create(&c).Error; err != nil {
		return nil, err
	}
	st := statusInit
	detail := initDetail(c.Transport)
	if c.Auth == authOAuth {
		st = statusNeedsAuth
		detail = ""
	}
	row := db.BotConnector{
		ID: ids.New(), BotID: botID, ConnectorID: c.ID,
		AuthStatus: st, StatusDetail: detail, CreatedAt: time.Now(),
	}
	if err := a.DB.Create(&row).Error; err != nil {
		a.DB.Delete(&c)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("already attached"))
	}
	if c.Auth != authOAuth {
		a.startRefresh(row.ID)
	}
	return protoBotConnector(&row, &c, true), nil
}

func (a *App) uniqueName(botID, name, exceptID string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Connector"
	}
	if !a.slugTaken(botID, name, exceptID) {
		return name
	}
	for n := 2; n < 10000; n++ {
		cand := fmt.Sprintf("%s %d", name, n)
		if !a.slugTaken(botID, cand, exceptID) {
			return cand
		}
	}
	return name + " " + ids.New()[:6]
}

func (a *App) slugTaken(botID, name, exceptID string) bool {
	slug := toolsgen.Slug(name)
	var rows []db.Connector
	a.DB.Where("bot_id = ?", botID).Find(&rows)
	for i := range rows {
		if rows[i].ID != exceptID && toolsgen.Slug(rows[i].Name) == slug {
			return true
		}
	}
	return false
}

func (a *App) editConnector(ctx context.Context, row *db.Connector) error {
	if row.Kind == catalog.KindCustom && row.BotID != "" {
		_, err := a.ownBot(ctx, row.BotID)
		return err
	}
	return access.RequireAdmin(ctx)
}
