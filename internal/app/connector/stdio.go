package connector

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

func (s *Service) lockStdio(id string) func() {
	v, _ := s.locks.LoadOrStore("mcp:"+id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// ensureStdio makes sure the sidecar container for row is running and its
// bridge has connected to the CP. It is safe to call repeatedly.
func (s *Service) ensureStdio(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	unlock := s.lockStdio(row.ID)
	defer unlock()
	if s.docker == nil {
		return errors.New("docker unavailable")
	}
	if strings.TrimSpace(s.cfg().DataDir) == "" {
		return errors.New("data_dir required for stdio")
	}
	if b := s.lookupBridge(row.ID); b != nil {
		select {
		case <-b.done:
			s.unregisterBridge(row.ID, b)
		default:
			return nil
		}
	}
	if row.ContainerID != "" {
		st, err := s.docker.Inspect(ctx, row.ContainerID)
		switch {
		case err == nil && !st.Running:
			if err := s.docker.Start(ctx, st.ID); err != nil {
				s.DropStdio(row)
			} else {
				return s.awaitBridge(ctx, row)
			}
		case err == nil:
			return s.awaitBridge(ctx, row)
		case dockerx.IsNotFound(err):
			s.DropStdio(row)
		default:
			return err
		}
	} else {
		s.docker.DropStdio(ctx, row.ID, "")
	}
	if err := s.startStdio(ctx, row, c); err != nil {
		return err
	}
	return s.awaitBridge(ctx, row)
}

func (s *Service) awaitBridge(ctx context.Context, row *db.BotConnector) error {
	if _, err := s.waitBridge(ctx, row.ID, stdioInitWait); err != nil {
		s.DropStdio(row)
		return fmt.Errorf("stdio sidecar not ready: %w", err)
	}
	return nil
}

func (s *Service) startStdio(ctx context.Context, row *db.BotConnector, c *db.Connector) error {
	env, err := s.stdioEnv(row.BotID, c)
	if err != nil {
		return err
	}
	image := strings.TrimSpace(c.StdioImage)
	if image == "" {
		image = s.cfg().MCPStdioImage
	}
	token := ids.New() + ids.New()
	env = append(env,
		"SILO_MCP_CMD="+c.StdioCommand,
		"SILO_MCP_ARGS="+mustJSON(mcpbridge.ParseArgs(c.StdioArgsJSON)),
		"SILO_CP_URL="+s.cfg().CPURL,
		"SILO_BRIDGE_TOKEN="+token,
	)
	s.mask(row.BotID).Add(token)
	cid, err := s.docker.CreateStdio(ctx, dockerx.StdioSpec{ID: row.ID, Image: image, Env: env})
	if err != nil {
		return err
	}
	if err := s.docker.Start(ctx, cid); err != nil {
		s.docker.DropStdio(ctx, row.ID, cid)
		return err
	}
	// Persist the container and token hash only after a successful create.
	hash := ids.Hash(token)
	s.db.Model(&db.BotConnector{}).Where("id = ?", row.ID).Updates(map[string]any{
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
func (s *Service) DropStdio(row *db.BotConnector) {
	if row == nil {
		return
	}
	s.DropMCP(row.ID)
	if b := s.lookupBridge(row.ID); b != nil {
		s.unregisterBridge(row.ID, b)
		b.close(errors.New("sidecar dropped"))
	}
	if s.docker != nil {
		s.docker.DropStdio(context.Background(), row.ID, row.ContainerID)
	}
	if dir := s.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(filepath.Join(dir, "mcp", row.ID))
	}
	if row.ContainerID != "" || row.BridgeTokenHash != "" {
		row.ContainerID = ""
		row.BridgeTokenHash = ""
		if s.db != nil {
			s.db.Model(&db.BotConnector{}).Where("id = ?", row.ID).Updates(map[string]any{
				"container_id":      "",
				"bridge_token_hash": "",
			})
		}
	}
}

func (s *Service) DropBotStdio(botID string) {
	if s.db == nil {
		return
	}
	var rows []db.BotConnector
	s.db.Where("bot_id = ?", botID).Find(&rows)
	for i := range rows {
		s.DropStdio(&rows[i])
	}
}

// reconcileStdio reclaims sidecar containers whose BotConnector row is gone
// (crash mid-delete, DB reset, older-version leftovers). Runs at startup.
func (s *Service) ReconcileStdio() {
	if s.db == nil || s.docker == nil {
		return
	}
	containers, err := s.docker.ListStdio(context.Background())
	if err != nil {
		return
	}
	if len(containers) == 0 {
		return
	}
	valid := map[string]bool{}
	var rows []db.BotConnector
	s.db.Find(&rows)
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
		s.docker.DropStdio(context.Background(), id, c.ID)
		if dir := s.cfg().DataDir; dir != "" && id != "" {
			_ = os.RemoveAll(filepath.Join(dir, "mcp", id))
		}
		log.Printf("reclaimed orphaned stdio sidecar %s (%s)", c.ID, c.Name)
	}
}

func (s *Service) stdioEnv(botID string, c *db.Connector) ([]string, error) {
	entries := parseEnv(c.EnvJSON)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		val := e.Value
		if e.Secret != "" {
			var sec db.Secret
			if err := s.db.Where("bot_id = ? AND name = ?", botID, e.Secret).Limit(1).Find(&sec).Error; err != nil {
				return nil, err
			}
			if sec.ID == "" {
				return nil, fmt.Errorf("secret %s not found", e.Secret)
			}
			val = sec.Value
		}
		if val != "" {
			s.mask(botID).Add(val)
		}
		out = append(out, e.Name+"="+val)
	}
	return out, nil
}
