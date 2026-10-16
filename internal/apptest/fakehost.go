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
	"net/netip"
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
	// DriveLaunch, when set, runs after CreateDrive records a sidecar spec;
	// tests use it to run the real drive guest in-process.
	DriveLaunch func(spec dockerx.DriveSpec) func()

	drive        *driveState
	driveSpecs   map[string]dockerx.DriveSpec
	driveStops   map[string]func()
	driveOrphans []dockerx.DriveContainer

	// The network filter: the gateway, each client network, and the spec each
	// client container was created with.
	botNet   map[string]*dockerx.ClientNet
	egress   *egressState
	runtimeS string

	EgressUps    atomic.Int32
	NetworkMade  atomic.Int32
	NetworkDrops atomic.Int32

	Creates      atomic.Int32
	Starts       atomic.Int32
	Stops        atomic.Int32
	Drops        atomic.Int32
	StdioCreates atomic.Int32
	StdioDrops   atomic.Int32
	DriveCreates atomic.Int32
	DriveDrops   atomic.Int32
}

func NewFakeHost() *FakeHost {
	return &FakeHost{
		inspect:    map[string]dockerx.State{},
		envHash:    map[string]string{},
		tokens:     map[string]string{},
		specs:      map[string]dockerx.StdioSpec{},
		stdioStops: map[string]func(){},
		botNet:     map[string]*dockerx.ClientNet{},
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

func (f *FakeHost) Create(_ context.Context, botID, token string, net *dockerx.ClientNet) (string, error) {
	f.Creates.Add(1)
	if f.CreateErr != nil {
		return "", f.CreateErr
	}
	id := "cid-" + botID
	f.mu.Lock()
	f.tokens[botID] = token
	f.botNet[botID] = net
	// Mirror Docker: the container carries SILO_BOT_TOKEN, so EnvTokenHash
	// reports the hash of the token it was created with.
	f.envHash[id] = ids.Hash(token)
	st := dockerx.State{ID: id, Running: true}
	if net != nil {
		st.Networks = []string{net.Network}
		st.CAHash = caHash(net.CA)
	}
	f.inspect[id] = st
	f.inspect[dockerx.Name(botID)] = st
	f.mu.Unlock()
	return id, nil
}

// caHash mirrors Docker's Inspect: the hash of SILO_EGRESS_CA, "" without one.
func caHash(pem string) string {
	if pem == "" {
		return ""
	}
	return ids.Hash(pem)
}

// BotNet returns the network spec a Bot's box was created with, or nil.
func (f *FakeHost) BotNet(botID string) *dockerx.ClientNet {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.botNet[botID]
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
	if spec.Net != nil {
		st.Networks = []string{spec.Net.Network}
		st.CAHash = caHash(spec.Net.CA)
	}
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

// SetBoxNetworks replaces the networks a bot's fake container reports, as a
// box made before the network filter was turned on would.
func (f *FakeHost) SetBoxNetworks(botID string, nets []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range f.inspect {
		if v.ID == "cid-"+botID {
			v.Networks = nets
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

type driveState struct {
	dirs    map[string]bool
	prepErr error
}

func (f *FakeHost) drives() *driveState {
	if f.drive == nil {
		f.drive = &driveState{dirs: map[string]bool{}}
	}
	return f.drive
}

// SetPrepareDriveErr makes PrepareDriveDir fail (no drive image, or the
// engine refused the shared mount).
func (f *FakeHost) SetPrepareDriveErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drives().prepErr = err
}

func (f *FakeHost) PrepareDriveDir(_ context.Context, _, root, botID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.drives().prepErr; err != nil {
		return err
	}
	f.drives().dirs[dockerx.DriveDir(root, botID)] = true
	return nil
}

func (f *FakeHost) RemoveDriveDir(_ context.Context, _, root, botID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.drives().dirs, dockerx.DriveDir(root, botID))
	return nil
}

// DriveDirExists reports whether PrepareDriveDir made a Bot's drive dir and
// nothing removed it since.
func (f *FakeHost) DriveDirExists(root, botID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drives().dirs[dockerx.DriveDir(root, botID)]
}

func (f *FakeHost) CreateDrive(_ context.Context, spec dockerx.DriveSpec) (string, error) {
	f.DriveCreates.Add(1)
	if f.CreateErr != nil {
		return "", f.CreateErr
	}
	id := "drive-" + spec.BotID
	f.mu.Lock()
	st := dockerx.State{ID: id, Running: true}
	f.inspect[id] = st
	f.inspect[dockerx.DriveName(spec.BotID)] = st
	if f.driveSpecs == nil {
		f.driveSpecs = map[string]dockerx.DriveSpec{}
	}
	f.driveSpecs[spec.BotID] = spec
	launch := f.DriveLaunch
	f.mu.Unlock()
	if launch != nil {
		stop := launch(spec)
		f.mu.Lock()
		if f.driveStops == nil {
			f.driveStops = map[string]func(){}
		}
		f.driveStops[spec.BotID] = stop
		f.mu.Unlock()
	}
	return id, nil
}

// DriveSpecFor returns the recorded sidecar spec for a Bot.
func (f *FakeHost) DriveSpecFor(botID string) (dockerx.DriveSpec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.driveSpecs[botID]
	return s, ok
}

func (f *FakeHost) DropDrive(_ context.Context, botID, _ string) {
	f.DriveDrops.Add(1)
	f.mu.Lock()
	delete(f.inspect, "drive-"+botID)
	delete(f.inspect, dockerx.DriveName(botID))
	delete(f.driveSpecs, botID)
	stop := f.driveStops[botID]
	delete(f.driveStops, botID)
	f.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (f *FakeHost) ListDrives(context.Context) ([]dockerx.DriveContainer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []dockerx.DriveContainer
	for bot := range f.driveSpecs {
		out = append(out, dockerx.DriveContainer{ID: "drive-" + bot, Name: dockerx.DriveName(bot), BotID: bot})
	}
	return append(out, f.driveOrphans...), nil
}

// AddDriveOrphan registers a drive sidecar with no Bot behind it.
func (f *FakeHost) AddDriveOrphan(c dockerx.DriveContainer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.driveOrphans = append(f.driveOrphans, c)
}

func (f *FakeHost) HasDriveBind(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drives().prepErr == nil, nil
}

type fakeNetwork struct {
	subnet   string
	clientID string
	attached map[string]bool
}

type egressState struct {
	containerID string
	env         []string
	networks    map[string]*fakeNetwork
}

func (f *FakeHost) egressState() *egressState {
	if f.egress == nil {
		f.egress = &egressState{networks: map[string]*fakeNetwork{}}
	}
	return f.egress
}

func (f *FakeHost) EgressUp(_ context.Context, spec dockerx.EgressSpec) (string, error) {
	f.EgressUps.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.egressState()
	if e.containerID == "" {
		e.containerID = "egress"
		f.inspect[e.containerID] = dockerx.State{ID: e.containerID, Running: true}
	}
	e.env = spec.Env
	return e.containerID, nil
}

func (f *FakeHost) EgressID(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.egressState().containerID, nil
}

func (f *FakeHost) DropEgress(context.Context, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.egressState(); e.containerID != "" {
		delete(f.inspect, e.containerID)
		e.containerID = ""
	}
}

func (f *FakeHost) EnsureNetwork(_ context.Context, name, subnet, clientID string) error {
	f.NetworkMade.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.egressState()
	if _, ok := e.networks[name]; !ok {
		e.networks[name] = &fakeNetwork{subnet: subnet, clientID: clientID, attached: map[string]bool{}}
	}
	return nil
}

func (f *FakeHost) DropNetwork(_ context.Context, name string) error {
	f.NetworkDrops.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.egressState().networks, name)
	return nil
}

func (f *FakeHost) NetworkConnect(_ context.Context, networkName, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n := f.egressState().networks[networkName]; n != nil {
		n.attached[containerID] = true
	}
	return nil
}

func (f *FakeHost) NetworkDisconnect(_ context.Context, networkName, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n := f.egressState().networks[networkName]; n != nil {
		delete(n.attached, containerID)
	}
	return nil
}

func (f *FakeHost) NetworkAddr(_ context.Context, containerID, networkName string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.egressState()
	n := e.networks[networkName]
	if n == nil || containerID == "" || !n.attached[containerID] || containerID != e.containerID {
		return "", nil
	}
	p, err := netip.ParsePrefix(n.subnet)
	if err != nil {
		return "", nil
	}
	return p.Masked().Addr().Next().String(), nil
}

func (f *FakeHost) ListClientNetworks(context.Context) ([]dockerx.NetworkInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []dockerx.NetworkInfo
	for name, n := range f.egressState().networks {
		out = append(out, dockerx.NetworkInfo{Name: name, Subnet: n.subnet, ClientID: n.clientID})
	}
	return out, nil
}

func (f *FakeHost) Runtime(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runtimeS != "" {
		return f.runtimeS, nil
	}
	return "podman", nil
}

// SetRuntime makes Runtime report a specific engine (a test point for the
// Docker refusal).
func (f *FakeHost) SetRuntime(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runtimeS = name
}

// EgressAttached reports whether the gateway is attached to a client network.
func (f *FakeHost) EgressAttached(networkName string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.egressState()
	n := e.networks[networkName]
	return n != nil && e.containerID != "" && n.attached[e.containerID]
}

// EgressEnv returns the env the gateway was started with.
func (f *FakeHost) EgressEnv() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.egressState().env...)
}

// NetworkSubnet returns the subnet a client network was carved with.
func (f *FakeHost) NetworkSubnet(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n := f.egressState().networks[name]; n != nil {
		return n.subnet
	}
	return ""
}
