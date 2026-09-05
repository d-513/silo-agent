# Silo Agent

Token-conserving multi-user agent. Each **Bot** is a durable Docker container with a GUI. Architecture: `docs/Description.md`. UI system and Stitch handoff: `DESIGN.md`.

## Shape

- **Control Plane (Go):** users, bots, secrets, connectors, security engine, **agent loop**, LLM providers. Docker lifecycle = create/start/stop/inspect only. ConnectRPC server.
- **Worker (Go, inside Bot):** dials the CP. Executes commands. Local Unix-socket HTTP for Python tools. Tunnels VNC. No other CP channel.
- **Python in the Bot:** `exec_python` + generated `/opt/tools` wrappers around `silo_runtime`. Not the orchestrator, not a ConnectRPC client.
- **Frontend:** pnpm + React + Tailwind. ConnectRPC + VNC websocket (both session-auth to the CP).

## Two hops

1. **CP ↔ Worker:** ConnectRPC, Worker-initiated, bot token. Concurrent RPCs (`Commands` stream + unary `CallConnector`/`GetSecret` + `VNC`). Docker may be on another machine; `SILO_CP_URL` must be reachable from that host.
2. **Python ↔ Worker:** HTTP/JSON on `unix:///var/run/silo/worker.sock`. Shipped `silo_runtime.call` / `get_secret`. Generated files are docs + one-liners.

After create, never `docker exec`, `docker cp`, socket mounts, or a published VNC port the CP dials.

## Invariants

- Strip `SILO_BOT_TOKEN` and `SILO_CP_URL` from every child env. Children get `SILO_WORKER_SOCK` only.
- Mask known secrets (raw + base64/url/json/hex, len ≥ 8) on the Worker *and* the CP before persist and before any provider request. CI-style, not perfect.
- One headed Chromium on X11 (`DISPLAY=:1`, CDP `:9222`). Playwright attaches.
- Images: path refs → multimodal parts, not base64-in-stdout.
- Operator config (Koanf YAML+env) ≠ admin-UI settings (DB).
- SQLite + GORM, WAL. No repository layer.

## Patterns to avoid repeating

- Do not add first-class tools for every connector action; discover them on disk.
- Do not put a Python Connect client in `/opt/tools`.
- Do not funnel Worker traffic through one synchronous RPC — `exec_python` + nested `CallConnector` will deadlock.
- Do not float python-connector repos on `main`; pin a commit SHA.
