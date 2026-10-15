// Package egress is the control plane's side of the network filter: it runs the
// silo-egress gateway (one per container engine), gives every Bot box and STDIO
// sidecar its own internal network carved from a hidden pool, pushes the whole
// desired state (clients, lists, the CA, the CP link) to the gateway, and reads
// back what it blocked and flagged.
package egress

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/egress"
	"silo.agent/internal/ids"
)

// gatewayID is the single EgressHost row.
const gatewayID = "gateway"

// caID is the single EgressCA row.
const caID = "default"

// Tick is how often the control plane reconciles the gateway and its client
// networks.
const Tick = 30 * time.Second

// cpLinkPort is where the gateway forwards a client's control-plane link.
func cpLinkPort() string {
	_, port, err := splitHostPort(egress.DefaultCPLinkAddr)
	if err != nil || port == "" {
		return "8443"
	}
	return port
}

// Service owns the gateway, every client's network, the CA and the gateway
// session.
type Service struct {
	db     *gorm.DB
	docker dockerx.Host
	store  *config.Store
	cfg    func() config.Config

	// OnEvents and OnTraffic are the ingest sinks: the audit timeline and the
	// per-minute traffic sums (wired in app.New). Nil drops the report.
	OnEvents  func(events []*v1.EgressEvent)
	OnTraffic func(rows []*v1.EgressTrafficRow)

	hub *hub

	mu      sync.Mutex
	problem string
	locks   sync.Map
}

func New(gdb *gorm.DB, d dockerx.Host, store *config.Store, cfg func() config.Config) *Service {
	return &Service{db: gdb, docker: d, store: store, cfg: cfg, hub: &hub{}}
}

// Enabled reports whether the network filter is on and usable on this engine.
// A plain Docker engine is refused until the filter has been tested against it.
func (s *Service) Enabled() bool {
	if s == nil || s.docker == nil || s.db == nil {
		return false
	}
	if !s.cfg().Egress.Enabled {
		return false
	}
	if rt, err := s.docker.Runtime(context.Background()); err == nil && rt == "docker" {
		return false
	}
	return true
}

// Problem is the last gateway failure, shown to admins. Empty when healthy.
func (s *Service) Problem() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.problem
}

func (s *Service) setProblem(e error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e == nil {
		s.problem = ""
		return
	}
	s.problem = e.Error()
}

func splitHostPort(addr string) (string, string, error) {
	host, port, err := splitHostPortRaw(addr)
	return host, port, err
}

func splitHostPortRaw(addr string) (string, string, error) {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return addr, "", nil
	}
	return addr[:i], addr[i+1:], nil
}

// cpAddr is host:port the gateway forwards a client's control-plane link to.
func (s *Service) cpAddr() string {
	raw := strings.TrimSpace(s.cfg().CPURL)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// Ensure brings the gateway up (when enabled) and attaches it to every client
// network. It is cheap once the gateway is connected.
func (s *Service) Ensure(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	if _, err := s.ensureGateway(ctx); err != nil {
		s.setProblem(err)
		return err
	}
	s.setProblem(nil)
	s.Apply()
	return nil
}

// ensureGateway makes sure the gateway container exists and is attached to
// every client network.
func (s *Service) ensureGateway(ctx context.Context) (string, error) {
	var host db.EgressHost
	s.db.Where("id = ?", gatewayID).Limit(1).Find(&host)
	id := host.ContainerID
	if id != "" {
		st, err := s.docker.Inspect(ctx, id)
		switch {
		case err == nil && st.Running:
		case err == nil:
			if s.docker.Start(ctx, st.ID) == nil {
				id = st.ID
			} else {
				s.docker.DropEgress(ctx, id)
				id = ""
			}
		case dockerx.IsNotFound(err):
			id = ""
		default:
			return "", err
		}
	}
	token := ""
	if id == "" {
		token = ids.New() + ids.New()
		cid, err := s.docker.EgressUp(ctx, dockerx.EgressSpec{
			Image: s.cfg().EgressImage(),
			Env: []string{
				"SILO_CP_URL=" + s.cfg().CPURL,
				"SILO_EGRESS_TOKEN=" + token,
			},
		})
		if err != nil {
			return "", err
		}
		id = cid
		host.ID = gatewayID
		host.ContainerID = id
		host.TokenHash = ids.Hash(token)
		if err := s.db.Save(&host).Error; err != nil {
			return "", err
		}
	}
	// Attach to every client network; a network created after the gateway
	// started is joined here.
	var clients []db.EgressClient
	s.db.Find(&clients)
	for _, c := range clients {
		if err := s.docker.EnsureNetwork(ctx, c.Network, c.Subnet, c.ID); err != nil {
			return "", err
		}
		if err := s.docker.NetworkConnect(ctx, c.Network, id); err != nil {
			return "", err
		}
	}
	return id, nil
}

// ClientNetwork gives a client container its own internal network and the
// gateway it reaches the internet through. Nil is returned when the filter is
// off (today's unfiltered network); an error when it is on but could not be
// set up, so the caller refuses to start an unfiltered client.
func (s *Service) ClientNetwork(ctx context.Context, kind, botID, connectorID, name string) (*dockerx.ClientNet, error) {
	if !s.Enabled() {
		return nil, nil
	}
	row, addr, err := s.ensureClient(ctx, kind, botID, connectorID, name)
	if err != nil {
		s.setProblem(err)
		return nil, err
	}
	s.setProblem(nil)
	s.Apply()
	return &dockerx.ClientNet{
		Network: row.Network,
		DNS:     addr,
		CPURL:   "http://" + addr + ":" + cpLinkPort(),
	}, nil
}

func (s *Service) lockClient(key string) func() {
	v, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// ensureClient finds or creates a client row, carves its subnet, makes its
// network and returns the gateway's address on it.
func (s *Service) ensureClient(ctx context.Context, kind, botID, connectorID, name string) (*db.EgressClient, string, error) {
	unlock := s.lockClient(kind + ":" + botID + ":" + connectorID)
	defer unlock()

	var row db.EgressClient
	q := s.db.Where("kind = ? AND bot_id = ? AND connector_id = ?", kind, botID, connectorID)
	if err := q.Limit(1).Find(&row).Error; err != nil {
		return nil, "", err
	}
	if row.ID == "" {
		taken := map[string]bool{}
		for _, u := range s.clientRows() {
			taken[u.Subnet] = true
		}
		// A network left behind by a lost row still holds its subnet on the
		// engine; never hand the same one out twice.
		if nets, err := s.docker.ListClientNetworks(ctx); err == nil {
			for _, n := range nets {
				if n.Subnet != "" {
					taken[n.Subnet] = true
				}
			}
		}
		subnet, err := s.carve(taken)
		if err != nil {
			return nil, "", err
		}
		row = db.EgressClient{
			ID:          ids.New(),
			Kind:        kind,
			BotID:       botID,
			ConnectorID: connectorID,
			Name:        name,
			Subnet:      subnet,
			Network:     networkName(kind, botID, connectorID),
		}
		if err := s.db.Create(&row).Error; err != nil {
			return nil, "", err
		}
	}
	if err := s.docker.EnsureNetwork(ctx, row.Network, row.Subnet, row.ID); err != nil {
		return nil, "", err
	}
	gid, err := s.ensureGateway(ctx)
	if err != nil {
		return nil, "", err
	}
	addr, err := s.docker.NetworkAddr(ctx, gid, row.Network)
	if err != nil {
		return nil, "", err
	}
	if addr == "" {
		return nil, "", errors.New("the gateway has no address on the client network")
	}
	return &row, addr, nil
}

// networkName is a client's internal network name.
func networkName(kind, botID, connectorID string) string {
	if kind == "stdio" {
		return "silo-egress-mcp-" + connectorID
	}
	return "silo-egress-bot-" + botID
}

// carve returns the lowest free /24 in the pool.
func (s *Service) carve(taken map[string]bool) (string, error) {
	pool := s.cfg().EgressPool()
	if !pool.Addr().Is4() {
		return "", errors.New("egress.subnet_pool must be IPv4")
	}
	block := uint32(1) << (32 - config.EgressSubnetBits)
	base := pool.Masked().Addr().As4()
	start := binary.BigEndian.Uint32(base[:])
	size := uint32(1) << (32 - pool.Bits())
	for off := uint32(0); off+block <= size; off += block {
		v := start + off
		p := netip.PrefixFrom(netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), config.EgressSubnetBits)
		if !taken[p.String()] {
			return p.String(), nil
		}
	}
	return "", errors.New("the egress subnet pool is exhausted")
}

// caPEM returns the installation's CA, generating one on first use.
func (s *Service) caPEM() ([]byte, []byte, error) {
	var row db.EgressCA
	s.db.Where("id = ?", caID).Limit(1).Find(&row)
	if row.ID != "" {
		return row.CertPEM, row.KeyPEM, nil
	}
	cert, key, err := egress.GenerateCA("Silo network filter", time.Now())
	if err != nil {
		return nil, nil, err
	}
	row = db.EgressCA{ID: caID, CertPEM: cert, KeyPEM: key}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// Rotate makes a new CA and pushes it to the gateway.
func (s *Service) Rotate(ctx context.Context) error {
	cert, key, err := egress.GenerateCA("Silo network filter", time.Now())
	if err != nil {
		return err
	}
	s.db.Where("id = ?", caID).Delete(&db.EgressCA{})
	if err := s.db.Create(&db.EgressCA{ID: caID, CertPEM: cert, KeyPEM: key}).Error; err != nil {
		return err
	}
	s.Apply()
	return nil
}

// clientRows returns every client the CP knows.
func (s *Service) clientRows() []db.EgressClient {
	var out []db.EgressClient
	s.db.Order("created_at").Find(&out)
	return out
}

// renderApply builds the whole desired state.
func (s *Service) renderApply() (*v1.EgressApply, error) {
	cert, key, err := s.caPEM()
	if err != nil {
		return nil, err
	}
	clients := s.clientRows()
	apply := &v1.EgressApply{
		Operator: s.operatorLists(),
		CaCert:   cert,
		CaKey:    key,
		CpAddr:   s.cpAddr(),
		Feeds:    s.feeds(),
	}
	seenBot := map[string]bool{}
	for _, c := range clients {
		apply.Clients = append(apply.Clients, &v1.EgressClient{
			Subnet:      c.Subnet,
			Kind:        c.Kind,
			BotId:       c.BotID,
			ConnectorId: c.ConnectorID,
			Name:        c.Name,
		})
		if c.BotID != "" && !seenBot[c.BotID] {
			seenBot[c.BotID] = true
			apply.Bots = append(apply.Bots, s.renderBot(c.BotID))
		}
	}
	return apply, nil
}

// operatorLists is the operator's floor, from silo.yaml.
func (s *Service) operatorLists() *v1.EgressHostLists {
	e := s.cfg().Egress
	return &v1.EgressHostLists{Block: e.Block, Trust: e.Trust, Pass: e.Pass}
}

func (s *Service) feeds() []*v1.EgressFeed {
	var out []*v1.EgressFeed
	for _, f := range s.cfg().EgressFeeds() {
		if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.URL) == "" {
			continue
		}
		out = append(out, &v1.EgressFeed{Id: f.ID, Url: f.URL, Format: f.Format})
	}
	return out
}

// renderBot is what the gateway needs about one Bot: its surrogate secrets
// (the box holds the stand-in; the gateway swaps in the real value for the
// allowed hosts and the stand-in back in their answers) and the values of its
// plain secrets (seeing one leave is a flag). A channel's own secrets are the
// CP's, never the Bot's, so they are left out.
func (s *Service) renderBot(botID string) *v1.EgressBot {
	var secrets []db.Secret
	s.db.Where("bot_id = ? AND name NOT LIKE ?", botID, "channel.%").Find(&secrets)
	bot := &v1.EgressBot{BotId: botID}
	for _, sec := range secrets {
		if sec.Value == "" {
			continue
		}
		if sec.Mode != "plain" && sec.Surrogate != "" {
			hosts := secretHosts(sec.Hosts)
			if len(hosts) == 0 {
				continue
			}
			bot.Surrogates = append(bot.Surrogates, &v1.EgressSurrogate{
				Name: sec.Name, Surrogate: sec.Surrogate, Real: sec.Value, Hosts: hosts,
			})
			continue
		}
		bot.PlainSecrets = append(bot.PlainSecrets, sec.Value)
	}
	return bot
}

// secretHosts reads a secret's stored JSON host list, or nil.
func secretHosts(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// Apply pushes a fresh apply to a connected gateway; a gateway that is away
// gets one on its next connect.
func (s *Service) Apply() {
	if s == nil {
		return
	}
	sess := s.hub.get()
	if sess == nil {
		return
	}
	apply, err := s.renderApply()
	if err != nil {
		s.setProblem(err)
		return
	}
	sess.push(&v1.EgressDown{Body: &v1.EgressDown_Apply{Apply: apply}})
}

// Drop removes a Bot's client network (DeleteBot). The container must be gone
// first.
func (s *Service) Drop(ctx context.Context, botID string) {
	if s == nil {
		return
	}
	var rows []db.EgressClient
	s.db.Where("bot_id = ?", botID).Find(&rows)
	for _, r := range rows {
		s.dropClient(ctx, &r)
	}
	s.Apply()
}

// DropConnector removes a STDIO sidecar's client network.
func (s *Service) DropConnector(ctx context.Context, connectorID string) {
	if s == nil {
		return
	}
	var rows []db.EgressClient
	s.db.Where("connector_id = ?", connectorID).Find(&rows)
	for _, r := range rows {
		s.dropClient(ctx, &r)
	}
	s.Apply()
}

func (s *Service) dropClient(ctx context.Context, r *db.EgressClient) {
	if s.docker != nil {
		if gid, _ := s.docker.EgressID(ctx); gid != "" {
			_ = s.docker.NetworkDisconnect(ctx, r.Network, gid)
		}
		_ = s.docker.DropNetwork(ctx, r.Network)
	}
	s.db.Where("id = ?", r.ID).Delete(&db.EgressClient{})
}

// Reconcile reclaims client rows and networks whose Bot or connector is gone,
// and makes sure the gateway is up.
func (s *Service) Reconcile(ctx context.Context) {
	if !s.Enabled() || s.db == nil {
		return
	}
	rows := s.clientRows()
	live := map[string]bool{}
	for i := range rows {
		r := &rows[i]
		live[r.Network] = true
		gone := false
		switch r.Kind {
		case "bot":
			var n int64
			s.db.Model(&db.Bot{}).Where("id = ?", r.BotID).Count(&n)
			gone = n == 0
		case "stdio":
			var n int64
			s.db.Model(&db.BotConnector{}).Where("id = ?", r.ConnectorID).Count(&n)
			gone = n == 0
		}
		if gone {
			delete(live, r.Network)
			s.dropClient(ctx, r)
		}
	}
	gid, _ := s.docker.EgressID(ctx)
	if nets, err := s.docker.ListClientNetworks(ctx); err == nil {
		// The gateway must let go of an orphan network before it can go.
		s.detach(ctx, gid, nets, live)
	}
	// A CP with no clients needs no gateway: remove it (and any network left
	// behind by a lost row), so a fresh store never inherits a stale one.
	if len(live) == 0 {
		for _, n := range s.listNetworks(ctx) {
			_ = s.docker.DropNetwork(ctx, n.Name)
		}
		if gid != "" {
			s.docker.DropEgress(ctx, gid)
		}
		return
	}
	if err := s.Ensure(ctx); err != nil {
		log.Printf("egress: %v", err)
	}
}

func (s *Service) listNetworks(ctx context.Context) []dockerx.NetworkInfo {
	nets, err := s.docker.ListClientNetworks(ctx)
	if err != nil {
		return nil
	}
	return nets
}

// detach lets the gateway go of every network that is no longer a live
// client's, then removes it.
func (s *Service) detach(ctx context.Context, gid string, nets []dockerx.NetworkInfo, live map[string]bool) {
	for _, n := range nets {
		if live[n.Name] {
			continue
		}
		if gid != "" {
			_ = s.docker.NetworkDisconnect(ctx, n.Name, gid)
		}
		_ = s.docker.DropNetwork(ctx, n.Name)
	}
}

// hub holds the one live gateway session.
type hub struct {
	mu sync.Mutex
	s  *session
}

func (h *hub) get() *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.s
	if s != nil {
		select {
		case <-s.done:
			h.s = nil
			return nil
		default:
		}
	}
	return s
}

func (h *hub) put(s *session) {
	h.mu.Lock()
	old := h.s
	h.s = s
	h.mu.Unlock()
	if old != nil {
		old.close()
	}
}

func (h *hub) remove(s *session) {
	h.mu.Lock()
	if h.s == s {
		h.s = nil
	}
	h.mu.Unlock()
	s.close()
}

type session struct {
	send chan *v1.EgressDown
	done chan struct{}
	once sync.Once
}

func (s *session) close() { s.once.Do(func() { close(s.done) }) }

func (s *session) push(m *v1.EgressDown) bool {
	select {
	case s.send <- m:
		return true
	case <-s.done:
		return false
	}
}

// Session is the EgressHost endpoint the gateway dials.
func (s *Service) Session(ctx context.Context, stream *connect.BidiStream[v1.EgressUp, v1.EgressDown]) error {
	if _, ok := ctx.Value(hostKey).(*db.EgressHost); !ok {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	sess := &session{send: make(chan *v1.EgressDown, 8), done: make(chan struct{})}
	h := s.hub
	h.put(sess)
	defer h.remove(sess)

	if apply, err := s.renderApply(); err == nil {
		sess.send <- &v1.EgressDown{Body: &v1.EgressDown_Apply{Apply: apply}}
	}
	s.setProblem(nil)

	errc := make(chan error, 2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			case <-sess.done:
				errc <- nil
				return
			case m := <-sess.send:
				if err := stream.Send(m); err != nil {
					errc <- err
					return
				}
			}
		}
	}()
	go func() {
		for {
			up, err := stream.Receive()
			if err != nil {
				errc <- err
				return
			}
			s.egressUp(up)
		}
	}()
	return <-errc
}

// egressUp handles one frame from the gateway.
func (s *Service) egressUp(up *v1.EgressUp) {
	switch b := up.GetBody().(type) {
	case *v1.EgressUp_Events:
		if s.OnEvents != nil && len(b.Events.GetEvents()) > 0 {
			s.OnEvents(b.Events.GetEvents())
		}
	case *v1.EgressUp_Traffic:
		if s.OnTraffic != nil && len(b.Traffic.GetRows()) > 0 {
			s.OnTraffic(b.Traffic.GetRows())
		}
	case *v1.EgressUp_Health:
		h := b.Health
		s.db.Model(&db.EgressHost{}).Where("id = ?", gatewayID).
			Updates(map[string]any{"networks": h.GetNetworks(), "healthy_at": time.Now()})
	}
}

type hostCtxKey struct{}

var hostKey = hostCtxKey{}

// Intercept authenticates a gateway session by its token.
func (s *Service) Intercept(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		host, _, err := access.RowByToken[db.EgressHost](s.db, "token_hash", conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return err
		}
		return next(context.WithValue(ctx, hostKey, host), conn)
	}
}
