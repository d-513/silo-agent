1. Scheduling, proactivity & autonomy — Silo has none

- Hermes: full cronjob tool (natural language + cron expressions), pause/resume/edit/trigger, skill-attached jobs, delivery to any platform, script-only "no-agent" watchdogs, webhook/event-triggered jobs, continuable deliveries you can reply into, and cron jobs that manage cron jobs. Fresh isolated sessions with per-job toolsets.
- OpenClaw: two systems — Automations (precise cron/one-shot, isolated or main session, task ledger) and Heartbeat (system-owned ambient monitor turn every 30m with HEARTBEAT.md, active-hours, isolated/light-context modes, openclaw system event manual wake). Plus inbound webhook triggers.
- Muse: "always working… continues on a schedule and in response to relevant events," works after app close.
- Instinct: proactively re-engages dropped threads; texts/calls you unprompted.
- Silo has the runRequest seam but zero implementation. This is the #1 assistant primitive missing.

2. Voice & telephony — Silo has none

- Hermes: full voice mode (mic + spoken replies), on-device wake word ("Hey Hermes"), live conversations in Discord voice channels, voice-note transcription, TTS across 10 providers.
- OpenClaw: voice notes in/out, TTS, Voice Call channel via Twilio/Plivo/Telnyx (real phone calls), Discord voice channels with auto-join, Talk/PTT node commands.
- Instinct: Concierge outbound phone calls (phone-only restaurants, cancellation lists, bill disputes); Muse shipped the same the same week.
- Silo can only do Telegram text + a VNC desktop.

3. Subagents / parallel delegation — Silo is single-run-per-conversation

- Hermes: delegate_task spawns isolated child agents (fresh context, restricted toolsets, own terminals), configurable concurrency, orchestrator nesting via max_spawn_depth, /review background reviewer, steer_subagent/interrupt_subagent, /agents audit overlay, execute_code for zero-context multi-step pipelines.
- OpenClaw: sessions_spawn with isolated/fork context modes, nested orchestrators, background task ledger, push-based completion handoff that wakes the requester.
- Muse: "launches swarms of subagents."
- Silo injects new messages into the one live run; no delegation.
  Memory & learning

4. Deep memory / retrieval

- Hermes: separate USER.md + MEMORY.md, 8 external memory providers (Honcho, Mem0, Hindsight, OpenViking, Supermemory…), FTS5 session search over all past conversations with scroll, background self-improvement review that saves memory/skills after a turn, memory.write_approval gate, and a "learning journey" timeline.
- OpenClaw: vector-embedding memory / knowledge graph, daily append-only logs (memory/YYYY-MM-DD.md), compaction, /dreaming memory.
- Silo: SOUL + MEMORY blobs in SQLite, 8k cap. No semantic search, no cross-session search, no auto-learning.

5. Self-improving skills

- Hermes: "autonomous skill creation after complex tasks; skills self-improve during use" (closed learning loop).
- OpenClaw: "it can even write its own" skills.
- Silo skills are human-authored/installed only.

6. Context compaction & session lifecycle

- Hermes: /compress, /new, /reset, /retry, /undo, /insights, named sessions.
- OpenClaw: explicit compaction reserve + pluggable ContextEngine (e.g. lossless-claw hierarchical summarization).
- Silo has no explicit compaction, retry, undo, or steer; runs cap at 10 min.
  Channels, identity & reach

7. Channel breadth — OpenClaw: ~29 (WhatsApp, Discord, Slack, Signal, iMessage, SMS, Matrix, Teams, IRC, LINE, Twitch, Nostr, Zalo, WeChat…); Hermes: Telegram/Discord/Slack/WhatsApp/Signal/Email; Instinct: iMessage/SMS/WhatsApp/phone. Silo: Telegram only. The adapter framework is ready, the adapters aren't.
8. Agent-owned identity — Instinct has its own email address (registers accounts, receives confirmations) and phone number. Silo bots have no independent identity.
9. Payments & credential rails — Instinct (Stripe Link, 1Password vault sharing), Muse (Stripe Link/Shop Pay, 1Password, hatch-authd). Silo has secret masking but no payment path or password-manager integration.
10. Agent-to-agent / multiplayer — Instinct's Trusted Person Network (your agent negotiates with your spouse's/friends' agents); OpenClaw multi-agent routing + shared gateway team sessions; Hermes Bot Mode with group chats and @mentions. Silo bots are strictly isolated per user.
    Device, context & notifications
11. Native nodes / personal data — OpenClaw iOS/Android/macOS nodes exposing camera, screen recording, location, contacts, calendar, reminders, photos, health/pedometer, call log, SMS, and system.notify push; macOS menu-bar app, Windows Hub, Canvas/A2UI. Instinct: location + screen/audio/device context. Silo has a headless container desktop but no mobile/desktop nodes, no location, no native push (in-app "Needs you" only).
12. Notifications — Hermes/OpenClaw push via TTS, node system.notify, channel DMs. Silo is in-app only (iOS polls).
13. Media generation — Hermes: image gen (11 FAL models), video; OpenClaw: image_generate/video_generate/music_generate; Muse/Instinct build rich interactive artifacts/pages. Silo can present files but generates no media and artifacts aren't interactive apps.
14. Web extract / browser backends — Hermes: search+extract+multiple browser backends (Browserbase, Browser Use, CDP); OpenClaw built-in browser. Silo: DuckDuckGo-scraper only, page extract "not available yet."
    Extensibility & operations
15. Hooks, plugins & event triggers — Hermes: gateway hooks + plugin hooks (tool interception, guardrails), event-driven. OpenClaw: lifecycle hooks (before_agent_start, before_prompt_build, session:compact:\*, message_sending), HTTP webhooks (Gmail Pub/Sub, Zapier, GitHub), plugin SDK, ContextEngine slot. Silo has no plugin/hook/webhook system.
16. Provider routing, fallback & credential pools — Hermes: provider routing (sort/whitelist/blacklist/priority), fallback providers per task, credential pools with rotation, local models (Ollama), OpenAI-compatible API server, ACP/IDE integration. Silo: static allowlist + per-bot/per-chat model; no fallback, routing, pools, local models, or OpenAI-compatible endpoint.
17. Checkpoints / rollback — Hermes snapshots the working dir before file changes and supports /rollback. Silo has no snapshot/rollback.
18. Workspace context files & @ references — Hermes auto-loads .hermes.md/AGENTS.md/CLAUDE.md/.cursorrules and expands @file/@folder/@url. Silo has only SYSTEM.md.
19. Personality presets & skins — Hermes /personality presets, themes/skins. Silo has editable SOUL only.
    Security model depth (Silo is close, but)
20. Independent permission authority & credential surrogation — Muse's Sentinel is a separate host-side agent that is the sole authority for connector actions and network egress, and hatch-authd substitutes surrogate tokens so the agent never sees real credentials (even typed browser passwords), plus untrusted-input labeling. OpenClaw has a configurable exec reviewer with allow-once/deny/ask outcomes and DM/node pairing. Silo's authorizeAction + masker is solid but there's no out-of-process sentinel or token substitution.
    Suggested priority for an assistant product
21. Scheduling/heartbeat/cron + proactive runs (the engine seam already exists)
22. More channel adapters (WhatsApp, Discord, Slack, Email, SMS)
23. Subagent delegation
24. Session search + external/semantic memory + auto-learning
25. Voice (TTS/STT, wake word, eventually calls)
26. Notifications/push + mobile/device nodes
27. Media generation + richer interactive artifacts
