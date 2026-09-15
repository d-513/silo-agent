# Development

Silo is three processes you run locally, plus two container images (Bots and STDIO MCP sidecars).

```
browser  →  Vite :5173  →  Control Plane :8080  →  Bot container (silo-worker)
                │                    │
                └── /silo.v1.UI ─────┘
                └── /vnc /console (websocket)
```

| Piece         | Command                 | Role                                                              |
| ------------- | ----------------------- | ----------------------------------------------------------------- |
| Control Plane | `./bin/silo`            | Users, chats, agent loop, Docker, VNC + console proxy             |
| Frontend      | `pnpm dev` in `web/`    | UI on http://127.0.0.1:5173                                       |
| Bot image     | `localhost/silo-bot:v1` | X11 desktop + `silo-worker` (Chromium on the dock, not autostart) |
| STDIO image   | `localhost/silo-mcp-stdio:v1` | `silo-mcp-bridge` + Node/`npx` + Python; one sidecar per STDIO connector |

Architecture: `docs/Description.md`. UI: `DESIGN.md`. Agent prompt: `internal/prompts/SYSTEM.md`.

## Prerequisites

- Go 1.26+ (`CGO_ENABLED=0` for this tree)
- pnpm + Node 22+
- Podman (or Docker) with a socket the CP can reach
- `buf` only when you change `.proto` files
- An [OpenRouter](https://openrouter.ai) API key

This machine uses rootless Podman:

```
export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
```

`podman machine` / Docker Desktop: point `docker_host` at that socket instead.

## First-time setup

```
cp silo.yaml.example silo.yaml
# edit silo.yaml: openrouter.api_key, bootstrap.email / bootstrap.password
cd web && pnpm install
```

`silo.yaml` is gitignored. Keys and load order: [docs/CONFIGURATION.md](docs/CONFIGURATION.md). Nested keys use `__` (`SILO_OPENROUTER__API_KEY` → `openrouter.api_key`).

The OpenRouter key, model, and search engine live in `silo.yaml` (Admin can edit the file). `SILO_*` env still wins. `bootstrap.*` seeds the first admin user; wipe `data/` to recreate it.

## Build

From the repo root:

```
export CGO_ENABLED=0
export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock

container_arch="$(podman info --format '{{.Host.Arch}}')"
go build -o bin/silo ./cmd/silo
GOOS=linux GOARCH="$container_arch" go build -o bin/silo-worker ./cmd/silo-worker
GOOS=linux GOARCH="$container_arch" go build -o bin/silo-mcp-bridge ./cmd/silo-mcp-bridge
podman build -t localhost/silo-bot:v1 -f botimage/Containerfile .
podman build -t localhost/silo-mcp-stdio:v1 -f mcpimage/Containerfile .
```

Build `silo-worker` **before** the Bot image. The Containerfile copies `bin/silo-worker` into the Bot.

Build `silo-mcp-bridge` **before** the STDIO image. The Containerfile copies `bin/silo-mcp-bridge` into the sidecar.

Image tags must match `bot_image` / `mcp_stdio_image` in `silo.yaml` (defaults `localhost/silo-bot:v1` and `localhost/silo-mcp-stdio:v1`).

## Run

Two terminals.

```
# 1 — Control Plane
export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
./bin/silo
```

```
# 2 — Frontend
cd web && pnpm dev
```

Open http://127.0.0.1:5173 and sign in with `bootstrap.email` / `bootstrap.password`.

Vite proxies `/silo.v1.UI`, `/silo.v1.BotWorker`, `/vnc`, `/console`, `/healthz`, `/oauth`, and `/connectors` to `:8080`. Do not point the browser at the CP unless you are serving a production frontend build.

`cp_url` (`http://host.containers.internal:8080` by default) is what the **container** uses to dial the CP. The create path adds `host.containers.internal:host-gateway`. If the worker never connects, that URL is not reachable from the Bot.

## Fast debugging

The repository includes VS Code tasks and launch configurations in `.vscode/`:

1. Run `Silo: start local stack` from the Command Palette. This builds only the control plane and starts Vite with HMR.
2. Start `Silo: control plane + browser` from Run and Debug. Go breakpoints work in the control plane and browser breakpoints work in the UI.
3. After changing `cmd/silo-worker`, `botimage/`, or worker-facing generated code, run `Silo: rebuild bot image`. Remove the affected container with `podman rm -f silo-<bot-id>`, then Start the Bot so it is recreated from the new image.
4. After changing `cmd/silo-mcp-bridge` or `mcpimage/`, run `Silo: rebuild MCP image`, then refresh the connector.

The equivalent commands are `./rebuild.sh cp`, `./rebuild.sh bot`, and `./rebuild.sh stdio`. `./rebuild.sh all` retains the old full rebuild behavior and removes all `silo-*` containers. The targeted modes do not remove containers, which keeps the database and running development environment intact.

For cross-process debugging, keep the control-plane terminal visible and inspect the container that owns the Bot with `podman logs -f silo-<bot-id>`. MCP sidecars use `podman logs -f silo-mcp-<connector-id>`. A worker change requires an image rebuild because the worker binary is copied into the image; a control-plane change does not.

## After you change…

| What                         | Then                                                                                  |
| ---------------------------- | ------------------------------------------------------------------------------------- |
| Go (CP only)                 | `go build -o bin/silo ./cmd/silo` and restart `./bin/silo`                            |
| `internal/prompts/SYSTEM.md` | same — it is `go:embed`’d                                                             |
| `internal/catalog/*`         | same — library presets and skills are `go:embed`’d; restart the CP to seed new keys |
| Go (worker)                  | rebuild `bin/silo-worker`, rebuild the image, recreate the Bot container              |
| `botimage/*`                 | rebuild the image, recreate the Bot container                                         |
| Go (stdio bridge)            | rebuild `bin/silo-mcp-bridge`, rebuild `localhost/silo-mcp-stdio:v1`, Refresh the connector |
| `mcpimage/*`                 | same                                                                                  |
| `proto/**`                   | `buf generate`, then rebuild CP and worker (and the image if the worker stub changed) |
| `web/**`                     | Vite reloads. `pnpm build` is the production bundle only                              |

A running Bot keeps its old image. **Start** will not rebuild it. Stop the Bot, `podman rm -f silo-<botId>`, then Start (or delete and create the Bot). After a host reboot or a manual `podman rm`, the ID in SQLite is stale; `GetBot` / `StartBot` recover by name (`silo-<id>`).

## Tests

```
export CGO_ENABLED=0
go test ./cmd/... ./internal/...
```

The explicit package patterns are intentional. `data/` is ignored runtime state, not Go source, but `go test ./...` still walks it. A Bot Chromium profile may be owned by the container UID and unreadable from the host, which makes the broad pattern fail before Go can run a test. See [TESTING.md](TESTING.md) for the test tiers and live checks.

## Layout

```
cmd/silo            Control Plane
cmd/silo-worker     process inside the Bot
cmd/silo-mcp-bridge reverse tunnel from a STDIO sidecar to the CP (raw JSON-RPC)
internal/app        UI + worker RPCs, agent loop
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
