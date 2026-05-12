# Desktop Agent Architecture

The desktop agent is a standalone Claude Code session that controls the OS via `@zavora-ai/computer-use-mcp`. Unlike project-scoped coding/reviewer agents, it operates globally (cwd = `$HOME` / `%USERPROFILE%`) and is limited to a single instance across all connected TUI clients.

---

## Why proxy MCP (vs hooks vs full trust)

Four approaches were considered. Three were rejected:

**Raw `.mcp.json` registration of `computer-use-mcp`** — exposes `run_script`, which gives the agent arbitrary PowerShell/AppleScript execution. This bypasses every zpit safety layer (path-guard, bash-firewall, git-guard) and the OS-level sandbox entirely. Rejected.

**`--allowedTools` filtering alone** — Claude Code filters by tool *name* only. There is no parameter-level enforcement. `--allowedTools` can block `run_script` by name, but it cannot reject `key text="win+r"` while permitting `key text="enter"`. Rejected.

**PreToolUse `mcp-firewall.sh` shell hook** — MCP tool parameters arrive as nested JSON inside the hook's input envelope. Shell parsing of nested JSON is brittle and error-prone. Structured, policy-aware error responses (explaining *why* a call was denied) are much easier to produce from a Go process than from a shell script. Rejected.

**Chosen: Go proxy MCP server (`zpit serve-desktop-proxy`)** — sits on the stdio path between Claude Code and the upstream `npx @zavora-ai/computer-use-mcp` subprocess. Every JSON-RPC `tools/call` frame is intercepted, evaluated against the loaded policy, and either forwarded or rejected with a structured error message that the agent can reason about.

```
Claude Code (desktop agent) ─── stdio ──> zpit serve-desktop-proxy (Go)
                                                     │
                                                     ├── policy gate (allow/deny per call)
                                                     └── stdio ──> npx @zavora-ai/computer-use-mcp (Node subprocess)
                                                                              │
                                                                              └── OS APIs (CGEvent / UIA / AX)
```

---

## Security model

The five-layer hook stack (path-guard, bash-firewall, git-guard, worktree isolation, final merge gate) **does not apply** to the desktop agent. The agent has SendInput over the whole desktop; file-write hooks are meaningless. The proxy IS the safety layer.

Three categories of per-call enforcement:

1. **Tool allowlist** — only tools on the explicit allowlist are forwarded. Everything else is rejected at the proxy before the upstream process sees the frame. The list is intentionally conservative; expansion requires a deliberate policy edit.

2. **Parameter policy** — for tools on the allowlist the proxy applies:
   - `deny_keys`: reject `key` tool calls whose `text` parameter matches (substring) any entry in the deny list.
   - `allow_bundles`: named shortcut groups the user pre-approves (e.g. `["browser_navigation"]`). Calls matching a bundle are allowed even if the key would otherwise be on the deny list.
   - Focus-strategy override: the policy can require a `focus_check` before pointer actions, reducing the chance the agent types into the wrong window.

3. **Single-instance lock** — `AppState.activeDesktopAgent` is a mutex-protected field. The `[w]` launch path checks this before spawning; a second launch attempt is rejected with a status toast. This prevents two concurrent agents from racing on the same input surface.

Hard escape hatches blocked at the allowlist level:

- `run_script` — arbitrary shell execution; no legitimate desktop-control use case requires it.
- `filesystem` — arbitrary file I/O bypasses path-guard.
- `process_kill` — can kill zpit itself or any other process the user is running.
- `registry` — Windows registry writes are a persistence vector.
- `notification` — can be used as a social-engineering vector (fake OS alerts).
- `scrape` — arbitrary HTTP egress; outside the desktop-control threat model.
- `multi_edit` / `multi_select` — batch operations that pre-date the policy mode; audit granularity is lost.
- `snapshot` — composite tool that pulls in observation + annotation in one call, making per-call audit harder than calling each component tool separately.
- All virtual-desktop tools (`switch_desktop`, `create_desktop`, etc.) — can hide what the user is looking at.
- `resize_window` — annoyance risk; can occlude the cursor and confuse subsequent coordinate-based actions.

OS-level dependency: Accessibility permission (macOS) / UIAutomation registration (Windows) must be granted independently. Without it, mouse/keyboard tools fail at the upstream level. This is the OS's final say, independent of the proxy policy.

---

## Tool-by-tool allowlist justification

**Pointer (12 tools)** — `move_mouse`, `click`, `double_click`, `right_click`, `middle_click`, `scroll`, `drag`, `hover`, `click_and_hold`, `release`, `multi_click`, `mouse_position`. Core desktop-control primitives. No shell execution, no file I/O. Parameter policy (focus-strategy) is applied before forwarding.

**Keyboard (3 tools)** — `key`, `type_text`, `key_combo`. Required for any meaningful automation. Gated by `deny_keys` to block OS-level escape hatches. `type_text` is forwarded without key-level policy (it types literal characters, not key sequences).

**Clipboard (2 tools)** — `read_clipboard` and `write_clipboard` (plain text only). Clipboard is the standard agent-to-application data bridge. Binary/image clipboard write is not on the allowlist because the upstream tool does not expose it separately; plain-text write is the only variant needed.

**Observation (10+ tools)** — `screenshot`, `get_screen_size`, `get_cursor_position`, `find_on_screen`, `wait_for_element`, `get_focused_window`, `list_windows`, `get_window_info`, `get_active_app`, `get_screen_text`, and related. Read-only; they cannot modify system state. Necessary for the agent to perceive the current UI state before acting.

**App/window management (9 tools)** — `open_app`, `close_window`, `minimize_window`, `maximize_window`, `restore_window`, `focus_window`, `move_window`, `bring_to_front`, `activate_app`. Required for standard desktop workflows (open an app, bring a window to focus). `resize_window` is excluded (annoyance + coordinate confusion risk).

**Accessibility actions (8 tools)** — `get_accessibility_tree`, `click_element`, `type_in_element`, `get_element_text`, `set_element_value`, `get_element_attributes`, `find_element`, `get_focusable_elements`. Allows structured interaction with UI elements by accessibility role rather than pixel coordinates, which is more robust and auditable.

**Explicitly rejected tools:**

| Tool | Reason |
|------|--------|
| `run_script` | Arbitrary shell execution — no desktop-control task requires it |
| `filesystem` | Arbitrary file I/O bypasses path-guard entirely |
| `process_kill` | Can terminate zpit itself or any user process |
| `registry` | Windows persistence vector |
| `notification` | Social-engineering vector (fake OS alerts) |
| `scrape` | Arbitrary HTTP egress; outside the threat model |
| `multi_edit` / `multi_select` | Batch ops that pre-date policy mode; per-call audit granularity is lost |
| `snapshot` | Composite call; makes per-call audit harder than individual component tools |
| Virtual-desktop tools | Can hide the active desktop from the user |
| `resize_window` | Annoyance risk; can occlude the cursor |

---

## Default deny_keys per platform

| Shortcut | Platform | What it does | Why it's blocked |
|---|---|---|---|
| `win+r` | Windows | Open Run dialog | Arbitrary command execution |
| `ctrl+shift+esc` | Windows | Open Task Manager | Process kill / privilege ops |
| `ctrl+alt+del` | Windows | Secure attention sequence | OS-only; agent has no legitimate use case |
| `alt+f4` | Both | Close foreground window | Can lose unsaved work without explicit user intent |
| `cmd+q` | macOS | Quit current app | Same as alt+f4 |
| `cmd+option+esc` | macOS | Force quit dialog | Process kill UI |
| `cmd+ctrl+q` | macOS | Lock screen | Denial-of-service against the user |

Note: substring matching means `cmd+q` also catches `cmd+shift+q` (log out), which is the intended behaviour.

Users can loosen these defaults by adding `allow_bundles` entries in `~/.zpit/desktop-policy.toml`. They cannot be tightened beyond this default set from config alone; changing the defaults requires a code change.

---

## Future Phase 2 expansion points

- **Channel/MCP integration** — Phase 1: desktop agent is standalone (no broker subscription). Phase 2 could subscribe to coding-agent channel events and react (e.g. open a browser to test a deployed feature automatically).
- **Auto-compact when context window fills** — hook-layer feature, not agent-layer; tracked separately from the proxy work.
- **Per-profile policies** — Phase 1 uses one global policy file. Per-project profiles would cover situations like "PLC ladder editor allows alt+f4 because that is how you close a dialog in that application."
- **zpit-API tools** — deferred; would solve the "operate zpit's own TUI" use case without screenshot+hotkey brittleness by exposing TUI actions as MCP tools.
- **Linux support** — blocked on upstream `computer-use-mcp` adding Linux support; the package declares `os: ["darwin", "win32"]` in its `package.json`. The `[w]` hotkey is a no-op on `runtime.GOOS == "linux"`.
- **Recording / replay** — capture every approved tool call to a per-session JSONL for replay or audit. Useful for regression testing UI workflows.
