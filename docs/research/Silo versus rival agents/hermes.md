# Hermes Agent (Nous Research): capability inventory, November 2026

Scope note: "Hermes" in the AI space also names Nous Research's own **Hermes LLM family** (Hermes 4 runs on Nous's inference backend, [dev.to, Aug 2026](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)). Hermes LLM is the model and Hermes Agent is the agent harness. I found no other "Hermes" agent more prominent than NousResearch/hermes-agent by late 2026. There are also unrelated lookalikes, for example a community Android app also called "Hermes Mobile" ([F-Droid](https://f-droid.org/packages/com.m57.hermescontrol/)).

Snapshot: the latest stable release is **v0.23.2 (Nov 16, 2026)**, and canary builds ship nightly ([GitHub releases](https://github.com/NousResearch/hermes-agent/releases)). The repo has 264.2k stars and 59.6k forks ([GitHub](https://github.com/NousResearch/hermes-agent)).

## 1. Deployment & hosting

### Takeaway
Hermes Agent is a self-hosted, single-operator Python agent that runs on a laptop, VPS, Raspberry Pi or Android/Termux. It has six built-in terminal backends: local, Docker, SSH, Modal, Daytona and Vercel Sandbox. Nous also sells a hosted version, Hermes Cloud: one dedicated instance per agent, billed hourly, in preview as of Aug 2026.

### Cited Findings
- Install is a curl one-liner for Linux, macOS and WSL2, or `install.ps1` for native Windows. The installer handles Python 3.14, Node.js, npm, pnpm, ripgrep and FFmpeg. Android/Termux gets a signed APT repo with stable and canary channels, and the repo includes Docker and docker-compose files — [GitHub README](https://github.com/NousResearch/hermes-agent)
- Termux on Android is documented as "best effort only" — [aiagentstore, Jul 2026](https://aiagentstore.ai/agentic-ai-and-workflow-automation/en/the-20-biggest-problems-with-hermes-agent-what-thousands-of-reddit-and-x-users-are-actually-struggling-with-ranked)
- Typical hosts are a laptop, VPS, Mac Mini or Raspberry Pi. `hermes daemon start` runs it as a background service — [Hermes Atlas guide, 2026-11-16](https://hermesatlas.com/guide/)
- The six built-in terminal backends are local, Docker, SSH, Modal, Daytona and Vercel Sandbox. Plugin backends add Singularity/Apptainer and Sprites. Daytona and Modal offer serverless persistence that hibernates when idle — [GitHub README](https://github.com/NousResearch/hermes-agent); [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- One gateway process per host serves every local profile, and closing the desktop window does not stop it. Remote gateways, SSH hosts and cloud instances run their Bots on their own machines — [Bot Mode docs](https://hermes-agent.nousresearch.com/docs/user-guide/bot-mode)
- v0.23.0, "The Hearth Release" (Nov 5, 2026), lets the gateway serve the full Desktop workspace as a browser Webapp. It also makes one gateway own each session and improves multi-machine Desktop — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- Hermes Cloud (preview) is a hosted agent on a dedicated instance with its own workspace and dashboard, billed hourly from Nous credits. Running costs are about $8.70, $16.80 and $32.70 per month for Small, Medium and Large. A stopped instance costs $0.03/day for storage, and inference and tools are billed separately. There is an isolated container per agent — [dev.to (OpenClaw Launch, a competing host), Aug 2, 2026](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)
- Each Portal Plus, Super and Ultra plan includes one Cloud agent, which can be opened from a phone's browser — [Hermes Atlas mobile guide, Nov 16, 2026](https://hermesatlas.com/guide/mobile/)
- Third parties sell managed hosting, for example Hostinger from $5.99/month — [search summary of Hostinger page](https://www.hostinger.com/ng/tutorials/?p=147949)

### Inferences
- The architecture is one person's agent, which can run on many machines. Silo's model is different: one server runs many durable containers for many users. In Hermes the container is optional, chosen as a terminal backend, while in Silo the box is the Bot.

### Gaps
- I could not verify Hermes Cloud's current (Nov 2026) pricing or whether it is still in preview from a Nous primary source.
- An Oct 2026 article reports a "launch for business teams" alongside a $90M Series B at a $1.5B valuation ([emergent.sh, Oct 8, 2026](https://emergent.sh/news/nous-research-hits-15b-valuation-launches)). Its feature list (templates, visual no-code builder, "enterprise-grade compliance") matches nothing in the docs and may be generic or inaccurate. Treat it as unverified.

## 2. Multi-user / teams / access control

### Takeaway
Hermes has no multi-account model: no user database, no roles and no workspaces. "Multi-user" means letting several chat-platform identities talk to one operator's agent through gateway allowlists and DM pairing, with an admin and regular-user tier per chat scope. "Teams" in Bot Mode means teams of Bots, not of people.

### Cited Findings
- The gateway denies by default. The check order is: per-platform allow-all, DM-pairing approved list, platform allowlists, global allowlist, global allow-all. Anything else is refused — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- DM pairing codes are 8 characters from a CSPRNG and expire after 1 hour. Each user may request one per 10 minutes, each platform allows at most 3 pending, and 5 failed approvals cause a 1-hour lockout. The operator approves with `hermes pairing approve <platform> <code>` — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security); [Messaging docs](https://hermes-agent.nousresearch.com/docs/user-guide/messaging/)
- Each scope (a DM, group or channel) has admins, who may run every slash command, and regular users, who may run only the commands in `user_allowed_commands`. Answering an approval prompt counts as a command. Non-admin users see only sessions from their own chat origin — [Messaging docs](https://hermes-agent.nousresearch.com/docs/user-guide/messaging/)
- `approvals.approvers` limits who can answer an approval in a shared chat: the requester, listed IDs and platform admins — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- A Bot "**is** a Hermes profile", isolated under `~/.hermes/profiles/<name>/` with its own config, memory, skills, credentials and history. "Sections" such as Team or Clients only organize Bots and don't change permissions. The Bot Mode docs describe one human operator, not several human accounts — [Bot Mode docs](https://hermes-agent.nousresearch.com/docs/user-guide/bot-mode)
- "Teams can share one agent by using a gateway allowlist" — [Hermes Atlas guide](https://hermesatlas.com/guide/)
- The Webapp listens on `127.0.0.1:9120` by default, so remote access needs separate networking and authentication. "A signed-in Webapp user can run commands and reach the host's files and credentials" — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)
- Remote calls between gateways need the target gateway's API key — [Bot Mode docs](https://hermes-agent.nousresearch.com/docs/user-guide/bot-mode)

### Inferences
- This is a strong contrast with Silo. Silo has workspaces, roles (owner/admin/member/guest, manage/use), per-chat authorship, approvals routed to who may answer them, and lending of credentials. Hermes's identity layer is chat-platform allowlists and a local Webapp with no user accounts.

### Gaps
- I found no documented SSO/OIDC, per-user audit log or role model for the Webapp or Desktop.

## 3. Execution sandbox (container/VM, desktop, browser control, shell, code)

### Takeaway
Hermes runs shell commands and agent-written Python (`execute_code`) on whichever terminal backend is chosen. The default is local, with no isolation. Docker runs hardened (capabilities dropped, a 256-process cap, a 5 GB memory default). There is no Linux desktop the agent drives through screenshots. Browser control is a single `browser_exec` tool over cloud or local browsers via CDP. File-write denylists are explicitly "defense-in-depth, not a hard boundary".

### Cited Findings
- The Docker backend drops all capabilities and adds back only DAC_OVERRIDE, CHOWN and FOWNER. It sets `no-new-privileges` and a pids limit of 256, and makes `/tmp` and `/var/tmp` size-limited tmpfs (the latter `noexec`). Defaults are 5 GB memory and 50 GB disk. Persistent mode bind-mounts workspace and home, and ephemeral mode uses tmpfs — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- Local is the no-isolation backend, and container and cloud backends **skip dangerous-command checks** because the container is treated as the boundary — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- `execute_code` runs agent-written Python that calls Hermes tools over RPC, collapsing multi-step pipelines into one inference call. Scripts can use subprocess, os and shutil directly, so a gateway session gets one approval for the whole script — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview); [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- `write_file` and `patch` are refused outright on credential paths such as `~/.ssh` keys, `~/.aws`, `~/.kube`, `.env` and OAuth tokens. `HERMES_WRITE_SAFE_ROOT` is an optional write sandbox, set in the official Docker image. The `terminal` tool runs as the same user and can still reach those paths — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- Browser automation runs through Browserbase, Browser Use, local Chrome/Brave/Chromium/Edge over CDP, or a local Chromium — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- v0.23.0 removed the old browser tools (agent-browser and `browser_*`), so browser automation now uses `browser_exec` only — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- Checkpoints snapshot the working directory before file changes, and `/rollback` restores them. v0.23.0 added `/rewind` — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview); [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- Desktop and mobile include an SSH terminal view — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)
- Nous Portal's Tool Gateway offers Modal sandboxes for code execution — [dev.to](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)

### Inferences
- Hermes has no equivalent to Silo's per-Bot Xvfb desktop with look/click/type, a VNC hatch, or a pause/takeover switch. On macOS the agent can drive `osascript` on the host, which the approval engine treats specially.

### Gaps
- I found no evidence of a persistent per-conversation Python kernel like Silo's. `execute_code` appears to be one script per call (unconfirmed).

## 4. Tools, MCP & integrations

### Takeaway
Hermes ships "40+ tools" grouped into toolsets that can be enabled per platform. It has a full MCP client over stdio and HTTP with per-server tool filtering, sampling, trust levels and spend limits for paid MCP tools. It also has a plugin system, an OpenAI-compatible API server and an ACP integration for IDEs.

### Cited Findings
- "40+ tools" in a toolset system — [GitHub README](https://github.com/NousResearch/hermes-agent)
- The MCP client connects over stdio or HTTP, with per-server tool filtering and sampling — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- A stdio MCP subprocess gets a filtered environment: PATH, HOME, USER, LANG, TERM and XDG_* only. Write-capable tools on `trust: untrusted` servers prompt, and declared spend limits still apply under YOLO — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- v0.23.0 added "price tags and spend limits for paid MCP tools" and auto-updating plugins — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- The API server exposes Hermes as an OpenAI-compatible HTTP endpoint for frontends such as Open WebUI. ACP runs it inside VS Code, Zed and JetBrains. Event hooks cover logging, alerts, webhooks, tool interception and guardrails. Context references use `@` for files, folders, git diffs and URLs — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- The plugin catalog is very active: canary builds add plugins almost nightly, such as dosbox-mcp, permission-tiers, opendockit, and provider and memory plugins. Microsoft Graph auth moved into a teams_pipeline plugin — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- A marketplace lives at hermes-agent.nousresearch.com/marketplace — [Docs index](https://hermes-agent.nousresearch.com/docs/)
- Credential pools spread API calls across several keys and rotate on limits. Hermes can adopt Codex CLI and Claude Code logins (`auth.adopt_external_logins`) — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview); [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)

### Inferences
- The tool and integration count is far broader than Silo's. Credentials, though, sit on the same host as the agent, not held back on a control plane. A Hermes skill's `required_environment_variables` pass straight through to the terminal.

### Gaps
- I could not find a complete Nov 2026 list of built-in tools.

## 5. Skills / plugins / marketplace

### Takeaway
Self-improving skills are Hermes's headline feature. The agent writes and patches its own SKILL.md files (`skill_manage`) after complex tasks, errors or corrections. A background review and a "curator" consolidate them, and writes can be staged for human approval. Skills follow the agentskills.io standard, and a Skills Hub pulls from skills.sh, ClawHub, LobeHub, GitHub, well-known URLs and more, behind four trust tiers and a security scanner.

### Cited Findings
- `skill_manage` supports create, patch (preferred), rewrite, delete, write_file and remove_file. Skills are made after multi-step workflows, resolved errors and user corrections. With `skills.write_approval`, writes are staged under `~/.hermes/pending/skills/` for `/skills pending | diff | approve | reject`. An advisory linter runs on writes, and `hermes curator lock <skill>` blocks agent edits to a skill — [Skills docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)
- Skills are created after repeated patterns, typically five or more tool calls — [Hermes Atlas guide](https://hermesatlas.com/guide/)
- Progressive disclosure: `skills_list()` costs about 3k tokens, then `skill_view(name)`, then `skill_view(name, path)` — [Skills docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)
- Hub sources are official, skills-sh (Vercel), well-known (`/.well-known/skills/index.json`), github, clawhub, lobehub, browse-sh and url. agentskills.io is the format standard, not a hub source — [Skills docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)
- Trust tiers are builtin, official, trusted (such as openai/skills and anthropics/skills) and community. The install scanner checks for exfiltration, injection, destructive commands and supply-chain signals. A "dangerous" verdict always blocks, and caution or warn can be forced. An optional NVIDIA SkillEvaluator scan is advisory only — [Skills docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)
- v0.23.0 cut the bundled skill set from 58 to 44 — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- An arXiv analysis argues that self-generated skills skip the hub scanner: "the security scanner exists but is not invoked on the Background Review pathway" — [arXiv 2606.23075 (via search summary)](https://arxiv.org/pdf/2606.23075)

### Inferences
- Silo installs skills only by a human ("Save skill" on an artifact card, or Workspace install), and its Bot does not rewrite its own skills unasked. Hermes's learning loop is a real feature gap for Silo, and also a safety and quality risk (see §18).

### Gaps
- I did not read the separate Curator page, so its schedule and algorithm are not covered.

## 6. Memory & personalization

### Takeaway
Built-in memory is two small, hard-capped files frozen into the system prompt at session start. MEMORY.md holds 2,200 characters (about 800 tokens) and USER.md holds 1,375 characters (about 500 tokens). Past sessions are searchable with SQLite FTS5. A background review after turns may save memory or skills. External providers come as plugins: Honcho for dialectic user modelling, Mem0, Hindsight, Supermemory, RetainDB, ByteRover, OpenViking and Holographic.

### Cited Findings
- Limits are enforced strictly: an overflowing write errors and the agent must consolidate. Memory does not auto-compact. The `memory` tool has add, patch and remove — [Memory docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)
- The block is a frozen snapshot at session start, to keep the prefix cache. Mid-session writes reach disk but not the prompt — [Memory docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)
- Sessions live in SQLite (`~/.hermes/state.db`) with FTS5, and results are raw messages — [Memory docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory). The README says "FTS5 session search with LLM summarization" — [GitHub README](https://github.com/NousResearch/hermes-agent). The two sources conflict on summarization.
- The background self-improvement review may save memory or update skills. `write_approval` stages memory writes for `/memory pending`. A review on the same model reuses the warm cache, and one on another model replays a compact digest capped at 75% of its window (up to 600k tokens) — [Memory docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)
- RetainDB and ByteRover are bundled. Honcho, Hindsight, Supermemory, Mem0, OpenViking and Holographic come from the plugin catalog — [Memory docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)
- SOUL.md sets personality, and `/personality` applies per-session presets — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- `hermes import-agent openclaw` imports SOUL.md, memories, skills, command allowlists, messaging settings and TTS assets — [GitHub README](https://github.com/NousResearch/hermes-agent)

### Inferences
- Silo's memory is richer in structure: kinds, tentative-until-seen-twice, retirement instead of deletion, digests, and a keeper reading quiet chats. Silo also has RAG over folders (Knowledge), which I found no built-in Hermes equivalent for beyond plugins. Hermes's user-modelling story (Honcho) has no Silo counterpart.

### Gaps
- `memory.nudge_interval` is named but not explained in the docs I read.

## 7. Scheduling, automations, proactivity, cron

### Takeaway
A built-in cron takes natural-language or cron schedules, delivers to any connected platform, and supports pause, resume, edit and cancel. Headless jobs deny dangerous commands by default.

### Cited Findings
- Jobs run on natural-language or cron schedules, with delivery to platforms and pause/resume/edit — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- Typical uses are daily reports and nightly backups — [GitHub README](https://github.com/NousResearch/hermes-agent)
- `cron_mode`, `single_query_mode` and `unattended_mode` all default to `deny` for dangerous commands — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- v0.23.0 added cron job cancellation — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- Bot Mode "routines" are cron jobs per profile (`hermes cron list`) — [Bot Mode docs](https://hermes-agent.nousresearch.com/docs/user-guide/bot-mode)
- The home channel receives restart and shutdown notices and operator alerts. Cron failure delivery is configured separately — [Messaging docs](https://hermes-agent.nousresearch.com/docs/user-guide/messaging/)

### Gaps
- I found no "proactivity level", heartbeat, Dashboard of asks or Feed-like inbox equivalent.

## 8. Messaging channels

### Takeaway
About 28 platforms run from one gateway process, far more than Silo's three.

### Cited Findings
- The list: Telegram, Discord, Slack, Google Chat, WhatsApp, WhatsApp Cloud API, Signal, SMS, Email, Home Assistant (plugin), Mattermost, Matrix, DingTalk, Feishu/Lark, WeCom, WeCom Callback, Weixin, BlueBubbles (iMessage), Photon (iMessage), QQ, Yuanbao (Labs plugin), Microsoft Teams, LINE, ntfy, Raft, IRC, Buzz and SimpleX. "Hermes Relay" (experimental) fronts Discord, Telegram, Slack and WhatsApp — [Messaging docs](https://hermes-agent.nousresearch.com/docs/user-guide/messaging/)
- Per-channel overrides let different channels use different models and system prompts. `gateway.bot_loop_guard` and `*_ALLOW_BOTS` control bot-to-bot traffic — [Messaging docs](https://hermes-agent.nousresearch.com/docs/user-guide/messaging/)
- Messaging slash commands include /new, /model, /retry, /undo, /compress and /usage — [GitHub README](https://github.com/NousResearch/hermes-agent)
- An early bug truncated Telegram bot tokens (since fixed). v0.23.2 fixed duplicate "Recovered reply" messages on Telegram — [aiagentstore](https://aiagentstore.ai/agentic-ai-and-workflow-automation/en/the-20-biggest-problems-with-hermes-agent-what-thousands-of-reddit-and-x-users-are-actually-struggling-with-ranked); [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)

### Gaps
- I could not check whether WhatsApp uses an unofficial linked-device library (like Silo's whatsmeow) or only the Cloud API. Both entries appear.

## 9. Voice

### Takeaway
Voice coverage is broad: CLI push-to-talk, a "Hey Hermes" wake word, streaming STT and TTS, barge-in, Discord voice channels and voice-memo transcription on messaging platforms. Desktop-only full-duplex "GPT-Live" delegates to Hermes. Local options (faster-whisper, NeuTTS, Piper, Edge TTS) mean voice can cost nothing.

### Cited Findings
- STT: local faster-whisper by default (base model about 150 MB), then Groq whisper-large-v3-turbo (about 0.5 s), then OpenAI whisper-1 or gpt-4o-transcribe. Mistral, xAI, ElevenLabs and DeepInfra are also supported. Oversized audio is split at pauses. Streaming STT (`stt.streaming`) works with OpenAI, xAI and ElevenLabs — [Voice docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/voice-mode)
- TTS: Edge TTS (free, the default fallback), ElevenLabs, OpenAI (with `instructions`), NeuTTS (local), plus minimax, mistral, gemini, xai, kittentts and piper. Replies stream sentence by sentence — [Voice docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/voice-mode)
- Barge-in works by speaking over generation or playback, and the agent is told it was cut off. Tuning is `voice.barge_in_threshold_multiplier` (3.0) and `barge_in_grace_seconds` (0.5) — [Voice docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/voice-mode)
- In Discord, `/voice join` transcribes each speaker in the voice channel and replies in voice, limited by `DISCORD_ALLOWED_USERS` or `ROLES`. The listener pauses during playback to avoid self-hearing. Silence timeouts are 3 s in the CLI and 1.5 s in Discord — [Voice docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/voice-mode)
- GPT-Live (desktop only) uses the full-duplex `gpt-live-1`, which delegates real work to Hermes. OpenAI bills $0.05 per minute of session, idle included — [Voice docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/voice-mode)
- A `voice_chat` auxiliary model slot routes spoken turns to a faster model, with reasoning off on voice turns by default — [Voice docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/voice-mode)
- The "Hey Hermes" wake word works in the CLI, TUI and desktop — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)

### Inferences
- GPT-Live delegating to Hermes is architecturally close to Silo's "talker" that delegates to the Bot. Silo's WebRTC browser and iOS calls with echo-cancelled barge-in look more polished for phone and web. Hermes is ahead on local, free STT and TTS, and on Discord voice channels.

### Gaps
- The docs do not cover phone calls (PSTN). The mobile app's "Live Mode" is unconfirmed.

## 10. Clients

### Takeaway
Hermes has a CLI/TUI, a native Desktop app (public preview Jun 3, 2026, with Bot Mode and group chats), a browser Webapp served by the gateway (v0.23.0, Nov 2026), and IDE integration via ACP. An OpenAI-compatible API serves third-party UIs such as Open WebUI. The official iOS/Android "Hermes Mobile" app is in waitlisted beta (launched at NousCon, late Oct 2026).

### Cited Findings
- The CLI/TUI has multiline editing, streaming tool output and, from v0.23.0, inline images — [GitHub README](https://github.com/NousResearch/hermes-agent); [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- Desktop entered public preview on June 3, 2026 for Mac, Windows and Linux, as a native window onto the same agent — [search summary of mejba.me first look](https://www.mejba.me/blog/hermes-desktop-app-first-look). v0.16.0 (June 5) added the desktop app and a browser-based admin dashboard — [search summary, Releasebot / Hermes Atlas](https://hermesatlas.com/guide/). Desktop needs macOS 13+ or 64-bit Windows — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- The Webapp is enabled with `hermes config set webapp.enabled true` and served at `127.0.0.1:9120`. Remote use needs your own HTTPS and authentication — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)
- Hermes Mobile is an official beta via TestFlight and Firebase App Distribution, waitlist only, with no GA date. It has chat by profile, model and skill toggles, dictation, an SSH terminal, scheduled jobs, artifacts, files, skills, connections, plugins, MCP controls and a Kanban board. Approvals and push notifications are not described — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)
- Before v0.9 Hermes was CLI-only — [aiagentstore](https://aiagentstore.ai/agentic-ai-and-workflow-automation/en/the-20-biggest-problems-with-hermes-agent-what-thousands-of-reddit-and-x-users-are-actually-struggling-with-ranked)

### Inferences
- Silo's web and iOS clients are first-class and multi-user. Hermes's web surface is a local single-operator workspace, and its mobile app lags (beta, waitlist).

## 11. Security & safety

### Takeaway
Hermes has layered heuristic defences. Approvals of dangerous commands default to "smart", where an auxiliary LLM rates risk and escalates uncertain ones. There is a hardline blocklist, user deny rules, write-path denylists, env scrubbing, SSRF guards, a website blocklist, and a heuristic prompt-injection scan of context files. There is no general LLM-based filter for tool output, and no egress gateway in front of the sandbox. Several 2026 CVEs hit the skills scanner and tools.

### Cited Findings
- `approvals.mode` options: `smart` (default; an auxiliary LLM approves low risk, denies dangerous, escalates uncertain), `manual`, and `off` (= `--yolo`). Timeout is 300 s and fails closed. Grants can be once, session or always (stored in `command_allowlist`). `hermes approvals suggest` mines approval history for allowlist entries but never proposes destructive classes — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security); [Bot Mode docs](https://hermes-agent.nousresearch.com/docs/user-guide/bot-mode)
- The hardline blocklist (root wipes, fork bombs, mkfs on the live root, and so on) holds even under YOLO. `approvals.deny` glob rules unwrap sudo/env and `sh -c`, but are "a command policy, not a sandbox" — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- Prompt injection: AGENTS.md, .cursorrules and SOUL.md are scanned for overrides, hidden HTML comments, secret reads, curl exfiltration and invisible Unicode. Flagged project files are not loaded. These are "pattern heuristics, not semantic intent detection". `curl`/`wget` carrying secrets is flagged, as is bidi Unicode in commands — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- Network: SSRF blocks RFC 1918, loopback, link-local/metadata and CGNAT, and re-validates redirects at each hop. `security.website_blocklist` covers web_extract, browser, vision and hub downloads, but **`web_search` is not gated** — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- Secrets: `execute_code` strips variables named *KEY*, *TOKEN* and similar. Tool errors redact tokens. Supply-chain checks flag known-compromised Python packages (`hermes doctor`). The tirith scanner was removed with no replacement. Gateway hooks load in-process with no consent gate — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- CVE-2026-9353 is an injection flaw in Skills Guard (`agent/skills_guard.py`), fixed in 0.15.0 — [GitHub Advisory GHSA-238w-f66p-w349](https://github.com/advisories/GHSA-238w-f66p-w349). CVE-2026-14783 is a path traversal in `skill_view` (`tools/skills_tool.py`) — [SentinelOne](https://www.sentinelone.com/vulnerability-database/cve-2026-14783/)
- A third-party summary lists CVE-2026-9350, -9367 and -9368 (batch-runner authorization, terminal command injection, env sandbox bypass), all said to be fixed in v0.13.0. I did not verify these against NVD — [search summary, techjacksolutions/meshworld](https://techjacksolutions.com/ai-tools/hermes/securing-hermes-agent/)
- An approvals parsing fix ("nested git option parsing in approvals") landed in the Nov 16 canary — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)

### Inferences
- Silo differentiates on security. It has per-action rules with operator, workspace and Bot floors, LLM or decision-model auto-approval with human fallback, an LLM output filter on every tool result, an egress gateway with TLS inspection and surrogate secrets, and credentials that never enter the box. Hermes's model is "trust the operator, harden the host", with the container as optional boundary. In container mode, command approvals are simply skipped.

### Gaps
- I found no confirmed in-the-wild malicious-skill incident on the Hermes hub.

## 12. Model providers & cost/token efficiency

### Takeaway
Hermes is model-agnostic: Nous Portal (about 250–300 models), OpenRouter, OpenAI, Anthropic, local via Ollama or LM Studio, custom OpenAI-compatible endpoints and borrowed Codex or Claude Code logins. It has fallback providers, provider routing and credential pools. Prompt caching is a 1-hour cross-session prefix cache for Claude. Frozen memory snapshots and progressive skill disclosure save tokens, and `/compress` compacts the context.

### Cited Findings
- Nous Portal offers "300+ models" via `/model` — [GitHub README](https://github.com/NousResearch/hermes-agent). Another source counts 248 models and 29 embedding models, with third-party models routed via OpenRouter, Nous Hermes 4 on Nous's own backend, and x402 USDC payments in beta — [dev.to, Aug 2026](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)
- Portal plans: Free ($0, free models, 50 RPM), Plus ($20/mo with $22 credits), Super ($100/mo with $110 credits) and Ultra ($200/mo with $220 credits). The paid Tool Gateway bundles Firecrawl search, FAL and Krea images, FAL video, OpenAI speech, Browser Use and Modal sandboxes, all paid from credits — [dev.to, prices verified Aug 1, 2026](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh). The Tool Gateway is included with every paid Portal subscription, and the free tier gets inference only — [Tool Gateway docs (via search)](https://hermes-agent.nousresearch.com/docs/user-guide/features/tool-gateway)
- Since May 15, 2026, Grok subscriptions connect via OAuth — [dev.to](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)
- Prompt caching is a "built-in 1-hour cross-session prefix cache for Claude on Anthropic, OpenRouter, and Nous Portal". Fallback providers and credential pools are covered — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- Local models need a 64K context window, while Ollama defaults to 4K. Claude or GPT usage can exceed $100/month — [Hermes Atlas guide](https://hermesatlas.com/guide/)
- There is a docs guide to running Nemotron 3 Ultra for free — [Docs index](https://hermes-agent.nousresearch.com/docs/)
- `hermes fallback check` was noted in v0.23.x — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)

### Gaps
- I did not find the context-compression algorithm (how `/compress` works, or whether auto-compaction triggers at a threshold).

## 13. Multi-agent / subagents

### Takeaway
`delegate_task` spawns isolated child agents (3 concurrent by default) with a configurable provider, model and limits. v0.23.0 made delegation pluggable, and "durable subagents" appeared in v0.23.x. Separately, Bot Mode lets named profile-Bots message each other (`message_agent`) and hold group chats of 2–6 Bots with `@mentions`, under hop and round caps.

### Cited Findings
- `delegate_task` spawns isolated child agents, three concurrent by default — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview). Subagent provider, model and limits are set under Settings → Model → Subagents — [GitHub README](https://github.com/NousResearch/hermes-agent)
- v0.23.0 added "pluggable delegation". The v0.23.1 and v0.23.2 notes list "durable subagents", undocumented until v0.24.0 — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- In Bot Mode group rooms of 2–6 Bots, up to 3 serial rounds run per user message and each Bot replies or passes. `@user` escalates with a "needs you" badge. Caps are 10 messages per send and `max_hops` (default 4) for bot-to-bot exchanges. Rooms can include Bots on other machines — [Bot Mode docs](https://hermes-agent.nousresearch.com/docs/user-guide/bot-mode)
- v0.9.0 (Apr 13) added a background process monitor for subagents — [search summary, heyuan110 review](https://www.heyuan110.com/posts/ai/2026-04-14-hermes-agent-guide/)
- The Mobile beta shows a Kanban board — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)

### Inferences
- Bot-to-Bot collaboration across machines is something Silo lacks. Silo's subagents are within one Bot's chat, and its Bots do not talk to each other.

## 14. Web search, browsing

### Takeaway
Web search and extraction go through configurable backends, with Firecrawl bundled through the Nous Tool Gateway. Browsing uses `browser_exec` over Browserbase, Browser Use, or local Chrome-family browsers via CDP. Vision covers pasted images and video analysis.

### Cited Findings
- The Tool Gateway covers web search, image generation (FAL), TTS (OpenAI) and cloud browser (Browser Use), set up with `hermes setup --portal` — [GitHub README](https://github.com/NousResearch/hermes-agent); its search and scraping is Firecrawl — [dev.to](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)
- The browser backends are Browserbase, Browser Use, local Chrome/Brave/Chromium/Edge over CDP, or local Chromium. The `browser_*` tools were replaced by `browser_exec` in v0.23.0 — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview); [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- `web_extract`, browser, vision and video analysis obey the website blocklist and SSRF guards — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)

### Gaps
- I could not confirm the self-hosted search backends without Portal (for example SearXNG, Brave or Tavily keys).

## 15. Output surfaces

### Takeaway
Output goes to the CLI/TUI (inline images), Desktop or Webapp workspaces, chat platforms, cron delivery to any platform, an artifacts and files view on mobile, image and video generation, and TTS audio. I found no hosted live HTML apps or share links like Silo's artifacts.

### Cited Findings
- Hermes Mobile shows generated artifacts, files and a Kanban board — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)
- Image generation uses FAL.ai with eleven models, and the Gateway adds Krea and FAL video — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview); [dev.to](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)
- The CLI shows inline images (v0.23.0), and the Webapp serves the full Desktop workspace in a browser — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)

### Gaps
- I found no evidence of a dashboard of widgets, a Feed, or public share URLs.

## 16. Research/training features

### Takeaway
This is unique to Hermes. The same harness generates batch trajectories (ShareGPT format), compresses trajectories to token budgets, and hosts Atropos RL environments, which Nous uses to train tool-calling models.

### Cited Findings
- Batch processing runs the agent over many prompts in parallel and outputs ShareGPT-format trajectories — [Features overview](https://hermes-agent.nousresearch.com/docs/user-guide/features/overview)
- The README covers "batch trajectory generation" and "trajectory compression for training tool-calling models" — [GitHub README](https://github.com/NousResearch/hermes-agent)
- The docs index lists "Batch processing, trajectory export, and RL training with Atropos" — [Docs index](https://hermes-agent.nousresearch.com/docs/)
- The RL code lives in `environments/`, using atroposlib and tinker dependencies with `TINKER_API_KEY` and `WANDB_API_KEY`. README mirrors disagree on whether a git submodule is needed — [mirror of README](https://github.com/UfukNode/hermes-agent)

### Inferences
- Silo has nothing comparable. Its run logs and `turn` events are close to trajectories but are not exported for training.

### Gaps
- I found no official doc on how the Atropos environments work.

## 17. Adoption & ecosystem

### Takeaway
Hermes is extremely popular, with about 264k stars, about 60k forks, 74k commits and 5k+ open issues and PRs. It ships every 1–2 weeks with nightly canaries, with roughly 2,000 PRs merged between v0.23.1 and v0.23.2 alone. Its ecosystem is driven by OpenClaw migrants, and it has a one-command importer from OpenClaw, Claude Code, Codex CLI and Cursor.

### Cited Findings
- The repo has 264.2k stars, 59.6k forks, 74,180 commits and "5k+" issues and PRs — [GitHub](https://github.com/NousResearch/hermes-agent). Another count gives 263,846 stars as of 2026-11-16, with 318+ community projects in Hermes Atlas — [Hermes Atlas guide](https://hermesatlas.com/guide/)
- Releases: v0.23.0 "The Hearth Release" (Nov 5, 2026); v0.23.1 (Nov 9/10, about 618 PRs); v0.23.2 (Nov 16, "~1,989 merged PRs since v0.23.1", full notes deferred to v0.24.0); nightly canaries — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)
- Earlier releases: v0.9.0 "the everywhere release" (Apr 13), v0.16.0 (Jun 5: desktop app and admin dashboard), v0.17.0 (Jun 19) and v0.18.0 (Jul 1) — [search summary of Releasebot/heyuan110](https://releasebot.io/updates/nousresearch/hermes-agent)
- Launch dates conflict: one source says February 2026 and another says October 7, 2026 — [search summary](https://emergent.sh/news/nous-research-hits-15b-valuation-launches)
- Migration: `hermes import-agent <openclaw|claude-code|codex|cursor> [--dry-run]` imports SOUL.md, memories, skills, allowlists, messaging settings and TTS assets. API keys come over only with `--migrate-secrets` — [GitHub README](https://github.com/NousResearch/hermes-agent)
- Funding: a reported $90M Series B at a $1.5B valuation (Oct 7, 2026) — [emergent.sh](https://emergent.sh/news/nous-research-hits-15b-valuation-launches). Nous joined an Nvidia (Nemotron) coalition in June 2026 — [Wikipedia: Nemotron (via search)](https://en.wikipedia.org/wiki/Nemotron)
- NousCon (late Oct 2026) launched the mobile beta — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)

### Gaps
- The exact first public release date is uncertain (February 2026 per several guides). I did not directly read the releases between v0.18 and v0.22.

## 18. Known weaknesses / common complaints

### Takeaway
Complaints center on the learning loop: self-assessment is always "success", bad skills and memories persist, and self-improvement overwrites user edits. Others are churn and breakage from very fast releases, CLI display glitches, setup-wizard loops, poor tool calling on small models, and a weak security model for shared or remote use (local Webapp, approvals skipped in containers, several 2026 CVEs).

### Cited Findings
- From a July 12, 2026 aggregator of Reddit and X complaints (secondhand, unverified): self-evaluation almost always "successful"; manual edits to skills overwritten; erroneous learned skills (one merged unfinished work into main); persistent bad memory; memory and skill bloat; CLI input appearing in the wrong place and output stalling; `hermes setup` loops; small (7B) models failing at tool calls; Docker images missing dependencies; Termux failures; a Codex NoneType crash (fixed May 27, 2026); suspected astroturfing; rapid changes breaking workflows; some broken early releases (v0.3–v0.5) — [aiagentstore](https://aiagentstore.ai/agentic-ai-and-workflow-automation/en/the-20-biggest-problems-with-hermes-agent-what-thousands-of-reddit-and-x-users-are-actually-struggling-with-ranked)
- Research warns that self-generated skills bypass the hub scanner, and that "deceptive skills" can pass surface checks — [arXiv 2606.23075](https://arxiv.org/pdf/2606.23075)
- Approval checks are skipped on container backends. The denylist is "not a hard boundary". The website blocklist does not gate `web_search`. The tirith scanner was removed with nothing in its place — [Security docs](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- A Webapp user can reach the host's files and credentials, and the Webapp is localhost-only without your own authentication — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)
- Release volume is high: about 2,000 PRs in one patch cycle, with curated notes deferred — [GitHub releases](https://github.com/NousResearch/hermes-agent/releases)

### Gaps
- I did not read primary HN or Reddit threads. The ranking comes from an aggregator of unknown rigor.

## 19. Pricing & license

### Takeaway
Hermes Agent is MIT-licensed and free. Costs are model and API spend, optional Nous Portal subscriptions ($20/$100/$200 per month tiers per Aug 2026 data) that unlock the Tool Gateway and one Cloud agent, and optional Hermes Cloud instance hours.

### Cited Findings
- MIT license — [GitHub](https://github.com/NousResearch/hermes-agent)
- Hermes is free, and a local model can cost $0. The Portal is a flat monthly subscription — [Hermes Atlas guide](https://hermesatlas.com/guide/)
- Portal tiers: Free $0, Plus $20, Super $100 and Ultra $200 per month, each with credits worth about 110% of the price. Hermes Cloud runs about $8.70–$32.70/month per instance, plus $0.03/day while stopped — [dev.to, Aug 2026 (by a competing host)](https://dev.to/zackchew/nous-portal-explained-plans-models-tools-and-hermes-cloud-9hh)
- Each paid Portal plan includes one Cloud agent — [Hermes Atlas mobile guide](https://hermesatlas.com/guide/mobile/)
- Some sources say no fixed Portal rate is published in the docs — [techjacksolutions](https://techjacksolutions.com/ai-tools/hermes/hermes-agent-cost-breakdown/)

### Gaps
- portal.nousresearch.com could not be fetched, so the Nov 2026 prices are unconfirmed.
