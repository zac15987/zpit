# Zpit 架構文件索引

> 版本: 2.0 | 日期: 2026-04-01

Zpit 是一個 TUI 調度中心，用 Go + Bubble Tea 打造，負責選專案、啟動 Claude Code agent、監控進度、協調完整的 issue 生命週期。

---

| # | 文件 | 內容 |
|---|------|------|
| 01 | [願景](01-vision.md) | 調度模式設計原則、終端啟動方式 |
| 02 | [TUI 介面設計](02-tui-design.md) | 各畫面 mockup（含已實作/未實作標注） |
| 03 | [系統架構](03-system-architecture.md) | 架構圖、Terminal Launcher、Session Log Watcher |
| 04 | [設定檔與 Provider](04-config.md) | config.toml 結構、TrackerClient、Profile |
| 05 | [Issue Spec](05-issue-spec.md) | 格式定義、驗證邏輯、Prompt 模板 |
| 06 | [Agent 定義與 i18n](06-agents.md) | Clarifier/Reviewer/Task-Runner agent、go:embed 部署、i18n |
| 07 | [Worktree + Loop + 狀態流](07-worktree-and-loop.md) | Worktree 架構、Loop 狀態機、Issue 狀態流 |
| 08 | [阻塞與通知](08-notification.md) | Agent 阻塞偵測、通知管道 |
| 09 | [安全與管控](09-safety.md) | 5 層安全、Hook 系統、ZPIT_AGENT |
| 10 | [AppState 與多客戶端](10-appstate.md) | SSH Server、併發安全、Pub/Sub |
| 11 | [Milestone 紀錄](11-milestone.md) | M1-M4c 完成紀錄、M5 規劃 |
| 12 | [跨 Agent Channel 通訊](12-channel.md) | Broker + MCP + 跨專案通訊、TUI 整合 |
