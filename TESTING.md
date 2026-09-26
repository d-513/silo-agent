# Testing

Three tiers, one rule: the default suite is deterministic and self-cleaning, and
it tests **features through public seams**, not private helpers. Live model
calls, real channels, and MCP providers only ever run in the opt-in integration
tier.

## Commands

```sh
make test            # everything: unit, feature, and the real-container tier
make test-fast       # same but skips Podman (SILO_SKIP_CONTAINERS=1)
make test-containers # only the real Bot-image tests; fails if Podman is missing
make test-integration # Go client against a running CP (SILO_INTEGRATION=1)
make e2e             # Playwright against a running `make dev` stack
```

`make test` does not use `go test ./...` — that walks `data/`, where a
container-owned Chromium profile can be unreadable from the host.

## Tiers

### 1. Unit tests (pure packages)
No network, Docker, display, or model. They cover parsing, authorization
decisions, path validation, masking, archive safety, prompt/section splitting,
provider request encoding, and the worker's own filesystem logic. Each package
is expected to test behaviour and boundaries, not one method per file.

### 2. Feature tests (`internal/app`, deterministic)
The control plane is driven through its real ConnectRPC API on an in-process
h2c listener, with its own throwaway Postgres schema (`internal/db/dbtest`, needs `make db-up`), a temp data dir, and a **DummyLLM**
provider. No tokens are spent.

- `internal/llm/dummy` is a scripted model. A prompt containing `Test_NN_Input`
  yields `Test_NN_Output`; register multi-turn tool flows with
  `dummy.Script("Test_NN", dummy.Turn{...})`. The provider is stateless: it
  derives the current turn from the assistant messages already in the request,
  so a tool call followed by its result replays exactly.
- `internal/apptest` is the harness: `apptest.New(t)` gives you `H.Client`
  (signed-in UI client), `H.DB`, `H.Fake` (fake Docker host), and helpers like
  `CreateBot`, `FirstChat`, `Send`, `WaitRun`, `WaitApproval`.
- `H.StartWorker(botID)` launches the **real `cmd/silo-worker` subprocess**
  against the harness CP over h2c. File/terminal/Python tools exercise the real
  worker without a container and tear down completely with the test.
- `H.WorkerClient(botID)` is a BotWorker RPC client authenticated as the Bot, so
  `CallTool`, `GetSecret`, and the approval gates are tested through the exact
  RPC the container worker uses.
- Connectors are real too: HTTP MCP connectors point at an in-process
  streamable-HTTP MCP server, and `apptest.WithStdioBridge()` runs the actual
  `cmd/silo-mcp-bridge` process with the env the CP put on the sidecar spec.
  The bridge spawns this test binary as its MCP child (`SILO_MCP_HELPER=echo`),
  so the full attach → authorize → `CallTool` → detach path is covered without
  Docker.
- `config.FromYAML` loads a deterministic config that ignores `SILO_*` env, so
  a stray shell variable cannot perturb a test.

### 3. Container tests (`internal/app`, real Podman)
Same API path, but with a real `dockerx.Engine`, the real `localhost/silo-bot:v1`
image, and a worker inside the box dialing the CP through
`host.containers.internal`. They boot a bot, wait for its heartbeat, round-trip
a tool, and assert lifecycle semantics.

Residue is engineered away:

- Bot ids are prefixed `zztest-`; cleanup only ever touches `silo-zztest-*` and
  `silo-mcp-zztest-*`. A developer's real Bots are never candidates.
- Each test uses a temp `data_dir`; cleanup removes it with
  `podman unshare rm -rf` so root-owned files (the Chromium profile start.sh
  chowns) disappear too.
- `TestMain` sweeps leftover `zztest` containers before and after the run, so an
  interrupted test cannot leak a box.

Container tests skip when Podman or the image is missing. Set
`SILO_REQUIRE_CONTAINERS=1` (what `make test-containers` does) to fail instead.
Run `make images` first.

### 4. Integration tests (real tokens, opt-in)
These use the developer's own `silo.yaml` — real provider keys, real tools,
channels, and MCP — so they cost money. They never gate `make test`.

- **Go client**: `integration/client_test.go` signs in over ConnectRPC,
  creates a Bot, sends a message, and streams the reply. Requires a running CP:
  `SILO_INTEGRATION=1 SILO_CP_URL=http://localhost:8080 make test-integration`.
- **Playwright**: `web/e2e/` drives the real UI. It assumes the stack is already
  running (`make dev`) and does not start servers itself. Credentials come from
  `SILO_BOOTSTRAP_EMAIL` / `SILO_BOOTSTRAP_PASSWORD` (defaults match
  `silo.yaml.example`); the base URL is `SILO_E2E_URL` (default
  `http://localhost:5173`). First run: `pnpm --dir web run e2e:install`.

## Writing a test

- Reach for a **feature test** first: drive `H.Client` (or call a handler
  through the same API the UI uses). If a test needs a private helper to pass,
  that is usually a missing seam, not a reason to white-box.
- Use `dummy.Script` to describe the model's turns (text, reasoning, tool
  calls). Reset with `dummy.Reset()` when a test registers scenarios.
- Reserve real network/MCP expectations for the integration tier.
- Keep every test hermetic: temp dirs, in-memory DB, no shared global state.

## Environment knobs

| Variable | Effect |
| --- | --- |
| `SILO_SKIP_CONTAINERS=1` | Skip the container tier |
| `SILO_REQUIRE_CONTAINERS=1` | Fail (not skip) when Podman/image is missing |
| `SILO_BOT_IMAGE` | Bot image the container tier boots |
| `SILO_INTEGRATION=1` | Enable the Go client integration tests |
| `SILO_CP_URL` | Control plane URL for integration tests |
| `SILO_BOOTSTRAP_EMAIL` / `SILO_BOOTSTRAP_PASSWORD` | Integration sign-in |
| `SILO_E2E_URL` | Playwright base URL |
