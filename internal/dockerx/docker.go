package dockerx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"silo.agent/internal/config"
)

func Name(botID string) string { return "silo-" + botID }

var ErrNotFound = errors.New("container not found")

func IsNotFound(err error) bool {
	return err != nil && (errors.Is(err, ErrNotFound) || cerrdefs.IsNotFound(err))
}

type State struct {
	ID      string
	Running bool
}

type Host interface {
	Inspect(ctx context.Context, id string) (State, error)
	Create(ctx context.Context, botID, token string) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Drop(ctx context.Context, botID, containerID string)
}

type Engine struct {
	cli *client.Client
	cfg *config.Config
}

func New(cfg *config.Config) (*Engine, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if cfg.DockerHost != "" {
		opts = append(opts, client.WithHost(cfg.DockerHost))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Engine{cli: cli, cfg: cfg}, nil
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
	return State{ID: c.ID, Running: run}, nil
}

func (e *Engine) Create(ctx context.Context, botID, token string) (string, error) {
	ws := filepath.Join(e.cfg.DataDir, "bots", botID, "workspace")
	chrome := filepath.Join(e.cfg.DataDir, "bots", botID, "chrome-profile")
	for _, d := range []string{ws, chrome} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
	}
	absWS, _ := filepath.Abs(ws)
	absChrome, _ := filepath.Abs(chrome)
	resp, err := e.cli.ContainerCreate(ctx, &container.Config{
		Image: e.cfg.BotImage,
		Env: []string{
			"SILO_CP_URL=" + e.cfg.CPURL,
			"SILO_BOT_TOKEN=" + token,
			"SILO_BOT_ID=" + botID,
		},
		Hostname: "bot",
	}, &container.HostConfig{
		Binds: []string{
			absWS + ":/workspace",
			absChrome + ":/home/bot/chrome-profile",
		},
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
