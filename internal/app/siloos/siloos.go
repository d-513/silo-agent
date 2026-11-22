// Package siloos is the control plane's side of SiloOS: Admin's SiloOS page,
// whose RPCs (the SiloOS* ones of the UI service) are an instance admin's
// and are passed to siloosd on the machine (siloos.socket, internal/siloosd),
// and the HTTPS proxy, Caddy, which it configures through Caddy's admin API
// (caddy.admin, internal/caddy) on SiloOS or anywhere else.
//
// Nothing here is a Bot tool, and nothing ever may be: what siloosd does is
// the host's network, updates and power.
package siloos

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/app/access"
	"silo.agent/internal/config"
	"silo.agent/internal/siloosd"
)

// Tick is how often the proxy's configuration is compared with Caddy's.
const Tick = 15 * time.Second

// Service is the SiloOS domain.
type Service struct {
	db  *gorm.DB
	cfg func() config.Config

	mu     sync.Mutex
	path   string
	client silov1connect.SiloOSDClient
	proxy  *v1.ProxyStatus
	// local are the machine's names and addresses as siloosd last gave
	// them, at localAt; rootCA Caddy's authority's root, once fetched.
	local   []string
	localAt time.Time
	rootCA  string
}

// New makes the service.
func New(gdb *gorm.DB, cfg func() config.Config) *Service {
	return &Service{db: gdb, cfg: cfg, proxy: &v1.ProxyStatus{}}
}

// daemon is the siloosd client, or nil off SiloOS.
func (s *Service) daemon() silov1connect.SiloOSDClient {
	path := strings.TrimSpace(s.cfg().SiloOS.Socket)
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == "" {
		s.path, s.client = "", nil
		return nil
	}
	if path != s.path {
		s.path, s.client = path, siloosd.Dial(path)
	}
	return s.client
}

var errNotSiloOS = connect.NewError(connect.CodeFailedPrecondition, errors.New("Silo is not running on SiloOS here."))

// call checks the caller is an instance admin and passes the request to
// siloosd. siloosd not answering is FailedPrecondition, never Unavailable,
// which the console reads as the control plane being down.
func call[Req, Res any](ctx context.Context, s *Service, req *connect.Request[Req], fn func(silov1connect.SiloOSDClient, context.Context, *connect.Request[Req]) (*connect.Response[Res], error)) (*connect.Response[Res], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	d := s.daemon()
	if d == nil {
		return nil, errNotSiloOS
	}
	res, err := fn(d, ctx, connect.NewRequest(req.Msg))
	if err != nil {
		return nil, daemonError(err)
	}
	return connect.NewResponse(res.Msg), nil
}

func daemonError(err error) error {
	switch connect.CodeOf(err) {
	case connect.CodeInvalidArgument, connect.CodeFailedPrecondition, connect.CodeInternal:
		var ce *connect.Error
		if errors.As(err, &ce) {
			return connect.NewError(ce.Code(), errors.New(ce.Message()))
		}
	}
	return connect.NewError(connect.CodeFailedPrecondition, errors.New("siloosd is not answering: "+err.Error()))
}

func (s *Service) GetSiloOS(ctx context.Context, _ *connect.Request[v1.GetSiloOSRequest]) (*connect.Response[v1.SiloOSStatus], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	out := &v1.SiloOSStatus{Proxy: s.ProxyStatus()}
	d := s.daemon()
	if d == nil {
		return connect.NewResponse(out), nil
	}
	out.Available = true
	res, err := d.GetSystem(ctx, connect.NewRequest(&v1.GetSystemRequest{}))
	if err != nil {
		out.Error = daemonError(err).(*connect.Error).Message()
	} else {
		out.System = res.Msg
	}
	return connect.NewResponse(out), nil
}

func (s *Service) SetSiloOSHostname(ctx context.Context, req *connect.Request[v1.SetHostnameRequest]) (*connect.Response[v1.SystemInfo], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.SetHostname)
}

func (s *Service) SetSiloOSTime(ctx context.Context, req *connect.Request[v1.SetTimeRequest]) (*connect.Response[v1.SystemInfo], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.SetTime)
}

func (s *Service) GetSiloOSNetwork(ctx context.Context, req *connect.Request[v1.GetNetworkRequest]) (*connect.Response[v1.Network], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.GetNetwork)
}

func (s *Service) ApplySiloOSConnection(ctx context.Context, req *connect.Request[v1.ApplyConnectionRequest]) (*connect.Response[v1.Network], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.ApplyConnection)
}

func (s *Service) ConfirmSiloOSNetwork(ctx context.Context, req *connect.Request[v1.ConfirmNetworkRequest]) (*connect.Response[v1.Network], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.ConfirmNetwork)
}

func (s *Service) RollBackSiloOSNetwork(ctx context.Context, req *connect.Request[v1.RollBackNetworkRequest]) (*connect.Response[v1.Network], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.RollBackNetwork)
}

func (s *Service) GetSiloOSUpdates(ctx context.Context, req *connect.Request[v1.GetUpdatesRequest]) (*connect.Response[v1.Updates], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.GetUpdates)
}

func (s *Service) RunSiloOSUpdate(ctx context.Context, req *connect.Request[v1.RunUpdateRequest]) (*connect.Response[v1.Updates], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.RunUpdate)
}

func (s *Service) SetSiloOSUpdatePolicy(ctx context.Context, req *connect.Request[v1.SetUpdatePolicyRequest]) (*connect.Response[v1.Updates], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.SetUpdatePolicy)
}

func (s *Service) SetSiloOSUpdateChannel(ctx context.Context, req *connect.Request[v1.SetUpdateChannelRequest]) (*connect.Response[v1.Updates], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.SetUpdateChannel)
}

func (s *Service) SiloOSPower(ctx context.Context, req *connect.Request[v1.PowerRequest]) (*connect.Response[v1.PowerResponse], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.Power)
}

func (s *Service) ReadSiloOSLogs(ctx context.Context, req *connect.Request[v1.ReadLogsRequest]) (*connect.Response[v1.ReadLogsResponse], error) {
	return call(ctx, s, req, silov1connect.SiloOSDClient.ReadLogs)
}
