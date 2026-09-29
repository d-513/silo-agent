package dockerx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// DriveName is a Bot's drive sidecar container.
func DriveName(botID string) string { return "silo-drive-" + botID }

// DriveSpec is one Bot's drive sidecar.
type DriveSpec struct {
	BotID string
	Image string
	Env   []string
	// Root is the drive mount root as the engine sees it; the sidecar binds
	// Root/<BotID> with rshared propagation.
	Root string
	// CacheDir is the durable per-Bot VFS cache on the CP's data dir.
	CacheDir string
}

// DriveContainer is a discovered drive sidecar.
type DriveContainer struct {
	ID    string
	Name  string
	BotID string
}

// rootRe keeps the mount root to a plain absolute path: it is spliced into the
// helper's argv (never a shell line), but a sane path also keeps error text
// readable and rules out surprises like a trailing newline.
var rootRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

// DriveDir is Root/<botID>, the directory that carries a Bot's mounts.
func DriveDir(root, botID string) string { return filepath.Join(root, botID) }

// prepareScript runs in a throwaway privileged helper. It finds the mount
// namespace the engine's containers are created in (the rootless pause
// process, `catatonit -P`, or pid 1 on a rootful engine) and, inside it:
// creates root/<bot>, makes root a shared mount so sidecar mounts propagate
// to the Bot, and labels it container_file_t on SELinux hosts so neither
// container needs a recursive `:z` relabel (which would walk into live FUSE
// mounts, i.e. the whole remote drive). Idempotent; the rootless namespace
// is recreated on VM reboot, so the CP runs it before every sidecar start.
const prepareScript = `set -e
R="$1"; B="$2"
P=1
for p in /proc/[0-9]*; do
  c=$(tr '\0' ' ' < "$p/cmdline" 2>/dev/null) || continue
  case "$c" in "catatonit -P"*) P=${p#/proc/}; break;; esac
done
exec nsenter -t "$P" -m -- sh -c '
set -e
mkdir -p "$0/$1"
mountpoint -q "$0" || mount --bind "$0" "$0"
mount --make-shared "$0"
if command -v chcon >/dev/null 2>&1; then chcon -t container_file_t "$0" "$0/$1" 2>/dev/null || true; fi
' "$R" "$B"
`

// removeScript deletes root/<bot> in the same namespace, refusing while
// anything under it is still mounted.
const removeScript = `set -e
R="$1"; B="$2"
P=1
for p in /proc/[0-9]*; do
  c=$(tr '\0' ' ' < "$p/cmdline" 2>/dev/null) || continue
  case "$c" in "catatonit -P"*) P=${p#/proc/}; break;; esac
done
exec nsenter -t "$P" -m -- sh -c '
set -e
[ -d "$0/$1" ] || exit 0
if grep -q " $0/$1/" /proc/self/mountinfo; then echo "drives still mounted" >&2; exit 1; fi
rm -rf --one-file-system "$0/$1"
' "$R" "$B"
`

// PrepareDriveDir makes root shared in the engine's mount namespace and
// creates root/<botID>.
func (e *Engine) PrepareDriveDir(ctx context.Context, image, root, botID string) error {
	return e.driveHelper(ctx, image, prepareScript, root, botID)
}

// RemoveDriveDir deletes root/<botID> (after the sidecar is gone).
func (e *Engine) RemoveDriveDir(ctx context.Context, image, root, botID string) error {
	return e.driveHelper(ctx, image, removeScript, root, botID)
}

func (e *Engine) driveHelper(ctx context.Context, image, script, root, botID string) error {
	if !rootRe.MatchString(root) || strings.Contains(root, "..") {
		return fmt.Errorf("drives.mount_root %q must be a plain absolute path", root)
	}
	if botID == "" || strings.ContainsAny(botID, "/.") {
		return errors.New("bad bot id")
	}
	resp, err := e.cli.ContainerCreate(ctx, &container.Config{
		Image:      image,
		Entrypoint: []string{"sh", "-c", script, "silo-drive-prepare", root, botID},
		Labels:     map[string]string{"silo.role": "drive-helper"},
	}, &container.HostConfig{
		Privileged:  true,
		PidMode:     "host",
		SecurityOpt: []string{"label=disable"},
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("drive helper: %w", err)
	}
	defer func() {
		_ = e.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	}()
	if err := e.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("drive helper: %w", err)
	}
	waitC, errC := e.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errC:
		return fmt.Errorf("drive helper: %w", err)
	case st := <-waitC:
		if st.StatusCode != 0 {
			return fmt.Errorf("drive helper exited %d: %s", st.StatusCode, e.logs(ctx, resp.ID))
		}
	}
	return nil
}

func (e *Engine) logs(ctx context.Context, id string) string {
	r, err := e.cli.ContainerLogs(ctx, id, container.LogsOptions{ShowStderr: true, ShowStdout: true, Tail: "5"})
	if err != nil {
		return ""
	}
	defer r.Close()
	buf := make([]byte, 2048)
	n, _ := r.Read(buf)
	// Strip the multiplexed stream headers (8 bytes per frame) crudely: keep
	// printable text only.
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || (r >= 32 && r < 127) {
			return r
		}
		return -1
	}, string(buf[:n])))
}

// CreateDrive creates a Bot's drive sidecar. It holds /dev/fuse and
// SYS_ADMIN (the Bot never does) and binds the Bot's drive dir rshared so its
// mounts propagate into the Bot, which binds the same dir rslave.
func (e *Engine) CreateDrive(ctx context.Context, spec DriveSpec) (string, error) {
	if spec.BotID == "" || spec.Image == "" {
		return "", errors.New("drive sidecar needs a bot id and image")
	}
	binds := []string{DriveDir(spec.Root, spec.BotID) + ":/mnt/drives:rshared"}
	if spec.CacheDir != "" {
		if err := os.MkdirAll(spec.CacheDir, 0o700); err != nil {
			return "", err
		}
		abs, err := filepath.Abs(spec.CacheDir)
		if err != nil {
			return "", err
		}
		binds = append(binds, abs+":/cache")
	}
	resp, err := e.cli.ContainerCreate(ctx, &container.Config{
		Image:    spec.Image,
		Env:      spec.Env,
		Hostname: "drives",
		Labels: map[string]string{
			"silo.role":   "drive",
			"silo.bot_id": spec.BotID,
		},
	}, &container.HostConfig{
		Binds:         binds,
		CapAdd:        []string{"SYS_ADMIN"},
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		ExtraHosts:    []string{"host.containers.internal:host-gateway"},
		Resources: container.Resources{Devices: []container.DeviceMapping{
			{PathOnHost: "/dev/fuse", PathInContainer: "/dev/fuse", CgroupPermissions: "rwm"},
		}},
	}, nil, nil, DriveName(spec.BotID))
	if err != nil {
		return "", fmt.Errorf("drive sidecar create: %w", err)
	}
	return resp.ID, nil
}

func (e *Engine) DropDrive(ctx context.Context, botID, containerID string) {
	_ = e.Remove(ctx, containerID)
	if botID != "" {
		_ = e.Remove(ctx, DriveName(botID))
	}
}

// ListDrives returns every drive sidecar, for reclaiming orphans.
func (e *Engine) ListDrives(ctx context.Context) ([]DriveContainer, error) {
	items, err := e.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "silo.role=drive")),
	})
	if err != nil {
		return nil, err
	}
	out := make([]DriveContainer, 0, len(items))
	for _, c := range items {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, DriveContainer{ID: c.ID, Name: name, BotID: c.Labels["silo.bot_id"]})
	}
	return out, nil
}

// botDriveBind prepares the Bot's drive dir and returns its bind, or "" when
// drives cannot be offered here (no drive image yet, or the engine refused
// the shared mount). A Bot without the bind still works; it sees drives after
// a Reset once the problem is fixed.
func (e *Engine) botDriveBind(ctx context.Context, botID string) string {
	cfg := e.store.Config()
	root := cfg.DriveMountRoot()
	if err := e.PrepareDriveDir(ctx, cfg.DriveImage(), root, botID); err != nil {
		fmt.Fprintf(os.Stderr, "silo: drives unavailable for bot %s: %v\n", botID, err)
		return ""
	}
	return DriveDir(root, botID) + ":/workspace/drives:rslave"
}

// HasDriveBind reports whether a Bot container was created with the drive
// bind, so the UI can ask for a Reset instead of failing silently.
func (e *Engine) HasDriveBind(ctx context.Context, id string) (bool, error) {
	c, err := e.cli.ContainerInspect(ctx, id)
	if err != nil {
		return false, err
	}
	for _, m := range c.Mounts {
		if m.Destination == "/workspace/drives" {
			return true, nil
		}
	}
	return false, nil
}
