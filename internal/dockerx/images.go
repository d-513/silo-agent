package dockerx

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"

	"silo.agent/internal/config"
)

// SpecLabel records how Silo made a container (specOf), so a change to that
// is seen on a container made before it.
const SpecLabel = "silo.spec"

// specs is the generation of how each role's container is made. Bump a
// role's number by hand when a change to its create (an environment
// variable, a bind, a capability, a sysctl) needs every existing one made
// again; Outdated then reports them, and each is recreated by its own policy.
var specs = map[string]string{
	"bot":       "1",
	"mcp-stdio": "1",
	"drive":     "1",
	"egress":    "1",
}

// specOf is what SpecLabel should say for a role's container made now: its
// generation, plus for a Bot's box the operator's limits, which are taken at
// create. "" for a role that carries none (the drive helper).
func specOf(role string, cfg config.Config) string {
	v := specs[role]
	if v == "" {
		return ""
	}
	if role == "bot" {
		v += fmt.Sprintf(" mem=%d pids=%d", cfg.BotLimits.MemoryBytes(), cfg.BotLimits.PidsLimit())
	}
	return v
}

// Outdated reports why a container is not what Silo would make for role now:
// "image" when image resolves on the engine to another image than the one it
// runs (a new release, a rebuilt or re-pulled tag), "spec" when it was made
// another way (specOf). "" when it is current, and when that cannot be told
// (the wanted image is not on the engine yet: a container cannot be made from
// it anyway). It never pulls.
func (e *Engine) Outdated(ctx context.Context, st State, role, ref string) string {
	ref = strings.TrimSpace(ref)
	if st.ID == "" || ref == "" {
		return ""
	}
	// Nothing is outdated that could not be made again now: dropping a
	// working container for an image the engine does not have (not built
	// yet, not pulled yet) would leave nothing in its place.
	img, err := e.cli.ImageInspect(ctx, ref)
	if err != nil {
		return ""
	}
	if want := specOf(role, e.store.Config()); want != "" && st.Spec != want {
		return "spec"
	}
	if st.ImageID != "" && imageID(img.ID) != imageID(st.ImageID) {
		return "image"
	}
	return ""
}

// imageID is an image id without its algorithm, which engines spell
// differently (podman's compat API sometimes drops it).
func imageID(id string) string { return strings.TrimPrefix(strings.TrimSpace(id), "sha256:") }

// pullEvery is how often PullAlways asks the registry for one image again.
const pullEvery = 10 * time.Minute

// pullTimeout bounds one pull, which no caller cancels: a Bot's image is
// gigabytes, and a browser closing the request that started it must not
// throw away what has been fetched.
const pullTimeout = 30 * time.Minute

// pulls makes one pull per image at a time and remembers when each last
// succeeded.
type pulls struct {
	mu       sync.Mutex
	inflight map[string]*pullCall
	last     map[string]time.Time
}

type pullCall struct {
	done chan struct{}
	err  error
}

// EnsureImage makes sure ref is on the engine, pulling it as images.pull
// says. A local image (localhost/…, a dev build's) is never pulled: its
// absence is an error that says to build it.
func (e *Engine) EnsureImage(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	policy := e.store.Config().PullPolicy()
	_, err := e.cli.ImageInspect(ctx, ref)
	have := err == nil
	if have && (policy != config.PullAlways || e.pulledLately(ref)) {
		return nil
	}
	if local(ref) || policy == config.PullNever {
		if have {
			return nil
		}
		if local(ref) {
			return fmt.Errorf("image %s is not on this engine: build it with `make images`", ref)
		}
		return fmt.Errorf("image %s is not on this engine and images.pull is never: pull it by hand", ref)
	}
	if err := e.pull(ctx, ref); err != nil {
		if have {
			// A registry that cannot be reached must not stop a Bot whose
			// image is already here.
			log.Printf("silo: pulling %s: %v; using the image already here", ref, err)
			return nil
		}
		return fmt.Errorf("pulling image %s: %w", ref, err)
	}
	return nil
}

// Prefetch pulls each image as images.pull says, so the first Bot to start
// after an upgrade does not wait for its download. Failures are logged.
func (e *Engine) Prefetch(ctx context.Context, refs []string) {
	seen := map[string]bool{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		if err := e.EnsureImage(ctx, ref); err != nil {
			log.Printf("silo: %v", err)
		}
	}
}

// local reports whether ref names an image built on this host, which no
// registry has.
func local(ref string) bool { return strings.HasPrefix(ref, "localhost/") }

func (e *Engine) pulledLately(ref string) bool {
	e.pulls.mu.Lock()
	defer e.pulls.mu.Unlock()
	t, ok := e.pulls.last[ref]
	return ok && time.Since(t) < pullEvery
}

// pull fetches ref once however many ask at the same time; each caller waits
// only as long as its own context allows, and the pull goes on without it.
func (e *Engine) pull(ctx context.Context, ref string) error {
	p := &e.pulls
	p.mu.Lock()
	c := p.inflight[ref]
	if c == nil {
		if p.inflight == nil {
			p.inflight = map[string]*pullCall{}
			p.last = map[string]time.Time{}
		}
		c = &pullCall{done: make(chan struct{})}
		p.inflight[ref] = c
		go func() {
			log.Printf("silo: pulling %s", ref)
			pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pullTimeout)
			c.err = e.pullNow(pctx, ref)
			cancel()
			p.mu.Lock()
			delete(p.inflight, ref)
			if c.err == nil {
				p.last[ref] = time.Now()
			}
			p.mu.Unlock()
			close(c.done)
		}()
	}
	p.mu.Unlock()
	select {
	case <-c.done:
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) pullNow(ctx context.Context, ref string) error {
	rc, err := e.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	// The engine reports a failed pull inside the stream, not as the call's
	// error.
	return jsonmessage.DisplayJSONMessagesStream(rc, io.Discard, 0, false, nil)
}
