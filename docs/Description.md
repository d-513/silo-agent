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
| Operator config | Koanf: defaults → YAML → env (`SILO_FOO__BAR` → `foo.bar`) | listen addr, sqlite path, docker host, **OpenRouter API key**. **Not** editable in the UI |
| Product settings | DB rows, admin UI | models. Do not overlap with operator config |
| Desktop | X11 (Xvfb + Openbox + Thunar) | not Wayland |
| Remote desktop | x11vnc + noVNC, **tunneled on the Worker session** | no published VNC port, no CP dial-back |
| Process supervisor in Bot | fail-fast `start.sh` | any of Xvfb/Openbox/tint2/x11vnc/worker dying exits the container; Docker `unless-stopped` recreates the stack |
| Web extract/search | not in v1 | keep provider keys off the Bot |

Python is not a ConnectRPC citizen. Generated `tools` never see `SILO_CP_URL` or `SILO_BOT_TOKEN`.

## Two hops, not one

```
Browser
  │  ConnectRPC (session) + VNC websocket (session)
  ▼
Control Plane                          Docker API (any host): create/start/stop/inspect only
  │                                    inject SILO_CP_URL, SILO_BOT_TOKEN, SILO_BOT_ID
  │
  │  ConnectRPC  (Worker dials out, bot token)
  │  commands · results · GetSecret · VNC bytes
  ▼
Go Worker
  │  HTTP/JSON  unix:///var/run/silo/worker.sock
  ▼
Python  (exec_python + generated tools.*)
```

There is no third path. No `docker exec`, no `docker cp`, no socket mount into the CP, no host-network assumption, no CP connecting to a port on the Bot. A remote Docker host is the same protocol with a different `SILO_CP_URL` (must be reachable *from that host*, not `127.0.0.1` of the CP machine). Volumes live on the machine that runs the container; the CP reads files by asking the Worker.

`SILO_BOT_TOKEN` is rotated on recreate. `docker inspect` can see it; acceptable for v1.

### CP ↔ Worker (ConnectRPC)

Worker-facing service, auth: `Authorization: Bearer <bot-token>` on every call. One HTTP/2 connection, **several concurrent RPCs** — do not funnel everything through a single synchronous request. `exec_python` stays open while Python calls `GetSecret`; that call is a *separate* RPC. Commands carry `run_id` so parallel runs stay correlated.

```
service BotWorker {
  rpc Commands(stream CmdEvent) returns (stream Cmd);
  rpc GetSecret(SecretReq) returns (SecretRes);
  rpc VNC(stream Frame) returns (stream Frame);
}
```

Approvals: `GetSecret` does not return until the security engine decides. The Python HTTP request simply blocks. Heartbeat on `Commands` so a long approval does not look like a dead Worker. The VNC RPC waits until a browser viewer attaches, then the Worker dials local x11vnc.

When the Worker spawns Python or a shell, it **strips** `SILO_BOT_TOKEN` and `SILO_CP_URL` from the child env. Children get `SILO_WORKER_SOCK=/var/run/silo/worker.sock` and `SILO_RUN_ID` for the current run.

### Python ↔ Worker (local bus)

This hop is same-container only. It must not know the CP exists.

**Transport:** HTTP/1.1 JSON over a Unix socket at `/var/run/silo/worker.sock`. Not TCP (a future `-p` publish would leak it). Not ConnectRPC (dynamic connectors are not a proto we want to codegen into every stub; the model will read these files).

**One shipped client**, not generated: `silo_runtime` (site-packages in the image).

```python
# silo_runtime — the only file that knows the socket
def get_secret(name: str) -> str: ...
def call(connector: str, action: str, args: dict) -> dict: ...
```

Local HTTP surface (Worker listens, nothing else):

| Method | Path | Body | Result |
|---|---|---|---|
| `POST` | `/v1/secrets/get` | `{name, run_id?}` | `{value}` or `{error}` |
| `POST` | `/v1/tools/call` | `{connector, action, args, run_id?}` | `{result}` or `{error}` |

No local auth. The container is the trust boundary; the CP security engine is the gate. Debug: `curl --unix-socket /var/run/silo/worker.sock http://localhost/v1/...`.

Worker on receive: translate to `GetSecret` or `CallTool`, wait, JSON the result back. Register any returned secret value with the **masker** before writing it to the socket.

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

First-class tools the model sees stay small: `exec_python`, `terminal`, files (`read`/`write`/`patch`/`grep`), `present`, maybe `browser_snapshot`. `read` is numbered and sliced (`offset`/`limit`). `patch` requires a unique `old_text`. `grep` takes `include` and is capped. `present` shows a workspace file in the thread (CP uses `browse_file`; the model gets a short ack, not the bytes). Connectors are discovered on disk as `tools.*`.

## Tools

### Terminal / files

Pushed on `Commands`. `/workspace` is the agent's artifact dir (volume on the *Docker host*, not necessarily the CP host).

### Web / connectors

Admin owns a **catalog** of connectors (type `mcp` for now). HTTP MCP only; STDIO is reserved. Auth is `none` or `oauth`. Extra headers stay on the CP. Each catalog entry has a **default mode** (`allow` / `ask` / `deny`) used when a Bot has no rule for that action. Bots attach from the catalog. The CP is the MCP client (`internal/mcpx`, streamable HTTP). Worker generates `tools/<slug>/*.py` stubs that `silo_runtime.call` → `CallTool`. OAuth uses MCP authorization code + PKCE; tokens never enter the Bot. `public_url` is the browser origin for `/oauth/callback`.

### Chromium

Not started with the desktop. The dock **Web** item (or `chromium` with the flags in `silo-web.desktop`) launches a headed browser on `DISPLAY=:1`, persistent `--user-data-dir=/home/bot/chrome-profile`, CDP on `127.0.0.1:9222`. Closing Chromium must not take down the container. Playwright/CDP attaches when it is running. Python may use `localhost:9222` with no Worker hop — CDP has no secret.

### Computer use (later)

Same `:1`. Not v1.

### Images

Path refs (`[[silo:image /path]]` or structured `{type:"image", path}`), turned into multimodal parts on the CP. No base64-in-stdout.

## Auth and multi-user

v1: session cookie, owner sees their bots, admin sees settings. `User` + `Session` tables; OIDC later is "create session after IdP callback".

## What we are explicitly not doing in v1

- Wayland
- Multi-host Docker *UI* (the Worker protocol is already remote-safe)
- OIDC
- Computer-use loop
- STDIO MCP (schema only)
- Generated tools calling the CP, or a Python ConnectRPC client
- Provider keys or connector tokens inside the Bot
- `docker exec` / `docker cp` / published VNC as a control channel
- One overlapping config file that the web UI also edits
- Inline base64 images in tool text
