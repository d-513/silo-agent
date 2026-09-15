package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpbridge"
	"silo.agent/internal/mcpx"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

const stdioReadyWait = 45 * time.Second

// stdioTCPToken is the prefix CP↔bridge dial addresses carry to distinguish
// a published TCP port from a unix socket path.
const stdioTCPToken = "tcp:"

func (a *App) lockStdio(id string) func() {
	v, _ := a.lifecycle.LoadOrStore("mcp:"+id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (a *App) stdioSockDir(id string) string {
	return filepath.Join(a.cfg().DataDir, "mcp", id)
}

// stdioDial parses the ensureStdio address ("tcp:host:port|token" or a unix
// socket path) into an mcpx.Dial.
func stdioDial(addr string, h mcpauth.OAuthHandler) (mcpx.Dial, error) {
	host, token, _ := strings.Cut(addr, "|")
	if strings.HasPrefix(host, stdioTCPToken) {
		host = strings.TrimPrefix(host, stdioTCPToken)
		if host == "" {
			return mcpx.Dial{}, errors.New("stdio sidecar address missing")
		}
		return mcpx.Dial{URL: "http://" + host + "/mcp", Token: token}, nil
	}
	if host == "" {
		return mcpx.Dial{}, errors.New("stdio sidecar socket missing")
	}
	return mcpx.Dial{URL: "http://localhost/mcp", Sock: host, Token: token}, nil
}

// stdioRememberPort records (or clears, with port 0) the published port and
// bridge token for a sidecar.
func (a *App) stdioRememberPort(id string, port int, token string) {
	a.stdioMu.Lock()
	defer a.stdioMu.Unlock()
	if port == 0 {
		delete(a.stdioPorts, id)
		delete(a.stdioTokens, id)
		return
	}
	a.stdioPorts[id] = port
	a.stdioTokens[id] = token
}

// waitTCP polls the sidecar's published port until the bridge accepts
// connections.
func waitTCP(ctx context.Context, port int, d time.Duration) error {
	if port == 0 {
		return errors.New("no published port")
	}
	deadline := time.Now().Add(d)
	for {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("bridge port not ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
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
	addr, err := a.stdioTCPAddr(ctx, row, c)
	if err != nil {
		return "", err
	}
	return addr, nil
}

// stdioTCPAddr returns the CP dial address ("tcp:127.0.0.1:<port>|<token>")
// for the sidecar, creating or restarting the container when needed. Ports
// and tokens live only in CP memory: after a CP restart the container is
// replaced on first use (the token is lost, so the old box is unusable).
func (a *App) stdioTCPAddr(ctx context.Context, row *db.BotConnector, c *db.Connector) (string, error) {
	port := a.stdioPortOf(row.ID)
	drop := func() {
		a.stdioRememberPort(row.ID, 0, "")
		if row.ContainerID != "" {
			a.Docker.DropStdio(ctx, row.ID, row.ContainerID)
		} else {
			a.Docker.DropStdio(ctx, row.ID, "")
		}
		row.ContainerID = ""
		if a.DB != nil {
			a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("container_id", "")
		}
	}
	if row.ContainerID != "" {
		st, err := a.Docker.Inspect(ctx, row.ContainerID)
		switch {
		case err == nil && !st.Running:
			if err := a.Docker.Start(ctx, st.ID); err != nil {
				drop()
			} else if waitTCP(ctx, port, stdioReadyWait) == nil {
				return a.stdioAddrOf(row.ID), nil
			} else {
				drop()
			}
		case err == nil:
			if waitTCP(ctx, port, stdioReadyWait) == nil {
				return a.stdioAddrOf(row.ID), nil
			}
			drop()
		case dockerx.IsNotFound(err):
			drop()
		default:
			return "", err
		}
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
	port, perr := dockerx.FreePort()
	if perr != nil {
		return "", perr
	}
	token := ids.New() + ids.New()
	env = append([]string{
		"SILO_MCP_CMD=" + c.StdioCommand,
		"SILO_MCP_ARGS=" + mustJSON(mcpbridge.ParseArgs(c.StdioArgsJSON)),
	}, env...)
	cid, err := a.Docker.CreateStdioTCP(ctx, dockerx.StdioSpec{
		ID: row.ID, Image: image, Env: env, SockDir: a.stdioSockDir(row.ID),
	}, dockerx.StdioTCP{HostPort: port, Token: token})
	if err != nil {
		return "", err
	}
	if err := a.Docker.Start(ctx, cid); err != nil {
		a.Docker.DropStdio(ctx, row.ID, cid)
		return "", err
	}
	row.ContainerID = cid
	a.DB.Model(&db.BotConnector{}).Where("id = ?", row.ID).Update("container_id", cid)
	a.stdioRememberPort(row.ID, port, token)
	if err := waitTCP(ctx, port, stdioReadyWait); err != nil {
		a.stdioRememberPort(row.ID, 0, "")
		drop()
		return "", fmt.Errorf("stdio sidecar not ready: %w", err)
	}
	return a.stdioAddrOf(row.ID), nil
}

// stdioPortOf returns the remembered published port for a sidecar (0 = none).
func (a *App) stdioPortOf(id string) int {
	a.stdioMu.Lock()
	defer a.stdioMu.Unlock()
	return a.stdioPorts[id]
}

// stdioAddrOf returns the CP dial address for the sidecar. The bridge token
// rides along ("tcp:host:port|token") so callers can set the Authorization
// header without a second lookup.
func (a *App) stdioAddrOf(id string) string {
	a.stdioMu.Lock()
	defer a.stdioMu.Unlock()
	return stdioTCPToken + "127.0.0.1:" + strconv.Itoa(a.stdioPorts[id]) + "|" + a.stdioTokens[id]
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
