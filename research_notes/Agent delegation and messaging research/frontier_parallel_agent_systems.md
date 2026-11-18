# Frontier parallel / multi-agent inference and delegation systems (2025–2026)

Researched 2026-11-17. Labels used throughout: **[OFFICIAL]** = the lab's own blog, docs, model or system card; **[3P]** = credible third-party reporting (press, practitioner write-ups); **[SPEC]** = speculation, unverified secondary blogs, or my own inference. Many "how Grok works" pages online are SEO blogs that repeat each other. They are marked [SPEC] even where they agree.

---

## 1. xAI: Grok 4 Heavy (July 2025) and Grok 4.20 Multi-Agent (Feb–Mar 2026)

### Takeaway
Grok 4 Heavy (2025-07-09) is officially described only as "parallel test-time compute" that "considers multiple hypotheses at once". Musk said on the livestream that independent agents work in parallel and then "compare notes like a study group", and that the answer is "not always majority vote". So it is parallel sampling plus a comparison and aggregation step, with no documented messaging between agents during a run. Grok 4.20 Multi-Agent (consumer beta 2026-02-17; API `grok-4.20-multi-agent`, March 2026) is a different, documented product: a **leader agent plus 4 or 16 sub-agents** run server-side. The sub-agents "discuss and collaborate", "may run in parallel", and use server-side tools, and the leader synthesizes the answer. xAI does not document how the sub-agents exchange information. The named roles (Grok, Harper, Benjamin, Lucas) and the hallucination-reduction numbers come only from secondary sources.

### Cited Findings
**Grok 4 Heavy (July 2025)**
- [OFFICIAL] The xAI announcement is dated 2025-07-09. It says Grok 4 Heavy uses "parallel test-time compute" that lets it "consider multiple hypotheses at once". The illustration shows parallel "Agent 1…Agent 3" and "Thought for 10 minutes". No agent count is given in the text. — [xAI, "Grok 4"](https://x.ai/news/grok-4)
- [OFFICIAL] Benchmarks on that page: Grok 4 Heavy is described as the first model to reach 50% on Humanity's Last Exam (50.7% on the text-only subset), and it scores 61.9% on USAMO'25. The page says it "saturates most academic benchmarks". Base Grok 4 scores 15.9% on ARC-AGI-2. — [xAI, "Grok 4"](https://x.ai/news/grok-4)
- [OFFICIAL] Access is through a new SuperGrok Heavy tier. — [xAI, "Grok 4"](https://x.ai/news/grok-4)
- [3P] TechCrunch reports that the tier costs $300/month. Musk said Heavy "spawns multiple agents to work on a problem simultaneously, and then they all compare their work 'like a study group' to find the best answer." — [TechCrunch, 2025-07-09](https://techcrunch.com/2025/07/09/elon-musks-xai-launches-grok-4-alongside-a-300-monthly-subscription)
- [3P] Musk quoted: the agents "do work independently, and then they compare their work. Often only one of the agents actually figures out the trick or figures out the solution." — [Built In](https://builtin.com/artificial-intelligence/grok-4). AiNews reports xAI saying it is "not always majority vote". — [AiNews](https://www.ainews.com/p/xai-launches-grok-4-and-grok-4-heavy-aims-to-redefine-ai-intelligence)
- [3P] Axios: the Heavy version "deploys multiple agents to collaborate on solving particularly tough problems." — [Axios, 2025-07-10](https://www.axios.com/2025/07/10/grok4-grok-xai-elon-musk)
- [SPEC] Claims of "up to 16 parallel instances" for Grok 4 Heavy come from SEO-style secondary pages and are not confirmed by xAI. — [grokmag](https://grokmag.com/grok-models/grok-heavy/), [ai-tldr](https://ai-tldr.dev/models/grok-4-heavy/)

**Grok 4.20 Multi-Agent (2026)**
- [OFFICIAL] xAI docs, "Multi Agent" (beta, "may include breaking changes"). The model is `grok-4.20-multi-agent`. "Several agents are launched to discuss and collaborate on a query," and a **leader agent** "is responsible for synthesizing the discussion and presenting the final answer." The docs say agents can "iterate on research in real time" and "may run in parallel". The mechanism by which sub-agents pass information to each other is **not documented**. — [docs.x.ai multi-agent](https://docs.x.ai/developers/model-capabilities/text/multi-agent)
- [OFFICIAL] Agent count: `agent_count=4` or `16` in the xAI SDK. In the OpenAI-compatible and REST APIs, `reasoning.effort` low/medium gives 4 agents and high/xhigh gives 16. The docs say: "More agents means deeper, more thorough research at the cost of higher token usage and latency." They recommend 4 agents for quick, focused queries and 16 for deep, multi-faceted research. — [docs.x.ai multi-agent](https://docs.x.ai/developers/model-capabilities/text/multi-agent); [docs.x.ai reasoning](https://docs.x.ai/developers/model-capabilities/text/reasoning)
- [OFFICIAL] Tools and agent loop: the server runs the loop and the built-in tools (`web_search`, `x_search`, `code_execution`, `collections_search`, remote MCP). Any agent, leader or sub-agent, may call them. Client-side function calling and custom tools are **not supported**, and neither are Chat Completions or `max_tokens`. — [docs.x.ai multi-agent](https://docs.x.ai/developers/model-capabilities/text/multi-agent)
- [OFFICIAL] Output: only the leader's tool calls and final response are returned. Sub-agent state comes back encrypted only with `use_encrypted_content=True`, so it can carry over between turns (`previous_response_id`). — [docs.x.ai multi-agent](https://docs.x.ai/developers/model-capabilities/text/multi-agent)
- [OFFICIAL] Cost: every token of the leader and all sub-agents (input, output and reasoning) is billed, and so is each server-side tool call by any agent. — [docs.x.ai multi-agent](https://docs.x.ai/developers/model-capabilities/text/multi-agent)
- [3P] OpenRouter lists `x-ai/grok-4.20-multi-agent` at $1.25/M input and $2.50/M output with a 2M-token context window, and gives the same effort→agents map (low/medium = 4, high/xhigh = 16). — [OpenRouter](https://openrouter.ai/x-ai/grok-4.20-multi-agent)
- [3P/SPEC] Timeline: consumer beta on grok.com on 2026-02-17, API variants around 2026-03-09/10 (`grok-4.20-multi-agent-beta-20260309`). OpenRouter lists a release date of 2026-03-31 for the non-beta model. — [apiyi docs](https://docs.apiyi.com/en/news/grok-4-20-beta-launch), [OpenRouter beta-20260309](https://openrouter.ai/x-ai/grok-4.20-multi-agent-beta-20260309), [NextBigFuture](https://www.nextbigfuture.com/2026/02/xai-launches-grok-4-20-and-it-has-4-ai-agents-collaborating.html)
- [SPEC] Named roles in the consumer app: "Grok/Captain" decomposes the query and synthesizes, "Harper" researches with live X and web data, "Benjamin" does logic, math and verification, and "Lucas" plays the contrarian. One account has Benjamin checking Harper's findings before Grok writes the answer, which would be cross-checking between agents. These details come from X posts and blogs, not from xAI docs. — [nextbigfuture on X](https://x.com/nextbigfuture/status/2023827848075899019), [adwaitx](https://www.adwaitx.com/grok-4-20-agents-harper-benjamin-lucas/)
- [SPEC] Reported hallucination numbers (about 65% fewer hallucinations, or 78% or 83% "non-hallucination" rates) disagree with one another and are labelled self-reported or early internal figures. One aggregator says xAI "has not yet published official comprehensive benchmark results." — [happycapyguide](https://happycapyguide.com/blog/grok-420-beta-4-agent-multi-agent-hallucination-reduction-xai-2026), [tokenmix](https://tokenmix.ai/blog/grok-4-20-multi-agent-review-2026), [apiyi docs](https://docs.apiyi.com/en/news/grok-4-20-beta-launch)

### Inferences
- Grok 4 Heavy fits "parallel independent rollouts plus a learned or prompted comparator". Musk's line that often only one agent finds the trick, and that the result is not a majority vote, points to selection or synthesis rather than voting. That makes it a cousin of best-of-N, not delegation.
- Grok 4.20 Multi-Agent is the nearest thing among the production APIs to "agents that discuss". The word "discussion" in xAI's docs, together with the leader synthesizing it, suggests the agents see each other's contributions. Whether that happens mid-run (a shared transcript, rounds) or only at aggregation is undocumented. It is also closed: there are no custom tools and sub-agent state is hidden or encrypted, so a third party cannot reproduce or inspect the coordination.
- Mapping `reasoning.effort` to agent count rather than to thinking depth is notable: xAI treats parallel agents as the main lever for test-time compute.

### Gaps
- No xAI paper or blog explains Grok 4 Heavy's aggregation (verifier? judge? synthesis?) or its exact agent count. I could not get a livestream transcript.
- I found no official xAI launch post for Grok 4.20 with benchmark tables. Whether the 4.20 agents exchange messages round by round, share one context, or are separate rollouts that the leader reads is undocumented.
- I found nothing on a "Grok Bot" product with sub-agents.

---

## 2. Google DeepMind: Gemini Deep Think ("parallel thinking")

### Takeaway
Deep Think is a reasoning mode in one model. It generates several lines of reasoning in parallel and, per Google as reported, can revise or combine them before answering. It is parallel test-time compute inside the model, not tool-using sub-agents. Measured: IMO 2025 gold (35/42, 5 of 6 problems solved, officially graded), with no tools.

### Cited Findings
- [OFFICIAL] On 2025-07-21, an advanced Gemini Deep Think solved 5 of 6 IMO problems perfectly for 35 points, a gold-medal standard officially graded by IMO coordinators. It worked end to end in natural language, with no tool use or internet. It "incorporates… parallel thinking" plus new RL techniques on multi-step reasoning and theorem-proving data. — [DeepMind blog, 2025-07-21](https://deepmind.google/discover/blog/advanced-version-of-gemini-with-deep-think-officially-achieves-gold-medal-standard-at-the-international-mathematical-olympiad); quoted at [Simon Willison, 2025-07-21](https://simonwillison.net/2025/Jul/21/gemini-imo/)
- [OFFICIAL] On 2025-08-01, Gemini 2.5 Deep Think rolled out in the Gemini app to Google AI Ultra subscribers and "uses parallel thinking" for more detailed answers. Mathematicians got the full IMO version. — [blog.google, "Try Deep Think in the Gemini app"](https://blog.google/products-and-platforms/products/gemini/gemini-2-5-deep-think/)
- [3P] InfoQ: Google says the model generates several candidate solutions in parallel, then revises or merges them during reasoning to reach the final answer. — [InfoQ, 2025-08](https://infoq.com/news/2025/08/gemini-2-5-deep-think)
- [3P] The app version is faster than the IMO model, which reportedly took hours. — [9to5Google, 2025-08-01](https://9to5google.com/2025/08/01/gemini-2-5-deep-think/). Ultra costs $250/month. — [etcentric](https://www.etcentric.org/?p=194840)

### Inferences
- This is "parallel sampling with in-model aggregation": the branches are not separate agents with tools. Combining ideas across branches is more than pure best-of-N, but it happens inside one inference process, not through messages between agents.

### Gaps
- There is no public technical description of how branches are produced or merged, or how many there are. I did not check Gemini 3 Deep Think (later 2025/2026) for a changed mechanism or numbers.

---

## 3. OpenAI: GPT-5 Pro / GPT-5.5 Pro ("parallel test-time compute"), Agents SDK, Codex

### Takeaway
OpenAI's "Pro" is officially only "a setting that makes use of parallel test time compute" on the same model. No sample count or aggregation method is disclosed. The Agents SDK documents two delegation patterns: **handoffs**, which pass control and conversation history to a specialist, and **agents-as-tools**, where a manager calls a sub-agent and keeps control. Neither involves agents messaging each other during a run.

### Cited Findings
- [OFFICIAL] GPT-5 system card: gpt-5-thinking-pro is "gpt-5-thinking using a setting that makes use of parallel test time compute". Safety evaluations of gpt-5-thinking were treated as strong proxies, so Pro was not re-evaluated separately. — [GPT-5 System Card (arXiv 2601.03267)](https://arxiv.org/html/2601.03267v1); [openai.com GPT-5 system card](https://openai.com/de-DE/index/gpt-5-system-card/)
- [OFFICIAL] The GPT-5.5 system card (April 2026) describes GPT-5.5 Pro the same way, as the same model with the parallel test-time-compute setting, evaluated separately only where the setting could change the risk. — [GPT-5.5 system card](https://openai.com/index/gpt-5-5-system-card)
- [OFFICIAL, via secondary summaries] Agents SDK orchestration: with agents-as-tools (`agent.asTool()`) the manager keeps control and history is not inherited automatically. With a handoff, the specialist becomes the active agent and gets the full conversation history by default (filterable). — [OpenAI, "Orchestration and handoffs"](https://developers.openai.com/api/docs/guides/agents/orchestration); [openai-agents-js handoffs](https://openai.github.io/openai-agents-js/guides/handoffs); [agentpatterns.ai](https://agentpatterns.ai/patterns/agent-design/agent-as-tool-vs-handoff/)

### Inferences
- From the outside, Pro is the purest "best-of-N / parallel sampling plus aggregator": the user sees one model and one answer.

### Gaps
- No public numbers on Pro's sample count, aggregation (vote or judge), or the cost multiple over Thinking.
- My search found no primary documentation on Codex sub-agents or parallel Codex tasks. Codex cloud's "multiple attempts" per task (best-of-N with a human picking) is something I remember but did not verify in this session.

---

## 4. Moonshot AI: Kimi K2.5 Agent Swarm and PARL

### Takeaway
This is the only production system found where parallel delegation is **trained by RL**. A trainable orchestrator spawns frozen sub-agents on its own (up to 100 by Kimi's blog) that run in parallel with independent memory. Only their task-relevant outputs return to it. There is no documented communication between sub-agents. The reward explicitly fights two failure modes: "serial collapse" (never parallelizing) and "spurious parallelism" (spawning sub-agents without real decomposition). Gains: BrowseComp 60.6 → 78.4 and WideSearch item-F1 72.7 → 79.0, with a 3–4.5× wall-clock reduction.

### Cited Findings
- [OFFICIAL] Tech report "Kimi K2.5: Visual Agentic Intelligence" (arXiv 2602.02276, early Feb 2026). PARL uses a **trainable orchestrator** and **frozen sub-agents** taken from fixed intermediate checkpoints. Sub-agent trajectories are left out of the optimization, and their outputs are treated as environment observations. — [arXiv 2602.02276](https://arxiv.org/html/2602.02276v1)
- [OFFICIAL] At each stage, the orchestrator either calls a tool directly or launches a group of sub-agents in parallel. Parallelism is learned and "not presumed to be inherently advantageous". Sub-agents are created dynamically, not predefined, each with independent working memory. Only task-relevant outputs, not full traces, go back to the orchestrator, which the paper calls "context sharding". — [arXiv 2602.02276](https://arxiv.org/html/2602.02276v1)
- [OFFICIAL] Reward: r_PARL = λ₁·r_parallel + λ₂·r_finish + r_perf. r_parallel counters serial collapse, and r_finish (the sub-agent finish rate) counters spurious parallelism and reward hacking by over-spawning. λ₁ and λ₂ are annealed to 0. Cost is counted in "critical steps": per stage, the orchestrator's steps plus the **longest** sub-agent's steps, like the critical path of a computation graph. Splitting work without shortening the longest branch earns nothing. — [arXiv 2602.02276](https://arxiv.org/html/2602.02276v1)
- [OFFICIAL] Results, single agent → swarm: BrowseComp 60.6 → 78.4; WideSearch item-F1 72.7 → 79.0; in-house Swarm Bench 41.6 → 58.3. On WideSearch, time to a target score falls 3–4.5×. At a 70% target, single-agent time rises past 7× the baseline while the swarm stays around 0.6–1.6×. — [arXiv 2602.02276](https://arxiv.org/html/2602.02276v1)
- [OFFICIAL] Kimi's blog: it "self-direct[s] an agent swarm of up to 100 sub-agents, executing parallel workflows across up to 1,500 coordinated steps," with no predefined roles or hand-made workflows, and is up to 4.5× faster than a single agent. — [Kimi blog, K2.5](https://kimi.com/blog/kimi-k2-5.html)
- [3P] Practitioner summary of how Kimi, Cursor and Chroma train agentic models with RL. — [Phil Schmid](https://www.philschmid.de/kimi-composer-context)
- [OFFICIAL] When it helps: the largest gains are on broad, parallelizable work (wide search, multi-branch deep research, independent subtasks), and the training prompts were built to favour those patterns. The paper does not report token or compute cost for swarm runs. — [arXiv 2602.02276](https://arxiv.org/html/2602.02276v1)

### Inferences
- Structurally this matches Anthropic's orchestrator-worker design (independent workers, compressed returns), with the orchestrator's decomposition and fan-out policy learned rather than prompted. The critical-steps metric is a useful idea: parallelism pays only if it shortens the longest branch.

### Gaps
- No token-cost figures for swarm mode. I did not verify Kimi K2 Thinking's (Nov 2025) "heavy mode", which I remember as running 8 parallel trajectories and then reflectively aggregating them. Treat that as unverified.

---

## 5. Anthropic: multi-agent Research system, Claude Code subagents, agent teams

### Takeaway
Anthropic's Research feature (engineering post, 2025-06-13) is the canonical **orchestrator-worker** design. An Opus 4 lead plans and spawns Sonnet 4 subagents that search **in parallel** but **synchronously**: the lead waits for each batch, and "subagents can't coordinate". A separate CitationAgent attributes sources. It beat single-agent Opus 4 by **90.2%** on an internal research eval at about **15× the tokens of chat**. Token use alone explained 80% of the variance on BrowseComp. Claude Code then added **agent teams** (experimental), a real peer-messaging design: teammates are separate sessions with a **mailbox** (`SendMessage`), a **shared task list** with claiming and dependencies, and direct messages to each other by name.

### Cited Findings
**Research system (2025-06-13)**
- [OFFICIAL] The lead agent plans and spawns subagents that search in parallel, each with its own context window. The lead is Claude Opus 4 and the subagents Claude Sonnet 4. — [Anthropic Engineering, "How we built our multi-agent research system"](https://www.anthropic.com/engineering/multi-agent-research-system)
- [OFFICIAL] Execution is synchronous: the lead waits for each set of subagents. Stated limits: "the lead agent can't steer subagents, subagents can't coordinate." The lead synthesizes and decides whether to research further, saves its plan to memory because context is cut past 200k tokens, and a CitationAgent attributes claims at the end. Subagents can write outputs to external storage and pass lightweight references back. — [same](https://www.anthropic.com/engineering/multi-agent-research-system)
- [OFFICIAL] Gain: Opus 4 lead with Sonnet 4 subagents beat single-agent Opus 4 by 90.2% on the internal research eval. — [same](https://www.anthropic.com/engineering/multi-agent-research-system)
- [OFFICIAL] Cost: on BrowseComp, token usage explained 80% of performance variance, and with tool-call count and model choice, 95%. Agents use about 4× the tokens of chat and multi-agent systems about 15×. — [same](https://www.anthropic.com/engineering/multi-agent-research-system)
- [OFFICIAL] Effort scaling in the prompts: 1 agent with 3–10 tool calls for a simple fact, 2–4 subagents with 10–15 calls each for a comparison, more than 10 for complex research. An early failure spawned "50 subagents for simple queries." Running 3–5 subagents in parallel, each making 3+ tool calls in parallel, cut research time by up to 90% on complex queries. — [same](https://www.anthropic.com/engineering/multi-agent-research-system)
- [OFFICIAL] When it helps: breadth-first queries, highly parallel work, information beyond one context window, many complex tools. When it hurts: domains needing shared context or with many dependencies between agents, such as "most coding tasks". Asynchronous execution is named as future work, with its problems: coordinating results, keeping state consistent, and errors propagating. — [same](https://www.anthropic.com/engineering/multi-agent-research-system)

**Claude Code agent teams (experimental, current docs)**
- [OFFICIAL] Agent teams are disabled by default (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`). One session is the team lead and the teammates are separate Claude Code instances, each with its own context window, that "communicate directly with each other". The user can message any teammate too. — [Claude Code docs, agent teams](https://code.claude.com/docs/en/agent-teams)
- [OFFICIAL] Compared with subagents: subagents return a result to the caller ("Lower [token cost]: results summarized back to main context"). Teammates "message each other directly" and coordinate themselves through messages plus a shared task list ("Higher: each teammate is a separate Claude instance"). Named subagents can also message each other. — [same](https://code.claude.com/docs/en/agent-teams)
- [OFFICIAL] Mechanism: each agent's mailbox is a JSON file at `~/.claude/teams/{team}/inboxes/{agent}.json`, and messages are delivered automatically with no polling. An idle notification to the lead carries the teammate's final answer. The task list has pending, in progress and completed states with dependencies, and teammates claim tasks under file locks so they do not race. There is no broadcast: one message goes to each recipient. A teammate gets the lead's spawn prompt, not the lead's history. — [same](https://code.claude.com/docs/en/agent-teams)
- [OFFICIAL] Safety: a message from another agent is marked as coming from another Claude session, not from the user. A teammate cannot approve permissions on the user's behalf or pass on a denied action to get around the check. In auto mode, a classifier reviews messages between agents. — [same](https://code.claude.com/docs/en/agent-teams)
- [OFFICIAL] Guidance: start with 3–5 teammates and 5–6 tasks each, since "three focused teammates often outperform five scattered ones". Tokens scale linearly with team size. Teams are good for research, review, competing-hypothesis debugging ("talk to each other to try to disprove each other's theories") and features that span layers. They are bad for sequential work, edits to the same file, or work with many dependencies. Limits: no nested teams, a fixed lead, one team per session, task status can lag, and the lead may stop early or start doing the work itself. — [same](https://code.claude.com/docs/en/agent-teams)

### Inferences
- Anthropic itself runs both ends of the range: the Research system is strictly hub-and-spoke with no peer communication and was measured with numbers. Agent teams have full peer messaging but are experimental, and the docs give no benchmark gains for them, only qualitative guidance.

### Gaps
- No published numbers comparing agent teams with subagents or a single session.

---

## 6. Cursor: parallel agents (Cursor 2.0) and long-running multi-agent experiments

### Takeaway
Cursor 2.0 (2025-10-29) runs **up to 8 agents in parallel on one prompt**, each in its own git worktree or remote machine. That is best-of-N with a human picking, and the agents do not talk. Separately, Cursor's research harness (2026-01-14) ran **hundreds of concurrent agents** for weeks. Flat peer coordination through a shared file with locks **failed**. What worked was a hierarchy: **planners** (which can spawn sub-planners), **workers** that do not coordinate with each other, and a **judge** at each cycle boundary.

### Cited Findings
- [OFFICIAL] Cursor 2.0 changelog, 2025-10-29: "Run up to eight agents in parallel on a single prompt"; it "uses git worktrees or remote machines to prevent file conflicts"; "Each agent operates in its own isolated copy of your codebase." There are no measured gains for multi-agent use. — [Cursor changelog 2.0](https://cursor.com/changelog/2-0)
- [3P] Users describe it as best-of-N, often across different models, then comparing and picking. Early forum complaints say there was no UI for choosing among the worktrees. — [Medium guide](https://medium.com/towards-data-engineering/parallel-ai-agents-in-cursor-2-0-a-practical-guide-e808f89cffb9), [Cursor forum](https://forum.cursor.com/t/what-s-the-point-of-multiple-agents/141805)
- [OFFICIAL] "Scaling long-running autonomous coding," Wilson Lin, 2026-01-14. Attempt 1 had flat, equal agents coordinating through a shared file with locks: "Twenty agents would slow down to the effective throughput of two or three." It was brittle, with locks held and never released. Attempt 2 used optimistic concurrency, which was more robust, but with no hierarchy agents became risk-averse, avoided hard tasks and owned nothing end to end. — [Cursor blog](https://cursor.com/blog/scaling-agents)
- [OFFICIAL] The design that worked: planners explore and create tasks and can spawn sub-planners; workers "don't coordinate with other workers or worry about the big picture" and push to the same branch with "minimal conflicts"; a judge decides whether to continue at the end of each cycle, and each iteration starts fresh. An integrator role "created more bottlenecks than it solved" and was removed. — [Cursor blog](https://cursor.com/blog/scaling-agents)
- [OFFICIAL] Scale: a browser built from scratch in close to a week, over 1M lines across 1,000 files with hundreds of concurrent workers, and "trillions of tokens" overall. A Solid→React migration took over 3 weeks (+266K/−193K). Rendering got 25× faster in Rust (merged). Ongoing: a Java LSP (550K LoC), a Windows 7 emulator (1.2M LoC), Excel (1.6M LoC). — [Cursor blog](https://cursor.com/blog/scaling-agents)
- [OFFICIAL] Models: GPT-5.2 was "much better at extended autonomous work". Opus 4.5 "tends to stop earlier and take shortcuts when convenient". GPT-5.2 plans better than GPT-5.1-Codex, so models are now chosen per role. Lessons: "Prompting mattered more than the harness or models"; removing complexity helped; structure belongs in the middle; periodic fresh starts are still needed against drift. — [Cursor blog](https://cursor.com/blog/scaling-agents)
- [3P] A follow-up post, "Towards self-driving codebases", reportedly dropped the separate planner for a continuous executor. — [Cursor blog, self-driving codebases](https://cursor.com/blog/self-driving-codebases) (summarized by secondary sources, not fetched by me); [The Decoder](https://the-decoder.com/cursors-agent-swarm-suggests-cheaper-models-can-handle-most-coding-when-frontier-models-plan-the-work/)

### Inferences
- Cursor's result is the strongest production evidence against flat peer coordination: shared mutable coordination state became the bottleneck, and the fix was a hierarchy with workers that do not talk to each other, the same shape as Anthropic and Kimi.

### Gaps
- I did not fetch the "self-driving codebases" follow-up in full. There are no quality metrics beyond the project descriptions.

---

## 7. Manus Wide Research (2025-07-31)

### Takeaway
Manus Wide Research fans one task out to 100+ sub-agents, each "a fully capable, general-purpose Manus instance", usually one per item (100 sneakers, 50 posters). Manus calls it "a protocol for agent-to-agent collaboration" but does not describe the protocol, the merging, or any benchmarks.

### Cited Findings
- [OFFICIAL] "a system-level mechanism for parallel processing, and a protocol for agent-to-agent collaboration"; each subagent is "a fully capable, general-purpose Manus instance" rather than a fixed role; available to Pro, with Plus and Basic to follow. No benchmarks, merge method or limits are given. — [Manus blog, "Introducing Wide Research"](https://manus.im/blog/introducing-wide-research)
- [3P] It spins up 100+ agents. Demos compared 100 sneakers (one agent each) and made 50 poster designs. Critics note there is no evidence it beats one strong agent and no explanation of merging. — [VentureBeat](https://venturebeat.com/ai/youve-heard-of-ai-deep-research-tools-now-manus-is-launching-wide-research-that-spins-up-100-agents-to-scour-the-web-for-you), [AlternativeTo, 2025-07](https://alternativeto.net/news/2025/7/manus-unveils-new-wide-research-mode-enabling-100-parallel-ai-agents-for-scalable-tasks)
- [3P, single source] Pro costs $199/month. — [implicator.ai](https://www.implicator.ai/manus-unleashes-100-agent-swarms-challenging-openais-deep-learning-model/)

### Gaps
- The mechanism and any measured gains are undisclosed.

---

## 8. Classification: parallel sampling + aggregator vs delegation vs agents messaging mid-run

### Takeaway
Most production "multi-agent" systems are either (a) **parallel sampling of the same task with an aggregator** or (b) **hub-and-spoke delegation of separate subtasks with compressed returns and no peer communication**. Agents that message each other during a run appear officially only in Claude Code agent teams (experimental). Grok 4.20 Multi-Agent's agents "discuss" by xAI's own description, but the mechanism is undocumented. Cursor tried flat peer coordination at scale and dropped it.

### Cited Findings
- **(a) Same task, parallel samples, then aggregate:** GPT-5 Pro / GPT-5.5 Pro ([system card](https://arxiv.org/html/2601.03267v1)); Gemini Deep Think, where branches may also be combined inside the model ([blog.google](https://blog.google/products-and-platforms/products/gemini/gemini-2-5-deep-think/), [InfoQ](https://infoq.com/news/2025/08/gemini-2-5-deep-think)); Grok 4 Heavy, independent agents then a comparison that is "not always majority vote" ([x.ai](https://x.ai/news/grok-4), [Built In](https://builtin.com/artificial-intelligence/grok-4)); Cursor 2.0's up to 8 parallel agents with a human picking ([changelog](https://cursor.com/changelog/2-0)).
- **(b) Delegation of separate subtasks, hub-and-spoke, no peer messages:** Anthropic Research ("subagents can't coordinate") ([Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)); Kimi K2.5 Agent Swarm (independent memory, outputs only) ([arXiv](https://arxiv.org/html/2602.02276v1)); Claude Code subagents ([docs](https://code.claude.com/docs/en/agent-teams)); Cursor planners and workers ("workers don't coordinate with other workers") ([Cursor](https://cursor.com/blog/scaling-agents)); OpenAI Agents SDK handoffs and agents-as-tools ([OpenAI](https://developers.openai.com/api/docs/guides/agents/orchestration)); Manus Wide Research, probably, given one item per agent ([Manus](https://manus.im/blog/introducing-wide-research)).
- **(c) Agents messaging peers mid-run:** Claude Code agent teams (mailbox, `SendMessage` by name, shared task list) ([docs](https://code.claude.com/docs/en/agent-teams)); Grok 4.20 Multi-Agent ("discuss and collaborate", leader synthesizes, channel undocumented) ([docs.x.ai](https://docs.x.ai/developers/model-capabilities/text/multi-agent)). Cursor's flat shared-file coordination was tried and replaced ([Cursor](https://cursor.com/blog/scaling-agents)).

### Inferences
- Measured gains so far come from (a) and (b): Grok 4 Heavy's HLE, Deep Think's IMO, Anthropic's +90.2%, Kimi's BrowseComp +17.8 points. I found **no published benchmark isolating the value of messaging between agents mid-run** in a production system.

### Gaps
- See the per-system gaps above, chiefly the Grok 4.20 coordination channel and the aggregation methods for GPT-5 Pro and Grok 4 Heavy.

---

## 9. When multi-agent helps vs hurts (what the teams said)

### Takeaway
They agree: multi-agent helps on **breadth-first, parallelizable work whose pieces need little shared context** (wide search, research, review, competing hypotheses, independent modules). It hurts on **sequential, tightly coupled work** (most coding in one shared state, edits to the same file) and when coordination becomes the bottleneck. Cost grows roughly linearly with agents (Anthropic: about 15× chat), and performance is driven mostly by the tokens spent.

### Cited Findings
- Anthropic: helps with breadth, parallelism, information beyond one context and many tools; hurts where context must be shared or agents depend on each other, "most coding tasks"; about 15× tokens; tokens explain 80% of variance on BrowseComp; early prompts over-spawned (50 subagents for a simple query). — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- Claude Code docs: teams add coordination overhead and "significantly more tokens"; avoid them for sequential tasks, edits to the same file, or many dependencies; returns diminish past 3–5 teammates. — [docs](https://code.claude.com/docs/en/agent-teams)
- Kimi: parallelism is "not presumed to be inherently advantageous"; it must shorten the critical path; serial collapse and spurious parallelism are both failure modes the reward has to counter. — [arXiv](https://arxiv.org/html/2602.02276v1)
- Cursor: flat peers with locks gave the throughput of 2–3 agents from 20; with no hierarchy, agents grew risk-averse; an integrator role became a bottleneck; "Prompting mattered more than the harness or models"; more agents did help on very large projects. — [Cursor](https://cursor.com/blog/scaling-agents)
- xAI: "More agents means deeper, more thorough research at the cost of higher token usage and latency"; 4 agents for focused queries, 16 for deep research; every agent's tokens are billed. — [docs.x.ai](https://docs.x.ai/developers/model-capabilities/text/multi-agent)

### Inferences
- For a self-hosted product that conserves tokens, the evidence favours hub-and-spoke delegation with compressed returns, a cap on fan-out tied to effort (Anthropic's effort scaling, xAI's effort→agents map), and parallelism only where it shortens the longest branch (Kimi's critical steps). Messaging between peers mid-run is the least proven and costliest option.

### Gaps
- No lab published a controlled comparison of peer messaging against hub-and-spoke delegation at equal token budgets.
