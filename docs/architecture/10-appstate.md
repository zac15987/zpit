# 10. AppState 與多客戶端架構

---

## 10.1 AppState 分離

`AppState`（`internal/tui/appstate.go`）持有所有共享可變狀態。多個 `tea.Program` 實例共享同一個 `*AppState`：

```
AppState (shared, one instance)
  ├── cfg, env, clients, notifier, logger     (read-only after init)
  ├── projects                                (read-only after init)
  ├── clarifierMD, reviewerMD, ...            (read-only, embedded templates)
  ├── hookScripts                             (read-only)
  ├── activeTerminals                         (mutable)
  ├── loops, wtManager                        (mutable)
  ├── lastLivenessCheck                       (mutable)
  ├── lastPermissionCheck                     (mutable)
  └── lastSessionScan                         (mutable)
```

- `NewAppState()` 接管初始化邏輯（logger 建立、tracker client 建立、map 初始化）
- `NewModel(appState)` 只設定 viewport 和 keymap
- `main.go` 先建立 `appState`，再傳給 `NewModel(appState)` 或 SSH session handler

---

## 10.2 SSH Server Mode（Wish）

三個子命令透過 `os.Args` routing：

```
zpit           → runLocalTUI()     # 本機 TUI
zpit serve     → runServe()        # 無頭 SSH daemon
zpit connect   → runConnect()      # 便利包裝: ssh localhost -p <port>
```

**架構圖：**

```
┌─────────────────────────────────────────────────────────┐
│  zpit serve (headless)                                  │
│                                                         │
│  AppState ─────────────────────────────────────────┐    │
│    (shared, one instance)                          │    │
│                                                    │    │
│  Wish SSH Server (charmbracelet/wish v1.4.7)       │    │
│    │                                               │    │
│    ├── SSH Client A ──► tea.Program ──► Model A    │    │
│    │   (isRemote=true)    (alt screen)   ├─ cursor │    │
│    │                                     ├─ viewport│   │
│    │                                     └─ overlays│   │
│    │                                               │    │
│    ├── SSH Client B ──► tea.Program ──► Model B    │    │
│    │   (isRemote=true)                             │    │
│    │                                               │    │
│    └── (each session: NewModelWithState(state,true))│   │
│                                                    │    │
│  RunServerInit(state) — 啟動時同步執行一次          │    │
│    ├── session scan (找已跑的 Claude Code)          │    │
│    ├── .gitignore check                            │    │
│    └── provider validation                         │    │
│                                                    │    │
│  Graceful shutdown: SIGINT/SIGTERM → 30s timeout   │    │
└─────────────────────────────────────────────────────────┘
```

**SSH Config（`[ssh]` section in config.toml）：**

```toml
[ssh]
port = 2200                                    # 預設
host = "0.0.0.0"
host_key_path = "~/.zpit/ssh/host_ed25519"     # 支援 ~/ 展開
password_env = "ZPIT_SSH_PASSWORD"              # env var 名稱，選配
authorized_keys_path = "~/.ssh/authorized_keys" # 選配
```

**認證機制：**
- Public key auth: 讀取 `authorized_keys_path` 檔案
- Password auth: 從 `password_env` 指向的環境變數讀取密碼
- 兩者至少啟用一個，否則啟動時 hard error

**Remote vs Local session 差異：**

| 行為 | Local (`isRemote=false`) | Remote (`isRemote=true`) |
|------|--------------------------|--------------------------|
| `Init()` | `serverInitCmds()` + `tickCmd()` | `tickCmd()` only |
| Quit (`q`) | 停 watchers + loops + `tea.Quit` | `tea.Quit` only |
| Server init | 在 `Init()` 中執行 | `zpit serve` 啟動時同步執行一次 |

---

## 10.3 多客戶端併發安全

**問題：** 多個 SSH 客戶端（tea.Program）共享同一個 `AppState`，Bubble Tea 的 `Update` 在各自 goroutine 執行，會造成 race condition。

**解決方案：** `sync.RWMutex` + channel-based pub/sub

```
AppState
  ├── mu (sync.RWMutex)     ← 保護 mutable fields
  │     ├── activeTerminals
  │     ├── loops
  │     ├── lastLivenessCheck
  │     ├── lastPermissionCheck
  │     └── lastSessionScan
  │
  └── subMu (sync.Mutex)    ← 保護 subscribers map（獨立於 mu）
        └── subscribers map[int]chan struct{}
```

**兩個獨立的 mutex：**
- `mu`（RWMutex）：保護共享狀態。Write lock 用於 mutation，Read lock 用於讀取。
- `subMu`（Mutex）：保護 subscriber map。獨立於 `mu`，避免 `NotifyAll` 在持有 `mu` 時 deadlock。

**Pub/Sub 廣播機制：**

```
Model A (SSH Client)                   AppState
  │                                      │
  │  Subscribe() ──────────────────────► subscribers[1] = chan(1)
  │                                      │
  │  waitForStateRefresh()               │
  │    └── blocks on subscriberCh        │
  │                                      │
  │                           Model B mutates state
  │                             └── Lock() → mutate → NotifyAll() → Unlock()
  │                                      │
  │  ◄── StateRefreshMsg ───────── ch ◄──┘  (non-blocking send)
  │                                      │
  │  re-render View() (RLock)            │
  │  re-subscribe → waitForStateRefresh  │
```

- `Subscribe()` 回傳 ID + buffered channel (size 1)
- `NotifyAll()` non-blocking send 到所有 subscriber channel，合併快速連續變更
- `Unsubscribe(id)` 在 quit 時清理

**Lock patterns（程式碼規範）：**

| 場景 | Pattern |
|------|---------|
| 讀取 loops/terminals 建立 cmd | `RLock` → 複製到 local vars → `RUnlock` → return cmd closure |
| 修改 loops/terminals | `Lock` → mutate → `NotifyAll` → `Unlock` → create cmds |
| handlers 需要讀+寫 | `Lock` → collect actions into slice → `Unlock` → create cmds（action-defer pattern） |
| View rendering | `RLock` → render → `RUnlock` |
| Read-only fields (`cfg`, `clients`, `env`) | No lock needed（init 後不變） |

**禁止事項：**
- 持有 `mu` 時呼叫會取 `RLock` 的 cmd 方法（會 deadlock）
- cmd closure 中直接引用 `AppState` 的 mutable fields（必須先複製）
