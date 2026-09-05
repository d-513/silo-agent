# Configuration

Operator config is Koanf. Load order is **defaults → `silo.yaml` → `SILO_*` env**. Later wins.

Copy [silo.yaml.example](../silo.yaml.example) to gitignored `silo.yaml` in the repo root (the process working directory). Nested keys use `__` in env:

```
SILO_OPENROUTER__API_KEY → openrouter.api_key
```

Do not put the OpenRouter key in Admin or SQLite.

## Keys

| Key | Default | Env | What |
|---|---|---|---|
| `http_addr` | `:8080` | `SILO_HTTP_ADDR` | Control Plane listen address |
| `data_dir` | `./data` | `SILO_DATA_DIR` | SQLite + per-bot volumes |
| `docker_host` | `$DOCKER_HOST` | `SILO_DOCKER_HOST` | Docker/Podman socket. Empty falls back to the `DOCKER_HOST` env |
| `public_url` | (request origin) | `SILO_PUBLIC_URL` | Browser origin for OAuth redirects (`{public_url}/oauth/callback`). Dev: `http://127.0.0.1:5173` |
| `cp_url` | `http://host.containers.internal:8080` | `SILO_CP_URL` | URL the **Bot container** uses to dial the CP |
| `bot_image` | `localhost/silo-bot:v1` | `SILO_BOT_IMAGE` | Image tag `StartBot` / create use |
| `bootstrap.email` | (none) | `SILO_BOOTSTRAP__EMAIL` | First admin only. Ignored after a user exists |
| `bootstrap.password` | (none) | `SILO_BOOTSTRAP__PASSWORD` | Same. Wipe `data/` to re-seed |
| `openrouter.api_key` | (none) | `SILO_OPENROUTER__API_KEY` | Required for the agent loop |

This machine:

```
export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
```

`podman machine` / Docker Desktop: set `docker_host` to that socket instead.

`cp_url` must be reachable from inside the Bot. Create adds `host.containers.internal:host-gateway`. If the worker never connects, the container cannot see the CP.

## Example

```yaml
http_addr: ":8080"
data_dir: ./data
docker_host: unix:///run/user/1000/podman/podman.sock
cp_url: http://host.containers.internal:8080
bot_image: localhost/silo-bot:v1

bootstrap:
  email: admin@local
  password: change-me

openrouter:
  api_key: "sk-or-…"
```

## Product settings (not YAML)

Admin stores the **model slug** in SQLite. It is not operator config.

Default when unset: `openai/gpt-5.6-luna` (`config.DefaultModel`). Change it in Admin. Saving writes a `settings` row; the code fallback is used only when that row is empty.

API keys, listen address, Docker host, and `cp_url` stay in YAML / env. Admin will not show or store them.

## What is not config

- Bot tokens, container IDs, chats, secrets: SQLite under `data_dir`
- Session cookie: issued at sign-in. A missing session row is a stale cookie, not a server fault
- Frontend: Vite `web/` proxies `/silo.v1.UI`, `/silo.v1.BotWorker`, `/vnc`, `/console`, `/healthz` to `http_addr`
