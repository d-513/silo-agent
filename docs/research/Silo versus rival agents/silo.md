# Silo: capability inventory (from its own source repository)

All sources are files in the Silo repository (`/Users/szsob/code/silo-agent`, state of 2026-11-17, 397 commits since 2026-09-05); links are relative to this notes file. `AGENTS.md` is the maintained behaviour map and is the most current source. `TODO.md` is a competitor status report dated 2026-10-21 and is **partly out of date**: where it conflicts with AGENTS.md (voice output and calls, shared Bots, retry), AGENTS.md wins and the conflict is noted.

## 1. Deployment & hosting

### Takeaway
Self-hosted only: one Go control plane (CP) binary, Postgres 17 + pgvector, and a Podman (or Docker) engine that runs one durable Linux container per Bot plus sidecars. There is no installer, no hosted offering, no documented backup/restore.

### Cited Findings
- Architecture: Go Control Plane (agent loop, users, rules, every key and token), Go Worker inside each Bot box that dials out to the CP over ConnectRPC, Vite/React web frontend — [AGENTS.md § Processes and hops](../../AGENTS.md); "locked" stack table — [docs/Description.md § Stack](../../docs/Description.md)
- Database: Postgres 17 + pgvector, GORM (pgx); every string scrubbed of NUL/invalid UTF-8 — [docs/Description.md](../../docs/Description.md)
- Container engine reached through the Docker API client on the Podman socket by default (`DOCKER_HOST`/`docker_host`); "Docker may be local or on another machine" without changing the worker protocol; no `docker exec`/`cp`, no published ports, CP never dials into the box — [AGENTS.md § Working in this repo / Containers](../../AGENTS.md), [docs/Description.md § Two hops](../../docs/Description.md)
- Four images: Bot box (`botimage/`, Debian trixie, Openbox desktop), STDIO MCP sidecar (`mcpimage/`), drive sidecar (`driveimage/`), egress gateway (`egressimage/`); every base pinned by digest — [AGENTS.md § Worker and image](../../AGENTS.md)
- Prerequisites: Go 1.26+ with cgo (libopus for calls), pnpm + Node 22+, Podman or Docker with a reachable socket, an OpenRouter key (for dev) — [DEVELOPMENT.md § Prerequisites](../../DEVELOPMENT.md)
- Config: Koanf defaults → `silo.yaml` → `SILO_*` env; Admin → Settings edits `silo.yaml` live (form per category + YAML editor) — [docs/CONFIGURATION.md](../../docs/CONFIGURATION.md)
- Per-Bot resource limits `bot_limits.memory_mb`/`pids` (default no memory cap, 4096 processes) — [AGENTS.md § Containers](../../AGENTS.md)
- Self-admitted: "Install needs Docker/Podman and Postgres; there is no desktop installer"; no documented backup/restore of Postgres + `data/`; nothing stops idle boxes automatically (no scale-to-zero) — [TODO.md §6, §8](../../TODO.md)
- Not in v1: multi-host Docker UI, Wayland — [docs/Description.md § not doing in v1](../../docs/Description.md)

### Inferences
- Operator-grade, server-shaped product (one CP serving many users), closer to a team platform than a single-user desktop agent; install friction is materially higher than signed desktop apps.

### Gaps
- No production deployment guide, compose bundle for the CP, or licence file found in the repository root (no `LICENSE*`).

## 2. Multi-user / teams / access control

### Takeaway
Genuinely multi-user: accounts, admin, Workspaces with roles, per-Bot roles, invite links, OIDC SSO, TOTP 2FA, sessions management, and an RPC-level access table enforced by tests. Several people can share one Bot.

### Cited Findings
- Workspaces hold Bots and people; roles `owner|admin|member|guest` per Workspace and `manage|use` per Bot; every check through `access`; `access.RPCs` declares who may call every RPC, pinned by `TestEveryRPCChecksAccess` — [AGENTS.md § Workspaces and access](../../AGENTS.md)
- Invite links (single-use, 7 days, hash only stored), Workspace transfer, removal with a hand-off plan for Bots only that person manages, access ending at once (sockets, streams, calls closed) — [AGENTS.md § Workspaces and access](../../AGENTS.md)
- "Several people on one Bot": chat authors, managers read/stop others' chats but never write in them, per-person approvals (users answer their own with Allow once/Deny; manager-only actions such as automations, SOUL, public tunnels, channel sends) — [AGENTS.md § Several people on one Bot](../../AGENTS.md). Conflicts with TODO.md 10-21 "no shared Bot between teammates" — [TODO.md §5 #10](../../TODO.md) (superseded)
- "Lending": a connection/drive carrying a person's identity stays personal (only in Bots whose people are exactly its lender) unless shared — [AGENTS.md § Workspaces and access](../../AGENTS.md)
- Accounts: bootstrap admin, password + RFC 6238 TOTP with recovery codes, throttling (per account+IP, per IP, per account), sessions list with "sign out everywhere", one OIDC provider (PKCE + nonce, admin group mapping, auto-create by domain) — [AGENTS.md § Accounts](../../AGENTS.md)
- Workspace Activity audit (joins, roles, grants, sign-ins) for admins — [AGENTS.md § Workspaces and access](../../AGENTS.md)
- Instance admins cannot open another user's Bot — [AGENTS.md § Workspaces and access](../../AGENTS.md)
- Not built: passkeys, SCIM, more than one OIDC provider — [TODO.md §8](../../TODO.md); `Bot.activity` not filtered per viewer (a "Can use" person sees titles of others' live chats) — [TODO.md §10](../../TODO.md)

### Inferences
- "The Bot is the unit of trust": people on one Bot share its memory, files and connections; the isolation boundary between people is separate Bots — [AGENTS.md § Several people on one Bot](../../AGENTS.md). This is an explicit design choice, not per-user memory partitioning.

### Gaps
- No SCIM/LDAP provisioning; no per-user memory isolation inside a shared Bot (by design).

## 3. Execution sandbox (container, desktop/VNC, browser, shell, Python)

### Takeaway
Every Bot always gets its own durable, capability-narrowed Linux container with an X11 desktop the human can watch and take over (VNC), a PTY console, a file browser, headed Chromium, a persistent Python interpreter per conversation and a shell. No host-execution mode exists.

### Cited Findings
- One durable container per Bot; desktop 1600×900 X11 (Xvfb + Openbox + Thunar + mousepad + dock); x11vnc/noVNC tunnelled over the worker's own connection (no published VNC port); PTY console in `/workspace` — [AGENTS.md § Worker and image / Desktop and Chromium](../../AGENTS.md), [docs/Description.md](../../docs/Description.md)
- Box runs narrowed: all capabilities dropped except a small set (`botCaps`; `SYS_CHROOT` for Chromium's sandbox; no raw sockets or device nodes), runs as uid 1000 with passwordless sudo; machine is UTC — [AGENTS.md § Containers](../../AGENTS.md)
- Installed: Node LTS + npm/npx/pnpm, Python 3 with data/doc libs, uv, git, rg, fd, gcc/make, pandoc, qpdf, ocrmypdf, tesseract, ffmpeg, ImageMagick, Graphviz, sqlite3, Lightpanda — [internal/prompts/SYSTEM.md § Machine](../../internal/prompts/SYSTEM.md)
- Python: persistent interpreter per conversation (max 4, closed after 30 idle min), 5-minute per-command limit (clock stops while waiting on approvals), SIGINT stop — [AGENTS.md § Python](../../AGENTS.md)
- Computer use: one `desktop` chat tool (`look`/`click`/`type`/`key`/`scroll`), each action its own rule; same actions on `silo_runtime` so a secret can be typed without passing through the model — [AGENTS.md § Desktop and Chromium](../../AGENTS.md)
- Chromium: dock launcher with uBlock Origin Lite + a stealth extension, managed policy turning off password manager/autofill/sign-in/notifications, persistent profile, ~67% zoom; Playwright `chrome_page()` on the same headed browser for data extraction only — [AGENTS.md § Desktop and Chromium](../../AGENTS.md), [SYSTEM.md § Playwright](../../internal/prompts/SYSTEM.md)
- **Pause**: owner takes over the desktop; click/type/key/scroll, Chromium and Playwright cells are refused before any approval while paused; `look` still works — [AGENTS.md § Desktop and Chromium](../../AGENTS.md)
- Five file tools (`read`/`write`/`patch`/`delete`/`grep`, ripgrep) + `present`; uploads 50 MB to `/workspace/tmp` — [AGENTS.md § File tools](../../AGENTS.md)
- Changes: shadow-git snapshots of `/workspace` outside the workspace, diff per run, owner Restore of a file or a whole change (itself undoable); drive writes journalled from rclone's log — [AGENTS.md § Changes](../../AGENTS.md)
- A message to a stopped Bot starts its box — [AGENTS.md § Containers](../../AGENTS.md)
- Not covered (pause): a Playwright page kept in a variable from before the pause and driven by a later cell that does not mention Playwright — [AGENTS.md § Desktop and Chromium](../../AGENTS.md)

### Inferences
- The sandbox is the only execution mode (TODO.md: "One container per Bot, always"), unlike frameworks that default to the host and offer Docker as opt-in — [TODO.md §2](../../TODO.md).

### Gaps
- No remote backends (SSH, Modal, Daytona etc.); no GPU passthrough mentioned.

## 4. Tools, MCP & integrations

### Takeaway
MCP-first: HTTP (Streamable + legacy SSE) and STDIO MCP via the official Go SDK, with OAuth handled on the CP; STDIO servers run in isolated per-grant sidecars. A 64-preset connector library plus two Go built-ins (Email IMAP/SMTP, Calendar CalDAV). Tools are exposed "Python-first" to save tokens.

### Cited Findings
- Connector library (64 presets in `internal/catalog/connectors.json`): GitHub, Linear, Notion, Stripe, Sentry, Context7, GitLab, Postman, Semgrep, CircleCI, Buildkite, Atlassian, Asana, Monday.com, ClickUp, Airtable, Todoist, Miro, Calendly, Zapier, Slack, Gmail, Google Drive, Google Calendar, Google Sheets, Email (built-in), Calendar (built-in), Resend, Lightpanda, Exa, Tavily, Firecrawl, Perplexity, DeepWiki, Microsoft Learn, Hugging Face, Supabase, Neon, MongoDB, DBHub, ClickHouse, Prisma Postgres, Cloudflare, Azure, Vercel, Netlify, Railway, DigitalOcean, Heroku, Datadog, PagerDuty, Square, Xero, PayPal, Mercury, Ramp, Alpha Vantage, fal.ai, Obsidian, Figma, Canva, HubSpot, Attio, Klaviyo — [internal/catalog/connectors.json](../../internal/catalog/connectors.json); count confirmed in [TODO.md §1](../../TODO.md)
- `internal/mcpx` wraps the official go-sdk; tries Streamable HTTP then legacy 2024-11-05 HTTP+SSE; OAuth tries CIMD, pre-registered client, then DCR; headers and tokens stay on the CP — [AGENTS.md § MCP over HTTP and STDIO](../../AGENTS.md)
- STDIO: one isolated container per grant, bridge dials the CP, structured argv never a shell, no published port; npm cache per Bot — [AGENTS.md § MCP over HTTP and STDIO / Other people's bytes](../../AGENTS.md)
- Connections are Workspace-level, granted per Bot; `${secrets.NAME}` resolves per Bot; one shared token refresh per connection — [AGENTS.md § Connections and grants](../../AGENTS.md)
- Built-in Email: IMAP (go-imap v2) + SMTP (go-smtp), Gmail/Outlook presets, attachments; Calendar: CalDAV (iCloud, Fastmail, Nextcloud, Yahoo, any URL), client-side recurrence expansion; reads Allow, writes Ask by default — [AGENTS.md § Built-in connectors](../../AGENTS.md), [docs/Description.md](../../docs/Description.md)
- Connector tools always reachable from Python (`tools.<slug>.<fn>`), optionally as chat tools (`<slug>__<action>`) via a per-connector switch — [AGENTS.md § Tools are one engine](../../AGENTS.md)
- Exposure is Python-first: at most 17 chat tools by default; everything else is a `silo_runtime` function (the "code execution with MCP" pattern) — [AGENTS.md § Tools are one engine](../../AGENTS.md), [docs/Description.md § Who owns the agent loop](../../docs/Description.md)
- Bot can offer setup of a library connector/channel/drive via a card that opens Silo's own form; it may prefill only non-secret, non-address fields — [AGENTS.md § Setup offers](../../AGENTS.md)
- Drives: 15 rclone providers (B2, Box, Dropbox, Google Drive, Nextcloud, OneDrive, ownCloud, pCloud, R2, S3, S3-compatible, Seafile, SFTP, SMB, WebDAV) mounted by a per-Bot FUSE sidecar that holds credentials — [internal/drives/catalog](../../internal/drives/catalog), [AGENTS.md § Drives](../../AGENTS.md)
- Outbound SSRF guard for user-typed addresses (`outbound.block_private`, checked after DNS at dial) — [AGENTS.md § Outbound connections from the CP](../../AGENTS.md)

### Inferences
- No inbound webhook triggers and no Bot-authored MCP servers; Muse-style "build a connector for me" is listed as a next step, not built — [TODO.md §5 #1, §6](../../TODO.md)

### Gaps
- No payment rail and no password-manager connector (1Password/Bitwarden) — [TODO.md §5 #9](../../TODO.md)

## 5. Skills / plugins / marketplace

### Takeaway
Agent Skills (`SKILL.md`) format with a small seeded library (6) and install from GitHub URL or zip; no plugin SDK, hooks or public marketplace.

### Cited Findings
- Library skills seeded from `internal/catalog/skills`: `product-self-knowledge`, `pdf`, `word`, `spreadsheets`, `presentations`, `images`; auto-enabled, owner can disable — [AGENTS.md § Skills](../../AGENTS.md), [internal/catalog/skills](../../internal/catalog/skills)
- Workspace skills installed from a GitHub URL or zip (10 MB / 25 MB / 1000 files caps), switched on per Bot ("Choose Bots") — [AGENTS.md § Skills](../../AGENTS.md)
- A Bot can emit a skill directory as an artifact; it installs only when a human presses **Save skill** — [AGENTS.md § Artifacts](../../AGENTS.md)
- Self-assessed gaps: "Hooks, plugins, webhooks: Missing"; "Silo has MCP, skills and in-tree Go only"; self-improving skills Partial (human clicks Save skill) — [TODO.md §5 #5, #15](../../TODO.md)

### Inferences
- Extensibility is via MCP + skills + in-tree Go built-ins; there is no third-party plugin ecosystem.

### Gaps
- No plugin SDK, marketplace or review process.

## 6. Memory & personalization

### Takeaway
A structured long-term memory (fact/preference/lesson/work) in pgvector with a background "memory keeper" job, tentative memories that need evidence before use, retire-not-delete semantics, plus SOUL (persona) and Knowledge RAG over workspace/drive folders with OCR.

### Cited Findings
- Memory kinds: fact, preference, lesson, work; statuses active/tentative/retired; an inferred memory waits until seen on a separate occasion (2 sightings ≥ 1 h apart), stated by the human, or kept by the owner — "code promotes, never a model's say-so" — [AGENTS.md § Memories and SOUL](../../AGENTS.md)
- Nothing rewritten or deleted by a model: updates `replaces` (retire), retired memories restorable 30 days — [AGENTS.md § Memories and SOUL](../../AGENTS.md)
- Memories reach the model through the prompt, never through a search while the human waits: preferences/lessons verbatim, facts in full up to 6000 chars then a background digest, 6 newest work entries; `recall` is hybrid (vector + keyword) — [AGENTS.md § Memories and SOUL](../../AGENTS.md)
- Memory keeper: a pinned automation narrowed by a job profile (own prompt/model/tools, no machine) reads quiet chats after 10 min, tidies when ≥ 20 memories wait; treats tool results as outside content — [AGENTS.md § The memory keeper](../../AGENTS.md)
- Memories page with filters, Keep, Put back, search; humans do not add/edit memories — [AGENTS.md § Memories and SOUL](../../AGENTS.md)
- SOUL stored in Postgres, in every run's prompt, editable by manager or by the Bot (`bot.soul`, manager-only) — [AGENTS.md § Memories and SOUL](../../AGENTS.md)
- Knowledge (RAG): owner picks workspace/drive folders; worker extracts (PDF, docx/odt/rtf/epub/html, .doc), OCR of scanned pages via tesseract; hybrid pgvector + tsvector search with RRF; incremental sync by size+mtime then sha256, rename detection; caps 5000 files, 2 MB text/file — [AGENTS.md § Knowledge (RAG)](../../AGENTS.md)
- Onboarding interview (`/onboard`) via ask cards: areas of help, facts, SOUL, proactivity, starter automations — [AGENTS.md § Asking the owner](../../AGENTS.md)
- Not built: search across past chats, external memory providers, transcript import; "nothing crosses between Bots yet" for work memories — [TODO.md §5 #4](../../TODO.md), [AGENTS.md § Memories and SOUL](../../AGENTS.md)

### Inferences
- Memory design prioritizes cache stability (lists oldest first, never re-ordered by use) and resistance to injected memories (tentative until corroborated).

### Gaps
- No cross-chat semantic search tool for the Bot beyond `read_chats`.

## 7. Scheduling, automations, proactivity, heartbeat

### Takeaway
Cron automations in the Bot's time zone, a pinned Heartbeat, a proactivity dial (off/quiet/active), Dashboard probes on intervals, and mail wake from verified senders. No webhook triggers or one-shot "at" schedules.

### Cited Findings
- Automations: name, prompt, 5-field cron in the Bot's zone, fresh context each run, fired once (skip if previous run live), ≥ 5 min between firings, ≤ 50 per Bot; create/update Ask, manager-only — [AGENTS.md § Automations](../../AGENTS.md)
- Pinned Heartbeat (no schedule until given one), Dashboard curator and Memory keeper as pinned automations — [AGENTS.md § Automations](../../AGENTS.md)
- Proactivity: `off` (default), `quiet` (09:00 Heartbeat to the Feed), `active` (every 3 h 09–21, may ask in the main chat) — [AGENTS.md § Asking the owner, setup offers, proactivity](../../AGENTS.md)
- Mail wake: a verified (DKIM/SPF-aligned) message from an allow-listed sender starts a run, ≤ 10/hour — [AGENTS.md § Mail](../../AGENTS.md)
- Time zone follows the owner's device unless pinned; frozen per message so history never changes — [AGENTS.md § Time zones](../../AGENTS.md)
- Left: webhook triggers, one-shot "at" schedules, heartbeat active hours, per-job delivery target — [TODO.md §5 #1](../../TODO.md)

### Inferences
- Event-driven triggers are limited to inbound channel messages and verified mail.

### Gaps
- No webhook/event bus triggers.

## 8. Messaging channels

### Takeaway
Three chat channels — Telegram, WhatsApp (unofficial linked-device), Discord — plus a receive-only email inbox per Bot. No Slack, Signal, Matrix, iMessage/SMS, Teams, or outbound email channel.

### Cited Findings
- Built-in adapters only: Telegram (gotd MTProto), WhatsApp (whatsmeow linked device on the owner's account; GUIDE says use a spare number), Discord (discordgo) — [AGENTS.md § Channels](../../AGENTS.md), [internal/channels](../../internal/channels)
- Each channel bound to exactly one conversation; `reply_mode` for groups; inbound files saved to workspace, voice memos transcribed, ordered per-conversation lane; typing indicator; live-only (never backfills) — [AGENTS.md § Channels](../../AGENTS.md)
- Discord sends with `allowed_mentions: users only` so a model can't `@everyone` — [AGENTS.md § Channels](../../AGENTS.md)
- Receive-only SMTP listener per Bot (`<name>@<mail.domain>`), never sends/relays, DKIM/SPF "verified" marking — [AGENTS.md § Mail](../../AGENTS.md)
- Self-assessed: "Three chat channels against Hermes' 20+ and OpenClaw's ~25; nothing on iMessage or SMS"; "Mail is receive-only; Silo sends no mail" — [TODO.md §8](../../TODO.md)
- Telegram bot accounts and WhatsApp cannot read platform history (falls back to local log) — [AGENTS.md § Channels](../../AGENTS.md)
- Not built: binding an owner's DM channel to the main chat (schema-ready) — [TODO.md §9](../../TODO.md)

### Inferences
- Channel breadth is Silo's weakest dimension versus OpenClaw/Hermes.

### Gaps
- Sending email from the Bot's own address requires the Email connector against someone's account.

## 9. Voice

### Takeaway
Dictation, Read aloud (TTS) on every reply, and live in-browser voice calls over WebRTC with barge-in and a fast "talker" model, across many speech vendors. No telephony (PSTN), iOS calls exist in code but are untested on hardware.

### Cited Findings
- Dictation via a transcription model (default `openrouter/openai/whisper-1`); composer mic never auto-sends — [AGENTS.md § Voice](../../AGENTS.md)
- Read aloud: sentence-by-sentence streamed MP3/PCM; per-Bot speech model, voice, speed — [AGENTS.md § Voice](../../AGENTS.md)
- Calls: browser Silero VAD, WebRTC (pion) outbound audio, Opus via libopus, barge-in with duck/interrupt, a "talker" model (`model_voice`) answering small talk with only three tools (`delegate`, `stop_task`, `end_call`) so "the talker cannot act"; ending a call does not end the work — [AGENTS.md § Voice](../../AGENTS.md)
- Live streaming STT/TTS paths with word timings when the model supports it — [AGENTS.md § Voice / Model kinds](../../AGENTS.md)
- Speech vendors: Deepgram, ElevenLabs, Cartesia, Inworld, Soniox, AssemblyAI, plus OpenAI/OpenRouter/Groq/local — [AGENTS.md § Configuration / Model kinds](../../AGENTS.md)
- Caveat: vendor wire protocols "pinned by fakes … none has been run against the real service yet" — [AGENTS.md § Model kinds](../../AGENTS.md)
- Conflict: TODO.md 10-21 says "Speech to text only… no TTS, calls" — [TODO.md §2, §8](../../TODO.md); superseded by AGENTS.md's Voice section (calls and Read aloud now documented)
- Open per TODO.md §10: no WebSocket media fallback when ICE finds no route; approvals by voice unused; no phone line, wake word or speech-to-speech talker — [TODO.md §10](../../TODO.md) (some items, e.g. streaming STT/TTS, now appear built per AGENTS.md)
- iOS calls: CallKit, echo-cancelled AVAudioEngine, Silero VAD — "none of it has met real hardware" — [ios/todo_skipped.md § Not yet run on a phone](../../ios/todo_skipped.md)

### Inferences
- Voice is in-app (browser/iOS) only; competitors that place phone calls (OpenClaw, Instinct, Muse per TODO.md) remain ahead on telephony.

### Gaps
- No measured latency figures (P50/P95 panel not built per TODO.md §10).

## 10. Clients

### Takeaway
Web console (React, EN/PL/DE, dark mode, phone layout) is primary; a native SwiftUI iOS app with widgets, Live Activities, share extension and calls lags the web. No Android, desktop app, or CLI.

### Cited Findings
- Web: Vite + React + Tailwind, ConnectRPC + WebSockets; tabs per chat, right rail panes (Files, Desktop, Console, Changes, Mail), layout below 960 px — [AGENTS.md § Frontend / Layout](../../AGENTS.md)
- i18n: English, Polish, German; iOS not translated — [AGENTS.md § Languages](../../AGENTS.md)
- iOS: SwiftUI app + `SiloWidgets` (widgets, Live Activities, controls) + `SiloShare` share sheet; App Intents, Action Button `CallBotIntent` — [AGENTS.md § iOS](../../AGENTS.md)
- iOS web-only areas: Desktop (VNC), Console, channel setup wizard, connector OAuth/custom MCP form, Admin, OIDC, dashboard arranging, Runs review, artifact sharing — [ios/todo_skipped.md](../../ios/todo_skipped.md)
- No push service (Live Activities go stale when suspended) — [AGENTS.md § iOS](../../AGENTS.md); "No APNs or web push" — [TODO.md §5 #12](../../TODO.md)
- Channels (Telegram/WhatsApp/Discord) act as additional thin clients — [AGENTS.md § Channels](../../AGENTS.md)

### Inferences
- No OpenAI-compatible API endpoint for programmatic use ("Left: … an OpenAI-compatible API endpoint") — [TODO.md §5 #16](../../TODO.md)

### Gaps
- No Android, desktop, or CLI client found in the repository (`ios/` and `web/` only).

## 11. Security & safety

### Takeaway
Security is the core design: keys never in the box, one gate for every action with four modes and three rule layers, model-judged auto-approval, an output filter that judges tool results for prompt injection, an egress gateway that decrypts and filters all box traffic with surrogate secrets, and secret maskers.

### Cited Findings
- Bots never hold provider keys, OAuth tokens or connector credentials; Worker strips `SILO_BOT_TOKEN`/`SILO_CP_URL` from child envs; bot token opens the worker API only while the box runs — [AGENTS.md § Processes and hops](../../AGENTS.md), [docs/Description.md](../../docs/Description.md)
- One gate (`AuthorizeAction`) for chat tools and Python calls; modes Allow/Auto/Ask/Deny per `connector.action`; each secret is its own action; three layers: operator (locked floors), Workspace guardrails, Bot rules — [AGENTS.md § The security engine and rule layers](../../AGENTS.md)
- Auto-approval by a text or decision model given the policy, the action and the human's latest message; model error/timeout = ask, never allow; an automation's prompt is never passed off as the human's — [AGENTS.md § Approvals and auto-approval](../../AGENTS.md)
- Output filter per action: pass/flag/check/withhold; outside content (search, mail, chats, every connector) checked by default; warnings delivered by the CP so the Bot's code can't drop them; fenced with random ids; `Defang` neutralizes fake filter headers; invisible Unicode tag chars revealed to the judge — [AGENTS.md § Output rules](../../AGENTS.md)
- Output filter "Not covered, by design": image pixels under Check, the live stream the human watches, channel inbound/voice memo transcripts/wake mail (not tool results), and data code stored and printed later — [AGENTS.md § Output rules](../../AGENTS.md)
- Egress gateway: every box and STDIO sidecar on its own `--internal` network; TLS decrypted and re-originated with a mirrored utls hello; blocks (lists, threat feeds, private/metadata IPs after resolve and at dial), silent flags (exfil sinks, secret shapes, entropy, 50 MB/h upload budget, beaconing); no Ask at network layer; per-minute traffic audit — [AGENTS.md § Egress gateway](../../AGENTS.md)
- Surrogate secrets: the box gets a stand-in; the gateway substitutes the real value only toward allowed hosts over verified HTTPS and swaps it back in responses; leaks elsewhere blocked — [AGENTS.md § Egress gateway](../../AGENTS.md)
- Egress "Not covered": drive sidecars, the CP's own connections, a rootful engine's `.1` bridge, TLS inside the SOCKS door, DNS-over-UDP identity; Podman only (`Enabled()` false on plain Docker, filter silently off) — [AGENTS.md § Egress gateway](../../AGENTS.md), [TODO.md §10](../../TODO.md)
- Secret maskers on worker and CP (exact + base64/URL/JSON/hex encodings) before persist and before provider calls — [docs/Description.md § Secret sanitization](../../docs/Description.md)
- Hardening of the console origin: passive-only inline downloads, sandboxed live apps without `allow-same-origin`, WebSocket origin checks, `__Host-` cookies option, OAuth callback bound to its starter — [AGENTS.md § Other people's bytes in our origin](../../AGENTS.md)
- Known ceiling: `docker inspect` can read a Bot's token and a STDIO sidecar's env; code in the box can read its own token — [AGENTS.md § Other people's bytes in our origin](../../AGENTS.md)
- Left: taint tracking across steps (OpenClaw taints the turn) — [TODO.md §5 #20](../../TODO.md)

### Inferences
- Defense in depth spans model layer (rules, approvals, output filter), network layer (egress) and credential layer (surrogates, maskers); this is unusually comprehensive for a self-hosted agent.

### Gaps
- No independent security audit referenced in the repo.

## 12. Model providers & cost/token efficiency

### Takeaway
Many providers behind a neutral layer, model allowlists per kind, aggressive prompt caching with an append-only history enforced by a test, Python-first tool exposure, context clearing and compaction, decision models for cheap typed judgments, and a Claude subscription provider via the official Claude Code binary.

### Cited Findings
- Providers: OpenRouter, OpenAI, Anthropic (native), `claude` (Claude subscription via Claude Code binary), TypeSafe (decisions), `local` (any OpenAI-compatible self-hosted server; "Silo never hosts models"), Groq, Cerebras, and speech vendors — [AGENTS.md § Configuration](../../AGENTS.md)
- Model kinds registry: text, decision, transcription, speech, embedding, each with its own allowlist — [AGENTS.md § Model kinds](../../AGENTS.md)
- System prompt in cache tiers stable → volatile (base shared by all Bots, then session, SOUL, then memories/notes after last breakpoint); history append-only, replayed byte-for-byte (`TestHistoryIsAppendOnly`); time zone frozen per message to keep the prefix stable — [AGENTS.md § The system prompt and context / Context is append-only](../../AGENTS.md)
- Clearing stale tool results before compaction; compaction with a summary that must carry the latest request verbatim; context window discovery per model — [AGENTS.md § Clearing / Compaction](../../AGENTS.md)
- ≤ 17 chat tools by default; the rest via Python; 12,000-char result cap; repeat guard on 3rd identical read/grep — [AGENTS.md § Tools are one engine / File tools](../../AGENTS.md)
- Decision models (OpenRouter Decisions API, TypeSafe System One): typed `noul`/`choice`/`score` answers with probabilities, used for auto-approval, the output filter and a `decide` tool — [AGENTS.md § Decision models](../../AGENTS.md)
- Claude Agent provider: runs the official `claude` binary with no built-in tools, its own MCP server for Silo's tools, session per conversation, Silo's rules/approvals/filter still apply — [AGENTS.md § Claude Agent](../../AGENTS.md)
- Policy warning: subscription sign-in is for the operator's own, self-started use, not several people or unattended Bots — [AGENTS.md § Claude Agent](../../AGENTS.md)
- Thinking ladder (off…max) per chat; fast mode (`:fast` suffix, premium price) that fails rather than falls back — [AGENTS.md § Thinking levels / Fast mode](../../AGENTS.md)
- Usage (incl. cache read/write) shown in the composer; context meter with manual compaction — [docs/Description.md](../../docs/Description.md), [AGENTS.md § Compaction](../../AGENTS.md)
- Runs review page: per-run tokens, tool calls, approvals, filter verdicts, background model calls — [AGENTS.md § Runs](../../AGENTS.md)
- Left: provider fallback on failure, cross-provider retry, key pools — [TODO.md §5 #16](../../TODO.md)

### Inferences
- "Token-conserving" is the product's headline claim ([docs/Description.md](../../docs/Description.md)) and is backed by enforced mechanisms (append-only test, cache tiers), not just guidance.

### Gaps
- No published token/cost benchmarks versus competitors.

## 13. Multi-agent / subagents

### Takeaway
A lead agent can spawn up to 8 background subagents (depth 1) with their own logs, optional models and a shared taskboard; results wake the lead. No Bot-to-Bot or cross-user agent communication.

### Cited Findings
- `spawn_agent` (name, goal, context, tasks, optional model; default `model_subagent`), `agent_status`/`message_agent`/`stop_agent`; depth 1; ≤ 8 concurrent per lead chat — [AGENTS.md § Subagents](../../AGENTS.md)
- Subagents run with fresh context, a trimmed tool list (no lead-only tools) and the core prompt only — [AGENTS.md § Subagents / The system prompt](../../AGENTS.md)
- Debounced wake of the lead with a `subagent_report`; taskboard shared with subagents; UI: Taskboard strip, Subagent tray, read-only Subagent page — [AGENTS.md § Subagents](../../AGENTS.md)
- Self-assessed Done; nice to have: depth > 1, fork-context — [TODO.md §5 #3](../../TODO.md)
- Not built: agent-to-agent across Bots ("Later: … Bots talking to each other") — [TODO.md §10](../../TODO.md)

### Inferences
- Multi-agent is intra-Bot parallelism, not a society of agents.

### Gaps
- No inter-Bot messaging.

## 14. Web search, page reading, browsing

### Takeaway
A keyless built-in metasearch (DuckDuckGo, Mojeek, Brave, Startpage, Wikipedia, Marginalia) with careful rate-limiting and a Chrome fingerprint, keyed Brave/Exa options, a CP-side page reader with site handlers that renders scripts in the Bot's own box, and the real headed Chromium for interaction.

### Cited Findings
- Built-in Scraper: per-site token buckets, fan-out to 2 scraped sites, RRF fusion, blocked-vs-empty distinction with notes, 5-min cache; keyed Brave Search API and Exa — [AGENTS.md § Web search](../../AGENTS.md)
- Client mimics Chrome 145 on macOS in TLS (utls), HTTP/2 and header order — [AGENTS.md § Web search](../../AGENTS.md)
- `web_read`: site handlers (GitHub, YouTube captions, arXiv, Apple Podcasts), Readability → markdown, render with Lightpanda in the Bot's own box only on evidence, PDFs/docs via the box; `max_chars`, offsets, outline, BM25 `query` passages; 1-hour shared cache; `problem` codes (blocked, login_wall, needs_render…) — [AGENTS.md § Page reader](../../AGENTS.md), [SYSTEM.md § Reading the web](../../internal/prompts/SYSTEM.md)
- Reader blocks private addresses by default and honours operator/Bot block lists — [AGENTS.md § Page reader](../../AGENTS.md)
- Not covered: `Accept: text/markdown` negotiation, podcast transcription in the reader, X/Reddit/sign-in sites (use desktop Chromium) — [AGENTS.md § Page reader](../../AGENTS.md)
- Challenges reported, never solved; Google and Bing not scraped — [AGENTS.md § Page reader / Web search](../../AGENTS.md)

### Inferences
- Search costs no API key out of the box, which lowers operating cost, but scraping is shared per CP IP and is rate-limited.

### Gaps
- No browser login vault (password manager) — [TODO.md §5 #14](../../TODO.md)

## 15. Output surfaces

### Takeaway
Versioned artifacts with previews, live HTML apps on sandboxed subdomains, public shares, a Dashboard of Bot-written widgets kept current by code probes, an owner Feed, ask cards, and HTTP tunnels to services running in the box (private session-bound or public).

### Cited Findings
- Artifacts: content-addressed pinned store, versions v1/v2…, preview thumbnails, download as file/zip, skill cards — [AGENTS.md § Artifacts](../../AGENTS.md)
- Live HTML apps at `app-<name>.<content.host>`, sandbox without `allow-same-origin` (no storage), network Ask-gated; public shares at `share-<name>…` copied to CP so they work while the Bot is stopped — [AGENTS.md § Artifacts](../../AGENTS.md)
- Dashboard: 11 widget kinds (note, stat, chart, list, table, timeline, status, checklist, progress, ideas, app), probes = approved-once Python on intervals with no model in the loop, `run`/`hand` actions, ask cards with forms, urgent asks — [AGENTS.md § Dashboard](../../AGENTS.md)
- Feed: Bot posts markdown from any run; owner quotes into chat; 500 posts kept — [AGENTS.md § Feed and quotes](../../AGENTS.md)
- Tunnels: `tunnel-<name>.<host>` reverse proxy into a box port (keep-alive, SSE, WebSocket), private tunnels bound to the Silo session (12 h grant), public Ask + manager-only; 20 per Bot — [AGENTS.md § Content host and tunnels](../../AGENTS.md)
- Left: native image/video/audio generation (connector only, e.g. fal.ai); sharing a conversation publicly — [TODO.md §5 #13, §6](../../TODO.md)

### Inferences
- The Dashboard pattern ("the model writes a widget once and code keeps it current") is a token-saving design distinct from agents that re-run the model to refresh status.

### Gaps
- No public conversation pages.

## 16. Known gaps / not-yet-built / limitations stated in the docs

### Takeaway
The repo is candid: biggest stated gaps are webhook triggers, cross-chat search, channel breadth, telephony, a credential vault/payments, push + iOS parity, plugins, installer/backup, and provider fallback; plus explicit "not covered" lists for the output filter, egress and reader.

### Cited Findings
- "Biggest gaps now": event/webhook triggers, search across past chats, channel reach (Slack, Signal, iMessage/SMS, email replies), voice output and calls [since partly built], credential vault and payments, push + iOS parity, extension/plugin story — [TODO.md TL;DR](../../TODO.md)
- Honest caveats: no phone, no payments, no password-manager sign-in; 3 chat channels; mail receive-only; no passkeys/SCIM, one OIDC provider; Docker/Podman + Postgres install; iOS lags; WhatsApp unofficial; `docker inspect` token ceiling — [TODO.md §8](../../TODO.md)
- Missing: device nodes/personal data from user's machines, agent-to-agent, scale-to-zero, group-chat participation, Bot-built connectors, `AGENTS.md`-style workspace context autoload / `@file` refs — [TODO.md §5, §6](../../TODO.md)
- Egress: Docker engine unsupported (filter silently off), no "newly registered domain" flag, operator can't set upload budget limits, SOCKS TLS uninspected — [TODO.md §10](../../TODO.md), [AGENTS.md § Egress gateway](../../AGENTS.md)
- Output filter not covered: pixels, live stream, non-tool-result messages, stored data later printed — [AGENTS.md § Output rules](../../AGENTS.md)
- Live speech/transcription sessions not recorded in run review; speech vendor protocols untested against real services — [AGENTS.md § Runs / Model kinds](../../AGENTS.md)
- Voice: no WebSocket media fallback when ICE fails, approvals-by-voice unused — [TODO.md §10](../../TODO.md)
- iOS: no push; desktop/console/admin/OIDC web-only; calls/CallKit/widgets untested on real hardware — [ios/todo_skipped.md](../../ios/todo_skipped.md)
- Prompts still say "your owner" on shared Bots; lent connection calls don't name the lender — [TODO.md §10](../../TODO.md)
- No Bot-to-Bot memory transfer ("nothing crosses between Bots yet") — [AGENTS.md § Memories and SOUL](../../AGENTS.md)

### Inferences
- Several TODO.md 10-21 gaps have closed since (voice output and calls, shared Bots, one-click retry per AGENTS.md § Runs), so the report writer should treat TODO.md gap statuses as a lower bound on progress.

### Gaps
- No public roadmap dates.

## 17. Distinctive advantages

### Takeaway
Silo's differentiator is the combination, in one self-hosted multi-user product, of an always-isolated per-Bot desktop machine the human can step into, a control plane that holds every secret, layered rules with model-judged approvals, a prompt-injection output filter on tool results, a TLS-inspecting egress gateway with surrogate secrets, and strict token-economy engineering.

### Cited Findings
- Self-assessed "only Silo" list: self-hosted multi-user with real admin UI; isolated box per Bot with keys elsewhere; step into the machine (VNC desktop, PTY console, Files) as the only mode; output filter on every tool result; decision models; workspace history with Restore + drive journal; drives (15) + Knowledge RAG with OCR; tunnels/live apps/public shares; receive-only mailbox with DKIM/SPF wake; Dashboard with probes; token-free test suite — [TODO.md §3](../../TODO.md)
- Hermes v0.22 "Bot Screen" narrows the desktop takeover claim, but Silo argues Hermes' is "a tool-level fence, not an OS one" on its default local backend — [TODO.md §3](../../TODO.md) (competitor claim from Silo's own report, not verified here)
- Python-first tool exposure ("code execution with MCP" pattern) with ≤ 17 chat tools — [AGENTS.md § Tools are one engine](../../AGENTS.md)
- Append-only, byte-for-byte replay of context enforced by a test, for provider prompt-cache hits across runs — [AGENTS.md § Context is append-only](../../AGENTS.md)
- Secrets typed into sites without passing through the model (`type_text(get_secret(...))`, desktop typing stays in the box) — [SYSTEM.md § Permissions](../../internal/prompts/SYSTEM.md), [AGENTS.md § Desktop and Chromium](../../AGENTS.md)
- Memory that won't promote a model's guess without corroboration; memory keeper runs as a narrowed job with no machine — [AGENTS.md § Memories and SOUL](../../AGENTS.md)
- Voice "talker cannot act": a fast voice model limited to delegate/stop/end, so every action stays under rules/approvals/filter — [AGENTS.md § Voice](../../AGENTS.md)
- Claude subscription usage through the official binary with zero built-in tools and three guards against foreign tools — [AGENTS.md § Claude Agent](../../AGENTS.md)
- Test discipline: feature tests drive the real API with a scripted dummy LLM and the real worker/bridge, plus a real-Podman tier; guard tests for every RPC's access — [AGENTS.md § Tests](../../AGENTS.md)

### Inferences
- Positioning: security/teams-first, self-hosted "agent with its own computer" for small organizations; weaker on consumer reach (channels, telephony, payments, mobile push, install ease).

### Gaps
- Competitor comparisons in TODO.md are Silo's own and dated 2026-10-21; they need independent verification by the other researchers.
