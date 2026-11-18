# Communication topology in multi-agent LLM systems: peer messaging / blackboard vs hierarchical orchestration (2025–2026 evidence)

Scope note: research date 2026-11-17. Priority given to 2025–2026 frontier-model work. GPT-3.5/GPT-4-era results are flagged as historical. Where a source page did not name its models, that is stated (and listed under Gaps) rather than guessed.

## Q1. Controlled evidence: how do topologies compare on accuracy, error propagation, cost?

### Takeaway
The strongest controlled study (Kim et al., "Towards a Science of Scaling Agent Systems", arXiv 2512.08296, Dec 2025 / rev. Apr 2026, GPT-5 / Gemini 2.5 / Claude Sonnet 4.5 families) finds no topology wins in general. Mean multi-agent change versus a single agent is about −0.3%. Centralized (orchestrator, no peer links) contains errors best: trace-level error amplification is 4.4× centralized versus 7.8× decentralized peer messaging and 17.2× independent-and-aggregate. Peer messaging only clearly helped on high-entropy parallel web search. Every multi-agent topology costs 1.6–6× the tokens of one agent and gets far fewer successes per token. Debate re-evaluations from 2025 converge on one result: majority voting or self-consistency explains most of debate's gains, and debate rounds alone do not raise expected correctness.

### Cited Findings
**Kim et al., "Towards a Science of Scaling Agent Systems" (Google Research et al.), arXiv 2512.08296, v1 2025-12-09, v3 2026-04-08**
- Setup: 260 configurations, 6 agentic benchmarks (BrowseComp-Plus, Finance-Agent, PlanCraft, WorkBench, SWE-bench Verified, Terminal-Bench). Tools, prompts and compute are standardized. Five architectures: Single, Independent, Centralized, Decentralized, Hybrid. — [arXiv abs](https://arxiv.org/abs/2512.08296)
- Models: GPT-5-nano, GPT-5-mini, GPT-5; Gemini 2.0 Flash, 2.5 Flash, 2.5 Pro; Claude Sonnet 3.7, Sonnet 4, Sonnet 4.5. — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- Topology definitions:
  - Independent: agents report only to an aggregator, with no peer links.
  - Centralized: an orchestrator talks to the agents, with no peer links.
  - Decentralized: all-to-all peer communication in sequential debate rounds, with no hierarchy.
  - Hybrid: an orchestrator plus limited peer-to-peer exchange.
  
  — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- **Trace-level error amplification** relative to a single agent at 1.0: Independent 17.2, Decentralized 7.8, Hybrid 5.1, Centralized 4.4. The authors say architectures with verification contain errors better. Task-level amplification is about 1.1–1.3. — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- The abstract states that architectures without centralized verification propagate errors more than those with centralized coordination. — [arXiv abs](https://arxiv.org/abs/2512.08296)
- Per-benchmark change versus a single agent:

  | Benchmark | Results |
  | --- | --- |
  | Finance-Agent | Centralized +80.8%, Decentralized +74.5%, Hybrid +73.1% |
  | WorkBench | Decentralized +5.6%; Centralized and Hybrid −1.2% |
  | BrowseComp-Plus | Decentralized +9.2% (0.347 vs 0.318, the only clear browsing gain, attributed to parallel exploration of a high-entropy search space); Centralized +0.2%; Independent about −35% |
  | PlanCraft (sequential planning) | Independent −70.0%, Centralized −50.3%, Decentralized −41.5%, Hybrid −39.1% |
  | SWE-bench Verified | Independent −14.9%, Decentralized −5.4%, Centralized −3.1%, Hybrid −2.1% |
  | Terminal-Bench | Independent +1.7%, Centralized −19.2% |

  Overall mean multi-agent change is about −0.3% (95% CI about −58.7% to +77.2%). — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- **Cost**:

  | | Single | Independent | Decentralized | Centralized | Hybrid |
  | --- | --- | --- | --- | --- | --- |
  | Token overhead vs single | — | +58% | +263% | +285% | +515% |
  | Successes per 1K tokens | 67.7 | 42.4 | 23.9 | 21.5 | 13.6 |
  | Average turns | 7.2 | 11.4 | 26.1 | 27.7 | 44.3 |

  — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- **Capability ceiling**: once the single-agent baseline is above about 45% accuracy, adding agents tends to give negative returns. Tool-heavy tasks pay a multi-agent overhead. — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3); [abs](https://arxiv.org/abs/2512.08296)
- Message density (messages per turn): Decentralized 0.41, Centralized 0.39, Hybrid 0.24, Independent 0. Success rises roughly logarithmically with density and plateaus near 0.39; the authors call this descriptive, not a law. — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- A fitted model (cross-validated R² of 0.373, or 0.413 with a task-grounded capability metric) picks the best architecture for 87% of held-out configurations. — [arXiv abs](https://arxiv.org/abs/2512.08296)

**Multi-agent debate re-evaluations (2025)**
- Choi, Zhu, Li, "Debate or Vote", arXiv 2508.17536, NeurIPS 2025 Spotlight, Aug/Oct 2025. Across 7 NLP benchmarks, "Majority Voting alone accounts for most of the performance gains" usually credited to debate. Modelling debate as a stochastic process shows it induces a martingale over agents' beliefs, so debate alone does not improve expected correctness. Biasing updates toward correction helps. Models are not named on the abstract page. — [arXiv 2508.17536](https://arxiv.org/abs/2508.17536)
- Zhang et al., "Stop Overvaluing Multi-Agent Debate", arXiv 2502.08788, a position paper (Feb 2025, v3 June 2025). Five debate methods across 9 benchmarks with 4 foundation models often fail to beat chain-of-thought or self-consistency despite using more compute. Model heterogeneity (different models in the debate) consistently improved the methods. — [arXiv 2502.08788](https://arxiv.org/abs/2502.08788)
- "Talk Isn't Always Cheap", arXiv 2509.05396 (Sep 2025): debate can do worse than majority voting and can deteriorate as rounds go on, notably with heterogeneous agents. This comes from a search snippet; the full text was not read. — [arXiv 2509.05396](https://arxiv.org/html/2509.05396v2)
- Kaesberg et al., "Voting or Consensus?", arXiv 2502.19130 (Feb 2025): voting protocols gave +13.2% on reasoning tasks and consensus protocols +2.8% on knowledge tasks. This comes from a search snippet. — [alphaxiv 2502.19130](https://alphaxiv.org/overview/2502.19130v4)
- Conflict between debate papers: Zhang et al. say heterogeneity helps debate; "Talk Isn't Always Cheap" flags heterogeneous groups as a case where debate can hurt. — [2502.08788](https://arxiv.org/abs/2502.08788); [2509.05396](https://arxiv.org/html/2509.05396v2)

**Failure taxonomy**
- Cemri et al., "Why Do Multi-Agent LLM Systems Fail?" (MAST), arXiv 2503.13657 (Mar 2025, v3 Oct 2025). The study annotated 1,600+ traces across 7 frameworks and found 14 failure modes in three categories: system design/specification, inter-agent misalignment, and task verification. Multi-agent systems often show minimal gains on popular benchmarks. Models: GPT-4, Claude 3, Qwen2.5, CodeLlama (partly pre-frontier, so treat with care). — [arXiv 2503.13657](https://arxiv.org/abs/2503.13657)

### Inferences
- With frontier models, centralized verification is the variable that most limits error spread. Peer links without a verifier (decentralized) roughly double error amplification compared with an orchestrator. No coordination plus naive aggregation (independent) is by far the worst for error spread, even though it is the cheapest multi-agent form.
- Peer messaging earned its keep only where the search space is wide and exploration is parallel (BrowseComp-Plus). It did not help sequential planning or coding.
- A system whose single agent already scores high (above about 45%) should default to one agent. Fan-out should be a deliberate choice for decomposable or wide-search work.
- For "ask several and decide", vote over independent samples (ideally from heterogeneous models) before reaching for debate rounds.

### Gaps
- The exact models in "Debate or Vote" and "Stop Overvaluing MAD" were not on the abstract pages read. It is unconfirmed whether they are frontier (GPT-5/Claude 4-class) or smaller open models. Treat them as mixed-era until checked.
- No 2026 paper was found that repeats the Kim et al. topology comparison on newer models (GPT-5.x, Claude Opus 4.5+).
- The second half of the Kim et al. HTML (beyond 100k characters) was not read, so there may be more topology detail there.

## Q2. Production lessons: who tried flat peers, and what did they settle on?

### Takeaway
Every production system found converged on a hierarchy with a single owner: Cursor, Anthropic Research, Kimi Agent Swarm, the OpenAI Agents SDK guidance and Claude Code subagents. Peer messaging survives only as an add-on inside a hierarchy that keeps a fixed lead (Claude Code agent teams) and is explicitly positioned as the costlier, experimental option for research, review and competing hypotheses. Cursor's write-up is the clearest first-hand account: a flat swarm failed and planners plus workers plus a judge worked.

### Cited Findings
**Cursor, "Scaling long-running autonomous coding" (Wilson Lin, 2026-01-14)**
- Attempt 1, flat peers with locks: agents of equal status coordinated through a shared file, claimed tasks and took a lock. Twenty agents ran at the effective throughput of two or three, mostly waiting on locks. The scheme was brittle: agents crashed while holding locks, re-acquired locks they already held, and edited the file without locking. — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)
- Attempt 2, optimistic concurrency (a write fails if the state changed since the last read): simpler and more robust, but "with no hierarchy, agents avoided hard tasks and made small, safe changes". No agent owned difficult problems or end-to-end implementation, so work churned without progress. — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)
- Attempt 3, planners plus workers plus a judge:
  - Planners continuously explore the codebase, create tasks, and can spawn **sub-planners**, so planning is recursive.
  - Workers do one task and push without coordinating with other workers.
  - A judge decides at the end of each cycle whether to continue, and the next cycle starts fresh.
  
  This "resolved most coordination problems". — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)
- Scale:
  - A browser from scratch: about a week, more than 1M lines of code across about 1,000 files, hundreds of concurrent workers on one branch with minimal conflicts.
  - A Solid-to-React migration: +266K/−193K over three weeks.
  - Further projects: a Java LSP (550K LoC), a Windows 7 emulator (1.2M LoC) and Excel (1.6M LoC).
  - "Trillions of tokens" overall.
  
  — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)
- Lessons:
  - An integrator role for quality control and conflict resolution created more bottlenecks than it solved; workers handled conflicts themselves.
  - "Too little structure causes conflicts, duplicated work, and drift. Too much causes fragility."
  - Prompts mattered more than the harness.
  - Periodic fresh starts are still needed against drift and tunnel vision.
  - An open problem: planners should wake when their tasks finish.
  
  — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)
- Models: GPT-5.2 was better at extended autonomous work and a better planner than GPT-5.1-Codex. Opus 4.5 "tended to stop early and take shortcuts". Cursor now picks the best model per role. — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)

**Anthropic, "How we built our multi-agent research system" (2025-06-13)**
- Orchestrator-worker: a lead agent plans and spawns parallel search subagents. Subagents **do not coordinate with each other**; they report to the lead. An Opus 4 lead with Sonnet 4 subagents beat a single Opus 4 agent by 90.2% on an internal research eval. — [anthropic.com/engineering/multi-agent-research-system](https://www.anthropic.com/engineering/multi-agent-research-system)
- Cost and drivers: agents use about 4× the tokens of chat, multi-agent about 15×. Token usage alone explained 80% of performance variance on BrowseComp. — [same](https://www.anthropic.com/engineering/multi-agent-research-system)
- Failure modes:
  - spawning 50 subagents for simple queries;
  - endless searching for sources that don't exist;
  - excessive updates distracting other agents;
  - duplicated work (two subagents on the same supply-chain topic);
  - vague, short delegation causing identical searches.
  
  The fix was explicit objectives, output formats, tool guidance, boundaries and effort budgets (for example 3–10 tool calls for simple fact-finding). — [same](https://www.anthropic.com/engineering/multi-agent-research-system)
- Limits: execution is synchronous, so the lead waits, cannot steer mid-task, and stalls on a slow subagent; asynchronous execution is named as future work. Outputs go to a filesystem or artifacts and are passed back as references, avoiding a "game of telephone" through the lead. — [same](https://www.anthropic.com/engineering/multi-agent-research-system)

**Claude Code agent teams vs subagents (docs, current as of Nov 2026)**
- Agent teams are experimental and off by default (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`). Components:
  - a fixed team lead;
  - teammates, each a separate Claude Code instance;
  - a shared task list (pending, in progress or completed, with dependencies; blocked tasks are not self-claimed);
  - a mailbox: per-agent JSON inbox files with direct `SendMessage` by name.
  
  Task claiming uses file locking. — [code.claude.com/docs/en/agent-teams](https://code.claude.com/docs/en/agent-teams)
- Docs comparison: subagents return results to the caller, have lower token cost and suit "focused tasks where only the result matters". Teams self-coordinate via messages and the task list, cost more, and suit "complex work requiring discussion". The docs say teams work best when teammates are independent. For "sequential tasks, same-file edits, or work with many dependencies, a single session or subagents are more effective". — [same](https://code.claude.com/docs/en/agent-teams)
- Guidance:
  - Start with 3–5 teammates; "three focused teammates often outperform five scattered ones".
  - Plan 5–6 tasks per teammate.
  - Two teammates editing the same file leads to overwrites, so partition files.
  - Start with research or review rather than parallel implementation.
  - The best-documented use is "competing hypotheses", where teammates try to disprove each other to counter anchoring.
  
  — [same](https://code.claude.com/docs/en/agent-teams)
- Known limitations:
  - task status lags (teammates fail to mark tasks complete, which blocks dependents);
  - the lead starts implementing instead of waiting, or stops early;
  - **no nested teams** (teammates cannot spawn teammates);
  - one team per session;
  - the lead is fixed;
  - unattended teams waste effort.
  
  — [same](https://code.claude.com/docs/en/agent-teams)

**Kimi K2.5 Agent Swarm (Moonshot AI, Jan–Feb 2026)**
- A trained orchestrator directs up to 100 sub-agents over up to 1,500 steps without predefined roles. It was trained with Parallel-Agent RL (PARL): only the orchestrator is updated, and sub-agents are frozen and excluded from the objective. — [kimi.com blog](https://www.kimi.com/blog/kimi-k2-5); [arXiv 2602.02276](https://arxiv.org/html/2602.02276)
- "Serial collapse": the orchestrator falls back to single-agent execution even when parallel capacity exists. This was countered with an annealed parallelism reward. — [arXiv 2602.02276](https://arxiv.org/html/2602.02276)
- Self-reported results: up to 4.5× lower latency on wide-search, and item-level F1 rising from 72.8% to 79.0% over a single agent. A third-party write-up notes the speedup is on highly parallel tasks and shrinks with sequential dependencies. — [arXiv 2602.02276](https://arxiv.org/html/2602.02276); [spawn08 llmbase summary](https://spawn08.github.io/llmbase/07_recent_advances/12_kimi_k2_5)

**OpenAI Agents SDK**
- Two patterns:
  - Agents-as-tools (`Agent.as_tool()`, the "manager" pattern): the manager keeps the conversation, combines specialists and enforces guardrails in one place.
  - Handoffs: a triage agent routes and the specialist becomes the active agent for the rest of the turn.
  
  They can be combined. Code-driven orchestration (structured-output routing, chaining, an evaluator loop, `asyncio.gather` of independent agents) is "more deterministic and predictable" in speed, cost and performance. — [openai.github.io/openai-agents-python/multi_agent](https://openai.github.io/openai-agents-python/multi_agent/)

**A2A protocol**
- The Linux Foundation announced on 2026-04-09 that A2A had passed 150 supporting organizations, landed in major cloud platforms, and had enterprise production use in its first year. It is positioned as complementary to MCP (MCP is agent-to-tool, A2A is agent-to-agent). — [Linux Foundation press release](https://www.linuxfoundation.org/press/a2a-protocol-surpasses-150-organizations-lands-in-major-cloud-platforms-and-sees-enterprise-production-use-in-first-year)
- Secondary sources report v1.0 in March 2026 and a move into the Agentic AI Foundation in August 2026. They also caution that 150 supporters is not 150 production deployments. These claims are unverified against primary sources. — [rywalker.com analysis](https://rywalker.com/research/google-a2a); [Enterprise DNA](https://enterprisedna.co/resources/news/google-a2a-aaif-agent-protocol-open-standard-august-2026)

### Inferences
- Cursor's failures map onto the same mechanisms the controlled study measured:
  - lock contention is coordination overhead;
  - risk-aversion and churn are diffusion of ownership;
  - drift is decentralized error growth.
  
  The fix was not "more communication" but ownership: planners own decomposition, workers own one task, and a judge owns stop-or-continue.
- Production hierarchies keep workers isolated from each other (Cursor workers do not coordinate; Anthropic subagents do not talk). Peer channels, where they exist, are for research, review and debate rather than parallel writes to shared state.
- A2A is an interoperability protocol between separately owned agents, not evidence that peer topologies work better inside one system.

### Gaps
- No public account was found of a team that moved from hierarchy to a flat swarm and stayed. All the reversals found go flat → hierarchical.
- No quantitative public evaluation of Claude Code agent teams against subagents (accuracy or cost) was found; only docs guidance.
- Google ADK's own multi-agent guidance (sequential, parallel or loop workflow agents, LLM-driven transfer) was not fetched in this pass.

## Q3. Shared taskboards and blackboards vs direct messages

### Takeaway
A shared task list or blackboard works when one party owns writing the tasks and the board only arbitrates claiming. Cursor's flat board (self-claim plus locks or OCC, with no planner) failed on contention, then on risk-aversion. Claude Code keeps a lead-authored task list with dependency blocking and file-locked claims, but documents status lag and stale tasks. Two 2025 blackboard papers report gains over static multi-agent systems and master-slave dispatch. Both still use a control unit or a posted request, and their model generations are unclear.

### Cited Findings
- Cursor's flat shared coordination file: 20 agents gave the throughput of 2–3, with lock brittleness. With OCC and no hierarchy, agents "avoided hard tasks and made small, safe changes" and "no agent owned difficult problems". — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)
- Claude Code's task list: three states plus dependencies; teammates do not self-claim blocked tasks; claiming uses file locking. Known issue: "teammates sometimes fail to mark tasks as completed, which blocks dependent tasks". Too-small tasks mean coordination overhead exceeds the benefit, and too-large tasks mean wasted effort without check-ins. — [code.claude.com/docs/en/agent-teams](https://code.claude.com/docs/en/agent-teams)
- Han & Zhang, "Exploring Advanced LLM Multi-Agent Systems Based on Blackboard Architecture" (LbMAS), arXiv 2507.01701, July 2025:
  - Design: a control unit (an LLM picks which agent acts next), a shared blackboard, and role agents that communicate only via the blackboard.
  - Results: +4.33% on average over chain-of-thought methods and +5.02% over static multi-agent systems, with fewer tokens.
  - Tasks are commonsense, reasoning and math; the models were not identified in what was read.
  
  — [arXiv 2507.01701](https://arxiv.org/html/2507.01701v1)
- Salemi et al. (Google), "LLM-based Multi-Agent Blackboard System for Information Discovery in Data Science", arXiv 2510.01285 (Sep 2025, rev. Jan 2026):
  - Design: the master-slave pattern is replaced with a shared board where requests are posted and agents decide whether to respond, so the coordinator does not need to know each agent's expertise.
  - Results: 13–57% relative improvement in end-to-end success and up to 9% relative gain in data-discovery F1 over the best baseline.
  
  — [arXiv 2510.01285](https://arxiv.org/abs/2510.01285v2)
- Anthropic: "excessive updates that distracted other agents" is a failure mode, and duplicated work came from vague delegation, not from the lack of a peer channel. — [anthropic.com/engineering/multi-agent-research-system](https://www.anthropic.com/engineering/multi-agent-research-system)
- Kim et al.: success plateaus near 0.39 messages per turn, so more messaging past that point does not help. — [arXiv 2512.08296v3](https://arxiv.org/html/2512.08296v3)

### Inferences
- A taskboard is better than free-form direct messages for preventing duplicated work: claims are atomic and visible, and dependencies block early starts. But it does not supply ownership of hard work, which needs a planner or lead that writes and sizes the tasks.
- Board status drift (unmarked completions, stale claims) is the recurring operational cost. Systems need a reconciler, either the lead or code that reads results rather than trusting self-reported status.
- Direct peer messages add value mainly for adversarial checking (competing hypotheses) and add noise otherwise.

### Gaps
- No controlled 2025–2026 study isolating "shared task list with claiming" against "direct messaging" on frontier models was found.
- The LbMAS and Google blackboard papers' models and baselines were not verified in full text, and their baselines may be weak static multi-agent systems.

## Q4. Is depth > 1 (sub-sub-agents) ever shown to help?

### Takeaway
There is no controlled evidence that depth beyond one helps. The single production example is Cursor's recursive sub-planners: only the planning layer is deep, and workers remain leaves. Claude Code explicitly forbids nested teams, and Anthropic's research system and Kimi's swarm are depth 1. Hybrid (more layers or links) was the most expensive and least token-efficient topology in the controlled study.

### Cited Findings
- Cursor: "Planners ... can spawn sub-planners for specific areas, making planning parallel and recursive." Workers are leaves that do not coordinate. This is reported as part of what worked, with no ablation of depth. — [cursor.com/blog/scaling-agents](https://cursor.com/blog/scaling-agents)
- Claude Code: "No nested teams: teammates cannot spawn their own teammates. Only the lead can manage the team." — [code.claude.com/docs/en/agent-teams](https://code.claude.com/docs/en/agent-teams)
- Anthropic Research is lead → subagents, one level, with synchronous waits. — [anthropic.com](https://www.anthropic.com/engineering/multi-agent-research-system)
- Kimi Agent Swarm is one trained orchestrator over frozen sub-agents. — [arXiv 2602.02276](https://arxiv.org/html/2602.02276)
- Hybrid (orchestrator plus peers) had +515% token overhead, 44.3 average turns and the lowest successes per 1K tokens (13.6). — [arXiv 2512.08296v3](https://arxiv.org/html/2512.08296v3)
- Historical, not frontier-era: Huang et al., "On the Resilience of LLM-Based Multi-Agent Collaboration with Faulty Agents", arXiv 2408.00989 (Aug 2024, ICML 2025).
  - A hierarchical A→(B↔C) structure degraded least under injected faulty agents (5.5% vs 10.5% and 23.7% for the other structures; the abstract version says 9.2%).
  - A star beat a complete graph.
  - A corrupted planner did the most damage (accuracy 28 → 12).
  - Challenger and Inspector mitigations recovered up to 96% of the lost performance.
  - Models were largely GPT-3.5 era, so treat as historical.
  
  — [arXiv 2408.00989](https://arxiv.org/pdf/2408.00989); [ICML 2025 poster](https://icml.cc/virtual/2025/poster/44721)

### Inferences
- If depth is added, it belongs in decomposition (planner → sub-planner), not in execution. Leaves should be single-task workers that report up.
- The planner is the single point of failure. Corrupting it hurt most in Huang et al., which argues for a judge or verifier over planners rather than more layers.

### Gaps
- No ablation of depth 1 against depth 2+ on frontier models was found, in research or production.

## Q5. Security: does peer messaging widen prompt-injection spread relative to hierarchy?

### Takeaway
The 2025–2026 security literature treats multi-agent prompt injection as propagation over the communication graph. Attacks optimize routing through the topology, and defenses are topology-aware (graph-based detection of "infected" agents). No frontier-era controlled study was found that directly measures infection spread under hierarchy against a peer mesh. The mechanism (every peer edge is an untrusted input path), Kim et al.'s error-amplification data, and production practice (workers isolated; Claude Code marking inter-agent messages as untrusted) all point the same way: hierarchy with verification narrows propagation.

### Cited Findings
- "Agents Under Siege", arXiv 2504.00218 (Apr 2025): models prompt propagation as a max-flow / min-cost problem over the multi-agent topology to reach a target agent while evading detection. Llama-Guard and PromptGuard variants failed to stop it. This comes from search snippets. — [arXiv 2504.00218](https://arxiv.org/html/2504.00218v2)
- G-Safeguard, arXiv 2502.11127 (Feb 2025): GNN-based anomaly detection on the utterance graph plus topology intervention. Reported 19.50–39.23% reduction in attack success under prompt injection in large-scale systems. This comes from a search snippet. — [arXiv 2502.11127](https://arxiv.org/html/2502.11127v1)
- INFA-Guard, arXiv 2601.14667 (Jan 2026): attack agents persuade benign agents and convert them into "infected agents that output erroneous conclusions". Binary benign/attacker labelling misses this. This comes from a snippet. — [arXiv 2601.14667](https://web3.arxiv.org/pdf/2601.14667)
- "Conjunctive Prompt Attacks in Multi-Agent LLM Systems", arXiv 2604.16543 (Apr 2026): attacks split across agents depend on segmentation, routing and hidden templates. Prior multi-hop attacks carried a single instruction across agents. This comes from a snippet. — [arXiv 2604.16543](https://arxiv.org/html/2604.16543v1)
- "Thought Virus", arXiv 2603.00131 (Mar 2026): whether and how propagation happens depends on both the attack and the chosen topology. The search summary says it cites work suggesting denser connections propagate errors less; this was not verified in the full text. — [arXiv 2603.00131](https://arxiv.org/html/2603.00131)
- An aggregator reported Cohen's d = 2.34 for hierarchical versus centralized topologies in three-tier command chains for indirect injection. The original paper was not located, so this is **unverified**. — [synapsesocial aggregator](https://synapsesocial.com/papers/69df2c2fe4eeef8a2a6b129f)
- Claude Code production control: a message from another agent is marked as coming from another Claude session, not the user. A teammate cannot approve permissions or relay a denied action to another teammate. In auto mode a classifier treats relayed approval claims as untrusted and reviews inter-agent messages before delivery. — [code.claude.com/docs/en/agent-teams](https://code.claude.com/docs/en/agent-teams)
- Historical baseline: "Prompt Infection" (Lee & Tiwari, 2024) showed self-replicating prompts spreading between agents. It is cited by 2504.00218 and is pre-frontier. — [arXiv 2504.00218](https://arxiv.org/html/2504.00218v2)

### Inferences
- Each peer edge is another indirect-injection channel. In a hub topology, worker outputs pass through one place (the lead) where filtering and approval can sit, so the blast radius is bounded by the lead's guardrails. In a mesh, a compromised worker can address every peer directly.
- Kim et al.'s error-amplification ordering (independent > decentralized > hybrid > centralized) measures benign errors, but the containment mechanism (a central verifier) is the same one that bounds injected instructions.
- The minimum safeguard, if peer messaging exists at all: tag inter-agent messages as agent-originated and untrusted, never let them carry approvals or consent, and keep permissions per agent, not per message.

### Gaps
- No frontier-model (2025–2026) controlled measurement of injection spread by topology was found. Most security papers report results from abstracts or snippets with unclear model generations.
- The "Thought Virus" claim that denser connections propagate errors less conflicts with the intuitive "more edges, more spread" view and with Kim et al.'s decentralized > centralized amplification. This needs a full-text check.
