package siloosd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// A network change runs under a NetworkManager checkpoint: NetworkManager
// puts every device back as it was when the checkpoint's timeout passes,
// unless ConfirmNetwork destroys it first. The confirmation comes from the
// console over the new settings, so a change that cuts the admin off undoes
// itself. Checkpoints do not survive a reboot: a machine restarted inside the
// window keeps the new settings.

const (
	nmService = "org.freedesktop.NetworkManager"
	nmPath    = "/org/freedesktop/NetworkManager"
	nmIface   = "org.freedesktop.NetworkManager"
	// NM_CHECKPOINT_CREATE_FLAG_DESTROY_ALL: a new checkpoint replaces any
	// other, so two never stack.
	cpDestroyAll = "1"

	confirmDefault = 120
	confirmMin     = 30
	confirmMax     = 600
)

// shownTypes are the devices the page lists (not loopback, tunnels, veths).
var shownTypes = map[string]bool{"ethernet": true, "wifi": true, "bond": true, "bridge": true, "vlan": true, "team": true}

// pendingNet is a change waiting for confirmation, kept in StateDir so a
// restarted siloosd still knows it.
type pendingNet struct {
	Checkpoint string    `json:"checkpoint"`
	Connection string    `json:"connection"`
	Expires    time.Time `json:"expires"`
}

const pendingFile = "network-pending.json"

func (s *Server) loadPending() *pendingNet {
	b, err := os.ReadFile(s.statePath(pendingFile))
	if err != nil {
		return nil
	}
	var p pendingNet
	if json.Unmarshal(b, &p) != nil || !pathRE.MatchString(p.Checkpoint) {
		return nil
	}
	return &p
}

func (s *Server) savePending(p *pendingNet) {
	path := s.statePath(pendingFile)
	if p == nil {
		os.Remove(path)
		return
	}
	b, _ := json.Marshal(p)
	writeFile(path, b)
}

func (s *Server) GetNetwork(ctx context.Context, _ *connect.Request[v1.GetNetworkRequest]) (*connect.Response[v1.Network], error) {
	n, err := s.network(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(n), nil
}

func (s *Server) network(ctx context.Context) (*v1.Network, error) {
	raw, err := s.run.Run(ctx, "nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CON-UUID", "device", "status")
	if err != nil {
		return nil, err
	}
	out := &v1.Network{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := splitTerse(line)
		if len(f) < 4 || !shownTypes[f[1]] || f[2] == "unmanaged" {
			continue
		}
		d := &v1.NetDevice{Name: f[0], Type: f[1], State: f[2]}
		// The device's name came from nmcli; it goes back to nmcli as one
		// argument, but never as an option.
		if !strings.HasPrefix(d.Name, "-") {
			if show, err := s.run.Run(ctx, "nmcli", "-t", "-e", "no", "-f", "GENERAL.HWADDR,IP4.ADDRESS,IP4.GATEWAY,IP4.DNS", "device", "show", d.Name); err == nil {
				fillDevice(d, string(show))
			}
		}
		if validUUID(f[3]) {
			if c, err := s.connection(ctx, f[3]); err == nil {
				d.Connection = c
			}
		}
		out.Devices = append(out.Devices, d)
	}
	if p := s.livePending(ctx); p != nil {
		out.Pending = &v1.PendingNetwork{ExpiresAt: p.Expires.UTC().Format(time.RFC3339), Connection: p.Connection}
	}
	return out, nil
}

// livePending is the change waiting for confirmation, or nil once
// NetworkManager no longer holds its checkpoint (rolled back, or a reboot).
func (s *Server) livePending(ctx context.Context) *pendingNet {
	s.mu.Lock()
	p := s.pending
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	held, err := s.checkpoints(ctx)
	if (err == nil && !held[p.Checkpoint]) || (err != nil && s.now().After(p.Expires)) {
		s.mu.Lock()
		if s.pending == p {
			s.pending = nil
			s.savePending(nil)
		}
		s.mu.Unlock()
		return nil
	}
	return p
}

func (s *Server) checkpoints(ctx context.Context) (map[string]bool, error) {
	raw, err := s.run.Run(ctx, "busctl", "--json=short", "get-property", nmService, nmPath, nmIface, "Checkpoints")
	if err != nil {
		return nil, err
	}
	var v struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("busctl: %w", err)
	}
	out := map[string]bool{}
	for _, p := range v.Data {
		out[p] = true
	}
	return out, nil
}

func (s *Server) connection(ctx context.Context, uuid string) (*v1.NetConnection, error) {
	raw, err := s.run.Run(ctx, "nmcli", "-t", "-e", "no", "-f",
		"connection.id,connection.uuid,connection.type,ipv4.method,ipv4.addresses,ipv4.gateway,ipv4.dns,ipv4.ignore-auto-dns",
		"connection", "show", uuid)
	if err != nil {
		return nil, err
	}
	kv := terseShow(string(raw))
	c := &v1.NetConnection{
		Uuid:          kv["connection.uuid"],
		Name:          kv["connection.id"],
		Method:        kv["ipv4.method"],
		Addresses:     list(kv["ipv4.addresses"]),
		Gateway:       kv["ipv4.gateway"],
		Dns:           list(kv["ipv4.dns"]),
		IgnoreAutoDns: kv["ipv4.ignore-auto-dns"] == "yes",
	}
	if kv["connection.type"] != "802-3-ethernet" {
		// Only wired profiles are edited here; others are shown as they are.
		c.Method = "other:" + c.Method
	}
	return c, nil
}

func (s *Server) ApplyConnection(ctx context.Context, req *connect.Request[v1.ApplyConnectionRequest]) (*connect.Response[v1.Network], error) {
	m := req.Msg
	uuid := strings.TrimSpace(m.GetUuid())
	if !validUUID(uuid) {
		return nil, invalid("Name a connection by its UUID.")
	}
	args, err := connectionArgs(m)
	if err != nil {
		return nil, err
	}
	within := int(m.GetConfirmWithin())
	if within == 0 {
		within = confirmDefault
	}
	if within < confirmMin || within > confirmMax {
		return nil, invalid(fmt.Sprintf("Confirm within %d to %d seconds.", confirmMin, confirmMax))
	}
	cur, err := s.connection(ctx, uuid)
	if err != nil {
		return nil, invalid("There is no connection " + uuid + ".")
	}
	if strings.HasPrefix(cur.Method, "other:") {
		return nil, invalid("Only wired (ethernet) connections can be changed here.")
	}
	if s.livePending(ctx) != nil {
		return nil, refused("Another change is waiting to be kept or undone.")
	}

	raw, err := s.run.Run(ctx, "busctl", "--json=short", "call", nmService, nmPath, nmIface,
		"CheckpointCreate", "aouu", "0", strconv.Itoa(within), cpDestroyAll)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	cp, err := objectPath(raw)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	p := &pendingNet{Checkpoint: cp, Connection: uuid, Expires: s.now().Add(time.Duration(within) * time.Second)}
	s.mu.Lock()
	s.pending = p
	s.savePending(p)
	s.mu.Unlock()

	fail := func(err error) (*connect.Response[v1.Network], error) {
		s.rollBack(ctx, p)
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("NetworkManager refused the change, so it was undone: %w", err))
	}
	if _, err := s.run.Run(ctx, "nmcli", append([]string{"connection", "modify", uuid}, args...)...); err != nil {
		return fail(err)
	}
	if _, err := s.run.Run(ctx, "nmcli", "--wait", "30", "connection", "up", uuid); err != nil {
		return fail(err)
	}
	audit(ctx, "changed connection %s (%s); rolls back at %s unless kept", uuid, strings.Join(args, " "), p.Expires.UTC().Format(time.RFC3339))
	n, err := s.network(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(n), nil
}

// connectionArgs are nmcli's settings for a request: every IPv4 setting the
// page edits, so what is not said is cleared, never left from before.
func connectionArgs(m *v1.ApplyConnectionRequest) ([]string, error) {
	var addrs, dns []string
	for _, a := range m.GetAddresses() {
		if strings.TrimSpace(a) == "" {
			continue
		}
		p, ok := ipv4Prefix(a)
		if !ok {
			return nil, invalid(fmt.Sprintf("%q is not an IPv4 address with its prefix (192.168.1.10/24).", a))
		}
		addrs = append(addrs, p.String())
	}
	for _, d := range m.GetDns() {
		if strings.TrimSpace(d) == "" {
			continue
		}
		a, ok := dnsAddr(d)
		if !ok {
			return nil, invalid(fmt.Sprintf("%q is not a DNS server's address.", d))
		}
		dns = append(dns, a.String())
	}
	if len(addrs) > 8 || len(dns) > 4 {
		return nil, invalid("At most 8 addresses and 4 DNS servers.")
	}
	gw := strings.TrimSpace(m.GetGateway())
	if gw != "" {
		a, ok := ipv4Addr(gw)
		if !ok {
			return nil, invalid(fmt.Sprintf("%q is not an IPv4 gateway.", gw))
		}
		gw = a.String()
	}
	ignore := "no"
	if m.GetIgnoreAutoDns() {
		ignore = "yes"
	}
	switch m.GetMethod() {
	case "auto":
		if len(addrs) > 0 || gw != "" {
			return nil, invalid("DHCP takes no addresses or gateway of its own.")
		}
	case "manual":
		if len(addrs) == 0 {
			return nil, invalid("A manual connection needs at least one address.")
		}
		if gw != "" && !onLink(gw, addrs) {
			return nil, invalid("The gateway is not on any of the connection's networks.")
		}
	default:
		return nil, invalid("The method is auto (DHCP) or manual.")
	}
	return []string{
		"ipv4.method", m.GetMethod(),
		"ipv4.addresses", strings.Join(addrs, ","),
		"ipv4.gateway", gw,
		"ipv4.dns", strings.Join(dns, ","),
		"ipv4.ignore-auto-dns", ignore,
	}, nil
}

func onLink(gw string, addrs []string) bool {
	g, _ := ipv4Addr(gw)
	for _, a := range addrs {
		if p, ok := ipv4Prefix(a); ok && p.Masked().Contains(g) {
			return true
		}
	}
	return false
}

func (s *Server) ConfirmNetwork(ctx context.Context, _ *connect.Request[v1.ConfirmNetworkRequest]) (*connect.Response[v1.Network], error) {
	p := s.livePending(ctx)
	if p == nil {
		return nil, refused("No change is waiting: it was kept already, or its time ran out and it was undone.")
	}
	if _, err := s.run.Run(ctx, "busctl", "call", nmService, nmPath, nmIface, "CheckpointDestroy", "o", p.Checkpoint); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.clearPending(p)
	audit(ctx, "kept the change to connection %s", p.Connection)
	return s.GetNetwork(ctx, nil)
}

func (s *Server) RollBackNetwork(ctx context.Context, _ *connect.Request[v1.RollBackNetworkRequest]) (*connect.Response[v1.Network], error) {
	p := s.livePending(ctx)
	if p == nil {
		return nil, refused("No change is waiting to be undone.")
	}
	if err := s.rollBack(ctx, p); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	audit(ctx, "undid the change to connection %s", p.Connection)
	return s.GetNetwork(ctx, nil)
}

func (s *Server) rollBack(ctx context.Context, p *pendingNet) error {
	_, err := s.run.Run(ctx, "busctl", "call", nmService, nmPath, nmIface, "CheckpointRollback", "o", p.Checkpoint)
	s.clearPending(p)
	return err
}

func (s *Server) clearPending(p *pendingNet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == p {
		s.pending = nil
		s.savePending(nil)
	}
}

// objectPath reads busctl's JSON answer of one object path.
func objectPath(raw []byte) (string, error) {
	var v struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || len(v.Data) != 1 || !pathRE.MatchString(v.Data[0]) {
		return "", errors.New("NetworkManager made no checkpoint")
	}
	return v.Data[0], nil
}

// splitTerse splits one line of nmcli's terse output on the colons it did
// not escape, and unescapes the fields.
func splitTerse(line string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case c == ':':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(out, cur.String())
}

// terseShow reads `nmcli -t -e no … show`: FIELD:value lines, the first
// colon the separator. A repeated field (IP4.ADDRESS[1], [2]) is joined with
// commas under its bare name.
func terseShow(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		if i := strings.IndexByte(k, '['); i > 0 {
			k = k[:i]
		}
		v = strings.TrimSpace(v)
		if v == "--" {
			v = ""
		}
		if prev, ok := out[k]; ok && prev != "" && v != "" {
			v = prev + "," + v
		} else if ok && v == "" {
			v = prev
		}
		out[k] = v
	}
	return out
}

func fillDevice(d *v1.NetDevice, raw string) {
	kv := terseShow(raw)
	d.Mac = kv["GENERAL.HWADDR"]
	d.Addresses = list(kv["IP4.ADDRESS"])
	d.Gateway = kv["IP4.GATEWAY"]
	d.Dns = list(kv["IP4.DNS"])
}

// list splits a comma (or ", ") separated nmcli value.
func list(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
