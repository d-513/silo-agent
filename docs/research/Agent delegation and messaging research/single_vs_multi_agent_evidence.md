# Single agent vs multi-agent / subagent delegation: empirical evidence 2025–2026 (frontier models)

Research date: 2026-11-17. Scope: controlled studies and practitioner reports from 2025–2026 using frontier-era models (Claude 4.x, GPT-5.x, Gemini 2.5, o3, etc.). Findings that rely on GPT-4/GPT-4o/Claude 3.x-era models are flagged as such.

## What do controlled studies find?

### Takeaway
The best controlled evidence (Google/MIT et al., 180 configurations over GPT-5, Gemini 2.5, Claude Sonnet 4/4.5; journal version 260 configurations) says multi-agent helps only on decomposable, parallelizable tasks and when the single agent is still weak; once a single agent scores above roughly 45%, extra agents give diminishing or negative returns, and on sequential tasks every multi-agent variant got 39–70% worse. Studies that hold thinking tokens equal (Stanford, April 2026) find a single agent matches or beats multi-agent on multi-hop reasoning, which suggests many reported MAS gains come from extra compute rather than the architecture.

### Cited Findings

**Kim et al., "Towards a Science of Scaling Agent Systems", arXiv:2512.08296 (v1 2025-12-09, v2 2025-12-17; Google Research / MIT et al., 18 authors)**
- Design: one Single-Agent System (SAS) and four Multi-Agent Systems (MAS): Independent, Centralized, Decentralized and Hybrid. Tested across 3 LLM families and 4 benchmarks (Finance-Agent, BrowseComp-Plus, PlanCraft, Workbench), in 180 configurations and 15,750 instance runs — [arXiv 2512.08296](https://arxiv.org/abs/2512.08296v1); [HTML v2](https://arxiv.org/html/2512.08296v2)
- Models: GPT-5-nano, GPT-5-mini and GPT-5; Gemini 2.0 Flash, 2.5 Flash and 2.5 Pro; Claude Sonnet 3.7, 4.0 and 4.5. These are mostly frontier-era; Sonnet 3.7 and Gemini 2.0 Flash are early-2025 — [HTML v2](https://arxiv.org/html/2512.08296v2)
- Compute control: MAS and SAS were matched on total reasoning tokens (a mean of 4,800 tokens per trial) and on tool-call access. MAS agents got smaller iteration budgets each, and the SAS got more reasoning rounds to compensate — [HTML v2](https://arxiv.org/html/2512.08296v2)
- Change versus SAS, by task:
  - Finance-Agent, a decomposable parallel analysis task: Centralized +80.8% (+80.9% in the body), Decentralized +74.5%, Hybrid +73.1%.
  - Workbench: Decentralized +5.7%, Centralized and Hybrid −1.2%.
  - BrowseComp-Plus: Decentralized +9.2%, Centralized +0.2%, Independent −35%.
  - PlanCraft, sequential planning: Centralized −50.3%, Decentralized −41.5%, Hybrid −39.1%, Independent −70.1%. The abstract states this as "degraded performance by 39–70%" on sequential reasoning.
  - Source: [HTML v2](https://arxiv.org/html/2512.08296v2)
- **Capability saturation:** coordination has diminishing or negative returns once the single-agent baseline exceeds about 45% (β̂ = −0.404, p < 0.001). In standardized units the boundary is P_SA* ≈ 0.129 — [HTML v2](https://arxiv.org/html/2512.08296v2)
- **Error amplification (Ae, relative to SAS = 1.0):** Independent 17.2, Decentralized 7.8, Centralized 4.4, Hybrid 5.1. Without an orchestrator that checks outputs, errors compound. In the regression, neither the Ae main effect nor its interaction with tool count was significant (p = 0.441 and 0.506) — [HTML v2](https://arxiv.org/html/2512.08296v2)
- **Tool-coordination trade-off:** tool-heavy tasks are penalized under MAS. The efficiency × tools interaction is β̂ = −0.267 and the overhead × tools interaction is β̂ = −0.162 (both p < 0.001) — [HTML v2](https://arxiv.org/html/2512.08296v2)
- **Cost and efficiency (Table 5):**

  | Architecture | Overhead vs SAS | Mean turns | Successes per 1K tokens | Coordination efficiency |
  | --- | --- | --- | --- | --- |
  | SAS | — | 7.2 | 67.7 | 0.466 |
  | Independent | +58% | 11.4 | 42.4 | 0.234 |
  | Decentralized | +263% | 26.1 | 23.9 | 0.132 |
  | Centralized | +285% | 27.7 | 21.5 | 0.120 |
  | Hybrid | +515% | 44.3 | 13.6 | 0.074 |

  Source: [HTML v2](https://arxiv.org/html/2512.08296v2)
- **Predictive model:** cross-validated R² = 0.524 (±0.033), MAE 0.089. On held-out configurations it picks the best architecture 87% of the time, against 20% for random choice and 54% for a model that uses capability alone — [HTML v2](https://arxiv.org/html/2512.08296v2)
- **Out-of-sample check (v2):** on GPT-5.2, released after the study, the MAE was 0.071 and 4 of the 5 scaling principles held — [arXiv search summary / HTML v2](https://arxiv.org/html/2512.08296v2)

**Kim et al., "Capable language models can outgrow the benefits of collaboration", Nature Machine Intelligence 8, 1157–1172 (2026), DOI 10.1038/s42256-026-01268-y (listed 2026-07-24)**
- Design: prompts, tools and compute were held fixed, and only the coordination structure and model capability were varied. It covers 260 configurations, 6 benchmarks, 5 architectures and 3 LLM families — [MIT Media Lab page](https://www.media.mit.edu/publications/capable-language-models-can-outgrow-the-benefits-of-collaboration/)
- The single-agent baseline's performance is the most robust predictor of whether coordination helps or hurts. Above a capability-saturation threshold, extra agents are unlikely to help. "Baseline-scaled error amplification" is a second effect — [MIT Media Lab page](https://www.media.mit.edu/publications/capable-language-models-can-outgrow-the-benefits-of-collaboration/)
- Fit: R² is 0.373 (0.413 with a task-grounded capability metric), and the model picks the best architecture in 87% of held-out configurations. The threshold predicted the effect of coordination in 94% of validation configurations on **SWE-bench Verified and Terminal-Bench**, which makes this the coding/terminal extension — [MIT Media Lab page](https://www.media.mit.edu/publications/capable-language-models-can-outgrow-the-benefits-of-collaboration/)

**Tran & Kiela (Stanford), "Single-Agent LLMs Outperform Multi-Agent Systems on Multi-Hop Reasoning Under Equal Thinking Token Budgets", arXiv:2604.02460 (v1 2026-04-02, v2 2026-04-11)**
- Argument: by the Data Processing Inequality, a single agent with perfect use of its context is more information-efficient under a fixed reasoning-token budget, because every handoff can only lose information — [arXiv 2604.02460](https://arxiv.org/abs/2604.02460)
- Result: under equal thinking-token budgets, the SAS matches or beats MAS on multi-hop reasoning. MAS wins only when the single agent's effective context use is degraded (severe context corruption) or when more total compute is spent — [arXiv 2604.02460](https://arxiv.org/abs/2604.02460); [HTML v1](https://arxiv.org/html/2604.02460v1)
- Models: Qwen3, DeepSeek-R1-Distill-Llama and Gemini 2.5. These are open models plus one frontier family, with no Claude or GPT-5 — [arXiv 2604.02460](https://arxiv.org/abs/2604.02460)
- Methodological warning: API-based budget control showed "significant artifacts", especially for Gemini 2.5, and these can *inflate apparent MAS gains* — [arXiv 2604.02460](https://arxiv.org/abs/2604.02460)
- Criticism: the maximum thinking budget of about 10K tokens is small compared with the long-horizon tasks where MAS might help. This is from commentary, not peer review — [VentureBeat](https://venturebeat.com/orchestration/are-you-paying-an-ai-swarm-tax-why-single-agents-often-beat-complex-systems); [Towards AI](https://pub.towardsai.net/everyone-building-multi-agent-systems-is-spending-compute-on-something-mathematically-impossible-7f292b4acd1a)

**Gao et al., "Single-agent or Multi-agent Systems? Why Not Both?", arXiv:2505.18286 (May 2025)**
- The benefits of MAS over SAS shrink as LLM capability improves. The authors name OpenAI o3 and Gemini 2.5 Pro as frontier models whose long-context reasoning, memory retention and tool use remove much of the original motivation for MAS — [arXiv 2505.18286](https://arxiv.org/abs/2505.18286v1); [HTML](https://arxiv.org/html/2505.18286v1)
- They propose a hybrid router/cascade between SAS and MAS. A search summary reports +1.1% to 12% accuracy and up to 20% lower deployment cost; I did not verify this in the paper — [Emergent Mind](https://www.emergentmind.com/papers/2505.18286)

**Li, "When Single-Agent with Skills Replace Multi-Agent Systems and When They Fail", arXiv:2601.04748 (Jan 2026)**
- Framing: a MAS can be "compiled" into one agent that selects from a skill library, which swaps inter-agent communication for skill selection — [arXiv 2601.04748](https://arxiv.org/abs/2601.04748v1); [HTML v2](https://arxiv.org/html/2601.04748v2)
- On GSM8K, HumanEval and HotpotQA, the single agent with skills roughly matched MAS accuracy with about 54% fewer tokens and about 50% lower latency on average. These are older, saturated benchmarks — [HTML v2](https://arxiv.org/html/2601.04748v2)
- Failure mode: across libraries of 5 to 200 skills, selection accuracy falls sharply past a capacity threshold. Semantic confusability between skills matters more than the raw count. Hierarchical routing (skills grouped into categories) restored reliable selection — [HTML v2](https://arxiv.org/html/2601.04748v2)
- Follow-up: Xu et al., "From Multi-Agent to Single-Agent: When Is Skill Distillation Beneficial?", arXiv:2604.01608 (April 2026), asks when distillation helps — [arXiv 2604.01608](https://arxiv.org/html/2604.01608v1)

**Cemri et al. (UC Berkeley), "Why Do Multi-Agent LLM Systems Fail?" (MAST), arXiv:2503.13657 (v3 2025-10-26; NeurIPS 2025 Datasets & Benchmarks)**
- **Flag: older-model evidence.** The traces come from GPT-4o, GPT-4, GPT-4o-mini, Claude-3.7-Sonnet, Qwen2.5-Coder-32B and CodeLlama-7b, run in 7 frameworks: MetaGPT, ChatDev, HyperAgent, AppWorld, AG2/MathChat, Magentic-One and OpenManus. o1 was the LLM judge. No GPT-5 or Claude 4 — [HTML v3](https://arxiv.org/html/2503.13657v3)
- Failure rates of 41% to 86.7% across the evaluated MAS, on benchmarks that cannot be compared directly — [HTML v3](https://arxiv.org/html/2503.13657v3)
- Taxonomy (14 modes):
  - System design, about 44% when summed: disobey task specification 11.8%, step repetition 15.7%, unaware of termination conditions 12.4%, loss of conversation history 2.8%, disobey role specification 1.5%.
  - Inter-agent misalignment, about 32%: reasoning–action mismatch 13.2%, task derailment 7.4%, failure to ask for clarification 6.8%, conversation reset 2.2%, ignored another agent's input 1.9%, information withholding 0.85%.
  - Task verification, about 24%: incorrect verification 9.1%, no or incomplete verification 8.2%, premature termination 6.2%.
  - Source: [HTML v3](https://arxiv.org/html/2503.13657v3)
- Prompt and role fixes gave only small gains: ChatDev +9.4% from clearer roles, and +15.6% at most from adding high-level objective verification. The authors argue that "MAS failure is not merely a function of challenges in the underlying model" and that better base models "will be insufficient to address the full MAST" — [HTML v3](https://arxiv.org/html/2503.13657v3)

### Inferences
- Across the 2025–2026 controlled work, the gain from "more agents" mostly comes from (a) more total compute and (b) splitting a task that truly decomposes into parallel parts. Once tokens are matched and the task is sequential or tool-heavy, a single strong agent wins.
- The headline +80.9% (Finance-Agent) and −70% (PlanCraft) in the scaling paper come from the same experiment. Task structure, not "multi-agent" as such, decides the sign.
- Centralized orchestration, where a lead checks the subagents' outputs, is consistently the least harmful MAS: about 4× error amplification against 17× for independent agents. This supports an orchestrator-with-subagents design over peer swarms.
- The Nature MI paper is very likely the journal version of 2512.08296: same first author and team, the same 87% selection accuracy, extended to 260 configurations and 6 benchmarks including SWE-bench Verified and Terminal-Bench. The MIT page does not say so explicitly.

### Gaps
- I could not get exact accuracy tables or benchmark names for Tran & Kiela beyond the abstract.
- I could not confirm which models Gao et al. (2505.18286) and Li (2601.04748) actually ran; the benchmarks in Li are old and saturated.
- I found no controlled study that runs Claude Opus 4.5/4.6+ or GPT-5.x as lead and subagents on long-horizon real coding against an equal-token single agent, apart from the Nature MI SWE-bench/Terminal-Bench validation, whose details I did not see.
- I found no 2026 re-run of MAST on frontier models.

## Practitioner evidence (Anthropic, Cognition, LangChain, Cursor, Codex)

### Takeaway
Labs agree that subagents pay off for breadth-first, parallelizable read-only work (research, exploration, review) and as context isolation, at a large token cost (about 15× chat). They also agree that parallel *writers* on tightly coupled work (most coding) still fail. Cognition's 2026 follow-up converged on "many agents may think, one agent writes."

### Cited Findings

**Anthropic, "How we built our multi-agent research system" (2025-06-13)**
- A Claude Opus 4 lead with Claude Sonnet 4 subagents "outperformed single-agent Claude Opus 4 by 90.2% on our internal research eval" — [Anthropic Engineering](https://www.anthropic.com/engineering/multi-agent-research-system)
- Token cost: agents use about 4× the tokens of chat, and multi-agent systems about 15× — [Anthropic Engineering](https://www.anthropic.com/engineering/multi-agent-research-system)
- Three factors explained 95% of the performance variance on BrowseComp, and "token usage by itself explains 80% of the variance". The other two factors were the number of tool calls and the model choice — [Anthropic Engineering](https://www.anthropic.com/engineering/multi-agent-research-system)
- "upgrading to Claude Sonnet 4 is a larger performance gain than doubling the token budget on Claude Sonnet 3.7". A better model beats more tokens — [Anthropic Engineering](https://www.anthropic.com/engineering/multi-agent-research-system)
- Latency: the lead spins up 3–5 subagents in parallel, each uses 3+ tools in parallel, and this "cut research time by up to 90% for complex queries" — [Anthropic Engineering](https://www.anthropic.com/engineering/multi-agent-research-system)
- Not a good fit: domains where agents "share the same context or involve many dependencies between agents"; "most coding tasks involve fewer truly parallelizable tasks than research" — [Anthropic Engineering](https://www.anthropic.com/engineering/multi-agent-research-system)
- Early failure modes: spawning "50 subagents for simple queries", and subagents duplicating each other's work (two investigating 2025 supply chains while a third went off to the 2021 chip crisis) — [Anthropic Engineering](https://www.anthropic.com/engineering/multi-agent-research-system)

**Anthropic, "Effective context engineering for AI agents" (2025-09-29)**
- A subagent may spend "tens of thousands of tokens or more" exploring but returns a "condensed, distilled summary", often 1,000–2,000 tokens. This gives a "clear separation of concerns" — [Anthropic Engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
- When to use which: subagents for complex research and analysis where parallel exploration pays; compaction for long back-and-forth; structured note-taking for iterative development with checkpoints — [Anthropic Engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)

**Cognition (Walden Yan)**
- The original "Don't Build Multi-Agents" (2025-06-12) argues for sharing full context and traces, because actions carry implicit decisions and conflicting decisions produce bad results. Its date is given as June 12, 2025 by most sources; one blog says March 2025 — [echai summary](https://echai.ventures/how-founders-use-ai/r/don-t-build-multi-agents--941) (the primary post is on cognition.ai/blog, which I did not re-fetch).
- Follow-up, "Multi-Agents: What's Actually Working" (2026-04-22): parallel-writer swarms still do not work. What works is setups where "multiple agents contribute intelligence to a task while writes stay single-threaded" — [Cognition blog](https://cognition.com/blog/multi-agents-working)
  - **Review loops:** a coder agent and a reviewer agent iterate against each other. Devin Review catches an average of 2 bugs per PR, about 58% of them severe. It works "best when the coding and review agents do not share any context beforehand", because a clean reviewer can question the spec.
  - **Smart friend:** a cheaper primary escalates hard decisions to a stronger model. "smart-friend works today when both models are strong". Routing across frontier families (Claude ↔ GPT) helped on tricky tasks. It failed when the primary was too weak: SWE-1.5 was too far below Sonnet 4.5.
  - **Manager/child delegation** ("map-reduce-and-manage") is live in Devin.
  - Unstructured swarms negotiating with each other are "mostly a distraction". Parallel writers fail because implicit choices about style and edge cases conflict.
  - Open problems are communication problems: when a weaker model should escalate, how a child surfaces a discovery that affects its siblings, and how to transfer context without flooding the receiver.
  - Showcase builds (a 200k LOC browser, a 100k LOC C compiler) had simple, verifiable success criteria, unlike most real software.

**LangChain**
- "How and when to build multi-agent systems" (June 2025) treats context engineering as the central problem and presents Cognition's and Anthropic's posts as complementary — [LangChain blog](https://blog.langchain.com/how-and-when-to-build-multi-agent-systems)
- "Choosing the Right Multi-Agent Architecture" (Sydney Runkle, 2026-01-14) recommends starting with a single agent and good tool design ("add tools before adding agents"). It moves to multi-agent patterns only for context management across large domains or distributed team development — [LangChain blog](https://www.langchain.com/blog/choosing-the-right-multi-agent-architecture)
- Measured examples from the same post:
  - One-shot request: subagents 4 model calls against 3 for skills, handoffs and router.
  - Repeat request: subagents 8 calls in total against 5 for skills and handoffs, since a stateful pattern saves 40%.
  - Multi-domain query (3 languages × about 2K tokens of docs each): subagents 5 calls and about 9K tokens, against skills 3 calls and about 15K tokens. Isolation keeps irrelevant context out, so subagents processed 67% fewer tokens.
  - Source: [LangChain blog](https://www.langchain.com/blog/choosing-the-right-multi-agent-architecture)
- The Deep Agents docs and blog (January 2026) name context isolation ("each subagent invocation works in a clean context window, preventing context bloat") as the main reason for subagents, with multi-model use and parallelism second. "for simpler cases with just a few tools, use a single agent" — [LangChain docs](https://docs.langchain.com/oss/python/langchain/multi-agent/subagents); [Deep Agents blog](https://www.langchain.com/blog/building-multi-agent-applications-with-deep-agents)

**Coding tools (mostly product announcements, little measured evidence)**
- Cursor docs: subagents "break down complex tasks, do work in parallel, and preserve context in the main conversation". Cursor reads subagent definitions from `.claude/` and `.codex/` as well as its own — [Cursor docs](https://cursor.com/docs/agent/subagents)
- Cursor shipped subagents around 2026-02-24, and Cursor 3.x added /multitask with parallel agents on separate git worktrees. Forum reports describe limits on parallel dispatch (batches wait for the whole previous batch to finish) — [Cursor forum](https://forum.cursor.com/t/parallel-subagents-next-batch-waits-for-entire-previous-batch-to-finish/152681); [roundup](https://rinti.github.io/ai-roundup/issues/2026-04-25-cursor-3-multitask-clawsweeper-and-harness-debate.html)
- OpenAI Codex subagents launched in March 2026: each subagent has its own instructions, model and tools, and the parent blocks until all return. Secondary sources only — [Product Hunt](https://www.producthunt.com/p/openai/codex-subagents); [roundup](https://securityboulevard.com/2026/06/6-coding-agents-that-actually-run-sub-agents-in-parallel/)

### Inferences
- The lab write-ups match the controlled studies. Anthropic's 90.2% gain is on breadth-first research (decomposable and read-only), and token spend explains most of the variance there, consistent with Tran & Kiela's "it's the compute". Cognition's single-writer rule matches the scaling paper's sequential and tool-heavy penalty.
- Today's subagent tooling (Claude Code, Cursor, Codex, Deep Agents) is mostly sold as **context isolation**: a subagent spends tokens in a throwaway context and returns a 1–2K-token summary. That benefit is real even when accuracy does not improve, because it protects the lead's context and prompt cache.

### Gaps
- None of Cursor, Codex, Amp or Factory has published controlled accuracy deltas for subagents against a single agent. Their evidence is product claims, forum anecdotes and secondary roundups.
- Anthropic's 90.2% figure is on an internal, unpublished eval with no token-matched single-agent control.
- I did not re-fetch Cognition's 2025 primary post, so its details come from secondary summaries.

## Mechanisms, harms and quantitative profile by task type

### Takeaway
The benefit of delegation is mainly (1) more total tokens and compute, (2) context isolation (a clean window for each subtask), (3) wall-clock parallelism, and (4) a fresh, unbiased view for review. The harms are coordination overhead (+58% to +515% tokens in matched studies), error amplification (up to 17×), lost information at handoffs, and conflicting implicit decisions when several agents write shared state. Net value is positive for breadth-first research and decomposable analysis, and negative for sequential planning and tightly coupled coding.

### Cited Findings
- **The compute explanation:** token usage explains 80% of BrowseComp variance — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system). Under equal thinking tokens, MAS gains vanish except under severe context degradation, and API budget artifacts inflate MAS gains — [arXiv 2604.02460](https://arxiv.org/abs/2604.02460)
- **Context isolation:** subagents return 1–2K-token summaries of tens of thousands of tokens of exploration — [Anthropic](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents). Isolation cut tokens processed by 67% compared with loading every skill into one context in LangChain's multi-domain test — [LangChain](https://www.langchain.com/blog/choosing-the-right-multi-agent-architecture). MAS beats SAS when the single agent's context is corrupted or degraded — [arXiv 2604.02460](https://arxiv.org/abs/2604.02460)
- **Parallelism and latency:** up to 90% less research time with 3–5 parallel subagents — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system). In token-matched settings MAS takes far more turns (Hybrid 44.3 against SAS 7.2) — [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2)
- **Diversity and independent review:** reviewers without shared context catch about 2 bugs per PR, 58% of them severe — [Cognition 2026](https://cognition.com/blog/multi-agents-working). The scaling paper frames the trade-off as context integration against diversity — [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2)
- **Harm from sequential, tightly coupled tasks:** PlanCraft −39% to −70% for every MAS variant — [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2). "most coding tasks involve fewer truly parallelizable tasks than research" — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system). Parallel writers conflict on implicit style and edge-case choices — [Cognition 2026](https://cognition.com/blog/multi-agents-working)
- **Harm from tool-heavy tasks:** a significant negative interaction between tools and coordination efficiency or overhead — [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2)
- **Error amplification:** 17.2× for independent agents and 4.4× for centralized — [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2)
- **Information loss at handoffs ("telephone game"):** formalized as the Data Processing Inequality — [arXiv 2604.02460](https://arxiv.org/abs/2604.02460). In MAST (older models), information withholding, ignoring other agents' input, conversation reset and loss of history together make up about 8% of failures, and inter-agent misalignment about 32% — [arXiv 2503.13657](https://arxiv.org/html/2503.13657v3)
- **Coordination pathologies:** over-spawning (50 subagents for a simple query) and duplicated work — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system). Step repetition (15.7%) and not knowing when to stop (12.4%) are the largest MAST modes — [arXiv 2503.13657](https://arxiv.org/html/2503.13657v3)
- **Capability saturation:** gains turn to losses above a single-agent baseline of about 45% — [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2); this replicates on SWE-bench Verified and Terminal-Bench with 94% predictive accuracy — [Nature MI 2026](https://www.media.mit.edu/publications/capable-language-models-can-outgrow-the-benefits-of-collaboration/). A model upgrade beats doubling tokens — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- **Cost multipliers:** about 15× chat tokens for Anthropic's research MAS (about 4× for a single agent) — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system). Successes per 1K tokens: SAS 67.7 against MAS 13.6–42.4, so a single agent is about 1.6× to 5× more token-efficient — [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2). A single agent with skills used about 54% fewer tokens and about 50% less latency than the equivalent MAS — [arXiv 2601.04748](https://arxiv.org/html/2601.04748v2)

**Summary by task type**

| Task type | Evidence | Delta |
| --- | --- | --- |
| Breadth-first research / browsing | Anthropic internal eval | +90.2% at about 15× tokens |
| Breadth-first research / browsing | BrowseComp-Plus | Decentralized +9.2%, Independent −35% |
| Decomposable financial analysis | Finance-Agent | +73% to +81% |
| Tool-heavy workflows | Workbench | −1.2% to +5.7% |
| Sequential planning | PlanCraft | −39% to −70% |
| Multi-hop reasoning, tokens matched | Tran & Kiela | SAS ≥ MAS |
| Coding | Nature MI, SWE-bench / Terminal-Bench | Saturation rule holds (94%) |
| Coding | Practitioners | Single writer + reviewer / smart friend helps; parallel writers hurt |

Sources: [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system), [arXiv 2512.08296](https://arxiv.org/html/2512.08296v2), [arXiv 2604.02460](https://arxiv.org/abs/2604.02460), [Nature MI](https://www.media.mit.edu/publications/capable-language-models-can-outgrow-the-benefits-of-collaboration/), [Cognition](https://cognition.com/blog/multi-agents-working)

### Inferences
- For a product like Silo, where one lead Bot talks to a human, the evidence supports this design:
  - a single strong lead by default;
  - subagents for read-only, parallel, context-heavy exploration that return short summaries;
  - a centralized orchestrator that verifies results, not peer agents messaging each other;
  - never several agents writing the same files or state at once;
  - an independent-context reviewer as the most reliably useful second agent.
- Since a strong model's single-agent baseline on many everyday tasks is already above 45%, default delegation for accuracy is likely net-negative. Delegation should be justified by context isolation, wall-clock time or independent verification, not by "more agents = smarter".

### Gaps
- I found no clean 2025–2026 latency measurement for MAS against SAS with matched tokens beyond Anthropic's "up to 90%" and the turn counts in the scaling paper.
- I found no frontier-model study of agent-to-agent messaging protocols (structured messages against free text, or how much context to pass) with measured outcomes. Cognition lists these as open problems.
- Telephone-game loss is argued theoretically (DPI) and seen in older-model MAST traces, but I found no direct measurement on frontier models.
