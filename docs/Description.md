# Silo Agent

A token-conserving, multi-user, containerized, secure agent for personal and team tasks.

Each **Bot** is one durable Linux container: a machine the agent works on, with a GUI the user can step into (login, captcha, "what is it doing?"). Bots do not share state. One user can have many Bots.

## Verdict

Control Plane owns policy, the agent loop, LLM calls, and secrets. The Bot is a dumb executor with a desktop. Docker may be local or on another machine — that must not change the Worker protocol. After create, the only CP↔Worker channel is ConnectRPC, Worker-initiated.

## Stack (locked)

| Piece | Choice | Why |
|---|---|---|
| Control Plane | Go | one binary, Docker API, SQLite, ConnectRPC |
| Worker (inside Bot) | Go | only process that talks to the CP; supervises local exec |
| Agent execution | Python *inside the Bot* | the model writes/runs Python; it does not own the loop |
| Frontend | pnpm + React + Tailwind + Vite | admin + bot list + VNC embed + approvals |
| Remote RPC | ConnectRPC + protobuf | CP↔UI and CP↔Worker only. Worker *dials out*. |
| Local tools bus | HTTP/JSON over a Unix socket | Python↔Worker, same container, never leaves the Bot |
| DB | SQLite + GORM, WAL mode | single-node v1; no repository layer; sqlc only if GORM hurts |
| Auth | session cookies now | OIDC later creates the same `User`/`Session` rows — no plugin framework |
| Operator config | Koanf: defaults → YAML → env (`SILO_FOO__BAR` → `foo.bar`) | listen addr, sqlite path, docker host, OpenRouter key, model, search, STDIO MCP image. Admin Settings writes `silo.yaml`. |
| Product libraries | SQLite + `data/skills/` | Connectors Library, Skills Library. Not YAML. |
| Desktop | X11 (Xvfb + Openbox + Thunar + mousepad) | not Wayland |
| Remote desktop | x11vnc + noVNC, **tunneled on the Worker session** | no published VNC port, no CP dial-back |
| Process supervisor in Bot | fail-fast `start.sh` | any of Xvfb/Openbox/tint2/x11vnc/worker dying exits the container; Docker `unless-stopped` recreates the stack |
| Web search | CP engine registry (`internal/search`) | keys stay off the Bot; DuckDuckGo Scraper is the default; extract later |

Python is not a ConnectRPC citizen. Generated `tools` never see `SILO_CP_URL` or `SILO_BOT_TOKEN`.

## Two hops, not one

```
Browser
  │  ConnectRPC (session) + VNC / console websocket (session)
  ▼
Control Plane                          Docker API (any host): create/start/stop/inspect only
  │                                    inject SILO_CP_URL, SILO_BOT_TOKEN, SILO_BOT_ID
  │
  │  ConnectRPC  (Worker dials out, bot token)
  │  commands · results · GetSecret · VNC bytes · console PTY
  ▼
Go Worker
  │  HTTP/JSON  unix:///var/run/silo/worker.sock
  ▼
Python  (exec_python + generated tools.*)
```

There is no third path. No `docker exec`, no `docker cp`, no socket mount into the CP, no host-network assumption, no CP connecting to a port on the Bot. A remote Docker host is the same protocol with a different `SILO_CP_URL` (must be reachable *from that host*, not `127.0.0.1` of the CP machine). Volumes live on the machine that runs the container; the CP reads files by asking the Worker.

`SILO_BOT_TOKEN` is rotated on recreate (box gone), not on Stop/Start of the same ID. `docker inspect` can see it; acceptable for v1.

### CP ↔ Worker (ConnectRPC)

Worker-facing service, auth: `Authorization: Bearer <bot-token>` on every call. One HTTP/2 connection, **several concurrent RPCs** — do not funnel everything through a single synchronous request. `exec_python` stays open while Python calls `GetSecret`; that call is a *separate* RPC. Commands carry `run_id` so parallel runs stay correlated.

```
service BotWorker {
  rpc Commands(stream CmdEvent) returns (stream Cmd);
  rpc GetSecret(SecretReq) returns (SecretRes);
  rpc CallTool(ToolReq) returns (ToolRes);
  rpc VNC(stream Frame) returns (stream Frame);
  rpc Console(stream ConsoleIO) returns (stream ConsoleIO);
}
```

Approvals: `GetSecret` does not return until the security engine decides. The Python HTTP request simply blocks. Heartbeat on `Commands` so a long approval does not look like a dead Worker. The VNC RPC waits until a browser viewer attaches, then the Worker dials local x11vnc. Console is the same wait-then-pipe, but the Worker opens a PTY (`bash` in `/workspace`) instead of x11vnc. Resize is rows/cols on `ConsoleIO`. Do not `docker exec` / `podman exec` for this — a remote Docker host has no such path.

When the Worker spawns Python or a shell, it **strips** `SILO_BOT_TOKEN` and `SILO_CP_URL` from the child env. Children get `SILO_WORKER_SOCK=/var/run/silo/worker.sock` and `SILO_RUN_ID` for the current run.

### Python ↔ Worker (local bus)

This hop is same-container only. It must not know the CP exists.

**Transport:** HTTP/1.1 JSON over a Unix socket at `/var/run/silo/worker.sock`. Not TCP (a future `-p` publish would leak it). Not ConnectRPC (dynamic connectors are not a proto we want to codegen into every stub; the model will read these files).

**One shipped client**, not generated: `silo_runtime` (site-packages in the image).

```python
# silo_runtime — the only file that knows the socket
def get_secret(name: str) -> str: ...
def look() -> str: ...  # writes bot/screen.png
def click(x, y, button="left"): ...
def type_text(text: str): ...  # not type — that shadows Python
def key(name: str): ...
def scroll(x, y, dy): ...
def web_search(query: str, max_results: int | None = None) -> dict: ...
def call(connector: str, action: str, args: dict) -> dict: ...
def chrome_page(): ...  # Playwright Page; worker opens Chromium if needed
```

Local HTTP surface (Worker listens, nothing else):

| Method | Path | Body | Result |
|---|---|---|---|
| `POST` | `/v1/secrets/get` | `{name, run_id?}` | `{value}` or `{error}` |
| `POST` | `/v1/tools/call` | `{connector, action, args, run_id?}` | `{result}` or `{error}` |
| `POST` | `/v1/chrome/ensure` | `{}` | `{status}` (`up` / `started`) or `{error}` |

No local auth. The container is the trust boundary; the CP security engine is the gate. Debug: `curl --unix-socket /var/run/silo/worker.sock http://localhost/v1/...`.

Worker on receive: translate to `GetSecret` or `CallTool`, wait, JSON the result back. `desktop.*` is CallTool for the grant only; the Worker then runs look/click/type/key/scroll locally and never sends `type` text to the CP. Register any returned secret value with the **masker** before writing it to the socket.

### Why this split

ConnectRPC is the untrusted-network protocol: auth, multiplexing, VNC, remote Docker. The Unix socket is a localhost bus so Python stays a few lines the model can read without seeing URLs, tokens, or proto stubs. Putting a Python Connect client in `/opt/tools` would teach every run how to speak to the CP the moment someone leaks a token into the env.

## Secret sanitization

Agree: rogue `print` is a different class of failure from *shipping the secret to the provider on every turn*. Treat egress to the provider like CI logs.

Two maskers, same algorithm (defense in depth):

1. **Worker** — values it has actually handed to Python this session (`GetSecret` responses, plus anything the CP tells it was injected). Applied to stdout/stderr chunks and tool results *before* they go on ConnectRPC.
2. **Control Plane** — every secret in that bot's store, connector tokens, and the bot token itself. Applied before persist and before the provider request. The CP never writes an unmasked secret into the run log.

Algorithm (GitHub Actions-style, not cryptographic):

- Exact substring replace with `***` for each known value with length ≥ 8 (short pins will false-positive; that is the user's problem).
- Also mask common encodings of the same value: base64, URL-encoding, JSON-escaped, hex.
- Register a value the instant it is issued, before the bytes hit the Unix socket.

The model can still exfiltrate a secret it already holds. We do not try to stop that here. We do stop the boring case: leftover IMAP password in a traceback, `print(os.environ)`, a file read of something that contains a known secret, echoing into the next completion.

## Who owns the agent loop

**The Control Plane.** The Worker is a dumb executor plus event stream. Python is `exec_python` plus `/opt/silo/tools` — the [code execution with MCP](https://www.anthropic.com/engineering/code-execution-with-mcp) pattern.

First-class tools the model sees stay small: `exec_python`, `terminal`, files (`read`/`write`/`patch`/`grep`), `present`, `skill` / `propose_skill`, `web_search`. `read` is numbered and sliced (`offset`/`limit`). `patch` requires a unique `old_text`. `grep` takes `include` and is capped. `present` of a user-facing path is a folio in the thread. `present` of `bot/…` is scratch: collapsed “Looked at …” row, same pixels to the model. CP uses `browse_file`; the tool text is a short ack. Images (`png`/`jpg`/`webp`/`gif`) attach as a multimodal part on the next completion. No base64 in the tool text or the run log. Connectors are discovered on disk as `tools.*`. Skills follow the [Agent Skills](https://agentskills.io/specification) format (`SKILL.md`); bodies live on the CP under `data/skills/` (library + per-user personal). Enablement is SQLite `bot_skills`. The prompt lists name + description only; `skill` loads the markdown; extras sync to `/opt/silo/skills/<name>/`. Every gated action (`python.run`, `terminal.run`, `files.*`, `desktop.*`, `bot.soul`/`memory`, `skills.load`/`propose`, `web.search`, `secrets.<name>`, MCP `slug.action`) goes through one `authorizeAction`: bot rule, else `*` for that connector, else the builtin catalog default, else the MCP connector default, else ask. Python and Terminal default allow. `skills.load` and `skills.propose` default allow. `web.search` default allow. `propose_skill` emits a thread artifact; **Save skill** on the card copies the workspace dir to personal — there is no Ask slip that installs. Each secret is its own action — not `secrets.get`. Chat tools and Python `CallTool` share that gate. `desktop.*` from Python is grant-then-local on the Worker so typed secrets never ride to the CP. `web.search` runs on the CP (engine registry); Python reaches it via `silo_runtime.web_search`.

## Tools

### Terminal / files

Pushed on `Commands`. `/workspace` is user-facing artifacts (volume on the *Docker host*, not necessarily the CP host). `/workspace/bot` is the Bot's scratch (PRAV screenshots, dumps).

### Skills

Catalog skills are `go:embed`’d under `internal/catalog/skills` and seeded into `data/skills/library/` (insert-if-absent). Personal skills are `data/skills/users/<userID>/`. Install from a GitHub URL / `owner/repo` / `SKILL.md` / zip (same size caps as `npx skills`, no npx). Admin Skills Library can re-add defaults and remove. The Skill Hub (`/skills`) lists Personal | Library. A Bot Skills tab toggles enablement (unique `name` per bot). Worker `SyncSkills` replaces `/opt/silo/skills`.

### Web search

CP-only. `internal/search` is a registry of engines (`Descriptor` + `Engine.Search`). Default is DuckDuckGo Scraper (`duckduckgo_scraper`) from Koanf `search.engine`. Admin `/admin/search-extract` writes that key in `silo.yaml`. Provider keys never enter the Bot. Chat tool `web_search` and `silo_runtime.web_search` both run `web.search` after `authorizeAction`. Page extract is not in yet.

### Web / connectors

Admin owns a **library** of connector presets (type `mcp`), seeded from `internal/catalog`. HTTP MCP (streamable HTTP / legacy SSE) and STDIO MCP. Auth is `none` or `oauth` (HTTP only). Extra headers stay on the CP. Each preset has a **default mode** (`allow` / `ask` / `deny`) used when a Bot has no rule for that action. Catalog entries may include a `guide` shown as text when adding from the library — it is not a connector field. Bots attach a **copy** of a preset (shared form, pre-filled) as many times as they want — a second GitHub is `GitHub 2` with its own OAuth and `tools.github_2`. They can also add a custom MCP that never enters the library. The CP is the MCP client (`internal/mcpx`). Worker generates `tools/<slug>/*.py` stubs that `silo_runtime.call` → `CallTool`. OAuth uses MCP authorization code + PKCE; tokens never enter the Bot. `public_url` is the browser origin for `/oauth/callback`. Servers that omit `registration_endpoint` (GitHub) need a pre-registered OAuth Client ID and Secret on the connector.

STDIO connectors run **outside** the Bot: one isolated sidecar container per attachment (`silo-mcp-<botConnectorID>`), on the same Docker engine as Bots. The sidecar image (`mcp_stdio_image`, default `localhost/silo-mcp-stdio:v1`) includes Node/`npx`, Python, and `silo-mcp-bridge`. A library preset may name a curated image that still ships that bridge as entrypoint. The bridge starts exactly one child from structured `command` + `args` (never a shell), speaks MCP JSON-RPC on the child's stdin/stdout, and serves streamable HTTP on a unix socket bind-mounted from `data/mcp/<id>/`. The CP dials that socket with the same `mcpx` client used for HTTP MCPs, so authorization, audit, masking, and tool stubs are identical. Env values are CP-side (plaintext or a Bot secret name, resolved only at sidecar create). Docker inspect of the sidecar can still see injected environment — treat that as the v1 ceiling. Sidecars get no Bot volumes and no bot token. They are created on attach/refresh, replaced on connector edit, and removed on detach, bot delete, and CP shutdown. Custom STDIO command/args are allowed for the Bot owner; only an admin can set `stdio_image`.

### Chromium

Not autostarted with the desktop. The dock **Chromium** item and `silo-chromium` launch a headed browser on `DISPLAY=:1`, persistent `--user-data-dir=/home/silo/chrome-profile`, CDP on `127.0.0.1:9222`, ~67% page zoom, and uBlock Origin Lite (managed policy: ads, cookie banners, overlays). Closing Chromium must not take down the container. GUI work uses first-class `look` / `click` / `type` / `key` / `scroll` on the 1280×720 X11 desktop (screenshot pixels = display pixels; no scale) — that is the live click path. Playwright `silo_runtime.chrome_page()` is for page screenshots, mutating displayed HTML, and automated scripts. The worker opens `silo-chromium` when `exec_python` mentions Playwright / `chrome_page`; `chrome_page` also hits `POST /v1/chrome/ensure`. Do not `playwright install` a second browser. The model must not call `pyautogui` or `xdotool`; the worker may. The desktop, console, and worker run as user `silo` (uid 1000), not root; `sudo` is passwordless.

### Computer use

`look` grabs `DISPLAY=:1` root to `/workspace/bot/screen.png` and attaches the PNG (no `detail: high`). Coordinate law on every look: image is 1280×720, origin top-left, `click(x,y)` in those pixels. Prompt ladder: look → one act → look. The same actions exist on `silo_runtime` for programmatic sequences (type a secret into a field). Playwright is screenshots / DOM edits / scripts, not the click loop.

### Images

`present` of `png`/`jpg`/`webp`/`gif`: user-facing paths are a folio preview; `bot/…` is a collapsed row. The next model turn gets a multimodal image part from `browse_file` data. Tool text stays the short ack. No base64-in-stdout, no image bytes in the run log.

## Auth and multi-user

v1: session cookie, owner sees their bots, admin sees settings. `User` + `Session` tables; OIDC later is "create session after IdP callback".

## What we are explicitly not doing in v1

- Wayland
- Multi-host Docker *UI* (the Worker protocol is already remote-safe)
- OIDC
- STDIO MCP in the Bot container (sidecars are CP-managed, isolated from the Bot)
- Generated tools calling the CP, or a Python ConnectRPC client
- Provider keys or connector tokens inside the Bot
- `docker exec` / `docker cp` / published VNC as a control channel
- One overlapping config file that the web UI also edits
- Inline base64 images in tool text
