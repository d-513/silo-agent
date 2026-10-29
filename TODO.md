# Silo vs. the field

Status report, 2026-10-21. Replaces the 2026-10-05 report (in git history). Since then Silo shipped 76 commits on 11 working days, and the competitors shipped too: Hermes v0.22 "Threshold", OpenClaw's 2026.10 betas, Muse for Mac, and Instinct Concierge calls.

Competitor facts come from their release notes and press as of this date (links at the bottom). Anything marked † comes from a secondary source (an aggregator or a blog) and was not checked against the project's own docs.

## TL;DR

- **Since 10-05 Silo closed the checkpoint gap and moved six others forward.** #17 workspace checkpoints are done: Changes, with Restore, plus a journal of drive writes. Also new: a receive-only mailbox per Bot (#8), surrogate secrets at an egress gateway (#9, #20), live HTML apps, public shares and a Dashboard (#13), a keyless metasearch (#14), and decision models (#16). The platform also gained accounts with OIDC and 2FA, an operator rule layer, an output filter on tool results, i18n (PL/DE), dark mode, a persistent Python kernel and an append-only cached context.
- **Two of Silo's "only we have it" claims are weaker now.** Hermes v0.22 ships **Bot Screen**, so you can watch a bot's desktop and take over. Nous also sells **Hermes Business/Enterprise**† (teams, self-hosted). What still sets Silo apart is the combination: isolated per-Bot boxes, multi-user accounts, a CP that holds every key, an egress gateway, an output filter, drives, knowledge, tunnels, a mailbox, change history and a dashboard, all in one self-hosted product.
- **The field moved to where Silo is weakest:** phone calls (Instinct, Muse, OpenClaw), password-manager and payment rails (Hermes vault with 1Password/Bitwarden, Muse with Stripe Link), plugin marketplaces (Hermes ~600 reviewed plugins, ClawHub), one-click local models and signed desktop installers (Hermes), and reach over iMessage/SMS. The new "text-message agent" category has 20+ products.
- **Biggest gaps now, in order:** event/webhook triggers, search across past chats, channel reach (Slack, Signal, iMessage/SMS, email replies), voice output and calls, a credential vault and payments, push notifications plus iOS parity, and an extension/plugin story.

## 1. Snapshot

| | 10-05 | 10-21 |
|---|---|---|
| Control plane + worker + bridges (Go, non-test) | ~38k lines | ~66k lines |
| Go tests | ~17.8k lines, 585 test functions | ~38.9k lines, 1167 test functions |
| Web console (React/TS, hand-written) | ~18k lines | ~34k lines |
| iOS app (SwiftUI, hand-written) | ~7.5k lines | ~7.6k lines (no Dashboard, Changes, Mail, Tunnels or Knowledge) |
| UI API (ConnectRPC methods) | 109 | 174 |
| Connector library | 64 presets | 64 presets (2 built-in in Go: Email, Calendar) |
| Drive providers | 15 | 15 |
| Default skills | 6 | 6 |
| LLM providers | OpenRouter, OpenAI, Anthropic, `local` | + TypeSafe; decision models on OpenRouter and TypeSafe |
| Channels | Telegram, WhatsApp, Discord | same, plus inbound mail that can wake the Bot |
| Console languages | English | English, Polish, German |

## 2. The field

| | **Hermes Agent** (Nous) | **OpenClaw** | **Muse** (Meta) | **Instinct** | **Silo** |
|---|---|---|---|---|---|
| Hosting | Self-hosted MIT; Hermes Cloud (hourly, scales to zero); Business/Enterprise† | Self-hosted; cloud sessions (move local ↔ cloud) | Cloud only; Mac app since 09-17 | Hosted, invite-only, US | Self-hosted |
| Users | Profiles isolated under one gateway; teams via Business† | Paired devices, session send/receive policy | One consumer per VM | One consumer | Accounts, admin, OIDC, 2FA, per-user Bots |
| Install | Signed desktop apps (macOS, MS Store, Linux), first-run chat with no API key, one-click llama.cpp | Gateway + Tauri/Windows desktop apps, `doctor`, encrypted backups | App store | Invite | Docker/Podman + Postgres, operator config |
| Where tools run | Seven backends (local, Docker, SSH, Modal, Daytona, Singularity, Vercel) | Host by default; Docker sandbox opt-in; cloud workers | Dedicated "Secure VM" per user | Hosted | One container per Bot, always |
| Human takeover | **Bot Screen**: live desktop and takeover (v0.22) | – | – | – | VNC desktop, PTY console, Files |
| Channels | 20+ (Telegram, Slack, Signal, Matrix, WhatsApp, Feishu, …) | ~25 incl. a new X (Twitter) channel | App, web, WhatsApp | iMessage, WhatsApp, email, **phone** | Telegram, WhatsApp, Discord (+ mail in) |
| Voice | Live transcription, voice-chat model | **Voice calls**: briefs, reports, live steering, voicemail detection; Discord realtime voice | Calls (testing) | **Concierge phone calls** | Speech to text only |
| Credentials, payments | **Password-blind vault**: 1Password, Bitwarden or local; TOTP from a saved key | Secret masking (chunk-boundary aware) | **Stripe Link**; Shop Pay and 1Password "soon" | Acts in your name | Surrogate secrets swapped at the egress gateway; secret typing without the model seeing it |
| Extensibility | ~600 reviewed plugins, plugin SDK, plugin OAuth, MCP | ClawHub skills and plugins, hooks | Connector submission; Muse builds custom connectors | – | MCP (HTTP/SSE/STDIO), skills, Go built-ins |
| Security story | Command approval, container backends, profile isolation | Optional sandbox, **turn tainting** from network results, hooks cannot change approved args | Sentinel gates all egress and connector actions; `authd` surrogates | Broad terms; trains on data by default | One gate, per-action and operator-locked rules, auto-approval (text or decision model), output filter, egress gateway with surrogates, keys never in the box |

The "text-message agent" crowd (TechCrunch, 10-10) is the consumer pressure: Wajo has its own email, phone number **and payment card**; Folk runs on a private cloud computer for $8/mo; Tomo and Wajo join **group chats**; Martin covers SMS, phone, WhatsApp, email and Slack; Iris is built on Hermes.

## 3. What only Silo has (re-checked)

1. **Self-hosted, multi-user, with a real admin UI.** Accounts, invites, TOTP, OIDC, sessions, an operator rule floor, audit timeline, LLM log, silo.yaml editor. *Weaker than before:* Hermes Enterprise† targets the same buyer. Its public docs show profiles under one gateway, not accounts with an admin.
2. **An isolated box per Bot, keys elsewhere.** The CP holds provider keys, OAuth and secrets. Box egress goes through a gateway that swaps surrogates only for allowed hosts. Only Muse (Sentinel + `authd`) is comparable, and it is cloud-only.
3. **Step into the machine, isolated.** Hermes Bot Screen (v0.22) now does live view and takeover: Xvnc + Xfce per profile, the RFB stream spliced through the gateway, and a "control lease" that refuses the bot's `computer_use`/browser tools while the human drives. By its own docs it is "a tool-level fence, not an OS one" and "screens are work surfaces, not security boundaries". On the default `local` backend every bot shares the host's OS user, files, network, X socket and Chromium DevTools port. In a Docker backend it needs the `hermes-sandbox:desktop` image, and Modal, Daytona and Vercel cannot host it. It is Linux only and off by default. Silo's version is the only mode: one box per Bot, plus the PTY console and the file browser, with no published ports.
4. **Output filter on every tool result.** Pass, flag, check (a model judges for prompt injection) or withhold, per action, with the warning delivered by the CP so code cannot drop it. OpenClaw taints a turn after network results; nobody else judges the content.
5. **Decision models.** Typed yes/no, choice and score answers (OpenRouter Decisions, TypeSafe) used for auto-approval, filtering and a `decide` tool. Nobody else wires them.
6. **Workspace history with Restore, plus a drive journal.** State-based snapshots catch what a script did, not just file tools.
7. **Drives (15 providers) + Knowledge RAG with OCR.** A FUSE sidecar holds the credentials. Hybrid search and rename detection.
8. **Tunnels, live apps and public shares.** A Bot serves a real web app on a private (session-bound) or public address.
9. **A receive-only mailbox per Bot**, with DKIM/SPF verification and wake-on-verified-sender. Instinct and Wajo give an agent an email address, but they are hosted and can send.
10. **Dashboard.** Widgets the Bot writes once and code keeps current (probes approved once), plus asks. Hermes and OpenClaw have nothing like it. OpenClaw Workboards are closer to Silo's taskboard.
11. **Test suite without tokens.** Dummy LLM and decider, the real worker, the real bridge, a schema per test, and a real-Podman tier.

## 4. Where we matched them

| Area | Silo |
|---|---|
| Cron and proactive runs | Automations, Heartbeat, Dashboard automation, Run now, mail wake |
| Subagents | spawn/status/message/stop, taskboard, push wake, per-agent model |
| Context | Append-only cached prompt, clearing of stale results, auto/manual compaction |
| Memory | pgvector memories, all in prompt or a digest, collector, Memories page |
| Persistent code state | A Python kernel per conversation (OpenClaw Code Mode `store`/`load` is the equivalent) |
| Skills | Agent Skills format, GitHub/zip install, library + personal |
| MCP | HTTP, legacy SSE, STDIO; connectors optionally exposed as chat tools; several accounts per preset (Hermes v0.22 just added this) |
| Models | Four text providers + local, decision models, thinking ladder, per-chat model, window discovery |
| Web | Keyless metasearch with a Chrome fingerprint, Lightpanda reader, real Chromium |
| i18n | EN/PL/DE (Hermes Desktop: FR/DE/ES) |

## 5. Gap report, re-scored

**Done** / **Partial** / **Missing** / **Skip**. The arrow shows the change since 10-05.

| # | Gap | Status | What shipped, what is left |
|---|---|---|---|
| 1 | Scheduling, proactivity | **Partial** | New: mail wake (a real event trigger, verified senders only); Dashboard probes are scheduled code with no model in the loop. Left: **webhook triggers** (Hermes mirrors webhook deliveries into chat), one-shot "at" schedules, heartbeat active hours, per-job delivery target |
| 2 | Voice and telephony | **Partial, gap widened** | Only speech to text. Instinct, Muse and OpenClaw now **place calls**. OpenClaw's calls are a bundled plugin over a telephony API (Twilio Media Streams, Telnyx, Plivo; it needs a public webhook URL), with full-duplex realtime voice and briefs, reports, live steering and voicemail detection added on top. Instinct's and Muse's vendors are not public. Calls are live, so they belong in Silo natively rather than as a connector or channel; **deferred for now**. Left: TTS, spoken replies, voice channels, calls |
| 3 | Subagents | **Done** | Nice to have: depth > 1, fork-context |
| 4 | Memory and retrieval | **Partial** | New: memory digest, all memories in prompt. Left: **search across past chats** (Hermes `session_search` has date bounds), external memory providers (Hermes moved Honcho/Mem0 to plugins), import of transcripts |
| 5 | Self-improving skills | **Partial** | Unchanged: the human clicks Save skill |
| 6 | Session lifecycle | **Done** | Left: one-click retry |
| 7 | Channel breadth | **Partial** | Unchanged at 3 chat channels; mail is inbound only. Left: Slack, Signal, Matrix, iMessage/SMS (the consumer market lives there), Teams, X, **email replies**, group-chat participation |
| 8 | Agent-owned identity | **Missing → Partial** | New: a mailbox per Bot. Left: sending mail, a phone number (Wajo and Instinct have both) |
| 9 | Payments and credential rails | **Missing → Partial, but behind** | New: surrogate secrets at the gateway. Left: a password-manager vault for browser sign-in (Hermes: 1Password, Bitwarden or its own vault, TOTP from a saved key, model never sees the secret), a payment rail (Muse uses Stripe Link) |
| 10 | Agent-to-agent, multiplayer | **Missing** | Nothing across Bots or users; no shared Bot between teammates |
| 11 | Device nodes and personal data | **Missing** | OpenClaw nodes run as a background service on joined devices; Muse for Mac reads Files, Mail, Messages and Calendar |
| 12 | Notifications | **Partial** | Unchanged: Feed badge, channel messages. No APNs or web push |
| 13 | Media, interactive artifacts | **Partial → mostly done** | New: versioned artifacts, live HTML apps, public shares, app widgets on the Dashboard. Left: native image/video/audio generation (connector only) |
| 14 | Web extract, browser | **Partial** | New: Built-in Scraper. Left: native page extract, a browser login vault (see #9) |
| 15 | Hooks, plugins, webhooks | **Missing, gap widened** | Hermes ships a plugin SDK, ~600 reviewed plugins and plugin OAuth; OpenClaw has ClawHub and hooks. Silo has MCP, skills and in-tree Go only |
| 16 | Provider routing, fallback | **Partial** | New: decision models, TypeSafe, List models. Left: fallback on failure, transient-error retry across providers, key pools, an OpenAI-compatible API endpoint |
| 17 | Checkpoints and rollback | **Missing → Done** | Changes pane, Restore of a file or a whole change, drive journal |
| 18 | Workspace context files, `@` refs | **Partial** | Unchanged: no `AGENTS.md` autoload, no `@file` |
| 19 | Personality presets | **Skip** | |
| 20 | Permission authority, surrogation | **Done** | + operator rule layer, output filter, audit timeline. Left: taint tracking across steps (OpenClaw now taints the turn) |

## 6. New gaps the research turned up

- **Install and onboarding.** Hermes is a signed download with a first-run chat that needs no API key and has one-click local models. Silo needs Docker/Podman, Postgres and a config file. A one-command installer (or compose bundle) and a first-run wizard would matter more than any single feature for adoption.
- **Backups.** OpenClaw verifies SQLite backups and restores, with client-side encryption to NAS or R2. Silo has no documented backup and restore of Postgres plus `data/`.
- **iOS parity.** The web console roughly doubled since 10-05; the iOS app did not. Dashboard, Changes, Mail, Tunnels and Knowledge have no iOS screen, and there is no push.
- **Sharing a conversation.** OpenClaw has public conversation pages. Silo shares artifacts, not chats.
- **Scale-to-zero hosting.** Hermes Cloud bills hourly and sleeps idle agents. A stopped Silo box costs nothing, but nothing stops idle boxes automatically.
- **Group chats.** Tomo and Wajo act as a participant in a group. Silo's channel `reply_mode` covers mentions, not a multi-human thread with roles.
- **Muse builds connectors for the user.** A Bot writing and attaching its own MCP server (with approval) is a natural next step from STDIO sidecars.

## 7. Next up

Ordered by leverage, with the existing seams in brackets.

1. **Webhook triggers and one-shot schedules** (automation engine, `RunAutomation`; tunnels already route inbound HTTP to a Bot).
2. **Search across past chats** (run events, pgvector, the Knowledge hybrid search SQL).
3. **Credential vault for browser sign-in** (surrogates + `type_text`; add a 1Password/Bitwarden connector and TOTP from a stored secret).
4. **Push notifications + iOS catch-up** (Feed unread, Dashboard asks, approvals waiting).
5. **Email replies as a channel** (the mailbox is the inbound half; sending needs SPF/DKIM for `mail.domain`).
6. **Installer and backup story** (compose bundle, `make backup`/`restore`).
7. **TTS, then calls** (`models` path shared with transcription; calls native when they come, deferred for now).
8. **Provider fallback** (provider registry is neutral).
9. **Slack and Signal channels** (the adapter contract carries QR, picker and `Remover`).

## 8. Show and tell

Demo order, playing to what is still unique:

1. **Create a Bot** as a second user; show the admin's operator rules apply to it.
2. **The gate + filter:** a web result with a planted injection is flagged in the thread, and the Bot is told by the CP.
3. **Egress:** the Bot uses a surrogate GitHub token; show the box never had the real one.
4. **Step in:** Desktop, Console, Files on the same box.
5. **Changes:** the Bot rewrites files from a script; open Changes, Restore one.
6. **Drives + Knowledge:** a cited answer from a scanned PDF on OneDrive.
7. **Tunnels + Dashboard:** the Bot builds a small app, pins it as a widget, keeps a stat current with a probe.
8. **Mail:** sign the Bot up for something; the confirmation lands in its mailbox and wakes it.
9. **Subagents + automations:** a split research task; a daily digest in the Feed.

Honest caveats:

- No phone, no TTS, no payments, no password-manager sign-in. Competitors demo all four.
- Three chat channels against Hermes' 20+ and OpenClaw's ~25; nothing on iMessage or SMS.
- Mail is receive-only; Silo sends no mail (no email password reset either).
- No passkeys, no SCIM; one OIDC provider.
- Install needs Docker/Podman and Postgres; there is no desktop installer.
- iOS lags the web: no Desktop, Console, Dashboard, Changes, Mail, Tunnels, Knowledge or Admin, and no push.
- WhatsApp is the unofficial linked-device protocol; Telegram bots and WhatsApp cannot read history.
- `docker inspect` can still read a Bot's token and a STDIO sidecar's environment (documented v1 ceiling).

## 9. Muse lessons, shipped 2026-10-21

- **Slash commands** on every surface: `/clear` (context boundary, nothing deleted), `/wipe [last|all]` (deletes for good, asks first, memories kept), `/compact`, `/model`, `/thinking`, `/stop`, `/new`, `/rename`, `/remember`, `/onboard`, `/help`. Threads load a page at a time.
- **Main chat** per Bot: the landing page; Feed, Dashboard and Mail replies land there unless asked for a new chat.
- **Cards in the chat** (`ask_owner`): questions answered in place, plus **setup offers** that open Silo's own connector, channel and drive forms prefilled.
- **Interview** on a new Bot (opt-out), **proactivity dial** (off / quiet / active), **ideas** on the Dashboard, **live activity** under the Bot's name.
- **Designed documents**: themes, PDF templates and a render-and-look step in the document skills; Inter, EB Garamond and Libertine in the image.
- Still open from that plan: show queued messages as queued; make the Audit page a friendlier timeline; iOS does not know commands, cards or the main chat yet; binding an owner's DM channel to the main chat (schema-ready, not built).

## Sources

- Hermes Agent: [GitHub releases](https://github.com/NousResearch/hermes-agent/releases), [v0.22.0 announcement](https://x.com/NousResearch/status/2110385143643091204), [changelog aggregator](https://www.gradually.ai/en/changelogs/hermes-agent/), [Hermes Cloud](https://portal.nousresearch.com/cloud), [teams and private infrastructure†](https://alphasignal.ai/news/nous-research-packages-hermes-agent-for-teams-and-private-infrastructure)
- OpenClaw: [GitHub releases](https://github.com/openclaw/openclaw/releases), [changelog aggregator†](https://www.gradually.ai/en/changelogs/openclaw/), [NemoClaw release notes](https://docs.nvidia.com/nemoclaw/user-guide/openclaw/release-notes/2026/10/20)
- Muse: [TechCrunch launch](https://techcrunch.com/2026/09/08/meta-debuts-its-muse-ai-agent-will-consumers-trust-it/), [Muse guide†](https://codersera.com/blog/meta-muse-ai-agent-app-guide-2026/), [CNBC on adoption](https://www.cnbc.com/2026/10/07/meta-muse-personal-ai-agents-dazzle.html), [TIME on privacy](https://time.com/article/2026/10/06/meta-muse-ai-agent-privacy/), [Analytics India Mag on Sentinel](https://analyticsindiamag.com/ai-news/meta-launches-muse-ai-agent-with-internet-access-guardrails)
- Instinct: [TechCrunch on calls](https://techcrunch.com/2026/09/17/rival-ai-agents-instinct-and-metas-muse-both-add-the-ability-to-make-calls/), [Fox News](https://www.foxnews.com/tech/ai-agents-make-phone-calls)
- Text-message agents: [TechCrunch roundup, 10-10](https://techcrunch.com/2026/10/10/all-the-ai-agents-that-can-live-in-your-text-messages/)

## 10. Open from the build plans

The egress, voice and workspaces handoff docs were removed once built (they are in git history). What they planned and the code does not have yet:

**Egress gateway**
- Docker engine support: `egress.Service.Enabled()` is false on Docker, so the filter is silently off there, and Admin shows only "Off" with no reason.
- "Newly registered domain" flags; only `random_domain` exists.
- The CP never sends `EgressApply.limits` (upload budget, query minimum), so the gateway always uses its defaults and an operator cannot set them.
- A rootful engine's client bridge `.1` is reachable from a box (documented, not mitigated).
- The SOCKS door splices TLS without inspecting it; DNS over UDP identifies the client by source address only.

**Voice**
- Phase 0 measurements (talker time to first token, TTS/STT latency, echo cancellation across browsers) are not written down.
- Turn end is 550 ms of VAD silence (`redemptionMs`); no Smart Turn, no CP-side VAD.
- No WebSocket media fallback when ICE finds no route.
- Approvals by voice: `voice.approve_by_voice` is parsed but unused. `ask_owner` cards are not read out or answerable by voice.
- No streaming STT (`llm.StreamTranscriber`, live captions, speculative talker start), no streaming TTS (`SpeechSession`) or word timestamps.
- No Admin P50/P95 panel over `voice_turns`, no 30-day sweep of `voice_turns`/`voice_calls`.
- Late Bot results get no "About <task>: " prefix; no earcon; `Meta.unheard` is not drawn faint.
- `TestLiveTalker`, the delegation eval, the live latency probe and the replay suite do not exist.
- No per-layer fallback chain, STT watchdog, or reconnect that resumes the same `call_id`.
- Later: iOS calls, channel voice notes, a phone line, a wake word, a realtime speech-to-speech talker.

**Workspaces**
- iOS has no Workspace UI (it runs on the RPC shims).
- A call through a lent connection does not name the lender ("GitHub · as Alice" in the thread, audit and `security.Describe`).
- `desktop_pauses` has no user, so the refusal cannot say who holds the desktop.
- Prompts still say "your owner" (`SYSTEM.md`, `MAIL.md`, `ONBOARDING.md`) on shared Bots.
- `Bot.activity` is not filtered per viewer: a "Can use" person sees the titles and steps of other people's live chats (planned: "Working in another chat").
- The drive refresh-token race between two sidecars (Box) is untested per OAuth template; no `single_mount`; no UI warning that two Bots on one drive is last-writer-wins.
- Missing tests: `TestUngrantedIsAbsent`, `TestRevokePrunesOnlyThatBot`, `TestBotWithNoEnabledManagerSuspends` (only the unit `TestSuspendedIsNoEnabledManager`), and the two-person case of `TestHistoryIsAppendOnly`.
- The New Bot page still says "change it later in Customize" (`web/src/NewBotPage.tsx`, and its pl/de entries); Customize is gone.
- Later: OPSEC levels as named sets of Workspace floors, checked at the grant; Bots talking to each other.
