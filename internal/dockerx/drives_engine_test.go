//go:build engine

// Real-engine check of the drive plumbing: the prepare helper, the drive
// sidecar image (the real silo-drive guest), mount propagation into a
// Bot-like container through the exact bind Engine.Create uses, and cleanup.
// It needs a running engine and `make drive-image`:
//
//	SILO_TEST_DOCKER_HOST=unix:///…/podman.sock go test -tags engine -run TestDriveEngine ./internal/dockerx/
package dockerx

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/config"
)

type fakeCP struct {
	silov1connect.UnimplementedDriveHostHandler
	status chan *v1.DriveStatus
}

func (f *fakeCP) Session(ctx context.Context, s *connect.BidiStream[v1.DriveUp, v1.DriveDown]) error {
	if s.RequestHeader().Get("Authorization") != "Bearer test-token" {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	if err := s.Send(&v1.DriveDown{Body: &v1.DriveDown_Apply{Apply: &v1.DriveApply{Drives: []*v1.DriveSpec{{
		Id: "d1", Dir: "mem", Remote: "mem", Env: map[string]string{"RCLONE_CONFIG_MEM_TYPE": "memory"},
	}}}}}); err != nil {
		return err
	}
	for {
		up, err := s.Receive()
		if err != nil {
			return err
		}
		if st := up.GetStatus(); st != nil {
			f.status <- st
		}
	}
}

func TestDriveEngine(t *testing.T) {
	host := os.Getenv("SILO_TEST_DOCKER_HOST")
	if host == "" {
		t.Skip("SILO_TEST_DOCKER_HOST not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	cp := &fakeCP{status: make(chan *v1.DriveStatus, 32)}
	mux := http.NewServeMux()
	mux.Handle(silov1connect.NewDriveHostHandler(cp))
	srv := &http.Server{Handler: h2c.NewHandler(mux, &http2.Server{})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	root := "/var/tmp/silo-drives-enginetest"
	store, err := config.FromYAML([]byte(fmt.Sprintf("docker_host: %q\ndata_dir: %q\ndrives:\n  mount_root: %s\n", host, t.TempDir(), root)))
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	bot := fmt.Sprintf("enginetest%d", time.Now().UnixNano()%1e6)

	// The Bot side: exactly the bind Engine.Create adds.
	bind := e.botDriveBind(ctx, bot)
	if bind == "" {
		t.Fatal("prepare helper failed (is localhost/silo-drive:v1 built?)")
	}
	botC, err := e.cli.ContainerCreate(ctx, &container.Config{
		Image: "docker.io/library/alpine:latest", Entrypoint: []string{"sleep", "600"},
	}, &container.HostConfig{Binds: []string{bind}}, nil, nil, "silo-"+bot)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Remove(context.Background(), botC.ID)
	if err := e.cli.ContainerStart(ctx, botC.ID, container.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.HasDriveBind(ctx, botC.ID); err != nil || !ok {
		t.Fatalf("HasDriveBind = %v, %v", ok, err)
	}

	cid, err := e.CreateDrive(ctx, DriveSpec{
		BotID: bot, Image: store.Config().DriveImage(), Root: root, CacheDir: t.TempDir(),
		Env: []string{fmt.Sprintf("SILO_CP_URL=http://host.containers.internal:%d", port), "SILO_DRIVE_TOKEN=test-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.DropDrive(context.Background(), bot, cid)
	if err := e.Start(ctx, cid); err != nil {
		t.Fatal(err)
	}

	for mounted := false; !mounted; {
		select {
		case st := <-cp.status:
			t.Logf("status %s %s %s", st.GetId(), st.GetState(), st.GetDetail())
			if st.GetState() == "error" {
				t.Fatalf("mount failed: %s", st.GetDetail())
			}
			mounted = st.GetState() == "mounted"
		case <-ctx.Done():
			t.Fatal("never mounted")
		}
	}

	for _, id := range []string{botC.ID, cid} {
		c, _ := e.cli.ContainerInspect(ctx, id)
		for _, m := range c.Mounts {
			t.Logf("%s mount %s -> %s propagation=%q", c.Name, m.Source, m.Destination, m.Propagation)
		}
	}
	t.Logf("bot mountinfo: %s", execAs(t, e, botC.ID, "0", "grep drives /proc/self/mountinfo"))
	t.Logf("sidecar mountinfo: %s", execAs(t, e, cid, "0", "grep drives /proc/self/mountinfo"))
	out := execAs(t, e, botC.ID, "1000", "echo from-bot > /workspace/drives/mem/hello.txt && cat /workspace/drives/mem/hello.txt && stat -c %u /workspace/drives/mem/hello.txt")
	if !strings.Contains(out, "from-bot") || !strings.Contains(out, "1000") {
		t.Fatalf("bot view of the drive: %q", out)
	}

	// Stopping the sidecar unmounts cleanly: the Bot sees an empty folder, not a
	// hung FUSE mount.
	if err := e.Stop(ctx, cid); err != nil {
		t.Fatal(err)
	}
	out = execAs(t, e, botC.ID, "1000", "ls -A /workspace/drives/mem 2>&1; echo rc=$?")
	if strings.Contains(out, "hello.txt") || strings.Contains(out, "not connected") {
		t.Fatalf("after stop: %q", out)
	}

	e.DropDrive(ctx, bot, cid)
	_ = e.Remove(ctx, botC.ID)
	if err := e.RemoveDriveDir(ctx, store.Config().DriveImage(), root, bot); err != nil {
		t.Fatalf("RemoveDriveDir: %v", err)
	}
}

func execAs(t *testing.T, e *Engine, id, user, cmd string) string {
	t.Helper()
	ctx := context.Background()
	ex, err := e.cli.ContainerExecCreate(ctx, id, container.ExecOptions{User: user, Cmd: []string{"sh", "-c", cmd}, AttachStdout: true, AttachStderr: true})
	if err != nil {
		t.Fatal(err)
	}
	att, err := e.cli.ContainerExecAttach(ctx, ex.ID, container.ExecAttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer att.Close()
	var so, se bytes.Buffer
	_, _ = stdcopy.StdCopy(&so, &se, att.Reader)
	return so.String() + se.String()
}
