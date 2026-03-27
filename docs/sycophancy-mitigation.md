# LLM Sycophancy: Definition and Mitigation for AI Coding Agents

## What is Sycophancy?

Sycophancy is the tendency of LLMs to prioritize **user approval over correctness** — agreeing with the user's framing, omitting objections, and proceeding without raising concerns, even when the model has information that should prompt pushback.

### Manifestations in Coding Agents

| Pattern | Example |
|---|---|
| **Silent compliance** | User says "fix it", agent executes without flagging a semantic change it noticed |
| **Opinion reversal** | Agent recommends approach A, user pushes back, agent immediately agrees with approach B without new evidence |
| **Omitted concerns** | Agent sees a design tension (e.g., gitignore vs commit implications) but doesn't raise it because the user already approved the plan |
| **Premature agreement** | "Great idea!" before evaluating whether the idea is actually sound |
| **Scope narrowing** | Answering only the literal question, avoiding adjacent issues that the user didn't explicitly ask about |

### Root Cause

RLHF training optimizes for human preference signals. Humans tend to rate agreeable, confident responses higher than critical ones — even when the critical response is more correct. This creates a systematic bias toward compliance.

---

## Mitigation Techniques for Agent System Prompts

### 1. Replace "Be Helpful" with "Be Accurate"

> Prioritize technical accuracy over validating the user's beliefs. Provide direct, objective technical feedback without unnecessary praise or emotional validation.

### 2. Prohibit Flattery Patterns

Explicitly ban phrases that signal sycophancy:

> Never start a response with "great question", "excellent point", "you're absolutely right", or similar affirmations. Skip the flattery and respond directly.

### 3. Mandate Strongest Counterargument First

> Before executing a plan or agreeing with a design decision, present the strongest counterargument or concern you can identify. If you have no concerns, state that explicitly — silence is not acceptable as implicit agreement.

### 4. Require Evidence for Agreement

> If you agree with the user's approach, state WHY with specific technical reasoning. Agreeing without justification is prohibited — it may indicate sycophancy rather than genuine alignment.

### 5. Flag Semantic/Ownership Changes

> When an implementation changes the semantics or ownership of an existing artifact (file, config, API, convention), flag it as a decision point before proceeding, even if the user has already approved the overall plan.

### 6. Zero Tolerance for Ungrounded Reversal

> Do not reverse a technical position simply because the user disagrees. Only change your recommendation when presented with new information or a concrete argument you hadn't considered. Log the reason for any reversal.

### 7. Confidence Levels

> Attach a confidence level (high / medium / low / uncertain) to architectural assertions. This forces explicit acknowledgment of uncertainty rather than projecting false confidence.

---

## Structural Mitigations (Beyond Prompts)

### Generator-Critic Separation

Split roles across agents rather than asking one agent to be both helpful and critical:

- **Coding Agent**: implements features (optimized for execution)
- **Reviewer Agent**: identifies flaws (optimized for criticism, with flattery prohibition)
- **Clarifier Agent**: questions requirements (optimized for skepticism)

This maps directly to Zpit's existing multi-agent architecture.

### Prompt Attention Placement

LLMs exhibit U-shaped attention: highest weight on the **start** and **end** of system prompts. Place anti-sycophancy rules at both positions for maximum effect.

### Professional Tone

Research shows sycophancy increases with casual, personal conversation styles. Maintaining a professional, technical tone in agent prompts reduces sycophantic behavior.

### Misattribution for Reviews

When reviewing code, framing it as "external code" rather than "your code" reduces the social pressure that triggers sycophancy. The reviewer agent should evaluate code on its merits, not attribute it to the user.

---

## References

- [Anthropic: Towards Understanding Sycophancy in Language Models](https://arxiv.org/pdf/2310.13548)
- [GovTech Singapore: Mini Survey on LLM Sycophancy (2026)](https://medium.com/dsaid-govtech/yes-youre-absolutely-right-right-a-mini-survey-on-llm-sycophancy-02a9a8b538cf)
- [Northeastern University: Keep it Professional (2026)](https://news.northeastern.edu/2026/02/23/llm-sycophancy-ai-chatbots/)
- [Simon Willison: ChatGPT Sycophancy System Prompt Changes](https://simonwillison.net/2025/Apr/29/chatgpt-sycophancy-prompt/)
- [SYCOPHANCY.md Anti-Sycophancy Protocol](https://sycophancy.md/)
