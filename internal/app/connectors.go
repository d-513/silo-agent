package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	v1 "silo.agent/gen/silo/v1"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

const (
	connTypeMCP     = "mcp"
	transportHTTP   = "http"
	transportSTDIO  = "stdio"
	authNone        = "none"
	authOAuth       = "oauth"
	statusNone      = "none"
	statusNeedsAuth = "needs_auth"
	statusOK        = "authorized"
	statusErr       = "error"
	imageMax        = 512 << 10
)

func (a *App) ListConnectors(ctx context.Context, _ *connect.Request[v1.ListConnectorsRequest]) (*connect.Response[v1.ListConnectorsResponse], error) {
	admin := currentUser(ctx) != nil && currentUser(ctx).Admin
	var rows []db.Connector
	a.DB.Where("kind = ?", catalog.KindLibrary).Order("name").Find(&rows)
	out := &v1.ListConnectorsResponse{}
	for i := range rows {
		out.Connectors = append(out.Connectors, protoConnector(&rows[i], admin))
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateConnector(ctx context.Context, req *connect.Request[v1.CreateConnectorRequest]) (*connect.Response[v1.Connector], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	m := req.Msg
	row, err := parseConnector(m.GetName(), m.GetDescription(), m.GetTransport(), m.GetHttpUrl(), m.GetAuth(), m.GetDefaultMode(), m.GetImage(), m.GetImageType(), m.GetHeaders(), true)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), true)
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
	if m.GetTransport() != "" {
		tr := strings.ToLower(m.GetTransport())
		if tr == transportSTDIO {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("stdio is not available yet"))
		}
		if tr != transportHTTP {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("transport must be http"))
		}
		row.Transport = tr
	}
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
	if m.GetDefaultMode() != "" {
		row.DefaultMode = security.Rule(m.GetDefaultMode())
	}
	if m.GetHeaders() != nil {
		hdr, err := mergeHeaders(row.HeadersJSON, m.GetHeaders(), false)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		row.HeadersJSON = hdr
	}
	applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), false)
	if m.GetClearImage() {
		row.Image, row.ImageType = nil, ""
	} else if len(m.GetImage()) > 0 {
		img, imgType, err := clipImage(m.GetImage(), m.GetImageType())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		row.Image, row.ImageType = img, imgType
	}
	a.DB.Save(&row)
	return connect.NewResponse(protoConnector(&row, true)), nil
}

func (a *App) DeleteConnector(ctx context.Context, req *connect.Request[v1.DeleteConnectorRequest]) (*connect.Response[v1.DeleteConnectorResponse], error) {
	if err := requireAdmin(ctx); err != nil {
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
	from := strings.TrimSpace(m.GetSourceId())
	row, err := parseConnector(m.GetName(), m.GetDescription(), m.GetTransport(), m.GetHttpUrl(), m.GetAuth(), m.GetDefaultMode(), m.GetImage(), m.GetImageType(), m.GetHeaders(), from == "")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	applyOAuthClient(&row, m.GetOauthClientId(), m.GetOauthClientSecret(), true)
	if err := a.overlayLibrary(from, &row, m.GetHeaders()); err != nil {
		return nil, err
	}
	out, err := a.putBotConnector(ctx, m.GetBotId(), row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (a *App) overlayLibrary(src string, row *db.Connector, headers []*v1.HeaderInput) error {
	if src == "" {
		return nil
	}
	var lib db.Connector
	if err := a.DB.First(&lib, "id = ? AND kind = ?", src, catalog.KindLibrary).Error; err != nil {
		return connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	row.SourceID = lib.ID
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
	if len(headers) == 0 {
		row.HeadersJSON = lib.HeadersJSON
		return nil
	}
	hdr, err := mergeHeaders(lib.HeadersJSON, headers, false)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	row.HeadersJSON = hdr
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
			Connector: protoConnector(&c, false),
		}), nil
	}
	a.dropMCP(row.ID)
	if err := a.refreshTools(ctx, &row, &c); err != nil {
		row.AuthStatus = statusErr
		row.LastError = err.Error()
		a.DB.Save(&row)
	}
	return connect.NewResponse(&v1.BotConnector{
		Id: row.ID, BotId: row.BotID, AuthStatus: row.AuthStatus, LastError: row.LastError,
		Connector: protoConnector(&c, false),
	}), nil
}

func (a *App) StartConnectorAuth(ctx context.Context, req *connect.Request[v1.StartConnectorAuthRequest]) (*connect.Response[v1.StartConnectorAuthResponse], error) {
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
	if c.Auth != authOAuth {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("connector does not use oauth"))
	}
	redirect := a.publicURL(ctx) + "/oauth/callback"
	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)
	codeCh := make(chan *mcpauth.AuthorizationResult, 1)
	h, err := a.oauthHandler(&c, &row, redirect, func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		u := args.URL
		if st := queryParam(u, "state"); st != "" {
			a.mu.Lock()
			a.oauth[st] = &oauthWait{ch: codeCh, issuer: originOf(u)}
			a.mu.Unlock()
		}
		select {
		case urlCh <- u:
		default:
		}
		select {
		case res := <-codeCh:
			return res, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		return nil, err
	}
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		sess, err := mcpx.Connect(cctx, a.dial(&c, h))
		if err != nil {
			err = wrapOAuth(err)
			a.DB.Model(&row).Updates(map[string]any{"auth_status": statusErr, "last_error": err.Error()})
			select {
			case errCh <- err:
			default:
			}
			return
		}
		defer sess.Close()
		a.persistOAuthToken(h, &row)
		row.AuthStatus = statusOK
		row.LastError = ""
		a.DB.Save(&row)
		_ = a.refreshTools(cctx, &row, &c)
	}()
	select {
	case u := <-urlCh:
		return connect.NewResponse(&v1.StartConnectorAuthResponse{AuthorizeUrl: u}), nil
	case err := <-errCh:
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("authorization server did not start a redirect"))
	}
}

func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	a.mu.Lock()
	wait := a.oauth[state]
	delete(a.oauth, state)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if wait == nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "<p>Unknown or expired authorization. You can close this window.</p>")
		return
	}
	iss := q.Get("iss")
	if iss == "" {
		iss = wait.issuer
	}
	select {
	case wait.ch <- &mcpauth.AuthorizationResult{Code: q.Get("code"), State: state, Iss: iss}:
	default:
	}
	_, _ = io.WriteString(w, "<p>Authorized — you can close this.</p>")
}

func (a *App) handleConnectorImage(w http.ResponseWriter, r *http.Request) {
	if _, err := siloauth.UserFromRequest(a.DB, r); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

func (a *App) CallTool(ctx context.Context, req *connect.Request[v1.ToolReq]) (*connect.Response[v1.ToolRes], error) {
	bot := currentBot(ctx)
	slug := req.Msg.GetConnector()
	action := req.Msg.GetAction()
	if security.Reserved(slug) {
		return a.callBuiltin(ctx, bot, slug, action, req.Msg.GetArgsJson(), req.Msg.GetRunId())
	}
	bc, c, err := a.findBotConnector(bot.ID, slug)
	if err != nil {
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	if c.Transport != transportHTTP {
		return connect.NewResponse(&v1.ToolRes{Error: "stdio is not available yet"}), nil
	}
	if c.Auth == authOAuth && bc.AuthStatus != statusOK {
		return connect.NewResponse(&v1.ToolRes{Error: "connector is not authorized"}), nil
	}
	if hdr, err := mcpx.HeadersFromJSON(c.HeadersJSON); err == nil {
		for _, v := range hdr {
			a.Mask(bot.ID).Add(v)
		}
	}
	runID := req.Msg.GetRunId()
	tool := slug + "." + action
	a.emit(bot.ID, a.chatOfRun(runID), runID, "call", callTitle(c.Name, action), tool)
	if _, err := a.authorizeAction(ctx, bot, runID, slug, action, req.Msg.GetArgsJson(), c.DefaultMode); err != nil {
		a.emitCallDone(bot.ID, runID, tool, err.Error())
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	sess, err := a.mcpSession(ctx, bc, c)
	if err != nil {
		a.emitCallDone(bot.ID, runID, tool, err.Error())
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	args := map[string]any{}
	if raw := strings.TrimSpace(req.Msg.GetArgsJson()); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			a.emitCallDone(bot.ID, runID, tool, "invalid args")
			return connect.NewResponse(&v1.ToolRes{Error: "invalid args"}), nil
		}
	}
	for k, v := range args {
		if v == nil {
			delete(args, k)
		}
	}
	out, err := mcpx.Call(ctx, sess, action, args)
	if err != nil && errors.Is(err, mcpx.ErrSessionGone) {
		a.dropMCP(bc.ID)
		sess, err = a.mcpSession(ctx, bc, c)
		if err == nil {
			out, err = mcpx.Call(ctx, sess, action, args)
		}
	}
	if err != nil {
		a.emitCallDone(bot.ID, runID, tool, err.Error())
		return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
	}
	out = a.Mask(bot.ID).Apply(out)
	a.emitCallDone(bot.ID, runID, tool, capCall(out))
	return connect.NewResponse(&v1.ToolRes{ResultJson: out}), nil
}

func (a *App) callBuiltin(ctx context.Context, bot *db.Bot, slug, action, argsJSON, runID string) (*connect.Response[v1.ToolRes], error) {
	switch slug {
	case security.Desktop:
		tool := security.Key(slug, action)
		a.emit(bot.ID, a.chatOfRun(runID), runID, "call", callTitle("Desktop", action), tool)
		if _, err := a.authorizeAction(ctx, bot, runID, slug, action, argsJSON, ""); err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		a.emitCallDone(bot.ID, runID, tool, "ok")
		return connect.NewResponse(&v1.ToolRes{ResultJson: `{"ok":true}`}), nil
	case security.Web:
		if action != "search" {
			return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
		}
		tool := security.Key(slug, action)
		title := security.Describe(slug, action, argsJSON).Title
		a.emit(bot.ID, a.chatOfRun(runID), runID, "call", title, tool)
		if _, err := a.authorizeAction(ctx, bot, runID, slug, action, argsJSON, ""); err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		out, err := a.runWebSearch(ctx, argsJSON)
		if err != nil {
			a.emitCallDone(bot.ID, runID, tool, err.Error())
			return connect.NewResponse(&v1.ToolRes{Error: err.Error()}), nil
		}
		out = a.Mask(bot.ID).Apply(out)
		a.emitCallDone(bot.ID, runID, tool, capCall(out))
		return connect.NewResponse(&v1.ToolRes{ResultJson: out}), nil
	default:
		return connect.NewResponse(&v1.ToolRes{Error: "unknown connector"}), nil
	}
}

func callTitle(name, action string) string {
	a := action
	if i := strings.LastIndex(action, "__"); i >= 0 {
		a = action[i+2:]
	} else if i := strings.LastIndex(action, "."); i >= 0 {
		a = action[i+1:]
	}
	a = strings.ReplaceAll(a, "_", " ")
	if name == "" {
		return a
	}
	return name + " · " + a
}

func capCall(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "\n…truncated"
	}
	return s
}

func (a *App) authorizeAction(ctx context.Context, bot *db.Bot, runID, conn, action, argsJSON, fallback string) (string, error) {
	decision := security.Rule(a.ruleDecision(bot.ID, conn, action, fallback))
	switch decision {
	case security.Deny:
		a.audit(bot, "worker", conn+"."+action, security.Deny)
		return "", errors.New("denied")
	case security.Allow:
		a.audit(bot, "worker", conn+"."+action, security.Allow)
		return "", nil
	default:
		ap := db.Approval{
			ID: ids.New(), BotID: bot.ID, RunID: runID,
			Connector: conn, Action: action, ArgsJSON: security.Redact(conn, action, argsJSON), Status: "pending", CreatedAt: time.Now(),
		}
		a.DB.Create(&ap)
		ch := make(chan string, 1)
		a.mu.Lock()
		a.approvals[ap.ID] = &waiter{ch: ch, botID: bot.ID, runID: runID}
		a.mu.Unlock()
		a.setBotStatus(bot.ID, "needs_you")
		a.emit(bot.ID, a.chatOfRun(runID), runID, "approval", ap.ID, conn+"."+action)
		var dec string
		select {
		case <-ctx.Done():
			a.dropWaiter(ap.ID)
			a.DB.Model(&db.Approval{}).Where("id = ? AND status = ?", ap.ID, "pending").Update("status", "canceled")
			a.recomputeStatus(bot.ID)
			return ap.ID, errors.New("canceled")
		case dec = <-ch:
		}
		if !security.Granted(dec) {
			return ap.ID, errors.New("denied")
		}
		return ap.ID, nil
	}
}

func (a *App) refreshTools(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	sess, err := a.mcpSession(ctx, row, c)
	if err != nil {
		return err
	}
	tools, err := mcpx.ListAll(ctx, sess)
	if err != nil && errors.Is(err, mcpx.ErrSessionGone) {
		a.dropMCP(row.ID)
		sess, err = a.mcpSession(ctx, row, c)
		if err == nil {
			tools, err = mcpx.ListAll(ctx, sess)
		}
	}
	if err != nil {
		return err
	}
	b, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	row.ToolsJSON = string(b)
	row.AuthStatus = statusOK
	row.LastError = ""
	a.DB.Save(row)
	var names []string
	for _, t := range tools {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	a.pruneConnectorRules(row.BotID, toolsgen.Slug(c.Name), names)
	go a.pushTools(row.BotID)
	return nil
}

func (a *App) pushTools(botID string) {
	var rows []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Find(&rows)
	var stubs []*v1.ToolStub
	for i := range rows {
		var c db.Connector
		if a.DB.First(&c, "id = ?", rows[i].ConnectorID).Error != nil {
			continue
		}
		slug := toolsgen.Slug(c.Name)
		var tools []mcpx.Tool
		_ = json.Unmarshal([]byte(rows[i].ToolsJSON), &tools)
		for _, t := range tools {
			schema, _ := json.Marshal(t.InputSchema)
			stubs = append(stubs, &v1.ToolStub{
				Connector: slug, Action: t.Name, Description: t.Description, ArgsSchemaJson: string(schema),
			})
		}
	}
	cmd := &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_SyncTools{SyncTools: &v1.SyncToolsCmd{Stubs: stubs}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := a.Hub.Exec(ctx, botID, cmd); err != nil {
		log.Printf("sync tools bot=%s: %v", botID, err)
	}
}

func (a *App) connectorBlurb(botID string) string {
	if a.DB == nil {
		return ""
	}
	var rows []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Find(&rows)
	var b strings.Builder
	b.WriteString("\n\n## Connectors\n")
	if len(rows) == 0 {
		b.WriteString("None attached. Do not invent MCP servers, packages, or credentials.\n")
		return b.String()
	}
	b.WriteString("Attached on this Bot. Use exec_python and `import tools.<slug>`. They are not chat tools.\n")
	for i := range rows {
		var c db.Connector
		if a.DB.First(&c, "id = ?", rows[i].ConnectorID).Error != nil {
			continue
		}
		slug := toolsgen.Slug(c.Name)
		switch rows[i].AuthStatus {
		case statusNeedsAuth:
			fmt.Fprintf(&b, "- `%s` (%s) — needs Authorize on the Connectors tab.\n", slug, c.Name)
		case statusErr:
			err := rows[i].LastError
			if len(err) > 180 {
				err = err[:180]
			}
			fmt.Fprintf(&b, "- `%s` (%s) — refresh failed: %s\n", slug, c.Name, err)
		default:
			var tools []mcpx.Tool
			_ = json.Unmarshal([]byte(rows[i].ToolsJSON), &tools)
			var names []string
			for _, t := range tools {
				if t.Name != "" {
					names = append(names, t.Name)
				}
			}
			if len(names) == 0 {
				fmt.Fprintf(&b, "- `%s` (%s) — attached; Refresh on the Connectors tab if import tools finds nothing.\n", slug, c.Name)
				break
			}
			if len(names) > 12 {
				names = append(names[:12], "…")
			}
			fmt.Fprintf(&b, "- `import tools.%s` — %s\n", slug, strings.Join(names, ", "))
		}
	}
	return b.String()
}

func (a *App) mcpSession(ctx context.Context, row *db.BotConnector, c *db.Connector) (*mcpx.Session, error) {
	a.mu.Lock()
	if s := a.mcp[row.ID]; s != nil {
		a.mu.Unlock()
		return s, nil
	}
	a.mu.Unlock()
	var h mcpauth.OAuthHandler
	if c.Auth == authOAuth {
		oh, err := a.oauthHandler(c, row, a.redirectURL(), nil)
		if err != nil {
			return nil, err
		}
		h = oh
	}
	sess, err := mcpx.Connect(ctx, a.dial(c, h))
	if err != nil {
		return nil, wrapOAuth(err)
	}
	a.mu.Lock()
	if old := a.mcp[row.ID]; old != nil {
		_ = old.Close()
	}
	a.mcp[row.ID] = sess
	a.mu.Unlock()
	return sess, nil
}

func (a *App) dropMCP(id string) {
	a.mu.Lock()
	s := a.mcp[id]
	delete(a.mcp, id)
	a.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}

func (a *App) dial(c *db.Connector, h mcpauth.OAuthHandler) mcpx.Dial {
	hdr, _ := mcpx.HeadersFromJSON(c.HeadersJSON)
	return mcpx.Dial{URL: c.HTTPURL, Headers: hdr, OAuth: h}
}

func (a *App) oauthHandler(c *db.Connector, row *db.BotConnector, redirect string, fetch mcpauth.AuthorizationCodeFetcher) (*mcpauth.AuthorizationCodeHandler, error) {
	if fetch == nil {
		fetch = func(context.Context, *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			return nil, errors.New("authorization required")
		}
	}
	cfg := &mcpauth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirect,
		AuthorizationCodeFetcher: fetch,
		RequestRefreshToken:      true,
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				RedirectURIs:            []string{redirect},
				ClientName:              "Silo Agent",
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				TokenEndpointAuthMethod: "none",
			},
		},
		NewTokenSource: func(ctx context.Context, oc *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			src := oc.TokenSource(ctx, tok)
			return persistSrc{inner: src, save: func(t *oauth2.Token) { a.saveToken(row.ID, t) }}, nil
		},
	}
	if creds := preregisteredClient(c); creds != nil {
		cfg.PreregisteredClient = creds
	}
	if tok := tokenFromJSON(row.TokenJSON); tok != nil {
		cfg.InitialTokenSource = persistSrc{
			inner: oauth2.ReuseTokenSource(tok, oauth2.StaticTokenSource(tok)),
			save:  func(t *oauth2.Token) { a.saveToken(row.ID, t) },
		}
	}
	return mcpauth.NewAuthorizationCodeHandler(cfg)
}

func (a *App) persistOAuthToken(h *mcpauth.AuthorizationCodeHandler, row *db.BotConnector) {
	src, err := h.TokenSource(context.Background())
	if err != nil || src == nil {
		return
	}
	tok, err := src.Token()
	if err != nil || tok == nil {
		return
	}
	a.saveToken(row.ID, tok)
	row.TokenJSON = mustJSON(tok)
}

func (a *App) saveToken(id string, tok *oauth2.Token) {
	if tok == nil {
		return
	}
	a.DB.Model(&db.BotConnector{}).Where("id = ?", id).Update("token_json", mustJSON(tok))
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

func (a *App) publicURL(ctx context.Context) string {
	if u := strings.TrimSpace(a.cfg().PublicURL); u != "" {
		return strings.TrimRight(u, "/")
	}
	r := httpReq(ctx)
	if r == nil {
		return "http://127.0.0.1:5173"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func (a *App) redirectURL() string {
	if u := strings.TrimSpace(a.cfg().PublicURL); u != "" {
		return strings.TrimRight(u, "/") + "/oauth/callback"
	}
	return "http://127.0.0.1:5173/oauth/callback"
}

func (a *App) initConnectors() {
	if a.DB == nil {
		return
	}
	a.DB.Model(&db.Connector{}).Where("kind = '' OR kind IS NULL").Update("kind", catalog.KindLibrary)
	var links []db.BotConnector
	a.DB.Find(&links)
	for i := range links {
		var c db.Connector
		if a.DB.First(&c, "id = ?", links[i].ConnectorID).Error != nil {
			continue
		}
		if c.Kind == catalog.KindCustom && c.BotID != "" {
			continue
		}
		clone := cloneLibrary(&c, links[i].BotID)
		if err := a.DB.Create(&clone).Error; err != nil {
			log.Printf("connector backfill bot=%s: %v", links[i].BotID, err)
			continue
		}
		links[i].ConnectorID = clone.ID
		a.DB.Save(&links[i])
	}
	if a.Store != nil {
		if err := catalog.Seed(a.DB); err != nil {
			log.Printf("connector library seed: %v", err)
		}
		if err := catalog.SeedSkills(a.cfg().DataDir); err != nil {
			log.Printf("skill library seed: %v", err)
		}
	}
}

func parseConnector(name, desc, transport, httpURL, auth, mode string, image []byte, imageType string, headers []*v1.HeaderInput, requireHeaderValues bool) (db.Connector, error) {
	if strings.TrimSpace(name) == "" {
		return db.Connector{}, errors.New("name required")
	}
	tr := strings.ToLower(transport)
	if tr == "" {
		tr = transportHTTP
	}
	if tr == transportSTDIO {
		return db.Connector{}, errors.New("stdio is not available yet")
	}
	if tr != transportHTTP {
		return db.Connector{}, errors.New("transport must be http")
	}
	au := strings.ToLower(auth)
	if au == "" {
		au = authNone
	}
	if au != authNone && au != authOAuth {
		return db.Connector{}, errors.New("auth must be none or oauth")
	}
	if strings.TrimSpace(httpURL) == "" {
		return db.Connector{}, errors.New("http_url required")
	}
	hdr, err := mergeHeaders("", headers, requireHeaderValues)
	if err != nil {
		return db.Connector{}, err
	}
	img, imgType, err := clipImage(image, imageType)
	if err != nil {
		return db.Connector{}, err
	}
	return db.Connector{
		ID: ids.New(), Type: connTypeMCP, Name: strings.TrimSpace(name),
		Description: strings.TrimSpace(desc), Image: img, ImageType: imgType,
		Transport: tr, HTTPURL: strings.TrimSpace(httpURL), Auth: au,
		HeadersJSON: hdr, DefaultMode: security.Rule(mode), CreatedAt: time.Now(),
	}, nil
}

func cloneLibrary(lib *db.Connector, botID string) db.Connector {
	return db.Connector{
		ID: ids.New(), Kind: catalog.KindCustom, BotID: botID, SourceID: lib.ID,
		Type: lib.Type, Name: lib.Name, Description: lib.Description,
		Image: append([]byte(nil), lib.Image...), ImageType: lib.ImageType,
		Transport: lib.Transport, HTTPURL: lib.HTTPURL, Auth: lib.Auth,
		OAuthClientID: lib.OAuthClientID, OAuthClientSecret: lib.OAuthClientSecret,
		HeadersJSON: lib.HeadersJSON, DefaultMode: lib.DefaultMode, CreatedAt: time.Now(),
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
	st := statusNone
	if c.Auth == authOAuth {
		st = statusNeedsAuth
	}
	row := db.BotConnector{
		ID: ids.New(), BotID: botID, ConnectorID: c.ID,
		AuthStatus: st, CreatedAt: time.Now(),
	}
	if err := a.DB.Create(&row).Error; err != nil {
		a.DB.Delete(&c)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("already attached"))
	}
	if c.Auth != authOAuth {
		if err := a.refreshTools(ctx, &row, &c); err != nil {
			row.AuthStatus = statusErr
			row.LastError = err.Error()
			a.DB.Save(&row)
		}
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
	return requireAdmin(ctx)
}

func protoBotConnector(row *db.BotConnector, c *db.Connector, keys bool) *v1.BotConnector {
	return &v1.BotConnector{
		Id: row.ID, BotId: row.BotID, AuthStatus: row.AuthStatus, LastError: row.LastError,
		Connector: protoConnector(c, keys),
	}
}

func protoConnector(c *db.Connector, admin bool) *v1.Connector {
	kind := c.Kind
	if kind == "" {
		kind = catalog.KindLibrary
	}
	out := &v1.Connector{
		Id: c.ID, Type: c.Type, Name: c.Name, Description: c.Description,
		HasImage: len(c.Image) > 0, Transport: c.Transport, HttpUrl: c.HTTPURL, Auth: c.Auth,
		DefaultMode: security.Rule(c.DefaultMode), CreatedAt: c.CreatedAt.Format(time.RFC3339),
		Kind: kind, SourceId: c.SourceID, CatalogGuide: catalog.Guide(c.SeedKey),
	}
	hdr, _ := mcpx.HeadersFromJSON(c.HeadersJSON)
	for k := range hdr {
		out.HeaderKeys = append(out.HeaderKeys, &v1.HeaderKey{Name: k})
	}
	if admin {
		out.OauthClientId = c.OAuthClientID
		out.HasOauthClientSecret = c.OAuthClientSecret != ""
	} else {
		out.HeaderKeys = nil
	}
	return out
}

func applyOAuthClient(c *db.Connector, id, secret string, replaceSecret bool) {
	c.OAuthClientID = strings.TrimSpace(id)
	if replaceSecret || secret != "" {
		c.OAuthClientSecret = secret
	}
}

// preregisteredClient is the MCP-spec fallback when the authorization server
// has no registration_endpoint (GitHub) and no CIMD.
func preregisteredClient(c *db.Connector) *oauthex.ClientCredentials {
	id := strings.TrimSpace(c.OAuthClientID)
	if id == "" {
		return nil
	}
	out := &oauthex.ClientCredentials{ClientID: id}
	if s := c.OAuthClientSecret; s != "" {
		out.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: s}
	}
	return out
}

func wrapOAuth(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "no configured client registration methods") {
		return fmt.Errorf("%w — this server does not register clients automatically; set an OAuth Client ID and Secret on the connector", err)
	}
	return err
}

func clipImage(b []byte, typ string) ([]byte, string, error) {
	if len(b) == 0 {
		return nil, "", nil
	}
	if len(b) > imageMax {
		return nil, "", fmt.Errorf("image too large (%d bytes, max %d)", len(b), imageMax)
	}
	if typ == "" {
		typ = http.DetectContentType(b)
	}
	return b, typ, nil
}

func mergeHeaders(existingJSON string, ins []*v1.HeaderInput, requireValue bool) (string, error) {
	old, _ := mcpx.HeadersFromJSON(existingJSON)
	next := map[string]string{}
	for _, h := range ins {
		name := strings.TrimSpace(h.GetName())
		if name == "" {
			continue
		}
		val := h.GetValue()
		if val == "" {
			if requireValue {
				return "", fmt.Errorf("header %s needs a value", name)
			}
			if v, ok := old[name]; ok {
				next[name] = v
			}
			continue
		}
		next[name] = val
	}
	b, err := json.Marshal(next)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func queryParam(raw, key string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

type oauthWait struct {
	ch     chan *mcpauth.AuthorizationResult
	issuer string
}

func tokenFromJSON(raw string) *oauth2.Token {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var t oauth2.Token
	if json.Unmarshal([]byte(raw), &t) != nil || t.AccessToken == "" {
		return nil
	}
	return &t
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type persistSrc struct {
	inner oauth2.TokenSource
	save  func(*oauth2.Token)
}

func (p persistSrc) Token() (*oauth2.Token, error) {
	t, err := p.inner.Token()
	if err == nil && t != nil && p.save != nil {
		p.save(t)
	}
	return t, err
}
