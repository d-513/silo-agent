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
| `tunnels.enabled` | `true` | `SILO_TUNNELS__ENABLED` | Master switch for tunnels. Off: the proxy, the Bot's tunnel tools and the Tunnels tab's add form are gone (existing rows are kept) |
| `tunnels.host` | derived (see Tunnels) | `SILO_TUNNELS__HOST` | Domain suffix tunnels are served under, `host[:port]`: a tunnel is `<name>.<host>`. Lower-cased; no scheme, path or wildcard. Must differ from the `public_url` host |
| `tunnels.scheme` | follows `public_url` | `SILO_TUNNELS__SCHEME` | `http` or `https`, for the links tunnels are shown with. Set it when TLS ends at a proxy and `public_url` is an internal address |
| `model` | `openrouter/openai/gpt-5.6-luna` | `SILO_MODEL` | Default chat model (`provider/model`) |
| `model_title` | (none) | `SILO_MODEL_TITLE` | Chat title model. Empty = `model` |
| `model_subagent` | (none) | `SILO_MODEL_SUBAGENT` | Default model for subagents a lead starts with `spawn_agent` (must be in `models`). Empty = the lead's own model. The lead may still name another allowed model |
| `model_memory` | (none) | `SILO_MODEL_MEMORY` | Memory save model: the memory collector reads chats with it (must be in `models`). Empty = the title model. A cheap model is enough |
| `embedding_model` | `openrouter/openai/text-embedding-3-small` | `SILO_EMBEDDING_MODEL` | Embeds long-term memories (`remember`/`recall`) and indexed document chunks (`search_docs`). Must be an OpenAI-compatible provider returning 1536-wide vectors (`dimensions` is sent); changing the width means `make db-reset`. Admin → Settings → Models; a provider that cannot embed (Anthropic) is rejected |
| `transcribe_model` | `openrouter/openai/whisper-1` | `SILO_TRANSCRIBE_MODEL` | Speech-to-text for composer dictation (the mic, `Transcribe` RPC) and the Bot's `transcribe` tool / `silo_runtime.transcribe`. Any OpenAI-compatible `/audio/transcriptions` model: OpenRouter (`openai/whisper-1`, `openai/whisper-large-v3`, …), OpenAI (`whisper-1`, `gpt-4o-transcribe`), or `local/<model>` for LocalAI / Speaches / vLLM / whisper.cpp (`--inference-path /v1/audio/transcriptions`). 25 MB per recording. `off` disables voice (the mic hides and the tool is not offered). A provider that cannot transcribe (Anthropic) is rejected |
| `memory.auto_recall` | `true` | `SILO_MEMORY__AUTO_RECALL` | Inject up to 3 close long-term memories into each run's volatile prompt tail |
| `memory.collect` | `true` | `SILO_MEMORY__COLLECT` | Sweep web and channel chats that have been quiet for 10 minutes with new messages, and save the facts and lessons the Bot missed (one `model_memory` call per chat, each message read once). The composer's memory button works either way |
| `knowledge.enabled` | `true` | `SILO_KNOWLEDGE__ENABLED` | The folder index behind `search_docs`: the background sweep, adding folders on a Bot's Knowledge page, and the tool. Off hides the tool and stops syncing; what is already indexed stays in Postgres |
| `knowledge.sync_interval` | `15m` | `SILO_KNOWLEDGE__SYNC_INTERVAL` | How often an indexed folder is re-checked (Go duration, minimum `1m`). Drive folders are checked four times less often; a folder written to (by the Bot or the Files tab) is re-read about 15 s after it goes quiet. Embeds with `embedding_model` |
| `knowledge.ocr` | `true` | `SILO_KNOWLEDGE__OCR` | Read scanned PDF pages (only pages with next to no text) and image files (`png`/`jpg`/`tif`/`bmp`/`gif`/`webp`, at least 8 KB) with tesseract inside the Bot's box; image text must look like prose or it is dropped. At most 100 pages and 4 minutes per file. English and Polish ship in the bot image; every installed language is used. Off, scans are listed as not indexed (and re-read when you turn it on). A folder of photos makes syncing slow |
| `context.window` | `128000` | `SILO_CONTEXT__WINDOW` | Fallback context window in tokens, for models whose provider does not report one (OpenRouter's `/models` `context_length` is used when it does) |
| `context.compact_at` | `0.8` | `SILO_CONTEXT__COMPACT_AT` | Fraction of the window at which a run summarizes its history into one turn before the next model call (0.1–0.98) |
| `runs.max_duration` | `120m` | `SILO_RUNS__MAX_DURATION` | Cap on one agent run (a Go duration such as `120m`/`2h`, or bare minutes). `-1` means unlimited. Subagent runs and a lead sleeping while its subagents work count toward it |
| `context.windows` | (none) | — | Per-model window overrides (YAML list of `{model, window}`; a list because model ids contain dots). Wins over the provider's report |
| `thinking.levels` | (none) | — | Per-model thinking-level overrides for the composer's Thinking picker (YAML list of `{model, levels}`; levels from `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`; `[]` hides the picker). Wins over the provider's report: OpenRouter `/models` `supported_parameters`, the Anthropic Models API `capabilities`, OpenAI's reasoning families (`o*`, `gpt-5*` → low/medium/high). `local` models have no levels unless listed here (sent as `reasoning_effort`) |
| `models` | (none) | — | Allowlist of selectable models (YAML list). Admin → Settings → Models edits it; under Providers, **List models** asks a provider what its saved key can call and adds a model with one press |
| `providers.<id>.api_key` | (none) | `SILO_PROVIDERS__<ID>__API_KEY` | Provider key. Required when that provider is used (optional for `local`) |
| `providers.<id>.base_url` | (provider default) | `SILO_PROVIDERS__<ID>__BASE_URL` | Override the API base URL |
| `providers.<id>.cache` | `false` | `SILO_PROVIDERS__<ID>__CACHE` | Enable prompt caching (`openai` sends `prompt_cache_key`; `anthropic` adds breakpoints) |
| `providers.<id>.cache_ttl` | `5m` | `SILO_PROVIDERS__<ID>__CACHE_TTL` | Anthropic cache lifetime (`5m`/`1h`) |
| `providers.anthropic.max_tokens` | `8192` | `SILO_PROVIDERS__ANTHROPIC__MAX_TOKENS` | Anthropic per-response output cap |
| `providers.openrouter.ignore` | `DeepInfra` | `SILO_PROVIDERS__OPENROUTER__IGNORE` | Comma list of upstream hosts OpenRouter must not route to (`provider.ignore`). DeepInfra sends a tool call's arguments in one chunk at the end, so Python/terminal/write input cannot stream into the thread. `none` routes anywhere |
| `bootstrap.email` | (none) | `SILO_BOOTSTRAP__EMAIL` | First admin only. Ignored after a user exists |
| `bootstrap.password` | (none) | `SILO_BOOTSTRAP__PASSWORD` | Same. Wipe `data/` to re-seed |
| `auth.password` | `true` | `SILO_AUTH__PASSWORD` | Password sign-in. `false` leaves only OIDC, and only counts while OIDC is configured (see Sign-in) |
| `auth.trusted_proxies` | (none) | `SILO_AUTH__TRUSTED_PROXIES` | Comma-separated IPs/CIDRs of the reverse proxies in front of the control plane. `X-Forwarded-For` is believed only from them |
| `oidc.issuer` | (none) | `SILO_OIDC__ISSUER` | The OIDC provider's issuer URL. OIDC sign-in is on when this and `oidc.client_id` are set |
| `oidc.client_id` | (none) | `SILO_OIDC__CLIENT_ID` | OAuth client ID registered at the provider |
| `oidc.client_secret` | (none) | `SILO_OIDC__CLIENT_SECRET` | Its secret |
| `oidc.scopes` | `openid email profile` | `SILO_OIDC__SCOPES` | Space-separated scopes. `openid` is always requested |
| `oidc.label` | `Single sign-on` | `SILO_OIDC__LABEL` | Text of the button on the sign-in page |
| `oidc.auto_create` | `false` | `SILO_OIDC__AUTO_CREATE` | Make an account the first time someone signs in |
| `oidc.allowed_domains` | (none) | `SILO_OIDC__ALLOWED_DOMAINS` | Comma-separated email domains `auto_create` is limited to |
| `oidc.groups_claim` | `groups` | `SILO_OIDC__GROUPS_CLAIM` | ID token claim that lists a person's groups |
| `oidc.admin_group` | (none) | `SILO_OIDC__ADMIN_GROUP` | Members of this group are admins, checked at every OIDC sign-in |
| `search.engine` | `duckduckgo_scraper` | `SILO_SEARCH__ENGINE` | Web search engine. Future engines may add keys under `search.<engine_id>` |
| `connector_vars.<name>` | (none) | `SILO_CONNECTOR_VARS__<NAME>` | Connector variable. Referenced as `${NAME}` in connector settings. **Plain text, not a secret** |
| `autoenable_connectors` | (none) | `SILO_AUTOENABLE_CONNECTORS` | List of library connector identifiers every new Bot gets. The env form is comma-separated (`lightpanda,fal_ai`) |

## Sign-in

Accounts live in Postgres; **Admin → Users** manages them. `bootstrap.*` makes the first admin and nothing else.

- **New people get an invite link.** An admin makes one for an email address (optionally as an admin); the person opens it and chooses their own password. A link works once and lasts 7 days. Silo sends no mail: the admin passes the link on. A password is at least 8 characters.
- **A forgotten password** is a reset link from the user's page in Admin → Users (once, 24 hours), or a password the admin sets there. Either one signs the user out everywhere.
- **Disable** blocks a user's sign-in and stops their Bots (runs, automations, channels, tunnels and the boxes) without deleting anything. **Delete** removes the user with their Bots, the Bots' files and their personal skills. An admin cannot disable, delete or demote themselves, so one admin always remains.
- **Two-factor** is each user's choice on their Account page: an authenticator app (TOTP) plus ten single-use recovery codes, asked for after the password. An admin can turn a user's two-factor off if they lose both. It applies to password sign-in; a sign-in through OIDC is the provider's to protect.
- **Sessions** last 30 days. The Account page lists them and signs any of them out; changing the password signs out all the others. The table holds only a hash of each session cookie. The cookie is `Secure` when `public_url` is `https://`.
- **Guessing is limited.** Wrong passwords, two-factor codes and invite links count per account and address: five tries, then a wait that doubles from 30 seconds to 15 minutes; an address is also held after 20 failures in 15 minutes and an account after 50 in an hour. Counts are in memory and clear on restart.

### Behind a reverse proxy

The limiter and the session list need the visitor's address. By default that is the address of the TCP connection, and `X-Forwarded-For` is ignored, because anyone can send that header. Behind a proxy that makes every visitor look like the proxy: they share one limit, so a few wrong passwords from anyone can make everyone wait. Name the proxy and its header is believed:

```yaml
auth:
  trusted_proxies: 127.0.0.1, 10.0.0.0/8   # the proxy's address as the control plane sees it
```

The client is then the right-most `X-Forwarded-For` entry that is not itself a trusted proxy, so a visitor cannot forge it by sending their own header. The proxy must append to (or set) `X-Forwarded-For`.

### OIDC

One OpenID Connect provider (Authentik, Keycloak, Dex, Google, Entra, …) can sign people in beside, or instead of, passwords. Register a client at the provider with the redirect URI `{public_url}/auth/oidc/callback` (Admin → Settings → Sign-in shows it), then:

```yaml
oidc:
  issuer: https://id.example.com/application/o/silo/
  client_id: silo
  client_secret: "…"
  # label: Company login          # the button's text
  # auto_create: true             # make accounts on first sign-in
  # allowed_domains: example.com  # ...but only for these email domains
  # admin_group: silo-admins      # members are admins (needs a groups claim)
```

**Check** in Admin → Settings → Sign-in asks the provider for its discovery document with the saved settings. A trailing slash on the issuer is tolerated either way.

Who a sign-in becomes:

1. The user already linked to that identity (issuer + subject).
2. Otherwise the user whose email matches, **only if the provider says the email is verified** (`email_verified: true`) and that user is not linked to another identity. They are linked from then on, and their password keeps working.
3. Otherwise, with `auto_create` on (and the domain allowed), a new user with no password. They can set one on their Account page.
4. Otherwise nobody: the sign-in page says to ask an admin for an invite.

A provider that does not send `email_verified` (some Entra setups) can therefore only sign in people who are already linked; the email is read from the userinfo endpoint when the ID token has none. With `admin_group` set, the provider decides who is an admin at every sign-in, except that it never demotes the last one. With it unset, roles are only what Admin → Users says.

`auth.password: false` hides the password form and refuses password sign-in. It is ignored while OIDC is not configured, so removing the OIDC settings brings passwords back.

## Tunnels

A Bot can run a web service on its own localhost (a dev server, a dashboard, a notebook); a **tunnel** gives the owner an address for it, served by this control plane at `<name>.<tunnels.host>` (the name is generated, like `quiet-amber-heron`). Private tunnels open for the Bot's owner, signed in to Silo; public ones for anyone with the link. Make one on the Bot's **Tunnels** tab, or the Bot opens one with `open_tunnel` (making one public asks the owner first).

```yaml
tunnels:
  host: tunnels.example.com   # required unless public_url is local (below)
  # enabled: true
  # scheme: https             # default: public_url's scheme
```

- **Local development**: with `public_url` on `localhost`/`127.0.0.1`, `host` defaults to `localhost:<port of http_addr>` (links like `http://quiet-amber-heron.localhost:8080`). Browsers resolve `*.localhost` to loopback, so no DNS is needed. It uses the control plane's own port, not Vite's.
- **Anywhere else, set `host`.** Without it tunnels are "not configured": the tab says so and the Bot has no tunnel tools.
- **DNS and TLS**: point a wildcard record `*.<host>` at the control plane and serve a certificate for `*.<host>` (a wildcard certificate, or on-demand TLS in the fronting proxy). The proxy in front must pass WebSocket upgrades and the original `Host` header.
- **Use a separate domain** from the control plane's (`silo.example.com` → tunnels under `silo-tunnels.example.com`, not `tunnels.silo.example.com`). A page a Bot serves could otherwise set cookies for the whole domain and reach the control plane with them; a `host` equal to the `public_url` host is refused outright.
- **Changing `host`** takes effect on the next request and re-points every existing tunnel (rows store only the name). Private tunnels' sign-in cookies belong to the old host, so owners sign in again.
- **Private means signed in to Silo.** A private tunnel's access is tied to the owner's Silo session: signing out (or the session expiring) ends it immediately, and the next visit asks them to sign in again. An already-open WebSocket is not cut off until it closes.
- Ports `5900` (the desktop) and `9222` (Chromium's debugging port) are never tunnelled; a Bot has at most 20 tunnels. A tunnel request never starts a stopped Bot.

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

## Connectors on every new Bot

Every preset in the Connectors Library has an **identifier**: lowercase letters, digits and underscores, unique in the library (fal.ai is `fal_ai`, Google Drive is `google_drive`). The built-in presets come with one; for your own, the Add form fills it in from the name and you can change it. A preset from before identifiers existed keeps its long id as the identifier until you edit it.

`autoenable_connectors` lists the identifiers of the presets every **new** Bot starts with:

```yaml
autoenable_connectors:
  - lightpanda
  - fal_ai
```

```
export SILO_AUTOENABLE_CONNECTORS=lightpanda,fal_ai
```

It does the same thing as a preset's own **Add to new bots by default** switch, from the config instead of the database, and the two add up: a preset in both is still attached once. Case does not matter. An identifier with no library connector is logged and skipped, so the file can name a preset before it exists. Like the switch, it applies when a Bot is created: existing Bots are not changed, and a connector removed from a Bot is not re-added. Admin → Settings → Connector options edits the list (read-only while the env variable is set).

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

auth:
  trusted_proxies: 127.0.0.1   # only when a reverse proxy is in front

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
- Users, sessions, invite links and two-factor secrets: Postgres. A missing session row is a stale cookie, not a server fault
- Frontend: Vite `web/` proxies `/silo.v1.UI`, `/silo.v1.BotWorker`, `/vnc`, `/console`, `/healthz`, `/auth` to `http_addr`
