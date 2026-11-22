# Muse (Meta's personal AI agent): capability inventory as of November 2026

Research date: 2026-11-17. Most detail comes from Meta's own posts and from secondary explainers that quote them. Many secondary sites are SEO or vendor blogs (MyClaw, PostFast, CellCog, layer3labs, openclawdatabase), and I mark them where they carry a claim alone. Several primary pages (Meta Help Center, windowsreport) would not load in full. Their content is reported second-hand and flagged.

## 0. Identity & disambiguation: maker, launch date, open/closed, positioning

### Takeaway
The relevant "Muse" is **Meta's Muse**, a closed, cloud-hosted, consumer personal agent. Meta announced it on 2026-09-08. Each user gets their own always-on cloud Linux VM, which Meta calls the "Muse Secure VM", with a browser, connectors and a separate "Sentinel" permission agent. The press and competitors frame it as Meta's answer to OpenClaw. The other Muses (Microsoft's gameplay model, Meta's Muse Code CLI, the muse.ai video host, the Muse website builder, Unity Muse) are not personal agents.

### Cited Findings
- Developed by Meta Platforms and announced 2026-09-08 as a personal AI agent for "long-running tasks," rather than a single-exchange chatbot. It launched in the US on iOS, Android and the web. — [Wikipedia: Muse (AI agent)](https://en.wikipedia.org/wiki/Muse_(AI_agent)), citing [Axios](https://www.axios.com/2026/09/08/meta-debuts-muse-personal-ai-agent)
- Meta's launch post: the agent keeps working after the app closes and comes back when something changes or it needs approval. It is available on iOS, Android and muse.ai, with AI glasses "coming soon". — [Meta Newsroom, Introducing Muse](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- It was built under Alexandr Wang, Meta's chief AI officer, after Meta's June 2025 stake in Scale AI. The model family, Muse Spark, was introduced in April 2026. — [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent)), citing [Fortune](https://www.fortune.com/2026/04/08/meta-unveils-muse-spark-mark-zuckerberg-ai-push)
- Internal codename "Hatch". Process names are `hatch-safety` and `hatch-authd`, and the OAuth callback is `muse.ai/api/hatch/oauth/callback`. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026); [PostFast connector list](https://postfa.st/blog/meta-muse-connectors-list)
- Framed as "Meta's answer to OpenClaw". Forklog headlines that "Meta acknowledges Muse AI agent's inspiration from OpenClaw" (headline only; article not read). — [Trending Topics](https://www.trendingtopics.eu/muse-is-metas-answer-to-openclaw-and-it-comes-with-trust-issues/); [Forklog](https://forklog.com/en/meta-acknowledges-muse-ai-agents-inspiration-from-openclaw/?hl=-1)
- Alternative directories list OpenClaw, Hermes Agent (Nous Research), NanoClaw, Khoj and Leon as open-source alternatives to Muse. — [openalternative.co](https://openalternative.co/alternatives/meta-muse); [alternativeto](https://alternativeto.net/software/meta-muse/)
- CNBC groups Muse with other consumer personal agents: OpenAI's "Dots", SpaceXAI's "Grok Bot" and Spear Street Technology's "Instinct". — [CNBC, 2026-10-22](https://www.cnbc.com/2026/10/22/meta-muse-personal-ai-agents.html)
- **Runner-up / other "Muse"s:**
  - **Microsoft Muse** (Feb 2025) is a generative "World and Human Action Model" (WHAM) for gameplay ideation, trained on Bleeding Edge. Its weights are open and the paper is in *Nature*. It is not an agent. — [Microsoft Research](https://www.microsoft.com/en-us/research/blog/introducing-muse-our-first-generative-ai-model-designed-for-ga); [GeekWire](https://www.geekwire.com/2025/microsoft-reveals-new-gaming-focused-generative-ai-model-muse-that-could-revive-classic-games/)
  - **Muse Code** is Meta's terminal coding agent on the same Muse Spark models. It is reported to coordinate multiple persistent subagents. — [Decrypt](https://decrypt.co/375001/muse-code-meta-ai-coding-agent-claud-codex); [primetel](https://primetel.com.cy/meta-enters-the-terminal-arena-with-muse-code-ai-agent-8961)
  - **Unrelated products:** muse.ai video hosting (Basic/Plus/Pro tiers), the Muse website builder (muse.helpscoutdocs.com) and Unity Muse (a game-dev assistant). — [Muse website builder help](https://muse.helpscoutdocs.com/article/185-what-are-the-benefits-of-muse-pro); [Unity discussions](https://discussions.unity.com/t/muse-chat-responses-get-cut-off-or-truncated-partway-through/337372)

### Inferences
- Note the naming collision: Meta's agent now lives at **muse.ai**. That URL previously belonged to a video-hosting service, so third-party pricing pages mix the two up (inference from search results showing both).
- Muse is the closest closed-source, hyperscaler analogue to OpenClaw or Hermes. Its architecture (a per-user VM, an egress gate, credential surrogates, an output and injection classifier) overlaps strikingly with Silo's (a per-Bot container, the egress gateway, surrogate secrets, the output filter).

### Gaps
- I did not read the full Forklog piece, so the exact wording of Meta's OpenClaw acknowledgement is unverified.

## 1. Deployment & hosting

### Takeaway
SaaS only: it is hosted by Meta and cannot be self-hosted. Each user gets a dedicated, persistent cloud Linux VM. A "Confidential VM" that Meta itself cannot read was promised for late 2026 and was not confirmed shipped as of this research.

### Cited Findings
- It runs on a virtual machine in Meta's cloud, one VM per user. — [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent)), citing [Axios](https://www.axios.com/2026/09/08/meta-debuts-muse-personal-ai-agent)
- "Muse Secure VM" is a persistent, isolated Linux computer in the cloud with a browser, files, CPU, memory and storage. Meta calls it the "system of record" for agent activity. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026), summarizing [Meta: How we built safety into Muse](https://research.meta.ai/blog/how-we-built-safety-into-muse)
- Inside the VM, the agent runs in a systemd-nspawn "runtime cell". Root is mapped to an unprivileged host user, io_uring is blocked, and CAP_SYS_PTRACE and CAP_NET_ADMIN are dropped. Outside the cell run `hatch-safety` (classifiers), privsep connector workers, `hatch-authd` (credentials) and Sentinel. App state lives in a separate Postgres. Components talk over Unix sockets with SO_PEERCRED ACLs. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026), summarizing [Meta research blog](https://research.meta.ai/blog/how-we-built-safety-into-muse)
- "Muse Confidential VM" encrypts with a user-held key and is in testing for later in 2026. Until then, Meta can access VM data. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/); [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- **Regions:** US at launch (18+). Japan followed on 2026-10-23 and Singapore on 2026-10-27. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list), citing [Meta Newsroom Japan](https://about.fb.com/ja/news/2026/10/muse-personal-ai-agent-launches-in-japan/) and [Meta Newsroom Singapore](https://about.fb.com/news/2026/10/muse-is-now-available-in-singapore-the-worlds-first-personal-ai-agent-built-for-everyone/). A MyClaw comparison mentions US + Canada, which is unconfirmed by Meta. — [MyClaw](https://myclaw.ai/blog/muse-ai-alternatives)
- The Verge reported a VM filesystem export feature, which Meta says is intended. — via [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)

### Inferences
- Unlike Silo, the operator is always Meta, and data sovereignty depends on Meta's promise (the Confidential VM).

### Gaps
- I found no confirmation that the Confidential VM shipped by mid-November 2026.

## 2. Multi-user / teams / access control

### Takeaway
Muse is single-user and consumer-first: one agent and one VM per person. I found no teams, shared agents, roles or workspaces. "Muse for Small Business" exists, but its scope is unclear.

### Cited Findings
- Each user's agent runs in a separate VM. — [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent))
- Shared Muse chats (public share pages) could be found through passive DNS and subdomain discovery, and Meta suspended sharing. — [Ars Technica](https://arstechnica.com/security/2026/10/meta-muse-exposes-users-personal-and-business-secrets-when-they-share-conversations/); [Claroc](https://blog.claroc.io/ghostshares-muse-passive-dns/); [CNBC](https://www.cnbc.com/2026/10/22/meta-muse-personal-ai-agents.html)
- Spaces (mini-apps) cannot be shared yet, and Meta is "exploring" sharing. — [CellCog](https://cellcog.ai/blog/muse-spaces/), citing [Meta Newsroom](https://about.fb.com/news/2026/11/introducing-spaces-customize-muse-app/)
- A "Muse for Small Business" newsroom post (2026-09-29) accompanied a large batch of connectors (Canva, Stripe, Zoom, Slack, Dropbox, Box). — [PostFast](https://postfa.st/blog/meta-muse-connectors-list), citing [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-small-business/)
- Sign-in goes through Meta accounts. Facebook, Instagram and Threads connect automatically through Accounts Centre. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list), citing [Meta Help Centre](https://www.meta.com/help/artificial-intelligence/1687253048996149/)

### Inferences
- Silo's multi-user Workspaces, roles, per-Bot managers, lending and operator floors have no counterpart in Muse (inference from absence across all sources).

### Gaps
- I did not read the Muse for Small Business post, so whether it adds multi-seat or team features is unknown.

## 3. Execution sandbox (container/VM, desktop, browser control, shell, code)

### Takeaway
Muse has a strong sandbox: a per-user VM with an nspawn cell. It has a watchable Chromium that the agent reads through an accessibility tree (no page JavaScript or devtools for the agent), a shell with files and cron visible in a "System Files" panel, and since 2026-09-17 a Mac app that controls native macOS apps.

### Cited Findings
- The Muse browser can be watched while the agent works. — [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent)), citing Axios
- Browser design:
  - Chromium sits behind a virtualization layer, and the agent sees an accessibility-tree snapshot, not the raw DOM.
  - The agent cannot run page JavaScript or use devtools.
  - Classifiers watch for DOM-, image- and file-based injection and for risky form submits.
  - The agent pauses while the user takes manual control and during credential autofill.
  - Navigation is checked against blocklists.
  - — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026), summarizing [Meta research](https://research.meta.ai/blog/how-we-built-safety-into-muse)
- A Hacker News tester could view system files, AGENTS.md, and shell and cron scripts in the GUI. — [eesel review](https://www.eesel.ai/blog/meta-muse-agent-review), citing [HN](https://news.ycombinator.com/item?id=49620247)
- **Muse for Mac** (announced 2026-09-17/18) is the "first version of Muse that can complete tasks directly on a user's computer". It works with Messages, Calendar, Notes and Mail. Access is opt-in per app and data, and it keeps working when the app is closed. — [Business Today](https://www.businesstoday.in/technology/news/story/metas-muse-ai-comes-to-macbook-ai-agent-can-organise-files-emails-and-more-556344-2026-09-18); [Social Media Today](https://www.socialmediatoday.com/news/meta-expands-muse-agent-connections-launches-muse-for-mac/830852/); [AI Weekly](https://aiweekly.co/alerts/meta-ships-muse-for-mac-agent-now-acts-in-native-macos-apps); [TechCrunch 2026-09-23](https://techcrunch.com/2026/09/23/everything-new-coming-to-metas-ai-agent-muse/)
- It builds its own MCP client in the VM with the official SDK, which suggests it writes and runs code. — [parallel.ai](https://parallel.ai/articles/meta-muse-custom-integrations) (vendor blog)

### Inferences
- The cloud VM browser plus Mac computer use resembles Silo's container desktop with Chromium. A difference: Muse deliberately keeps the agent off page JavaScript, while Silo offers Playwright through `chrome_page`.
- Muse exposes a general-purpose shell to the agent and lets the user inspect it, but I found no user-facing terminal or VNC-style live desktop beyond the watchable browser (inference; unconfirmed).

### Gaps
- I found no documentation of a user-facing terminal, the code-execution language set, or the VM's resource limits.

## 4. Tools, MCP & integrations

### Takeaway
About 60 Meta-reviewed connectors are live in the US (third-party count as of 2026-11-02). There is a developer Connector Platform (platform.muse.ai) with review, and users can create "Custom Connectors", including remote MCP servers over streamable HTTP with OAuth. Custom connectors are not reviewed by Meta.

### Cited Findings
- **Launch connectors:** Gmail, Google Calendar, OpenTable, Facebook, Instagram, Peloton and Plaid, "among others". Shop Pay and 1Password were coming soon. — [eesel](https://www.eesel.ai/blog/meta-muse-agent-review)
- **Count:** 58 connectors are confirmed and roughly 60 are live in the US, with 18 announced and 30+ community custom connectors on GitHub. Notable ones:
  - Meta's own: Facebook, Instagram, Threads, Instagram/Threads Messages, Messenger.
  - Gmail and Outlook.
  - Payments and finance: Link by Stripe, Plaid, QuickBooks.
  - Shopping and travel: Shopify, OpenTable, Ticketmaster.
  - Productivity: Notion, Granola, Google Docs, Dropbox, Box, Slack, Zoom, Canva, Figma.
  - Developer: GitHub, Tailscale, Railway, Replit, Zapier.
  - Health: Peloton, Withings, Function Health.
  - — [PostFast (third-party tracker)](https://postfa.st/blog/meta-muse-connectors-list). Another tracker claims 79. — [sprites.ai](https://www.sprites.ai/muse/connectors)
- **Commerce partners** (Connect, 2026-09-23): Shopify catalog, Stripe Link and Shop Pay, PayPal, Best Buy, Gap, Sephora, Walmart, Wayfair, Expedia, and Instacart (soon). GitHub, Granola and Notion connectors were also announced. 1,500+ developer applications came in within a week. — [TechCrunch](https://techcrunch.com/2026/09/23/everything-new-coming-to-metas-ai-agent-muse/)
- **Connector Platform** (moved to platform.muse.ai on 2026-10-31):
  - Intake requires Persona business verification, a data-processing questionnaire, OAuth credentials and a test account.
  - Review covers risk, tools and end-to-end QA.
  - Tools are classed as Read, Write or Sensitive write, and a sensitive write cannot be "Always allow".
  - Meta is not approving new money-movement tools.
  - — [PostFast](https://postfa.st/blog/meta-muse-connectors-list), citing [muse.ai/platform/docs](https://muse.ai/platform/docs)
- **Custom connectors:** the user can "ask Muse to create a Custom Connector" for any API, and Meta does not review these. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list), citing [Meta Help Centre](https://www.meta.com/help/artificial-intelligence/1687253048996149/)
- **Remote MCP:** Settings → Connectors → Add custom connector, then paste the URL. The server must be internet-reachable because Muse runs in the cloud. Muse "builds an MCP client on its own VM using the official SDK, connects over streamable HTTP, and saves the integration as a skill". — [parallel.ai](https://parallel.ai/articles/meta-muse-custom-integrations) (vendor blog)
- **Reported custom-connector issues** (user reports, untested by the tracker):
  - Custom OAuth connectors lose access after about an hour because no refresh token is issued.
  - The "Add custom connector" button vanished in some app versions.
  - — [PostFast](https://postfa.st/blog/meta-muse-connectors-list), citing [r/MetaAI](https://www.reddit.com/r/MetaAI/comments/1wvvi35/muse_custom_connectors/)
- **Credential isolation:** connector code runs in privsep workers outside the agent cell. `hatch-authd` keeps OAuth tokens on the user's VM, and the agent holds only surrogate tokens that Sentinel swaps for real ones at the network boundary. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026), summarizing [Meta research](https://research.meta.ai/blog/how-we-built-safety-into-muse)

### Inferences
- The connector model maps onto Silo's catalog presets plus custom MCP. The Read / Write / Sensitive-write classes are comparable to Silo's per-action Allow/Ask rules.

### Gaps
- Meta has published no official connector count, and I have not verified MCP support against a Meta primary source.

## 5. Skills / plugins / marketplace

### Takeaway
There is a reviewed **connector directory** that acts as the marketplace. "Skills" appear only informally: custom integrations are saved as skills, and there are community skills for the Muse Home Link device. Spaces (2026-11-06) adds user-built mini-apps. I found no public skills marketplace.

### Cited Findings
- A custom MCP integration is saved "as a skill". — [parallel.ai](https://parallel.ai/articles/meta-muse-custom-integrations) (vendor blog)
- The Muse Gadgets SDK was open-sourced on 2026-10-02 and the Muse Home Link device was announced. The only "skills" mention is "community-built skills" for Home Link. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list), citing [GitHub muse-gadget-sdk](https://github.com/facebookincubator/muse-gadget-sdk) and [gadgets.muse.ai](https://gadgets.muse.ai/)
- **Spaces** (2026-11-06) are custom mini-apps you make by describing them:
  - A space can be pinned to the sidebar or tabs, hidden, or scheduled to appear.
  - It refreshes from connected apps.
  - Examples: a gift hub, school dashboard, wedding planner, order tracker, client hub and trip planner.
  - Spaces cannot be shared yet.
  - — [CellCog](https://cellcog.ai/blog/muse-spaces/), citing [Meta Newsroom](https://about.fb.com/news/2026/11/introducing-spaces-customize-muse-app/)

### Inferences
- Spaces are functionally close to Silo's Dashboard widgets with probes and its live HTML artifact apps.

### Gaps
- I found no Meta documentation of an Agent-Skills-style (SKILL.md) system or a skills marketplace.

## 6. Memory & personalization

### Takeaway
Muse has persistent memory that the user can inspect, edit, download and tell to "forget". Meta claims improved "Active Memory" from the Muse Wonder-trained models (2026-11-06). TIME reported that Muse keeps dossiers on users and their contacts. Users can personalize the agent's name, avatar and style.

### Cited Findings
- Muse remembers what matters and acts on details mentioned once, and users can tell it to forget things. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- Users can inspect, edit and download "Muse's memory about you". Memory persists between tasks, which creates a memory-poisoning risk. — [itadon security analysis](https://itadon.com/blog/muse-ai-agent-security/) (secondary)
- Users set the agent's name, avatar and communication style. — [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent)), citing Axios
- On 2026-11-06, Muse and Meta AI moved to models trained with "Muse Wonder". By Meta's own evals (not independently replicated):
  - forgetting of critical information fell 2.4x;
  - Active Memory rose from 76% to 90%;
  - "Personal Superintelligence Index" rose from 38 to 47.
  - — [CellCog](https://cellcog.ai/blog/muse-spaces/), citing [Meta AI Research](https://research.meta.ai/blog/specialized-models-for-muse-and-metaai)
- TIME reports that Muse keeps dossiers on users and their contacts. — via [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- An AGENTS.md file is visible in the VM's system files. — [eesel](https://www.eesel.ai/blog/meta-muse-agent-review), citing HN

### Inferences
- The memory model (user-visible, user-editable) is broadly comparable to Silo's Memories page. Silo additionally holds inferred memories as tentative and keeps a retire/restore history. I found nothing equivalent documented for Muse.

### Gaps
- I found no documentation of the memory store's structure, its retrieval method, or whether inferred memories are gated.

## 7. Scheduling, automations, proactivity

### Takeaway
Muse is always-on and proactive. It keeps working after the app closes, offers suggestions unprompted, and supports reminders and recurring tasks (an Upcoming tab, an Activity log, cron visible in the VM). Users report that recurring tasks are unreliable.

### Cited Findings
- Muse keeps working after the app closes, returns when something changes or needs approval, and makes unprompted suggestions. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- Reminders and recurring tasks persist until cancelled. Reminders are under an **Upcoming** tab and everything done is in an **Activity** log. — [eesel and review roundup (search summary)](https://www.eesel.ai/blog/meta-muse-agent-review); [aiagentslibrary](https://www.aiagentslibrary.com/blog/how-to-use-meta-muse/)
- An HN user saw cron job files in the "System Files" panel. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- Recurring daily briefs spend tokens whether or not they are read. — [openclawdatabase pricing](https://openclawdatabase.com/meta-muse/pricing/) (third-party)
- A user reports that recurring tasks are not reliable enough. — [Digg](https://digg.com/ai/heqfn47g)
- Spaces can be scheduled to appear. — [CellCog](https://cellcog.ai/blog/muse-spaces/)

### Inferences
- This is comparable to Silo's automations, Heartbeat and proactivity levels. I found no proactivity-level control (Silo's off/quiet/active) documented for Muse.

### Gaps
- I found no official docs on scheduling syntax, limits or a minimum interval.

## 8. Messaging channels

### Takeaway
**WhatsApp** is a first-class surface: you message Muse like a contact. Meta's own messaging apps (Messenger, Instagram and Threads DMs) are connectors. A dedicated agent email address (forward or CC to Muse) was announced as "soon". I found no Telegram, Discord or SMS.

### Cited Findings
- Muse can be messaged directly in WhatsApp, like messaging another person. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- Reviewers call WhatsApp the "strongest decision in the product". — [Medium review](https://ercanataycom.medium.com/meta-muse-review-the-personal-agent-inside-whatsapp-fdc4c90b816a)
- Instagram Messages, Threads Messages and Messenger are listed as connectors. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list)
- Muse will get its own email address, so users can add it to a thread or forward mail to it. It was "coming soon" with no timeline. — [TechCrunch](https://techcrunch.com/2026/09/23/everything-new-coming-to-metas-ai-agent-muse/)
- Muse places phone calls to businesses: 404 Media reported that some beta calls presented as automated were actually completed by human call-center workers. — [404 Media](https://www.404media.co/meta-tests-muse-ai-agent-calls-that-are-actually-made-by-humans-in-a-call-center/), via [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent))

### Inferences
- Silo supports more third-party channels (Telegram, Discord). Muse owns WhatsApp natively (official API, not a linked device). Silo's WhatsApp is an unofficial linked device.
- Silo's receive-only Bot mailbox is roughly analogous to Muse's announced agent email address.

### Gaps
- I could not confirm whether the agent email address had shipped by November 2026.

## 9. Voice

### Takeaway
Real-time voice conversations and expressive avatars launched 2026-11-05 in the US on mobile, web and Mac, with "hundreds of voices". A video-chat avatar runs on a "Muse Realtime Avatar" model. Wake-word use on Meta glasses and the Muse Charm device are "coming soon". Muse also makes outbound phone calls to businesses.

### Cited Findings
- Real-time voice conversations rolled out 2026-11-05 in the US on mobile, web and Mac, together with expressive avatars offering hundreds of voices. Glasses and Muse Charm voice are coming soon. — [CellCog](https://cellcog.ai/blog/muse-spaces/), citing [Meta Newsroom, Give Your Muse a Voice](https://about.fb.com/news/2026/11/introducing-muse-ai-voice-expressive-avatars/)
- Muse Realtime Avatar gives the agent a face, body and voice for real-time video chat (announced at Connect 2026-09-23). Glasses integration uses a wake word. — [TechCrunch](https://techcrunch.com/2026/09/23/everything-new-coming-to-metas-ai-agent-muse/)
- Muse Charm is a palm-sized device for using the agent away from a phone. — [Bloomberg](https://www.bloomberg.com/news/articles/2026-09-23/meta-debuts-a-dedicated-palm-sized-muse-charm-device-to-use-ai-on-the-go), via [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent))
- Outbound business calls exist (see the 404 Media item in section 8).

### Inferences
- This is comparable to Silo's voice calls (WebRTC, barge-in, talker). Muse adds video avatars, hardware and agent-placed phone calls, which Silo lacks.

### Gaps
- I found no technical detail on latency, barge-in, or STT/TTS model choice.

## 10. Clients (web, mobile, desktop, CLI)

### Takeaway
Clients are iOS, Android, the web (muse.ai), WhatsApp and a Mac app (2026-09-17), with Meta AI glasses and the Muse Charm and Home Link devices announced. The CLI sibling is Muse Code, a separate product.

### Cited Findings
- iOS, Android and muse.ai at launch, with glasses coming soon. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- Mac app — [Business Today](https://www.businesstoday.in/technology/news/story/metas-muse-ai-comes-to-macbook-ai-agent-can-organise-files-emails-and-more-556344-2026-09-18); [AI Weekly](https://aiweekly.co/alerts/meta-ships-muse-for-mac-agent-now-acts-in-native-macos-apps)
- App tabs: Library (created files), Upcoming (reminders) and Activity log. — [aiagentslibrary](https://www.aiagentslibrary.com/blog/how-to-use-meta-muse/) (secondary)
- Muse Code is a terminal agent. — [Decrypt](https://decrypt.co/375001/muse-code-meta-ai-coding-agent-claud-codex)

### Gaps
- I found nothing on Windows or Linux desktop apps.

## 11. Security & safety

### Takeaway
Security is Muse's most elaborate and most publicized area, and it is architecturally very close to Silo:
- **Sentinel** is a separate host-side agent and the sole authority for connector actions and all network egress, with allow/deny/ask decisions at L4 and L7.
- Credential surrogation: the agent never holds real tokens.
- Kernel-level "tainted egress" tracking uses eBPF and LSM.
- A classifier ensemble screens for prompt injection.
- Approval scopes are graded, with an approval UI outside the chat.
- Email OTP and reset links are stripped.
- Purchases use single-use virtual cards.
- A bug bounty pays up to $300k.

Incidents: shared chats were discoverable, Amazon blocked the agent, there were human-staffed "AI" calls, and the employee-reported problems.

### Cited Findings
- **Sentinel:**
  - It is the "sole permission authority" for connector actions and all network egress, reached through a forward proxy.
  - It decides allow, deny or ask for each action and request.
  - Network checks run at L4 (host, IP, port, protocol) and L7 (method, path, contents), with SSRF and DNS-rebinding protection.
  - — [Meta research blog](https://research.meta.ai/blog/how-we-built-safety-into-muse); [TheWindowsClub](https://www.thewindowsclub.com/is-meta-muse-safe-ai-sentinel-protects); [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- **Tainted egress:** each tool process starts clean and becomes tainted once it reads user data. Clean, narrow requests can be auto-allowed, while tainted or unverifiable ones need human approval. It is implemented with eBPF on cgroups plus LSM hooks. — [Meta research blog](https://research.meta.ai/blog/how-we-built-safety-into-muse); [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- **Approvals:** they can be one-time, session-bound, task-bound, time-bounded or permanent, each scoped to a connector, a destination and a use. They appear in a client dialog outside the chat, and the answer goes to Sentinel, not through the model. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- **Prompt-injection defense, five layers:**
  1. Model training.
  2. Labelling of untrusted content.
  3. A parallel classifier ensemble (`hatch-safety`).
  4. Human approval for data leaving the VM.
  5. Deterministic boundaries.
  - It explicitly cites Simon Willison's "lethal trifecta". — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026); [DeepLearning.AI The Batch](https://www.deeplearning.ai/the-batch/how-to-secure-agents-for-the-masses)
- **Email filter:** OTP codes, password-reset links and magic login links are stripped from what the agent can read. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- **Purchases:** every checkout needs approval. New merchants get a single-use Stripe Link virtual card limited to one merchant, one amount and a short window. Meta says it is the first agent covered by Link purchase protections. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026); [eesel](https://www.eesel.ai/blog/meta-muse-agent-review)
- **Bug bounty:** up to $300,000, and up to $130,000 for a working prompt injection that compromises one user. — [Meta research blog](https://research.meta.ai/blog/security-and-safety-for-ai-agents-our-approach-with-muse); [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- Zuckerberg said Meta delayed Muse "to focus on safety and security". — via [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- **Incidents:**
  - Shared chats could be found through passive DNS, and Meta suspended sharing (Oct 2026). — [Ars Technica](https://arstechnica.com/security/2026/10/meta-muse-exposes-users-personal-and-business-secrets-when-they-share-conversations/)
  - Amazon blocked Muse on 2026-09-21, saying it did not identify itself and appeared to capture credentials. Meta denied seeing passwords or payment methods. — [GeekWire](https://www.geekwire.com/2026/amazon-blocks-metas-muse-ai-assistant-in-new-standoff-over-agentic-shopping/)
  - Reuters, via Implicator, cited employee testers describing unexplained disconnects and uploads of sensitive information. — via [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- **Privacy:**
  - Conversations and tool calls train models by default after PII sanitization, with an opt-out. — [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent)); [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
  - Conversation and VM data are not sent to ad systems, but merchant visits can still fire retargeting pixels. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
  - EPIC warns of "a huge range of risks", including combining agent data with data-broker information. — [CNBC](https://www.cnbc.com/2026/10/22/meta-muse-personal-agents.html)

### Inferences
- **The closest match to Silo of any rival:**
  - Sentinel corresponds to Silo's egress gateway plus its rules engine.
  - Credential surrogation corresponds to Silo's surrogate secrets.
  - `hatch-safety` corresponds to Silo's output filter.
  - The out-of-chat approval dialog corresponds to Silo's approval slip.
- **Where Muse goes further:**
  - kernel-level taint tracking, which decides when approval is needed by data flow (Silo decides per action and per rule);
  - stripping OTPs from email;
  - single-use payment cards.
- **Where Silo goes further:**
  - self-hosting;
  - operator and workspace rule floors;
  - user-editable natural-language policy for the filter and auto-approval.

### Gaps
- I did not read Meta's research post directly. The details come from two independent secondary summaries (explainx, TheWindowsClub/DeepLearning.AI) that agree.

## 12. Model providers & cost

### Takeaway
Muse runs only on Meta's own models: Muse Spark 1.3 at launch, and from 2026-11-06 models trained with Muse Wonder. There is no bring-your-own-model and no model choice. Usage is metered in weekly "Muse tokens".

### Cited Findings
- Model is Muse Spark 1.3, shipped five days before Muse and the same model behind Muse Code. There is no model choice. — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026); [eesel](https://www.eesel.ai/blog/meta-muse-agent-review)
- Spark 1.3 trails Opus 5 on several agent benchmarks, and its top reasoning tier was gated at launch (the reviewer's opinion). An HN user said it "feels not far below opus 4.8ish". — [eesel](https://www.eesel.ai/blog/meta-muse-agent-review)
- On 2026-11-06 Muse moved to models trained with Muse Wonder. — [Meta AI Research](https://research.meta.ai/blog/specialized-models-for-muse-and-metaai), via [CellCog](https://cellcog.ai/blog/muse-spaces/)
- Muse Spark API: $1.25 per million input tokens and $4.25 per million output tokens (muse-spark-1.3). It is a separate product and not included in Muse subscriptions. — [openclawdatabase](https://openclawdatabase.com/meta-muse/pricing/), citing [dev.meta.ai](https://dev.meta.ai/docs/pricing-rate-limits)

### Inferences
- Model lock-in is total, in contrast to Silo's multi-provider registry (OpenRouter, Anthropic, Claude subscription, local and others).

## 13. Multi-agent / subagents

### Takeaway
I found no documented user-facing subagents or parallel agents in the Muse personal agent. Its "second agent" is Sentinel, which is a safety monitor, not a worker. Parallel subagents are documented for the Muse Spark model and for Muse Code.

### Cited Findings
- Meta's launch post mentions a separate Sentinel agent on the VM only as a safety component. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- Muse Spark 1.1 can act as a main agent delegating to parallel subagents (Medium post; treat with caution). — [Medium](https://medium.com/@tahirbalarabe2/meta-muse-spark-1-1-launches-with-agentic-ai-and-computer-use-57e84b0ebc23)
- Muse Code coordinates multiple persistent subagents per task. — [Decrypt](https://decrypt.co/375001/muse-code-meta-ai-coding-agent-claud-codex)

### Inferences
- The consumer agent likely uses subagents internally, given the model's capabilities, but does not expose them as a feature (inference).

### Gaps
- I found no source confirming user-visible subagents or a taskboard in Muse.

## 14. Web search, browsing

### Takeaway
Muse has a dedicated, watchable Chromium in the VM with accessibility-tree perception and injection classifiers, a "Browser" connector, and the user can take manual control. Its web access is limited by sites that block agents, most visibly Amazon.

### Cited Findings
- It has its own browser for opening pages, filling forms and negotiating. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- Browser design (accessibility tree, no page JavaScript, blocklists, pauses during manual control) — [explainx.ai](https://www.explainx.ai/blog/meta-muse-personal-agent-launch-sentinel-vm-security-2026)
- "Browser" is listed among Meta's own connectors. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list)
- Amazon blocked it, and agent capabilities fail where platforms disallow automated browsers. — [GeekWire](https://www.geekwire.com/2026/amazon-blocks-metas-muse-ai-assistant-in-new-standoff-over-agentic-shopping/); [Forbes](https://www.forbes.com/sites/the-prompt/2026/09/23/amazons-68-billion-reason-to-block-metas-muse/)
- Browsing and exploring cost more tokens than connector reads. — [openclawdatabase](https://openclawdatabase.com/meta-muse/pricing/)

### Gaps
- I found no documentation of a separate web-search engine or tool.

## 15. Output surfaces

### Takeaway
Outputs appear in chat (app and WhatsApp), a **Library** of created files, an **Upcoming** list, an **Activity** audit log, and **Spaces**, which are user-described mini-apps and dashboards refreshed from connectors (2026-11-06). Public shared-chat pages existed until they were suspended.

### Cited Findings
- Files appear in the Library tab, reminders in Upcoming, and actions in the Activity log. — [aiagentslibrary](https://www.aiagentslibrary.com/blog/how-to-use-meta-muse/)
- There is an audit trail of past and planned actions. — [Meta Newsroom](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/)
- Spaces — [Meta Newsroom](https://about.fb.com/news/2026/11/introducing-spaces-customize-muse-app/), via [CellCog](https://cellcog.ai/blog/muse-spaces/)
- Shared chat pages were suspended. — [Ars Technica](https://arstechnica.com/security/2026/10/meta-muse-exposes-users-personal-and-business-secrets-when-they-share-conversations/)

### Inferences
- These map onto Silo's Artifacts, Dashboard/Feed and runs log.

## 16. Adoption & ecosystem

### Takeaway
Adoption is very fast: 9M US downloads in 36 days (Sensor Tower), versus 670 days for Claude. One report gives about 700k daily users, with usage running roughly 10x Meta's projections, which forced limit resets. The ecosystem is roughly 60 to 80 connectors, 1,500 to 2,000+ developer submissions, a Gadgets SDK, and expansion to Japan and Singapore.

### Cited Findings
- 9M US downloads in 36 days, versus Claude's 670 days. Meta worked with Stripe to raise credit limits. Citi estimates $27B of revenue over several years. — [CNBC](https://www.cnbc.com/2026/10/22/meta-muse-personal-ai-agents.html)
- "Meta Muse Hits 700,000 Daily Users as Service Issues Appear" (headline only; the page returned 403). — [Windows Report](https://windowsreport.com/meta-muse-hits-700000-daily-users-as-service-issues-appear/)
- Usage exceeded internal projections by about 10x, and Meta reset token usage and raised limits (search-summary claim; secondary). — search results incl. [Windows Report](https://windowsreport.com/meta-muse-hits-700000-daily-users-as-service-issues-appear/)
- More than 1,500 connector applications in under a week. — [TechCrunch](https://techcrunch.com/2026/09/23/everything-new-coming-to-metas-ai-agent-muse/). More than 2,000 submissions in the first days. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list)
- A cottage industry of comparisons and third-party trackers has grown up (MyClaw, openclawdatabase, toolradar). — [MyClaw](https://myclaw.ai/blog/muse-ai-alternatives); [toolradar](https://toolradar.com/compare/meta-muse-vs-openclaw)

### Gaps
- Meta has published no MAU or DAU figure. The 700k DAU figure is unverified (I could not open the article).

## 17. Known weaknesses / complaints

### Takeaway
The main complaints:
- trust in Meta as the owner;
- default training on user data;
- the Meta login dependency;
- unreliable recurring tasks, slowness, and token caps hit quickly, with opaque token accounting;
- blocks by sites (Amazon);
- the shared-chat exposure;
- human-staffed calls;
- region limits;
- an OAuth refresh bug in custom connectors.

### Cited Findings
- Trust in the owner: "Love the idea and the polish. Hate the owners". There are also concerns about the Facebook login and dependency on Meta. — [eesel](https://www.eesel.ai/blog/meta-muse-agent-review), citing [HN](https://news.ycombinator.com/item?id=49620729)
- Recurring tasks are unreliable. — [Digg](https://digg.com/ai/heqfn47g)
- Meta does not publish how tasks translate to tokens. Free-tier work stops at the cap until the weekly reset, and large tasks can hit time limits. — [openclawdatabase](https://openclawdatabase.com/meta-muse/pricing/); [roborhythms](https://www.roborhythms.com/is-meta-muse-free/)
- Slowness ("super slow"). — [SmartScope](https://smartscope.blog/en/blog/muse-processing-speed-best-use-cases-common-pitfalls-2026/) (secondary)
- Amazon block, shared-chat exposure, human call-center calls — [GeekWire](https://www.geekwire.com/2026/amazon-blocks-metas-muse-ai-assistant-in-new-standoff-over-agentic-shopping/); [Ars Technica](https://arstechnica.com/security/2026/10/meta-muse-exposes-users-personal-and-business-secrets-when-they-share-conversations/); [404 Media](https://www.404media.co/meta-tests-muse-ai-agent-calls-that-are-actually-made-by-humans-in-a-call-center/)
- Custom OAuth connectors drop after about an hour. — [PostFast](https://postfa.st/blog/meta-muse-connectors-list)
- A New Mexico judge ordered Meta to pay $25B over data practices (context, not Muse-specific). — [CNBC](https://www.cnbc.com/2026/10/22/meta-muse-personal-ai-agents.html)

## 18. Pricing & license

### Takeaway
Muse is closed and proprietary, with free and paid tiers:

| Plan | Price | Weekly allowance |
|---|---|---|
| Free | $0 | About 100M Muse tokens (reported by testers) |
| Power | $20/month | 500M Muse tokens |
| Max | $100/month | 3B Muse tokens |

There are no ads, and Meta plans to earn transaction fees on commerce. Only the Gadgets SDK is open source.

### Cited Findings
- Free tier plus $20 and $100 per month. No advertising, and Meta plans to "profit by taking a small fee from transactions". — [Wikipedia](https://en.wikipedia.org/wiki/Muse_(AI_agent)), citing Axios and TechCrunch
- Plan names and allowances (Power $20 with 500M tokens a week, Max $100 with 3B a week; Free about 100M, unofficial). Both paid tiers have the same features. — [openclawdatabase](https://openclawdatabase.com/meta-muse/pricing/), citing [Meta Help Center](https://www.meta.com/help/subscriptions/1021145227643680/) (the Meta page did not render for me)
- Muse Gadgets SDK is open source. — [GitHub](https://github.com/facebookincubator/muse-gadget-sdk), via [PostFast](https://postfa.st/blog/meta-muse-connectors-list)

### Inferences
- Muse competes on price and convenience: it is free with no setup. Silo competes on self-hosting, multi-user control, model choice and data ownership.

### Gaps
- I could not verify the free-tier allowance against a Meta primary source.
