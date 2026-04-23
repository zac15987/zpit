# 9. 安全與管控

---

## 9.1 安全設計哲學

```
┌─────────────────────────────────────────────────────────────┐
│  安全防線層次（由軟到硬）                                    │
│                                                             │
│  Layer 1: agent-guidelines.md 行為原則（軟約束）              │
│    → .claude/docs/agent-guidelines.md                       │
│    → agent「應該」怎麼做，靠 LLM 遵守，不可靠但有用           │
│                                                             │
│  Layer 2: --allowedTools 權限（中約束）                       │
│    → agent「能用」哪些 tool                                  │
│    → Claude Code 原生控制                                    │
│                                                             │
│  Layer 3: PreToolUse Hook（硬約束）⬅ 本節重點                │
│    → tool 要執行前的「最後一道閘門」                          │
│    → 即使 bypass all permissions，hook 仍然生效              │
│    → exit 2 = 阻止操作，exit 0 = 放行                       │
│                                                             │
│  Layer 4: Git Worktree 隔離（物理隔離）                      │
│    → 每個 agent 在獨立目錄工作                               │
│    → 但 file system 層面 agent 仍可用絕對路徑逃逸            │
│    → 所以需要 Layer 3 的路徑守衛配合                         │
│                                                             │
│  Layer 5: 最終 merge 閘門（條件式）                          │
│    → auto_merge=false（預設）：人工 PR review，所有改動     │
│      必須你 approve 才進 dev                                 │
│    → auto_merge=true：AI reviewer 的 ai-review PASS 取代    │
│      人工閘門，Zpit 直接呼叫 tracker merge API              │
│    → 啟用 auto_merge 前需評估 reviewer model 對此專案的       │
│      品質是否值得信任                                          │
│                                                             │
│  類比：工控安全                                              │
│  Layer 1 = 操作 SOP → Layer 3 = 軟體安全限位                │
│  Layer 4 = 物理隔離 → Layer 5 = 人工確認                    │
│  bypass all permissions ≠ 拆掉安全限位                       │
│  bypass all permissions = 不用每次按確認鍵，但限位仍生效      │
└─────────────────────────────────────────────────────────────┘
```

---

## 9.2 權限控制

| 角色 | 權限模式 | bypass 模式 | Hook 保護 |
|------|---------|------------|-----------|
| 實作 agent | 無限制（frontmatter 不設 tools） | ✓ 建議開啟 | ✓ 全部 hook |
| Review agent | disallowedTools: Edit | 可開可不開 | ✓ 全部 hook |
| Clarifier agent | disallowedTools: Edit | 可開可不開 | ✓ 全部 hook |
| 你手動介入 | all permissions | ✓ 你自己判斷 | ✓ hook 仍生效 |
| agent teams subagent | 繼承 lead agent | 繼承 | ✓ hook 對 subagent 同樣生效 |

**所有模式下：遇到不確定的技術決策都必須停下來問你。**

---

## 9.3 ZPIT_AGENT 環境變數

Hook 腳本檢查 `ZPIT_AGENT` 環境變數 — 若不存在，直接 `exit 0`（全部放行）。
這確保 hooks 只限制 Zpit 啟動的 agent，不影響你手動開的 Claude Code session。

**注入方式：**
- **Windows (cmd)**：透過 `zpit-env.cmd` wrapper 腳本設定 `ZPIT_AGENT=1`，然後啟動 claude
- **Windows (PowerShell)**：透過 `zpit-env.ps1` wrapper 腳本設定 `ZPIT_AGENT=1`
- **Unix**：inline-prefix 到命令前（`ZPIT_AGENT=1 claude ...`）

`zpit-env.cmd` 和 `zpit-env.ps1` 由 go:embed 嵌入，部署到 `.claude/hooks/`。
`zpit-exit.cmd` 和 `zpit-exit.ps1` 為非 agent 的 Enter 啟動提供自動關閉 WT 分頁功能。

---

## 9.4 Hook 系統設計

### 9.4.1 Hook 架構

```
.claude/
├── settings.json          ← Hook 配置（由 Zpit 動態合併）
├── settings.local.json    ← Worktree 覆蓋（不 commit）
└── hooks/
    ├── path-guard.sh      ← 路徑守衛（Write/Edit/MultiEdit）
    ├── bash-firewall.sh   ← Bash 指令過濾
    ├── git-guard.sh       ← Git 操作守衛
    ├── notify-permission.sh ← 通知 hook（寫信號檔供 TUI 偵測）
    ├── zpit-env.cmd       ← Windows cmd agent 環境變數包裝
    ├── zpit-env.ps1       ← Windows PowerShell agent 環境變數包裝
    ├── zpit-exit.cmd      ← Windows cmd exit wrapper（自動關閉 WT 分頁）
    └── zpit-exit.ps1      ← Windows PowerShell exit wrapper（自動關閉 WT 分頁）
```

Hook 腳本透過 `go:embed` 嵌入 Zpit binary，每次 agent 啟動（`[c]`/`[r]`/`[l]`）或 redeploy（`[d]`）時自動部署。

**部署機制（`internal/worktree/hooks.go`）：**
- Hook 配置以 Go 常數定義（`settingsStrict`、`settingsStandard`、`settingsRelaxed`）
- `DeployHooksToProject()` — 部署到主 repo：寫入 hook 腳本 + 合併 hook 配置到 `.claude/settings.json`（保留既有的 `enabledPlugins` 等設定）
- `DeployHooksToWorktree()` — 部署到 worktree：寫入 hook 腳本 + 根據 hookMode 寫 `.claude/settings.local.json`（strict 模式不寫 overlay，繼承主 repo 設定）

### 9.4.2 settings.json — Hook 註冊格式

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          { "type": "command", "command": ".claude/hooks/path-guard.sh" }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": ".claude/hooks/bash-firewall.sh" },
          { "type": "command", "command": ".claude/hooks/git-guard.sh" }
        ]
      }
    ],
    "Notification": [
      {
        "hooks": [
          { "type": "command", "command": ".claude/hooks/notify-permission.sh" }
        ]
      }
    ]
  }
}
```

**重點：**
- `matcher` 用 regex 匹配 tool 名稱
- 同一個 matcher 可掛多個 hook，依序執行，任一個 exit 2 即阻止
- hook 對 subagent（agent teams）同樣生效
- exit 0 = 放行，exit 2 = 阻止（stderr 訊息回饋給 Claude）
- **絕對不能用 exit 1**（非阻擋性錯誤，action 仍會執行）

### 9.4.3 Hook 1: 路徑守衛 (path-guard.sh)

**目的：** 確保 Write/Edit 只發生在 agent 被分配的 worktree 目錄內。

**邏輯：**
1. 從 stdin JSON 解析 `tool_input.file_path`（或 `.path`/`.file`）
2. 檢查 `ZPIT_AGENT` — 不存在則放行（非 agent session）
3. 相對路徑轉絕對路徑（基於 `CLAUDE_PROJECT_DIR`）
4. **黑名單**（即使在 worktree 內也擋）：`.claude/agents/`、`.claude/settings`、`CLAUDE.md`、`.git/`、`.env`
5. **白名單**：必須在 `CLAUDE_PROJECT_DIR` 內

### 9.4.4 Hook 2: Bash 防火牆 (bash-firewall.sh)

**目的：** 攔截破壞性或危險的 bash 指令。

**攔截類別：**
- 破壞性檔案操作：`rm -rf /`、`rm -rf ..`、`rm -rf ~`
- 系統層級：`chmod 777`、`mkfs`、`dd if=`、`shutdown`、`reboot`
- 網路風險：`curl|bash`、`wget|bash`、`npm publish`、`dotnet nuget push`
- 全域套件安裝：`npm install -g`
- 程序管理：`kill -9 1`、`killall`、`pkill -9`
- **重導向逃逸偵測**：`>` 或 `>>` 指向 worktree 外的絕對路徑

**grep 相容性：** 先嘗試 `-P`（PCRE），不支援則 fallback 到 `-E`（ERE）。

### 9.4.5 Hook 3: Git 操作守衛 (git-guard.sh)

**目的：** 限制 agent 的 git 操作範圍。

**Push 白名單機制：**
- Agent 只能 push `feat/*` branch（開 PR 需要）
- Force push 一律阻擋
- 非 `feat/*` 的 push 一律阻擋（包括裸 `git push`）

**其他攔截項：**
- `git reset --hard`、`git clean -fd`
- `git checkout main|master|develop`
- `git branch -d/-D`（由 Zpit 管理）
- `git merge`、`git rebase`、`git tag`
- `git remote add|set-url|remove`
- `git stash drop`
- `git add -A`、`git add .`（防止 stage 不該 commit 的檔案）

**允許的 git 操作：** `git add <specific-file>`、`git commit`、`git status`、`git diff`、`git log`、`git push feat/*`

### 9.4.6 Hook 4: 通知 hook (notify-permission.sh)

非安全 hook。當 Claude Code 需要 tool 權限時觸發，寫入信號檔供 TUI 偵測。

---

## 9.5 Hook 防護等級 (Per Project)

```toml
[[projects]]
hook_mode = "strict"   # strict | standard | relaxed
```

| 等級 | path-guard | bash-firewall | git-guard | notify-permission |
|------|-----------|---------------|-----------|-------------------|
| strict | ✓ | ✓ | ✓ | ✓ |
| standard | ✓ | — | ✓ | ✓ |
| relaxed | — | — | ✓ | ✓ |

建議：機台專案用 strict，桌面工具用 standard，個人網頁用 relaxed。

---

## 9.6 Hook 測試

自動化測試在 `hooks/hooks_test.go`（`make test-hooks`），涵蓋所有阻擋規則。

手動測試範例：

```bash
# path-guard: 應該被阻擋（路徑在 worktree 外）
echo '{"tool_input":{"file_path":"/etc/passwd"}}' | \
  CLAUDE_PROJECT_DIR="/tmp/worktree" ZPIT_AGENT=1 \
  bash .claude/hooks/path-guard.sh
echo $?   # 應該是 2

# git-guard: push feat/* 放行
echo '{"tool_input":{"command":"git push origin feat/ASE-47-fix"}}' | \
  ZPIT_AGENT=1 bash .claude/hooks/git-guard.sh
echo $?   # 應該是 0

# git-guard: push main 阻擋
echo '{"tool_input":{"command":"git push origin main"}}' | \
  ZPIT_AGENT=1 bash .claude/hooks/git-guard.sh
echo $?   # 應該是 2
```

---

## 9.7 Git 安全

- Agent 永遠在 worktree + feature branch 上工作，絕不直接操作主 repo
- 每個 agent 的 Claude Code 工作目錄是 worktree 路徑，不是主 repo
- Git 危險操作由 git-guard.sh 硬性攔截
- PR merge 策略由 per-project `auto_merge` 決定（預設 false 需人工 approve；true 時由 AI reviewer PASS 驅動 tracker merge API，不走 agent 的 push/push hook 路徑）
- PR merge 後自動清理 worktree + branch

## 9.8 Loop 安全

- Agent 在可見終端中運行，使用者可隨時切過去介入（天然安全閥）
- `max_per_project` 限制每個專案同時 worktree 數量
- agent 等待回應超過 `re_remind_minutes`（預設 2 分鐘）→ TUI 再次發送提醒通知

---

## 9.9 Auto-Merge 的安全 trade-off

當 `auto_merge = true` 時，Layer 5 的最後一道人工關卡被 AI reviewer 的 PASS 判斷取代。這是**刻意的信任轉移**，不是漏洞 — 只有你自己決定這條 trade-off 值得時才應該開啟。

**技術事實：**
- merge API 由 Go 程式呼叫，不是 agent 的 `git push`，所以 **不經過 `git-guard.sh`**。這是正確的 — git-guard 防的是 agent 意外 push 到 main/master/develop/dev，而 auto-merge 是 Zpit 程式主動行為，不屬於 agent 行為。
- merge 失敗時（permanent/transient exhausted）slot 進入 `SlotNeedsHuman`，worktree 和 branch 都保留供你處理。Auth 錯誤進入 `SlotError`。
- 重試只針對 transient 錯誤（5xx / 408 / 429 / 網路 timeout），permanent 錯誤（409 / 405 / 422）立刻跳出不重試。

**建議：**
- 公用 fork / 公司專案：`auto_merge = false`（預設）。
- 個人實驗專案、私人 repo：可考慮 `auto_merge = true`，但先觀察 reviewer 品質（例如連續 10 個 issue 的 review 都合理再開啟）。
- **絕對不要**在不信任的專案上啟用，或者在 reviewer model 經常誤判的情境下啟用。
