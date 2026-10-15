# Configuration

Operator config is Koanf. Load order is **defaults → `silo.yaml` → `SILO_*` env**. Later wins.

Copy [silo.yaml.example](../silo.yaml.example) to gitignored `silo.yaml` in the repo root (the process working directory). Nested keys use `__` in env:

```
SILO_PROVIDERS__OPENROUTER__API_KEY → providers.openrouter.api_key
```

Admin Settings (form and YAML editor) writes `silo.yaml` only. Env still wins; the UI shows a warning and disables those fields. `http_addr`, `data_dir`, `docker_host`, and `bootstrap.*` save to YAML but take effect on restart.

Libraries (Connectors, Skills) are not YAML.

## Models and providers

A model id is **`provider/model`**, split on the first `/` (so `openrouter/openai/gpt-5.6-luna` is provider `openrouter`, model `openai/gpt-5.6-luna`). Providers are a modular registry (`internal/llm`): `openrouter`, `openai`, `anthropic`, `local`, and `typesafe` today. `openrouter`/`openai`/`local` speak the OpenAI-compatible API; `anthropic` uses the native Messages API; `typesafe` is TypeSafe's System One API and has decision models only (see below). `local` is any self-hosted OpenAI-compatible server (LocalAI, Ollama, vLLM, Speaches, whisper.cpp): `providers.local.base_url` is required and `api_key` is optional. Silo never runs models itself; it only connects to them.

- `models` is the operator **allowlist of text models**. Only these ids appear in the chat model picker, and the bot's `switch_model` tool can only choose from it.
- `model` is the default model (used when a chat has no override). It should be one of `models`.
- `model_title` names new chats; leave it empty to use `model`.

### Decision models

A model is one of two kinds. A **text model** writes: prose and tool calls, in a chat. A **decision model** (TypeSafe's Jev, Cloudflare's Clef, and the others of that family) writes nothing: it is shown a state and asked typed questions — `noul` (yes or no), `choice` (one of the options you describe), `score` (a place on an ordered scale) — and answers each with calibrated probabilities, in well under a second and for a fraction of the price. Code branches on those numbers.

- `decision_models` is the **allowlist of decision models**, separate from `models`: an id is in one list or the other, and a decision model is never a chat model.
- `model_decision` is the default decision model. Unset, it is the first of `decision_models`.
- Two providers serve them. `openrouter` reaches every decision model on its Decisions API with the OpenRouter key you already have (`openrouter/typesafe/jev-1.13`, `openrouter/cloudflare/clef-flash`, …); `typesafe` is TypeSafe's own API (`typesafe/jev-latest`) with a TypeSafe key. Under Admin → Settings → Providers, **List models** shows a provider's text and decision models apart, and each is one press from its allowlist.

They are used in two places:

- **The Bot's `decide` tool** (rule `model.decide`, Allow) asks the default decision model, or another allowed one by name, and gets the typed answers back as JSON. The model can call it directly as a chat tool, for a judgment or two in the middle of a task, or from Python (`silo_runtime.decide(state, questions, model=None)`) to judge many items in a loop. It is offered only while a decision model is allowed.
- **Auto-approval.** `model_approval` may be a model of either kind. A decision model is asked two yes-or-no questions (does the policy clearly allow this action; does it clearly forbid it) and the gate acts only on a sure answer: it approves at 0.9 or more allowed with 0.1 or less forbidden, denies at 0.9 or more forbidden, and asks a human for everything between. A text model is asked for one word, as before.

Whichever kind judges, the gate is shown the same three things: the Bot's auto-approve policy, the action the Bot is asking for (arguments redacted as on the approval slip), and the human's latest message in the conversation the action came from. An automation has no such message and neither has a background process or other code running outside a conversation; the gate is then told the action comes from Automations, or that its origin is unknown, and nothing is put in the message's place.

Each provider's key lives under `providers.<id>`. Prompt caching is **on by default** for `openrouter`, `openai` and `anthropic` (`cache: false` turns it off); `anthropic` and `openrouter` also take `cache_ttl` (`5m` or `1h`) for Claude.

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
| `mail.enabled` | `true` | `SILO_MAIL__ENABLED` | Master switch for Bot mailboxes. Off: the SMTP listener, the Bot's mail tools and the Mail page's address are gone (received mail is kept) |
| `mail.domain` | (none) | `SILO_MAIL__DOMAIN` | What follows the `@` of every Bot's address (`<name>@<domain>`). A bare domain; its MX record must point here. Unset, there is no mail (see Mail) |
| `mail.addr` | `:2525` | `SILO_MAIL__ADDR` | Where the SMTP listener binds, `host:port` or `:port`. The world delivers to port 25: publish or forward 25 to it |
| `mail.max_size_mb` | `10` | `SILO_MAIL__MAX_SIZE_MB` | Largest message accepted, attachments included (1 to 50). Bigger ones are refused with 552 |
| `mail.tls_cert` / `mail.tls_key` | self-signed | `SILO_MAIL__TLS_CERT` / `SILO_MAIL__TLS_KEY` | PEM files for STARTTLS. Unset, a self-signed certificate is made at start |
| `model` | `openrouter/openai/gpt-5.6-luna` | `SILO_MODEL` | Default chat model (`provider/model`) |
| `model_title` | (none) | `SILO_MODEL_TITLE` | Chat title model. Empty = `model` |
| `model_approval` | (none) | `SILO_MODEL_APPROVAL` | Auto-approval model: judges actions whose rule is Auto against the Bot's policy. A text model (in `models`) or a decision model (in `decision_models`). Empty = the title model |
| `model_filter` | (none) | `SILO_MODEL_FILTER` | Output filter model: judges tool results for actions whose output rule is Check, against the Bot's output policy. A text model (in `models`) or a decision model (in `decision_models`). Empty = the auto-approval model |
| `model_decision` | (none) | `SILO_MODEL_DECISION` | Default decision model, for the Bot's `decide` tool (must be in `decision_models`). Empty = the first of `decision_models` |
| `model_subagent` | (none) | `SILO_MODEL_SUBAGENT` | Default model for subagents a lead starts with `spawn_agent` (must be in `models`). Empty = the lead's own model. The lead may still name another allowed model |
| `model_memory` | (none) | `SILO_MODEL_MEMORY` | Memory save model: the memory collector reads chats with it (must be in `models`). Empty = the title model. A cheap model is enough |
| `embedding_model` | `openrouter/openai/text-embedding-3-small` | `SILO_EMBEDDING_MODEL` | Embeds long-term memories (`remember`/`recall`) and indexed document chunks (`search_docs`). Must be an OpenAI-compatible provider returning 1536-wide vectors (`dimensions` is sent); changing the width means `make db-reset`. Admin → Settings → Models; a provider that cannot embed (Anthropic) is rejected |
| `transcribe_model` | `openrouter/openai/whisper-1` | `SILO_TRANSCRIBE_MODEL` | Speech-to-text for composer dictation (the mic, `Transcribe` RPC) and the Bot's `transcribe` tool / `silo_runtime.transcribe`. Any OpenAI-compatible `/audio/transcriptions` model: OpenRouter (`openai/whisper-1`, `openai/whisper-large-v3`, …), OpenAI (`whisper-1`, `gpt-4o-transcribe`), or `local/<model>` for LocalAI / Speaches / vLLM / whisper.cpp (`--inference-path /v1/audio/transcriptions`). 25 MB per recording. `off` disables voice (the mic hides and the tool is not offered). A provider that cannot transcribe (Anthropic) is rejected |
| `memory.in_prompt` | `true` | `SILO_MEMORY__IN_PROMPT` | Put long-term memory in the system prompt: every memory while they total under 6,000 characters, then a summary a background sweep writes with `model_memory` and rewrites (at most every 30 minutes) when the memories changed. Nothing is embedded when a message arrives; the Bot searches with `recall`. Off keeps long-term memory out of the prompt |
| `tools.expose_completion` | (none) | `SILO_TOOLS__EXPOSE_COMPLETION` | Built-in tools to also offer the model as chat tools (YAML list, or comma-separated in the environment; `all` for every one). Most built-ins ship reachable from Python only (`silo_runtime.web_search`, `feed`, the taskboard, …) to keep each request small; name the ones a model handles badly that way |
| `memory.collect` | `true` | `SILO_MEMORY__COLLECT` | Sweep web and channel chats that have been quiet for 10 minutes with new messages, and save the facts and lessons the Bot missed (one `model_memory` call per chat, each message read once). The composer's memory button works either way |
| `knowledge.enabled` | `true` | `SILO_KNOWLEDGE__ENABLED` | The folder index behind `search_docs`: the background sweep, adding folders on a Bot's Knowledge page, and the tool. Off hides the tool and stops syncing; what is already indexed stays in Postgres |
| `knowledge.sync_interval` | `15m` | `SILO_KNOWLEDGE__SYNC_INTERVAL` | How often an indexed folder is re-checked (Go duration, minimum `1m`). Drive folders are checked four times less often; a folder written to (by the Bot or the Files tab) is re-read about 15 s after it goes quiet. Embeds with `embedding_model` |
| `knowledge.ocr` | `true` | `SILO_KNOWLEDGE__OCR` | Read scanned PDF pages (only pages with next to no text) and image files (`png`/`jpg`/`tif`/`bmp`/`gif`/`webp`, at least 8 KB) with tesseract inside the Bot's box; image text must look like prose or it is dropped. At most 100 pages and 4 minutes per file. English and Polish ship in the bot image; every installed language is used. Off, scans are listed as not indexed (and re-read when you turn it on). A folder of photos makes syncing slow |
| `changes.enabled` | `true` | `SILO_CHANGES__ENABLED` | Workspace change tracking: each Bot's machine snapshots `/workspace` before and after every run and the Changes pane shows the differences as diffs. The history is a git store on the Bot's own disk (`data/bots/<id>/history`), never in Postgres. Drives, `tmp/`, `bot/` and dependency folders are not read; a Bot keeps at most 300 snapshots and 1 GB. The same switch covers the drive journal: each drive's sidecar notes what is written, deleted or renamed through the mount (names and sizes only, 2000 lines per Bot, in Postgres). Off stops new snapshots and journal lines and empties the pane; what was recorded stays |
| `changes.max_file_mb` | `8` | `SILO_CHANGES__MAX_FILE_MB` | A file over this is recorded by size and modification time only, never by content (1–100) |
| `changes.keep_days` | `30` | `SILO_CHANGES__KEEP_DAYS` | Snapshots older than this are forgotten (1–365) |
| `filter.enabled` | `true` | `SILO_FILTER__ENABLED` | Output rules: before a Bot reads what a tool brought back from outside (a web search, mail, a connector's answer), the filter model judges it and it passes, passes marked as untrusted, or is withheld. One model call per checked result. Off, every result passes whatever a Bot's rules say; the rules are kept |
| `filter.max_chars` | `48000` | `SILO_FILTER__MAX_CHARS` | The longest result the filter reads, in characters (1000–400000). A longer one is passed on marked as unchecked |
| `rules.approval_policy` | (none) | `SILO_RULES__APPROVAL_POLICY` | The operator's auto-approval policy, read **before** a Bot's own (Admin → Settings → Rules). It says what may run without a human; a Bot's own policy is appended after it. The operator's rule rows (a floor owners may tighten but not loosen) live in the database and are edited on the same page |
| `rules.output_policy` | (none) | `SILO_RULES__OUTPUT_POLICY` | The operator's output policy, read **before** a Bot's own (or the built-in one) by the filter model, on the same page |
| `audit.detail_days` | `7` | `SILO_AUDIT__DETAIL_DAYS` | How long an audit line is kept: a tool call and its decision, a result the output filter did not pass, a block, a flag or an alert. A background sweep drops what is older |
| `audit.traffic_days` | `30` | `SILO_AUDIT__TRAFFIC_DAYS` | How long the network filter's per-minute traffic summaries (host, requests, bytes) are kept. They are sums only: what was asked for lives in the detail lines |
| `context.window` | `128000` | `SILO_CONTEXT__WINDOW` | Fallback context window in tokens, for models whose provider does not report one (OpenRouter's `/models` `context_length` is used when it does) |
| `context.compact_at` | `0.8` | `SILO_CONTEXT__COMPACT_AT` | Fraction of the window at which a run summarizes its history into one turn before the next model call (0.1–0.98) |
| `runs.max_duration` | `120m` | `SILO_RUNS__MAX_DURATION` | Cap on one agent run (a Go duration such as `120m`/`2h`, or bare minutes). `-1` means unlimited. Subagent runs and a lead sleeping while its subagents work count toward it |
| `context.windows` | (none) | — | Per-model window overrides (YAML list of `{model, window}`; a list because model ids contain dots). Wins over the provider's report |
| `thinking.levels` | (none) | — | Per-model thinking-level overrides for the composer's Thinking picker (YAML list of `{model, levels}`; levels from `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`; `[]` hides the picker). Wins over the provider's report: OpenRouter `/models` `supported_parameters`, the Anthropic Models API `capabilities`, OpenAI's reasoning families (`o*`, `gpt-5*` → low/medium/high). `local` models have no levels unless listed here (sent as `reasoning_effort`) |
| `models` | (none) | — | Allowlist of text models (YAML list). Admin → Settings → Models edits it; under Providers, **List models** asks a provider what its saved key can call and adds a model with one press |
| `decision_models` | (none) | — | Allowlist of decision models (YAML list), edited in the same two places. Only `openrouter` and `typesafe` ids are accepted, and an id cannot be in `models` too. Empty: no `decide` tool |
| `providers.<id>.api_key` | (none) | `SILO_PROVIDERS__<ID>__API_KEY` | Provider key. Required when that provider is used (optional for `local`) |
| `providers.<id>.base_url` | (provider default) | `SILO_PROVIDERS__<ID>__BASE_URL` | Override the API base URL |
| `providers.<id>.cache` | `true` | `SILO_PROVIDERS__<ID>__CACHE` | Prompt caching. `anthropic` adds `cache_control` breakpoints; `openai` sends `prompt_cache_key`; `openrouter` sends `prompt_cache_key` (it keeps a conversation on the upstream that holds its cache) and marks Claude prompts with `cache_control`, without which OpenRouter does not cache Claude at all. Other OpenRouter upstreams (OpenAI, DeepSeek, Grok, Gemini 2.5) cache a stable prefix on their own |
| `providers.<id>.cache_ttl` | `5m` | `SILO_PROVIDERS__<ID>__CACHE_TTL` | Claude cache lifetime (`5m`/`1h`), on `anthropic` and `openrouter`. `1h` costs more to write and survives the pause between a human's messages |
| `providers.anthropic.max_tokens` | `8192` | `SILO_PROVIDERS__ANTHROPIC__MAX_TOKENS` | Anthropic per-response output cap |
| `providers.openrouter.ignore` | `DeepInfra` | `SILO_PROVIDERS__OPENROUTER__IGNORE` | Comma list of upstream hosts OpenRouter must not route to (`provider.ignore`). DeepInfra sends a tool call's arguments in one chunk at the end, so Python/terminal/write input cannot stream into the thread. `none` routes anywhere |
| `bootstrap.email` | (none) | `SILO_BOOTSTRAP__EMAIL` | The first admin. Used at start only while no admin can sign in (see Sign-in) |
| `bootstrap.password` | (none) | `SILO_BOOTSTRAP__PASSWORD` | Their password, when the account has to be created |
| `outbound.block_private` | `false` | `SILO_OUTBOUND__BLOCK_PRIVATE` | Refuse loopback, private and other internal addresses (and names resolving to one) for connections Silo makes on a user's word (see Outbound connections) |
| `outbound.allow` | (none) | `SILO_OUTBOUND__ALLOW` | Internal addresses that stay reachable: comma-separated IPs or CIDRs, each optionally with one port (`127.0.0.1:9223, 10.0.5.0/24`) |
| `auth.password` | `true` | `SILO_AUTH__PASSWORD` | Password sign-in. `false` leaves only OIDC, and only counts while OIDC is configured (see Sign-in) |
| `auth.trusted_proxies` | (none) | `SILO_AUTH__TRUSTED_PROXIES` | Comma-separated IPs/CIDRs of the reverse proxies in front of the control plane. `X-Forwarded-For` is believed only from them |
| `auth.host_cookie` | `false` | `SILO_AUTH__HOST_COOKIE` | Name the session cookie `__Host-silo_session`, which only the exact host can set. Counts only while `public_url` is https (see Sign-in) |
| `oidc.issuer` | (none) | `SILO_OIDC__ISSUER` | The OIDC provider's issuer URL. OIDC sign-in is on when this and `oidc.client_id` are set |
| `oidc.client_id` | (none) | `SILO_OIDC__CLIENT_ID` | OAuth client ID registered at the provider |
| `oidc.client_secret` | (none) | `SILO_OIDC__CLIENT_SECRET` | Its secret |
| `oidc.scopes` | `openid email profile` | `SILO_OIDC__SCOPES` | Space-separated scopes. `openid` is always requested |
| `oidc.label` | `Single sign-on` | `SILO_OIDC__LABEL` | Text of the button on the sign-in page |
| `oidc.auto_create` | `false` | `SILO_OIDC__AUTO_CREATE` | Make an account the first time someone signs in |
| `oidc.allowed_domains` | (none) | `SILO_OIDC__ALLOWED_DOMAINS` | Comma-separated email domains `auto_create` is limited to |
| `oidc.groups_claim` | `groups` | `SILO_OIDC__GROUPS_CLAIM` | ID token claim that lists a person's groups |
| `oidc.admin_group` | (none) | `SILO_OIDC__ADMIN_GROUP` | Members of this group are admins, checked at every OIDC sign-in |
| `search.engine` | `builtin` | `SILO_SEARCH__ENGINE` | Web search engine. `builtin` is the **Built-in Scraper**: it needs no key, asks DuckDuckGo, Mojeek, Brave, Startpage, Wikipedia and Marginalia at once, and merges what they return. It rate-limits itself per site (about ten requests a minute each, a search asking one or two sites), because every search leaves from the Control Plane's one address and a site asked too often blocks it; a search queues a few seconds rather than fail when that budget is spent. An engine's own keys live under `search.<engine_id>`. The old `duckduckgo_scraper` reads as `builtin` |
| `search.builtin.sources` | (all) | `SILO_SEARCH__BUILTIN__SOURCES` | Comma-separated sources the Built-in Scraper asks: `duckduckgo`, `mojeek`, `brave`, `startpage`, `wikipedia`, `marginalia`. Empty asks all of them. Marginalia's public API key is a quota shared with every other user of it and its data is CC-BY-NC-SA; leave it out if that licence does not suit you |
| `connector_vars.<name>` | (none) | `SILO_CONNECTOR_VARS__<NAME>` | Connector variable. Referenced as `${NAME}` in connector settings. **Plain text, not a secret** |
| `autoenable_connectors` | (none) | `SILO_AUTOENABLE_CONNECTORS` | List of library connector identifiers every new Bot gets. The env form is comma-separated (`lightpanda,fal_ai`) |

## Sign-in

Accounts live in Postgres; **Admin → Users** manages them. `bootstrap.*` is read at start and only matters while no admin can sign in (a new install, or the only admin was disabled): the user with `bootstrap.email` becomes an admin, and is created with `bootstrap.password` if there is none. Once an admin exists it does nothing, so changing the admin's email or password in Silo is not undone by a restart.

- **New people get an invite link.** An admin makes one for an email address (optionally as an admin); the person opens it and chooses their own password. A link works once and lasts 7 days. Silo sends no mail: the admin passes the link on. A password is at least 8 characters.
- **Email** is what a person signs in with, and they can change it on their Account page. Nothing is sent to the new address and nothing confirms it. Because of that, OIDC never matches an account by an address its user typed in themselves (below); an address an admin sets on the user's page in Admin → Users is matched again.
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
2. Otherwise the user whose email matches, **only if the provider says the email is verified** (`email_verified: true`), that user is not linked to another identity, and the address is one an admin or an invite gave them, not one they changed to themselves. They are linked from then on, and their password keeps working.
3. Otherwise, with `auto_create` on (and the domain allowed), a new user with no password. They can set one on their Account page.
4. Otherwise nobody: the sign-in page says to ask an admin for an invite.

A provider that does not send `email_verified` (some Entra setups) can therefore only sign in people who are already linked; the email is read from the userinfo endpoint when the ID token has none. With `admin_group` set, the provider decides who is an admin at every sign-in, except that it never demotes the last one. With it unset, roles are only what Admin → Users says.

`auth.password: false` hides the password form and refuses password sign-in. It is ignored while OIDC is not configured, so removing the OIDC settings brings passwords back.

`auth.host_cookie: true` is for a deployment where tunnels (or anything else that serves content you do not write) live under the same registrable domain as Silo, such as `silo.example.com` with `*.tunnels.example.com`. A page on any host under `example.com` may set a cookie for `example.com`, and the browser sends it to Silo too; a tunnelled app could plant a `silo_session` of its own choosing and sign a visitor into another account without their noticing. A cookie whose name starts with `__Host-` is only accepted from the host itself, so with the setting on the session cookie is `__Host-silo_session` (and the OIDC round trip's `__Host-silo_oidc`), and the plain names are never read. Browsers only take such a cookie over HTTPS, so the setting is ignored unless `public_url` starts with `https://`. Turning it on or off signs everyone out once. Tunnels on a different registrable domain (`*.example-tunnels.com`) need none of this.

## Outbound connections

Some connections are made by the control plane itself, to an address a user typed: an HTTP connector's URL and the OAuth endpoints that server names, an Email connector's IMAP and SMTP servers, a Calendar connector's CalDAV address, the URL a skill is installed from, and a drive provider's sign-in. The control plane sits on your network, so by default a user can point one of these at `127.0.0.1`, at the database next to Silo, at a cloud metadata address (`169.254.169.254`) or at any machine on the LAN, and read what answers through the connector.

```yaml
outbound:
  block_private: true
  allow: 127.0.0.1:9223, 10.0.5.0/24   # what stays reachable anyway
```

`block_private` refuses loopback, private (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`), link-local, carrier-grade NAT (`100.64/10`, which is also Tailscale), multicast and reserved addresses. The check is made on the address a connection is about to be opened to, after DNS: a name that resolves inside is refused like the address itself, and so is a redirect that leads inside. `allow` takes IPs and CIDRs, each optionally with one port; names are not taken.

What it leaves alone:

- **A library connector you pointed at an internal address.** A Bot's copy connects unrestricted for as long as it still points where the library preset points (the Lightpanda preset on `localhost:9223` keeps working). Once its owner changes the address, it is their word again.
- **What you configured yourself:** model providers (a `local` provider is on your network by design), search, OIDC.
- **What a Bot's own machine, a STDIO connector's sidecar or a drive's sidecar can reach.** Those are containers; limit them with your container engine's network or a firewall.
- **Requests through `HTTP_PROXY`/`HTTPS_PROXY`.** With a proxy set in the control plane's environment, the proxy does the resolving and the connecting: filter there.

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

## Mail

Every Bot has its own **receive-only** email address, `<name>@<mail.domain>` (the name is generated, like `quiet-amber-heron`). The control plane runs a small SMTP listener of its own; what arrives for a Bot is filed in its inbox, where the Bot reads it with `list_mail` / `read_mail` and the owner on the Bot's **Mail** page. Nothing is ever sent: there is no relay, no outbound queue and no bounce, so there is no sender reputation, SPF/DKIM signing or reverse DNS to look after. It is separate from the **Email connector**, which signs in to somebody's existing IMAP/SMTP account.

```yaml
mail:
  domain: bots.example.com   # required: without it there is no mail
  # addr: ":2525"            # where the listener binds
  # enabled: true
  # max_size_mb: 10
  # tls_cert: /etc/silo/mail.crt   # optional; self-signed otherwise
  # tls_key: /etc/silo/mail.key
```

- **DNS**: one MX record, `bots.example.com. MX 10 silo.example.com.`, where the target is a name whose A/AAAA record is this machine. Nothing else is needed to receive. A subdomain used only for Bots keeps it apart from your own mail.
- **Port 25**: other mail servers deliver to port 25 and nowhere else. The listener binds `:2525` so the control plane needs no privileges; publish or forward 25 to it (`-p 25:2525`, a firewall redirect), or set `addr: ":25"` if it may bind that. An HTTP reverse proxy, Cloudflare's proxy or a tunnel does **not** carry SMTP: the MX target must reach this machine directly. Many home connections block inbound 25; a VPS normally does not.
- **TLS**: STARTTLS is always offered. Mail servers encrypt opportunistically and do not check who signed the certificate, so the self-signed one is enough; set `tls_cert` and `tls_key` if you publish MTA-STS or DANE.
- **Applies live**: the listener follows these settings within a few seconds (Admin → Settings → Mail), no restart. If the port cannot be bound, admins see why on a Bot's Mail page and it is tried again.
- **Changing `domain`** moves every address at once (a mailbox stores only its name); the old domain is refused from then on. The owner can also give one Bot a **new address** on its Mail page.
- **What is refused, inside the SMTP session** (so no bounce is ever written): a recipient that is not a Bot's address (550), a message over `max_size_mb` (552), more than 10 recipients, and a client that keeps naming addresses that do not exist or sends more than 60 messages a minute (451). A message that could not be stored is answered 451 and the sender retries, so a restart loses nothing.
- **Sender check**: each message is marked *verified* when the From address's domain vouched for it: it signed the message (DKIM), or it lists the sending server and is the envelope sender too (SPF), aligned on the organisational domain as DMARC does. Unverified mail is still delivered and shown as such, to the owner and to the Bot.
- **Waking the Bot** is off by default: mail waits until the Bot is asked to look. On a Bot's Mail page the owner can list senders (addresses or `@domains`) whose **verified** mail opens a new chat with the message, at most 10 an hour. A forged From address wakes nobody.
- A mailbox keeps its newest 200 messages. `name+anything@domain` reaches `name`'s mailbox.

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

# Optional: decision models, for the Bot's decide tool and auto-approval.
decision_models:
  - openrouter/typesafe/jev-1.13
model_decision: openrouter/typesafe/jev-1.13
model_approval: openrouter/typesafe/jev-1.13

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
  engine: builtin

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
