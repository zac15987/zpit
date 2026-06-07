# 12. Cross-Agent Channel Communication

---

## 12.1 Design Motivation

When multiple agents are concurrently handling related issues (for example, frontend and backend within the same project, or a shared module across projects), they need to exchange artifacts (interface definitions, type specs) and messages in real time.

The channel system lets agents communicate directly through an HTTP broker and MCP stdio server, without having to route through the issue tracker or the filesystem.

---

## 12.2 Overall Architecture

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

## 12.3 Broker (HTTP Event Hub)

Implementation lives in `internal/broker/`.

**Characteristics:**
- Binds to `127.0.0.1:<broker_port>` (default 17731); local access only
- In-memory storage (artifacts, messages); no persistence
- Non-blocking publish: uses buffered channels; drops events when full to prevent slow subscribers from stalling the broker
- SSE connection tracking: tracks active SSE connection counts per project per agent type (via `?agent_type=X` query parameter); exposed through the `agents` map returned by `/api/projects`
- Started in `NewAppState()`; only when at least one project has `channel_enabled = true`

**EventBus:**

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

**Artifact / Message structs** both include an `AgentName string` field (json tag: `agent_name,omitempty`), populated by the MCP server in the HTTP POST body. Used to identify which agent produced each entry in the TUI Channel view.
Naming format: manually launched agents use `{type}-{4hex}` (e.g. `clarifier-a3f7`); loop-launched agents use `{role}-#{issueID}` (e.g. `coding-#42`).

- Grouped by project key; each project has its own independent subscriber set
- `_global` and cross-project keys are ordinary project keys in the EventBus; no special-case logic

---

## 12.4 MCP Stdio Server (Agent-Side Bridge)

Implementation lives in `internal/mcp/`. Entry point: `zpit serve-channel` subcommand.

When an agent starts, Claude Code automatically launches an MCP stdio server as configured in `.mcp.json`. The server reads its configuration from environment variables:

| Environment Variable | Description |
|----------|------|
| `ZPIT_BROKER_URL` | Broker HTTP address |
| `ZPIT_PROJECT_ID` | Project ID this agent belongs to |
| `ZPIT_ISSUE_ID` | Issue ID currently being handled |
| `ZPIT_LISTEN_PROJECTS` | Additional project keys to subscribe to (comma-separated) |
| `ZPIT_AGENT_NAME` | Agent display name (optional, e.g. `clarifier-a3f7`) |
| `ZPIT_AGENT_TYPE` | Agent type (optional, e.g. `clarifier`, `coding`, `reviewer`, `efficiency`, `claude`) |

**Provided MCP Tools:**

| Tool | Description |
|------|------|
| `publish_artifact` | Publishes an artifact to the broker; includes `agent_name` in the HTTP body |
| `list_artifacts` | Lists all artifacts for a given project |
| `send_message` | Sends a message to a specified agent; includes `agent_name` in the HTTP body |
| `list_projects` | Lists all active projects and their per-type agent connection counts (`agents` map) |
| `subscribe_project` | Dynamically subscribes to the SSE event stream for a given project |
| `unsubscribe_project` | Unsubscribes from the SSE event stream for a given project (cannot unsubscribe from own project) |
| `list_subscriptions` | Returns all project keys currently subscribed via SSE |

**SSE Listening:**
- At startup, spawns one SSE listener goroutine for each of: own project + each entry in `ListenProjects`
- Self-echo filtering via a per-instance UUID to suppress events the server itself published
- Pushes received events to the agent's stdin as JSON-RPC notifications

---

## 12.5 Cross-Project Communication Model

Agents select the communication scope via the `target_project` parameter:

| `target_project` | `to` | Effect |
|---|---|---|
| omitted (default) | `"3"` | Same project, specific issue |
| `"project-a"` | `"5"` | Cross-project, specific issue |
| `"project-a"` | `"_project"` | Broadcast to all agents in the target project |
| `"_global"` | `"_all"` | Global broadcast to all listening agents |

**Example flow:**

```
Agent for Project X / Issue #3 defines an interface:
  → publish_artifact(issue_id="3", type="interface", content="...")
  → Broker stores it in artifacts["project-x"], publishes to "project-x" subscribers via EventBus

Agent for Project Y / Issue #7 needs that interface:
  → list_artifacts(project="project-x")
  → Broker returns all artifacts for project-x

Cross-project message:
  → send_message(to_issue_id="7", content="...", target_project="project-y")
  → Broker POST /api/messages/project-y/7
  → Project Y's SSE listener receives the event → pushes to agent
```

---

## 12.6 TUI Integration

**Subscription mechanism:**
- `channelSubscribeCmd()` is called on loop start or manual agent launch
- Subscription scope: own project + each `channel_listen` entry
- `channelReadNextCmd()` blocks on the EventBus channel
- Event received → `ChannelEventMsg` → appended to `AppState.channelEvents[projectID]`
- All related channels are unsubscribed when the loop stops

**Channel View ([m] key):**
- Merges events from own project and `channel_listen` entries, sorted by timestamp
- Cross-project events are tagged with a `[source]` tag
- Viewport supports scrolling

**Live TUI Toggle ([e] sub-menu):**
- `[1]` Toggle channel: switches `channel_enabled` on/off and immediately subscribes/unsubscribes the EventBus
  - OFF → ON: if broker is nil, lazy-starts it automatically; then subscribes to own project + `channel_listen` entries
  - ON → OFF: cancels the own project's EventBus channel subscription
  - Result is written back to config.toml via targeted TOML write (does not affect other content)
- `[2]` Edit channel_listen: multi-select list showing all other projects + `_global`
  - On confirm, updates the in-memory config immediately and writes back to config.toml
  - Newly added listen entries are subscribed immediately; removed entries are unsubscribed immediately

---

## 12.7 Configuration

```toml
# Global
broker_port = 17731          # Broker HTTP port
zpit_bin = "/path/to/zpit"   # Explicit binary path (used for .mcp.json generation)

# Per-project
[[projects]]
channel_enabled = true                      # Enable channel
channel_listen = ["_global", "other-proj"]  # Additional project keys to subscribe to
```

- `channel_enabled`: per-project toggle; when off, agents for that project do not start the MCP server
- `channel_listen`: additional project keys an agent subscribes to beyond its own project
- `broker_port`: global setting; all projects share a single broker
- `zpit_bin`: used to generate the `command` path in `.mcp.json`

`channel_enabled` and `channel_listen` can be toggled live from the TUI via the `[e]` sub-menu without restarting. Other channel-related settings (such as `broker_port`) require a restart to take effect.

---

## 12.8 Dependency Coordination Protocol

When an Issue Spec contains a `## COORDINATES_WITH` section, the Dependency Coordination Protocol is injected into the coding agent's prompt. This protocol resolves artifact dependency problems between concurrently running agents — under Claude Code's single-turn execution model, an agent cannot pause mid-run to wait for an external signal.

### Trigger Conditions

- `channel_enabled = true` (project level)
- A `## COORDINATES_WITH` section is present in the Issue Spec (listing the issue numbers of concurrent collaborators)

Both conditions must be met; if `ChannelEnabled=false`, no channel section is injected.

### Protocol Flow

```
Agent starts
  │
  ├─ 1. Startup Probe
  │     ├─ list_artifacts — check already-published artifacts
  │     ├─ list_projects — discover active agents
  │     └─ send_message → each issue listed in COORDINATES_WITH
  │         (declares the interfaces it plans to define or consume)
  │
  ├─ 2. Assumption Marking
  │     └─ when a required artifact is not yet available:
  │         ├─ continue implementation with best-guess assumptions
  │         └─ mark: // [CHANNEL_ASSUMPTION] <description, pending artifact from #N>
  │
  ├─ 3. Verification & Cleanup
  │     └─ upon receiving a channel notification:
  │         ├─ search for relevant [CHANNEL_ASSUMPTION] comments
  │         ├─ compare against artifact
  │         ├─ match → delete comment
  │         └─ mismatch → fix implementation, then delete comment
  │
  ├─ 4. Publish Obligation
  │     └─ publish_artifact immediately after defining an interface/type/schema
  │
  └─ 5. Review Gate (gate before transitioning to review)
        ├─ search all [CHANNEL_ASSUMPTION] comments
        ├─ if unresolved: list_artifacts + send_message (up to 3 cumulative attempts)
        ├─ still unresolved after 3 attempts → post issue comment, wait for user decision
        └─ all resolved → add "review" label
```

### Distinction from DEPENDS_ON

| | DEPENDS_ON | COORDINATES_WITH |
|---|---|---|
| Layer | Loop engine (infrastructure) | Prompt (instruction) |
| Behavior | Serial blocking — waits for dependency issue to close before starting | Non-blocking — runs concurrently, coordinated via channel |
| Timing | Before agent launch (Loop waits) | During agent execution (prompt-guided) |
| Use case | A's output is a prerequisite for B | A and B run simultaneously and share an interface |

### Design Rationale

**Why not a blocking tool?**
Claude Code's execution model is single-turn request-response. Channel pushes are only injected between turns (when the agent is idle). A blocking MCP tool call like `wait_for_artifact` would freeze the agent — mid-turn pausing is not feasible in the current architecture. The assumption-marking + post-hoc verification strategy is used instead.

**Fault tolerance of assumption marking:**
- Best case: artifact arrives while the agent is still implementing; agent verifies immediately upon receiving the notification
- Typical case: artifact arrives before the review gate; resolved within 3 attempts
- Worst case: artifact never arrives; agent stops and waits for user intervention (no incorrect PR is produced)

---

## 12.9 Dynamic Subscription Management

The MCP Server supports adding and removing SSE subscriptions at runtime, letting agents join or leave cross-project channels dynamically during a conversation.

**Architecture:**
- The `Server` struct manages per-project SSE goroutines via `sseContexts map[string]context.CancelFunc`
- Each subscription has its own `context.WithCancel`, allowing individual cancellation without affecting others
- `sseMu sync.Mutex` protects all reads and writes to `sseContexts`

**Tools:**

| Tool | Parameters | Behavior |
|------|------|------|
| `subscribe_project` | `project` (required) | Checks if already subscribed → if not, creates a new context and starts a `listenSSE` goroutine |
| `unsubscribe_project` | `project` (required) | Checks if subscribed → if so, cancels the context and removes it from the map. Cannot unsubscribe from own project |
| `list_subscriptions` | none | Returns a JSON array of all currently subscribed project keys (alphabetically sorted) |

**Use cases:**
- An agent in meeting mode needs to join a cross-project channel to pull another project's agents into the discussion
- After cross-project collaboration ends, the agent leaves the channel to reduce unnecessary event delivery
- Initial subscriptions (from `channel_listen` in config.toml) are created automatically at startup; runtime dynamic subscriptions are additional extensions

**Backward compatibility:** Initial subscription behavior is unchanged; `channel_listen` configuration still takes effect at startup. The new tools only provide additional runtime control.

---

## 12.10 Meeting Mode (Meeting Protocol)

### Overview

When a user presses `[c]` multiple times to launch multiple clarifier agents for the same project, those agents discover each other via the `agents.clarifier` count in `list_projects` and enter meeting mode using a **Facilitator/Advisor role model**.

- **Facilitator**: the first agent to broadcast `[Joining Meeting]`; drives the full workflow, asks questions of the user, relays user answers, and writes the Issue Spec.
- **Advisor**: agents that join subsequently; independently analyzes the codebase and sends findings to the Facilitator, then operates in follow mode responding to the Facilitator's messages. Advisors do not independently execute workflow steps 5–17 and do not ask the user questions directly (except for `[⚠ Warning]` emergency alerts).

### Trigger Conditions

Meeting mode is triggered when **both** of the following conditions are met:

1. **Channel tools are available**: `.mcp.json` is deployed and the MCP server is active
2. **Another clarifier agent is discovered**: determined by the `agents.clarifier` count returned by `list_projects` — own project `agents.clarifier >= 2`, or a project in `channel_listen` has `agents.clarifier >= 1`

If either condition is not met, the clarifier operates in single-agent mode.

### Flow Diagram

```
Clarifier A (Facilitator)              Clarifier B (Advisor)
  │                                      │
  ├─ 1. Startup Probe                    ├─ 1. Startup Probe
  │    list_projects                      │    list_projects
  │    → agents.clarifier >= 2            │    → agents.clarifier >= 2
  │    send_message [Joining Meeting]     │    receives A's [Joining Meeting]
  │    role: Facilitator            ───►  │    → automatically becomes Advisor
  │                                      │    send_message [Joining Meeting]
  │                                      │    role: Advisor
  │                                      │
  │                                      ├─ 2. Codebase Analysis
  │                                      │    reads relevant code
  │  ◄─── [{AgentName}] {analysis}       │    send_message analysis
  │  integrates Advisor analysis          │
  │                                      │
  ├─ 3. Ask User                         │
  │    (only agent that asks the user)   │
  │    send_message [User Relay]    ───►  │  receives → responds agree/disagree/supplement
  │  ◄─── [{AgentName}] {response}       │
  │                                      │
  ├─ 4. Convergence                      │
  │    [Convergence Check]          ───►  │  replies with final additions
  │  ◄─── additions                      │
  │    validates SCOPE path              │
  │    writes Issue Spec                 │
  │    pushes to Tracker                 │
  │    [Meeting Closed]             ───►  │  meeting ends
  └─────────────────                      └─────────────────
```

### Message Formats

| Type | Format | Example |
|---|---|---|
| Join meeting | `[Joining Meeting] I am {AgentName} (clarifier) on project {ProjectID}, role: {Role}` | `[Joining Meeting] I am clarifier-a3f7 (clarifier) on project zpit, role: Facilitator` |
| Analysis / opinion | `[{AgentName}] {content}` | `[clarifier-f4db] sseConns in broker.go needs to be changed to a nested map` |
| User relay | `[User Relay] {summary}` | `[User Relay] User wants agent_type as a query param` |
| Convergence check | `[Convergence Check] {consensus}` | `[Convergence Check] Current consensus: 1. Use query param... 2. ...` |
| Emergency warning | `[⚠ Warning] {AgentName}: {warning}` | `[⚠ Warning] clarifier-f4db: This change will break backward compatibility` |
| Meeting closed | `[Meeting Closed] Issue #{N} pushed — {title}` | `[Meeting Closed] Issue #80 pushed — Improve Meeting Protocol` |

### Relationship to the Existing Channel Mechanism

Meeting mode is built entirely on top of the existing MCP tools, using the `agent_type` infrastructure for role discovery:

| Mechanism used | Purpose |
|---|---|
| `agents.clarifier` from `list_projects` | Startup probe — discovers other clarifiers in the same or cross-project |
| `send_message` | All inter-agent communication (join, analysis, relay, convergence, warning, close) |
| `ZPIT_AGENT_TYPE` + SSE `?agent_type=` | Lets the broker distinguish agent types and provide accurate clarifier counts |
