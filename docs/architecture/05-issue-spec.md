# 5. Issue Spec — Agent 間的結構化合約

---

## 5.1 為什麼需要嚴格格式

Issue 是 Clarifier → Coding Agent → Reviewer 之間的**唯一通訊介面**。
三個 agent 不會直接對話，它們只透過 Issue Spec 傳遞意圖。

```
Clarifier ──寫入──▸ Issue Spec ──讀取──▸ Coding Agent
                        │
                        └──讀取──▸ Reviewer（比對 AC 是否達成）
```

如果格式模糊，Coding Agent 可能：
- 搞錯問題是什麼（`CONTEXT` 缺失 → 改錯地方）
- 搞錯要怎麼做（`APPROACH` 缺失 → 自己選一個方案）
- 搞錯做到什麼程度算完成（`ACCEPTANCE_CRITERIA` 模糊 → 少做或多做）
- 搞錯可以碰哪些檔案（`SCOPE` 缺失 → 改到不該改的地方）

因此 Issue Spec 的每個 section 都用 `## SECTION_NAME` 作為明確的 marker，
不允許省略，不允許合併，不允許改名。

---

## 5.2 Issue Spec 格式定義

以下是寫進 Tracker issue body 的完整格式。
Clarifier 產出時必須嚴格遵守，Zpit 讀取時按 `##` 標記解析。

```markdown
## CONTEXT
<!-- 問題現狀：目前的行為是什麼、為什麼有問題 -->
<!-- 必須包含：具體的檔案名稱、方法名稱、行為描述 -->

## APPROACH
<!-- 選定的實作方案：怎麼做、為什麼選這個方案 -->
<!-- 如果比較過多個方案，簡述排除原因 -->

## ACCEPTANCE_CRITERIA
<!-- 每條格式：AC-序號: 具體描述（不允許模糊詞如「適當的」「合理的」） -->
AC-1: ...
AC-2: ...

## SCOPE
<!-- 格式：[modify|create|delete] 檔案路徑 (修改原因) -->
[modify] src/Services/EtherCatService.cs (主要修改)
[modify] src/Alarms/AlarmManager.cs (新增 alarm code)

## CONSTRAINTS
<!-- 實作時的硬性限制 -->
<!-- 沒有限制則寫「無額外限制，遵循 CLAUDE.md」 -->

## REFERENCES
<!-- 可選。相關的參考資料 -->

## BRANCH
<!-- 可選。指定 PR 的 target branch（覆蓋專案預設的 base_branch） -->
dev

## TASKS
<!-- 可選。任務分解，用於大型 issue 的有序執行 -->
<!-- 格式：T{N}: [P] 描述 [action] 檔案路徑 (depends: T{M}, T{N}) -->
T1: [P] Add retry backoff to ReconnectAsync [modify] src/Services/EtherCatService.cs (depends: none)
T2: [P] Add alarm code constant [modify] src/Alarms/AlarmManager.cs (depends: none)
T3: Wire alarm trigger into retry flow [modify] src/Services/EtherCatService.cs (depends: T1, T2)

## COORDINATES_WITH
<!-- 可選。並行協作對象的 issue 編號 -->
#42
#43
```

---

## 5.3 Section 規則總覽

| Section | 必填 | 消費者 | 用途 |
|---------|------|--------|------|
| CONTEXT | ✓ | Coding Agent | 理解問題是什麼 |
| APPROACH | ✓ | Coding Agent | 理解該怎麼做 |
| ACCEPTANCE_CRITERIA | ✓ | Coding Agent + Reviewer | 做到什麼算完成 |
| SCOPE | ✓ | Coding Agent + Hook（路徑守衛） | 限制改動範圍 |
| CONSTRAINTS | ✓ | Coding Agent | 不可違反的限制 |
| REFERENCES | 可選 | Coding Agent | 參考資料 |
| BRANCH | 可選 | Coding Agent + Reviewer | PR target branch（覆蓋專案預設） |
| TASKS | 可選 | Coding Agent | 大型 issue 的任務分解與執行順序 |
| COORDINATES_WITH | 可選 | Coding Agent | 並行協作對象（觸發 channel 協調協議） |

**TASKS section 格式規則：**
- `T{N}:` — 任務 ID（T 加數字）
- `[P]` — 平行標記。連續多個 task 擁有相同 dependency set 且修改不同檔案時，全部標記 `[P]`；引擎將連續 `[P]` task 合為一組平行批次
- `[modify|create|delete] path` — 涉及的檔案（可多個）
- `(depends: T{M}, ...)` — 相依關係；`(depends: none)` 表示無相依
- TASKS 中的檔案路徑會與 SCOPE 交叉驗證

**COORDINATES_WITH section 格式規則：**
- 每行 `#N`，N 為並行協作對象的 issue 編號
- 非阻塞：Loop 引擎不對 COORDINATES_WITH 做任何等待（與 DEPENDS_ON 的串行阻塞相反）
- 純 prompt 層信號：存在時觸發 Dependency Coordination Protocol（見 12-channel.md）
- 與 DEPENDS_ON 可共存——語意不同（DEPENDS_ON = 串行阻塞，COORDINATES_WITH = 並行協調）
- 驗證：非 `#N` 格式的行產生 warning

**格式執行規則：**
- Clarifier 產出的 issue body 必須包含所有必填 section
- Section 標題用 `## SECTION_NAME`（全大寫英文），不允許改名或翻譯
- Clarifier 透過 MCP tools 直接推上 Tracker，推送前必須先讓使用者在終端中確認內容
- Zpit 的 `[s]` status 畫面從 Tracker 拉取 issue，可驗證格式

---

## 5.4 Issue Spec 驗證

實作位於 `internal/tracker/issuespec.go`。

驗證分兩個層級：
- **Errors**（硬阻擋）：Loop 引擎拒絕執行
- **Warnings**（軟提示）：TUI 顯示，Loop 仍會執行

```go
type ValidationResult struct {
    Errors   []string
    Warnings []string
}

func ValidateIssueSpec(body string) ValidationResult
```

**Error 檢查項：**
- 缺少必填 section（CONTEXT, APPROACH, ACCEPTANCE_CRITERIA, SCOPE, CONSTRAINTS）
- `[UNRESOLVED: ...]` marker 未解決
- SCOPE 格式錯誤（缺少 `[modify]`/`[create]`/`[delete]` 前綴、未關閉的括號、無效 action）

**Warning 檢查項：**
- AC 中出現模糊詞（"appropriate", "reasonable", "sufficient", "when necessary"）
- AC 編號有間隙（例如 AC-1, AC-3 但缺 AC-2）
- SCOPE 列出的檔案未被任何 AC 提及
- TASKS 中的檔案路徑未出現在 SCOPE 中
- COORDINATES_WITH 中非 `#N` 格式的行

**解析函數：**

```go
func ParseIssueSpec(body string) (*IssueSpec, error)
```

解析 `## SECTION_NAME` 標記，回傳結構化的 `IssueSpec`（含 Context、Approach、AcceptanceCriteria、Scope、Constraints、References、Branch、Tasks、DependsOn、CoordinatesWith）。

---

## 5.5 Coding Agent Prompt 模板

Loop 啟動 coding agent 時，由 `BuildCodingPrompt()` 組裝 prompt（`internal/prompt/coding.go`）。
Issue Spec 的每個 section 注入到明確的位置，coding agent 不需要自己解析。

**Prompt 結構：**

1. 語言指示（由 `locale.ResponseInstruction()` 注入）
2. Issue ID + 標題
3. **Problem to Solve** ← CONTEXT
4. **Implementation Approach** ← APPROACH
5. **Acceptance Criteria** ← AC 列表
6. **Allowed File Scope** ← SCOPE（超出範圍必須停下問使用者）
7. **Constraints** ← CONSTRAINTS
8. **References** ← REFERENCES（可選）
9. **Logging Policy** ← 依 project 的 `log_policy` 生成文字
10. **Task Decomposition** ← TASKS（可選，有 TASKS 時切換為任務導向工作流）
11. **Your Workflow** — 工作流程步驟（讀 CLAUDE.md → 讀 tracker.md → 讀 guidelines → 實作 → 自檢 → commit → label 更新 → 開 PR）
12. **When to Stop and Ask** — 需要停下問使用者的情境
13. **Tracker Operation Notes** — MCP/API 操作提示

**有 TASKS 時的差異：**
- 額外注入 **Task Decomposition** 和 **Execution Strategy** sections
- Coding agent 作為 **orchestrator**，不自行實作——將每個 task 委派給 `task-runner` subagent（context 隔離）
- 循序 task（無 `[P]`）：透過 Agent tool 依序委派給 `task-runner` subagent
- 平行 task（有 `[P]`）：建立 Agent Team，每個 `[P]` task 分配一個 teammate（使用 `task-runner` subagent type）
- 混合場景：按 dependency order 排列——循序用 subagent、平行群組用 Agent Team
- Commit 格式：`[ISSUE-ID] T{N}: {描述}`
- 每個 subagent 完成後 orchestrator 驗證 commit，失敗重試一次，仍失敗則停下通知（不開 PR）
- 所有 tasks 完成後，orchestrator 執行完整 ACCEPTANCE_CRITERIA 自我檢查再開 PR

---

## 5.6 Reviewer 驗收模板

由 `BuildReviewerPrompt()` 組裝（`internal/prompt/reviewer.go`）。
支援兩種模式：首次 review 和 revision review。

**首次 Review（ReviewRound == 0）：**

1. 語言指示
2. Issue ID + 標題
3. **Original Requirements** ← CONTEXT
4. **Expected Approach** ← APPROACH
5. **Acceptance Criteria** ← AC 列表（逐條 PASS / FAIL）
6. **Allowed File Scope** ← SCOPE
7. **Constraints** ← CONSTRAINTS
8. **Logging Policy**
9. **Your Review Process** — 讀 CLAUDE.md → 讀 issue/PR comments → `git diff base...HEAD` → 逐條驗 AC → 檢查 SCOPE 越界 → 驗 PR target branch → 檢查 CONSTRAINTS → 檢查 logging → 讀 code-construction-principles → 產出 Report
10. **Verdict**：任何 AC ❌ 或 SCOPE/CONSTRAINTS 違反 → NEEDS CHANGES；全部 ✅ → PASS
11. **Label 更新**：PASS → remove "review" add "ai-review"；NEEDS CHANGES → remove "review" add "needs-changes"

**Revision Review（ReviewRound > 0）：**

聚焦在差異而非全面重新 review：
1. 讀取前次 MUST FIX (🔴) items
2. 只看 revision commits 的 delta
3. 逐條驗證前次 MUST FIX 是否修復
4. Spot-check 全 diff 確認無 regression
5. 產出 Revision Review Report

---

## 5.7 Revision Coding Prompt 模板

由 `BuildRevisionPrompt()` 組裝（`internal/prompt/revision.go`）。
當 reviewer 判定 NEEDS CHANGES 且未超過 `max_review_rounds` 時啟動。

**與首次 coding prompt 的差異：**
- 明確標注是修正輪（round N）
- Workflow 先讀 PR 上的 Review Report，列出 MUST FIX items
- 讀不懂 reviewer feedback 時必須停下問使用者
- 認為 reviewer feedback 不正確時也要停下，不盲從
- Commit 格式：`[ISSUE-ID] fix: {描述}`
- Label 更新：修正前 remove "needs-changes" add "wip"，修正後 remove "wip" add "review"
