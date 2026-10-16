package dockerx

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// Everything Silo ever puts on the engine is named silo-… or carries a
// silo.… label. The Admin Containers tab lists that and nothing else: the
// engine may host things other tools made, and none of them are ours to
// touch.

// SiloContainer is one Silo container seen in one engine listing.
type SiloContainer struct {
	ID      string
	Name    string
	Role    string // silo.role label; "" when the container carries none
	Image   string
	State   string // the summary state, e.g. running | exited | created
	Running bool
	// Networks the summary shows; a full Inspect (or ListSiloNetworks) knows
	// the live set.
	Networks []string
}

// isSilo judges one listing row: a Silo name, or any label we wrote.
func isSilo(name string, labels map[string]string) bool {
	if strings.HasPrefix(name, "silo-") {
		return true
	}
	for k := range labels {
		if strings.HasPrefix(k, "silo.") {
			return true
		}
	}
	return false
}

// ListSilo returns every container Silo created, in any state, discovered as
// the Admin tab sees them: the Bot boxes the database claims plus any left
// over that it does not.
func (e *Engine) ListSilo(ctx context.Context) ([]SiloContainer, error) {
	items, err := e.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]SiloContainer, 0, len(items))
	for _, c := range items {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		if !isSilo(name, c.Labels) {
			continue
		}
		sc := SiloContainer{
			ID:      c.ID,
			Name:    name,
			Role:    c.Labels["silo.role"],
			Image:   c.Image,
			State:   c.State,
			Running: c.State == "running",
		}
		if c.NetworkSettings != nil {
			for n := range c.NetworkSettings.Networks {
				sc.Networks = append(sc.Networks, n)
			}
		}
		out = append(out, sc)
	}
	return out, nil
}

// SiloNetwork is one network the CP carved, with the engines' view of the
// containers attached to it.
type SiloNetwork struct {
	NetworkInfo
	// Containers are the names of the containers on the network; an engine
	// that does not name endpoints falls back to a short id.
	Containers []string
}

// ListSiloNetworks returns every client network with the containers it
// carries, so the Admin tab can draw the wires.
func (e *Engine) ListSiloNetworks(ctx context.Context) ([]SiloNetwork, error) {
	infos, err := e.ListClientNetworks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SiloNetwork, 0, len(infos))
	for _, n := range infos {
		sn := SiloNetwork{NetworkInfo: n}
		if insp, err := e.cli.NetworkInspect(ctx, n.Name, network.InspectOptions{}); err == nil {
			for id, ep := range insp.Containers {
				name := ep.Name
				if name == "" {
					// Engines that do not name their endpoints give the id.
					if len(id) > 12 {
						id = id[:12]
					}
					name = id
				}
				sn.Containers = append(sn.Containers, strings.TrimPrefix(name, "/"))
			}
		}
		out = append(out, sn)
	}
	return out, nil
}
