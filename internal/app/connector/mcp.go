package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/catalog"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/toolsgen"
)

func (s *Service) refreshTools(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	if c.Transport == TransportBuiltin {
		return s.refreshBuiltin(ctx, row, c)
	}
	sess, err := s.MCPSession(ctx, row, c)
	if err != nil {
		return err
	}
	tools, err := mcpx.ListAll(ctx, sess)
	if err != nil && errors.Is(err, mcpx.ErrSessionGone) {
		s.DropMCP(row.ID)
		sess, err = s.MCPSession(ctx, row, c)
		if err == nil {
			tools, err = mcpx.ListAll(ctx, sess)
		}
	}
	if err != nil {
		return err
	}
	return s.storeTools(row, c, tools)
}

// storeTools caches a connector's discovered tools, marks it authorized,
// drops rules for vanished actions, and resyncs the Bot's `tools` package.
func (s *Service) storeTools(row *db.BotConnector, c *db.Connector, tools []mcpx.Tool) error {
	b, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	row.ToolsJSON = string(b)
	row.AuthStatus = StatusOK
	row.LastError = ""
	row.StatusDetail = ""
	s.db.Save(row)
	var names []string
	for _, t := range tools {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	s.host.PruneRules(row.BotID, toolsgen.Slug(c.Name), names)
	go s.PushTools(row.BotID)
	return nil
}

func (s *Service) PushTools(botID string) {
	var rows []db.BotConnector
	s.db.Where("bot_id = ?", botID).Find(&rows)
	var stubs []*v1.ToolStub
	for i := range rows {
		var c db.Connector
		if s.db.First(&c, "id = ?", rows[i].ConnectorID).Error != nil {
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
	if _, err := s.hub.Exec(ctx, botID, cmd); err != nil {
		log.Printf("sync tools bot=%s: %v", botID, err)
	}
}

// View is one attached connector with its definition, so the prompt does not
// re-query the DB for each.
type View struct {
	Link db.BotConnector
	Conn db.Connector
}

// Views are a Bot's attached connectors with their definitions.
func (s *Service) Views(botID string) []View {
	var out []View
	var links []db.BotConnector
	s.db.Where("bot_id = ?", botID).Find(&links)
	for i := range links {
		var c db.Connector
		if s.db.First(&c, "id = ?", links[i].ConnectorID).Error != nil {
			continue
		}
		out = append(out, View{Link: links[i], Conn: c})
	}
	return out
}

// Prompt is the session-tier system-prompt note listing what is attached and
// how to reach it from Python, with each authorized connector's own notes.
func (s *Service) Prompt(views []View) string {
	var b strings.Builder
	b.WriteString("Attached on this Bot. Use exec_python and `import tools.<slug>`. They are not chat tools.\n")
	if len(views) == 0 {
		b.WriteString("None attached. Do not invent MCP servers, packages, or credentials.\n")
		return b.String()
	}
	for _, v := range views {
		slug := toolsgen.Slug(v.Conn.Name)
		switch v.Link.AuthStatus {
		case StatusNeedsAuth:
			if v.Conn.Transport == TransportBuiltin {
				fmt.Fprintf(&b, "- `%s` (%s) — not set up yet; the human fills in its settings on the Connectors tab.\n", slug, v.Conn.Name)
				continue
			}
			fmt.Fprintf(&b, "- `%s` (%s) — needs Authorize on the Connectors tab.\n", slug, v.Conn.Name)
		case StatusInit:
			fmt.Fprintf(&b, "- `%s` (%s) — still starting up; its tools are not ready yet.\n", slug, v.Conn.Name)
		case StatusErr:
			err := v.Link.LastError
			if len(err) > 180 {
				err = err[:180]
			}
			fmt.Fprintf(&b, "- `%s` (%s) — refresh failed: %s\n", slug, v.Conn.Name, err)
		default:
			var tools []mcpx.Tool
			_ = json.Unmarshal([]byte(v.Link.ToolsJSON), &tools)
			var names []string
			for _, t := range tools {
				if t.Name != "" {
					names = append(names, t.Name)
				}
			}
			if len(names) == 0 {
				fmt.Fprintf(&b, "- `%s` (%s) — attached; Refresh on the Connectors tab if import tools finds nothing.\n", slug, v.Conn.Name)
			} else {
				if len(names) > 12 {
					names = append(names[:12], "…")
				}
				fmt.Fprintf(&b, "- `import tools.%s` — %s\n", slug, strings.Join(names, ", "))
			}
			if v.Link.AuthStatus != StatusOK {
				continue
			}
			prompt := config.ExpandVars(v.Conn.Prompt, s.connectorVars())
			// A built-in's usage notes live in code, so they stay current on
			// copies attached before they changed.
			if bc, ok := builtinOf(&v.Conn); ok {
				if p := strings.TrimSpace(bc.Descriptor().Prompt); p != "" {
					prompt = strings.TrimSpace(prompt + "\n" + strings.ReplaceAll(p, "{slug}", slug))
				}
			}
			for _, line := range strings.Split(strings.TrimSpace(prompt), "\n") {
				if line = strings.TrimRight(line, "\r"); line != "" {
					fmt.Fprintf(&b, "  %s\n", line)
				}
			}
		}
	}
	return b.String()
}

func (s *Service) MCPSession(ctx context.Context, row *db.BotConnector, c *db.Connector) (*mcpx.Session, error) {
	unlock := s.lockSession(row.ID)
	defer unlock()
	s.mu.Lock()
	if live := s.mcp[row.ID]; live != nil {
		s.mu.Unlock()
		return live, nil
	}
	s.mu.Unlock()
	c = s.ResolveConnector(c)
	var h mcpauth.OAuthHandler
	if c.Auth == AuthOAuth {
		oh, err := s.oauthHandler(c, row, s.redirectURL(), nil)
		if err != nil {
			return nil, err
		}
		h = oh
	}
	d := s.dial(c, h)
	if c.Transport == TransportSTDIO {
		if err := s.ensureStdio(ctx, row, c); err != nil {
			return nil, err
		}
		d = mcpx.Dial{Transport: &bridgeTransport{s: s, row: row}}
	}
	sess, err := mcpx.Connect(ctx, d)
	if err != nil {
		return nil, wrapOAuth(err)
	}
	s.mu.Lock()
	if old := s.mcp[row.ID]; old != nil {
		_ = old.Close()
	}
	s.mcp[row.ID] = sess
	s.mu.Unlock()
	return sess, nil
}

// lockSession serializes creating one MCP session per attachment so concurrent
// callers do not interleave initialize over the same sidecar tunnel.
func (s *Service) lockSession(id string) func() {
	v, _ := s.locks.LoadOrStore("mcpsess:"+id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// CloseAll closes every live MCP session (Shutdown). STDIO sidecars are left
// running: their bridges reconnect when the control plane comes back.
func (s *Service) CloseAll() {
	s.mu.Lock()
	for id, sess := range s.mcp {
		go sess.Close()
		delete(s.mcp, id)
	}
	s.mu.Unlock()
}

func (s *Service) DropMCP(id string) {
	// Close the tunnel first: that unblocks the MCP read loop, so closing the
	// session below cannot wait on a read that will never return.
	if b := s.lookupBridge(id); b != nil {
		s.unregisterBridge(id, b)
		b.close(errors.New("session dropped"))
	}
	s.mu.Lock()
	sess := s.mcp[id]
	delete(s.mcp, id)
	s.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
}

func (s *Service) dial(c *db.Connector, h mcpauth.OAuthHandler) mcpx.Dial {
	hdr, _ := mcpx.HeadersFromJSON(c.HeadersJSON)
	return mcpx.Dial{URL: c.HTTPURL, Headers: hdr, OAuth: h}
}

func (s *Service) Init() {
	if s.db == nil {
		return
	}
	s.db.Model(&db.Connector{}).Where("kind = '' OR kind IS NULL").Update("kind", catalog.KindLibrary)
	var links []db.BotConnector
	s.db.Find(&links)
	for i := range links {
		var c db.Connector
		if s.db.First(&c, "id = ?", links[i].ConnectorID).Error != nil {
			continue
		}
		if c.Kind == catalog.KindCustom && c.BotID != "" {
			continue
		}
		clone := cloneLibrary(&c, links[i].BotID)
		if err := s.db.Create(&clone).Error; err != nil {
			log.Printf("connector backfill bot=%s: %v", links[i].BotID, err)
			continue
		}
		links[i].ConnectorID = clone.ID
		s.db.Save(&links[i])
	}
	if s.store != nil {
		if err := catalog.Seed(s.db); err != nil {
			log.Printf("connector library seed: %v", err)
		}
		if err := catalog.SeedSkills(s.cfg().DataDir); err != nil {
			log.Printf("skill library seed: %v", err)
		}
	}
}

// resumeConnectors reconciles attachment state after a restart. Initialization
// jobs live only in memory, so a row left "initializing" would otherwise be
// stuck forever; re-drive it so it converges to authorized or error. Transient
// progress details from before the restart are cleared so the UI cannot show a
// stale "Starting MCP server…".
func (s *Service) Resume() {
	if s.db == nil {
		return
	}
	var rows []db.BotConnector
	s.db.Find(&rows)
	for i := range rows {
		row := rows[i]
		if row.StatusDetail != "" {
			s.db.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("status_detail", "")
		}
		if row.AuthStatus == StatusInit {
			s.startRefresh(row.ID)
		}
	}
}

func initDetail(transport string) string {
	if transport == TransportBuiltin {
		return "Signing in…"
	}
	if transport == TransportSTDIO {
		return "Starting MCP server — the first run may download packages…"
	}
	return "Connecting…"
}

// applyInit marks a connector as initializing and clears any prior error so the
// UI can show progress while the async job runs.
func (s *Service) applyInit(row *db.BotConnector, transport string) {
	row.AuthStatus = StatusInit
	row.StatusDetail = initDetail(transport)
	row.LastError = ""
	if s.db != nil {
		s.db.Save(row)
	}
}

// startRefresh runs connect + tool discovery for one attachment in the
// background. Callers set the initializing status first.
func (s *Service) startRefresh(botConnectorID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), connectorInitWait)
		defer cancel()
		var row db.BotConnector
		if err := s.db.First(&row, "id = ?", botConnectorID).Error; err != nil {
			return
		}
		var c db.Connector
		if err := s.db.First(&c, "id = ?", row.ConnectorID).Error; err != nil {
			return
		}
		if err := s.refreshTools(ctx, &row, &c); err != nil {
			row.AuthStatus = StatusErr
			row.LastError = err.Error()
			row.StatusDetail = ""
			s.db.Save(&row)
		}
	}()
}

// restartConnector re-drives a Bot's copy after an edit: a STDIO sidecar is
// recycled, and a builtin re-checks its config.
func (s *Service) restartConnector(c *db.Connector) {
	if c == nil || c.Kind != catalog.KindCustom || c.BotID == "" {
		return
	}
	var row db.BotConnector
	if s.db.Where("connector_id = ? AND bot_id = ?", c.ID, c.BotID).Limit(1).Find(&row).Error != nil || row.ID == "" {
		return
	}
	s.DropStdio(&row)
	if c.Transport != TransportSTDIO && c.Transport != TransportBuiltin {
		return
	}
	s.applyInit(&row, c.Transport)
	s.startRefresh(row.ID)
}
