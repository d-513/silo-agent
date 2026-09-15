package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpbridge"
)

// stdioInitWait bounds how long the CP waits for a sidecar to dial in and the
// MCP session to come up. npm pulls on a cold container can be slow.
const stdioInitWait = 10 * time.Minute

func (a *App) lockStdio(id string) func() {
	v, _ := a.lifecycle.LoadOrStore("mcp:"+id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// ensureStdio makes sure the sidecar container for row is running and its
// bridge has connected to the CP. It is safe to call repeatedly.
func (a *App) ensureStdio(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	unlock := a.lockStdio(row.ID)
	defer unlock()
	if a.Docker == nil {
		return errors.New("docker unavailable")
	}
	if strings.TrimSpace(a.cfg().DataDir) == "" {
		return errors.New("data_dir required for stdio")
	}
	if b := a.lookupBridge(row.ID); b != nil {
		select {
		case <-b.done:
			a.unregisterBridge(row.ID, b)
		default:
			return nil
		}
	}
	if row.ContainerID != "" {
		st, err := a.Docker.Inspect(ctx, row.ContainerID)
		switch {
		case err == nil && !st.Running:
			if err := a.Docker.Start(ctx, st.ID); err != nil {
				a.dropStdio(row)
			} else {
				return a.awaitBridge(ctx, row)
			}
		case err == nil:
			return a.awaitBridge(ctx, row)
		case dockerx.IsNotFound(err):
			a.dropStdio(row)
		default:
			return err
		}
	} else {
		a.Docker.DropStdio(ctx, row.ID, "")
	}
	if err := a.startStdio(ctx, row, c); err != nil {
		return err
	}
	return a.awaitBridge(ctx, row)
}

func (a *App) awaitBridge(ctx context.Context, row *db.BotConnector) error {
	if _, err := a.waitBridge(ctx, row.ID, stdioInitWait); err != nil {
		a.dropStdio(row)
		return fmt.Errorf("stdio sidecar not ready: %w", err)
	}
	return nil
}

func (a *App) startStdio(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	env, err := a.stdioEnv(row.BotID, c)
	if err != nil {
		return err
	}
	image := strings.TrimSpace(c.StdioImage)
	if image == "" {
		image = a.cfg().MCPStdioImage
	}
	token := ids.New() + ids.New()
	env = append(env,
		"SILO_MCP_CMD="+c.StdioCommand,
		"SILO_MCP_ARGS="+mustJSON(mcpbridge.ParseArgs(c.StdioArgsJSON)),
		"SILO_CP_URL="+a.cfg().CPURL,
		"SILO_BRIDGE_TOKEN="+token,
	)
	a.Mask(row.BotID).Add(token)
	cid, err := a.Docker.CreateStdio(ctx, dockerx.StdioSpec{ID: row.ID, Image: image, Env: env})
	if err != nil {
		return err
	}
	if err := a.Docker.Start(ctx, cid); err != nil {
		a.Docker.DropStdio(ctx, row.ID, cid)
		return err
	}
	// Persist the container and token hash only after a successful create.
	hash := ids.Hash(token)
	a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Updates(map[string]any{
		"container_id":      cid,
		"bridge_token_hash": hash,
	})
	row.ContainerID = cid
	row.BridgeTokenHash = hash
	return nil
}

// dropStdio tears down a sidecar for good: closes the session and tunnel,
// removes the container (by ID and by name), wipes its data dir, and clears
// the remembered container/token. Idempotent.
func (a *App) dropStdio(row *db.BotConnector) {
	if row == nil {
		return
	}
	a.dropMCP(row.ID)
	if b := a.lookupBridge(row.ID); b != nil {
		a.unregisterBridge(row.ID, b)
		b.close(errors.New("sidecar dropped"))
	}
	if a.Docker != nil {
		a.Docker.DropStdio(context.Background(), row.ID, row.ContainerID)
	}
	if dir := a.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(filepath.Join(dir, "mcp", row.ID))
	}
	if row.ContainerID != "" || row.BridgeTokenHash != "" {
		row.ContainerID = ""
		row.BridgeTokenHash = ""
		if a.DB != nil {
			a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Updates(map[string]any{
				"container_id":      "",
				"bridge_token_hash": "",
			})
		}
	}
}

func (a *App) dropBotStdio(botID string) {
	if a.DB == nil {
		return
	}
	var rows []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Find(&rows)
	for i := range rows {
		a.dropStdio(&rows[i])
	}
}

func (a *App) dropAllStdio() {
	if a.DB == nil {
		return
	}
	var rows []db.BotConnector
	a.DB.Find(&rows)
	for i := range rows {
		a.dropStdio(&rows[i])
	}
}

// reconcileStdio reclaims sidecar containers whose BotConnector row is gone
// (crash mid-delete, DB reset, older-version leftovers). Runs at startup.
func (a *App) reconcileStdio() {
	if a.DB == nil || a.Docker == nil {
		return
	}
	containers, err := a.Docker.ListStdio(context.Background())
	if err != nil {
		return
	}
	if len(containers) == 0 {
		return
	}
	valid := map[string]bool{}
	var rows []db.BotConnector
	a.DB.Find(&rows)
	for i := range rows {
		valid[rows[i].ID] = true
	}
	for _, c := range containers {
		id := c.ConnectorID
		if id == "" && strings.HasPrefix(c.Name, "silo-mcp-") {
			id = strings.TrimPrefix(c.Name, "silo-mcp-")
		}
		if id != "" && valid[id] {
			continue
		}
		a.Docker.DropStdio(context.Background(), id, c.ID)
		if dir := a.cfg().DataDir; dir != "" && id != "" {
			_ = os.RemoveAll(filepath.Join(dir, "mcp", id))
		}
		log.Printf("reclaimed orphaned stdio sidecar %s (%s)", c.ID, c.Name)
	}
}

func (a *App) stdioEnv(botID string, c *db.Connector) ([]string, error) {
	entries := parseEnv(c.EnvJSON)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		val := e.Value
		if e.Secret != "" {
			var s db.Secret
			if err := a.DB.Where("bot_id = ? AND name = ?", botID, e.Secret).Limit(1).Find(&s).Error; err != nil {
				return nil, err
			}
			if s.ID == "" {
				return nil, fmt.Errorf("secret %s not found", e.Secret)
			}
			val = s.Value
		}
		if val != "" {
			a.Mask(botID).Add(val)
		}
		out = append(out, e.Name+"="+val)
	}
	return out, nil
}
