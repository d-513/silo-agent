package dockerx

import (
	"context"
	"fmt"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// EgressName is the network filter gateway container; there is one per engine.
func EgressName() string { return "silo-egress" }

// ClientNet is a client container's own internal network and what it needs to
// reach the outside through the gateway: the gateway's address on that network
// is its DNS, and the gateway's control-plane link is where it dials home.
type ClientNet struct {
	Network string
	DNS     string
	CPURL   string
}

// EgressSpec describes the gateway container.
type EgressSpec struct {
	Image string
	Env   []string
	// Primary is the ordinary network the gateway joins on top of every
	// client network; it is how the gateway reaches the control plane.
	Primary string
}

// EgressContainer is the discovered gateway.
type EgressContainer struct {
	ID   string
	Name string
}

// NetworkInfo is one client network the CP carved.
type NetworkInfo struct {
	Name     string
	Subnet   string
	ClientID string
}

// ClientNetworkLabel marks a network the CP made for one egress client.
const (
	clientNetLabel = "silo.egress"
	clientIDLabel  = "silo.client_id"
)

// EgressUp creates and starts the gateway when it is absent, attached to the
// ordinary network. Idempotent: an existing container is returned as-is.
func (e *Engine) EgressUp(ctx context.Context, spec EgressSpec) (string, error) {
	if spec.Image == "" {
		spec.Image = e.store.Config().EgressImage()
	}
	if st, err := e.Inspect(ctx, EgressName()); err == nil {
		if !st.Running {
			_ = e.Start(ctx, st.ID)
		}
		return st.ID, nil
	} else if !IsNotFound(err) {
		return "", err
	}
	host := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		CapAdd:        []string{"NET_ADMIN"},
	}
	if spec.Primary != "" {
		host.NetworkMode = container.NetworkMode(spec.Primary)
	}
	resp, err := e.cli.ContainerCreate(ctx, &container.Config{
		Image:  spec.Image,
		Env:    spec.Env,
		Labels: map[string]string{"silo.role": "egress"},
	}, host, nil, nil, EgressName())
	if err != nil {
		return "", fmt.Errorf("egress create: %w", err)
	}
	if err := e.Start(ctx, resp.ID); err != nil {
		_ = e.Remove(ctx, resp.ID)
		return "", err
	}
	return resp.ID, nil
}

// EgressID finds the gateway container, or "" when there is none.
func (e *Engine) EgressID(ctx context.Context) (string, error) {
	st, err := e.Inspect(ctx, EgressName())
	if IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return st.ID, nil
}

// DropEgress stops and removes the gateway.
func (e *Engine) DropEgress(ctx context.Context, containerID string) {
	for _, id := range []string{containerID, EgressName()} {
		if id == "" {
			continue
		}
		_ = e.Remove(ctx, id)
	}
}

// EnsureNetwork creates a client's internal network with its subnet when it is
// absent, and returns its name. An existing network is left alone.
func (e *Engine) EnsureNetwork(ctx context.Context, name, subnet, clientID string) error {
	if _, err := e.cli.NetworkInspect(ctx, name, network.InspectOptions{}); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	_, err := e.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		IPAM:     &network.IPAM{Config: []network.IPAMConfig{{Subnet: subnet}}},
		Labels:   map[string]string{clientNetLabel: "client", clientIDLabel: clientID},
	})
	if err != nil {
		return fmt.Errorf("network create %s: %w", name, err)
	}
	return nil
}

// DropNetwork removes a client's network. A network still in use is left: the
// client's container must go first.
func (e *Engine) DropNetwork(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	err := e.cli.NetworkRemove(ctx, name)
	if err != nil && cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// NetworkConnect attaches a container to a network.
func (e *Engine) NetworkConnect(ctx context.Context, networkName, containerID string) error {
	if networkName == "" || containerID == "" {
		return nil
	}
	err := e.cli.NetworkConnect(ctx, networkName, containerID, nil)
	if err != nil {
		m := strings.ToLower(err.Error())
		if strings.Contains(m, "already exists") || strings.Contains(m, "already connected") {
			return nil
		}
	}
	return err
}

// NetworkDisconnect detaches a container from a network.
func (e *Engine) NetworkDisconnect(ctx context.Context, networkName, containerID string) error {
	if networkName == "" || containerID == "" {
		return nil
	}
	err := e.cli.NetworkDisconnect(ctx, networkName, containerID, true)
	if err != nil && cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// NetworkAddr is a container's address on a network, or "" when it is not on
// it.
func (e *Engine) NetworkAddr(ctx context.Context, containerID, networkName string) (string, error) {
	if containerID == "" || networkName == "" {
		return "", nil
	}
	c, err := e.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if c.NetworkSettings == nil {
		return "", nil
	}
	if ep := c.NetworkSettings.Networks[networkName]; ep != nil {
		return strings.TrimSuffix(ep.IPAddress, "/32"), nil
	}
	return "", nil
}

// ListClientNetworks returns every network the CP made for an egress client,
// with its subnet and the client id it carries.
func (e *Engine) ListClientNetworks(ctx context.Context) ([]NetworkInfo, error) {
	nets, err := e.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", clientNetLabel+"=client")),
	})
	if err != nil {
		return nil, err
	}
	out := make([]NetworkInfo, 0, len(nets))
	for _, n := range nets {
		info := NetworkInfo{Name: n.Name, ClientID: n.Labels[clientIDLabel]}
		if len(n.IPAM.Config) > 0 {
			info.Subnet = n.IPAM.Config[0].Subnet
		}
		out = append(out, info)
	}
	return out, nil
}

// Runtime names the container engine behind the API: "podman", "docker", or
// "unknown". The network filter refuses a plain Docker engine until it is
// tested, so the CP asks.
func (e *Engine) Runtime(ctx context.Context) (string, error) {
	v, err := e.cli.ServerVersion(ctx)
	if err != nil {
		return "", err
	}
	hay := v.Platform.Name
	for _, c := range v.Components {
		hay += " " + c.Name
	}
	hay = strings.ToLower(hay)
	switch {
	case strings.Contains(hay, "podman"):
		return "podman", nil
	case strings.Contains(hay, "docker") || strings.Contains(hay, "engine"):
		return "docker", nil
	default:
		return "unknown", nil
	}
}

// clientNetworkConfig is the networking config for a client container: its own
// internal network as the primary.
func clientNetworkConfig(net *ClientNet) *network.NetworkingConfig {
	if net == nil || net.Network == "" {
		return nil
	}
	return &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{net.Network: {}}}
}

// clientDNS is the DNS list a client starts with: the gateway on its network.
func clientDNS(net *ClientNet) []string {
	if net == nil || net.DNS == "" {
		return nil
	}
	return []string{net.DNS}
}
