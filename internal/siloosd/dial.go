package siloosd

import (
	"context"
	"net"
	"net/http"
	"time"

	"silo.agent/gen/silo/v1/silov1connect"
)

// Dial is a client of the siloosd at the unix socket path. It connects per
// call, so a siloosd restarted meanwhile is simply reached again.
func Dial(path string) silov1connect.SiloOSDClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
		MaxIdleConns:    2,
		IdleConnTimeout: 30 * time.Second,
	}
	return silov1connect.NewSiloOSDClient(&http.Client{Transport: tr, Timeout: 2 * time.Minute}, "http://siloosd")
}
