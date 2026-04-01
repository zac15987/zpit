# 1. 願景

> 版本: 2.0
> 日期: 2026-04-01
> 作者: 智豪 + Claude

---

一個開機就緒的 TUI **調度中心**，讓你只需要：
1. 說出模糊的需求
2. 確認 AI 產出的 issue
3. Review PR 和機台驗證

中間的一切 — 需求釐清、實作、編譯、code review、開 PR、更新狀態 — 全部自動化。

## 核心設計原則：調度模式（非包裹模式）

TUI **不是** Claude Code 的外殼。Claude Code 在獨立的終端視窗中運行，
你可以隨時切過去直接操作。TUI 是調度中心 — 選專案、啟動 agent、
監控進度、顯示狀態。

```
                    ┌─────────────────────────┐
                    │  TUI 調度中心           │
                    │  (Bubble Tea, 常駐)      │
                    │  - 選專案               │
                    │  - 啟動 agent           │
                    │  - 即時狀態監控         │
                    └─────┬───────────────────┘
                          │ 開新終端
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
   ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
   │WT Tab: ASE  │ │WT Tab: 網頁 │ │WT Tab: Tool │
   │ claude code │ │ claude code │ │ claude code │
   │ (你可隨時   │ │             │ │             │
   │  切過去操作)│ │             │ │             │
   └──────┬──────┘ └──────┬──────┘ └──────┬──────┘
          │               │               │
          ▼               ▼               ▼
     session log     session log     session log
          │               │               │
          └───────────────┼───────────────┘
                          │ tail -f 監控
                          ▼
                    TUI 即時狀態更新
```

**不同環境的終端啟動方式：**

| 環境 | 啟動方式 | 切換方式 |
|------|---------|---------|
| Windows Terminal | `wt.exe new-tab -d <path> -- claude` | Alt+Tab 或 Ctrl+Tab 切 tab |
| WSL (tmux) | `tmux new-window -n <name> -c <path> "claude"` | TUI 顯示 `tmux select-window -t <name>` |
| Linux (tmux) | 同 WSL | 同 WSL |
