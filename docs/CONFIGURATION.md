# Configuration

Operator config is Koanf. Load order is **defaults → `silo.yaml` → `SILO_*` env**. Later wins.

Copy [silo.yaml.example](../silo.yaml.example) to gitignored `silo.yaml` in the repo root (the process working directory). Nested keys use `__` in env:

```
SILO_PROVIDERS__OPENROUTER__API_KEY → providers.openrouter.api_key
```

Admin Settings (form and YAML editor) writes `silo.yaml` only. Env still wins; the UI shows a warning and disables those fields. `http_addr`, `data_dir`, `docker_host`, and `bootstrap.*` save to YAML but take effect on restart.

Libraries (Connectors, Skills) are not YAML.

## Models and providers

A model id is **`provider/model`**, split on the first `/` (so `openrouter/openai/gpt-5.6-luna` is provider `openrouter`, model `openai/gpt-5.6-luna`). Providers are a modular registry (`internal/llm`): `openrouter`, `openai`, and `anthropic` today. `openrouter`/`openai` speak the OpenAI-compatible API; `anthropic` uses the native Messages API.

- `models` is the operator **allowlist**. Only these ids appear in the chat model picker, and the bot's `switch_model` tool can only choose from it.
- `model` is the default model (used when a chat has no override). It should be one of `models`.
- `model_title` names new chats; leave it empty to use `model`.

Each provider's key lives under `providers.<id>`. Prompt caching is **opt-in per provider** with `cache: true`; Anthropic also takes `cache_ttl` (`5m` or `1h`).

## Keys

| Key | Default | Env | What |
|---|---|---|---|
| `http_addr` | `:8080` | `SILO_HTTP_ADDR` | Control Plane listen address |
| `data_dir` | `./data` | `SILO_DATA_DIR` | Per-bot volumes + skill bodies |
| `database_url` | `postgres://silo:silo@localhost:5433/silo?sslmode=disable` | `SILO_DATABASE_URL` | Postgres with pgvector (`make db-up` for dev). Restart to apply |
| `docker_host` | `$DOCKER_HOST` | `SILO_DOCKER_HOST` | Docker/Podman socket. Empty falls back to the `DOCKER_HOST` env |
| `public_url` | (request origin) | `SILO_PUBLIC_URL` | Browser origin for OAuth redirects (`{public_url}/oauth/callback`). Dev: `http://127.0.0.1:5173` |
| `cp_url` | `http://host.containers.internal:8080` | `SILO_CP_URL` | URL Bot containers and STDIO sidecars use to dial the CP |
| `bot_image` | `localhost/silo-bot:v1` | `SILO_BOT_IMAGE` | Image tag `StartBot` / create use |
| `mcp_stdio_image` | `localhost/silo-mcp-stdio:v1` | `SILO_MCP_STDIO_IMAGE` | Default image for STDIO MCP sidecars. A connector may override with `stdio_image` (admin) |
| `model` | `openrouter/openai/gpt-5.6-luna` | `SILO_MODEL` | Default chat model (`provider/model`) |
| `model_title` | (none) | `SILO_MODEL_TITLE` | Chat title model. Empty = `model` |
| `models` | (none) | — | Allowlist of selectable models (YAML list) |
| `providers.<id>.api_key` | (none) | `SILO_PROVIDERS__<ID>__API_KEY` | Provider key. Required when that provider is used |
| `providers.<id>.base_url` | (provider default) | `SILO_PROVIDERS__<ID>__BASE_URL` | Override the API base URL |
| `providers.<id>.cache` | `false` | `SILO_PROVIDERS__<ID>__CACHE` | Enable prompt caching (`openai` sends `prompt_cache_key`; `anthropic` adds breakpoints) |
| `providers.<id>.cache_ttl` | `5m` | `SILO_PROVIDERS__<ID>__CACHE_TTL` | Anthropic cache lifetime (`5m`/`1h`) |
| `providers.anthropic.max_tokens` | `8192` | `SILO_PROVIDERS__ANTHROPIC__MAX_TOKENS` | Anthropic per-response output cap |
| `bootstrap.email` | (none) | `SILO_BOOTSTRAP__EMAIL` | First admin only. Ignored after a user exists |
| `bootstrap.password` | (none) | `SILO_BOOTSTRAP__PASSWORD` | Same. Wipe `data/` to re-seed |
| `search.engine` | `duckduckgo_scraper` | `SILO_SEARCH__ENGINE` | Web search engine. Future engines may add keys under `search.<engine_id>` |
| `connector_vars.<name>` | (none) | `SILO_CONNECTOR_VARS__<NAME>` | Connector variable. Referenced as `${NAME}` in connector settings. **Plain text, not a secret** |

## Connector variables

`connector_vars` is a flat map of operator-defined values that connectors can reuse. A connector references one as **`${NAME}`** in its URL, extra headers, OAuth Client ID/Secret, STDIO command/arguments/image, STDIO env values, and its prompt. Names must match `[A-Za-z_][A-Za-z0-9_]*`; env keys are lowercased, so a YAML key is also matched case-insensitively.

```yaml
connector_vars:
  TENANT: acme
  MCP_HOST: mcp.internal.example.com
```

```yaml
# then a connector can use:
http_url: https://${MCP_HOST}/${TENANT}/mcp
```

These are **variables, not secrets**. They are stored in `silo.yaml` in plain text and are not encrypted or masked; anyone who can read the config can read them. Use a Bot Secret for credentials. Values resolve when a connector connects, so a variable change takes effect on the next reconnect (use Refresh on the Connectors tab).

This machine:

```
export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
```

`podman machine` / Docker Desktop: set `docker_host` to that socket instead.

`cp_url` must be reachable from inside the Bot and from STDIO sidecars. Create adds `host.containers.internal:host-gateway` to both. If the worker or a bridge never connects, the container cannot see the CP.

## Example

```yaml
http_addr: ":8080"
data_dir: ./data
docker_host: unix:///run/user/1000/podman/podman.sock
cp_url: http://host.containers.internal:8080
bot_image: localhost/silo-bot:v1
mcp_stdio_image: localhost/silo-mcp-stdio:v1
model: openrouter/openai/gpt-5.6-luna

models:
  - openrouter/openai/gpt-5.6-luna
  - anthropic/claude-opus-5

bootstrap:
  email: admin@local
  password: change-me

providers:
  openrouter:
    api_key: "sk-or-…"
    cache: true
  anthropic:
    api_key: "sk-ant-…"
    cache: true
    cache_ttl: 5m

search:
  engine: duckduckgo_scraper
```

## What is not config

- Connector / skill libraries: Postgres + `data/skills/`
- Bot tokens, container IDs, chats, secrets: Postgres (`database_url`)
- A chat's model (override): Postgres `chats.model`
- Session cookie: issued at sign-in. A missing session row is a stale cookie, not a server fault
- Frontend: Vite `web/` proxies `/silo.v1.UI`, `/silo.v1.BotWorker`, `/vnc`, `/console`, `/healthz` to `http_addr`
