package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/mcpbridge"
)

const stdioReadyWait = 45 * time.Second

func (a *App) lockStdio(id string) func() {
	v, _ := a.lifecycle.LoadOrStore("mcp:"+id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (a *App) stdioSock(id string) string {
	return filepath.Join(a.cfg().DataDir, "mcp", id, mcpbridge.SockFile)
}

func (a *App) dropStdio(row *db.BotConnector) {
	if row == nil {
		return
	}
	a.dropMCP(row.ID)
	if a.Docker != nil {
		a.Docker.DropStdio(context.Background(), row.ID, row.ContainerID)
	}
	if dir := a.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(filepath.Join(dir, "mcp", row.ID))
	}
	if row.ContainerID != "" {
		row.ContainerID = ""
		if a.DB != nil {
			a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("container_id", "")
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
		if rows[i].ContainerID == "" {
			continue
		}
		a.dropStdio(&rows[i])
	}
}

func (a *App) ensureStdio(ctx context.Context, row *db.BotConnector, c *db.Connector) (string, error) {
	unlock := a.lockStdio(row.ID)
	defer unlock()
	if a.Docker == nil {
		return "", errors.New("docker unavailable")
	}
	if strings.TrimSpace(a.cfg().DataDir) == "" {
		return "", errors.New("data_dir required for stdio")
	}
	sock := a.stdioSock(row.ID)
	dir := filepath.Dir(sock)
	if row.ContainerID != "" {
		st, err := a.Docker.Inspect(ctx, row.ContainerID)
		if err == nil {
			if !st.Running {
				if err := a.Docker.Start(ctx, st.ID); err != nil {
					a.Docker.DropStdio(ctx, row.ID, row.ContainerID)
					row.ContainerID = ""
					a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("container_id", "")
				} else if waitSock(ctx, sock, stdioReadyWait) == nil {
					return sock, nil
				}
			} else if waitSock(ctx, sock, stdioReadyWait) == nil {
				return sock, nil
			}
		} else if !dockerx.IsNotFound(err) {
			return "", err
		}
		a.Docker.DropStdio(ctx, row.ID, row.ContainerID)
		row.ContainerID = ""
		a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("container_id", "")
	} else {
		a.Docker.DropStdio(ctx, row.ID, "")
	}

	env, err := a.stdioEnv(row.BotID, c)
	if err != nil {
		return "", err
	}
	image := strings.TrimSpace(c.StdioImage)
	if image == "" {
		image = a.cfg().MCPStdioImage
	}
	env = append([]string{
		"SILO_MCP_CMD=" + c.StdioCommand,
		"SILO_MCP_ARGS=" + mustJSON(mcpbridge.ParseArgs(c.StdioArgsJSON)),
	}, env...)
	cid, err := a.Docker.CreateStdio(ctx, dockerx.StdioSpec{
		ID: row.ID, Image: image, Env: env, SockDir: dir,
	})
	if err != nil {
		return "", err
	}
	if err := a.Docker.Start(ctx, cid); err != nil {
		a.Docker.DropStdio(ctx, row.ID, cid)
		return "", err
	}
	row.ContainerID = cid
	a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("container_id", cid)
	if err := waitSock(ctx, sock, stdioReadyWait); err != nil {
		a.Docker.DropStdio(ctx, row.ID, cid)
		row.ContainerID = ""
		a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("container_id", "")
		return "", fmt.Errorf("stdio sidecar not ready: %w", err)
	}
	return sock, nil
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

func waitSock(ctx context.Context, path string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("socket not ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
