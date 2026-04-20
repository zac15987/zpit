# 6. Agent 定義與 i18n

---

## 6.1 Clarifier Agent (.claude/agents/clarifier.md)

**Deployment:** go:embed 嵌入 Zpit binary，部署到每個專案的 `.claude/agents/`。
模板內容相同 — 專案特定上下文來自 CLAUDE.md（agent 啟動時自動讀取）。

```yaml
name: clarifier
description: Requirements clarification and technical advisor
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
disallowedTools: Edit
```

**核心行為：**
- 逐條比對 ACCEPTANCE_CRITERIA：✅ / ❌ / ⚠️
- 檢查 SCOPE 越界和 CONSTRAINTS 合規
- 驗證 PR target branch 是否匹配預期的 base branch
- 依 `code-construction-principles.md` 抽樣檢查 code 品質（**所有違反一律標 🔴**）
- 產出 Review Report（嚴重度標記：🔴 MUST FIX / 🟡 SUGGEST / 🟢 NICE）
- 將 report 寫到 PR comment 和 issue comment
- 設定判定 label：ai-review (PASS) 或 needs-changes (NEEDS CHANGES)

**嚴重度分級（重要）：**
- 🔴 MUST FIX：AC 未達、CONSTRAINTS 違反、**正確性 bug（功能壞掉、dead code、懸空引用、`void x` 類噪音抑制）**、code-construction-principles 違反、任何需要後續 PR 才能清掉的技術債
- 🟡 SUGGEST：**僅限真正的風格/品味偏好**（等價重構、命名替代、可選抽取）。若為正確性問題一律升為 🔴，不得以「non-blocking / minor / nit」為由放行
- 🟢 NICE：做得好的地方

**判定規則：**
- 任何 🔴 MUST FIX → NEEDS CHANGES（無論 AC 是否全 ✅）
- 任何 AC ❌ → NEEDS CHANGES
- SCOPE/CONSTRAINTS 違反 → NEEDS CHANGES（無論 AC 狀態）
- 全部 AC ✅、無 🔴、僅有 🟡 → PASS with suggestions
- 全部 AC ✅、無 🔴、無 🟡 → PASS

**設計動機**：原版把 AC 當作唯一正確性判準，導致 `.blink` CSS 未定義、`void TAB_ANCHORS` dead code 這類真實 bug 因「AC 沒寫」被降級為 🟡 放行，累積成技術債。新規則把「正確性」從 AC 解耦，reviewer 遇到明顯壞掉的行為必須 🔴，AC 沉默不構成放行許可。

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

**Parallel Commit Protocol（平行 `[P]` teammate 專用）：**

當 spawn prompt 帶有 `parallel_task_id: T{N}` 行時啟動，解決多個 teammate 共用同一 linked worktree 時共用 staging index 與 `refs/heads/<branch>.lock` 的 race（真實案例與 v1/v2 修復歷程見 `docs/known-issues.md` §2）：

**前提 —** 在 linked worktree 裡 `.git` 是 pointer file 不是 directory，所以所有路徑必須透過 `git rev-parse` 解析，不可 hard-code `.git/...`。整段序列必須在 SINGLE Bash tool 呼叫裡跑完（每個 Bash tool call 都是全新 shell，`export` 不跨呼叫生效）。

1. 解析路徑：`GIT_DIR=$(git rev-parse --git-dir)`、`GIT_COMMON_DIR=$(git rev-parse --git-common-dir)`；定義 `IDX="$GIT_DIR/index.zpit.T{N}"`、`LOCK="$GIT_COMMON_DIR/zpit-commit.lock"`
2. Seed 私有 index：`GIT_INDEX_FILE="$IDX" git read-tree HEAD` — 沒做這步，commit 會把未 stage 的檔案全部記為刪除
3. Stage 指定檔案：`GIT_INDEX_FILE="$IDX" git add -- <declared files only>` — pathspec 強制（hook 也擋 `-A` / `.`）
4. Serialize commit：`mkdir "$LOCK"` 取得鎖（重試 5 次、jittered sleep）→ `GIT_INDEX_FILE="$IDX" git commit ...` → `rmdir "$LOCK"` 釋放
5. 清理：`rm -f "$IDX"`（成功與失敗路徑都做）

循序 task 獨佔 index，不走此協定。完整命令與錯誤處理見 `agents/task-runner.md` → Parallel Commit Protocol。

完整模板見 `agents/task-runner.md`。

---

## 6.4 Efficiency Agent (.claude/agents/efficiency.md)

**Deployment:** go:embed 嵌入 Zpit binary，透過 `[f]` 快捷鍵部署（或 `[d]` 批次 redeploy 時一併寫入）。使用 `deployAndLaunchAgentLite`（不部署 hooks、不設定 ZPIT_AGENT=1）。

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
  → NewAppState(cfg, clarifierMD, reviewerMD, taskRunnerMD, efficiencyMD, guidelinesMD, principlesMD, hookScripts, logWriter)
    → stored in AppState fields
      → DeployHooksToProject()/DeployHooksToWorktree() on every agent launch ([c]/[r]/[l]) or redeploy ([d]) — [f] uses deployAndLaunchAgentLite (no hooks)
        → writes to target project's .claude/hooks/, .claude/agents/, .claude/docs/
        → merges hook config into .claude/settings.json (or settings.local.json for worktrees)
      → loopWriteAgentCmd() deploys task-runner.md when Issue Spec contains TASKS
```

變更 `agents/*.md`、`hooks/*.sh` 或 `docs/agent-guidelines.md` 後需要重新 build 才會生效。

---

## 6.6 Internationalization (i18n)

**雙軌策略**：TUI chrome 可在地化、但 agent 輸出一律英文。

- **TUI 字串**（在地化）：`internal/locale/` package，`T(key)` 查找。`language = "en" | "zh-TW"`（config.toml），翻譯在 `en.go` 和 `zh_tw.go`。新增語言需新的 `locale/{lang}.go` + `SetLanguage()` 的新 case。
- **Agent 輸出**（強制英文）：`locale.ResponseInstruction()` 永遠回傳 non-negotiable 英文規則 — 不因 `language` 切換而改變。規則涵蓋：agent 回話、Issue Spec（title + 所有 sections）、commit message、PR 描述、channel 訊息、tracker labels。使用者可以用任何語言輸入，agent 仍以英文作答。
- **Agent .md 檔案**：語言指示在 deploy time 由 `injectLangInstruction()` 注入（YAML frontmatter 之後）。適用於 `clarifier.md` / `reviewer.md` / `efficiency.md` / `task-runner.md`。
- **Prompt builder**：`BuildCodingPrompt` / `BuildReviewerPrompt` / `BuildRevisionPrompt` 在輸出開頭呼叫 `ResponseInstruction()`。
- **Domain term 例外**：專有名詞若無精確英文對應，agent 允許保留原文於括號，例如 `stocktake (盤點)`。clarifier.md 的 Issue Format 和 Meeting Protocol sections 都有明列此規則。

**設計動機**：CJK 字元在 Claude tokenizer 密度約為英文 2×。clarifier Q&A、coding 實作軌跡、reviewer 留言、channel 訊息這些最長的對話強制英文後，整體 token 消耗顯著下降。TUI chrome 走 `T()` 不經模型，i18n 完全不受影響。

---

## 6.7 Per-Role Model Selection

每個 agent role 在啟動時透過 `--model <id>` 傳給 Claude Code CLI。由 `[agent_models]` 區塊控制（`internal/config/config.go:AgentModelsConfig`）：

```toml
[agent_models]
clarifier = "opus[1m]"      # 需求澄清 — 最深層推理（1M context）
coding = "sonnet[1m]"       # 功能實作（1M context）
reviewer = "sonnet[1m]"     # PR review（1M context）
task_runner = "sonnet[1m]"  # advisory — 由 coding session 繼承
efficiency = "opus[1m]"     # 效能檢視 agent — 深層推理
```

**設計決策**：

- **為什麼 clarifier/efficiency 用 Opus**：需求澄清是品質關鍵點（一次錯全 loop 錯），且整個 issue 生命週期通常只跑一次，成本佔比小；efficiency 檢視則需要對整份 diff 做深層品質判斷，與 clarifier 同屬「單次、高價值」的推理場景。
- **為什麼 coding/reviewer 用 Sonnet**：Sonnet 4.6 在代碼實作與 review 能力足夠，單價約 Opus 的 1/5；loop 可能跑多個 review round，差距在這裡放大。
- **為什麼全部加 `[1m]`**：agent 生命週期內可能吃進整份 Issue Spec + 多檔案 diff + channel 訊息串，1M context tier 消除 context 窗口壓力；API / pay-as-you-go 直連時 1M 無 long-context premium，實質僅增加 token 消耗，不增加單價。
- **為什麼用 alias 而不是 full ID**：Anthropic API 直連情境下，alias（`opus` → 4.7、`sonnet` → 4.6）自動追蹤官方最新版，不需隨著模型更新手動改 config。代價是跨 provider 不一致（Bedrock/Vertex/Foundry 上 `opus` 會解析成 4.6），走這些 provider 的使用者應改用 full ID 覆寫（例如 `claude-opus-4-7[1m]`）。
- **task_runner 目前 advisory**：task-runner subagent 透過 Claude Code Agent tool 由 coding orchestrator spawn，預設繼承父 session 的 model（目前即 Sonnet 4.6 1M），所以 `task_runner` 欄位在 prompt builder 尚未主動使用，保留作為未來 escape hatch（例如想讓平行 `[P]` 任務全跑 Haiku）。

**Wiring**：

- **Manual 啟動**（`[c]` / `[r]` / `[f]` / Enter / `[d]`）：在 `internal/tui/launch.go` 六個 launch function 各讀取對應欄位，注入 `--model` 參數。`deployAndLaunchAgent` 透過 `resolveAgentModel()` helper 依 `agentName` dispatch。
- **Loop 啟動**（coding / reviewer）：在 `internal/tui/loop_cmds.go` 的 `loopLaunchCoderCmd` / `loopWriteAndLaunchReviewerCmd` 讀取對應欄位後注入。
- **Copy-before-closure**：model 值在 closure 外先 copy 到 local（`model := m.state.cfg.AgentModels.X`），符合 AppState 並行存取規範。

**Hot-reload**：`agent_models.*` 列為 hot-reloadable — 修改後下一次啟動 agent 即生效；已運行的 session 沿用啟動時的 model（Claude Code 無法中途改）。

---

## 6.8 CLAUDE.md 模板

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
