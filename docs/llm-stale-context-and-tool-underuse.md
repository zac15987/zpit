# LLM Failure Modes: Stale Context Assumption & Tool Underuse

Two related failure modes that directly impact AI coding agent reliability and developer efficiency.

---

## 1. Stale Context Assumption

### Definition

The LLM treats information observed earlier in the conversation as still current, without re-verifying. It operates on a **snapshot** of state rather than the **live** state.

### Manifestations in Coding Agents

| Pattern | Example |
|---|---|
| Git state assumption | Agent assumes it is on a feature branch without running `git status`; actually on main |
| File staleness | Agent edits a file based on content it read 20 tool calls ago; file has since changed |
| Conversation anchoring | Agent references "the latest commit" meaning the one from the start of conversation, not the actual latest |
| Conflicting edits | Agent modifies `auth.go`, forgets it did, makes conflicting edits later |
| Build state assumption | Agent assumes tests pass based on a previous run; code has changed since |

### Root Causes

**Positional attention bias (architectural).** Transformers exhibit a U-shaped attention curve — highest weight on the start and end of the context window. Information in the middle gets deprioritized. As conversations grow, earlier observations drift into the low-attention zone but remain "believed" because the model never explicitly invalidated them.

- Liu et al., "Lost in the Middle: How Language Models Use Long Contexts" (2023) — https://arxiv.org/abs/2307.03172

**Context rot.** Chroma Research found that performance degrades measurably as input tokens increase, even on simple retrieval tasks. Claude models showed the largest performance gaps between focused (~300 tokens) and full (~113K tokens) inputs.

- https://www.trychroma.com/research/context-rot

**Early-turn anchoring.** Models make assumptions in early turns and over-rely on them. A study showed a 39% average performance drop when information was staged across multi-turn exchanges versus presented at once.

- https://www.dbreunig.com/2025/06/22/how-contexts-fail-and-how-to-fix-them.html

**Agent drift.** SWE-bench Pro showed frontier models dropped from ~70% success on short tasks to ~23% on long-horizon tasks. As execution logs accumulate, they push the original instructions to the periphery of effective attention.

- https://prassanna.io/blog/agent-drift/

### Mitigation Techniques

| Technique | Level | Description |
|---|---|---|
| **Explicit re-verification rules** | Prompt | "ALWAYS run `git status` before any git operation. Re-read any file before editing if more than N tool calls have passed." |
| **Todo-list rewriting** | Architecture | Constantly rewrite a plan/todo file to push objectives into recency zone (Manus pattern) |
| **Observation masking** | Architecture | Replace older environment observations with placeholders while preserving agent reasoning (JetBrains: 2.6% higher solve rate, 52% lower cost) |
| **Sub-agent isolation** | Architecture | Spawn sub-agents with clean context for focused tasks; return condensed summaries |
| **Git worktree isolation** | Architecture | Physical isolation prevents shared-checkout state confusion |

---

## 2. Tool Underuse (Failure to Verify)

### Definition

The LLM generates answers from parametric knowledge (training data) or conversation memory instead of calling available tools to verify. It produces plausible-sounding but potentially outdated or incorrect answers when a tool call would have provided the ground truth.

### Manifestations in Coding Agents

| Pattern | Example |
|---|---|
| Skipping web search | Agent recommends a library API based on training data; the API changed 6 months ago |
| Answering from memory | Agent says "the tests pass" without running them |
| Fabricating state | Agent claims a dependency exists at version X without checking `package.json` |
| Skipping file reads | Agent generates code from memory instead of reading the existing codebase for patterns and conventions |
| Not re-checking after action | Agent assumes a `git push` succeeded without verifying |

### Root Causes

**RLHF training bias.** Human raters prefer confident, fluent answers over hedging or tool-calling pauses. This creates a training signal that rewards generating plausible text over verification. Both humans and preference models prefer convincingly-written responses over correct ones a non-negligible fraction of the time.

- Anthropic, "Towards Understanding Sycophancy in Language Models" (2023) — https://arxiv.org/pdf/2310.13548

**The Reasoning Trap.** Reinforcement learning that enhances reasoning simultaneously increases tool hallucination. Even training on pure mathematics elevated subsequent tool hallucination rates. The mechanism is "representation collapse" — reasoning RL destabilizes tool-related neural representations.

- "The Reasoning Trap" (2025) — https://arxiv.org/abs/2510.22977

**Tool selection overload.** Tool-calling hallucinations increase with tool count. With 31 tools available, agents more often generate answers from memory; with 3 relevant tools, they actually use them.

- https://dev.to/aws/stop-ai-agent-hallucinations-4-essential-techniques-2i94

**Token efficiency optimization.** The model implicitly optimizes for shorter responses. A tool call adds latency and tokens; generating from memory is "cheaper" from the model's perspective, even if less accurate.

### Mitigation Techniques

| Technique | Level | Description |
|---|---|---|
| **Tool-first instructions** | Prompt | "NEVER answer from memory when a tool can provide the answer. ALWAYS use WebSearch before recommending library APIs or versions." |
| **Chain of Verification (CoVe)** | Prompt | Force: (a) draft answer, (b) list verification questions, (c) answer each using tools, (d) produce final answer |
| **Semantic tool filtering** | Architecture | Only pass top-k relevant tools to reduce choice overload |
| **Neurosymbolic guardrails** | Architecture | Framework-level hooks that validate tool calls against rules; LLM cannot bypass because they execute outside the prompt |
| **Multi-agent validation** | Architecture | Executor/Validator pattern where Validator independently checks whether Executor actually called required tools |

---

## Implications for Zpit Agents

### Current Zpit Mitigations (Already in Place)

- **Git worktree isolation** — each agent gets its own worktree (physical state isolation)
- **Sub-agent architecture** — clarifier, coder, reviewer are separate agents with clean contexts
- **Explicit "stop and ask" rules** — coding/revision agents must stop on uncertainty
- **Mandatory WebSearch** — clarifier must search for every new requirement (in `clarifier.md`)

### Gaps to Address

1. ~~**No re-verification mandate**~~: **Resolved.** `docs/agent-guidelines.md` now includes a Re-verification Protocol (run `git status` before git ops, re-read files after 5+ tool calls, re-read modified files after changes). Also reflected in `task-runner.md` and `efficiency.md`.

2. **No web search mandate for coding/revision agents**: Only the clarifier has a mandatory WebSearch rule. Coding and revision agents may implement against outdated API knowledge without checking.

3. **No freshness signals**: The system does not inject timestamps or "last verified" markers into the context, so the agent has no way to judge whether information is stale.

4. **Long conversation drift**: Loop engine sessions can span many tool calls. No mechanism to periodically re-anchor the agent to the current plan/state.

---

## References

### Foundational Research
- [Lost in the Middle: How Language Models Use Long Contexts](https://arxiv.org/abs/2307.03172) — Liu et al., 2023
- [Towards Understanding Sycophancy in Language Models](https://arxiv.org/pdf/2310.13548) — Anthropic, 2023
- [The Reasoning Trap: How Enhancing LLM Reasoning Amplifies Tool Hallucination](https://arxiv.org/abs/2510.22977) — 2025
- [LLM-based Agents Suffer from Hallucinations: Survey](https://arxiv.org/html/2509.18970v1) — Lin et al., 2025

### Engineering & Practical
- [Effective Context Engineering for AI Agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) — Anthropic
- [Context Engineering Lessons from Building Manus](https://manus.im/blog/Context-Engineering-for-AI-Agents-Lessons-from-Building-Manus) — Manus
- [Context Rot: Increasing Input Tokens Impacts LLM Performance](https://www.trychroma.com/research/context-rot) — Chroma Research
- [How Long Contexts Fail](https://www.dbreunig.com/2025/06/22/how-contexts-fail-and-how-to-fix-them.html) — Breunig, 2025
- [Agent Drift: How Autonomous AI Agents Lose the Plot](https://prassanna.io/blog/agent-drift/) — Ravishankar
- [Efficient Context Management for LLM-Powered Agents](https://blog.jetbrains.com/research/2025/12/efficient-context-management/) — JetBrains Research
- [Stop AI Agent Hallucinations: 4 Essential Techniques](https://dev.to/aws/stop-ai-agent-hallucinations-4-essential-techniques-2i94) — AWS
- [AI Agent Guardrails: Rules That LLMs Cannot Bypass](https://dev.to/aws/ai-agent-guardrails-rules-that-llms-cannot-bypass-596d) — AWS
- [COLLAPSE.md Specification](https://collapse.md/)
