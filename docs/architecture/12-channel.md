# 12. 跨 Agent Channel 通訊

---

## 12.1 設計動機

當多個 agent 同時處理相關 issue（例如同專案的前後端、或跨專案的共用模組），
需要即時交換 artifact（interface 定義、type spec）和訊息。

Channel 系統讓 agent 不需要透過 issue tracker 或檔案系統間接溝通，
而是透過 HTTP broker + MCP stdio server 建立即時通道。

---

## 12.2 整體架構

```
Agent A (Project X, Issue #3)       Agent B (Project Y, Issue #7)
  └─ MCP stdio server                └─ MCP stdio server
       │ (zpit serve-channel)              │ (zpit serve-channel)
       │                                   │
       ├─ publish_artifact ─► HTTP POST    ├─ send_message ─► HTTP POST
       ├─ list_artifacts   ─► HTTP GET     ├─ list_projects ─► HTTP GET
       └─ SSE listener     ◄─ streaming    └─ SSE listener  ◄─ streaming
                     │                              │
                     ▼                              ▼
     ┌──────────────────────────────────────────────────┐
     │ Broker (HTTP on 127.0.0.1:<broker_port>)         │
     │                                                  │
     │  REST endpoints:                                 │
     │  ├─ POST /api/artifacts/{project}/{issue_id}     │
     │  ├─ GET  /api/artifacts/{project}                │
     │  ├─ POST /api/messages/{project}/{to}            │
     │  ├─ GET  /api/messages/{project}/{issue_id}      │
     │  ├─ GET  /api/events/{project}  (SSE)            │
     │  └─ GET  /api/projects          (discovery)      │
     │                                                  │
     │  EventBus (in-memory pub/sub, keyed by project)  │
     └──────────────────────────────────────────────────┘
                     │
                     ▼
     TUI: AppState.channelEvents → ViewChannel ([m] key)
```

---

## 12.3 Broker（HTTP 事件中樞）

實作位於 `internal/broker/`。

**特性：**
- 綁定 `127.0.0.1:<broker_port>`（預設 17731），僅限本機存取
- In-memory 儲存（artifacts、messages），不持久化
- Non-blocking publish：使用 buffered channel，滿時丟棄（避免慢 subscriber 拖住 broker）
- SSE 連線計數：per-project 追蹤活躍 SSE 連線數，供 `/api/projects` 回傳
- 在 `NewAppState()` 中啟動，僅當任一 project 有 `channel_enabled = true`

**EventBus：**

```go
type EventBus interface {
    Subscribe(project string) <-chan Event
    Unsubscribe(project string, ch <-chan Event)
}

type Event struct {
    Type    string          // "artifact" or "message"
    Payload json.RawMessage // JSON-encoded Artifact or Message
}
```

**Artifact / Message struct** 皆包含 `AgentName string` 欄位（json tag: `agent_name,omitempty`），
由 MCP server 在 HTTP POST 時帶入。用於在 TUI Channel view 中識別不同 agent 的發言。
命名格式：手動啟動 `{type}-{4碼hex}`（如 `clarifier-a3f7`），Loop 啟動 `{role}-#{issueID}`（如 `coding-#42`）。

- 以 project key 為分組，每個 project 獨立的 subscriber set
- `_global` 和跨專案 key 在 EventBus 中是普通的 project key，無特殊邏輯

---

## 12.4 MCP Stdio Server（Agent 端橋接）

實作位於 `internal/mcp/`。入口：`zpit serve-channel` 子命令。

每個 agent 啟動時，Claude Code 透過 `.mcp.json` 設定自動啟動一個 MCP stdio server。
Server 從環境變數讀取設定：

| 環境變數 | 說明 |
|----------|------|
| `ZPIT_BROKER_URL` | Broker HTTP 位址 |
| `ZPIT_PROJECT_ID` | 所屬 project ID |
| `ZPIT_ISSUE_ID` | 處理中的 issue ID |
| `ZPIT_LISTEN_PROJECTS` | 額外訂閱的 project key（逗號分隔） |
| `ZPIT_AGENT_NAME` | Agent 顯示名稱（optional，如 `clarifier-a3f7`） |

**提供的 MCP Tools：**

| Tool | 說明 |
|------|------|
| `publish_artifact` | 發布 artifact 到 broker，HTTP body 帶入 agent_name |
| `list_artifacts` | 列出指定 project 的所有 artifact |
| `send_message` | 發送訊息給指定 agent，HTTP body 帶入 agent_name |
| `list_projects` | 列出所有活躍 project 及其 agent 連線數 |
| `subscribe_project` | 動態訂閱指定 project 的 SSE 事件串流 |
| `unsubscribe_project` | 取消訂閱指定 project 的 SSE 事件串流（不可取消訂閱自身 project） |
| `list_subscriptions` | 列出目前所有已訂閱 SSE 的 project key |

**SSE 監聽：**
- 啟動時對自身 project + `ListenProjects` 各啟動一個 SSE listener goroutine
- 透過 per-instance UUID 過濾自身發送的事件（self-echo filtering）
- 收到事件時以 JSON-RPC notification 推送到 agent 的 stdin

---

## 12.5 跨專案通訊模型

Agent 透過 `target_project` 參數選擇通訊範圍：

| `target_project` | `to` | 效果 |
|---|---|---|
| 省略（預設） | `"3"` | 同專案，指定 issue |
| `"project-a"` | `"5"` | 跨專案，指定 issue |
| `"project-a"` | `"_project"` | 廣播給目標專案所有 agent |
| `"_global"` | `"_all"` | 全域廣播給所有監聽中的 agent |

**範例流程：**

```
Project X / Issue #3 的 agent 定義了一個 interface：
  → publish_artifact(issue_id="3", type="interface", content="...")
  → Broker 存入 artifacts["project-x"]，透過 EventBus 發布到 "project-x" 的 subscriber

Project Y / Issue #7 的 agent 需要該 interface：
  → list_artifacts(project="project-x")
  → Broker 回傳 project-x 的所有 artifact

跨專案訊息：
  → send_message(to_issue_id="7", content="...", target_project="project-y")
  → Broker POST /api/messages/project-y/7
  → Project Y 的 SSE listener 收到事件 → 推送到 agent
```

---

## 12.6 TUI 整合

**訂閱機制：**
- Loop 啟動 / 手動 launch 時呼叫 `channelSubscribeCmd()`
- 訂閱範圍：自身 project + 每個 `channel_listen` 項目
- `channelReadNextCmd()` 在 EventBus channel 上阻塞
- 收到事件 → `ChannelEventMsg` → 存入 `AppState.channelEvents[projectID]`
- Loop 停止時 unsubscribe 所有相關 channel

**Channel View（[m] key）：**
- 合併自身 project 與 `channel_listen` 的事件，按時間排序
- 跨專案事件標記 `[source]` tag
- Viewport 支援滾動瀏覽

---

## 12.7 設定

```toml
# 全域
broker_port = 17731          # Broker HTTP 埠
zpit_bin = "/path/to/zpit"   # 明確指定 binary 路徑（用於 .mcp.json 生成）

# Per-project
[[projects]]
channel_enabled = true                      # 啟用 channel
channel_listen = ["_global", "other-proj"]  # 額外訂閱的 project key
```

- `channel_enabled`：per-project 開關，關閉時該 project 的 agent 不啟動 MCP server
- `channel_listen`：該 project 的 agent 除了自身 project 外，額外訂閱的 key
- `broker_port`：全域設定，所有 project 共用同一個 broker
- `zpit_bin`：用於生成 `.mcp.json` 中的 `command` 路徑

---

## 12.8 Dependency Coordination Protocol（依賴協調協議）

當 Issue Spec 包含 `## COORDINATES_WITH` section 時，coding agent 的 prompt 會注入 Dependency Coordination Protocol。
此協議解決並行 agent 之間的 artifact 依賴問題——在 Claude Code 的 single-turn 執行模型下，agent 無法在執行中途暫停等待外部信號。

### 觸發條件

- `channel_enabled = true`（專案層級）
- Issue Spec 中存在 `## COORDINATES_WITH` section（列出並行協作對象的 issue 編號）
- 兩者缺一不觸發（ChannelEnabled=false 時不注入任何 channel section）

### 協議流程

```
Agent 啟動
  │
  ├─ 1. 啟動探查 (Startup Probe)
  │     ├─ list_artifacts — 檢查已發布的 artifact
  │     ├─ list_projects — 探索活躍 agent
  │     └─ send_message → COORDINATES_WITH 中的每個 issue
  │         （宣告自己計劃定義/消費的 interface）
  │
  ├─ 2. 假設標記 (Assumption Marking)
  │     └─ 需要的 artifact 尚不可用時：
  │         ├─ 以最佳推測繼續實作
  │         └─ 標記 // [CHANNEL_ASSUMPTION] <描述, pending artifact from #N>
  │
  ├─ 3. 驗證與清理 (Verification & Cleanup)
  │     └─ 收到 channel notification 時：
  │         ├─ 搜尋相關 [CHANNEL_ASSUMPTION] comment
  │         ├─ 比對 artifact
  │         ├─ 一致 → 刪除 comment
  │         └─ 不一致 → 修正實作，再刪除 comment
  │
  ├─ 4. 發布義務 (Publish Obligation)
  │     └─ 定義完 interface/type/schema 後立即 publish_artifact
  │
  └─ 5. Review Gate（轉 review 前的閘門）
        ├─ 搜尋所有 [CHANNEL_ASSUMPTION] comment
        ├─ 若有未解決：list_artifacts + send_message（最多 3 次累計嘗試）
        ├─ 3 次後仍有未解決 → post issue comment，等待使用者決定
        └─ 全部解決 → 加 "review" label
```

### 與 DEPENDS_ON 的區別

| | DEPENDS_ON | COORDINATES_WITH |
|---|---|---|
| 層級 | Loop 引擎（基礎設施層） | Prompt（指示層） |
| 行為 | 串行阻塞——依賴 issue closed 才開始 | 非阻塞——並行執行，channel 協調 |
| 時機 | Agent 啟動前（Loop 等待） | Agent 執行中（prompt 指引） |
| 適用場景 | A 的輸出是 B 的前提 | A 和 B 同時跑、共享 interface |

### 設計考量

**為何不用阻塞式 tool？**
Claude Code 的執行模型是 single-turn request-response。Channel 推送只在 turns 之間（agent idle 時）注入。
`wait_for_artifact` 之類的阻塞式 MCP tool call 會凍結 agent——mid-turn 暫停在當前架構下不可行。
因此採用 prompt 層的假設標記 + 事後驗證策略。

**假設標記的容錯性：**
- 最佳情況：artifact 在 agent 實作期間抵達，agent 收到 notification 後即時驗證
- 一般情況：artifact 在 review gate 前抵達，3 次嘗試內解決
- 最差情況：artifact 始終未抵達，agent 停下等待使用者介入（不產出錯誤的 PR）

---

## 12.9 動態訂閱管理

MCP Server 支援在 runtime 動態增減 SSE 訂閱，讓 agent 在對話中即時加入或退出跨專案頻道。

**架構：**
- `Server` struct 以 `sseContexts map[string]context.CancelFunc` 管理 per-project SSE goroutine
- 每個訂閱有獨立的 `context.WithCancel`，可單獨取消而不影響其他訂閱
- `sseMu sync.Mutex` 保護所有對 `sseContexts` 的讀寫操作

**工具：**

| Tool | 參數 | 行為 |
|------|------|------|
| `subscribe_project` | `project` (required) | 檢查是否已訂閱 → 否則建立新 context + 啟動 `listenSSE` goroutine |
| `unsubscribe_project` | `project` (required) | 檢查是否訂閱中 → 是則 cancel context + 從 map 移除。禁止取消訂閱自身 project |
| `list_subscriptions` | 無 | 回傳 JSON 陣列，包含所有已訂閱的 project key（按字母排序） |

**使用情境：**
- Agent 在會議模式中需要加入跨專案頻道拉取其他專案的 agent 進討論
- 跨專案協作結束後退出該頻道，減少不必要的事件推送
- 初始訂閱（config.toml `channel_listen`）在啟動時自動建立，runtime 動態訂閱為額外擴展

**向後相容：** 初始訂閱行為不變，`channel_listen` 設定仍在啟動時生效。新 tools 僅提供 runtime 的額外控制能力。

---

## 12.10 會議模式（Meeting Protocol）

### 概述

當使用者對同一專案多次按下 `[c]` 啟動多個 clarifier agent 時，這些 agent 可透過 channel 發現彼此並進入**會議模式**——各自代表不同視角進行辯論、交換觀點，最終將需求收斂為結構化的 Issue Spec。

會議模式的核心理念是：多個 clarifier 各自與使用者對話，同時透過 channel 共享彼此蒐集到的需求資訊。透過辯論協議，agent 之間會針對矛盾點交鋒，避免無條件附和，從而產出更完整、更嚴謹的需求規格。

### 觸發條件

會議模式在以下**兩個條件同時成立**時觸發：

1. **Channel tools 可用**：`.mcp.json` 已部署且 MCP server 處於活躍狀態
2. **發現其他 clarifier agent**：透過 `list_projects` 看到同專案有其他 SSE 連線，或收到來自另一個 clarifier 的 channel 訊息

任一條件不滿足時，clarifier 以**單一 agent 模式**運作——即原本的 clarifier 工作流程，不執行任何會議協議步驟。

### 流程圖

```
Clarifier A                        Clarifier B
  │                                  │
  ├─ 1. 啟動探查                      ├─ 1. 啟動探查
  │    list_projects                  │    list_projects
  │    send_message [加入會議]         │    send_message [加入會議]
  │                                  │
  ├─ 2. 轉發協議                      │
  │    使用者發言                      │
  │    send_message                   │
  │    [轉述使用者] {摘要}      ───►   │  收到 → 展示 + 回應
  │                                  │
  │                                  ├─ 3. 辯論協議
  │  ◄─── [{AgentName}] {觀點}        │    send_message
  │  展示 + 回應                       │
  │                                  │
  ├─ 4. 收斂協議                      │
  │    使用者說「收斂」                 │
  │    send_message                   │
  │    [收斂確認] {共識}        ───►   │  回覆補充
  │  ◄─── 補充                        │
  │    整合 → Issue Spec              │
  └─────────────────                  └─────────────────
```

### 四階段詳述

#### 階段 1：啟動探查（Startup Probe）

Agent 在讀取 codebase 後（clarifier 工作流程步驟 4）呼叫 `list_projects`。若同專案存在其他活躍 agent，則透過 `send_message(to_issue_id="_project")` 廣播：

```
[加入會議] 我是 {AgentName}，加入需求討論
```

若未發現其他 agent，跳過會議協議，以單一 agent 模式繼續。

#### 階段 2：轉發協議（Relay Protocol）

當使用者提供與需求相關的資訊時，agent 透過 `send_message(to_issue_id="_project")` 廣播：

```
[轉述使用者] {一句話摘要}
```

**僅轉發需求相關內容。** 閒聊、操作指令等非需求性對話不轉發。這確保 channel 中的訊息密度保持在有意義的水準。

#### 階段 3：辯論協議（Debate Protocol）

收到其他 agent 的訊息時：

1. 向使用者摘要展示收到的內容
2. 表達自身觀點——基於已蒐集到的需求資訊做出判斷
3. 若有不同意見，透過 `send_message` 回覆具體的反駁依據

**禁止無條件同意。** Agent 必須基於自身蒐集的資訊獨立判斷。若確實同意，需說明同意的技術理由。

所有 channel 訊息統一使用格式：`[{AgentName}] {觀點內容}`

#### 階段 4：收斂協議（Convergence Protocol）

使用者透過觸發詞發起收斂（例如「收斂」、「整理成 issue」、「wrap up」）。收到觸發後，agent 廣播：

```
[收斂確認] 準備撰寫 Issue Spec，目前共識：{關鍵決策摘要}，有最後要補充的嗎？
```

流程：
1. 廣播收斂確認訊息
2. 等待 30 秒，接收其他 agent 的最後補充
3. 整合所有補充內容
4. 繼續原本的 clarifier 工作流程步驟 13-17（撰寫 Issue Spec、張貼到 issue tracker）

### 與既有 channel 機制的關係

會議模式完全建立在既有的 MCP tools 之上，**不需要新增任何 tool 或 broker 端點**：

| 使用的 tool | 用途 |
|---|---|
| `send_message` | 所有 agent 間通訊（加入會議、轉發、辯論、收斂） |
| `list_projects` | 啟動探查——發現同專案的其他 agent |
| `list_artifacts` | 查詢已發布的需求 artifact（如有） |

通訊一律使用專案層級廣播（`to_issue_id="_project"`），確保同專案的所有 clarifier 都能收到訊息。這與 12.5 節描述的跨專案通訊模型一致——會議模式是同專案廣播的具體應用場景。
