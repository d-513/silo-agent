// Package apptest is the feature-test harness for the Silo control plane. It
// wires an in-memory database, a deterministic config, a fake Docker host, an
// in-process h2c server running the real App.Handler, and an authenticated
// ConnectRPC client. Tests drive the product through its public API instead of
// poking private helpers.
//
// The package imports internal/llm/dummy for its registration side effect, so
// any test binary that uses this harness has the deterministic "dummy" model
// provider available.
package apptest

import (
	"context"
	"sync"
	"sync/atomic"

	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
)

// FakeHost is an in-process dockerx.Host. It records lifecycle calls and lets
// tests inject failures. Container IDs are deterministic ("cid-<botID>") so a
// test can assert on them without guessing.
type FakeHost struct {
	mu         sync.Mutex
	inspect    map[string]dockerx.State
	envHash    map[string]string
	tokens     map[string]string
	specs      map[string]dockerx.StdioSpec
	sidecars   []dockerx.StdioContainer
	orphans    []dockerx.StdioContainer
	stdioStops map[string]func()
	InspectErr error
	CreateErr  error
	StartErr   error
	// StdioLaunch, when set, is called with the sidecar spec after CreateStdio
	// records it. Container-free tests use it to run the real bridge process.
	StdioLaunch func(spec dockerx.StdioSpec) func()

	Creates      atomic.Int32
	Starts       atomic.Int32
	Stops        atomic.Int32
	Drops        atomic.Int32
	StdioCreates atomic.Int32
	StdioDrops   atomic.Int32
}

func NewFakeHost() *FakeHost {
	return &FakeHost{
		inspect:    map[string]dockerx.State{},
		envHash:    map[string]string{},
		tokens:     map[string]string{},
		specs:      map[string]dockerx.StdioSpec{},
		stdioStops: map[string]func(){},
	}
}

func (f *FakeHost) Inspect(_ context.Context, id string) (dockerx.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.InspectErr != nil {
		return dockerx.State{}, f.InspectErr
	}
	if st, ok := f.inspect[id]; ok {
		return st, nil
	}
	return dockerx.State{}, dockerx.ErrNotFound
}

func (f *FakeHost) Stats(_ context.Context, id string) (dockerx.Stats, error) {
	st, err := f.Inspect(context.Background(), id)
	if err != nil {
		return dockerx.Stats{}, err
	}
	if !st.Running {
		return dockerx.Stats{}, nil
	}
	return dockerx.Stats{CPUPercent: 1.5, MemUsed: 100 << 20, MemLimit: 1 << 30}, nil
}

func (f *FakeHost) EnvTokenHash(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.envHash[id], nil
}

func (f *FakeHost) Create(_ context.Context, botID, token string) (string, error) {
	f.Creates.Add(1)
	if f.CreateErr != nil {
		return "", f.CreateErr
	}
	id := "cid-" + botID
	f.mu.Lock()
	f.tokens[botID] = token
	// Mirror Docker: the container carries SILO_BOT_TOKEN, so EnvTokenHash
	// reports the hash of the token it was created with.
	f.envHash[id] = ids.Hash(token)
	st := dockerx.State{ID: id, Running: true}
	f.inspect[id] = st
	f.inspect[dockerx.Name(botID)] = st
	f.mu.Unlock()
	return id, nil
}

// Token returns the container token passed to Create for a bot. Feature tests
// use it to authenticate a real worker subprocess.
func (f *FakeHost) Token(botID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens[botID]
}

func (f *FakeHost) Start(_ context.Context, id string) error {
	f.Starts.Add(1)
	if f.StartErr != nil {
		return f.StartErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.inspect[id]
	if !ok {
		return dockerx.ErrNotFound
	}
	for k, v := range f.inspect {
		if v.ID == st.ID {
			v.Running = true
			f.inspect[k] = v
		}
	}
	return nil
}

func (f *FakeHost) Stop(_ context.Context, id string) error {
	f.Stops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if st, ok := f.inspect[id]; ok {
		for k, v := range f.inspect {
			if v.ID == st.ID {
				v.Running = false
				f.inspect[k] = v
			}
		}
	}
	return nil
}

func (f *FakeHost) Drop(_ context.Context, botID, containerID string) {
	f.Drops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.inspect, containerID)
	delete(f.inspect, dockerx.Name(botID))
	delete(f.tokens, botID)
}

func (f *FakeHost) CreateStdio(_ context.Context, spec dockerx.StdioSpec) (string, error) {
	f.StdioCreates.Add(1)
	if f.CreateErr != nil {
		return "", f.CreateErr
	}
	id := "mcp-" + spec.ID
	f.mu.Lock()
	st := dockerx.State{ID: id, Running: true}
	f.inspect[id] = st
	f.inspect[dockerx.StdioName(spec.ID)] = st
	f.specs[spec.ID] = spec
	f.sidecars = append(f.sidecars, dockerx.StdioContainer{ID: id, Name: dockerx.StdioName(spec.ID), ConnectorID: spec.ID})
	launch := f.StdioLaunch
	f.mu.Unlock()
	if launch != nil {
		stop := launch(spec)
		f.mu.Lock()
		f.stdioStops[spec.ID] = stop
		f.mu.Unlock()
	}
	return id, nil
}

// StdioSpec returns the spec recorded for a sidecar attachment, so a test can
// inspect the env the real bridge would receive.
func (f *FakeHost) StdioSpec(id string) (dockerx.StdioSpec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	spec, ok := f.specs[id]
	return spec, ok
}

func (f *FakeHost) DropStdio(_ context.Context, id, _ string) {
	f.StdioDrops.Add(1)
	f.mu.Lock()
	delete(f.inspect, "mcp-"+id)
	delete(f.inspect, dockerx.StdioName(id))
	delete(f.specs, id)
	out := f.sidecars[:0]
	for _, s := range f.sidecars {
		if s.ConnectorID != id {
			out = append(out, s)
		}
	}
	f.sidecars = out
	stop := f.stdioStops[id]
	delete(f.stdioStops, id)
	f.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (f *FakeHost) ListStdio(context.Context) ([]dockerx.StdioContainer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]dockerx.StdioContainer(nil), f.sidecars...)
	return append(out, f.orphans...), nil
}

// AddOrphan registers a sidecar that exists in Docker but has no DB row, so
// reconcileStdio has something to reclaim.
func (f *FakeHost) AddOrphan(c dockerx.StdioContainer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orphans = append(f.orphans, c)
}

// SetEnvHash makes Inspect report a container whose SILO_BOT_TOKEN hash differs
// from the DB, so live() drops it as stale.
func (f *FakeHost) SetEnvHash(botID, hash string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envHash["cid-"+botID] = hash
}

// SetInspectError makes Inspect fail for every id.
func (f *FakeHost) SetInspectError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InspectErr = err
}

// SetRunning flips the running flag of a bot's fake container.
func (f *FakeHost) SetRunning(botID string, running bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range f.inspect {
		if v.ID == "cid-"+botID {
			v.Running = running
			f.inspect[k] = v
		}
	}
}

// Forget makes the fake container vanish, as if it were removed out of band.
func (f *FakeHost) Forget(botID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.inspect, "cid-"+botID)
	delete(f.inspect, dockerx.Name(botID))
}

// Gone reports whether the fake container for a bot no longer exists under
// either its id or its name.
func (f *FakeHost) Gone(botID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, byID := f.inspect["cid-"+botID]
	_, byName := f.inspect[dockerx.Name(botID)]
	return !byID && !byName
}
