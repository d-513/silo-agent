package dockerx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"silo.agent/internal/config"
	"silo.agent/internal/ids"
)

func Name(botID string) string { return "silo-" + botID }

func StdioName(id string) string { return "silo-mcp-" + id }

var ErrNotFound = errors.New("container not found")

func IsNotFound(err error) bool {
	return err != nil && (errors.Is(err, ErrNotFound) || cerrdefs.IsNotFound(err))
}

type State struct {
	ID      string
	Running bool
	Image   string
	// StartedAt is when the container last started; zero if never.
	StartedAt time.Time
}

type Stats struct {
	CPUPercent float64
	MemUsed    int64
	MemLimit   int64
}

type Host interface {
	Inspect(ctx context.Context, id string) (State, error)
	Stats(ctx context.Context, id string) (Stats, error)
	EnvTokenHash(ctx context.Context, id string) (string, error)
	Create(ctx context.Context, botID, token string) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Drop(ctx context.Context, botID, containerID string)
	CreateStdio(ctx context.Context, spec StdioSpec) (string, error)
	DropStdio(ctx context.Context, id, containerID string)
	ListStdio(ctx context.Context) ([]StdioContainer, error)
	PrepareDriveDir(ctx context.Context, image, root, botID string) error
	RemoveDriveDir(ctx context.Context, image, root, botID string) error
	CreateDrive(ctx context.Context, spec DriveSpec) (string, error)
	DropDrive(ctx context.Context, botID, containerID string)
	ListDrives(ctx context.Context) ([]DriveContainer, error)
	HasDriveBind(ctx context.Context, id string) (bool, error)
}

type StdioSpec struct {
	ID    string
	Image string
	Env   []string
}

// StdioContainer is a discovered MCP sidecar container.
type StdioContainer struct {
	ID          string
	Name        string
	ConnectorID string
}

type Engine struct {
	cli   *client.Client
	store *config.Store
}

func New(store *config.Store) (*Engine, error) {
	cfg := store.Config()
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if cfg.DockerHost != "" {
		opts = append(opts, client.WithHost(cfg.DockerHost))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Engine{cli: cli, store: store}, nil
}

func (e *Engine) Inspect(ctx context.Context, id string) (State, error) {
	if id == "" {
		return State{}, ErrNotFound
	}
	c, err := e.cli.ContainerInspect(ctx, id)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return State{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return State{}, err
	}
	run := c.State != nil && c.State.Running
	st := State{ID: c.ID, Running: run}
	if c.Config != nil {
		st.Image = c.Config.Image
	}
	if c.State != nil {
		if t, err := time.Parse(time.RFC3339Nano, c.State.StartedAt); err == nil && t.Year() > 1 {
			st.StartedAt = t
		}
	}
	return st, nil
}

func cpuPct(s container.StatsResponse) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage - s.PreCPUStats.SystemUsage)
	n := float64(s.CPUStats.OnlineCPUs)
	if n == 0 {
		n = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpuDelta > 0 && sysDelta > 0 && n > 0 {
		return (cpuDelta / sysDelta) * n * 100
	}
	return 0
}

func (e *Engine) EnvTokenHash(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", ErrNotFound
	}
	c, err := e.cli.ContainerInspect(ctx, id)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return "", err
	}
	if c.Config == nil {
		return "", nil
	}
	for _, e := range c.Config.Env {
		if v, ok := strings.CutPrefix(e, "SILO_BOT_TOKEN="); ok {
			return ids.Hash(v), nil
		}
	}
	return "", nil
}

func memUsed(s container.StatsResponse) int64 {
	used := s.MemoryStats.Usage
	if s.MemoryStats.Stats != nil {
		if v, ok := s.MemoryStats.Stats["inactive_file"]; ok && used > v {
			used -= v
		} else if v, ok := s.MemoryStats.Stats["cache"]; ok && used > v {
			used -= v
		}
	}
	return int64(used)
}

func (e *Engine) Stats(ctx context.Context, id string) (Stats, error) {
	if id == "" {
		return Stats{}, ErrNotFound
	}
	r, err := e.cli.ContainerStatsOneShot(ctx, id)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return Stats{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return Stats{}, err
	}
	defer r.Body.Close()
	var s container.StatsResponse
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		return Stats{}, err
	}
	return Stats{CPUPercent: cpuPct(s), MemUsed: memUsed(s), MemLimit: int64(s.MemoryStats.Limit)}, nil
}

func (e *Engine) Create(ctx context.Context, botID, token string) (string, error) {
	cfg := e.store.Config()
	ws := filepath.Join(cfg.DataDir, "bots", botID, "workspace")
	bot := filepath.Join(ws, "bot")
	tmp := filepath.Join(ws, "tmp")
	chrome := filepath.Join(cfg.DataDir, "bots", botID, "chrome-profile")
	for _, d := range []string{ws, bot, tmp, chrome} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
	}
	absWS, _ := filepath.Abs(ws)
	absChrome, _ := filepath.Abs(chrome)
	binds := []string{
		absWS + ":/workspace",
		absChrome + ":/home/silo/chrome-profile",
	}
	if b := e.botDriveBind(ctx, botID); b != "" {
		binds = append(binds, b)
	}
	resp, err := e.cli.ContainerCreate(ctx, &container.Config{
		Image: cfg.BotImage,
		Env: []string{
			"SILO_CP_URL=" + cfg.CPURL,
			"SILO_BOT_TOKEN=" + token,
			"SILO_BOT_ID=" + botID,
		},
		Hostname: "bot",
	}, &container.HostConfig{
		Binds:         binds,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		ExtraHosts:    []string{"host.containers.internal:host-gateway"},
	}, nil, nil, Name(botID))
	if err != nil {
		return "", fmt.Errorf("container create: %w", err)
	}
	return resp.ID, nil
}

func (e *Engine) Start(ctx context.Context, id string) error {
	return e.cli.ContainerStart(ctx, id, container.StartOptions{})
}

func (e *Engine) Stop(ctx context.Context, id string) error {
	timeout := 10
	return e.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

func (e *Engine) Remove(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return e.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
}

func (e *Engine) Drop(ctx context.Context, botID, containerID string) {
	_ = e.Remove(ctx, containerID)
	_ = e.Remove(ctx, Name(botID))
}

func (e *Engine) CreateStdio(ctx context.Context, spec StdioSpec) (string, error) {
	if spec.ID == "" {
		return "", errors.New("stdio id required")
	}
	cfg := e.store.Config()
	image := strings.TrimSpace(spec.Image)
	if image == "" {
		image = cfg.MCPStdioImage
	}
	var binds []string
	if cfg.DataDir != "" {
		cache := filepath.Join(cfg.DataDir, "mcp-npm")
		if err := os.MkdirAll(cache, 0o700); err != nil {
			return "", err
		}
		if abs, err := filepath.Abs(cache); err == nil {
			// Shared npm cache so recreating a sidecar does not re-download
			// packages its siblings already pulled.
			binds = append(binds, abs+":/root/.npm")
		}
	}
	resp, err := e.cli.ContainerCreate(ctx, &container.Config{
		Image:    image,
		Env:      spec.Env,
		Hostname: "mcp",
		Labels: map[string]string{
			"silo.role":         "mcp-stdio",
			"silo.connector_id": spec.ID,
		},
	}, &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		ExtraHosts:    []string{"host.containers.internal:host-gateway"},
		Binds:         binds,
	}, nil, nil, StdioName(spec.ID))
	if err != nil {
		return "", fmt.Errorf("stdio create: %w", err)
	}
	return resp.ID, nil
}

func (e *Engine) DropStdio(ctx context.Context, id, containerID string) {
	_ = e.Remove(ctx, containerID)
	if id != "" {
		_ = e.Remove(ctx, StdioName(id))
	}
}

// ListStdio returns every running-or-stopped MCP sidecar container, keyed by
// its connector label. The CP uses it to reclaim containers whose attachment
// row is gone.
func (e *Engine) ListStdio(ctx context.Context) ([]StdioContainer, error) {
	return listRole(ctx, e, "mcp-stdio", func(id, name string, labels map[string]string) StdioContainer {
		return StdioContainer{ID: id, Name: name, ConnectorID: labels["silo.connector_id"]}
	})
}

// listRole returns every running-or-stopped container labeled silo.role=role,
// each built by mk from its id, name (without the leading slash) and labels.
func listRole[T any](ctx context.Context, e *Engine, role string, mk func(id, name string, labels map[string]string) T) ([]T, error) {
	items, err := e.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "silo.role="+role)),
	})
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(items))
	for _, c := range items {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, mk(c.ID, name, c.Labels))
	}
	return out, nil
}
