# Development

Silo is three local processes — a Go control plane, a Vite frontend, and worker binaries that run inside Bot containers — plus two container images.

```
browser  →  Vite :5173  →  Control Plane :8080  →  Bot container (silo-worker)
                │                    │
                └── /silo.v1.UI ─────┘
                └── /vnc /console (websocket)
```

Architecture: `docs/Description.md`. UI: `DESIGN.md`. Agent prompt: `internal/prompts/SYSTEM.md`. Operator config: `docs/CONFIGURATION.md`.

## Prerequisites

- Go 1.26+ (`CGO_ENABLED=0`)
- pnpm + Node 22+
- Podman (or Docker) with a socket the CP can reach
- `tmux` for `make dev`, `watchexec` for `make watch-control`
- `buf` only when you change `.proto` files
- An [OpenRouter](https://openrouter.ai) API key

This machine uses rootless Podman. The Makefile defaults `DOCKER_HOST` to `unix:///run/user/1000/podman/podman.sock`; override it if yours differs. `podman machine` / Docker Desktop: point `docker_host` at that socket instead.

## First-time setup

```
make install
```

That creates `silo.yaml` from the example (if missing), downloads Go modules, and installs web dependencies. Then edit `silo.yaml`: `providers.<id>.api_key`, the `models` allowlist, and `bootstrap.email` / `bootstrap.password`.

`silo.yaml` is gitignored. Keys and load order: [docs/CONFIGURATION.md](docs/CONFIGURATION.md). Nested keys use `__` (`SILO_PROVIDERS__OPENROUTER__API_KEY` → `providers.openrouter.api_key`). Env wins over the file; `bootstrap.*` seeds the first admin, so wipe `data/` to recreate it.

## Everyday targets

Run `make help` to list everything.

| Target               | Does                                                              |
| -------------------- | ---------------------------------------------------------------- |
| `make dev`           | tmux session: watchexec CP + Vite (the usual way to develop)      |
| `make run-control`   | Build and run the CP on `:8080`                                   |
| `make run-frontend`  | Vite dev server on `:5173`                                        |
| `make watch-control` | Run the CP under watchexec (rebuild + restart on Go changes)      |
| `make build-cp`      | `bin/silo`                                                        |
| `make build-all`     | `bin/silo`, `bin/silo-worker`, `bin/silo-mcp-bridge`              |
| `make images`        | Bot + STDIO MCP images                                            |
| `make test`          | Full Go suite: units + feature tests + real-container tier        |
| `make test-fast`     | Go suite without the container tier (no Podman needed)            |
| `make test-containers` | Only the real Bot-image tests; fails if Podman is missing       |
| `make test-integration` | Go client integration tests against a running CP              |
| `make e2e`           | Playwright against an already-running `make dev` stack            |
| `make fmt` / `vet`   | `go fmt` / `go vet`                                               |
| `make proto`         | `buf generate`                                                    |
| `make cleanup`       | Force-remove every `silo-*` container                             |
| `make clean-images`  | Remove the local bot and STDIO images                             |
| `make clean`         | Remove `bin/` and `web/dist`                                      |
| `make reset-data`    | Delete `./data` (asks first)                                      |

`make dev` starts a tmux session named `silo` with two panes. The left pane runs `watch-control`; the right runs Vite. Detach with `Ctrl-b d` and come back with `make attach` (or `tmux attach -t silo`). Set `SESSION=` to use another name, `DOCKER_HOST=` to point elsewhere.

### How the backend watch is configured

`make watch-control` watches only `cmd/`, `internal/`, `go.mod`, `go.sum`, and `silo.yaml`, and only for Go/mod/Markdown/YAML/JSON changes. A 750 ms debounce coalesces a burst of editor writes, and `--on-busy-update restart` sends SIGTERM with a 5 s grace period so the CP releases `:8080` before the next build starts. The command builds first and only then `exec`s the new binary, so a compile error never replaces a working process.

## Build

```
make build-all     # CP + worker + bridge
make images        # localhost/silo-bot:v1 + localhost/silo-mcp-stdio:v1
```

`make images` builds the worker/bridge binaries first; the Containerfiles copy them from `bin/`. `CONTAINER_ARCH` comes from `podman info`.

Image tags must match `bot_image` / `mcp_stdio_image` in `silo.yaml` (defaults `localhost/silo-bot:v1`, `localhost/silo-mcp-stdio:v1`). `make rebuild MODE=cp|bot|stdio|all` wraps `rebuild.sh`; `all` also removes every `silo-*` container.

## Run

```
make dev
```

Open http://localhost:5173 and sign in with `bootstrap.email` / `bootstrap.password`.

Vite proxies `/silo.v1.UI`, `/silo.v1.BotWorker`, `/vnc`, `/console`, `/healthz`, `/oauth`, `/connectors`, and `/artifacts` to `:8080`. Do not point the browser at the CP unless you are serving a production frontend build.

`cp_url` (`http://host.containers.internal:8080` by default) is what the **container** uses to dial the CP. The create path adds `host.containers.internal:host-gateway`. If the worker never connects, that URL is not reachable from the Bot.

## Fast debugging

VS Code tasks and launch configs live in `.vscode/`:

1. `Silo: start local stack` builds the CP and starts Vite with HMR.
2. `Silo: control plane + browser` debugs CP Go breakpoints and UI breakpoints together.
3. After changing `cmd/silo-worker`, `botimage/`, or worker-facing generated code, run `make rebuild MODE=bot`, then `podman rm -f silo-<bot-id>` and Start the Bot so it is recreated.
4. After changing `cmd/silo-mcp-bridge` or `mcpimage/`, run `make rebuild MODE=stdio`, then refresh the connector.

Keep the CP terminal visible and inspect a Bot with `podman logs -f silo-<bot-id>`; MCP sidecars use `podman logs -f silo-mcp-<connector-id>`. A worker change needs an image rebuild because the binary is copied in; a CP change does not.

## After you change…

| What                         | Then                                                                                  |
| ---------------------------- | ------------------------------------------------------------------------------------- |
| Go (CP only)                 | `make watch-control` (or `make build-cp` and restart)                                 |
| `internal/prompts/SYSTEM.md` | same — it is `go:embed`’d                                                              |
| `internal/catalog/*`         | same — presets/skills are `go:embed`’d; restart to seed new keys                      |
| Go (worker)                  | `make rebuild MODE=bot`, recreate the Bot container                                   |
| `botimage/*`                 | same                                                                                  |
| Go (stdio bridge)            | `make rebuild MODE=stdio`, Refresh the connector                                      |
| `mcpimage/*`                 | same                                                                                  |
| `proto/**`                   | `make proto`, then rebuild CP and worker (and the image if the worker stub changed)   |
| `internal/channels/**`       | restart the CP; sessions live under `data/channels/<id>/`                             |
| `botimage/silo_runtime.py`   | `make rebuild MODE=bot`, recreate the Bot container                                   |
| `web/**`                     | Vite reloads. `pnpm build` is the production bundle only                              |

A running Bot keeps its old image. **Start** will not rebuild it. Stop the Bot, `podman rm -f silo-<botId>`, then Start (or delete and create the Bot). After a reboot or manual `podman rm`, the stored ID is stale; `GetBot` / `StartBot` recover by name (`silo-<id>`).

## Tests

```
make test          # full suite, including the real-container tier (run `make images` first)
make test-fast     # skip the container tier (no Podman needed)
make e2e           # Playwright against the already-running `make dev` stack
```

The explicit Go package patterns are intentional — `go test ./...` would walk `data/`, and a container-owned Chromium profile can be unreadable from the host. Feature tests use the deterministic DummyLLM provider (`internal/llm/dummy`) and the `internal/apptest` harness; the container tier boots the real Bot image and cleans up everything it creates. See [TESTING.md](TESTING.md) for tiers, environment knobs, and live checks.

## Commits

Use **Conventional Commits**: `<type>(<scope>): <imperative summary>` — lowercase, no trailing period, ≤72 characters.

| Type | Use for |
| --- | --- |
| `feat` | new user-visible capability |
| `fix` | bug fix |
| `refactor` | behavior-preserving change |
| `perf` | speed/resource work |
| `docs` | `*.md` and comments |
| `test` | tests only |
| `build` | Makefile, Containerfiles, dependencies, images |
| `chore` | housekeeping with no src/logic change |

**Scopes** mirror the layout: `app`, `worker`, `bridge`, `mcp`, `channels`, `llm`, `search`, `security`, `catalog`, `prompts`, `proto`, `web`, `botimage`, `mcpimage`, `config`. Omit the scope for cross-cutting changes.

The body (optional) explains what and why, not how, wrapped at 72 columns. Footers carry `BREAKING CHANGE:` and issue references.

```
feat(channels): bind Telegram rows to a single chat

requires_target adapters now persist external_id/target_title from
set_target, and only that chat is read or written. Bot accounts cannot
enumerate dialogs, so the picker lists seen chats or accepts @username.

Fixes #42
```

Keep commits atomic: one logical change each, not a mega-commit that bundles unrelated work.

## Layout

```
cmd/silo            Control Plane
cmd/silo-worker     process inside the Bot
cmd/silo-mcp-bridge reverse tunnel from a STDIO sidecar to the CP (raw JSON-RPC)
internal/app        UI + worker RPCs, agent loop
internal/channels   channel adapter engine (telegram/ is gotd MTProto; GUIDE.md go:embed’d)
internal/prompts    SYSTEM.md (embedded)
internal/catalog    connector presets + skills (embedded)
botimage/           Containerfile, start.sh, Thunar, wallpaper, dock
mcpimage/           STDIO sidecar Containerfile
proto/silo/v1       ui.proto, worker.proto
gen/                Go stubs (generated)
web/                Vite + React
web/src/gen         TS stubs (generated)
data/               SQLite + per-bot volumes + skills (gitignored)
```
