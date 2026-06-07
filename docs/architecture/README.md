# Zpit Architecture Document Index

> Version: 2.2 | Date: 2026-04-19

Zpit is a TUI dispatch center built with Go + Bubble Tea. It selects projects, launches Claude Code agents, monitors their progress, and coordinates the full issue lifecycle.

---

| # | Document | Contents |
|---|----------|----------|
| 01 | [Vision](01-vision.md) | Dispatch model design principles, terminal launch strategy |
| 02 | [TUI Interface Design](02-tui-design.md) | Screen mockups for each view (with implemented/unimplemented annotations) |
| 03 | [System Architecture](03-system-architecture.md) | Architecture diagram, Terminal Launcher, Session Log Watcher |
| 04 | [Config & Providers](04-config.md) | config.toml structure, TrackerClient, Profile |
| 05 | [Issue Spec](05-issue-spec.md) | Format definition, validation logic, prompt templates |
| 06 | [Agent Definitions & i18n](06-agents.md) | Clarifier/Reviewer/Task-Runner agents, go:embed deployment, i18n |
| 07 | [Worktree + Loop + State Flow](07-worktree-and-loop.md) | Worktree architecture, Loop state machine, issue status flow |
| 08 | [Blocking & Notifications](08-notification.md) | Agent blocking detection, notification channels |
| 09 | [Safety & Control](09-safety.md) | 5-layer safety system, hook system, ZPIT_AGENT |
| 10 | [AppState & Multi-Client](10-appstate.md) | SSH server, concurrency safety, pub/sub |
| 11 | [Milestone Log](11-milestone.md) | M1–M4c completion records, M5 planning |
| 12 | [Cross-Agent Channel Communication](12-channel.md) | Broker + MCP + cross-project communication, TUI integration |
| 13 | [Desktop Agent](desktop-agent.md) | Proxy MCP architecture, policy model, tool allowlist, deny_keys; backed by `zpit-desktop-mcp` (fork of `@zavora-ai/computer-use-mcp`) |
