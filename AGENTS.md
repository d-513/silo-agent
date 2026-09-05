# Silo Agent

Token-conserving multi-user agent. Each **Bot** is a durable container with a GUI. Architecture: `docs/Description.md`. UI: `DESIGN.md`.

How to build and run: [DEVELOPMENT.md](DEVELOPMENT.md).

Operator config: [docs/CONFIGURATION.md](docs/CONFIGURATION.md). Koanf **defaults → `silo.yaml` → `SILO_*` env**. Nested keys use `__` (`SILO_OPENROUTER__API_KEY` → `openrouter.api_key`). OpenRouter key is YAML/env, not Admin. Default model when unset: `openai/gpt-5.6-luna`.

## Shape

- **Control Plane (Go):** users, bots, secrets, **security engine** (`internal/security` — catalog of connector.action → title/fields; unknown actions get a generic prompt), **agent loop** (OpenRouter/GPT), Docker lifecycle = create/start/stop/inspect only. Approval slip never dumps `args_json`. `allow_once` is a valid vote.
- **Worker (Go, inside Bot):** dials the CP. Local Unix-socket HTTP for Python tools. Tunnels VNC. Desktop is Openbox + Thunar + iron wallpaper (`botimage/`).
- **Python in the Bot:** `exec_python` + `silo_runtime`. Not the orchestrator.
- **Frontend:** pnpm + React + Tailwind. ConnectRPC + VNC websocket.

UI: 64px rail on every page (Bots, crest switcher, +, Admin). Crest is a picked shape+color (`color * 8 + shape`), not hashed from the name. Bot tabs are `/bots/:id/{run,desktop,files,secrets,rules,container,settings}` labeled Chat, Desktop, Files, Secrets, Rules, Container, Settings. Container is Docker stats (CPU, RAM) plus Start/Stop (`GetContainer`). Create takes a description (shown on the folio). Settings edits name + description via `UpdateBot`. Chat is the chats list + thread (`/bots/:id/run/:chatId`). Tools start collapsed. Thinking is a spinner only while `thinking_chunk`s are the live tail — a later tool/assistant event (or send end) becomes Thought; leaving `streaming` true keeps the spinner forever. Desktop is the hatch only. A connected worker is **online** (pine lamp), not idle. The CP holds the VNC stream until a viewer attaches, then the Worker dials x11vnc and forwards the RFB handshake. Each hatch WebSocket gets a viewer ticket; closing an old socket must not clear a newer one (React Strict Mode remounts the hatch). Replacing a worker session must cancel any waiter on the old session. Sign-in must call `setEmail` from the response or the session cookie is ignored. `StreamRun` persists every event, replays history, and accepts `after_event_id` so the client can reconnect without duplicates. Commands carry `run_id`; children get `SILO_RUN_ID`. Parallel runs on one bot are allowed; bot status is derived from remaining running runs and pending approvals. A Docker inspect error is not proof the box is gone — only not-found / not-running / a box whose `SILO_BOT_TOKEN` hash ≠ `bots.token_hash` clears the cached ID. Persist a new token hash only after Create succeeds; a failed recreate plus `live()` adopting the leftover box is what makes the worker hammer 401s. Worker auth misses use `Find` (not `First`) — GORM `record not found` is not an error log. Desktop processes are fail-fast: if Xvfb/Openbox/x11vnc/worker dies, the container exits and `unless-stopped` recreates it. Chromium is a dock launcher, not a supervised process.

## Two hops

1. **CP ↔ Worker:** ConnectRPC, Worker-initiated, bot token. Concurrent RPCs.
2. **Python ↔ Worker:** HTTP/JSON on `unix:///var/run/silo/worker.sock`.

File tools stay four: `read` (numbered lines, `offset`/`limit`), `write`, `patch` (exactly one `old_text` match), `grep` (`include` glob, capped). Do not add a fifth unless a real loop needs it. The Files tab is a human browser (`mkdir` / `remove` / `put_file` / `browse_file` on the worker, 2 MB). Recreate the box after those worker cmds change.

SOUL and MEMORY live in SQLite (`bots.soul` / `bots.memory`), not on the box. Injected into the system prompt every run. Tools `soul` and `memory` write the DB (no worker hop). Model may write both; Settings can edit them. MEMORY cap is 8000 chars — over that the prompt and the tool tell the model to compact, they do not silent-truncate. ListBots omits the bodies; GetBot/UpdateBot include them.

After create, never `docker exec`, `docker cp`, or a published VNC port.

Container ID in SQLite is a cache. `GetBot`/`ListBots` inspect Docker (and the `silo-{id}` name) and clear a vanished box. `StartBot` recreates if the container is gone. Do not trust a stored ID after a host reboot or a `podman rm`.
