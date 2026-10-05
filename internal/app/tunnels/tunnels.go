// Package tunnels gives a Bot's localhost services public addresses: a
// declared tunnel is a name (a subdomain of tunnels.host), a port in the box,
// and who may open it. The Control Plane answers <name>.<tunnels.host> by
// reverse-proxying over a connection the worker opens into the box.
package tunnels

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/host"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/tunnel"
)

// MaxPerBot caps the tunnels one Bot may declare.
const MaxPerBot = 20

// State values of ListTunnelsResponse.state.
const (
	StateOK     = "ok"
	StateOff    = "off"     // tunnels.enabled is false
	StateNoHost = "no_host" // enabled, but no domain to serve from
)

// Host is what tunnels need from the App around them.
type Host interface {
	host.Authorizer
	host.Runs
}

// Service owns tunnels: their rows, the proxy that serves them, their RPCs and
// the Bot's tools.
type Service struct {
	db   *gorm.DB
	hub  *hub.Hub
	cfg  func() config.Config
	host Host
	px   proxyState
}

func New(gdb *gorm.DB, h *hub.Hub, cfg func() config.Config, host Host) *Service {
	return &Service{db: gdb, hub: h, cfg: cfg, host: host}
}

// State reports whether tunnels can be used, and the domain they live under.
func State(c config.Config) (state, domain string) {
	domain = c.TunnelHost()
	switch {
	case !c.Tunnels.Enabled:
		return StateOff, domain
	case domain == "":
		return StateNoHost, ""
	}
	return StateOK, domain
}

// usable is nil when tunnels can be declared and served right now.
func (s *Service) usable() error {
	switch st, _ := State(s.cfg()); st {
	case StateOff:
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("tunnels are turned off by the operator (tunnels.enabled)"))
	case StateNoHost:
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("tunnels have no domain yet: the operator sets tunnels.host"))
	}
	return nil
}

// Declare adds a tunnel to port on the Bot, or returns the one it already has
// (created=false). public is applied only when the tunnel is new; changing an
// existing one is SetPublic. createdBy is "owner" or "bot".
func (s *Service) Declare(botID string, port int, public bool, createdBy string) (t *db.Tunnel, created bool, err error) {
	if err := s.usable(); err != nil {
		return nil, false, err
	}
	if err := tunnel.CheckPort(port); err != nil {
		return nil, false, connect.NewError(connect.CodeInvalidArgument, err)
	}
	for range 6 {
		if t, err := s.byPort(botID, port); err != nil {
			return nil, false, err
		} else if t != nil {
			return t, false, nil
		}
		var n int64
		if err := s.db.Model(&db.Tunnel{}).Where("bot_id = ?", botID).Count(&n).Error; err != nil {
			return nil, false, err
		}
		if n >= MaxPerBot {
			return nil, false, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("a Bot can have at most %d tunnels; delete one first", MaxPerBot))
		}
		row := db.Tunnel{ID: ids.New(), BotID: botID, Port: port, Name: NewName(), Public: public, CreatedBy: createdBy, CreatedAt: time.Now()}
		// A name taken meanwhile (or the port, by a concurrent call) fails the
		// unique index; the next pass picks a new name or finds the port's row.
		if err := s.db.Create(&row).Error; err == nil {
			return &row, true, nil
		}
	}
	return nil, false, errors.New("could not allocate a tunnel name")
}

func (s *Service) byPort(botID string, port int) (*db.Tunnel, error) {
	var t db.Tunnel
	res := s.db.Where("bot_id = ? AND port = ?", botID, port).Limit(1).Find(&t)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &t, nil
}

// ByName finds a tunnel by its subdomain; nil when there is none.
func (s *Service) ByName(name string) (*db.Tunnel, error) {
	var t db.Tunnel
	res := s.db.Where("name = ?", name).Limit(1).Find(&t)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &t, nil
}

// ForBot lists a Bot's tunnels, oldest first.
func (s *Service) ForBot(botID string) ([]db.Tunnel, error) {
	var rows []db.Tunnel
	err := s.db.Where("bot_id = ?", botID).Order("created_at, id").Find(&rows).Error
	return rows, err
}

// SetPublic changes who may open a tunnel. Grants are not touched: they only
// matter while the tunnel is private, and each is re-checked against the Bot's
// owner on every request.
func (s *Service) SetPublic(t *db.Tunnel, public bool) error {
	if err := s.db.Model(t).Update("public", public).Error; err != nil {
		return err
	}
	t.Public = public
	return nil
}

// Delete removes a tunnel and every grant for it.
func (s *Service) Delete(t *db.Tunnel) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tunnel_id = ?", t.ID).Delete(&db.TunnelGrant{}).Error; err != nil {
			return err
		}
		return tx.Delete(t).Error
	})
}

// Drop removes a Bot's tunnels and their grants (DeleteBot).
func (s *Service) Drop(botID string) {
	s.db.Where("tunnel_id IN (?)", s.db.Model(&db.Tunnel{}).Select("id").Where("bot_id = ?", botID)).Delete(&db.TunnelGrant{})
	s.db.Where("bot_id = ?", botID).Delete(&db.Tunnel{})
}

func (s *Service) proto(t *db.Tunnel) *v1.Tunnel {
	out := &v1.Tunnel{
		Id: t.ID, Name: t.Name, Port: int32(t.Port), Public: t.Public, CreatedBy: t.CreatedBy,
		CreatedAt: t.CreatedAt.Format(time.RFC3339),
	}
	if c := s.cfg(); c.TunnelHost() != "" {
		out.Url = c.TunnelURL(t.Name)
	}
	if t.LastUsedAt != nil {
		out.LastUsedAt = t.LastUsedAt.Format(time.RFC3339)
	}
	return out
}

func (s *Service) ListTunnels(ctx context.Context, req *connect.Request[v1.ListTunnelsRequest]) (*connect.Response[v1.ListTunnelsResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	rows, err := s.ForBot(req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	state, domain := State(s.cfg())
	out := &v1.ListTunnelsResponse{State: state, Host: domain, Max: MaxPerBot}
	for i := range rows {
		out.Tunnels = append(out.Tunnels, s.proto(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) CreateTunnel(ctx context.Context, req *connect.Request[v1.CreateTunnelRequest]) (*connect.Response[v1.Tunnel], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	t, created, err := s.Declare(req.Msg.GetBotId(), int(req.Msg.GetPort()), req.Msg.GetPublic(), "owner")
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("port %d already has the tunnel %s", t.Port, t.Name))
	}
	return connect.NewResponse(s.proto(t)), nil
}

func (s *Service) UpdateTunnel(ctx context.Context, req *connect.Request[v1.UpdateTunnelRequest]) (*connect.Response[v1.Tunnel], error) {
	t, err := access.OwnBotRow[db.Tunnel](ctx, s.db, req.Msg.GetBotId(), req.Msg.GetId(), "tunnel")
	if err != nil {
		return nil, err
	}
	if err := s.SetPublic(t, req.Msg.GetPublic()); err != nil {
		return nil, err
	}
	return connect.NewResponse(s.proto(t)), nil
}

func (s *Service) DeleteTunnel(ctx context.Context, req *connect.Request[v1.DeleteTunnelRequest]) (*connect.Response[v1.DeleteTunnelResponse], error) {
	t, err := access.OwnBotRow[db.Tunnel](ctx, s.db, req.Msg.GetBotId(), req.Msg.GetId(), "tunnel")
	if err != nil {
		return nil, err
	}
	if err := s.Delete(t); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.DeleteTunnelResponse{}), nil
}
