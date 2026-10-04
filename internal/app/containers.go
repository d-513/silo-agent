package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/connector"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/mcpbridge"
	"silo.agent/internal/textx"
)

// Container kinds on the Containers tab.
const (
	boxBot   = "bot"
	boxDrive = "drive"
	boxMCP   = "mcp"
)

// botBox is one container a Bot owns, before it is inspected.
type botBox struct {
	kind, name, label, detail string
	id                        string // last known container id; "" means look up by name
}

// botBoxes lists what a Bot owns: the machine, the drive sidecar when it has
// drives (or one is left over), and a sidecar per STDIO connector.
func (a *App) botBoxes(b *db.Bot) []botBox {
	out := []botBox{{kind: boxBot, name: dockerx.Name(b.ID), label: "Machine", detail: "Desktop, files, Python, and the worker", id: b.ContainerID}}

	var host db.DriveHost
	a.DB.Where("bot_id = ?", b.ID).Limit(1).Find(&host)
	if n := len(a.Drives.BotDrives(b.ID)); n > 0 || host.ContainerID != "" {
		detail := fmt.Sprintf("rclone for %d drive%s", n, textx.Plural(n))
		if n == 0 {
			detail = "No drives left"
		}
		out = append(out, botBox{kind: boxDrive, name: dockerx.DriveName(b.ID), label: "Drives", detail: detail, id: host.ContainerID})
	}

	var links []db.BotConnector
	a.DB.Where("bot_id = ?", b.ID).Order("created_at").Find(&links)
	for _, l := range links {
		var c db.Connector
		if a.DB.Where("id = ?", l.ConnectorID).Limit(1).Find(&c); c.ID == "" || c.Transport != connector.TransportSTDIO {
			continue
		}
		cmd := strings.TrimSpace(c.StdioCommand + " " + strings.Join(mcpbridge.ParseArgs(c.StdioArgsJSON), " "))
		out = append(out, botBox{kind: boxMCP, name: dockerx.StdioName(l.ID), label: c.Name, detail: "MCP · " + textx.ClipRunes(cmd, 80), id: l.ContainerID})
	}
	return out
}

func (a *App) ListBotContainers(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.BotContainers], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	boxes := a.botBoxes(b)
	out := make([]*v1.BotContainer, len(boxes))
	var wg sync.WaitGroup
	for i, box := range boxes {
		out[i] = &v1.BotContainer{Kind: box.kind, Name: box.name, Label: box.label, Detail: box.detail, State: "absent"}
		if a.Docker == nil {
			continue
		}
		wg.Add(1)
		go func(c *v1.BotContainer, box botBox) {
			defer wg.Done()
			// The stored id may be stale (removed out of band); the name is
			// the fallback, as GetBot does.
			st, err := a.Docker.Inspect(ctx, box.id)
			if err != nil {
				st, err = a.Docker.Inspect(ctx, box.name)
			}
			if err != nil {
				return
			}
			c.Image = st.Image
			if !st.Running {
				c.State = "stopped"
				return
			}
			c.State = "running"
			if !st.StartedAt.IsZero() {
				c.StartedAt = st.StartedAt.UTC().Format(time.RFC3339)
			}
			if s, err := a.Docker.Stats(ctx, st.ID); err == nil {
				c.CpuPercent, c.MemUsed, c.MemLimit = s.CPUPercent, s.MemUsed, s.MemLimit
			}
		}(out[i], box)
	}
	wg.Wait()
	return connect.NewResponse(&v1.BotContainers{Containers: out}), nil
}

// RemoveBotContainers is a precise `make cleanup` for one Bot: its machine,
// drive sidecar, and MCP sidecars go; the workspace, drives, connectors, and
// the drive cache stay, and each container is made again on its next use.
func (a *App) RemoveBotContainers(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.Bot], error) {
	b, err := a.ownBot(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	a.destroyBot(ctx, b)
	a.Drives.RemoveSidecar(ctx, b.ID)
	a.Connectors.DropBotStdio(b.ID)
	b.Status = "stopped"
	a.DB.Save(b)
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}
