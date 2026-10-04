package app

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

func (a *App) refreshTools(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	if c.Transport == transportBuiltin {
		return a.refreshBuiltin(ctx, row, c)
	}
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
	return a.storeTools(row, c, tools)
}

// storeTools caches a connector's discovered tools, marks it authorized,
// drops rules for vanished actions, and resyncs the Bot's `tools` package.
func (a *App) storeTools(row *db.BotConnector, c *db.Connector, tools []mcpx.Tool) error {
	b, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	row.ToolsJSON = string(b)
	row.AuthStatus = statusOK
	row.LastError = ""
	row.StatusDetail = ""
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

func (a *App) connectorSections(pc promptContext) []promptSection {
	var b strings.Builder
	b.WriteString("Attached on this Bot. Use exec_python and `import tools.<slug>`. They are not chat tools.\n")
	if len(pc.connectors) == 0 {
		b.WriteString("None attached. Do not invent MCP servers, packages, or credentials.\n")
		return []promptSection{{title: "Connectors", body: b.String()}}
	}
	for _, v := range pc.connectors {
		slug := toolsgen.Slug(v.conn.Name)
		switch v.link.AuthStatus {
		case statusNeedsAuth:
			if v.conn.Transport == transportBuiltin {
				fmt.Fprintf(&b, "- `%s` (%s) — not set up yet; the human fills in its settings on the Connectors tab.\n", slug, v.conn.Name)
				continue
			}
			fmt.Fprintf(&b, "- `%s` (%s) — needs Authorize on the Connectors tab.\n", slug, v.conn.Name)
		case statusInit:
			fmt.Fprintf(&b, "- `%s` (%s) — still starting up; its tools are not ready yet.\n", slug, v.conn.Name)
		case statusErr:
			err := v.link.LastError
			if len(err) > 180 {
				err = err[:180]
			}
			fmt.Fprintf(&b, "- `%s` (%s) — refresh failed: %s\n", slug, v.conn.Name, err)
		default:
			var tools []mcpx.Tool
			_ = json.Unmarshal([]byte(v.link.ToolsJSON), &tools)
			var names []string
			for _, t := range tools {
				if t.Name != "" {
					names = append(names, t.Name)
				}
			}
			if len(names) == 0 {
				fmt.Fprintf(&b, "- `%s` (%s) — attached; Refresh on the Connectors tab if import tools finds nothing.\n", slug, v.conn.Name)
			} else {
				if len(names) > 12 {
					names = append(names[:12], "…")
				}
				fmt.Fprintf(&b, "- `import tools.%s` — %s\n", slug, strings.Join(names, ", "))
			}
			if v.link.AuthStatus != statusOK {
				continue
			}
			prompt := config.ExpandVars(v.conn.Prompt, a.connectorVars())
			// A built-in's usage notes live in code, so they stay current on
			// copies attached before they changed.
			if bc, ok := builtinOf(&v.conn); ok {
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
	return []promptSection{{title: "Connectors", body: b.String()}}
}

func (a *App) mcpSession(ctx context.Context, row *db.BotConnector, c *db.Connector) (*mcpx.Session, error) {
	unlock := a.lockSession(row.ID)
	defer unlock()
	a.mu.Lock()
	if s := a.mcp[row.ID]; s != nil {
		a.mu.Unlock()
		return s, nil
	}
	a.mu.Unlock()
	c = a.resolveConnector(c)
	var h mcpauth.OAuthHandler
	if c.Auth == authOAuth {
		oh, err := a.oauthHandler(c, row, a.redirectURL(), nil)
		if err != nil {
			return nil, err
		}
		h = oh
	}
	d := a.dial(c, h)
	if c.Transport == transportSTDIO {
		if a.bridgeTransportFn != nil {
			t, err := a.bridgeTransportFn(ctx, row, c)
			if err != nil {
				return nil, err
			}
			d = mcpx.Dial{Transport: t}
		} else {
			if err := a.ensureStdio(ctx, row, c); err != nil {
				return nil, err
			}
			d = mcpx.Dial{Transport: &bridgeTransport{a: a, row: row}}
		}
	}
	sess, err := mcpx.Connect(ctx, d)
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

// lockSession serializes creating one MCP session per attachment so concurrent
// callers do not interleave initialize over the same sidecar tunnel.
func (a *App) lockSession(id string) func() {
	v, _ := a.lifecycle.LoadOrStore("mcpsess:"+id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (a *App) dropMCP(id string) {
	// Close the tunnel first: that unblocks the MCP read loop, so closing the
	// session below cannot wait on a read that will never return.
	if b := a.lookupBridge(id); b != nil {
		a.unregisterBridge(id, b)
		b.close(errors.New("session dropped"))
	}
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

// resumeConnectors reconciles attachment state after a restart. Initialization
// jobs live only in memory, so a row left "initializing" would otherwise be
// stuck forever; re-drive it so it converges to authorized or error. Transient
// progress details from before the restart are cleared so the UI cannot show a
// stale "Starting MCP server…".
func (a *App) resumeConnectors() {
	if a.DB == nil {
		return
	}
	var rows []db.BotConnector
	a.DB.Find(&rows)
	for i := range rows {
		row := rows[i]
		if row.StatusDetail != "" {
			a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("status_detail", "")
		}
		if row.AuthStatus == statusInit {
			a.startRefresh(row.ID)
		}
	}
}

func initDetail(transport string) string {
	if transport == transportBuiltin {
		return "Signing in…"
	}
	if transport == transportSTDIO {
		return "Starting MCP server — the first run may download packages…"
	}
	return "Connecting…"
}

// applyInit marks a connector as initializing and clears any prior error so the
// UI can show progress while the async job runs.
func (a *App) applyInit(row *db.BotConnector, transport string) {
	row.AuthStatus = statusInit
	row.StatusDetail = initDetail(transport)
	row.LastError = ""
	if a.DB != nil {
		a.DB.Save(row)
	}
}

// startRefresh runs connect + tool discovery for one attachment in the
// background. Callers set the initializing status first.
func (a *App) startRefresh(botConnectorID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), connectorInitWait)
		defer cancel()
		var row db.BotConnector
		if err := a.DB.First(&row, "id = ?", botConnectorID).Error; err != nil {
			return
		}
		var c db.Connector
		if err := a.DB.First(&c, "id = ?", row.ConnectorID).Error; err != nil {
			return
		}
		if err := a.refreshTools(ctx, &row, &c); err != nil {
			row.AuthStatus = statusErr
			row.LastError = err.Error()
			row.StatusDetail = ""
			a.DB.Save(&row)
		}
	}()
}

// restartConnector re-drives a Bot's copy after an edit: a STDIO sidecar is
// recycled, and a builtin re-checks its config.
func (a *App) restartConnector(c *db.Connector) {
	if c == nil || c.Kind != catalog.KindCustom || c.BotID == "" {
		return
	}
	var row db.BotConnector
	if a.DB.Where("connector_id = ? AND bot_id = ?", c.ID, c.BotID).Limit(1).Find(&row).Error != nil || row.ID == "" {
		return
	}
	a.dropStdio(&row)
	if c.Transport != transportSTDIO && c.Transport != transportBuiltin {
		return
	}
	a.applyInit(&row, c.Transport)
	a.startRefresh(row.ID)
}
