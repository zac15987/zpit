# 6. Agent 定義與 i18n

---

## 6.1 Clarifier Agent (.claude/agents/clarifier.md)

**Deployment:** go:embed 嵌入 Zpit binary，部署到每個專案的 `.claude/agents/`。
模板內容相同 — 專案特定上下文來自 CLAUDE.md（agent 啟動時自動讀取）。

```yaml
name: clarifier
description: Requirements clarification and technical advisor
tools: Read, Grep, Glob, Bash, WebSearch, WebFetch
disallowedTools: Edit
```

**核心行為：**
- 將模糊需求轉化為結構化的 Issue Spec
- 主動比較 2-3 種實作方案及其取捨
- 一次問一個釐清問題
- 確認分支策略（讀 tracker.md 得知預設，有需要問使用者）
- 推上 Tracker 前自我驗證 Issue Spec 格式完整性
- 推送前必須讓使用者在終端中明確確認（label: pending）
- **強制使用 WebSearch** 查詢最新資訊（不依賴訓練資料猜測）
- 需讀取第三方原始碼時使用 WebFetch
- Write tool 限制為暫存檔（MCP 長文字 workaround）

**會議模式（Meeting Protocol）：**

當 Channel 工具可用（`.mcp.json` 存在 → MCP server 啟動）**且**透過 `list_projects` 的 `agents.clarifier` 計數發現其他 clarifier agent 時，自動啟用會議模式。若任一條件不滿足（無 channel 或無其他 clarifier），行為與單人模式完全相同。

會議模式採用 **Facilitator/Advisor 角色模型**：

- **角色分配**：第一個廣播 `[Joining Meeting]` 的 agent 成為 Facilitator；後續加入者自動成為 Advisor
- **Facilitator**：驅動完整工作流（步驟 1-17），在關鍵步驟前檢查 channel 獲取 Advisor 分析，是唯一向使用者提問和撰寫 Issue Spec 的 agent，轉發使用者回答給 Advisor
- **Advisor**：獨立讀取 codebase 並將分析傳送給 Facilitator，進入跟隨模式——回應 Facilitator 的訊息表達同意/異議/補充，不獨立執行步驟 5-17，不直接向使用者提問（`[⚠ Warning]` 緊急警告除外）
- **收斂**：Facilitator 在收斂前驗證所有 SCOPE 路徑存在，issue 推送後廣播 `[Meeting Closed]`

會議協議疊加在原工作流（步驟 1-17）之上作為 Facilitator 的額外通訊層，Advisor 不獨立執行完整流程。完整協議規格見 `agents/clarifier.md` 的 Meeting Protocol 區段。

完整模板見 `agents/clarifier.md`。

---

## 6.2 Reviewer Agent (.claude/agents/reviewer.md)

```yaml
name: reviewer
description: Code Review expert
tools: Read, Grep, Glob, Bash
disallowedTools: Write, Edit
```

**核心行為：**
- 逐條比對 ACCEPTANCE_CRITERIA：✅ / ❌ / ⚠️
- 檢查 SCOPE 越界和 CONSTRAINTS 合規
- 驗證 PR target branch 是否匹配預期的 base branch
- 依 `code-construction-principles.md` 抽樣檢查 code 品質
- 產出 Review Report（嚴重度標記：🔴 MUST FIX / 🟡 SUGGEST / 🟢 NICE）
- 將 report 寫到 PR comment 和 issue comment
- 設定判定 label：ai-review (PASS) 或 needs-changes (NEEDS CHANGES)

**判定規則：**
- 任何 AC ❌ → NEEDS CHANGES
- 全部 AC ✅ 但有建議 → PASS with suggestions
- SCOPE/CONSTRAINTS 違反 → NEEDS CHANGES（無論 AC 狀態）

完整模板見 `agents/reviewer.md`。

---

## 6.3 Task Runner Subagent (.claude/agents/task-runner.md)

**Deployment:** go:embed 嵌入 Zpit binary。僅在 Issue Spec 含 `## TASKS` 時由 `loopWriteAgentCmd()` 部署到 worktree 的 `.claude/agents/`。

```yaml
name: task-runner
description: Single-task execution subagent
tools: Read, Write, Edit, Bash, Glob, Grep
```

**核心行為：**
- 實作**恰好一個** task（由主 coding agent 指派）
- 啟動時讀取 CLAUDE.md、agent-guidelines.md、code-construction-principles.md
- 僅修改指派範圍內的檔案；發現需改範圍外檔案時回報主 agent
- Commit 格式：`[ISSUE-ID] T{N}: {short description}`（使用 `git add` 指定檔案，不用 `-A`）
- 錯誤處理：嘗試修復一次，仍失敗則回報主 agent
- 完成後提供摘要：修改檔案、實作內容、commit hash（成功）或錯誤詳情（失敗）

**使用方式：**
- 循序 task：主 coding agent 透過 Agent tool 的 `subagent_type: "task-runner"` 逐一委派
- 平行 task（`[P]`）：主 coding agent 建立 Agent Team，每個 teammate 使用 `task-runner` subagent type

完整模板見 `agents/task-runner.md`。

---

## 6.4 Efficiency Agent (.claude/agents/efficiency.md)

**Deployment:** go:embed 嵌入 Zpit binary，透過 `[f]` 快捷鍵部署。使用 `deployAndLaunchAgentLite`（不部署 hooks、不設定 ZPIT_AGENT=1）。

```yaml
name: efficiency
description: Lightweight fast-track agent for rapid iteration
```

**核心行為：**
- 輕量快速迭代 agent — 無 Issue Spec、無 tracker 整合、無 worktree、無 hooks
- 啟動時讀取 CLAUDE.md + agent-guidelines.md + code-construction-principles.md
- Plan-before-act 工作流：呈現修改計畫（檔案 + 變更 + 預期行為），等使用者確認才編輯
- 實作後 self-review：重讀修改檔案、比對計畫、依 code-construction-principles 評估品質
- Conventional commit 格式（feat:/fix:/refactor:/chore:/docs:/test:/style:/perf:）
- Plan mode 紀律：plan mode 禁止編輯檔案

**部署語義差異（vs. clarifier/reviewer）：**

| 項目 | clarifier/reviewer | efficiency |
|------|-------------------|------------|
| 部署函式 | `deployAndLaunchAgent` | `deployAndLaunchAgentLite` |
| Hooks | ✅ DeployHooksToProject | ❌ 不部署 |
| ZPIT_AGENT=1 | ✅ 設定 | ❌ 不設定 |
| Tracker 整合 | ✅ label check | ❌ 無 |
| Worktree | ✅ (Loop 模式) | ❌ 直接在專案目錄 |

完整模板見 `agents/efficiency.md`。

---

## 6.5 go:embed 部署流程

Agents、hooks、docs 嵌入 binary，每次 agent 啟動時自動部署：

```
main.go (go:embed vars)
  → NewAppState(cfg, clarifierMD, reviewerMD, taskRunnerMD, efficiencyMD, guidelinesMD, principlesMD, hookScripts)
    → stored in AppState fields
      → DeployHooks() on every agent launch ([c]/[r]/[l]) — [f] uses deployAndLaunchAgentLite (no hooks)
        → writes to target project's .claude/hooks/, .claude/agents/, .claude/docs/
        → merges hook config into .claude/settings.json (or settings.local.json for worktrees)
      → loopWriteAgentCmd() deploys task-runner.md when Issue Spec contains TASKS
```

變更 `agents/*.md`、`hooks/*.sh` 或 `docs/agent-guidelines.md` 後需要重新 build 才會生效。

---

## 6.6 Internationalization (i18n)

所有 agent template (.md) 和 prompt builder (.go) 以**英文**撰寫。回應語言由 config 控制：

```toml
language = "zh-TW"  # or "en" (default)
```

- **TUI 字串**：`internal/locale/` package，`T(key)` 查找。翻譯在 `en.go` 和 `zh_tw.go`。
- **Agent prompts**：`locale.ResponseInstruction()` 回傳 `"Always respond in Traditional Chinese (zh-TW).\n\n"`（zh-TW）或 `""`（English）。
- **Agent .md 檔案**：語言指示在 deploy time 由 `injectLangInstruction()` 注入（YAML frontmatter 之後）。

新增語言需要：新的 `locale/{lang}.go` 翻譯 map + `SetLanguage()` 和 `ResponseInstruction()` 的新 case。

---

## 6.7 CLAUDE.md 模板

每個目標專案根目錄放一份，agent 實作時會自動讀取。
以下為建議模板結構（Zpit 不自動產生，由使用者維護）：

```markdown
# CLAUDE.md — [專案名稱]

## 專案概述
- 類型: [machine / web / desktop / android]
- 技術棧: [WPF .NET 4.8 / Astro / Kotlin / ...]
- 用途: [一句話描述]

## 架構原則（不可違反）
- [例: 所有硬體操作必須有 timeout]
- [例: UI 更新必須回到 UI thread]

## Code 品質基準
- 遵循 `.claude/docs/code-construction-principles.md`

## Logging 現狀與規範
### 現有系統
- 使用: [NLog / Serilog / 自訂]

### 新 code 規範
- 格式: logger.Info("[{Module}] [{Method}] {Message}", ...)
- 碰到舊 code 修改時：順手補上 module/method
- 不主動重構舊 log

## Agent 行為原則
- 遇到不確定的技術決策時，必須停下來問使用者
- 即使在 bypass all permissions 模式下也一樣
- 停下來時清楚說明：你卡在什麼問題、有哪些選項、你的建議是什麼

## Git 規範
- branch 命名: feat/ISSUE-ID-description
- commit message: [ISSUE-ID] 簡短描述
```
