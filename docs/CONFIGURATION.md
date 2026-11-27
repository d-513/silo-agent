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

### Fast mode

A text model id ending in `:fast` is that model in its provider's **fast mode**: faster output at a premium price, chosen per model by the operator. It is an allowlist entry of its own (`claude/opus` and `claude/opus:fast` side by side), so the Bot default, a chat, `model_voice` and the other roles can each pick it, and the cost is decided where it is listed.

- Providers: `anthropic` (`speed: "fast"`; GA on Claude Opus 5.5 and Fable/Mythos 5.2), `openai` (`service_tier: "fast"`, GPT-5.6 and later), `openrouter` (`service_tier: "fast"`, its priority tier, Anthropic's fast mode included) and `claude` (Claude Code's own fast mode; models whose list marks `supportsFastMode`, e.g. `opus`; on a subscription it may be billed as extra usage, and an account with extra usage off is refused). Any other provider refuses `:fast`, as does a non-text list.
- Under Providers, **List models** shows a fast row under each model the provider says has fast mode (Claude Code reports it; for the others type the id on the Text page).
- A fast request the provider refuses or rate-limits **fails the turn**; Silo never answers it at standard speed. (OpenAI and OpenRouter may themselves serve a fast request at standard speed and price when traffic ramps.)
- `context.windows` and `thinking.levels` entries for the base model cover its `:fast` id unless it has an entry of its own.

### Decision models

A model is one of two kinds. A **text model** writes: prose and tool calls, in a chat. A **decision model** (TypeSafe's Jev, Cloudflare's Clef, and the others of that family) writes nothing: it is shown a state and asked typed questions — `noul` (yes or no), `choice` (one of the options you describe), `score` (a place on an ordered scale) — and answers each with calibrated probabilities, in well under a second and for a fraction of the price. Code branches on those numbers.

- `decision_models` is the **allowlist of decision models**, separate from `models`: an id is in one list or the other, and a decision model is never a chat model.
- `model_decision` is the default decision model. Unset, it is the first of `decision_models`.
- Two providers serve them. `openrouter` reaches every decision model on its Decisions API with the OpenRouter key you already have (`openrouter/typesafe/jev-1.13`, `openrouter/cloudflare/clef-flash`, …); `typesafe` is TypeSafe's own API (`typesafe/jev-latest`) with a TypeSafe key. Under Admin → Settings → Models → Providers, **List models** shows a provider's text and decision models apart, and each is one press from its allowlist.

They are used in two places:

- **The Bot's `decide` tool** (rule `model.decide`, Allow) asks the default decision model, or another allowed one by name, and gets the typed answers back as JSON. The Bot calls it from Python (`silo_runtime.decide(state, questions, model=None)`) to judge many items in a loop; name it in `tools.expose_completion` to offer it to the model as a chat tool as well. It is offered only while a decision model is allowed.
- **Auto-approval.** `model_approval` may be a model of either kind. A decision model is asked two yes-or-no questions (does the policy clearly allow this action; does it clearly forbid it) and the gate acts only on a sure answer: it approves at 0.9 or more allowed with 0.1 or less forbidden, denies at 0.9 or more forbidden, and asks a human for everything between. A text model is asked for one word, as before.

Whichever kind judges, the gate is shown the same three things: the Bot's auto-approve policy, the action the Bot is asking for (arguments redacted as on the approval slip), and the human's latest message in the conversation the action came from. An automation has no such message and neither has a background process or other code running outside a conversation; the gate is then told the action comes from Automations, or that its origin is unknown, and nothing is put in the message's place.

### Claude Agent (a Claude subscription)

The `claude` provider runs Claude on a **Claude subscription** instead of an API key, by running the official Claude Code program (`claude`) on the server, the way Anthropic's Agent SDK does. Model ids are `claude/<name>`, where the name is one Claude Code lists: an alias that follows the newest release (`claude/opus`, `claude/sonnet`, `claude/haiku`, `claude/fable`) or a pinned id (`claude/claude-opus-5`). **List models** shows what the signed-in account may use, and the Thinking picker offers the effort levels Claude Code lists for each.

> **For your own use only.** Anthropic does not allow a claude.ai sign-in or its rate limits to be offered in a product for other people. Use this provider on an instance only you use, for work you start yourself: not with several people on it, and not for Bots that act unattended (automations, a Heartbeat, channels that answer by themselves). For those, use the `anthropic` provider with an API key. Claude Code also tells the model the signed-in account's email address and the server's operating system and working directory, so a Bot on this provider can read them.

- **Install** it under Admin → Settings → Models → Providers → Claude Agent: **Install**. Silo does not include Claude Code, in its binaries, its images or SiloOS: it is Anthropic's program under Anthropic's own terms, not open source and not Silo's to pass on. The button downloads the newest release from Anthropic's own package on npm (`@anthropic-ai/claude-agent-sdk-<os>-<arch>`, about 190 MB), checks it against the checksum npm publishes, runs its `--version`, and keeps it in `<providers.claude.dir>/program/claude`; **Update** does the same again. The control plane's image and SiloOS need nothing more. A program of your own is used instead when `providers.claude.binary` names one (then there is no button), and with neither, `claude` on the server's `PATH` (`make claude-bin` still fetches one into `build/claude` for development).
- **Sign in** under Admin → Settings → Models → Providers → Claude Agent: **Sign in** runs `claude auth login` (the sign-in is kept in `providers.claude.dir` and renewed by Claude Code), **Create a long-lived token** runs `claude setup-token` and saves the token as `providers.claude.oauth_token`. Either way you open Anthropic's page, sign in there, and paste the code it shows. Silo never uses a sign-in from your own home directory.
- **What Claude Code may do:** nothing. Its built-in tools are off, it loads no settings, plugins, skills or MCP servers, and it is sent Silo's system prompt in place of its own. The model is offered only the Bot's tools, and each call is run by Silo under the Bot's rules, approvals and output filter, exactly as with any other provider.
- **Caching** is Claude Code's own (a one-hour cache under a subscription). Silo keeps one Claude Code session per conversation, so a turn sends only what is new; a history Claude Code does not hold (after a compaction, an edit, or turns written by another model) starts a new session that is told the history as text.

Each provider's key lives under `providers.<id>`. Prompt caching is **on by default** for `openrouter`, `openai` and `anthropic` (`cache: false` turns it off); `anthropic` and `openrouter` also take `cache_ttl` (`5m` or `1h`) for Claude.

### Model kinds

Every model is of one **kind**, and each kind has its own allowlist: `models` (text), `decision_models`, `transcription_models` (speech-to-text), `speech_models` (text-to-speech), `embedding_models` and `image_models`. An id is on one list only, and its provider must serve the kind: `openrouter`, `openai` and `local` serve text, speech-to-text, speech, embeddings and images (`openrouter` decision models too), `anthropic` only text, `typesafe` only decisions. **List models** under Admin → Settings → Models → Providers shows a tab per kind the provider lists. The model a setting names (`transcribe_model`, `voice.speech_model`, `embedding_model`, `model_image`) counts as allowed for its kind even when its list does not name it, so a `silo.yaml` from before the lists keeps working.

### Image models

`model_image` is the image model a Bot's `generate_image` tool (`silo_runtime.generate_image`) uses; `image_models` are the others it may name. With no `model_image` the tool does not exist. A picture is written into the Bot's workspace (the path it names, else `images/…`) and shown to nobody until the Bot presents it. Every picture is billed by its provider, and the rule **Generate an image** (`model.image`, Allow by default) can be set to Ask or Deny per Bot.

```yaml
image_models: [openrouter/google/gemini-3-pro-image, bfl/flux-3-image]
model_image: openrouter/google/gemini-3-pro-image
```

`openrouter` (its Images API), `openai` (`gpt-image-2`, …) and `local` (any server with OpenAI's `/images/generations`) serve images beside their other kinds. These providers have image models only (each takes `api_key` and an optional `base_url`):

| Provider | Model ids | Notes |
| --- | --- | --- |
| `fal` | the endpoint: `fal/fal-ai/flux-2-pro`, `fal/fal-ai/nano-banana-pro/edit` | Queued requests. Endpoints differ in what they take; an `…/edit` endpoint is the one that takes input images. |
| `replicate` | `replicate/<owner>/<name>` | The model's input schema is read to learn what it calls the shape and its input images; an input over 256 KB is uploaded to Replicate first. |
| `bfl` | `bfl/flux-3-image`, `bfl/flux-2-pro`, `bfl/flux-kontext-pro` | Black Forest Labs. One picture per request; several are made side by side. |
| `stability` | `stability/ultra`, `stability/core`, `stability/sd3.5-large` | One input image at most (`core` none); 4:3 and 3:4 become 5:4 and 4:5. |
| `google` | `google/gemini-3-pro-image`, `google/gemini-nano-banana-2.1` | The Gemini API's image models through `generateContent`; up to 14 input images. |
| `xai` | `xai/grok-imagine-image-2.0` | One input image for an edit. |

A request is a prompt, optional input images from the workspace, an aspect (`1:1 3:2 2:3 4:3 3:4 16:9 9:16 21:9`) and a count of 1 to 4; a provider with no such shape takes the nearest it has, and one that makes a single picture per request is asked once per picture. One request may take 5 minutes.

### Speech providers

Besides OpenRouter, OpenAI and `local`, a provider each for the fastest pieces of a call (Admin → Settings → Models → Providers; each takes `api_key` and an optional `base_url`):

| Provider | Kinds | What for |
| --- | --- | --- |
| `groq` | text, transcription | A talker (`model_voice`) with a very short time to the first word (`openai/gpt-oss-120b`, `llama-3.3-70b-versatile`), and Whisper. |
| `cerebras` | text | A talker (`gpt-oss-120b`, `qwen-3.8-27b`). |
| `deepgram` | transcription, speech | Live Nova-3 and Flux; Aura voices (one voice per model id). |
| `elevenlabs` | transcription, speech | Flash voices live with word timings; Scribe for files, Scribe Realtime live. |
| `cartesia` | transcription, speech | Sonic voices live with word timings; Ink live. |
| `inworld` | speech | TTS 2 voices live with word timings. The key is the Base64 one the Inworld Portal shows. |
| `soniox` | transcription | Live, 60+ languages with code-switching; judges the end of a turn. Files go through a live session. |
| `assemblyai` | transcription | Live Universal Streaming that judges the end of a turn; files in one request (Sync API). |

`providers.local.realtime` says a self-hosted server transcribes live over the Realtime API: `openai` for LocalAI and servers that follow OpenAI's current API, `speaches` for Speaches; empty (the default) means one request per turn.

### Voice

Read aloud (a speaker button on every reply) and **calls** (Talk, in the composer: a spoken conversation with a Bot, held in a chat). Admin → Settings → Voice & calls holds the switch, the limits and the call network; the models (what hears, what speaks, the talker) are jobs under Admin → Settings → Models.

| Key | Default | What it does |
| --- | --- | --- |
| `voice.enabled` | `true` | Off hides Read aloud and Talk everywhere. |
| `transcribe_model` | (none) | Dictation, the transcribe tool, and calls unless `voice.transcribe_model` names another. |
| `voice.transcribe_model` | empty | What hears a call, when it should differ from dictation. A **live** model (marked Live in Admin) hears you as you speak and has the words ready when you stop; any other is asked once per turn. Live choices: Deepgram (`nova-3`, `flux-general-en`/`-multi`, which also judge when you have finished), Soniox (`stt-rt-v5`, strong Polish), AssemblyAI (`universal-3-6-pro`), ElevenLabs (`scribe_v2_realtime`), Cartesia (`ink-whisper`), OpenAI (`gpt-live-transcribe`, `gpt-4o-transcribe`), or a `local` server with `realtime` set. |
| `voice.speech_model` | (none) | The default voice; a Bot may pick another from `speech_models`, and a voice and speed of it (Bot → Settings → Voice). Through OpenRouter (Kokoro, Fish, Voxtral, Gemini TTS, ElevenLabs) every sentence is one request. A **live** voice stays connected for the whole call and says when each word is spoken: ElevenLabs direct (`eleven_flash_v2_5`, Polish included), Cartesia (`sonic-3.6`), Inworld (`inworld-tts-2-flash`), Deepgram Aura (`aura-2-thalia-en`; the voice is the model, no Polish). Kokoro has no Polish voice. |
| `model_voice` | empty | The **talker**: a fast text model that hears each turn first, answers small talk itself and hands real work to the Bot, so answers start in under a second. Empty, the Bot answers every turn itself (smarter, slower). Pick it for time to first token and for not guessing; it must be on `models`. |
| `voice.max_minutes` | `30` | A call ends after this long. |
| `voice.idle_minutes` | `3` | A call nobody speaks in ends after this long. |
| `voice.approve_by_voice` | `true` | Reserved for approving by voice. Today a call says an approval is waiting and it is answered on screen. |
| `voice.opus_library` | empty | Where libopus is when the loader cannot find it (`libopus.so.0`). Calls need it, and a control plane built with cgo; Read aloud needs neither. |
| `voice.webrtc.udp_port` | `3479` | The one UDP port every call's audio uses. **Publish it** beside the HTTP port; a reverse proxy cannot carry it. Restart required. |
| `voice.webrtc.tcp_port` | `3479` | The route for networks that block UDP. Restart required. |
| `voice.webrtc.public_ips` | empty | The control plane's public address when it sits behind NAT (comma-separated). Restart required. |
| `voice.webrtc.ice_servers` | empty | `stun:`/`turn:` URLs offered to the browser, for networks that need a relay. |

Audio is never kept: what is said on a call is written into its chat as text, and the timings of each turn (end of turn, speech-to-text, first sound back) are kept in `voice_turns`.

## Keys

| Key | Default | Env | What |
|---|---|---|---|
| `http_addr` | `:8080` | `SILO_HTTP_ADDR` | Control Plane listen address |
| `data_dir` | `./data` | `SILO_DATA_DIR` | Per-bot volumes + skill bodies |
| `database_url` | `postgres://silo:silo@localhost:5433/silo?sslmode=disable` | `SILO_DATABASE_URL` | Postgres with pgvector (`make db-up` for dev). Restart to apply |
| `docker_host` | `$DOCKER_HOST` | `SILO_DOCKER_HOST` | Docker/Podman socket. Empty falls back to the `DOCKER_HOST` env |
| `public_url` | (request origin) | `SILO_PUBLIC_URL` | Browser origin for OAuth redirects (`{public_url}/oauth/callback`). Dev: `http://127.0.0.1:5173` |
| `cp_url` | `http://host.containers.internal:8080` | `SILO_CP_URL` | URL Bot containers and STDIO sidecars use to dial the CP |
| `worker_addr` | `:8081` | `SILO_WORKER_ADDR` | A second listener that serves only what a Bot's machine and a STDIO sidecar call (the worker and bridge APIs, each opened by its own token). The network filter's gateway forwards them there, at `cp_url`'s host, so nothing behind the filter can open the console, the people's API, an artifact or the sign-in through the control plane's link. It must be reachable from the gateway like `cp_url` is: on the host it is, on `cp_network` it is, and a control plane in a container that publishes 8080 publishes 8081 too. Empty: no such listener, and the gateway forwards to `cp_url`'s own port, console and all. Restart required |
| `cp_network` | (none) | `SILO_CP_NETWORK` | The container engine's network the control plane itself is on, when it runs as a container there. The network filter's gateway and the drive sidecars (and, with the filter off, the Bots' machines and STDIO sidecars) join it and reach the control plane at `cp_url`, which then names its container (`http://silo:8080`), so its port needs no publishing. Empty: they go through the host (`host.containers.internal`). A change makes the gateway again at once and a drive sidecar at its next start. See Running the control plane in a container |
| `bot_image` | this build's (see Images) | `SILO_BOT_IMAGE` | A Bot's machine. Leave unset to follow the release |
| `bot_limits.memory_mb` | `0` (none) | `SILO_BOT_LIMITS__MEMORY_MB` | Memory cap of each Bot's machine in MB, Chromium and all it runs included (`0`, or at least 512). Taken when a machine is created; a stopped machine made under another limit is made again at its next start |
| `bot_limits.pids` | `4096` | `SILO_BOT_LIMITS__PIDS` | Processes and threads each Bot's machine may hold at once (`0`, or at least 256): a runaway build or a fork bomb stops there instead of starving the host. Taken like `memory_mb` |
| `mcp_stdio_image` | this build's | `SILO_MCP_STDIO_IMAGE` | Default image for STDIO MCP sidecars. A connector may override with `stdio_image` (admin) |
| `drives.image` | this build's | `SILO_DRIVES__IMAGE` | The rclone drive sidecar |
| `images.pull` | `missing` | `SILO_IMAGES__PULL` | When an image is fetched from its registry: `missing`, `always` (at start, every hour and before a create, at most every 10 minutes per image) or `never`. See Images |
| `drives.mount_root` | `<data_dir>/drives-mnt`; on macOS `/var/tmp/silo-drives` | `SILO_DRIVES__MOUNT_ROOT` | Where drive mounts live, as a path the container engine sees. It must be able to carry mount propagation, so on podman machine it is inside the VM, not the virtiofs `data/` share |
| `drives.cache_max_size` | `10G` | `SILO_DRIVES__CACHE_MAX_SIZE` | rclone VFS cache cap per drive, kept in `data/drives/<bot>/cache` |
| `content.host` | derived (see Content host & tunnels) | `SILO_CONTENT__HOST` | Domain suffix Bot-made content is served under, `host[:port]`: tunnels at `tunnel-<name>.<host>`, live app artifacts at `app-<name>.<host>`, and public shares at `share-<name>.<host>`. Lower-cased; no scheme, path or wildcard. Must differ from the `public_url` host |
| `content.scheme` | follows `public_url` | `SILO_CONTENT__SCHEME` | `http` or `https`, for the links tunnels, apps and shares are shown with. Set it when TLS ends at a proxy and `public_url` is an internal address |
| `caddy.admin` | (none) | `SILO_CADDY__ADMIN` | Caddy's admin API (`unix//path/to/admin.sock` or `http://host:port`). Set, the control plane configures that Caddy itself: HTTPS for `public_url`'s name and `content.host` (see Content host & tunnels). SiloOS sets it |
| `caddy.upstream` | `127.0.0.1:<http_addr port>` | `SILO_CADDY__UPSTREAM` | The control plane's address as that Caddy reaches it (`host:port`). Set it when Caddy is a container of its own (`silo:8080`), and put Caddy's address in `auth.trusted_proxies`: besides `X-Forwarded-For`, that is who may ask before a certificate on demand, once Caddy is no longer a peer on loopback. SiloOS sets both |
| `caddy.mode` | `auto` | `SILO_CADDY__MODE` | Where certificates come from: `auto` (Let's Encrypt for a public domain, Caddy's own authority for an address or a local name), `acme` (Let's Encrypt), `internal` (Caddy's own authority; browsers warn until its root is installed) or `off` (Caddy serves nothing) |
| `caddy.email` | (none) | `SILO_CADDY__EMAIL` | The Let's Encrypt account's address, for expiry notices |
| `caddy.dns_provider` | (none) | `SILO_CADDY__DNS_PROVIDER` | Answer Let's Encrypt through this DNS host instead of on port 80 (no inbound connection; a wildcard for the content host): `cloudflare`, `route53`, `azure`, `bunny`, `desec`, `digitalocean`, `duckdns`, `gandi`, `godaddy`, `hetzner`, `ionos`, `linode`, `namecheap`, `netcup`, `ovh`, `porkbun`, `vultr`. Needs `silo-caddy` (`images/caddy/`) |
| `caddy.dns.<provider>.<field>` | (none) | `SILO_CADDY__DNS__<PROVIDER>__<FIELD>` | The provider's credentials, by the names its libdns provider uses (`caddy.dns.cloudflare.api_token`); Admin → Settings → HTTPS lists each provider's fields (`internal/caddy/dns.go`) |
| `siloos.socket` | (none) | `SILO_SILOOS__SOCKET` | siloosd's unix socket, on a SiloOS machine only (SiloOS sets it). Set, Admin has a SiloOS page |
| `tunnels.enabled` | `true` | `SILO_TUNNELS__ENABLED` | Master switch for tunnels. Off: the proxy, the Bot's tunnel tools and the Tunnels tab's add form are gone (existing rows are kept) |
| `artifacts.max_mb` | `2048` | `SILO_ARTIFACTS__MAX_MB` | Per-Bot disk budget (in MB, 1–102400) for pinned artifact versions in `data/bots/<id>/artifacts`. Past this, the oldest versions are evicted |
| `mail.enabled` | `true` | `SILO_MAIL__ENABLED` | Master switch for Bot mailboxes. Off: the SMTP listener, the Bot's mail tools and the Mail page's address are gone (received mail is kept) |
| `mail.domain` | (none) | `SILO_MAIL__DOMAIN` | What follows the `@` of every Bot's address (`<name>@<domain>`). A bare domain; its MX record must point here. Unset, there is no mail (see Mail) |
| `mail.addr` | `:2525` | `SILO_MAIL__ADDR` | Where the SMTP listener binds, `host:port` or `:port`. The world delivers to port 25: publish or forward 25 to it |
| `mail.max_size_mb` | `10` | `SILO_MAIL__MAX_SIZE_MB` | Largest message accepted, attachments included (1 to 50). Bigger ones are refused with 552 |
| `mail.tls_cert` / `mail.tls_key` | self-signed | `SILO_MAIL__TLS_CERT` / `SILO_MAIL__TLS_KEY` | PEM files for STARTTLS. Unset, a self-signed certificate is made at start |
| `model` | (none) | `SILO_MODEL` | Default chat model (`provider/model`). **No model is built in, of any kind:** every allowlist and every model setting starts empty, so a new installation shows no provider's models as allowed until you allow them. Until a chat model is chosen a reply fails saying so; with no embedding model memories and Knowledge wait; with no transcription or speech model the mic, calls and Read aloud are hidden. (Before, `model`, `embedding_model`, `transcribe_model` and `voice.speech_model` defaulted to OpenRouter models: a silo.yaml that relied on that names them now) |
| `model_title` | (none) | `SILO_MODEL_TITLE` | Chat title model. Empty = `model` |
| `model_approval` | (none) | `SILO_MODEL_APPROVAL` | Auto-approval model: judges actions whose rule is Auto against the Bot's policy. A text model (in `models`) or a decision model (in `decision_models`). Empty = the title model |
| `model_filter` | (none) | `SILO_MODEL_FILTER` | Output filter model: judges tool results for actions whose output rule is Check, against the Bot's output policy. A text model (in `models`) or a decision model (in `decision_models`). Empty = the auto-approval model |
| `model_decision` | (none) | `SILO_MODEL_DECISION` | Default decision model, for the Bot's `decide` tool (must be in `decision_models`). Empty = the first of `decision_models` |
| `model_image` | (none) | `SILO_MODEL_IMAGE` | Default image model, for the Bot's `generate_image` tool; the Bot may name another from `image_models`. Empty = no image generation |
| `model_subagent` | (none) | `SILO_MODEL_SUBAGENT` | Default model for subagents a lead starts with `spawn_agent` (must be in `models`). Empty = the lead's own model. The lead may still name another allowed model |
| `model_memory` | (none) | `SILO_MODEL_MEMORY` | Memory model: each Bot's Memory automation reads chats and tidies memories with it, and the facts' summary is written with it (must be in `models`, and must call tools). Empty = the title model. A cheap model is enough |
| `embedding_model` | (none) | `SILO_EMBEDDING_MODEL` | Embeds memories (`remember`/`recall`) and indexed document chunks (`search_docs`). Must be an OpenAI-compatible provider returning 1536-wide vectors (`dimensions` is sent); changing the width means `make db-reset`. Admin → Settings → Models → Embeddings; a provider that cannot embed (Anthropic) is rejected |
| `transcribe_model` | (none; `off` also disables) | `SILO_TRANSCRIBE_MODEL` | Speech-to-text for composer dictation (the mic, `Transcribe` RPC) and the Bot's `transcribe` tool / `silo_runtime.transcribe`. Any OpenAI-compatible `/audio/transcriptions` model: OpenRouter (`openai/whisper-1`, `openai/whisper-large-v3`, …), OpenAI (`whisper-1`, `gpt-4o-transcribe`), or `local/<model>` for LocalAI / Speaches / vLLM / whisper.cpp (`--inference-path /v1/audio/transcriptions`). 25 MB per recording. `off` disables voice (the mic hides and the tool is not offered). A provider that cannot transcribe (Anthropic) is rejected |
| `memory.in_prompt` | `true` | `SILO_MEMORY__IN_PROMPT` | Put memories in the system prompt: preferences and lessons always, and every fact while the facts total under 6,000 characters, then a summary of them a background sweep writes with `model_memory` and rewrites (at most every 30 minutes) when they changed. Nothing is embedded when a message arrives; the Bot searches with `recall`. Off keeps memories out of the prompt |
| `tools.expose_completion` | (none) | `SILO_TOOLS__EXPOSE_COMPLETION` | Built-in tools to also offer the model as chat tools (YAML list, or comma-separated in the environment; `all` for every one). Most built-ins ship reachable from Python only (`silo_runtime.web_search`, `feed`, the taskboard, …) to keep each request small; name the ones a model handles badly that way |
| `memory.collect` | `true` | `SILO_MEMORY__COLLECT` | Have each Bot's Memory automation read web and channel chats that have been quiet for 10 minutes with new messages and keep what the Bot missed (one short `model_memory` run per chat, each message read once), and tidy the memories when 20 or more wait to be looked over (at most every 6 hours per Bot). The composer's memory button and the automation's own schedule work either way |
| `memory.infer` | `true` | `SILO_MEMORY__INFER` | Keep what nobody stated: preferences read off how the human corrects and chooses, and lessons from what failed and then worked. Such a memory is tentative, kept out of the prompt and out of `recall`, until a later conversation (at least an hour on) shows it again or its owner keeps it on the Memories page. Off, only what the human said in so many words is kept |
| `knowledge.enabled` | `true` | `SILO_KNOWLEDGE__ENABLED` | The folder index behind `search_docs`: the background sweep, adding folders on a Bot's Knowledge page, and the tool. Off hides the tool and stops syncing; what is already indexed stays in Postgres |
| `knowledge.sync_interval` | `15m` | `SILO_KNOWLEDGE__SYNC_INTERVAL` | How often an indexed folder is re-checked (Go duration, minimum `1m`). Drive folders are checked four times less often; a folder written to (by the Bot or the Files tab) is re-read about 15 s after it goes quiet. Embeds with `embedding_model` |
| `knowledge.ocr` | `true` | `SILO_KNOWLEDGE__OCR` | Read scanned PDF pages (only pages with next to no text) and image files (`png`/`jpg`/`tif`/`bmp`/`gif`/`webp`, at least 8 KB) with tesseract inside the Bot's box; image text must look like prose or it is dropped. At most 100 pages and 4 minutes per file. English and Polish ship in the bot image; every installed language is used. Off, scans are listed as not indexed (and re-read when you turn it on). A folder of photos makes syncing slow |
| `changes.enabled` | `true` | `SILO_CHANGES__ENABLED` | Workspace change tracking: each Bot's machine snapshots `/workspace` before and after every run and the Changes pane shows the differences as diffs. The history is a git store on the Bot's own disk (`data/bots/<id>/history`), never in Postgres. Drives, `tmp/`, `bot/` and dependency folders are not read; a Bot keeps at most 300 snapshots and 1 GB. The same switch covers the drive journal: each drive's sidecar notes what is written, deleted or renamed through the mount (names and sizes only, 2000 lines per Bot, in Postgres). Off stops new snapshots and journal lines and empties the pane; what was recorded stays |
| `changes.max_file_mb` | `8` | `SILO_CHANGES__MAX_FILE_MB` | A file over this is recorded by size and modification time only, never by content (1–100) |
| `changes.keep_days` | `30` | `SILO_CHANGES__KEEP_DAYS` | Snapshots older than this are forgotten (1–365) |
| `filter.enabled` | `true` | `SILO_FILTER__ENABLED` | Output rules: before a Bot reads what a tool brought back from outside (a web search, mail, a connector's answer), the filter model judges it and it passes, passes marked as untrusted, or is withheld. One model call per checked result. Off, every result passes whatever a Bot's rules say; the rules are kept |
| `filter.max_chars` | `48000` | `SILO_FILTER__MAX_CHARS` | The longest result the filter reads, in characters (1000–400000). A longer one is passed on marked as unchecked |
| `rules.approval_policy` | (none) | `SILO_RULES__APPROVAL_POLICY` | The operator's auto-approval policy, read **before** a Bot's own (Admin → Settings → Rules). It says what may run without a human; a Bot's own policy is appended after it. The operator's rule rows (a floor owners may tighten but not loosen) live in the database and are edited on the same page |
| `onboarding.prompt` | (built-in) | `SILO_ONBOARDING__PROMPT` | The interview a new Bot opens its main chat with when its owner leaves **Start with a short interview** on (and what `/onboard` runs). Empty uses the built-in one (`internal/prompts/ONBOARDING.md`) |
| `rules.output_policy` | (none) | `SILO_RULES__OUTPUT_POLICY` | The operator's output policy, read **before** a Bot's own (or the built-in one) by the filter model, on the same page |
| `audit.detail_days` | `7` | `SILO_AUDIT__DETAIL_DAYS` | How long an audit line is kept: a tool call and its decision, a result the output filter did not pass, a block, a flag or an alert. A background sweep drops what is older |
| `audit.traffic_days` | `30` | `SILO_AUDIT__TRAFFIC_DAYS` | How long the network filter's per-minute traffic summaries (host, requests, bytes) are kept. They are sums only: what was asked for lives in the detail lines |
| `context.window` | `128000` | `SILO_CONTEXT__WINDOW` | Fallback context window in tokens, for models whose provider does not report one (OpenRouter's `/models` `context_length` is used when it does) |
| `context.compact_at` | `0.8` | `SILO_CONTEXT__COMPACT_AT` | Fraction of the window at which a run summarizes its history into one turn before the next model call (0.1–0.98) |
| `runs.max_duration` | `120m` | `SILO_RUNS__MAX_DURATION` | Cap on one agent run (a Go duration such as `120m`/`2h`, or bare minutes). `-1` means unlimited. Subagent runs and a lead sleeping while its subagents work count toward it. A run that reaches it ends with an error saying so |
| `context.windows` | (none) | — | Per-model window overrides (YAML list of `{model, window}`; a list because model ids contain dots). Wins over the provider's report |
| `thinking.levels` | (none) | — | Per-model thinking-level overrides for the composer's Thinking picker (YAML list of `{model, levels}`; levels from `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`; `[]` hides the picker). Wins over the provider's report: OpenRouter `/models` `supported_parameters`, the Anthropic Models API `capabilities`, OpenAI's reasoning families (`o*`, `gpt-5*` → low/medium/high). `local` models have no levels unless listed here (sent as `reasoning_effort`) |
| `models` | (none) | — | Allowlist of text models (YAML list). Admin → Settings → Models → Allowed models edits it, and picking a model for a job under Models → Jobs allows it too; under Providers, **List models** asks a provider what its saved key can call and adds a model with one press |
| `decision_models` | (none) | — | Allowlist of decision models (YAML list), edited in the same two places. Only `openrouter` and `typesafe` ids are accepted, and an id cannot be in `models` too. Empty: no `decide` tool |
| `providers.<id>.api_key` | (none) | `SILO_PROVIDERS__<ID>__API_KEY` | Provider key. Required when that provider is used (optional for `local`) |
| `providers.<id>.base_url` | (provider default) | `SILO_PROVIDERS__<ID>__BASE_URL` | Override the API base URL |
| `providers.<id>.cache` | `true` | `SILO_PROVIDERS__<ID>__CACHE` | Prompt caching. `anthropic` adds `cache_control` breakpoints; `openai` sends `prompt_cache_key`; `openrouter` sends `prompt_cache_key` (it keeps a conversation on the upstream that holds its cache) and marks Claude prompts with `cache_control`, without which OpenRouter does not cache Claude at all. Other OpenRouter upstreams (OpenAI, DeepSeek, Grok, Gemini 2.5) cache a stable prefix on their own |
| `providers.<id>.cache_ttl` | `5m` | `SILO_PROVIDERS__<ID>__CACHE_TTL` | Claude cache lifetime (`5m`/`1h`), on `anthropic` and `openrouter`. `1h` costs more to write and survives the pause between a human's messages |
| `providers.anthropic.max_tokens` | `8192` | `SILO_PROVIDERS__ANTHROPIC__MAX_TOKENS` | Anthropic per-response output cap |
| `providers.claude.binary` | the installed copy, else `claude` on `PATH` | `SILO_PROVIDERS__CLAUDE__BINARY` | A Claude Code program of your own for the Claude Agent provider. Empty: the copy Admin's **Install** put in `<providers.claude.dir>/program`, else `claude` on the `PATH`. A relative path is read against the control plane's directory |
| `providers.claude.dir` | `<data_dir>/claude` | `SILO_PROVIDERS__CLAUDE__DIR` | Where Claude Code keeps its sign-in (`config/`), its sessions and Silo's record of them (`sessions/`), and where the installed program is (`program/`, which need not be backed up). Back it up with the rest of the data |
| `providers.claude.oauth_token` | (none) | `SILO_PROVIDERS__CLAUDE__OAUTH_TOKEN` | A long-lived token from `claude setup-token`, used instead of the stored sign-in. Admin can have Claude Code issue one |
| `providers.openrouter.ignore` | `DeepInfra` | `SILO_PROVIDERS__OPENROUTER__IGNORE` | Comma list of upstream hosts OpenRouter must not route to (`provider.ignore`). DeepInfra sends a tool call's arguments in one chunk at the end, so Python/terminal/write input cannot stream into the thread. `none` routes anywhere |
| `bootstrap.email` | (none) | `SILO_BOOTSTRAP__EMAIL` | The first admin. Used at start only while no admin can sign in (see Sign-in) |
| `bootstrap.password` | (none) | `SILO_BOOTSTRAP__PASSWORD` | Their password, when the account has to be created |
| `outbound.block_private` | `false` | `SILO_OUTBOUND__BLOCK_PRIVATE` | Refuse loopback, private and other internal addresses (and names resolving to one) for connections Silo makes on a user's word (see Outbound connections) |
| `outbound.allow` | (none) | `SILO_OUTBOUND__ALLOW` | Internal addresses that stay reachable: comma-separated IPs or CIDRs, each optionally with one port (`127.0.0.1:9223, 10.0.5.0/24`) |
| `auth.password` | `true` | `SILO_AUTH__PASSWORD` | Password sign-in. `false` leaves only OIDC, and only counts while OIDC is configured (see Sign-in) |
| `auth.trusted_proxies` | (none) | `SILO_AUTH__TRUSTED_PROXIES` | Comma-separated IPs/CIDRs of the reverse proxies in front of the control plane. `X-Forwarded-For` is believed only from them |
| `auth.host_cookie` | `false` | `SILO_AUTH__HOST_COOKIE` | Name the session cookie `__Host-silo_session`, which only the exact host can set. Counts only while `public_url` is https (see Sign-in) |
| `spaces.invite_signup` | `false` | `SILO_SPACES__INVITE_SIGNUP` | Let a Workspace's invite link make an account for someone who has none. Off, a link only joins someone already signed in, and new people come from the admin's invite links |
| `oidc.issuer` | (none) | `SILO_OIDC__ISSUER` | The OIDC provider's issuer URL. OIDC sign-in is on when this and `oidc.client_id` are set |
| `oidc.client_id` | (none) | `SILO_OIDC__CLIENT_ID` | OAuth client ID registered at the provider |
| `oidc.client_secret` | (none) | `SILO_OIDC__CLIENT_SECRET` | Its secret |
| `oidc.scopes` | `openid email profile` | `SILO_OIDC__SCOPES` | Space-separated scopes. `openid` is always requested |
| `oidc.label` | `Single sign-on` | `SILO_OIDC__LABEL` | Text of the button on the sign-in page |
| `oidc.auto_create` | `false` | `SILO_OIDC__AUTO_CREATE` | Make an account the first time someone signs in |
| `oidc.allowed_domains` | (none) | `SILO_OIDC__ALLOWED_DOMAINS` | Comma-separated email domains `auto_create` is limited to |
| `oidc.groups_claim` | `groups` | `SILO_OIDC__GROUPS_CLAIM` | ID token claim that lists a person's groups |
| `oidc.admin_group` | (none) | `SILO_OIDC__ADMIN_GROUP` | Members of this group are admins, checked at every OIDC sign-in |
| `search.engine` | `builtin` | `SILO_SEARCH__ENGINE` | Web search engine: `builtin`, `searxng`, `brave` or `exa`. `builtin` is the **Built-in Scraper**: it needs no key, asks DuckDuckGo, Yahoo, Seznam and Wikipedia at once, and merges what they return. It rate-limits itself per site (about ten requests a minute each, a search asking one or two sites), because every search leaves from the Control Plane's one address and a site asked too often blocks it. With that budget spent a search waits for its turn (up to 20 seconds, a query of a `web_search_many` batch up to a minute) and past that answers `busy` with the seconds to wait, which is not a block; a site that refuses is left alone from 30 seconds doubling to 30 minutes, one that merely timed out for 15 seconds to 2 minutes; when nothing can answer, an answer to the same query from the last hour is served and said to be one. Admin → Settings → Search lists each source's health as the server has seen it and runs a test search. `searxng` is your own [SearXNG](https://docs.searxng.org/) instance (`search.searxng.url`): no key and many more engines, kept working by its community; its `settings.yml` must list `json` under `search.formats`, and its limiter must let the control plane's address through. `brave` is [Brave's Search API](https://brave.com/search/api/) and `exa` is [Exa](https://exa.ai/): each is one provider's own index behind your key (`search.brave.api_key`, `search.exa.api_key`), so there is no scraping and no self-rate-limit, but every search spends the plan's quota (a 429 is retried once, then the Bot is told the plan is used up); answers are cached 5 minutes, and a rejected key or an account out of credit is said in words, never as an empty result. Exa takes no language, only the country. An engine's own keys live under `search.<engine_id>`. The old `duckduckgo_scraper` reads as `builtin` |
| `search.builtin.sources` | (the defaults) | `SILO_SEARCH__BUILTIN__SOURCES` | Comma-separated sources the Built-in Scraper asks. Empty asks `duckduckgo`, `yahoo`, `seznam`, `wikipedia` (and `marginalia` once it has a key). A list asks only what it names, which is also how to try `brave`, `mojeek` and `startpage`: since 2026 they answer a plain request from most addresses with a script challenge (a captcha, a proof of work), so they are off unless named |
| `search.builtin.marginalia_key` | (none) | `SILO_SEARCH__BUILTIN__MARGINALIA_KEY` | An API key for [Marginalia](https://about.marginalia-search.com/article/api/), an index of the small, text-heavy web (English queries only). With a key it is one more source; without one it is not asked (the shared `public` key is over its limit most of the time). A key is free for non-commercial use and its results are CC-BY-NC-SA |
| `search.searxng.url` | (none) | `SILO_SEARCH__SEARXNG__URL` | The root address of your SearXNG instance, for `search.engine: searxng` (`http://searxng:8080`) |
| `reader.render` | `true` | `SILO_READER__RENDER` | Let the page reader (`silo_runtime.web_read`) render a page that is empty until its scripts run. The rendering is done by Lightpanda, a headless browser in the calling Bot's own machine, never on the control plane; off, such a page is reported as needing a browser |
| `reader.block_private` | `true` | `SILO_READER__BLOCK_PRIVATE` | Refuse loopback, private and other internal addresses (and names or redirects leading to one) for the pages the control plane fetches because a Bot named their address. On by default, unlike `outbound.block_private`: the address comes from a model, which may be repeating what a web page told it |
| `reader.allow` | (none) | `SILO_READER__ALLOW` | Internal addresses the page reader may still reach: comma-separated IPs or CIDRs, each optionally with one port (`10.0.4.20, 10.0.5.0/24`) |
| `reader.github_token` | (none) | `SILO_READER__GITHUB_TOKEN` | A GitHub token for the page reader's GitHub reads (repositories, issues, files go through GitHub's API: 60 requests an hour without a token, 5,000 with any). It needs no permissions for public repositories |
| `search.brave.api_key` | (none) | `SILO_SEARCH__BRAVE__API_KEY` | Brave Search API subscription token, for `search.engine: brave`. Not the Built-in Scraper's `brave` source, which needs no key |
| `search.exa.api_key` | (none) | `SILO_SEARCH__EXA__API_KEY` | Exa API key, for `search.engine: exa` |
| `connector_vars.<name>` | (none) | `SILO_CONNECTOR_VARS__<NAME>` | Connector variable. Referenced as `${NAME}` in connector settings. **Plain text, not a secret** |
| `autoenable_connectors` | (none) | `SILO_AUTOENABLE_CONNECTORS` | List of library connector identifiers every new Bot gets. The env form is comma-separated (`lightpanda,fal_ai`) |

## Sign-in

Accounts live in Postgres; **Admin → Users** manages them. `bootstrap.*` is read at start and only matters while no admin can sign in (a new install, or the only admin was disabled): the user with `bootstrap.email` becomes an admin, and is created with `bootstrap.password` if there is none. Once an admin exists it does nothing, so changing the admin's email or password in Silo is not undone by a restart. That admin's first sign-in opens **Admin**, where a new installation is set up, and leaves `<data_dir>/first-admin-signed-in` behind (SiloOS reads it).

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

## Images

Silo runs four images: the Bot's machine (`silo-bot`), the STDIO sidecar (`silo-mcp-stdio`), the drive sidecar (`silo-drive`) and the network filter's gateway (`silo-egress`). A control plane names the images of the build it is (`internal/version`), so leave the four keys unset unless you mean to pin one:

| Build | Images |
|---|---|
| `go build`, `make build-cp`, `make dev` | `localhost/silo-*:dev`, built by `make images` and never pulled |
| `make release VERSION=v0.4.2` | `ghcr.io/d-513/silo-*:v0.4.2` |
| `make nightly` | `ghcr.io/d-513/silo-*:master-<sha>` |

`silo version` prints the build. With `images.pull: missing` the control plane fetches a release's images as it starts and before it makes a container from one it does not have; a release's tag never moves, so nothing more is needed. `always` is for a pinned tag that does move (`:master`, `:latest`).

A container made from an older image, or made another way (a new `bot_limits`), is made again by the rule for its kind; Settings → Containers and Admin → Containers mark it **Update waiting** until then:

| Container | Made again |
|---|---|
| Bot's machine | When it next starts from stopped. A running machine holds what the Bot has open, so it is never replaced under it: Stop, then Start (the workspace, drives and connectors stay) |
| Drive sidecar | With its machine: it stops with the Bot and is made again at the next start |
| STDIO sidecar | When the control plane next opens a session to it (after a restart, or the next use once a session ended) |
| Network filter gateway | Within a minute, running or not. It keeps its address on every Bot's network; the Bots lose their way out for the seconds it takes to start |

## Running the control plane in a container

`make cp-image` builds `images/cp/Containerfile` as `silo:<version>` (`make release` pushes it with the others). It holds the control plane with the web console compiled in, served on the same port as the API (`public_url` is that address behind your TLS): no second container and no proxy rules. Postgres is yours to run. The builder needs about 4 GB of memory.

The control plane makes every other container through the engine's socket, so the container needs four things:

- **The socket** at `/run/docker.sock` (the image's `DOCKER_HOST`). Whoever holds a rootful engine's socket is root on the host; running as uid 1000 inside is a second line, not the wall.
- **Its directory at the same path as on the host**: `-v /var/lib/silo:/var/lib/silo`. It holds `silo.yaml` (Admin → Settings writes it) and `data/`. The control plane hands the engine paths under `data/` to bind into the Bots' machines, and the engine reads them on the host, so a different path, or a named volume, points every Bot's workspace at nothing. Give it to uid 1000: `chown 1000:1000 /var/lib/silo`.
- **Its ports published**: 8080 (the Bots' machines dial it at `cp_url`, by default `host.containers.internal:8080`; put TLS in front for people), 8081 (`worker_addr`: where the network filter forwards the Bots' machines, and all they can reach of the control plane), 3479 UDP and TCP for calls, and 25 → 2525 for mail. Port 8080 then answers on the host for everyone who can reach it; **On a network of its own** below avoids that.
- **The database**: `SILO_DATABASE_URL`.

```sh
# Docker (rootful): the socket's group is added to the control plane's user.
docker run -d --name silo --restart unless-stopped \
  -v /var/run/docker.sock:/run/docker.sock \
  --group-add "$(stat -c %g /var/run/docker.sock)" \
  -v /var/lib/silo:/var/lib/silo \
  -p 8080:8080 -p 8081:8081 -p 3479:3479/udp -p 3479:3479/tcp -p 25:2525 \
  -e SILO_DATABASE_URL=postgres://… -e SILO_PUBLIC_URL=https://silo.example.com \
  ghcr.io/d-513/silo:v0.4.2

# Podman (rootless): keep-id makes the container's uid 1000 the user who owns the socket.
podman run -d --name silo --restart unless-stopped \
  --userns=keep-id:uid=1000,gid=1000 --security-opt label=disable \
  -v "$XDG_RUNTIME_DIR/podman/podman.sock:/run/docker.sock" \
  -v /var/lib/silo:/var/lib/silo \
  -p 8080:8080 -p 8081:8081 -p 3479:3479/udp -p 3479:3479/tcp -p 25:2525 \
  -e SILO_DATABASE_URL=postgres://… -e SILO_PUBLIC_URL=https://silo.example.com \
  ghcr.io/d-513/silo:v0.4.2
```

**On a network of its own.** Publishing 8080 only so the Bots' machines can come back in through the host is the long way round. Put the control plane on a network of the engine and name it in `cp_network`: the gateway and the drive sidecars join that network and dial the control plane by its container's name, and 8080 is published for your proxy alone, or not at all when the proxy is a container on a network with it.

```sh
podman network create silo
podman run -d --name silo --restart unless-stopped --network silo \
  --userns=keep-id:uid=1000,gid=1000 --security-opt label=disable \
  -v "$XDG_RUNTIME_DIR/podman/podman.sock:/run/docker.sock" \
  -v /var/lib/silo:/var/lib/silo \
  -p 127.0.0.1:8080:8080 -p 3479:3479/udp -p 3479:3479/tcp -p 25:2525 \
  -e SILO_CP_URL=http://silo:8080 -e SILO_CP_NETWORK=silo \
  -e SILO_DATABASE_URL=postgres://… -e SILO_PUBLIC_URL=https://silo.example.com \
  ghcr.io/d-513/silo:v0.4.2
```

Three things follow from the control plane no longer being on the host:

- **Its resolvers are the network's.** With the network filter on and no `egress.dns`, bind the host's `/run/systemd/resolve` read-only at the same path (a host with systemd-resolved), or set `egress.dns`: the gateway cannot use the DNS of a network it is not on.
- **Callers' addresses.** A rootless Podman forwards a published port through `rootlessport`, and the container sees its own address in place of the caller's: mail is then never verified (SPF needs the sender's address) and its limits count everyone as one. Podman 6 has a forwarder that keeps the address: `rootless_port_forwarder = "pasta"` under `[network]` in `containers.conf` (experimental; change it only while no container runs). An older Podman has no such option, and works without it, less those addresses. A rootful engine keeps them by itself.
- **Calls** need the host's addresses in `voice.webrtc.public_ips`: the control plane's own are the network's.

For a machine that runs Silo and nothing else, SiloOS (`images/os/`, see images/os/README.md) is a bootable image that does all of this with rootless Podman.

Do not name the container `silo-…`: that prefix is the control plane's own containers, which `make cleanup` removes and Admin → Containers lists. The image has a `HEALTHCHECK` (`silo healthcheck`, which asks `/healthz`), stops on SIGTERM, and runs under `tini`. The network filter needs Podman (see Network filter); on Docker it is off.

## Network filter

Every Bot's machine sits on a network of its own, with a small **gateway** as its only way out. So one Bot cannot reach another, and no Bot can reach your LAN. The gateway watches DNS, HTTP, HTTPS and raw SOCKS connections; it sees the host, the method and the number of bytes, never the content of a connection it lets through. What it blocks, flags, or thinks a human should look at lands on the Bot's **Audit** tab and at `/admin/audit`.

```yaml
egress:
  # enabled: true
  # image: …                    # unset follows the release (see Images)
  # block: bad.example.com, *.tracker.example.com, 203.0.113.7, 203.0.113.0/24
  # trust: registry.npmjs.org     # reached even where a rule would check it
  # pass: chat.example.com        # forwarded untouched (websockets, odd TLS)
  # feeds: []                    # a threat list set of your own
  # dns: 1.1.1.1, 9.9.9.9         # the gateway's resolvers; empty is the system's
```

- **`block`** is a floor: no Bot may reach it, and an owner may add to it from the Bot's Rules page but never remove it. `trust` and `pass` are yours too, and shown locked; the Bot's Rules page says "from the operator" on them. Patterns are hosts, `*.` prefixes, IPs and CIDRs, matched case-insensitively; an owner may add at most 200 of their own.
- **What the lists mean.** `block` refuses the connection (the Bot gets a plain `403` saying it was blocked, and nothing for a flag). `trust` reaches the host without inspection even where an owner's rule would check it — an internal package registry is the case. `pass` forwards it untouched, for a service the gateway would otherwise break (websockets, unusual TLS).
- **Threat lists** (`feeds`) are downloaded and refreshed by the gateway itself; three are built in and none needs a key (Spamhaus DROP, Emerging Threats' compromised IPs, and Hagezi's threat-intelligence domains; abuse.ch's URLhaus and ThreatFox host files were retired in 2026-11). A host on one is blocked and recorded. Naming any feed replaces the built-in set; each is `id`, `url` and `format` (`hosts`, `domains`, `ips`, `cidrjson`).
- **DNS** (`dns`): the resolvers the gateway looks every outside name up with, comma-separated IPs, each optionally with a port (`10.0.0.53:5353`); names are not taken. Empty uses the system's: systemd-resolved's upstreams (`/run/systemd/resolve/resolv.conf`) where that file is, else the nameservers in the control plane's own `/etc/resolv.conf` (never a loopback stub). Either way the control plane hands them to the gateway, which uses them instead of what the container engine wrote into its `resolv.conf`: the engine lists the Bots' internal networks there, and after a restart nothing else. A change reaches the running gateway within 30 seconds, without a restart. Admin → Settings → Network shows the resolvers in use; with none found, set them here.
- **Turning it off** (`enabled: false`) leaves Bot machines on today's unfiltered network; Admin warns that nothing is being watched. The lists are kept and take effect again when it is back on.
- **The gateway** is one container per container engine. Each client's `/24` is carved from `subnet_pool` (`10.213.0.0/16`, enough for 256 Bots without overlap); change it only if it collides with a network you really use. Admin → Settings → Network shows the gateway's certificate and the state of each threat list, and rotates the certificate (running machines pick up a new one on their next start).

## Content host & tunnels

What Bots make is served on a domain of its own (`content.host`), never the console's origin, dispatched by subdomain prefix:

- **`tunnel-<name>.<content.host>`** — a **tunnel** to a web service running on a Bot's own localhost (a dev server, a dashboard, a notebook). Private tunnels open for the Bot's owner, signed in to Silo; public ones for anyone with the link. Make one on the Bot's **Tunnels** tab, or the Bot opens one with `open_tunnel` (making one public asks the owner first).
- **`app-<name>.<content.host>`** — a **live app artifact** (an `.html` file or folder with `index.html` handed over via `artifact`), served from its pinned version in a sandboxed iframe for the signed-in owner.
- **`share-<name>.<content.host>`** — a **public shared link** created by the owner from an artifact card, served from its pinned copy on the control plane (`data/shares/<id>`) so it works even while the Bot is stopped.

```yaml
content:
  host: content.example.com   # required unless public_url is local (below)
  # scheme: https             # default: public_url's scheme

tunnels:
  # enabled: true
```

- **Local development**: with `public_url` on `localhost`/`127.0.0.1`, `content.host` defaults to `localhost:<port of http_addr>` (links like `http://tunnel-quiet-amber-heron.localhost:8080`). Browsers resolve `*.localhost` to loopback, so no DNS is needed. It uses the control plane's own port, not Vite's.
- **Anywhere else, set `content.host`.** Without it, tunnels, live apps and public shares are unavailable: the Tunnels tab says so, the Bot has no tunnel tools, and HTML artifacts fall back to download-only.
- **DNS and TLS**: point a wildcard record `*.<content.host>` at the control plane and serve a certificate for `*.<content.host>` (a wildcard certificate, or on-demand TLS in the fronting proxy). The proxy in front must pass WebSocket upgrades and the original `Host` header.
- **Use a separate domain** from the control plane's (`silo.example.com` → content under `silo-content.example.com`, not `content.silo.example.com`). A page a Bot serves could otherwise set cookies for the whole domain and reach the control plane with them; a `content.host` equal to the `public_url` host is refused outright.
- **HTTPS by Silo itself** (`caddy.*`, `internal/caddy`): the control plane gives Caddy its whole configuration through Caddy's admin API, and gives it again whenever Caddy's differs (every 15 s). `public_url`'s name (https, port 443) is proxied to the control plane with the issuer `caddy.mode` picks, and `*.<content.host>` under the same issuer: through a DNS provider (`caddy.dns_provider`) or Caddy's own authority as one wildcard certificate, with Let's Encrypt over port 80 as a certificate per name made on demand. Before each of those Caddy asks `/caddy/ask?domain=…`, which answers yes only on loopback and only for `public_url`'s name and a tunnel, live app or share that exists, so nobody can have Caddy request certificates for names at will. On SiloOS the machine's host name and addresses are always served too, with Caddy's own authority. While that authority certifies anything, its root certificate is offered for download in Admin → Settings → HTTPS. DNS providers need `silo-caddy` (`make caddy-image`, published with the other images), the official image has none. Caddy and the control plane share a machine (the upstream is `127.0.0.1:<http_addr port>`); start Caddy with `caddy run --resume` (silo-caddy's default) and its admin API on a socket only the control plane reaches (`CADDY_ADMIN=unix//…`), and add `127.0.0.1` to `auth.trusted_proxies`.
- **Changing `content.host`** takes effect on the next request and moves every existing tunnel, app and shared link at once (rows store only the name). Private tunnels' and apps' sign-in cookies belong to the old host, so owners sign in again.
- **Private means signed in to Silo.** A private tunnel's or live app's access is tied to the owner's Silo session: signing out (or the session expiring) ends it immediately, and the next visit asks them to sign in again. An already-open WebSocket is not cut off until it closes.
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

The **Lightpanda** library preset (not attached by default: name it in `autoenable_connectors` to give it to every new Bot) uses `http_url: ${LIGHTPANDA_URL}/mcp`, so define `LIGHTPANDA_URL` here (dev: `http://localhost:9223`, the shared instance `make db-up` starts). Until it is set the connector fails to connect.

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

`cp_url` must be reachable from inside the Bot and from STDIO sidecars. Create adds `host.containers.internal:host-gateway` to both, or with `cp_network` set puts them on that network instead. If the worker or a bridge never connects, the container cannot see the CP.

## Example

```yaml
http_addr: ":8080"
data_dir: ./data
docker_host: unix:///run/user/1000/podman/podman.sock
cp_url: http://host.containers.internal:8080
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
  engine: builtin      # or brave / exa, with search.<engine>.api_key

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
