# 10. AppState and Multi-Client Architecture

---

## 10.1 AppState Separation

`AppState` (`internal/tui/appstate.go`) holds all shared mutable state. Multiple `tea.Program` instances share a single `*AppState`:

```
AppState (shared, one instance)
  ├── cfg, env, clients, notifier, logger     (read-only after init)
  ├── projects                                (read-only after init)
  ├── clarifierMD, reviewerMD, ...            (read-only, embedded templates)
  ├── hookScripts                             (read-only)
  ├── activeTerminals                         (mutable)
  ├── loops, wtManager                        (mutable)
  ├── channelEvents, channelSubs              (mutable)
  ├── lastLivenessCheck                       (mutable)
  ├── lastPermissionCheck                     (mutable)
  └── lastSessionScan                         (mutable)
```

- `NewAppState()` owns the initialization logic (logger creation, tracker client creation, map initialization)
- `NewModel(appState)` only sets up viewports and keymaps
- `main.go` creates `appState` first, then passes it to `NewModel(appState)` or the SSH session handler

---

## 10.2 SSH Server Mode (Wish)

Four paths are routed via `os.Args`:

```
zpit           → runLocalTUI()     # local TUI (when auto_serve=false)
zpit           → runAutoServe()    # auto serve + connect (when auto_serve=true)
zpit serve     → runServe()        # headless SSH daemon
zpit connect   → runConnect()      # convenience wrapper: ssh localhost -p <port>
```

**Architecture diagram:**

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
│    │                                     ├─ dock VPs│   │
│    │                                     │  (projects/term/loop/hotkeys) │
│    │                                     ├─ viewport│   │
│    │                                     │  (Status/Channel/GitStatus/EditConfig shared) │
│    │                                     └─ overlays│   │
│    │                                               │    │
│    ├── SSH Client B ──► tea.Program ──► Model B    │    │
│    │   (isRemote=true)                             │    │
│    │                                               │    │
│    └── (each session: NewModelWithState(state,true))│   │
│                                                    │    │
│  RunServerInit(state) — runs once synchronously at startup (session.go)│   │
│    ├── session scan (finds already-running Claude Code) │    │
│    ├── .gitignore check                            │    │
│    └── provider validation                         │    │
│                                                    │    │
│  Graceful shutdown: SIGINT/SIGTERM → 30s timeout   │    │
└─────────────────────────────────────────────────────────┘
```

**Auto-serve mode (`auto_serve = true`):**

```
┌─────────────────────────────────────────────────────────┐
│  zpit (auto_serve=true)                                 │
│                                                         │
│  runAutoServe()                                         │
│    ├── AppState (logger → logFile only, no stdout)      │
│    ├── StartServerAsync() → ServerHandle                │
│    │     ├── setup: resolve paths, auth, wish.NewServer │
│    │     ├── RunServerInit() (session scan, providers)  │
│    │     ├── net.Listen("tcp", addr)  ← port ready      │
│    │     └── go srv.Serve(ln)                           │
│    ├── ssh localhost -p <port> (subprocess, blocks)     │
│    │     └── SSH session → NewModelWithState(state,true)│
│    └── on disconnect → handle.Shutdown() → exit         │
└─────────────────────────────────────────────────────────┘
```

Difference from `zpit serve`: in auto_serve mode the server lifecycle is controlled by the local SSH connection — the server shuts down when the connection drops. `zpit serve` runs continuously until it receives SIGINT/SIGTERM. Both modes share `StartServerAsync()` and `ServerHandle`.

**SSH Config (`[ssh]` section in config.toml):**

```toml
[ssh]
port = 2200                                    # default
host = "0.0.0.0"
host_key_path = "~/.zpit/ssh/host_ed25519"     # supports ~/ expansion
password_env = "ZPIT_SSH_PASSWORD"              # env var name, optional
authorized_keys_path = "~/.ssh/authorized_keys" # optional
auto_serve = false                             # when true, zpit auto-starts server + connects
```

**Authentication:**
- Public key auth: reads the `authorized_keys_path` file
- Password auth: reads the password from the environment variable named by `password_env`
- At least one must be enabled; otherwise startup produces a hard error

**Remote vs Local session differences:**

| Behavior | Local (`isRemote=false`) | Remote (`isRemote=true`) |
|------|--------------------------|--------------------------|
| `Init()` | `serverInitCmds()` + `tickCmd()` (both in session.go) | `tickCmd()` only |
| Quit (`q`) | stops watchers + loops + `tea.Quit` | `tea.Quit` only |
| Server init | executed inside `Init()` | runs once synchronously at `zpit serve` / `runAutoServe` startup |

> **Note:** SSH sessions under auto_serve mode are also `isRemote=true` and behave exactly the same as sessions created by `zpit serve`.

---

## 10.3 Multi-Client Concurrency Safety

**Problem:** Multiple SSH clients (`tea.Program`) share a single `AppState`. Bubble Tea's `Update` runs in each client's own goroutine, which creates race conditions.

**Solution:** `sync.RWMutex` + channel-based pub/sub

```
AppState
  ├── mu (sync.RWMutex)     ← protects mutable fields
  │     ├── activeTerminals
  │     ├── loops
  │     ├── channelEvents
  │     ├── channelSubs
  │     ├── lastLivenessCheck
  │     ├── lastPermissionCheck
  │     └── lastSessionScan
  │
  └── subMu (sync.Mutex)    ← protects subscribers map (independent of mu)
        └── subscribers map[int]chan struct{}
```

**Two independent mutexes:**
- `mu` (RWMutex): protects shared state. Write lock for mutations, read lock for reads.
- `subMu` (Mutex): protects the subscriber map. Independent of `mu` to avoid deadlock when `NotifyAll` is called while `mu` is held.

**Pub/Sub broadcast mechanism:**

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

- `Subscribe()` returns an ID + buffered channel (size 1)
- `NotifyAll()` sends non-blocking to all subscriber channels, coalescing rapid successive changes
- `Unsubscribe(id)` cleans up on quit

**Lock patterns (code conventions):**

| Scenario | Pattern |
|------|---------|
| Read loops/terminals to build a cmd | `RLock` → copy to local vars → `RUnlock` → return cmd closure |
| Mutate loops/terminals | `Lock` → mutate → `NotifyAll` → `Unlock` → create cmds |
| Handler needs both read and write | `Lock` → collect actions into slice → `Unlock` → create cmds (action-defer pattern) |
| View rendering | `RLock` → render → `RUnlock` |
| Read-only fields (`cfg`, `clients`, `env`) | No lock needed (unchanged after init) |

**Prohibited patterns:**
- Calling cmd methods that acquire `RLock` while holding `mu` (causes deadlock)
- Referencing `AppState` mutable fields directly inside cmd closures (must copy first)
