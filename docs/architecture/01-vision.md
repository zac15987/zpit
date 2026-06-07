# 1. Vision

> Version: 2.0
> Date: 2026-04-01
> Author: Chih Hao + Claude

---

A boot-ready TUI **dispatch center** where all you need to do is:
1. Describe a vague requirement
2. Confirm the AI-generated issue
3. Review the PR and validate on the target machine

Everything in between — requirement clarification, implementation, compilation, code review, opening PRs, updating status — is fully automated.

## Core Design Principle: Dispatch Mode (Not Wrapper Mode)

The TUI is **not** a shell around Claude Code. Claude Code runs in independent terminal windows
that you can switch to and operate directly at any time. The TUI is the dispatch center —
it selects projects, launches agents, monitors progress, and displays status.

```
                    ┌─────────────────────────┐
                    │  TUI Dispatch Center     │
                    │  (Bubble Tea, resident)  │
                    │  - select project        │
                    │  - launch agent          │
                    │  - live status monitor   │
                    └─────┬───────────────────┘
                          │ open new terminal
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
   ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
   │WT Tab: ASE  │ │WT Tab: Web  │ │WT Tab: Tool │
   │ claude code │ │ claude code │ │ claude code │
   │ (switch to  │ │             │ │             │
   │  it anytime)│ │             │ │             │
   └──────┬──────┘ └──────┬──────┘ └──────┬──────┘
          │               │               │
          ▼               ▼               ▼
     session log     session log     session log
          │               │               │
          └───────────────┼───────────────┘
                          │ tail -f monitor
                          ▼
                  TUI live status update
```

**Terminal launch method by environment:**

| Environment | Launch Method | How to Switch |
|-------------|--------------|---------------|
| Windows Terminal | `wt.exe new-tab -d <path> -- claude` | Alt+Tab or Ctrl+Tab to switch tabs |
| WSL (tmux) | `tmux new-window -n <name> -c <path> "claude"` | TUI shows `tmux select-window -t <name>` |
| Linux (tmux) | Same as WSL | Same as WSL |
