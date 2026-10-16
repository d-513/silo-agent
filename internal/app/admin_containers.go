package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
)

// Admin Containers tab: everything Silo has on the engine, grouped by user
// and Bot, with the system-wide container (the egress gateway) and whatever
// Silo left over that the database no longer claims. Containers other tools
// made are never listed here and never touched: the engine only answers for
// silo-* names and silo.* labels.

// siloLister plugs the two discovery RPCs through a.Docker without widening
// every caller's contract; only the Admin tab asks.
type siloLister interface {
	ListSilo(ctx context.Context) ([]dockerx.SiloContainer, error)
	ListSiloNetworks(ctx context.Context) ([]dockerx.SiloNetwork, error)
}

// containerTarget classifies a Silo engine name: a Bot's machine
// (silo-<botID>), its drive sidecar, a STDIO connector's sidecar, the egress
// gateway, or anything else Silo-named.
func containerTarget(name string) (kind, id string) {
	switch {
	case name == "silo-egress":
		return "egress", ""
	case strings.HasPrefix(name, "silo-mcp-"):
		return "mcp", strings.TrimPrefix(name, "silo-mcp-")
	case strings.HasPrefix(name, "silo-drive-"):
		return "drive", strings.TrimPrefix(name, "silo-drive-")
	case strings.HasPrefix(name, "silo-"):
		return "bot", strings.TrimPrefix(name, "silo-")
	}
	return "", ""
}

// fillAdminContainer is the Inspect-and-Stats the per-Bot page does, on one
// expected box; c already carries kind/name/label/detail/state.
func (a *App) fillAdminContainer(ctx context.Context, c *v1.AdminContainer, box botBox) {
	if a.Docker == nil {
		return
	}
	st, err := a.Docker.Inspect(ctx, box.id)
	if err != nil {
		st, err = a.Docker.Inspect(ctx, box.name)
	}
	if err != nil {
		return
	}
	c.Image = st.Image
	c.Networks = st.Networks
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
}

func (a *App) ListAdminContainers(ctx context.Context, req *connect.Request[v1.ListAdminContainersRequest]) (*connect.Response[v1.AdminContainers], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	out := &v1.AdminContainers{System: &v1.AdminSystemGroup{}}
	if a.Docker == nil {
		return connect.NewResponse(out), nil
	}
	if rt, err := a.Docker.Runtime(ctx); err == nil {
		out.Runtime = rt
	}

	var users []db.User
	if err := a.DB.Model(&db.User{}).Order("created_at").Find(&users).Error; err != nil {
		return nil, err
	}
	emails := make(map[string]string, len(users))
	for _, u := range users {
		emails[u.ID] = u.Email
	}
	var bots []db.Bot
	if err := a.DB.Model(&db.Bot{}).Order("created_at").Find(&bots).Error; err != nil {
		return nil, err
	}

	// Each Bot gets its group up front, so leftover sidecars can land there.
	userGroups := map[string]*v1.AdminUserGroup{}
	botGroups := map[string]*v1.AdminBotGroup{}
	var groupOrder []*v1.AdminUserGroup
	for i := range bots {
		b := &bots[i]
		ug := userGroups[b.UserID]
		if ug == nil {
			ug = &v1.AdminUserGroup{UserId: b.UserID, Email: emails[b.UserID]}
			userGroups[b.UserID] = ug
			groupOrder = append(groupOrder, ug)
		}
		bg := &v1.AdminBotGroup{BotId: b.ID, BotName: b.Name, Status: b.Status}
		ug.Bots = append(ug.Bots, bg)
		botGroups[b.ID] = bg
	}
	out.Users = groupOrder

	// One Inspect+Stats per expected box, in parallel.
	type job struct {
		c   *v1.AdminContainer
		box botBox
	}
	var jobs []*job
	for i := range bots {
		b := &bots[i]
		bg := botGroups[b.ID]
		for _, box := range a.botBoxes(b) {
			c := &v1.AdminContainer{Kind: box.kind, Name: box.name, Label: box.label, Detail: box.detail, State: "absent"}
			bg.Containers = append(bg.Containers, c)
			jobs = append(jobs, &job{c: c, box: box})
		}
	}
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j *job) {
			defer wg.Done()
			a.fillAdminContainer(ctx, j.c, j.box)
		}(j)
	}
	wg.Wait()

	// Networks: engine first, then the client rows that say who they serve.
	var clients []db.EgressClient
	a.DB.Find(&clients)
	byClient := make(map[string]db.EgressClient, len(clients))
	for _, r := range clients {
		byClient[r.ID] = r
	}
	if l, ok := a.Docker.(siloLister); ok {
		nets, err := l.ListSiloNetworks(ctx)
		if err != nil {
			nets = nil
		}
		for _, n := range nets {
			row := &v1.AdminNetwork{Name: n.Name, Subnet: n.Subnet, Containers: n.Containers}
			if r, ok := byClient[n.ClientID]; ok {
				row.BotId = r.BotID
				if bg := botGroups[r.BotID]; bg != nil {
					bg.Networks = append(bg.Networks, row)
					continue
				}
			}
			out.System.Networks = append(out.System.Networks, row)
		}
	}

	// Then every Silo container on the engine the groups do not already
	// carry: egress gateway, forgotten sidecars, boxes whose Bot row is gone.
	covered := map[string]bool{}
	for _, bg := range botGroups {
		for _, c := range bg.Containers {
			covered[c.Name] = true
		}
	}
	if l, ok := a.Docker.(siloLister); ok {
		found, err := l.ListSilo(ctx)
		if err != nil {
			return nil, err
		}
		// Leftover sidecars know their bot by name; a connector or drive row
		// maps them into the right group.
		var linkIDs []string
		for _, e := range found {
			if kind, id := containerTarget(e.Name); kind == "mcp" && id != "" {
				linkIDs = append(linkIDs, id)
			}
		}
		links := map[string]db.BotConnector{}
		if len(linkIDs) > 0 {
			var rows []db.BotConnector
			a.DB.Where("id IN (?)", linkIDs).Find(&rows)
			for _, r := range rows {
				links[r.ID] = r
			}
		}
		connectorNames := func(connectorID string) string {
			var c db.Connector
			a.DB.Where("id = ?", connectorID).Limit(1).Find(&c)
			return c.Name
		}
		for _, e := range found {
			if covered[e.Name] {
				continue
			}
			covered[e.Name] = true
			c := &v1.AdminContainer{Name: e.Name, State: "stopped", Image: e.Image, Networks: e.Networks}
			if e.Running {
				c.State = "running"
			}
			kind, id := containerTarget(e.Name)
			switch {
			case kind == "egress" || e.Role == "egress":
				c.Kind = "egress"
				c.Label = "Egress gateway"
				out.System.Containers = append(out.System.Containers, c)
			case kind == "mcp" && links[id].ID != "":
				link := links[id]
				c.Kind = "mcp"
				c.Label = connectorNames(link.ConnectorID)
				c.Detail = "leftover sidecar"
				if bg := botGroups[link.BotID]; bg != nil {
					bg.Containers = append(bg.Containers, c)
					continue
				}
				out.System.Containers = append(out.System.Containers, c)
			case kind == "drive" && botGroups[id] != nil:
				c.Kind = "drive"
				c.Label = "Drives"
				c.Detail = "leftover sidecar"
				botGroups[id].Containers = append(botGroups[id].Containers, c)
			case kind == "bot" && botGroups[id] != nil:
				c.Kind = "bot"
				c.Label = "Machine"
				c.Detail = "the Bot's box"
				botGroups[id].Containers = append(botGroups[id].Containers, c)
			default:
				c.Kind = "stray"
				c.Label = e.Name
				switch {
				case kind == "drive":
					c.Detail = "leftover drive sidecar; its Bot is gone"
				case kind == "mcp":
					c.Detail = "leftover MCP sidecar; its connector is gone"
				case kind == "bot":
					c.Detail = "box of a Bot that no longer exists"
				default:
					c.Detail = "Silo container nothing in the database claims"
				}
				out.System.Containers = append(out.System.Containers, c)
			}
		}
	}
	return connect.NewResponse(out), nil
}

// siloContainerNames is the one list of engine names anything on the Admin
// tab may act on: what the groups carry plus what the engine discovers. This
// is the guard that keeps the actions off everyone else's containers.
func (a *App) siloContainerNames(ctx context.Context) (map[string]bool, error) {
	names := map[string]bool{"silo-egress": true}
	var bots []db.Bot
	a.DB.Model(&db.Bot{}).Find(&bots)
	for i := range bots {
		names[dockerx.Name(bots[i].ID)] = true
		names[dockerx.DriveName(bots[i].ID)] = true
		var links []db.BotConnector
		a.DB.Where("bot_id = ?", bots[i].ID).Find(&links)
		for _, l := range links {
			names[dockerx.StdioName(l.ID)] = true
		}
	}
	if l, ok := a.Docker.(siloLister); ok {
		found, err := l.ListSilo(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range found {
			names[e.Name] = true
		}
	}
	return names, nil
}

func (a *App) AdminContainerAction(ctx context.Context, req *connect.Request[v1.AdminContainerActionRequest]) (*connect.Response[v1.AdminActionResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if a.Docker == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("docker unavailable"))
	}
	name := strings.TrimSpace(req.Msg.GetName())
	action := strings.TrimSpace(req.Msg.GetAction())
	if action != "start" && action != "stop" && action != "remove" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown action %q", action))
	}
	allowed, err := a.siloContainerNames(ctx)
	if err != nil {
		return nil, err
	}
	if !allowed[name] {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("not a Silo container"))
	}
	kind, id := containerTarget(name)

	switch {
	case kind == "bot":
		var b db.Bot
		if err := a.DB.First(&b, "id = ?", id).Error; err != nil {
			// A box of a deleted Bot: engine actions only.
			return connect.NewResponse(&v1.AdminActionResponse{}), a.boxAction(ctx, name, action)
		}
		switch action {
		case "start":
			if err := a.ensureRunning(ctx, &b); err != nil {
				return nil, connect.NewError(connect.CodeFailedPrecondition, err)
			}
		case "stop":
			a.haltBot(ctx, &b)
			b.Status = "stopped"
			a.DB.Save(&b)
		case "remove":
			a.destroyBot(ctx, &b)
			b.Status = "stopped"
			a.DB.Save(&b)
		}
		return connect.NewResponse(&v1.AdminActionResponse{}), nil

	case kind == "drive":
		var n int64
		a.DB.Model(&db.Bot{}).Where("id = ?", id).Count(&n)
		if action == "remove" && n > 0 {
			// The service's own teardown also unmounts the drive directory.
			a.Drives.RemoveSidecar(ctx, id)
			return connect.NewResponse(&v1.AdminActionResponse{}), nil
		}

	case kind == "mcp":
		var link db.BotConnector
		if err := a.DB.First(&link, "id = ?", id).Error; err == nil && action == "remove" {
			a.Connectors.DropStdio(&link)
			return connect.NewResponse(&v1.AdminActionResponse{}), nil
		}
	}
	return connect.NewResponse(&v1.AdminActionResponse{}), a.boxAction(ctx, name, action)
}

// boxAction is one plain engine action on a Silo container by name.
func (a *App) boxAction(ctx context.Context, name, action string) error {
	var err error
	st, serr := a.Docker.Inspect(ctx, name)
	switch action {
	case "start":
		if serr != nil {
			return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s: not created", name))
		}
		if st.Running {
			return nil
		}
		err = a.Docker.Start(ctx, st.ID)
	case "stop":
		if dockerx.IsNotFound(serr) || !st.Running {
			return nil
		}
		err = a.Docker.Stop(ctx, st.ID)
	case "remove":
		if dockerx.IsNotFound(serr) {
			return nil
		}
		err = a.Docker.Remove(ctx, st.ID)
	}
	if err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return nil
}

// RemoveAdminBotContainers is RemoveBotContainers without the ownership
// check: the machine, the drive sidecar, and the MCP sidecars go; drives,
// connectors, caches, and the workspace stay.
func (a *App) RemoveAdminBotContainers(ctx context.Context, req *connect.Request[v1.GetBotRequest]) (*connect.Response[v1.AdminActionResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	var b db.Bot
	if err := a.DB.First(&b, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such bot"))
	}
	a.destroyBot(ctx, &b)
	a.Drives.RemoveSidecar(ctx, b.ID)
	a.Connectors.DropBotStdio(b.ID)
	b.Status = "stopped"
	a.DB.Save(&b)
	return connect.NewResponse(&v1.AdminActionResponse{}), nil
}
