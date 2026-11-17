# OpenBot (openbot.run) — capability inventory, November 2026

Research date: 2026-11-17. Sources: openbot.run (homepage, pricing, teams, compare, changelog), the GitHub repo nightly-labs/openbot (README and docs/self-hosted-server.md), and a few press and aggregator write-ups. The changelog is about 612k characters long. I read the newest releases (0.69–0.72), 0.63–0.66 and 0.51–0.56. I did not read 0.67–0.68, 0.57–0.62 or the August–September releases, so "first appeared" dates are the earliest I saw, not proven first dates. I found no Hacker News or Reddit threads and no independent hands-on review. Most third-party coverage is aggregator or SEO material.

## 0. Identity: maker, launch date, open/closed source, positioning

### Takeaway
openbot.run is **OpenBot by Nightly Labs**, published by **Synthetify Labs**, a Polish Web3 studio that also makes Nightly Wallet. It is a local-first **desktop app**, built with Electron, Bun and SolidJS, that runs existing coding-agent CLIs (Codex, Claude Code, Gemini, Grok, OpenCode, Cursor, Cline and more) as persistent "AI teammates". There is also an optional self-hosted or hosted Linux server. The code is **source-available, not open source**: it is under PolyForm Noncommercial 1.0.0. Version 0.1.0 shipped on 13 Aug 2026, there was a press and Product Hunt push in late October 2026, and the latest version at research time was 0.72.0 (16 Nov 2026). It is **not** Intel's Android robot OpenBot, **not** CopilotKit's OpenBot, and **not** an OpenClaw fork. It runs other vendors' agent harnesses and positions itself against OpenClaw.

### Cited Findings
- The homepage footer gives copyright to Synthetify Labs and says "Made within Poland". The pitch is "Run Codex, Claude, Gemini, Grok and your own models in one persistent workspace." The GitHub link is nightly-labs/openbot. — [openbot.run](https://openbot.run)
- The README describes it as "a local-first desktop workspace for persistent AI teammates". It is an Electron main process plus a SolidJS renderer. It drives a local Codex App Server over stdio JSONL and a Claude Agent SDK session over stream JSON, and keeps a SQLite event log. Other providers connect through ACP (Agent Client Protocol). The account API runs on Cloudflare Workers (TanStack Start, D1, R2). — [GitHub README](https://github.com/nightly-labs/openbot)
- AlphaSignal (26 Oct 2026) attributes it to "Nightly Labs, which operates as Synthetify Labs" and calls it source-available. — [AlphaSignal](https://alphasignal.ai/news/nightly-labs-openbot-runs-multiple-coding-agents-from-one-desktop-app)
- Synthetify Labs is a Web3 development studio. Its products include Nightly Wallet (a multichain crypto wallet) and Neony (a perpetuals DEX). — [Alchemy dapps listing](https://www.alchemy.com/dapps/synthetify-labs)
- The changelog lists 126 releases, from 0.1.0 (13 Aug 2026) to 0.72.0 (16 Nov 2026). — [Changelog](https://openbot.run/changelog)
- ExplainX (26 Oct 2026) says this project is different from the CopilotKit OpenBot covered in August 2026. It labels OpenBot a "development preview". — [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)
- Several unrelated projects share the name: CopilotKit's OpenBot (self-hosted "AI coworkers"), the PyPI package `openbot-ai`, and a "meetopenbot" project. — [CopilotKit OpenBot](https://copilotkit.ai/openbot), [PyPI openbot-ai](https://pypi.org/project/openbot-ai/), [SourcePulse meetopenbot](https://www.sourcepulse.org/projects/27153408)
- OpenBot's own comparison pages position it against OpenClaw, Hermes Agent, Claude Cowork, Manus, Devin, Paperclip, Viktor, Instinct, ChatGPT dots, Meta Muse and Grok Bot. Its recurring pitch is "free, local, uses the AI plans you already pay for". — [Compare](https://openbot.run/compare)

### Inferences
- *(inference)* The product is an orchestration and UI shell around vendors' agent harnesses. Its core bet is reusing consumer subscriptions (ChatGPT, Claude, Gemini, Grok plans) instead of API keys. That is unlike Silo, which runs its own agent loop and tool engine. Silo's `claude` provider takes a similar subscription-wrapping approach for one vendor only.
- *(inference)* Shipping is extremely fast: about 72 minor versions in 3 months, often more than one release a day. Features land and change quickly, so any snapshot ages fast.

### Gaps
- Exact founding team, funding and headcount: not found.
- The precise date of the Apache-2.0 → PolyForm switch: the README only says versions up to 0.1.11 remain Apache-2.0.

## 1. Deployment & hosting

### Takeaway
There are three modes: (a) the desktop app on your own Mac, Windows or Linux machine, (b) a self-hosted headless Linux server (systemd install or Docker image, plus AWS ECS, Umbrel and Unraid recipes), and (c) a hosted server that OpenBot runs (EU, from €30/month). Clients reach servers through OpenBot's account service, with no published port.

### Cited Findings
- Desktop apps are available for macOS 13+, Windows 10+ and Linux (AppImage). "Workspaces, conversations, files and browser data stay on the computer that runs OpenBot." — [openbot.run](https://openbot.run)
- The self-hosted server targets Ubuntu 24.04 or 26.04 (x86_64 or arm64) with systemd, and installs via a curl script that needs sudo. Docker is the alternative. Debian 12 and 13 were tested only for the package step. — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)
- The README says the server also runs as a container, with docs for AWS ECS and Umbrel. There is also a Nix package. — [GitHub README](https://github.com/nightly-labs/openbot), [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)
- An Unraid community-app listing exists. — [Unraid apps](https://ca.unraid.net/apps/openbot-1lok16u0sa40zu)
- Hosted servers are "a hosted EU server from €30 or $36 a month". — [Compare: OpenClaw](https://openbot.run/compare/openclaw)
- In 0.71.0 (14 Nov), new hosted servers can run on a Leaseweb virtual server, with "a boat sandbox as the fallback". — [Changelog](https://openbot.run/changelog)
- Clients reach a self-hosted server through the account service. "The server publishes no port." The phone connects "through the account server and Signal". — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)
- In 0.69.0, "Copy to another server" can move agents, and full server export and import were added. — [Changelog](https://openbot.run/changelog)
- On Ubuntu 23.10+ and Debian 13, an AppArmor profile is required or the app exits at launch. — [everydev.ai summary via search](https://www.everydev.ai/tools/openbot)

### Inferences
- *(inference)* Even self-hosted servers depend on OpenBot's Cloudflare-hosted account service for sign-in and client relay. It is not fully self-contained, unlike Silo, where the control plane is the whole stack.

### Gaps
- What the "boat sandbox" is: no description found.
- Whether the server can run fully offline or air-gapped without the account service: not documented.

## 2. Multi-user / teams / access control

### Takeaway
Multi-user support exists through **joined servers**: people sign in to a shared server with Owner, Admin and Member roles. A Permissions page grants MCP servers by agent and role, limits Vault items to chosen agents, and controls who may use remote desktop. It is young (most of it arrived in late October–November 2026) and coarser than Silo's Workspace/Bot role model.

### Cited Findings
- "Bring people and AI coworkers into one shared server." The page lists Local, Managed server and Custom deployment setups. It says nothing about SSO, audit or roles. — [Teams](https://openbot.run/teams)
- Joining servers first shows up in 0.53.0 (28 Oct 2026). — [Changelog](https://openbot.run/changelog)
- A sign-in claim makes the claimant the only owner. The owner can restart the server and toggle auto-updates. Owners, admins and permitted members can install updates. — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)
- 0.64.0 (7 Nov): owners and admins can set who may control the computer through remote desktop. 0.66.0 (9 Nov): the Roles page was renamed Permissions. — [Changelog](https://openbot.run/changelog)
- 0.69.0 (12 Nov): per-MCP-server access by agent and role (No access / Can use / Can manage). 0.70.0 (13 Nov): Vault items can be limited to chosen agents, and admins set agent defaults and thread limits per server. — [Changelog](https://openbot.run/changelog)
- 0.72.0 (16 Nov): an experimental CRM for owners and admins, off by default. Deleting agents now requires an owner-approved card. — [Changelog](https://openbot.run/changelog)
- Enterprise covers "more than 10 members". The Team license is priced per member. — [Pricing](https://openbot.run/pricing)
- "Anyone with the admin token can get a session for any email." — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)

### Inferences
- *(inference)* There is no separation between people inside one server. All agents run as one OS user (see §11), so roles are UI and API permissions, not isolation. Silo's equivalent wall is separate Bots and containers.

### Gaps
- SSO/OIDC, 2FA, audit log for people: not found.
- A full role and permission matrix: not documented.

## 3. Execution sandbox (container/VM, desktop, browser control, shell, code)

### Takeaway
By default agents run **on the host with full access**: `danger-full-access`, `approvalPolicy: never` after first-launch consent. On a server they run as one unprivileged service user. Isolation is limited to the vendor CLIs' own sandboxes (Claude's bwrap "Workspace only" mode on Linux), a separate virtual display for agents, and a sandboxed Electron session for the browser. Computer Use (desktop control) is optional on macOS, Windows and Linux.

### Cited Findings
- Agents run with "`danger-full-access` and `approvalPolicy: never`". "Full local access is an explicit current product decision, not a security boundary." — [GitHub README](https://github.com/nightly-labs/openbot)
- The homepage warns that agents "can read and change files, run commands and use the network without asking each time". — [openbot.run](https://openbot.run)
- On a server, agents run as a dedicated service user (default `openbot`) without sudo, at nice 10 with an OOM score of 500. They get a separate virtual display, and an AppArmor rule supports bwrap for Claude's "Workspace only" sandbox. "Agents already have full access to the service user's files, keyring, and database." — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)
- Optional Computer Use goes through a `cua-driver` binary. The embedded browser uses "a separate sandboxed Electron session". — [GitHub README](https://github.com/nightly-labs/openbot)
- Computer Use first shows up in 0.53.1 (28 Oct). 0.70.0 attaches screenshots to replies. 0.64.0 added per-session approval and "Stop all" for remote desktop control. — [Changelog](https://openbot.run/changelog)
- 0.70.0: "A Workspace only agent no longer asks for approval for each edit inside a folder added to it." 0.71.0/0.72.0: clearer errors when Claude or Codex cannot start its sandbox on Linux or Windows. — [Changelog](https://openbot.run/changelog)
- Linux remote desktop needs X11 and does not work under Wayland. — [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)
- The Docker image ships a remote-desktop display (0.65.0). WebGL works on Linux servers (0.66.0). — [Changelog](https://openbot.run/changelog)

### Inferences
- *(inference)* There is no per-agent container or VM. All agents on a machine share one OS user, filesystem and network. This is the sharpest contrast with Silo's one-container-per-Bot with a narrowed capability set.

### Gaps
- Whether hosted servers give each customer a VM: the Leaseweb mention suggests so (*inference*), but it is not documented.

## 4. Tools, MCP & integrations

### Takeaway
Tools come mostly from the wrapped agent CLIs (file edits, shell, and their own tools), plus OpenBot's browser, Computer Use, Vault and agent messaging. MCP is first-class as of November 2026: custom MCP servers live in Marketplace → Apps (edit, test, OAuth sign-in), up to 100 servers, with per-agent and per-role access. There is a catalog of app connectors (Notion, Figma, Linear, Obsidian, Canva, Aave, GitLab, Omnisend, ElevenLabs, Microsoft 365, Dropbox, Railway, Adobe Analytics and others).

### Cited Findings
- Listed plugins: Aave, Canva, Notion, Figma, Linear and Obsidian ("apps and skills an agent can use"). — [openbot.run](https://openbot.run)
- MCP first shows up in 0.52.0 (27 Oct). 0.64.0: OAuth sign-in for servers without client registration, and GitLab.com, Omnisend and ElevenLabs connect from the marketplace. 0.66.0: up to 100 MCP servers (was 32). 0.70.0/0.71.0: Microsoft 365, Dropbox and Railway sign-in fixes. 0.72.0: custom MCP servers in Marketplace → Apps with Edit, Test connection and Sign in. — [Changelog](https://openbot.run/changelog)
- 0.70.0 added Sunra, TryPost and Adobe Analytics connectors. 0.71.0 removed MongoDB Atlas. — [Changelog](https://openbot.run/changelog)
- Routines can be triggered by external services through webhooks, which need the host online. — [GitHub README](https://github.com/nightly-labs/openbot)

### Inferences
- *(inference)* There is no single tool gateway like Silo's `tool.Engine`. Each wrapped CLI runs its own tools, and OpenBot can gate only what passes through its layer (browser steps, Vault fills, MCP access).

### Gaps
- Whether MCP tool calls go through any OpenBot-side approval or rule: not documented.
- STDIO MCP isolation: not documented.

## 5. Skills / plugins / marketplace

### Takeaway
There is a Marketplace with Skills, Agents and Apps, opened via `openbot://` links. Skills install from public GitHub repos. Agents can **create and revise skills themselves** ("skill learning", with Automatic / Ask first / Off modes). "Teach a task" records browser steps as a skill.

### Cited Findings
- Agents use `list_local_skills`, `read_local_skill` and `install_local_skill` against a shared local library. The Marketplace offers Skills, Agents and Apps via `openbot://` links. — [GitHub README](https://github.com/nightly-labs/openbot)
- 0.66.0: agents create and revise skills automatically, and skill learning offers Automatic, Ask first and Off. 0.69.0: skills from public GitHub repositories, and "Teach a task" records browser steps and saves them as a skill. 0.72.0: skill-learning changes ask for confirmation first. — [Changelog](https://openbot.run/changelog)
- 0.63.0: the Usage page shows a Skill tab and skill-load cost estimates. — [Changelog](https://openbot.run/changelog)
- Plugin catalog: [openbot.run/marketplace/apps](https://openbot.run/marketplace/apps) (linked from the homepage, not fetched). — [openbot.run](https://openbot.run)

### Gaps
- Marketplace size and whether third parties can publish: not found.

## 6. Memory & personalization

### Takeaway
There is per-agent and per-channel memory with automatic **memory reviews** (background model passes at the lowest reasoning effort), memory search, and a Server Settings → Memory section (limits, review, skill learning). It first appears around 0.53.2 (28 Oct 2026). Context is auto-compacted. Agents imported from Hermes carry over their memories.

### Cited Findings
- Per-agent and per-channel memories. Context is compacted automatically before the model window fills. Imported agents carry their memories over. — [GitHub README](https://github.com/nightly-labs/openbot)
- Memory first shows up in 0.53.2 (28 Oct). 0.64.0: Codex agents get saved memories with their messages. 0.65.0: memory reviews run at the lowest reasoning effort. 0.66.0: memory search works for unspaced languages and beyond 10 results. 0.71.0: automatic reviews save memories in the user's language. 0.72.0: a Server Settings Memory section (limits, review, skill learning), and phones see each turn's memory changes. — [Changelog](https://openbot.run/changelog)
- Hermes settings, sign-ins and memories live in `~/.hermes` under the service user. — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)
- OpenBot's own OpenClaw comparison page describes no separate memory feature, only a SQLite store of workspaces and conversations. *(This page looks stale against the changelog.)* — [Compare: OpenClaw](https://openbot.run/compare/openclaw)

### Gaps
- Memory structure (kinds, tentative/verified, retirement), storage (vector or text) and prompt injection of memories: not documented.

## 7. Scheduling, automations, proactivity

### Takeaway
There are **Routines** (scheduled, per-timezone, run history with skip reasons, webhook triggers, "run in separate thread", self-pausing when the task says it is done) and **Workflows** (multi-step pipelines combining agents, models and approval steps, including image and video steps). Both appeared in late October 2026. Per-agent FIFO message queues can be paused, resumed or cancelled.

### Cited Findings
- Multi-step workflows combine agents, models and approval steps on a schedule ("Every Monday at 9:00"). — [openbot.run](https://openbot.run)
- FIFO queues support pause, resume and cancel. Routines can be triggered by webhooks (host must be online). — [GitHub README](https://github.com/nightly-labs/openbot)
- Routines first show up in 0.53.0 (28 Oct) and Workflows in 0.56.0 (30 Oct). 0.65.0: routines run again after the computer wakes. 0.66.0: workflow outputs can post to channels. 0.70.0: a routine pauses itself when its task says to stop. 0.71.0: stop runs from the Routines canvas, and quiet runs can end without a message. 0.72.0: create routines from the Schedule page, run history shows skips and reasons, and per-routine timezone. — [Changelog](https://openbot.run/changelog)
- Teams pitch: routines run "24/7 – even at night, on weekends or when your team is offline". — [Teams](https://openbot.run/teams)

### Gaps
- A proactivity or heartbeat concept like Silo's (the Bot acting unasked): not found.

## 8. Messaging channels

### Takeaway
Agents answer in **Slack, Discord, Telegram and WhatsApp**, with multiple Slack workspaces or Discord servers. Agents and routines can read connected chats, reply to specific messages, post Discord embeds, and ping users through mentions. Channels arrived around 0.52.0 (27 Oct). WhatsApp came later (the date is not pinned down). OpenBot also has its own internal "channels", which are shared chats with people and agents.

### Cited Findings
- "Agents answer in Slack, Discord, and Telegram", with multiple Slack workspaces or Discord servers. WhatsApp is not listed on this page. — [Compare: OpenClaw](https://openbot.run/compare/openclaw)
- Slack, Telegram and Discord first show up in 0.52.0 (27 Oct). — [Changelog](https://openbot.run/changelog)
- 0.64.0: mentions ping users on Telegram and WhatsApp, and Slack thread replies reach the orchestrator. 0.66.0: Discord embeds. 0.69.0: agents and routines can read connected Slack, Discord, Telegram and WhatsApp chats, and a WhatsApp number can turn off its "Working on it…" message. 0.72.0: replies to specific messages on all four, WhatsApp keeps allowed chats' messages for 90 days, and status reactions can be turned off. — [Changelog](https://openbot.run/changelog)
- Internal shared channels have "one task owner" and explicit delegation, with Reassign and Archive. — [GitHub README](https://github.com/nightly-labs/openbot)

### Gaps
- Whether WhatsApp is the unofficial linked-device protocol or the Business API: not documented.
- Signal, iMessage, Matrix, Teams and email: not found, and OpenBot's own comparison concedes that OpenClaw covers more channels.

## 9. Voice

### Takeaway
Voice is well developed by November 2026. It covers dictation (local Parakeet, plus Whisper large-v3 turbo, plus Groq), read-aloud (local voices in 31 languages, ElevenLabs incl. Eleven v4, xAI), a Voice pane with barge-in, and **"Codex live voice"** / GPT voice calls built on OpenAI's realtime voice, which also work on joined and hosted servers.

### Cited Findings
- "You can dictate messages or talk to an agent in the Voice pane and interrupt it by speaking. Local models handle speech-to-text and read replies aloud in 31 languages, so only text reaches the agent." — [Compare: OpenClaw](https://openbot.run/compare/openclaw)
- Dictation first shows up in 0.55.0 (30 Oct). 0.64.0: Codex live voice on joined and hosted servers. 0.65.0: read-aloud with ElevenLabs and xAI, and Groq dictation. 0.66.0: Eleven v4, and long replies summarized for live voice. 0.71.0: live voice waits for the end of a sentence, and Whisper large-v3 turbo covers languages Parakeet does not support. 0.72.0: Enter sends a voice message, the default pause before sending is 1.5 s, and GPT voice calls read out other chat answers. — [Changelog](https://openbot.run/changelog)

### Inferences
- *(inference)* "Codex live voice" appears to reuse the Codex/ChatGPT realtime voice from the user's plan. If so, live calls are tied to OpenAI, whereas Silo's call pipeline is provider-agnostic (its own STT, talker and TTS over WebRTC).

### Gaps
- Phone or PSTN calling, and voice on Telegram or WhatsApp: not found.

## 10. Clients (web, mobile, desktop, CLI)

### Takeaway
Desktop apps for macOS, Windows and Linux are the primary surface. There are also an iPhone app (with Live Activities and Face ID/PIN lock), an Android app, a web client at `/app`, and a small CLI for server setup. Phones and the web connect from anywhere through OpenBot's relay with no VPN. An optional "private network mode" limits connections to Tailscale or the same Wi-Fi.

### Cited Findings
- Desktop apps for macOS 13+, Windows 10+ and Linux, iPhone and Android apps that connect to your computer, and a web app at openbot.run/app. — [openbot.run](https://openbot.run), [App Store](https://apps.apple.com/us/app/openbot-run/id6807024266), [Google Play](https://play.google.com/store/apps/details?id=run.openbot.mobile)
- Phone, iPhone and web apps connect over an encrypted connection with "no VPN or tunnel". Private network mode allows only Tailscale or the same Wi-Fi. The app locks with Face ID, Touch ID or PIN. — [Compare: OpenClaw](https://openbot.run/compare/openclaw)
- CLI: `openbot login` and `openbot` commands for server setup. — [GitHub README](https://github.com/nightly-labs/openbot)
- Mobile Connect pairing uses a QR code that is single-use and expires in 2 minutes. — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)
- 0.70.0: iPhone Live Activity stays current with several agents running. 0.72.0: the dock chat answers requests, questions and browser steps. — [Changelog](https://openbot.run/changelog)

## 11. Security & safety

### Takeaway
The security posture is explicitly permissive. Agents get full access with no per-action approvals by default, and all agents share one OS user, so they can read each other's files and API keys. OpenBot's controls are a **Vault** (OS-encrypted logins, cards, API keys and TOTP; agents see only a card's name and last four digits; first card fill on a site needs approval; items can be scoped to agents), approval cards for some actions (deleting agents, remote-desktop sessions, workflow approval steps, skill learning), and approvals from the phone. I found **no prompt-injection output filter, no egress network filter, and no rules engine** comparable to Silo's.

### Cited Findings
- Agents run with "danger-full-access and approvalPolicy: never". "Full local access is an explicit current product decision, not a security boundary." — [GitHub README](https://github.com/nightly-labs/openbot)
- The homepage advises giving agents only trusted tasks and keeping backups. — [openbot.run](https://openbot.run)
- The Vault stores logins and cards encrypted by the OS. Agents see only a card's name and last four digits, the first card fill on each site needs approval, and actions can be approved from the phone. — [Compare: OpenClaw](https://openbot.run/compare/openclaw)
- 0.66.0: TOTP codes in the Vault. 0.70.0: per-agent API keys go into the encrypted Vault, and Vault items can be scoped to agents. 0.71.0: the Vault "now states that all agents can read each other's API key files, because they run as the same system user." — [Changelog](https://openbot.run/changelog)
- Agents have full access to the service user's files, keyring and database. "Anyone with the admin token can get a session for any email." Claim tokens are one-time, last 15 minutes and are stored hashed. The control socket is mode 0600. — [self-hosted-server.md](https://github.com/nightly-labs/openbot/blob/main/docs/self-hosted-server.md)
- ExplainX lists the risks: prompt injection from browsed pages or issues can lead to host actions, and agent-to-agent messaging widens the blast radius. It recommends running OpenBot in a VM or Docker. — [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)
- AlphaSignal likens the access model to giving an agent SSH access to your machine. — [AlphaSignal](https://alphasignal.ai/news/nightly-labs-openbot-runs-multiple-coding-agents-from-one-desktop-app)
- Product analytics records webhook requests without URLs or bodies (0.63.0). *(Telemetry exists.)* — [Changelog](https://openbot.run/changelog)

### Inferences
- *(inference)* Per-agent Vault scoping is advisory while every agent runs as the same OS user. OpenBot's own 0.71.0 note admits this for API key files.

### Gaps
- Third-party audit or pen test: none found. The wavect.io review says it was documentation-only and "did not deploy or penetration-test OpenBot" ([wavect.io](https://wavect.io/blog/openbot-self-hosted-ai-coworkers-review.md); the snippet came via search, and I could not confirm whether it reviews this OpenBot or CopilotKit's).

## 12. Model providers & cost/token efficiency

### Takeaway
Breadth is very wide through vendor CLIs and ACP: Codex, Claude Code, Gemini (via Antigravity CLI), Grok, OpenCode, Cursor, Cline, Kimi, Devin, GitHub Copilot, Mistral Vibe, Hermes, Muse Code, Pi and Mercury, plus any ACP agent, OpenAI-compatible endpoints and Ollama/LM Studio. The cost strategy is **reuse of consumer subscriptions** with cross-provider failover and "pool accounts" that rotate accounts as usage limits approach. A Usage page shows per-turn cost. I found no token-conservation design (prompt-cache discipline and similar).

### Cited Findings
- Providers listed: Codex, Claude Code, Gemini, Grok, OpenCode, Cursor, Cline, Muse Code, Pi and Mercury, plus ACP agents, OpenAI-compatible endpoints and Ollama/LM Studio. — [openbot.run](https://openbot.run)
- The README adds Kimi, Devin, GitHub Copilot, Mistral Vibe and Hermes (Nous Portal). Gemini goes through Google's Antigravity CLI. — [GitHub README](https://github.com/nightly-labs/openbot)
- Failover: an agent switches to a backup on another provider when its plan hits a limit or the provider is down, keeping its chat and workspace. Failover first shows up in 0.53.0. — [openbot.run](https://openbot.run), [Changelog](https://openbot.run/changelog)
- 0.69.0: "Pool accounts for a provider, which switch accounts as usage limits approach." 0.70.0: Claude Ultrafast and fast mode. 0.71.0: ClinePass models, and Gemini cached-context cost display. 0.64.0: Usage costs for Pi, DeepSeek, OpenRouter and Inception. — [Changelog](https://openbot.run/changelog)
- Caveats: Gemini needs a Google AI Pro or Ultra plan. Cline's free models may train on prompts. ExplainX flags Claude subscription use inside a wrapper as a terms-of-service question. — [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)

### Inferences
- *(inference)* Pooling accounts to dodge usage limits on consumer plans is likely to conflict with some providers' terms. That is a fragility Silo avoids by defaulting to API keys and keeping its Claude subscription path to the operator's own use.

## 13. Multi-agent / subagents

### Takeaway
This is the central concept. You run a **team of named agents**, each with its own instructions, workspace and queue. Agents message each other (replies, reactions), hand off tasks and share files, and shared channels have a single task owner with delegation. Subagents come from the wrapped CLIs: OpenCode background subagents can run up to 4 hours, and Codex subagents can run inside routines.

### Cited Findings
- "Each agent has its own name, instructions and workspace. Agents message each other, hand off tasks and share files." — [openbot.run](https://openbot.run)
- Agent-to-agent messages, replies and reactions. Shared channel chats have one task owner and explicit delegation (Reassign, Archive). — [GitHub README](https://github.com/nightly-labs/openbot)
- Subagents first show up in 0.52.1 (27 Oct). 0.71.0: OpenCode background subagents up to 4 h, and Codex subagents inside routine runs. 0.66.0: channel members answer while working in their own chats. — [Changelog](https://openbot.run/changelog)

### Inferences
- *(inference)* OpenBot's "agents" are closer to Silo's Bots (durable personas) than to Silo's subagents. The difference is that OpenBot agents share one machine and can talk to each other directly, while Silo Bots are isolated containers.

## 14. Web search, browsing

### Takeaway
There is a persistent **embedded browser** (an Electron session) that agents open, inspect and control, with named profiles that can be assigned to agents. It handles bot-check pages, and the Vault fills logins, cards and TOTP. Web search is whatever the wrapped CLI provides. I found no OpenBot-native search engine or page reader.

### Cited Findings
- A persistent embedded browser that agents can "open, inspect, and control". Named browser profiles keep cookies and storage separate. — [GitHub README](https://github.com/nightly-labs/openbot)
- 0.65.0: bot-check pages are handled, and idle tabs use less memory. 0.66.0: large pages are read more completely. 0.71.0: pinch zoom and a full browser view. 0.72.0: browser profiles assignable to agents, and sign-in cards explain when a site refuses sign-in from a server. — [Changelog](https://openbot.run/changelog)

### Gaps
- Native web-search tool: not found.

## 15. Output surfaces

### Takeaway
Output goes to chat in the desktop, web and phone apps, with screenshots attached to replies, Grok image and video generation inline, workflow outputs posted to channels or Slack/Discord/Telegram/WhatsApp, iPhone Live Activities, and an experimental CRM. I found no dashboard, artifact store with versions, live HTML apps, or public share links.

### Cited Findings
- 0.69.0: Grok agents create images and videos in chat. 0.70.0: screenshots attached to replies. 0.66.0: workflow outputs post to channels, and Discord embeds. 0.72.0: CRM (experimental). — [Changelog](https://openbot.run/changelog)

### Gaps
- Artifact, preview or share features: not documented in what I read.

## 16. Adoption & ecosystem

### Takeaway
Traction is quick but early. The repo has about 8.6k GitHub stars by mid-November 2026, up from about 1.2k in late October, along with 836 forks, about 2.9k issues and over 4.3k commits. There was a Product Hunt launch with modest engagement, Discord and subreddit communities, and an Unraid app. I found no meaningful independent reviews or HN/Reddit discussion.

### Cited Findings
- 8.6k stars, 836 forks, 22 watchers, 4,363 commits, about 2.9k issues and 377 PRs. "A new release ships almost every day." — [GitHub](https://github.com/nightly-labs/openbot)
- About 1.2k stars as of 26 Oct, with about 312 in one day while trending, 139 open issues and 37 open PRs. — [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)
- Product Hunt listings showed about 61 and 95 followers, and the launch comment came from a builder. — [Product Hunt](https://www.producthunt.com/products/openbot) (figures via the search summary)
- Community: [Discord](https://discord.openbot.run), [r/OpenBot_](https://www.reddit.com/r/OpenBot_/), [X @OpenBot_](https://x.com/OpenBot_), and a status page. — [openbot.run](https://openbot.run)
- Most third-party coverage is directory or SEO sites (toolradar, aitoolly, everydev, aiindigo, hunted.space). — [toolradar](https://toolradar.com/tools/openbot), [aitoolly](https://aitoolly.com/ai-news/article/2026-10-29-openbot-by-nightly-labs-local-first-desktop-workspace-enabling-multi-agent-collaboration-with-claude)

### Inferences
- *(inference)* About 2.9k issues against 377 PRs suggests heavy user bug-reporting. The app files phone bug reports, and owners can "remove reports from GitHub" (0.72.0), so many issues may be automated reports rather than an organic community.

### Gaps
- User counts and downloads: not published.

## 17. Known weaknesses / complaints

### Takeaway
The main criticisms are the full-access, no-approval security model with all agents under one OS user, development-preview maturity with daily releases and churn, the noncommercial license (it is not open source), dependence on OpenBot's account service and relay, provider terms-of-service risk from wrapping consumer subscriptions, Linux friction (AppArmor/bwrap, X11 only), and fewer channels than OpenClaw.

### Cited Findings
- Development preview, full host access, prompt-injection risk, agent-to-agent blast radius, ToS questions for subscription wrapping, Wayland unsupported, and no benchmarks. — [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)
- The Nix section calls the license "not free software". — [ExplainX](https://explainx.ai/blog/nightly-labs-openbot-desktop-workspace-codex-claude-grok-agents-danger-full-access-polyform-noncommercial-2026)
- Agents can read each other's API key files (same system user). — [Changelog 0.71.0](https://openbot.run/changelog)
- The AppArmor profile is required on Ubuntu 23.10+ and Debian 13, or the app exits. — [everydev.ai](https://www.everydev.ai/tools/openbot)
- OpenBot's own comparison shows OpenClaw supporting many more channels (Signal, iMessage, Teams, Matrix, and 20+ others). — [Compare: OpenClaw](https://openbot.run/compare/openclaw)

### Gaps
- No user complaints found on Reddit or HN, likely because of low organic discussion so far.

## 18. Pricing & license

### Takeaway
Personal or noncommercial use of the desktop app is €0 with every feature included. You pay only your model provider or plan. Commercial use needs a license: €10/month for 1 member, €25/month for 2 members plus about €10.80 per extra member (yearly 20% off). The license is **free until 1 Dec 2026**. Hosted servers cost €30, €50 or €100 per month (Starter, Standard, Pro), with Enterprise priced on request. The code license is PolyForm Noncommercial 1.0.0, with a CLA. Versions ≤0.1.11 were Apache-2.0.

### Cited Findings
- Free personal tier at €0. Hosted Starter €30/month (€24 yearly, 25 GB), Standard €50/month (€40 yearly, 50 GB, 2× speed), Pro €100/month (€80 yearly, 100 GB, 4× speed), Enterprise custom (>10 members). Commercial Starter license €10/month (€7.20 yearly, 1 member). Team license €25/month (€18 yearly, 2 members, plus €10.80 per extra member). 10% bundle discount with a server. Commercial license free until 1 Dec 2026. Prices also shown in USD, PLN and BRL. — [Pricing](https://openbot.run/pricing)
- PolyForm Noncommercial 1.0.0, versions up to 0.1.11 remain Apache-2.0, and a CLA applies. — [GitHub README](https://github.com/nightly-labs/openbot), [CLA](https://openbot.run/cla), [Commercial license](https://openbot.run/commercial-license)
- No credit system. You pay the AI provider directly. — [openbot.run](https://openbot.run), [Pricing](https://openbot.run/pricing)

### Inferences
- *(inference)* Any company using it for work, including self-hosting, needs a paid license after 1 Dec 2026. A permissively licensed self-hosted product could contrast with that.

### Gaps
- Whether the extra-member price is monthly or yearly is ambiguous on the page.
