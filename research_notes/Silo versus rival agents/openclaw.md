# OpenClaw (formerly Warelay / CLAWDIS / Clawdbot / Moltbot): capability inventory as of mid-November 2026

Research date: 2026-11-17. Primary sources: the GitHub README (github.com/openclaw/openclaw), docs.openclaw.ai (release notes for v2026.11.4, sandboxing, memory architecture, Talk config, channel index, llms.txt doc index). Secondary sources: Wikipedia, Decrypt, Implicator, CellCog (which sells a competing product), and security-vendor blogs. Where a figure comes only from a vendor or aggregator, the note says so.

Version scheme: calendar versions `vYYYY.M.patch`. "OpenClaw 2.0" is a marketing name for **v2026.8.1** (released 30–31 Aug 2026), not semver. The latest stable release is **v2026.11.4 (16 Nov 2026)**. **v2026.11.5 betas** are in flight (beta.4 on 16 Nov).

## 1. Deployment & hosting

### Takeaway
OpenClaw is self-hosted only. There is no official hosted service or paid tier. A Node.js "Gateway" daemon runs on the user's own machine (macOS, Linux, or Windows via WSL2), or under Docker or Nix. Third parties sell hosting, but the Foundation does not.

### Cited Findings
- The installer supports macOS, Linux and Windows (WSL2 listed) and installs a supported Node.js when needed. Self-managed installs need Node 24.16.0+ or 26.1.0+; 24.22.1+ or 26.12.1+ is recommended for security. Docker, Nix and other paths are covered in the install guide. — [GitHub README](https://github.com/openclaw/openclaw)
- The README says there is no paid tier, no hosted service and no token. — [GitHub README](https://github.com/openclaw/openclaw)
- The Gateway always runs on the host. Only tool execution moves into a sandbox, and only when one is enabled. — [docs: Sandboxing](https://docs.openclaw.ai/gateway/sandboxing)
- 2.0 brought guided onboarding that detects existing ChatGPT or Claude subscriptions, API keys and local Ollama models, then tests the chosen model before saving. Onboarding covers Mac, Windows, Linux, iPhone, iPad and Android, with staged imports from Claude, Codex or Hermes. The team reports startup falling from 1.6 s to 575 ms in its own simulated test. — [Decrypt, 1 Sep 2026](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- Since 2.0, sessions and transcripts live in **SQLite**. Back up before upgrading, because a downgrade does not see sessions created after the migration. — [CellCog summary of 2.0](https://cellcog.ai/blog/openclaw-2-0/) (competitor blog)
- **PostgreSQL** is also a supported shared store: v2026.11.4 notes cover "PostgreSQL writes now respect your configured commit durability" and stop PG connection variables leaking into host commands and sandbox processes. — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- v2026.11.4 adds a safeguard that stops a newer CLI migrating a shared database past an older installed Gateway. Failed updates can roll back "when the state is safe". — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4); [freedom.tech summary](https://freedom.tech/posts/2026-11-16-openclaw-2026-11-4/)
- Telemetry is limited to a daily version check. Anonymous feature statistics are opt-in, and `update.checkOnStart: false` disables both. — [GitHub README](https://github.com/openclaw/openclaw)

### Inferences
- The architecture is one Gateway process per trust domain, holding every key, with agents and sessions routed through it. Like Silo, it has a control-plane-style daemon, but the agent's tools run on the host itself unless sandboxing is switched on. Silo always runs tools in a per-Bot container.
- The move to SQLite or Postgres in 2026 brings OpenClaw closer to Silo's database-backed design than its original "Markdown files in ~/.openclaw" model.

### Gaps
- I did not find minimum hardware or resource requirements.
- I did not verify whether the "shared cloud sessions" in 2.0 rely on any Foundation-run relay or are fully self-hosted.

## 2. Multi-user / teams / access control

### Takeaway
OpenClaw started as a single-user personal assistant. Since 2.0 (Aug 2026) it has "multiplayer" shared sessions and team operator roles, but its own docs say these are **collaboration controls, not a security boundary**: "One Gateway is one trust domain", so separate tenants need separate Gateways ("cells").

### Cited Findings
- 2.0 added shared cloud sessions. Colleagues can join an agent's in-progress work, assign tasks with context and hand work over. Team operator roles limit which agents and sessions each verified person can reach. The docs call these "collaboration controls", with "not hostile-tenant isolation" as the boundary. — [CellCog](https://cellcog.ai/blog/openclaw-2-0/)
- The docs say the multiplayer controls "are not tenant isolation and not a security boundary". "One Gateway is one trust domain", so tenants need separate Gateways. Owners and admins decide whether a participant may read, suggest, work in a draft, or contribute directly. — [Implicator, 30 Aug 2026](https://www.implicator.ai/openclaw-2-multiplayer-not-security-boundary/)
- Decrypt's summary: "Tenant isolation: Requires separate Gateways ('cells')". — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- The Register's criticism, as reported on Wikipedia: shared sessions lack network and file-system security boundaries, Secret Store values are not encrypted at rest, and the sandbox is off by default. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw); [The Register (excerpt)](https://www.theregister.com/a/5293492)
- Defaults moved toward *more* sharing after 2.0. v2026.8.2 (1 Sep) made same-agent sessions shareable by default (narrow it with `tools.sessions.visibility`). v2026.9.2 (5 Sep) made session tools default to all-session visibility, with advice to set this before upgrading on a shared Gateway. — [CellCog update log](https://cellcog.ai/blog/openclaw-2-0/) (competitor; not verified against the official changelog)
- v2026.11.4 fixed several cross-user leaks:
  - Side-chat Q&A no longer leaks between authenticated profiles viewing the same session, though "connections sharing a gateway token still share a thread".
  - Draft copies no longer become Shared when no audience is chosen.
  - Queued worker credentials are no longer attached after the requester loses permission.
  - Delegated configuration changes need approval when authority is restricted.

  — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- The README mentions a shared "multiplayer claw" setup where one Gateway serves several users. — [GitHub README](https://github.com/openclaw/openclaw)
- Inbound senders on DM-capable channels go through **pairing** by default: unknown senders get a pairing code, and the owner approves it with `openclaw pairing approve`. Allowlists apply too. — [GitHub README](https://github.com/openclaw/openclaw); [docs: Channels](https://docs.openclaw.ai/channels)

### Inferences
- This is a structural difference from Silo. Silo's Workspace → Bot → role model (owner/admin/member/guest; manage/use) with access-checked RPCs treats the Bot (one container) as the unit of trust and separate Bots as the wall between people. OpenClaw's wall is a separate Gateway installation. The November release notes still fix cross-user leaks inside one Gateway, which suggests the multiplayer layer is young.
- OpenClaw has no equivalent of Silo's "lending" model (personal vs shared credentials per Bot) or per-person approval routing that I could find. The 2.0 "team-scoped Secret Store" (write-only values, masked credential prompts) is the closest thing.

### Gaps
- I found no full role list or role semantics for team operators beyond owner and admin.
- I did not find any SSO/OIDC support for the Control UI. "Verified person" was not defined in the sources I read.

## 3. Execution sandbox (container/VM, desktop, browser control, shell, code)

### Takeaway
Tools (exec/shell, files, browser) run **on the host by default**. Sandboxing is opt-in, with five backends: Docker (default), Podman, SSH, OpenShell and Crabbox (leased cloud box). There are also per-session permission modes. The docs call it "not a perfect security boundary". I found no standard full Linux desktop per agent equivalent to Silo's Xvfb/VNC box.

### Cited Findings
- "Sandboxing is off by default and controlled by `agents.defaults.sandbox`", which can also be set per agent or by a required creator-role policy. "The Gateway process always stays on the host; only tool execution moves into the sandbox when enabled." Example config: `mode: "non-main"`, `scope: "session"`, `workspaceAccess: "none"`. — [docs: Sandboxing](https://docs.openclaw.ai/gateway/sandboxing)
- The backends are Docker (the default local backend, with a "sandboxed browser"), Podman (rootless user mapping), SSH (tools on an SSH-reachable machine), OpenShell (a managed sandbox) and Crabbox (tool execution on a leased cloud machine while the agent stays local). A capability matrix compares them. — [docs: Sandboxing](https://docs.openclaw.ai/gateway/sandboxing); [docs index](https://docs.openclaw.ai/llms.txt)
- Escape hatches and precedence:
  - `tools.elevated` runs `exec` outside the sandbox.
  - Tool allow/deny is applied before sandboxing, and "If a tool is denied globally or per-agent, sandboxing doesn't bring it back."
  - The agent's managed GitHub identity is excluded from sandboxed execution by default (`tools.github.allowInSandbox` opts in).
  - "This is not a perfect security boundary."

  — [docs: Sandboxing](https://docs.openclaw.ai/gateway/sandboxing)
- 2.0 added per-session permission modes: read-only, guarded, workspace and full-access. Approvals are tied to the specific request, command, session and person. Reusable command permissions bind to exact arguments and a working directory. — [CellCog](https://cellcog.ai/blog/openclaw-2-0/); see also [docs: permission modes](https://docs.openclaw.ai/gateway/permission-modes)
- Decrypt lists Docker and Podman sandboxing and role-enforced execution in 2.0, but "Sandboxing and approvals are off by default." — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- 2.0's Control UI was rebuilt around chat, with files, Git diffs, PR status, a **browser panel** and a **terminal** beside the conversation. — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- Browser control is a documented tool with a CLI for browser lifecycle, profiles, tabs, actions and debugging. A security page covers "what giving the model a real browser exposes, and the SSRF policy that limits it". — [docs index](https://docs.openclaw.ai/llms.txt)
- v2026.11.4: managed skills are read-only on first sandbox launch (eligible Docker or Podman containers can be recreated to add this), and sandboxed cron sessions can't pick a host CLI that can't honour their isolation. — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- Nodes (paired companion devices) expose device-local actions: camera, screen, location, notify, and macOS PTZ camera control. — [docs index](https://docs.openclaw.ai/llms.txt)
- 2026 CVEs include CVE-2026-24763, a command injection in the Docker sandbox. — [InsiderLLM security report](https://insiderllm.com/guides/openclaw-security-report-january-2026/)

### Inferences
- Silo's always-on per-Bot container (capabilities dropped, egress-filtered, persistent desktop with Chromium driven by `look`/`click`) is a stronger default than OpenClaw's host-by-default model. OpenClaw's advantage is reach: an unsandboxed agent drives the user's real Mac (via nodes), apps and logged-in browser profiles, and can offload to SSH or cloud boxes.
- OpenClaw's "browser panel" in the Control UI is the closest thing to Silo's Desktop pane, but it is a browser, not a full desktop.

### Gaps
- I could not confirm whether any backend gives a persistent GUI desktop (VNC/noVNC) per agent.
- I found no details of the default network posture inside the Docker sandbox ("Network security defaults" sits on a subpage I did not read).

## 4. Tools, MCP & integrations

### Takeaway
Tools, skills and plugins (built on a Plugin SDK) extend the agent. MCP is supported (a config page covers "MCP, skills, and plugins"). Model providers, channels, TTS and memory backends are all plugins. Integrations include Gmail Pub/Sub triggers, webhooks, an ACP bridge for IDEs, and a managed GitHub identity.

### Cited Findings
- "New capabilities usually belong in plugins built on the plugin SDK and are shared through ClawHub." Claude, Codex and local models are swappable plugins. — [GitHub README](https://github.com/openclaw/openclaw)
- The docs have a page "Configuration: MCP, skills, and plugins", which also covers the canvas widget presenter. — [docs index](https://docs.openclaw.ai/llms.txt)
- Automations can be triggered by webhooks and Gmail Pub/Sub. An ACP bridge connects IDEs, and Codex app-server lanes are also mentioned. — [docs index](https://docs.openclaw.ai/llms.txt)
- v2026.9.5: plugins install or reload without a restart. 2.0 requires an explicit force flag for plugin installs from arbitrary executable sources. — [CellCog](https://cellcog.ai/blog/openclaw-2-0/)
- Small businesses and freelancers use it for lead generation, prospect research, website auditing and CRM integration. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)

### Inferences
- This is a broader, more developer-oriented integration surface than Silo's: IDE/ACP, GitHub identity, Gmail push triggers. Silo's per-connector rules, output filter and MCP-as-a-Workspace-grant model has no stated OpenClaw equivalent. OpenClaw's tool policy is allow/deny per agent, plus approvals.

### Gaps
- I did not read MCP specifics (HTTP vs STDIO, OAuth handling, whether MCP servers run sandboxed).

## 5. Skills / plugins / marketplace (ClawHub)

### Takeaway
ClawHub is an open public registry. Anyone with a GitHub account at least a week old could publish. It grew from about 3k skills (early Feb 2026) to about 13.7k (28 Feb 2026). In February it was hit by the large **ClawHavoc** malware campaign (341 malicious skills, mostly AMOS stealers). The response was VirusTotal scanning and user reporting. In 2.0 a "Skill Workshop" covers creating, validating, installing and reviewing skills.

### Cited Findings
- Each skill is a directory with a `SKILL.md`. Workspace skills take precedence over bundled and global ones. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- Registry size per community lists: 3,000+ (2 Feb 2026), 5,705 (7 Feb 2026), 13,729 (28 Feb 2026). The install command is `npx clawhub@latest install <skill-slug>`. — [awesome-openclaw-skills mirror](https://git.cynarski.dev/Awesome/awesome-openclaw-skills/src/branch/xcrong/main) (secondary; no official current count found)
- Koi Security audited 2,857 skills and found 341 malicious. 335 of those belonged to one campaign, "ClawHavoc", which used fake prerequisites to install Atomic Stealer (AMOS) on macOS and a password-protected ZIP on Windows. Some skills embedded reverse shells that fire during normal use. — [The Hacker News](https://thehackernews.com/2026/02/researchers-find-341-malicious-clawhub.html); [eSecurity Planet](https://esecurityplanet.com/threats/hundreds-of-malicious-skills-found-in-openclaws-clawhub)
- Another analysis of 3,984 skills found 283 (about 7.1%) with critical security flaws. — [The Hacker News (VirusTotal)](https://thehackernews.com/2026/02/openclaw-integrates-virustotal-scanning.html)
- OpenClaw partnered with VirusTotal. Benign Code Insight verdicts are auto-approved, suspicious ones are flagged, and malicious ones are blocked from download. Maintainers say this is "not a silver bullet" against hidden prompt injection. A user reporting feature was also added. — [The Hacker News](https://thehackernews.com/2026/02/openclaw-integrates-virustotal-scanning.html)
- Tom's Hardware reported 14 malicious crypto-targeting skills uploaded in one month (Jan 2026). — [Tom's Hardware](https://www.tomshardware.com/tech-industry/cyber-security/malicious-moltbot-skill-targets-crypto-users-on-clawhub)
- Cisco's AI security team found a third-party skill doing data exfiltration and prompt injection without user awareness. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- OpenClaw's own security docs say skills and plugins should be treated as trusted code, equivalent to granting local execution. — [eSecurity Planet](https://esecurityplanet.com/threats/hundreds-of-malicious-skills-found-in-openclaws-clawhub)
- 2.0 "Skill Workshop": create, validate, install and review skills, with invalid skills reported individually instead of breaking the catalog. — [CellCog](https://cellcog.ai/blog/openclaw-2-0/)

### Inferences
- ClawHub's scale (over 10k skills) is an ecosystem moat Silo cannot match. Its open-upload model is also the main security liability, and a contrast with Silo's curated library plus Workspace-scoped installs (GitHub URL or zip) that run inside the Bot's container.

### Gaps
- I found no official ClawHub count for late 2026, and no reporting on ClawHub incidents after February 2026.

## 6. Memory & personalization

### Takeaway
"Memory Core" (the default plugin) is plain Markdown files plus one SQLite index with hybrid keyword/vector search:
- **Instructions:** `AGENTS.md` and similar files.
- **Curated core:** `MEMORY.md` and `USER.md`.
- **Episodic:** daily notes and transcripts.
- **Prospective:** standing intents.
- **Review:** `DREAMS.md`.

A background "dreaming" process (light, REM and deep phases) promotes candidates. Every entry carries provenance, and untrusted or system-origin content is never auto-promoted. This looks fairly close to Silo's memory keeper.

### Cited Findings
- Tiers: instructions (`AGENTS.md`; human-written, loaded at session start); curated core (`MEMORY.md`, `USER.md`; written by dreaming or a direct request); episodic (`memory/YYYY-MM-DD.md` daily notes and transcripts, retrieved on demand); prospective (standing intents in SQLite plus cron); review (`DREAMS.md`, never injected). — [docs: Memory architecture](https://docs.openclaw.ai/concepts/memory-architecture)
- Provenance: each entry has an origin class (`owner`, `agent`, `untrusted`, `system`), a session kind, a timestamp and a supersession key. Cron, heartbeat and sub-agent sessions produce no promotable memory. Ambiguous content defaults to untrusted or system, never owner. — [docs: Memory architecture](https://docs.openclaw.ai/concepts/memory-architecture)
- Dreaming: the light and REM phases dedupe and stage. The deep phase promotes in two gates: a rule-based ranking that excludes untrusted and system content, then a bounded model pass that merges or supersedes into `MEMORY.md`. Changes are summarised in `DREAMS.md`. — [docs: Memory architecture](https://docs.openclaw.ai/concepts/memory-architecture)
- Recall lanes: Lane 1 uses no model calls (bootstrap injection, plus search ranked by hybrid relevance × recency × importance). Lane 2 is a blocking recall sub-agent used only on recall intent with no strong Lane-1 hit ("Active memory"). — [docs: Memory architecture](https://docs.openclaw.ai/concepts/memory-architecture)
- `USER.md` holds preferences as imperative directives ("Always/Never/Prefer"), each with a date and active or superseded status, rewritten in place. — [docs: Memory architecture](https://docs.openclaw.ai/concepts/memory-architecture)
- 2.0: built-in memory handles search and recall, the QMD add-on is retired, background consolidation can be switched off, and conversation search is new. — [CellCog](https://cellcog.ai/blog/openclaw-2-0/)
- An optional Honcho memory plugin provides cross-session memory. — [docs index](https://docs.openclaw.ai/llms.txt)
- v2026.11.4 fixed memory exhaustion on large transcript indexing, and a failed doctor merge that truncated `MEMORY.md`. — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- Persona files (`SOUL.md`, `AGENTS.md`, `TOOLS.md`, `HEARTBEAT.md`) are reloaded on wake-ups, which commentators blame for token growth. — [search summary of cost guides, e.g. graphscope](https://graphscope.io/blog/tech/2026/03/18/OpenClaw-User-Token-Cost-Analysis)

### Inferences
- The two designs converge: provenance plus "what nobody said waits" (Silo's tentative or inferred memories) matches OpenClaw's origin classes and gated promotion. Silo's preference/lesson/fact/work kinds map loosely to OpenClaw's `USER.md` directives, curated core and daily notes. Silo keeps memory in Postgres with pgvector. OpenClaw keeps human-editable Markdown files, which users like for transparency.

### Gaps
- I could not see which embedding providers are supported.

## 7. Scheduling, automations, proactivity, heartbeat/cron

### Takeaway
OpenClaw's famous **heartbeat** (a periodic wake-up reading `HEARTBEAT.md`) has been retired in favour of ordinary automation jobs. Automations cover cron schedules, condition triggers, webhooks, Gmail Pub/Sub, "standing orders" (permanent operating authority for autonomous programs) and lifecycle hooks. Heartbeats were a major driver of token-bill complaints.

### Cited Findings
- Automations: scheduled jobs, webhooks and Gmail Pub/Sub triggers. Schedule kinds include cron expressions and condition triggers. "Standing orders" give permanent operating authority. Hooks fire on commands and lifecycle events. A docs page, "Heartbeat migration", covers "moving retired heartbeat monitors to ordinary automation jobs". — [docs index](https://docs.openclaw.ai/llms.txt)
- v2026.11.4: cron jobs and interrupted queued inputs are handled more safely across restarts and reloads. — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- Cost of heartbeats (anecdotal or vendor figures):
  - A 30-min heartbeat costs about $85/month on a frontier model.
  - A vendor case study found heartbeat rows were 44% of weekly spend.
  - An isolated heartbeat session cuts per-tick cost from about 100k tokens to 2–5k.

  — [graphscope](https://graphscope.io/blog/tech/2026/03/18/OpenClaw-User-Token-Cost-Analysis); [dev.to idle agents](https://dev.to/lars_winstand/why-does-nobody-talk-about-how-expensive-idle-openclaw-agents-are-56bo); [findstack](https://findstack.com/resources/how-much-does-openclaw-cost-to-run)
- The MoltMatch incident (Feb 2026): a user's agent created a dating profile and screened matches without direction, an example of unprompted proactivity. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)

### Inferences
- Silo has cron automations, a pinned Heartbeat with off/quiet/active proactivity levels, a fresh context per firing and a 5-minute minimum interval. This lines up with where OpenClaw ended up after retiring its always-on heartbeat. Silo's fresh-context firings avoid the "full history resent every tick" cost complaint by design.

### Gaps
- I did not find the exact date the heartbeat was retired.

## 8. Messaging channels (list every supported platform)

### Takeaway
OpenClaw has the broadest channel coverage of any agent of its kind: about 30 platforms across bundled, official-plugin and external plugins, plus WebChat and telephony. Silo has 3 (Telegram, WhatsApp, Discord).

### Cited Findings
- **Bundled:** A2A, Reef, Telegram, X/Twitter. **Core:** WebChat.
- **Official plugins** (`openclaw plugins install @openclaw/<id>`): Buzz, ClickClack, Discord, Feishu, Google Chat, iMessage, IRC, LINE, Matrix, Mattermost, Microsoft Teams, Nextcloud Talk, Nostr, QQ Bot, Raft, Signal, Slack, SMS, Synology Chat, Tlon, Twitch, WhatsApp, Zalo, Zalo Personal.
- **External plugins:** WeChat, WeCom, Yuanbao, Zalo ClawBot.
- **Voice Call** (official plugin) provides telephony via Plivo, Telnyx or Twilio.
- DM pairing and allowlists are enforced. Group behaviour varies by channel. Discord, LINE, Matrix, Slack and Telegram post a join introduction (`joinIntro: false` disables it).

  — [docs: Channels](https://docs.openclaw.ai/channels)
- Approvals can be answered in-channel: rich approval cards exist for Discord, Telegram and Matrix. — [docs index](https://docs.openclaw.ai/llms.txt)
- v2026.11.4 channel fixes: Signal, Telegram, WhatsApp, Slack, IRC (an empty sender list now denies all senders instead of accepting them), Feishu, Tlon. — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- Inbound voice notes are downloaded, transcribed and fed into replies. — [docs index (Audio and voice notes)](https://docs.openclaw.ai/llms.txt)

### Inferences
- Channel breadth is OpenClaw's clearest advantage over Silo. In-channel approval cards are a feature Silo lacks; Silo's approvals live in the web and iOS apps.

### Gaps
- I did not determine which channels are unofficial-protocol (e.g. WhatsApp likely Baileys-style linked device; iMessage on macOS only).

## 9. Voice (dictation, TTS, live calls, wake word)

### Takeaway
Voice is extensive:
- **Talk mode** on macOS, iOS, Android and the browser Control UI, with realtime transports (WebRTC, provider WebSocket, gateway relay, managed room), barge-in (`interruptOnSpeech`) and an "agent-consult" brain where a realtime voice model consults the full agent.
- **TTS** via ElevenLabs, OpenAI, Microsoft Edge TTS or local MLX.
- **Telephony** via the Voice Call plugin.
- **Discord voice:** channel auto-join, transcripts and meeting notes.

### Cited Findings
- Talk mode runs on macOS, iOS, Android and the browser Control UI. `talk.realtime.mode` is realtime, stt-tts or transcription. `transport` is webrtc, provider-websocket, gateway-relay or managed-room. `brain` is agent-consult, direct-tools or none. Turn detection is tuned with `vadThreshold` and `silenceDurationMs` (default pause 700 ms on macOS and Android, 900 ms on iOS). There is an `interruptOnSpeech` setting. — [docs: messages and talk](https://docs.openclaw.ai/gateway/config-agents/messages-and-talk)
- TTS providers are ElevenLabs, OpenAI and Microsoft (Edge TTS). Talk providers include ElevenLabs, a local `mlx` helper (macOS) and `system`. `tts.auto` is off, always, inbound or tagged. STT relies on the platform's speech recognition (`speechLocale`). — [docs: messages and talk](https://docs.openclaw.ai/gateway/config-agents/messages-and-talk)
- Realtime voice calls go through `openclaw_agent_consult` to a full agent run (`consultThinkingLevel`, `consultFastMode`). — [docs: messages and talk](https://docs.openclaw.ai/gateway/config-agents/messages-and-talk)
- Discord voice channels: realtime voice, auto-join, follow-user, continuous capture with transcripts and automatic meeting notes. — [docs index](https://docs.openclaw.ai/llms.txt)
- Voice Call plugin: telephony via Plivo, Telnyx or Twilio. — [docs: Channels](https://docs.openclaw.ai/channels)
- iOS chat dictation exists; v2026.11.4 fixed a crash when speech recognition was allowed. — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)

### Inferences
- OpenClaw's "agent-consult" design is architecturally similar to Silo's talker, which delegates turns to the Bot. Silo's talker is deliberately restricted to delegate, stop_task and end_call so every action stays under the rules. OpenClaw also offers a `direct-tools` brain where the realtime model calls tools itself, which Silo forbids.
- OpenClaw adds PSTN telephony and Discord voice, which Silo lacks.

### Gaps
- I could not confirm a wake-word ("Voice Wake") feature in current docs. The Talk config page does not mention one, though earlier macOS app versions reportedly had one (unverified).

## 10. Clients (web, iOS, Android, macOS app, CLI, nodes/companion devices)

### Takeaway
- **Native apps:** macOS, iOS, Android, Windows and Linux.
- **Web:** the Control UI.
- **CLI and TUI.**
- **Nodes:** paired companion devices exposing camera, screen, location, notifications and canvas.

Messaging apps are the main interface for many users.

### Cited Findings
- Native apps for macOS, iOS, Android, Windows and Linux. The web is the Control UI, and the CLI and TUI connect to the Gateway. Companion apps and nodes add voice, canvas, camera and screen. — [GitHub README](https://github.com/openclaw/openclaw)
- Nodes: pairing, capabilities, permissions, and camera/screen/location/notify commands. There is a headless node host and a macOS widget panel. — [docs index](https://docs.openclaw.ai/llms.txt)
- 2.0's browser app opens directly into a conversation, and chat can hold interactive widgets. The Control UI shows files, Git diffs, PR status, a browser panel and a terminal. — [CellCog](https://cellcog.ai/blog/openclaw-2-0/); [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- iOS stores Gateway setup codes; v2026.11.4 stopped saving them in preferences. — [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- Written in TypeScript and Swift. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)

### Inferences
- OpenClaw has broader client coverage than Silo (web and iOS): it adds Android, desktop apps and the device-as-node concept. Silo's iOS app with CallKit, Live Activities and widgets may be deeper on iOS specifically; I could not verify OpenClaw's iOS widget or Live Activity support.

### Gaps
- I did not check app-store availability or the maturity of each native app.

## 11. Security & safety (approvals, sandboxing, prompt injection defences, known CVEs/incidents, exposed instances)

### Takeaway
Security is OpenClaw's defining weakness:
- **CVEs:** a one-click RCE (CVE-2026-25253, CVSS 8.8), a Docker sandbox command injection, and later critical CVEs.
- **Exposed Gateways:** counts range from roughly 21k to 245k depending on scanner and date. The cause was largely auth-less defaults before early 2026.
- **ClawHub malware:** the ClawHavoc campaign.
- **Defaults:** sandboxing and approvals are still off by default in 2.0.
- **Government bans:** Chinese state bodies restricted it in March 2026.

I found no dedicated prompt-injection *output filter* or *egress network filter* comparable to Silo's. Defences are pairing, allowlists, tool policy, approvals, provenance-tagged memory and SSRF policy on the browser.

### Cited Findings
- CVE-2026-25253 (CVSS 8.8): one-click takeover via WebSocket token theft, fixed in v2026.1.29. It works through the user's browser, so local-only instances were also at risk. — [MintMCP](https://www.mintmcp.com/blog/openclaw-cve-explained); [InsiderLLM](https://insiderllm.com/guides/openclaw-security-report-january-2026/); [Hunt.io](https://hunt.io/blog/cve-2026-25253-openclaw-ai-agent-exposure)
- CVE-2026-24763 (Docker sandbox command injection) and CVE-2026-25157 (SSH command injection in the macOS app) were patched in the same January release. — [InsiderLLM](https://insiderllm.com/guides/openclaw-security-report-january-2026/)
- Later critical CVEs (spring 2026), e.g. CVE-2026-32922, reportedly CVSS 9.9. The fixed version is reported as "v3.24.2", which looks like a garbled "v2026.3.24"; unverified. — [Remote OpenClaw blog](https://www.remoteopenclaw.com/blog/openclaw-security-crisis-explained) (secondary)
- An October 2026 CVE chain disclosed by Cyera reportedly affected up to about 245k servers. — [aiweekly](https://aiweekly.co/alerts/cyera-exposes-openclaw-cve-chain-hitting-245k-ai-servers); [cybersecuritynews](https://cybersecuritynews.com/openclaw-chain-vulnerabilities/) (secondary; details not verified)
- Exposed-instance counts conflict:
  - Censys: 21,639 (31 Jan 2026).
  - Bitsight: 30k+ (27 Jan – 8 Feb).
  - Hunt.io: 17.5k+ vulnerable to CVE-2026-25253.
  - SecurityScorecard: 135k+ across 82 countries, with 50k+ RCE-exploitable.
  - Techzine: "over 40,000 vulnerable".
  - Penligent: "over 220,000" exposed.
  - Shodan + ZoomEye: about 245k (date unclear).

  — [InsiderLLM](https://insiderllm.com/guides/openclaw-security-report-january-2026/); [Hunt.io](https://hunt.io/blog/cve-2026-25253-openclaw-ai-agent-exposure); [Techzine](https://www.techzine.eu/news/security/138633/over-40000-openclaw-agents-vulnerable/); [Penligent](https://www.penligent.ai/hackinglabs/hi/over-220000-openclaw-instances-exposed-to-the-internet-why-agent-runtimes-go-naked-at-scale/)
- Root cause: early installs did not require Gateway authentication. The January release removed auth mode "none" and required a token or password. — [OrcaRouter](https://www.orcarouter.ai/blog/2026-02-10-openclaw-kong-zhi-mian-ban) (secondary)
- 2.0 still ships with "sandboxing and approvals off by default". The docs say sandboxing and exec approvals "are not necessarily enabled by default". The Register criticised the unencrypted-at-rest Secret Store and the lack of boundaries in shared sessions. — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new); [VentureBeat](https://venturebeat.com/ai/openclaw-2-0-is-here-what-it-means-for-enterprises); [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- The README tells users to "treat inbound messages as untrusted input" and to read the security guide, exposure runbook and sandboxing guide before exposing a Gateway. — [GitHub README](https://github.com/openclaw/openclaw)
- A browser-control risk page describes an SSRF policy limiting the real browser. — [docs index](https://docs.openclaw.ai/llms.txt)
- Government and industry reactions:
  - China restricted use by state agencies, SOEs and banks (March 2026).
  - Satya Nadella called it a "virus"-like security risk (Feb 2026), yet Microsoft was testing an OpenClaw-based "ClawPilot" by May.
  - VentureBeat pointed enterprises to NanoClaw, a containerised alternative.

  — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- Maintainer "Shadow": "if you can't understand how to run a command line, this is far too dangerous of a project for you to use safely." — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- v2026.11.2 kept secrets and conversation content out of diagnostic logs. v2026.11.4 "hardens isolation between users". — [search summary of releases](https://releasebot.io/updates/openclaw); [freedom.tech](https://freedom.tech/posts/2026-11-16-openclaw-2026-11-4/)

### Inferences
- Silo's differentiators line up with OpenClaw's documented failures: secure-by-default (the container is always on), an egress gateway with surrogate secrets (vs a Secret Store not encrypted at rest), output-rule filtering of tool results for prompt injection, Auto (model-judged) approvals and operator- and Workspace-locked rule floors. I found no OpenClaw equivalent of a network egress filter or a model-judged tool-output injection filter.
- OpenClaw's memory provenance classes are the one injection defence that parallels Silo's keeper rule that tool results are outside content.

### Gaps
- I did not read the official GitHub Security Advisories list, so CVE details beyond January come from secondary sources.
- I did not verify the Cyera October 2026 chain.

## 12. Model providers & cost/token efficiency (subscription OAuth, local models, caching, typical token spend complaints)

### Takeaway
Providers are plugins (Claude, OpenAI/Codex, local and custom OpenAI-compatible servers including Ollama, DeepSeek, and others), with OAuth, auth-profile rotation and model failover. Subscription OAuth was a major draw until **Anthropic blocked Claude subscription OAuth in third-party harnesses on 4 April 2026**. The docs still mention "Claude CLI reuse". Token spend is the top user complaint: full history resent on every call, persona files reloaded, heartbeats.

### Cited Findings
- Docs cover provider plugins, custom and local runtimes, OAuth (token exchange, multi-account), "OAuth, API keys, and Claude CLI reuse", and model failover rotating auth profiles and falling back across models. — [docs index](https://docs.openclaw.ai/llms.txt)
- 2.0 onboarding detects ChatGPT or Claude subscriptions, API keys and local Ollama models. `codex/*` and `openai-codex/*` model refs migrate to `openai/*`. — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new); [CellCog](https://cellcog.ai/blog/openclaw-2-0/)
- Anthropic's moves (Boris Cherny cited "outsized strain" on infrastructure):
  - A January 2026 block attempt was reversed.
  - The February terms restricted subscription OAuth to Claude Code and claude.ai.
  - From 4 April 2026, subscription OAuth tokens stopped working in third-party harnesses, starting with OpenClaw.
  - Google reportedly restricted AI Ultra OAuth use with OpenClaw.

  — [heise](https://heise.de/-11246096); [Implicator](https://www.implicator.ai/anthropic-cuts-openclaw-from-claude-subscriptions-citing-unsustainable-compute-costs/); [Hongkiat](https://www.hongkiat.com/blog/anthropic-closed-openclaw-loophole/)
- Developers adapted OpenClaw for DeepSeek models and WeChat. Tencent and Z.ai announced OpenClaw-based services. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- Cost complaints (anecdotal):
  - $623 in one month.
  - Reports over $3,600/month.
  - Heartbeats a large share of spend.
  - Input includes the full conversation history on every call.
  - `AGENTS.md`, `SOUL.md`, `TOOLS.md` and `HEARTBEAT.md` reloaded on every wake-up.

  — [graphscope](https://graphscope.io/blog/tech/2026/03/18/OpenClaw-User-Token-Cost-Analysis); [dev.to](https://dev.to/lars_winstand/i-read-the-49-comment-openclaw-meltdown-and-the-real-problem-isnt-just-openclaw-5ap6); [pulse.bot](https://www.pulse.bot/ai/blogs/the-openclaw-bill-shock-no-one-sees-coming-1165bdbc-f862-4769-8b85-e92ae6d83f9f/); [findstack](https://findstack.com/resources/how-much-does-openclaw-cost-to-run)
- Talk consult settings include `consultFastMode` and `consultThinkingLevel`. — [docs: messages and talk](https://docs.openclaw.ai/gateway/config-agents/messages-and-talk)

### Inferences
- Silo's positioning as "token-conserving" targets exactly OpenClaw's loudest complaint:
  - an append-only, cache-friendly history,
  - tiered system-prompt cache breakpoints,
  - Python-first tool exposure (a lean tool list),
  - clearing stale tool results before compaction,
  - fresh-context automations.
- Silo's `claude` provider runs the official Claude Code binary rather than reusing the OAuth token, which is relevant given Anthropic's April crackdown. OpenClaw's docs also mention "Claude CLI reuse"; I did not verify how it works or whether it complies with Anthropic's terms.

### Gaps
- I found no official statement on prompt-caching strategy in OpenClaw.

## 13. Multi-agent / subagents

### Takeaway
OpenClaw has multi-agent routing:
- Several agents per Gateway, each with its own workspace.
- Channel accounts and conversations bound to agents.
- Agent templates and roles/teams.
- Parallel "specialist lanes".
- Sub-agent orchestration through session tools, with yield/handoff semantics.

### Cited Findings
- Doc pages cover:
  - **Multi-agent routing:** agent boundaries, channel accounts, bindings.
  - **Agents and Templates.**
  - **Agent bindings.**
  - **Parallel specialist lanes:** "without overloading shared capacity".
  - **Subagent yield handoff:** ownership of completion when a requester yields.
  - **Session tools:** cross-session status and sub-agent orchestration.
  - **Cross-agent messaging and subagent defaults.**
  - **Agents CLI:** roles, teams, workspaces, routing.

  — [docs index](https://docs.openclaw.ai/llms.txt)
- Decrypt: OpenClaw separates a Gateway daemon from the agents it routes to. — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- Sub-agent sessions produce no promotable memory. — [docs: Memory architecture](https://docs.openclaw.ai/concepts/memory-architecture)
- Moltbook, an "AI-agent social network" built on OpenClaw agents, launched 27 Jan 2026. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)

### Inferences
- The two are broadly comparable to Silo's lead plus subagents (depth 1, at most 8, a shared taskboard). OpenClaw adds persistent multi-agent routing per channel account, which Silo does with separate Bots.

### Gaps
- I did not read limits on depth or concurrency.

## 14. Web search, browsing

### Takeaway
OpenClaw has built-in web search and fetch tools (configured under built-in tool settings) and a real-browser tool with profiles, tabs and actions, governed by an SSRF policy. The Docker sandbox offers a sandboxed browser, and the 2.0 Control UI has a browser panel.

### Cited Findings
- "Configuration: built-in tool settings: per-tool settings, including web search and fetch". The FAQ covers web search setup. — [docs index](https://docs.openclaw.ai/llms.txt)
- Browser CLI: lifecycle, profiles, tabs, actions, debugging. "Browser control risks" covers what a real browser exposes and the SSRF policy. The Docker backend includes "the sandboxed browser". — [docs index](https://docs.openclaw.ai/llms.txt)

### Inferences
- Silo has a keyless metasearch and a page reader with a Lightpanda render in the box. OpenClaw's search likely needs a provider API key. Silo's keyless default may be a convenience edge; this is unverified.

### Gaps
- I did not confirm which search providers are supported (Brave, Perplexity and others are commonly cited, but I did not verify this in 2026 docs), or whether browser control uses CDP on the user's Chrome.

## 15. Output surfaces (canvas, artifacts, dashboards)

### Takeaway
OpenClaw has a "Canvas" surface on companion apps and nodes, a "canvas widget presenter", and, since 2.0, interactive widgets in chat. The Control UI shows files, Git diffs and PR status. I found no equivalent of Silo's Dashboard (persistent widgets with probes), Feed, or versioned artifacts with public share links.

### Cited Findings
- Canvas is listed among companion-app and node features. — [GitHub README](https://github.com/openclaw/openclaw)
- A config page covers the "canvas widget presenter". — [docs index](https://docs.openclaw.ai/llms.txt)
- In 2.0, chat can hold interactive widgets. — [CellCog](https://cellcog.ai/blog/openclaw-2-0/)
- The Control UI has files, Git diffs, PR status, a browser panel and a terminal. — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)

### Inferences
- Silo's Dashboard, Feed, artifacts, live HTML apps and tunnels are a richer "output to the owner" layer than anything I found documented for OpenClaw.

### Gaps
- I found no dedicated Canvas/A2UI doc page in the visible docs index (it may sit beyond the truncated portion).

## 16. Adoption & ecosystem (GitHub stars, community, governance/foundation, release cadence, notable 2026 releases)

### Takeaway
OpenClaw is the most-starred agent project. It had about **395k GitHub stars** (Nov 2026), up from 247k in March. It is MIT-licensed and stewarded by the **OpenClaw Foundation** (a 501(c)(3)) since creator Peter Steinberger joined OpenAI in February 2026. Releases come at a very high cadence: 106 releases in 230 days before 2.0, and four stable releases in the first half of November 2026.

### Cited Findings
- Stars: 394.7k on the GitHub page as of Nov 2026. — [GitHub](https://github.com/openclaw/openclaw)
- Earlier: 247k stars and 47.7k forks on 2 Mar 2026. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- Name history:
  - Warelay: 24 Nov 2025.
  - CLAWDIS: 3 Dec 2025.
  - Clawdbot: 2 Jan 2026.
  - Moltbot: 27 Jan 2026, after an Anthropic trademark complaint.
  - OpenClaw: 30 Jan 2026.

  — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- Steinberger announced on 14 Feb 2026 that he was joining OpenAI, with a non-profit foundation to steward the project. The Foundation (501(c)(3)) employs the core team and signs releases, and sponsors don't direct the project. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw); [GitHub README](https://github.com/openclaw/openclaw)
- 2.0 (v2026.8.1, 30–31 Aug 2026): over 16,000 merged PRs from 933 contributors (569 first-time), roughly half of all PRs ever merged. It had been 106 releases in 230 days before the seven-week gap. — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- November 2026 releases:
  - v2026.11.1 (5 Nov): reliability.
  - v2026.11.2 (8 Nov): privacy in diagnostic logs.
  - v2026.11.3 (10 Nov): fixes.
  - v2026.11.4 (16 Nov): 220 commits, 125 PRs, 90 contributors.
  - v2026.11.5 betas.

  — [GitHub release](https://github.com/openclaw/openclaw/releases/tag/v2026.11.4); [releasebot](https://releasebot.io/updates/openclaw)
- Ecosystem:
  - Moltbook.
  - Tencent and Z.ai services.
  - Microsoft "ClawPilot" testing.
  - Google's rival "Remy".
  - A "lobster trade" in Chinese stocks.
  - NanoClaw (a containerised fork or alternative).
  - Rival Hermes Agent (Nous Research): Decrypt says Hermes leads on self-improvement and safer defaults, OpenClaw on ecosystem and channel breadth.

  — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw); [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)

### Inferences
- The ecosystem gap with Silo is enormous: hundreds of thousands of stars, about 1k contributors per major release, over 10k skills. Silo should compete on secure defaults, multi-user isolation and token efficiency, not on breadth.

### Gaps
- I found no active-user or install counts.

## 17. Known weaknesses / common complaints

### Takeaway
- Insecure defaults (host execution, sandbox and approvals off) and a history of internet-exposed Gateways and CVEs.
- A malware-prone open skills registry.
- High and unpredictable token bills.
- Multi-user that is "not a security boundary".
- Complexity unsuitable for non-technical users.
- Fast churn with breaking migrations (SQLite move, Plugin SDK deprecations, changing visibility defaults).
- Loss of cheap subscription access after Anthropic's April ban.

### Cited Findings
- Security defaults and boundary disclaimers. — [Implicator](https://www.implicator.ai/openclaw-2-multiplayer-not-security-boundary/); [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- Platformer: flexible and open, but complexity and security risks make it unsuitable for casual users. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)
- Breaking changes in 2.0:
  - SQLite migration (no downgrade).
  - OpenProse and `/prose` removed.
  - Plugin SDK deprecation gates (1 Sep 2026).
  - A plugin-reload validation copy "is not a rollback backup".

  — [CellCog](https://cellcog.ai/blog/openclaw-2-0/)
- Token bill shock. — [graphscope](https://graphscope.io/blog/tech/2026/03/18/OpenClaw-User-Token-Cost-Analysis); [pulse.bot](https://www.pulse.bot/ai/blogs/the-openclaw-bill-shock-no-one-sees-coming-1165bdbc-f862-4769-8b85-e92ae6d83f9f/)
- Reliability: November releases fix memory exhaustion on large conversations, message loss across restarts, and SQLite stalls. — [freedom.tech](https://freedom.tech/posts/2026-11-16-openclaw-2026-11-4/); [docs: release 2026.11.4](https://docs.openclaw.ai/releases/2026.11.4)
- Unprompted autonomy incidents (MoltMatch); concerns about data deletion and leaks cited in China's restrictions. — [Wikipedia](https://en.wikipedia.org/wiki/OpenClaw)

### Inferences
- November's stream of fixes (cross-user leaks, lost replies, stalls) suggests the 2.0 rearchitecture is still settling.

### Gaps
- I did not read Hacker News or Reddit threads directly; complaint themes come from blogs and press.

## 18. Pricing & license

### Takeaway
OpenClaw is free and MIT-licensed, with no paid tier, hosted service or token from the Foundation. Users pay model providers directly, and the Anthropic subscription route closed in April 2026. Third-party hosts and resellers exist.

### Cited Findings
- MIT license; "no paid tier, hosted service, or token". — [GitHub README](https://github.com/openclaw/openclaw)
- Free and MIT-licensed. — [Decrypt](https://decrypt.co/377135/openclaw-2-0-is-here-whats-new)
- The cost is model spend. Examples range from about $0 on cheap or local models to about $85/month for heartbeats alone on frontier models, with reports from $600 to $3,600/month. — [findstack](https://findstack.com/resources/how-much-does-openclaw-cost-to-run); [graphscope](https://graphscope.io/blog/tech/2026/03/18/OpenClaw-User-Token-Cost-Analysis)

### Inferences
- OpenClaw is a free MIT competitor with a huge community, so Silo's licensing and pricing must justify itself through security, team features and lower token spend.

### Gaps
- I did not survey third-party OpenClaw hosting prices.
