// siloosd is the SiloOS host daemon: the one way Silo changes the machine it
// runs on (internal/siloosd, proto/silo/v1/siloos.proto). It runs as root
// under systemd (images/os/rootfs/usr/lib/systemd/system/siloosd.service) and
// answers on a unix socket only root and the silo user may open.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"silo.agent/internal/siloosd"
	"silo.agent/internal/version"
)

func main() {
	socket := flag.String("socket", "/run/siloosd/siloosd.sock", "the unix socket to listen on")
	state := flag.String("state", "/var/lib/siloosd", "where the update policy and a pending network change are kept")
	allow := flag.String("user", "silo", "the user (besides root) whose processes may call siloosd; the socket is given to its group")
	flag.Parse()
	log.SetFlags(0)

	u, err := user.Lookup(*allow)
	if err != nil {
		log.Fatalf("siloosd: %v", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := siloosd.New(siloosd.Config{StateDir: *state})
	ln, err := siloosd.Listen(*socket, gid)
	if err != nil {
		log.Fatalf("siloosd: %v", err)
	}
	log.Printf("siloosd %s: listening on %s for root and %s (uid %d)", version.Version, *socket, *allow, uid)
	go srv.Run(ctx)
	if err := siloosd.Serve(ctx, ln, srv, []int{0, uid}); err != nil {
		log.Fatalf("siloosd: %v", err)
	}
}
