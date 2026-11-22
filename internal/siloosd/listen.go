package siloosd

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"silo.agent/gen/silo/v1/silov1connect"
)

// Peer is the process at the other end of the socket.
type Peer struct{ UID, PID int }

func (p Peer) String() string { return strconv.Itoa(p.UID) + ", pid " + strconv.Itoa(p.PID) }

type peerKey struct{}

// PeerFrom is the caller of a request Serve handed on.
func PeerFrom(ctx context.Context) (Peer, bool) {
	p, ok := ctx.Value(peerKey{}).(Peer)
	return p, ok
}

// Listen opens the socket at path, readable and writable by its owner and
// gid only (0660); a negative gid leaves the group as it is. A stale socket
// from a previous run is removed first.
func Listen(path string, gid int) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if gid >= 0 {
		if err := os.Chown(path, -1, gid); err != nil {
			ln.Close()
			return nil, err
		}
	}
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Serve answers the SiloOSD service on ln until ctx ends. A connection from a
// uid not in allow is closed before a byte is read: the socket's mode is the
// first wall, this the second.
func Serve(ctx context.Context, ln net.Listener, s *Server, allow []int) error {
	path, h := silov1connect.NewSiloOSDHandler(s)
	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if p, err := peerOf(c); err == nil {
				return context.WithValue(ctx, peerKey{}, p)
			}
			return ctx
		},
	}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	err := srv.Serve(&guarded{Listener: ln, allow: allow})
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// guarded is a listener that drops connections from uids it was not given.
type guarded struct {
	net.Listener
	allow []int
}

func (g *guarded) Accept() (net.Conn, error) {
	for {
		c, err := g.Listener.Accept()
		if err != nil {
			return nil, err
		}
		p, err := peerOf(c)
		if err != nil {
			log.Printf("siloosd: refused a connection: %v", err)
			c.Close()
			continue
		}
		if !slices.Contains(g.allow, p.UID) {
			log.Printf("siloosd: refused uid %d (pid %d)", p.UID, p.PID)
			c.Close()
			continue
		}
		return c, nil
	}
}
