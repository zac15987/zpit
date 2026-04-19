# 11. Milestone 紀錄

> 此文件為歷史紀錄，記錄各階段的完成狀態。

---

## M1: 能用的最小版本 ✅

- [x] Go 專案骨架 + Bubble Tea 基礎框架
- [x] config.toml 讀取 (TOML)
- [x] 專案選擇 → 自動 cd + 在新終端啟動 Claude Code
- [x] 偵測 Windows / WSL 環境，選對應 path 和終端啟動方式
- [x] Terminal Launcher 模組（wt new-tab / tmux new-window）
- [x] Hook 腳本撰寫 + 測試（path-guard / bash-firewall / git-guard）
- [x] 第一個專案的 .claude/settings.json + .claude/hooks/ 建立（auto-deploy from embedded binary）
- [x] Code Construction Principles 整合至 Reviewer 流程 + 部署腳本

## M2: Session Log 監控 + 通知 ✅

- [x] Session Log Watcher 模組（fsnotify + log 解析）
- [x] TUI「活躍終端」區域即時更新
- [x] Agent 等待回應偵測 + TUI 變色警示
- [x] Windows Toast 通知
- [x] 音效提示

## M3: Clarifier + Tracker 串接 ✅

- [x] TrackerClient 模組：直接 REST API（Forgejo / GitHub），token_env auth
- [x] Issue Spec 格式驗證模組（ValidateIssueSpec + ParseIssueSpec）
- [x] Clarifier agent 定義（agents/clarifier.md，go:embed 嵌入）
- [x] TUI [c] clarify：開新終端啟動 claude --agent clarifier（未部署時 huh confirm 自動部署）
- [x] TUI [s] status：唯讀 issue 列表（透過 TrackerClient 拉取）+ [y] 確認 + [i] 開瀏覽器
- [x] TUI [i] open tracker：主畫面開瀏覽器到 issue list
- [x] 「待確認」→「Todo」確認流程（[y] 透過 TrackerClient 改 label）

## M4a: Worktree + Prompt 模板 + Profile ✅

- [x] Worktree Manager 模組（建立 / 清理 / 列出，shell out to git）
- [x] Worktree 建立時根據 hook_mode 自動配置 settings.local.json
- [x] Hook 自動化測試（make test-hooks）
- [x] Coding Agent Prompt 模板實作（Issue Spec → prompt 組裝 + log_policy 注入）
- [x] Reviewer 驗收模板實作（Issue Spec → reviewer prompt 組裝）
- [x] TrackerClient 擴充：GetIssue（含 body）、GetPRStatus
- [x] Profile 定義落地至 config.toml（log_policy: strict/standard/minimal）
- [x] Reviewer agent 定義（agents/reviewer.md，go:embed）+ TUI [r] 部署啟動
- [x] Per-project base_branch config（預設 "dev"）
- [x] Slug 工具（issue title → URL-safe slug）

## M4b: Loop 引擎 + 自動化 ✅

- [x] Loop 引擎實作（抓 todo → 建 worktree → coding agent → PR 出現觸發 reviewer → PR merge 清理）
- [x] 同一專案多 agent 平行執行（受 max_per_project 限制）
- [x] TUI [l] toggle + Loop Status 顯示
- [x] PR merge 偵測（FindPRByBranch）+ 自動清理 worktree
- [x] LaunchClaudeInDir — worktree path override
- [x] Coding 完成信號：label poll 偵測 "review" label（非 PID 消失），終端保留
- [x] NEEDS CHANGES 自動重試（reviewer 判定→重跑 coding→再 review，max_review_rounds 限制）
- [x] Reviewer label 更新（PASS → ai-review, NEEDS CHANGES → needs-changes）
- [x] BuildRevisionPrompt — 修正版 coding prompt（讀 review comment → 修正 → 重送）
- [x] Label on-demand 檢查：操作前檢查 required labels，缺少時 overlay confirm dialog 確認後建立（LabelManager interface）
- [x] Per-issue branch 控制：Issue Spec `## BRANCH` → coding prompt 強制 PR target、reviewer 驗證

## M4c: SSH 遠端存取 + 併發安全 ✅

- [x] AppState 分離：共享狀態抽出為獨立 struct，Model 只保留 per-connection UI 狀態
- [x] SSH Server（Wish）：`zpit serve` 無頭 SSH daemon + `zpit connect` 便利包裝
- [x] SSH 認證：public key（authorized_keys）+ password（env var），至少啟用一個
- [x] SSHConfig：`[ssh]` section in config.toml
- [x] Remote session lifecycle：`isRemote=true` quit 不停 watchers/loops，server init 只跑一次
- [x] `main.go` 重構為子命令 routing（runLocalTUI / runServe / runConnect）
- [x] `sync.RWMutex` 保護 AppState mutable fields
- [x] Channel-based pub/sub（Subscribe/Unsubscribe/NotifyAll）：狀態變更廣播 StateRefreshMsg 給所有客戶端
- [x] 兩個獨立 mutex（`mu` for state、`subMu` for subscribers）避免 deadlock
- [x] Copy-before-closure + action-defer pattern 避免 nested lock
- [x] 所有 loop handlers 加 write lock + NotifyAll，所有 loop cmds 加 read lock
- [x] View rendering 加 RLock

## Refactoring: model.go 拆分 ✅

> PR #59 | Issue #24

- [x] model.go（2433 行）拆分為 6 檔，降至 860 行
- [x] 新增 session.go（733 行）：session 生命週期、discovery、monitoring、liveness、permission detection
- [x] 新增 launch.go（479 行）：terminal launch cmds、slot operations、deploy helpers
- [x] 新增 tracker_ops.go（242 行）：label check/ensure、issue load/confirm
- [x] 新增 confirm.go（208 行）：confirm dialogs、executePendingOp、undeploy
- [x] 新增 channel.go（78 行）：broker EventBus subscription、event reading
- [x] update() 所有 >5 行 inline handler 轉為 one-line dispatch（`return m.handleXxx(msg)`）
- [x] 每個新檔案頂部標註 lock protocol doc comment
- [x] 純 code movement + inline handler extraction，零行為變更

---

## Refactoring: Task 執行模型重構 ✅

> PR #69 | Issue #68

- [x] 新增 `agents/task-runner.md` subagent 定義（tools: Read, Write, Edit, Bash, Glob, Grep）
- [x] 改寫 `buildTaskWorkflow()` 為 subagent/team delegation prompt 生成
- [x] 新增 `groupTasks()` 將 tasks 分組為循序 singletons 和平行 batches
- [x] 新增 `buildSubagentDelegation()` 和 `buildTeamDelegation()` prompt builders
- [x] `main.go` 新增 `//go:embed agents/task-runner.md`，`AppState` 新增 `taskRunnerMD` 欄位
- [x] `loopWriteAgentCmd()` 在 spec 含 TASKS 時部署 `task-runner.md` 到 worktree
- [x] 測試更新：循序 tasks、混合平行 tasks、無 tasks 三種場景

---

## M4d: Cross-Agent Channel 通訊

> 已完成

- [x] HTTP broker (`internal/broker/`) — REST endpoints for artifacts + messages, SSE streaming, project discovery
- [x] MCP stdio server (`internal/mcp/`) — 7 tools: publish_artifact, list_artifacts, send_message, list_projects, subscribe_project, unsubscribe_project, list_subscriptions
- [x] Channel view in TUI (`[m]` key) — cross-project event timeline
- [x] Dynamic subscription management (subscribe/unsubscribe at runtime)
- [x] Meeting Protocol for clarifier agents (Facilitator/Advisor roles)
- [x] AgentName tracking (`{type}-{4hex}` for manual, `{role}-#{issueID}` for loop)
- [x] Self-echo filtering via per-instance UUID

## M4e: Terminal Focus Panel + Exit Wrappers

> 已完成

- [x] Three-panel focus system (Projects → Terminals → LoopSlots) with Tab cycling
- [x] `[x]` key to kill selected terminal (confirm dialog, force-kill process)
- [x] `zpit-exit.cmd` / `zpit-exit.ps1` exit wrappers for auto-closing WT tabs
- [x] WindowSizeMsg on confirm dialog init for immediate button rendering

## M4f: Redeploy 快捷鍵 + 部署狀態標記

> 已完成

- [x] `[d]` Redeploy hotkey：一鍵 undeploy + 重新部署 4 個 agents（clarifier/reviewer/task-runner/efficiency）+ hooks + docs，不啟動 Claude；confirm 對話框保護
- [x] `deployAllCmd()` in `internal/tui/launch.go` — 沿用 `undeployFiles` + `DeployHooksToProject` + `deployDocs`，無 `.mcp.json` 寫入
- [x] 專案列表狀態標記：🟢 已部署（10 個檔案齊全）/ 🟡 部分部署 / ⚪ 未部署
- [x] `deployStatus()` in `view_projects.go` — 每次渲染 `os.Stat` 10 個檔案，不快取
- [x] `showRedeployConfirm()` 沿用 huh confirm dialog pattern
- [x] i18n：7 個新 locale key（KeyRedeploy、KeyRedeployConfirm/Button/Done、KeyDeployStatus{Full,Partial,None}）

## M4g: Dock 版面 + Catppuccin Mocha

> 已完成

- [x] `ViewProjects` 從單一共用 viewport 改為四面板 dock：專案 / 活躍終端 / Loop（左欄堆疊）+ 快捷鍵（右欄固定）；每個面板各自擁有 `viewport.Model`，可獨立上下捲動
- [x] 版面演算法 `computePanelRects`：70/30 欄寬 + `dockMinLeftWidth`/`dockMinRightWidth` clamp；左欄高度依權重分配（專案 3 / 終端 2 / Loop 2），空面板自動收合；不再有 `<100 cols` 單欄 fallback — 任何寬度 Hotkeys 永遠 dock 右邊
- [x] 標題 chrome：`▎` mauve 焦點 bar（單欄寬，只出現在 focused 面板的標題列）+ 大寫 title + count badge + 6 字 rule；堆疊面板前加一列 gutter blank 分隔
- [x] 256-color ANSI → Catppuccin Mocha 24-bit 色盤；`colorAccent/Text/Muted/...` 語意 alias 保留，call sites 不動
- [x] 滑鼠滾輪 `hitTestDockPanel`：依游標位置分派到命中面板的 viewport，未命中回退 projectsVP
- [x] `renderTerminalsBody` / `renderLoopBody` 建 `termLineStarts` / `loopLineStarts` 供可變 stride cursor-follow 使用
- [x] `TestComputePanelRects` 8 個 case 覆蓋 wide/narrow/empty/clamp 邊界
- [x] 後續 refactor：`panelInnerSize` + `applyPanelContent` 抽出 4 個 sync 函式的重複 prologue/epilogue；`dockPanel` struct 把 `renderOne`/`renderPanelChrome` 參數從 7/6 降到 1；magic number 全改為具名常數

---

## M5: 完整體驗（規劃中）

- [ ] 機台 push 回來後自動觸發 review
- [ ] 最近活動 feed（從 session log 解析）
- [ ] shared-core 跨專案影響偵測
- [ ] 開機自啟動設定（Windows startup / WSL .bashrc）
- [ ] Cross-compile: 同一份 code 編譯 Windows + Linux binary
- [ ] TUI log area（主畫面底部可捲動事件 log，顯示最近 N 筆）

## Refine: 體驗優化

- [ ] 專案 CLAUDE.md 模板（TUI 按鍵觸發 claude /init，已有則跳過）
