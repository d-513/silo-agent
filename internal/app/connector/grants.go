package connector

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"connectrpc.com/connect"

	"log"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

// A connector a Bot uses is a Workspace's connection (a custom row with
// space_id) granted to it (a bot_connectors row). The connection holds what
// is the same for every Bot: the definition, a built-in's config, the OAuth
// sign-in. The grant holds what is the Bot's own: its MCP session and STDIO
// sidecar (never shared, so one process never serves two boxes), the tools
// its session listed, and their status.

// connection loads a Workspace connection by id.
func (s *Service) connection(id string) (*db.Connector, error) {
	var c db.Connector
	res := s.db.Where("id = ? AND kind = ? AND space_id <> ''", id, catalog.KindCustom).Limit(1).Find(&c)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	return &c, nil
}

// credentialRef is a value that is only a reference (${NAME} or
// ${secrets.NAME}): no person's credential is typed into it.
var credentialRef = regexp.MustCompile(`^\$\{[A-Za-z0-9_.]+\}$`)

// typedCredential reports whether a person typed a credential into the
// connection: a header or environment value that is not a reference.
func typedCredential(c *db.Connector) bool {
	hdr, _ := mcpx.HeadersFromJSON(c.HeadersJSON)
	for _, v := range hdr {
		if v != "" && !credentialRef.MatchString(v) {
			return true
		}
	}
	for _, e := range parseEnv(c.EnvJSON) {
		if e.Value != "" && !credentialRef.MatchString(e.Value) {
			return true
		}
	}
	return false
}

// createConnection adds a connection to a Workspace, made by userID. Its name
// is made unique in the Workspace: it is the `tools.<slug>` of every Bot it
// is granted to.
func (s *Service) createConnection(spaceID, userID string, c db.Connector) (*db.Connector, error) {
	c.Name = s.uniqueName(spaceID, c.Name, c.ID)
	if security.Reserved(toolsgen.Slug(c.Name)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("that name is reserved"))
	}
	c.Kind, c.SpaceID, c.BotID, c.CreatedBy = catalog.KindCustom, spaceID, "", userID
	// A library preset's own values are the operator's, not a person's.
	if typedCredential(&c) && !s.operatorValues(&c) {
		c.LentBy = userID
	}
	if c.ID == "" {
		c.ID = ids.New()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	if err := s.db.Create(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// operatorValues reports whether c's header and environment values are its
// library preset's as they stand.
func (s *Service) operatorValues(c *db.Connector) bool {
	if c.SourceID == "" {
		return false
	}
	var lib db.Connector
	if s.db.Where("id = ? AND kind = ?", c.SourceID, catalog.KindLibrary).Limit(1).Find(&lib); lib.ID == "" {
		return false
	}
	return lib.HeadersJSON == c.HeadersJSON && lib.EnvJSON == c.EnvJSON
}

// grant gives connection c to a Bot of its Workspace and starts the Bot's
// session on it. A grant that exists is returned as it is.
func (s *Service) grant(botID string, c *db.Connector) (*db.BotConnector, error) {
	var b db.Bot
	if s.db.Where("id = ?", botID).Limit(1).Find(&b); b.ID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New(access.NoBot))
	}
	if b.SpaceID == "" || b.SpaceID != c.SpaceID {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the Bot and the connector are in different workspaces"))
	}
	if err := access.CheckLend(s.db, lend(c), botID); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if s.db.Where("bot_id = ? AND connector_id = ?", botID, c.ID).Limit(1).Find(&row); row.ID != "" {
		return &row, nil
	}
	row = db.BotConnector{ID: ids.New(), BotID: botID, ConnectorID: c.ID, CreatedAt: time.Now()}
	waiting := c.Auth == AuthOAuth && !s.signedIn(c)
	if waiting {
		row.AuthStatus = StatusNeedsAuth
	} else {
		row.AuthStatus, row.StatusDetail = StatusInit, initDetail(c.Transport)
	}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("already granted"))
	}
	if !waiting {
		s.startRefresh(row.ID)
	}
	return &row, nil
}

// revoke takes a connection away from one Bot: its session and sidecar, its
// rules for the connection, and the tools in its machine. The connection and
// every other Bot's grant stay.
func (s *Service) revoke(ctx context.Context, row *db.BotConnector) {
	s.DropMCP(row.ID)
	s.DropStdio(row)
	var c db.Connector
	stdio := false
	if s.db.Where("id = ?", row.ConnectorID).Limit(1).Find(&c); c.ID != "" {
		stdio = c.Transport == TransportSTDIO
		s.host.PruneRules(row.BotID, toolsgen.Slug(c.Name), nil)
	}
	s.db.Delete(row)
	if stdio && s.ClientNetDrop != nil {
		s.ClientNetDrop(ctx, row.ID)
	}
	go s.PushTools(row.BotID)
}

// grants are a connection's grants.
func (s *Service) grants(connectorID string) []db.BotConnector {
	var rows []db.BotConnector
	s.db.Where("connector_id = ?", connectorID).Order("created_at").Find(&rows)
	return rows
}

// refreshGrants re-drives every Bot's session on a connection: after it is
// signed in or edited.
func (s *Service) refreshGrants(connectorID string) {
	var c db.Connector
	if s.db.Where("id = ?", connectorID).Limit(1).Find(&c); c.ID == "" {
		return
	}
	for _, row := range s.grants(connectorID) {
		row := row
		s.DropMCP(row.ID)
		s.DropStdio(&row)
		if c.Auth == AuthOAuth && !s.signedIn(&c) {
			s.db.Model(&row).Updates(map[string]any{"auth_status": StatusNeedsAuth, "status_detail": "", "last_error": ""})
			continue
		}
		s.applyInit(&row, c.Transport)
		s.startRefresh(row.ID)
	}
}

// deleteConnection revokes a connection from every Bot and deletes it.
func (s *Service) deleteConnection(ctx context.Context, c *db.Connector) {
	for _, row := range s.grants(c.ID) {
		row := row
		s.revoke(ctx, &row)
	}
	s.db.Delete(c)
	s.tokens.Delete(c.ID)
}

// DropSpace deletes a Workspace's connections, for a Workspace that is
// going (its Bots, and so its grants, are gone already).
func (s *Service) DropSpace(spaceID string) {
	var rows []db.Connector
	s.db.Where("space_id = ? AND kind = ?", spaceID, catalog.KindCustom).Find(&rows)
	for i := range rows {
		s.deleteConnection(context.Background(), &rows[i])
	}
}

// canEdit reports whether the caller may change or delete a connection: who
// added it, or an admin of its Workspace. A manager of a Bot it is granted
// to is not enough: the change reaches every Bot that uses it.
func (s *Service) canEdit(ctx context.Context, c *db.Connector) error {
	_, role, err := access.Space(ctx, s.db, c.SpaceID, access.Build)
	if err != nil {
		return err
	}
	if c.CreatedBy == access.User(ctx).ID || access.Rank(role) >= access.Admin {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("only whoever added this connector, or an admin of the workspace, can change it"))
}

// spaceProto is a Workspace connection as the caller sees it.
func (s *Service) spaceProto(ctx context.Context, c *db.Connector) *v1.Connector {
	u := access.User(ctx)
	out := protoConnector(c, true)
	out.SpaceId, out.Shared, out.SignedIn = c.SpaceID, c.Shared, s.signedIn(c)
	out.CreatedBy, out.LentBy = s.email(c.CreatedBy), s.email(c.LentBy)
	out.CanEdit = u != nil && s.canEdit(ctx, c) == nil
	for _, row := range s.grants(c.ID) {
		if u != nil && access.BotRole(s.db, row.BotID, u.ID) != "" {
			out.UsedBy = append(out.UsedBy, row.BotID)
		} else {
			out.UsedByHidden++
		}
	}
	return out
}

func (s *Service) email(userID string) string {
	if userID == "" {
		return ""
	}
	var u db.User
	s.db.Select("email").Where("id = ?", userID).Limit(1).Find(&u)
	return u.Email
}

// grantProto is a grant with the other Bots its connection is granted to.
func (s *Service) grantProto(ctx context.Context, row *db.BotConnector, c *db.Connector) *v1.BotConnector {
	out := protoBotConnector(row, c, true)
	out.Connector = s.spaceProto(ctx, c)
	u := access.User(ctx)
	for _, other := range s.grants(c.ID) {
		if other.BotID == row.BotID {
			continue
		}
		var b db.Bot
		if u != nil && access.BotRole(s.db, other.BotID, u.ID) != "" {
			s.db.Select("name").Where("id = ?", other.BotID).Limit(1).Find(&b)
			out.AlsoIn = append(out.AlsoIn, b.Name)
		} else {
			out.AlsoInHidden++
		}
	}
	return out
}

func (s *Service) ListSpaceConnectors(ctx context.Context, req *connect.Request[v1.ListSpaceConnectorsRequest]) (*connect.Response[v1.ListConnectorsResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Build)
	if err != nil {
		return nil, err
	}
	var rows []db.Connector
	s.db.Where("space_id = ? AND kind = ?", sp.ID, catalog.KindCustom).Order("name").Find(&rows)
	out := &v1.ListConnectorsResponse{}
	for i := range rows {
		out.Connectors = append(out.Connectors, s.spaceProto(ctx, &rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) GrantConnector(ctx context.Context, req *connect.Request[v1.GrantConnectorRequest]) (*connect.Response[v1.BotConnector], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	c, err := s.connection(req.Msg.GetConnectorId())
	if err != nil {
		return nil, err
	}
	row, err := s.grant(req.Msg.GetBotId(), c)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.grantProto(ctx, row, c)), nil
}

func (s *Service) RevokeConnector(ctx context.Context, req *connect.Request[v1.RevokeConnectorRequest]) (*connect.Response[v1.RevokeConnectorResponse], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Administer); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if s.db.Where("bot_id = ? AND connector_id = ?", req.Msg.GetBotId(), req.Msg.GetConnectorId()).Limit(1).Find(&row); row.ID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the Bot does not use that connector"))
	}
	s.revoke(ctx, &row)
	return connect.NewResponse(&v1.RevokeConnectorResponse{}), nil
}

// lend is a connection as the lending checks see it.
func lend(c *db.Connector) access.Lend {
	return access.Lend{Kind: "connector", ID: c.ID, Name: c.Name, LentBy: c.LentBy, Shared: c.Shared}
}

// Lent is every connection userID lent in the Workspace.
func (s *Service) Lent(spaceID, userID string) []access.Lend {
	var rows []db.Connector
	s.db.Where("space_id = ? AND kind = ? AND lent_by = ?", spaceID, catalog.KindCustom, userID).Order("name").Find(&rows)
	out := make([]access.Lend, 0, len(rows))
	for i := range rows {
		out = append(out, lend(&rows[i]))
	}
	return out
}

// Lending loads a Workspace connection for the lending checks.
func (s *Service) Lending(id string) (access.Lend, string, bool) {
	c, err := s.connection(id)
	if err != nil {
		return access.Lend{}, "", false
	}
	return lend(c), c.SpaceID, true
}

// Grantees are the Bots a connection is granted to.
func (s *Service) Grantees(connectorID string) []string {
	var out []string
	s.db.Model(&db.BotConnector{}).Where("connector_id = ?", connectorID).Order("created_at").Pluck("bot_id", &out)
	return out
}

// SetShared records the lender's say-so on a connection.
func (s *Service) SetShared(id string, shared bool) {
	s.db.Model(&db.Connector{}).Where("id = ?", id).Update("shared", shared)
}

// Unlend takes the lender's identity out of a connection: a personal one is
// revoked everywhere and deleted; a shared one is signed out (its token and
// typed credentials cleared) and stays for someone else to sign in.
func (s *Service) Unlend(ctx context.Context, l access.Lend, why string) {
	c, err := s.connection(l.ID)
	if err != nil {
		return
	}
	if l.Personal() {
		s.deleteConnection(ctx, c)
		return
	}
	s.forgetToken(c.ID)
	hdr, _ := mcpx.HeadersFromJSON(c.HeadersJSON)
	for k, v := range hdr {
		if v != "" && !credentialRef.MatchString(v) {
			hdr[k] = ""
		}
	}
	env := parseEnv(c.EnvJSON)
	for i := range env {
		if env[i].Value != "" && !credentialRef.MatchString(env[i].Value) {
			env[i].Value = ""
		}
	}
	hb, _ := json.Marshal(hdr)
	eb, _ := json.Marshal(env)
	c.HeadersJSON, c.EnvJSON = string(hb), string(eb)
	if len(env) == 0 {
		c.EnvJSON = ""
	}
	s.db.Model(&db.Connector{}).Where("id = ?", c.ID).Updates(map[string]any{
		"headers_json": c.HeadersJSON, "env_json": c.EnvJSON, "secrets_json": "", "lent_by": "",
	})
	s.refreshGrants(c.ID)
	s.db.Model(&db.BotConnector{}).Where("connector_id = ?", c.ID).Update("status_detail", why)
}

// CheckNewBot says whether a new Bot that the people will use may get these
// connections: each of its Workspace, none someone else's personal sign-in.
func (s *Service) CheckNewBot(spaceID string, ids, people []string) error {
	for _, id := range ids {
		c, err := s.connection(id)
		if err != nil {
			return err
		}
		if c.SpaceID != spaceID {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("the Bot and the connector are in different workspaces"))
		}
		if err := access.CheckLend(s.db, lend(c), "", people...); err != nil {
			return err
		}
	}
	return nil
}

// GrantNew gives a new Bot the connections ticked for it.
func (s *Service) GrantNew(botID string, ids []string) {
	for _, id := range ids {
		if c, err := s.connection(id); err == nil {
			if _, err := s.grant(botID, c); err != nil {
				log.Printf("grant %s to new bot %s: %v", id, botID, err)
			}
		}
	}
}
