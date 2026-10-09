# Silo vs. the field

Status report, 2026-10-05. Replaces the earlier gap list (still in git history); that list was written when Silo had no scheduler, no subagents, no compaction and no memory search, and most of it has since shipped.

Competitor facts come from their public docs and press as of this date (links at the bottom). Anything marked † is carried over from the earlier notes or comes from a secondary source, and was not checked against the project's own docs.

## TL;DR

- **Of the 20 gaps in the old list: 2 are done, 11 are partly done, 6 are still open, 1 we chose to skip.** The #1 gap (scheduling and proactivity) now has cron automations, a Heartbeat, a feed and "run now". Subagents and compaction, #3 and #6, are done.
- **Silo's edge is the architecture, not the feature count.** It is the only one of the four that is self-hosted, multi-user, isolated by default, and lets a human step into the agent's real desktop. Provider keys, OAuth tokens and secrets never enter the box.
- **What is still behind:** reach (Telegram, WhatsApp and Discord against Hermes' 20+ and OpenClaw's ~24 channels), voice output and phone calls, event/webhook triggers, push notifications, and an egress firewall like Muse's Sentinel.
- **Built in about a month:** 178 commits across 17 working days since 2026-09-05.

## 1. Snapshot

| | |
|---|---|
| Control plane + worker + bridges (Go, non-test) | ~38k lines |
| Go tests | ~17.8k lines, 585 test functions |
| Web console (React/TS) | ~18k lines |
| iOS app (SwiftUI) | ~7.5k lines, most pages at parity with web |
| UI API | 109 ConnectRPC methods, one `.proto` |
| Connector library | 64 presets (HTTP MCP, 6 STDIO MCP, 2 built-in in Go: Email, Calendar) |
| Drive providers | 15 (S3, B2, R2, GDrive, OneDrive, Dropbox, Box, pCloud, Nextcloud, ownCloud, Seafile, SFTP, SMB, WebDAV, S3-compatible) |
| Default skills | 6 (pdf, word, spreadsheets, presentations, images, product-self-knowledge) |
| LLM providers | OpenRouter, OpenAI, Anthropic (native), `local` (any self-hosted OpenAI-compatible server) |
| Channels | Telegram (MTProto), WhatsApp (linked device), Discord |

## 2. The field

| | **Hermes Agent** (Nous Research) | **OpenClaw** | **Muse** (Meta) | **Instinct** | **Silo** |
|---|---|---|---|---|---|
| Hosting | Self-hosted, MIT | Self-hosted, open source | Cloud only, Meta infrastructure | Hosted, invite-only US beta | Self-hosted |
| Users | Multi-user not documented | Multi-user not documented | One consumer per VM | One consumer per agent | Accounts, admin, per-user Bots and skills |
| Where tools run | Seven backends: local, Docker, SSH, Modal, Daytona, Singularity, Vercel Sandbox | On the host for the main session; per-session Docker sandbox is opt-in | Dedicated "Secure VM" per user | Hosted | One container per Bot, always |
| Interface | TUI + messaging gateway | Local gateway to chat apps | iOS/Android apps, web, WhatsApp | iMessage, WhatsApp, calls, web settings | Web console + native iOS app, Telegram, WhatsApp, Discord |
| Channels | 20+ platforms through one gateway | ~24 (WhatsApp, Slack, Discord, Signal, iMessage, Teams, Matrix, LINE, WeChat, …) | App, web, WhatsApp | iMessage, WhatsApp, phone | Telegram, WhatsApp, Discord |
| Security story | Command approval, container backends | Optional sandbox; exec reviewer and DM pairing† | Sentinel: separate agent as sole authority for connector actions and network egress; `authd` swaps surrogate tokens for real credentials at the network edge | Terms let the agent "bind you as if you had signed"; trains on your data by default (opt-out) | One authorization gate, per-action rules, approvals, masker, keys never in the box |

## 3. What only Silo has

"Only" means none of the other three documents it publicly. Items 1 to 8 are the ones to lead with.

1. **Self-hosted, multi-user, and with a real UI.** Accounts, an admin role, per-user Bots, per-user skills, an operator settings page with a YAML editor, an audit log and an LLM log. The others are single-user tools or single-tenant hosted products. A team can run one Silo.
2. **A person can step into the agent's machine.** Every Bot is a Linux box with an X11 desktop, a PTY console and a file browser, all reachable from the browser (VNC and console tunnelled over the worker's own outbound stream, no published ports, no `docker exec`). The agent drives it with `look` / `click` / `type` / `key` / `scroll` and hands over for logins, captchas and 2FA. The human sees the same screen.
3. **Isolation is the only mode, and the keys live somewhere else.** The Control Plane owns provider keys, connector tokens, OAuth and secrets; the Bot box has none of them and cannot reach the CP except through the worker's stream. Python never sees the CP URL or the bot token. A masker runs on both sides of the wire, covering base64, URL, JSON and hex encodings. In Hermes and OpenClaw isolation is a backend or sandbox setting you choose; OpenClaw's main session runs on the host by default. Muse's Sentinel is the one comparable design.
4. **One gate for every action, down to a single secret.** Chat tools and Python calls share `authorizeAction`: Bot rule, then connector default, then catalog default, then ask. Each stored secret is its own action. Allow / Ask / Deny per Bot per connector action, an approval slip that never shows arguments (typed text is redacted), and a plain-language auto-approval policy judged by a model that may only answer `approve` / `ask` / `deny`.
5. **STDIO MCP servers in their own sidecars.** Each runs in an isolated container whose bridge dials the CP, with a per-attachment token and no published port. Authorization, masking and audit are identical to HTTP MCP. OAuth tokens (CIMD, pre-registered client, or DCR) never enter the Bot. The same preset can be attached several times, each with its own OAuth.
6. **Drives.** 15 cloud and network storage providers mounted into the Bot's workspace by a FUSE sidecar that holds every credential. The Bot sees files, never keys. Drives work with Knowledge, so a Bot can search a OneDrive folder it cannot authenticate to.
7. **Knowledge: RAG over any workspace folder.** Hybrid cosine + keyword search fused in one SQL, OCR for scanned PDFs and images (tesseract, English and Polish), rename detection so moved files keep their chunks, an index that never deletes on a disconnected drive. Files stay on the box; only chunks and vectors are in Postgres.
8. **Tunnels.** Named HTTPS addresses (`<adjective>-<colour>-<animal>.<host>`) for any port on the Bot's localhost, carried over the worker's outbound stream. Private tunnels hand off the owner's session and end when it ends. Making one public is an Ask. This is also the answer to "interactive artifacts": the Bot can build and serve a real web app.
9. **The Feed.** A read-only inbox per Bot with an unread badge, written by chats, automations and channels. Quote turns a post into a new chat.
10. **Run logs are first-class.** Automations and subagents each get a hidden log that renders through the same `Thread` as chat. The subagent tray and per-agent pages are the same pattern.
11. **A test suite that runs the real thing without spending tokens.** A scripted `DummyLLM`, the real worker as a subprocess, the real STDIO bridge, a per-test Postgres schema, plus a real-Podman tier that boots the actual Bot image. TESTING.md documents it.

## 4. Where we matched them

| Area | Silo |
|---|---|
| Cron and proactive runs | Automations (5-field cron, fresh context per run, once-only claim, no backfill), Heartbeat, Bot-created automations (Ask) |
| Subagents | `spawn_agent`, status, message, stop; taskboard; push-based wake of the lead; per-agent model; 8 at once |
| Context compaction | Auto and manual, mid-run, persisted as events, never deletes history; context meter |
| Memory | Core memory + pgvector long-term memories, auto-recall, a collector that saves what the Bot forgot, a Memories page |
| Skills | Agent Skills format, GitHub or zip install, progressive disclosure, library + personal |
| MCP | HTTP, legacy SSE and STDIO |
| Models | OpenRouter / OpenAI / Anthropic native / local, thinking ladder, per-chat model, window discovery, prompt-cache breakpoints |
| Speech to text | Composer mic, `transcribe` tool, Python helper |
| Branching | Edit message, diverge chat, delete, stop |

## 5. Gap report: the original 20, re-scored

Status: **Done** / **Partial** / **Missing** / **Skip** (chosen not to do).

| # | Gap | Status | What shipped, what is left |
|---|---|---|---|
| 1 | Scheduling, proactivity, autonomy | **Partial** | Shipped: automations, Heartbeat, Run now, Feed, UI and iOS. Left: webhook and event triggers, one-shot ("at") schedules, skill-attached jobs, script-only watchdogs, heartbeat active hours, declared per-job delivery target |
| 2 | Voice and telephony | **Partial** | Shipped: speech to text. Left: text to speech, spoken replies, wake word, voice channels, phone calls |
| 3 | Subagents | **Done** | Shipped as above. Left (nice to have): nesting beyond depth 1, fork-context mode, background reviewer |
| 4 | Deep memory and retrieval | **Partial** | Shipped: memories, auto-recall, collector, Knowledge RAG. Left: search over all past conversations (`chats` reads one chat), external memory providers, a write-approval gate on `remember` |
| 5 | Self-improving skills | **Partial** | Shipped: the Bot can write a skill directory and offer it through `artifact`, and the human clicks **Save skill**. Left: autonomous skill creation after a hard task, skills that patch themselves |
| 6 | Compaction and session lifecycle | **Done** | Shipped: compaction, edit/branch/delete, stop, steering by injection, run cap now `runs.max_duration` (120 min). Left: one-click retry, undo (see 17) |
| 7 | Channel breadth | **Partial** | Telegram (MTProto bot), WhatsApp (linked device paired by QR, unofficial protocol) and Discord (bot on the gateway). Left: Slack, Signal, iMessage, Matrix, Teams, and Email as a channel (it is a connector today) |
| 8 | Agent-owned identity | **Missing** | A Bot can use a mailbox you give it (Email connector); nothing provisions an address or number for it |
| 9 | Payments and credential rails | **Missing** | Secrets are per-name, Ask-gated and typed via `silo_runtime.type_text`; no virtual cards, no password-manager integration |
| 10 | Agent-to-agent and multiplayer | **Missing** | Subagents inside one Bot work; nothing across users or Bots |
| 11 | Native nodes and personal data | **Missing** | The iOS app is a client of the Control Plane, not a device node (no camera, location, contacts) |
| 12 | Notifications | **Partial** | In-app Feed badge, and the Bot can message you on Telegram, WhatsApp or Discord. No APNs or web push; iOS polls |
| 13 | Media generation, interactive artifacts | **Partial** | The fal.ai connector preset and the images/pdf/word/spreadsheets/presentations skills cover a lot; Tunnels let a Bot serve a live app. No native image, video or music tool |
| 14 | Web extract and browser backends | **Partial** | Lightpanda (auto-attached markdown reader), Exa / Tavily / Firecrawl / Perplexity presets, real Chromium with Playwright. One native search engine (DuckDuckGo scraper); native page extract still "later" |
| 15 | Hooks, plugins, webhooks | **Missing** | Extensibility today is MCP, skills and in-tree Go connectors |
| 16 | Provider routing and fallback | **Partial** | Shipped: four provider kinds including `local`, per-Bot and per-chat model, subagent/title/memory models, OpenRouter `ignore`. Left: fallback on failure, key pools, routing policy, an OpenAI-compatible API endpoint, IDE integration |
| 17 | Checkpoints and rollback | **Missing** | Workspace is a durable bind mount, but nothing snapshots before `write` / `patch` / `delete` |
| 18 | Workspace context files and `@` references | **Partial** | Knowledge folders and composer attachments cover "know my files". No `AGENTS.md` / `CLAUDE.md` autoload, no `@file` expansion |
| 19 | Personality presets and skins | **Skip** | Per-Bot SOUL is editable and enough |
| 20 | Independent permission authority, credential surrogation | **Partial** | The gate, masker, auto-approval policy and audit exist. No egress allowlist, no surrogate tokens, no untrusted-input labelling |

## 6. New gaps the research turned up

- **Execution backends.** Hermes runs on seven (local, Docker, SSH, Singularity, Modal, Daytona, Vercel Sandbox), with serverless hibernation. Silo is Docker/Podman only, on any host the Docker API reaches.
- **Network egress control.** Muse routes all egress through Sentinel and tracks tainted data flow. Silo has no firewall on the Bot box.
- **Unprompted contact.** Instinct texts and calls first. Silo can post to the Feed or a channel from a heartbeat, but only when a heartbeat is scheduled.
- **Batch processing.** Hermes runs one prompt over hundreds of inputs. Silo has no batch mode.
- **OpenClaw's skill library.** 100+ preconfigured skills† against our 6.
- **Facts that changed in the old list.** OpenClaw lists about two dozen channels, not 29. The "runs cap at 10 min", "no compaction", "no steer", "no automation" and "no subagents" lines were already wrong for Silo.

## 7. Next up

Ordered by leverage, with the engine seams that already exist in brackets.

1. **Webhook and event triggers, plus one-shot schedules** (finishes #1; the automation engine and `RunAutomation` are there).
2. **Email as a channel** (reach; WhatsApp and Discord are in, and the adapter contract carries QR, picker and `Remover` for the next one).
3. **Search across all past chats** (finishes #4; run events and pgvector are there).
4. **Push notifications to the iOS app** (finishes #12; the app and the Feed unread count exist).
5. **Text to speech** (finishes #2; speech to text already shares the `models` path).
6. **Workspace checkpoints** before mutating file tools (#17).
7. **Provider fallback** (#16; the provider registry is neutral already).
8. **An egress allowlist on the Bot box**, the cheap half of Sentinel (#20).

## 8. Show and tell

A demo order that plays to the advantages, and what to say is unfinished.

1. **Create a Bot** and watch it come online (crest, lamp, container).
2. **Step in:** open Desktop, let the Bot drive Chromium, take over for a login, hand back.
3. **The gate:** a Bot asks for a secret; the approval slip names it and never shows the value. Flip a rule from Ask to Allow in Rules.
4. **Connectors:** attach GitHub through OAuth; attach Email; show that the Bot has `import tools` and no token.
5. **Drives and Knowledge:** mount a cloud drive, index a folder with scanned PDFs, ask a question, get a cited answer.
6. **Tunnels:** the Bot builds a small web app; open its private address, then make it public and watch the Ask.
7. **Subagents:** a research task that splits into three, with the taskboard and tray.
8. **Automations and Feed:** a daily digest lands in the Feed; Quote it into a chat.
9. **Telegram and iOS:** the same Bot from a phone chat and from the native app.

Honest caveats to say out loud:

- Auth is session cookies with invite links, TOTP two-factor and one OIDC provider. No passkeys, no email-based reset (Silo sends no mail), no SCIM.
- The Docker host can be remote at the protocol level, but there is no multi-host UI.
- Telegram bot accounts and WhatsApp linked devices cannot read history, so `chats` falls back to the local log.
- WhatsApp runs on the unofficial linked-device protocol: use a spare number, and expect WhatsApp to be free to restrict automated accounts. Inbound media arrives as a placeholder, not a file.
- `docker inspect` can still read the Bot's token and a STDIO sidecar's injected environment (the documented v1 ceiling).
- The iOS app has no Desktop, Console, channel setup wizard, connector OAuth, Admin, Knowledge or Tunnels (`ios/todo_skipped.md`).
- A box made before Drives needs a Container reset to get the mount.

## Sources

- Hermes Agent: [GitHub](https://github.com/nousresearch/hermes-agent), [features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- OpenClaw: [DigitalOcean overview](https://www.digitalocean.com/resources/articles/what-is-openclaw), [OpenReplay](https://blog.openreplay.com/openclaw-open-source-ai-assistant/), [DEV Community](https://dev.to/aws-builders/what-is-openclaw-a-self-hosted-ai-assistants-311j)
- Muse: [Meta announcement](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/), [Vellum breakdown](https://www.vellum.ai/blog/official-muse-breakdown), [Yahoo Tech on Sentinel](https://tech.yahoo.com/ai/meta-ai/articles/meta-muse-agent-lives-behind-094818903.html)
- Instinct: [terms and capabilities](https://clawdocx.com/ai-agents/instinct), [Fox News on phone calls](https://www.foxnews.com/tech/ai-agents-make-phone-calls)
