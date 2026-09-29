# Configuration

Operator config is Koanf. Load order is **defaults → `silo.yaml` → `SILO_*` env**. Later wins.

Copy [silo.yaml.example](../silo.yaml.example) to gitignored `silo.yaml` in the repo root (the process working directory). Nested keys use `__` in env:

```
SILO_PROVIDERS__OPENROUTER__API_KEY → providers.openrouter.api_key
```

Admin Settings (form and YAML editor) writes `silo.yaml` only. Env still wins; the UI shows a warning and disables those fields. `http_addr`, `data_dir`, `docker_host`, and `bootstrap.*` save to YAML but take effect on restart.

Libraries (Connectors, Skills) are not YAML.

## Models and providers

A model id is **`provider/model`**, split on the first `/` (so `openrouter/openai/gpt-5.6-luna` is provider `openrouter`, model `openai/gpt-5.6-luna`). Providers are a modular registry (`internal/llm`): `openrouter`, `openai`, `anthropic`, and `local` today. `openrouter`/`openai`/`local` speak the OpenAI-compatible API; `anthropic` uses the native Messages API. `local` is any self-hosted OpenAI-compatible server (LocalAI, Ollama, vLLM, Speaches, whisper.cpp): `providers.local.base_url` is required and `api_key` is optional. Silo never runs models itself; it only connects to them.

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
| `drives.image` | `localhost/silo-drive:v1` | `SILO_DRIVES__IMAGE` | The rclone drive sidecar (`make drive-image`) |
| `drives.mount_root` | `<data_dir>/drives-mnt`; on macOS `/var/tmp/silo-drives` | `SILO_DRIVES__MOUNT_ROOT` | Where drive mounts live, as a path the container engine sees. It must be able to carry mount propagation, so on podman machine it is inside the VM, not the virtiofs `data/` share |
| `drives.cache_max_size` | `10G` | `SILO_DRIVES__CACHE_MAX_SIZE` | rclone VFS cache cap per drive, kept in `data/drives/<bot>/cache` |
| `model` | `openrouter/openai/gpt-5.6-luna` | `SILO_MODEL` | Default chat model (`provider/model`) |
| `model_title` | (none) | `SILO_MODEL_TITLE` | Chat title model. Empty = `model` |
| `model_subagent` | (none) | `SILO_MODEL_SUBAGENT` | Default model for subagents a lead starts with `spawn_agent` (must be in `models`). Empty = the lead's own model. The lead may still name another allowed model |
| `model_memory` | (none) | `SILO_MODEL_MEMORY` | Memory save model: the memory collector reads chats with it (must be in `models`). Empty = the title model. A cheap model is enough |
| `embedding_model` | `openrouter/openai/text-embedding-3-small` | `SILO_EMBEDDING_MODEL` | Embeds long-term memories (`remember`/`recall`). Must be an OpenAI-compatible provider returning 1536-wide vectors (`dimensions` is sent); changing the width means `make db-reset`. Admin → Settings → Models; a provider that cannot embed (Anthropic) is rejected |
| `transcribe_model` | `openrouter/openai/whisper-1` | `SILO_TRANSCRIBE_MODEL` | Speech-to-text for composer dictation (the mic, `Transcribe` RPC) and the Bot's `transcribe` tool / `silo_runtime.transcribe`. Any OpenAI-compatible `/audio/transcriptions` model: OpenRouter (`openai/whisper-1`, `openai/whisper-large-v3`, …), OpenAI (`whisper-1`, `gpt-4o-transcribe`), or `local/<model>` for LocalAI / Speaches / vLLM / whisper.cpp (`--inference-path /v1/audio/transcriptions`). 25 MB per recording. `off` disables voice (the mic hides and the tool is not offered). A provider that cannot transcribe (Anthropic) is rejected |
| `memory.auto_recall` | `true` | `SILO_MEMORY__AUTO_RECALL` | Inject up to 3 close long-term memories into each run's volatile prompt tail |
| `memory.collect` | `true` | `SILO_MEMORY__COLLECT` | Sweep web and channel chats that have been quiet for 10 minutes with new messages, and save the facts and lessons the Bot missed (one `model_memory` call per chat, each message read once). The composer's memory button works either way |
| `context.window` | `128000` | `SILO_CONTEXT__WINDOW` | Fallback context window in tokens, for models whose provider does not report one (OpenRouter's `/models` `context_length` is used when it does) |
| `context.compact_at` | `0.8` | `SILO_CONTEXT__COMPACT_AT` | Fraction of the window at which a run summarizes its history into one turn before the next model call (0.1–0.98) |
| `runs.max_duration` | `120m` | `SILO_RUNS__MAX_DURATION` | Cap on one agent run (a Go duration such as `120m`/`2h`, or bare minutes). `-1` means unlimited. Subagent runs and a lead sleeping while its subagents work count toward it |
| `context.windows` | (none) | — | Per-model window overrides (YAML list of `{model, window}`; a list because model ids contain dots). Wins over the provider's report |
| `thinking.levels` | (none) | — | Per-model thinking-level overrides for the composer's Thinking picker (YAML list of `{model, levels}`; levels from `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`; `[]` hides the picker). Wins over the provider's report: OpenRouter `/models` `supported_parameters`, the Anthropic Models API `capabilities`, OpenAI's reasoning families (`o*`, `gpt-5*` → low/medium/high). `local` models have no levels unless listed here (sent as `reasoning_effort`) |
| `models` | (none) | — | Allowlist of selectable models (YAML list) |
| `providers.<id>.api_key` | (none) | `SILO_PROVIDERS__<ID>__API_KEY` | Provider key. Required when that provider is used (optional for `local`) |
| `providers.<id>.base_url` | (provider default) | `SILO_PROVIDERS__<ID>__BASE_URL` | Override the API base URL |
| `providers.<id>.cache` | `false` | `SILO_PROVIDERS__<ID>__CACHE` | Enable prompt caching (`openai` sends `prompt_cache_key`; `anthropic` adds breakpoints) |
| `providers.<id>.cache_ttl` | `5m` | `SILO_PROVIDERS__<ID>__CACHE_TTL` | Anthropic cache lifetime (`5m`/`1h`) |
| `providers.anthropic.max_tokens` | `8192` | `SILO_PROVIDERS__ANTHROPIC__MAX_TOKENS` | Anthropic per-response output cap |
| `providers.openrouter.ignore` | `DeepInfra` | `SILO_PROVIDERS__OPENROUTER__IGNORE` | Comma list of upstream hosts OpenRouter must not route to (`provider.ignore`). DeepInfra sends a tool call's arguments in one chunk at the end, so Python/terminal/write input cannot stream into the thread. `none` routes anywhere |
| `bootstrap.email` | (none) | `SILO_BOOTSTRAP__EMAIL` | First admin only. Ignored after a user exists |
| `bootstrap.password` | (none) | `SILO_BOOTSTRAP__PASSWORD` | Same. Wipe `data/` to re-seed |
| `search.engine` | `duckduckgo_scraper` | `SILO_SEARCH__ENGINE` | Web search engine. Future engines may add keys under `search.<engine_id>` |
| `connector_vars.<name>` | (none) | `SILO_CONNECTOR_VARS__<NAME>` | Connector variable. Referenced as `${NAME}` in connector settings. **Plain text, not a secret** |

## Drive providers

Drives mount rclone remotes into a Bot at `/workspace/drives/<name>`. Providers that sign in with OAuth (Google Drive, OneDrive, Dropbox, Box, pCloud) need an OAuth client registered once, with redirect URI `{public_url}/oauth/callback`. Set it in **Admin → Drives** or here:

```yaml
drives:
  providers:
    gdrive:
      client_id: 1234.apps.googleusercontent.com
      client_secret: GOCSPX-…
```

Keys are `drives.providers.<template>.<var>`; the env form is `SILO_DRIVES__PROVIDERS__GDRIVE__CLIENT_SECRET`, and env wins. An unknown template or var is rejected. Form-based providers (S3, WebDAV, Nextcloud, SFTP, …) need nothing here.

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

The **Lightpanda** library preset (auto-attached to new Bots) uses `http_url: ${LIGHTPANDA_URL}/mcp`, so define `LIGHTPANDA_URL` here (dev: `http://localhost:9223`, the shared instance `make db-up` starts). Until it is set the connector fails to connect.

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

context:
  compact_at: 0.8
  windows:
    - model: anthropic/claude-opus-5
      window: 200000

thinking:
  levels:
    - model: local/qwen3-32b
      levels: [low, medium, high]

runs:
  max_duration: 120m   # -1 = unlimited
```

## What is not config

- Connector / skill libraries: Postgres + `data/skills/`
- Bot tokens, container IDs, chats, secrets: Postgres (`database_url`)
- A chat's model (override): Postgres `chats.model`
- Session cookie: issued at sign-in. A missing session row is a stale cookie, not a server fault
- Frontend: Vite `web/` proxies `/silo.v1.UI`, `/silo.v1.BotWorker`, `/vnc`, `/console`, `/healthz` to `http_addr`
