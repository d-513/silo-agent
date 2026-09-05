# Silo Agent

Token-conserving multi-user agent. Each **Bot** is a durable container with a GUI. Architecture: `docs/Description.md`. UI: `DESIGN.md`.

How to build and run: [DEVELOPMENT.md](DEVELOPMENT.md).

Operator config: [docs/CONFIGURATION.md](docs/CONFIGURATION.md). Koanf **defaults → `silo.yaml` → `SILO_*` env**. Nested keys use `__` (`SILO_OPENROUTER__API_KEY` → `openrouter.api_key`). OpenRouter key is YAML/env, not Admin. Default model when unset: `openai/gpt-5.6-luna`.

## Shape

- **Control Plane (Go):** users, bots, secrets, security engine, **agent loop** (OpenRouter/GPT), Docker lifecycle = create/start/stop/inspect only.
- **Worker (Go, inside Bot):** dials the CP. Local Unix-socket HTTP for Python tools. Tunnels VNC. Desktop is Openbox + Thunar + iron wallpaper (`botimage/`).
- **Python in the Bot:** `exec_python` + `silo_runtime`. Not the orchestrator.
- **Frontend:** pnpm + React + Tailwind. ConnectRPC + VNC websocket.

UI: 64px rail on every page (Bots, crest switcher, +, Admin). Crest is a picked shape+color (`color * 8 + shape`), not hashed from the name. Bot tabs are `/bots/:id/{run,desktop,files,secrets,rules}` labeled Chat, Desktop, Files, Secrets, Rules. Chat is the chats list + thread (`/bots/:id/run/:chatId`). Desktop is the hatch only. A connected worker is **online** (pine lamp), not idle. The CP holds the VNC stream until a viewer attaches, then the Worker dials x11vnc and forwards the RFB handshake. Each hatch WebSocket gets a viewer ticket; closing an old socket must not clear a newer one (React Strict Mode remounts the hatch). Replacing a worker session must cancel any waiter on the old session. Sign-in must call `setEmail` from the response or the session cookie is ignored. `StreamRun` persists every event, replays history, and accepts `after_event_id` so the client can reconnect without duplicates. Commands carry `run_id`; children get `SILO_RUN_ID`. Parallel runs on one bot are allowed; bot status is derived from remaining running runs and pending approvals. A Docker inspect error is not proof the box is gone — only not-found / not-running clears the cached ID. Desktop processes are fail-fast: if Xvfb/Openbox/x11vnc/worker dies, the container exits and `unless-stopped` recreates it. Chromium is a dock launcher, not a supervised process.

## Two hops

1. **CP ↔ Worker:** ConnectRPC, Worker-initiated, bot token. Concurrent RPCs.
2. **Python ↔ Worker:** HTTP/JSON on `unix:///var/run/silo/worker.sock`.

After create, never `docker exec`, `docker cp`, or a published VNC port.

Container ID in SQLite is a cache. `GetBot`/`ListBots` inspect Docker (and the `silo-{id}` name) and clear a vanished box. `StartBot` recreates if the container is gone. Do not trust a stored ID after a host reboot or a `podman rm`.
