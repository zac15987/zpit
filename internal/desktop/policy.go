// Package desktop manages the policy for the desktop-control agent proxy.
// The proxy forwards MCP tool calls to the upstream computer-use-mcp Node
// subprocess subject to the constraints defined in Policy. See
// docs/architecture/desktop-agent.md for the full design rationale.
package desktop

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// PolicyFileName is the basename of the policy file under ~/.zpit/.
const PolicyFileName = "desktop-policy.toml"

// validFocusStrategies lists the accepted values for KeyboardFocusStrategy.
var validFocusStrategies = []string{"strict", "best_effort", "none", "prepare_display"}

// policyTemplate is written verbatim when LoadPolicy creates a new file.
const policyTemplate = `# Zpit Desktop Agent Policy
# Auto-created on first run; you own this file thereafter.

# allowed_tools: MCP tools forwarded to the upstream computer-use-mcp
# subprocess. Anything not in this list is rejected without spawning the
# subprocess. Default is the safe 42-tool subset — escape hatches
# (run_script, filesystem, process_kill, registry, virtual-desktop tools)
# are intentionally omitted.
allowed_tools = [
    "screenshot", "zoom", "left_click", "right_click", "middle_click",
    "double_click", "triple_click", "mouse_move", "left_click_drag",
    "left_mouse_down", "left_mouse_up", "scroll", "cursor_position",
    "type", "key", "hold_key", "wait", "list_windows", "list_running_apps",
    "get_window", "get_cursor_window", "get_frontmost_app",
    "activate_window", "activate_app", "open_application", "hide_app",
    "unhide_app", "get_display_size", "list_displays", "read_clipboard",
    "write_clipboard", "click_element", "press_button", "set_value",
    "fill_form", "get_ui_tree", "get_focused_element", "find_element",
    "list_menu_bar", "select_menu_item", "get_app_capabilities",
    "get_tool_guide",
]

# allow_bundles: when non-empty, any tool call whose target_app is not in
# this list is rejected. Empty (default) = no bundle filtering.
allow_bundles = []

# deny_keys: case-insensitive substring matches against the ` + "`text`" + ` field of
# ` + "`key`" + ` and ` + "`hold_key`" + ` calls. Anything matched is rejected.
deny_keys = ["win+r", "ctrl+shift+esc", "ctrl+alt+del", "cmd+q", "cmd+option+esc", "cmd+ctrl+q", "alt+f4"]

# allow_run_script: gates the ` + "`run_script`" + ` tool. Default false. Even when
# true, ` + "`run_script`" + ` must additionally appear in allowed_tools — this flag
# is a second layer, not a bypass.
allow_run_script = false

# keyboard_focus_strategy: overrides focus_strategy for ` + "`type`" + `, ` + "`key`" + `,
# ` + "`hold_key`" + `, ` + "`set_value`" + `, ` + "`fill_form`" + `. One of: strict | best_effort |
# none | prepare_display. Default "strict" — a wrong-target keystroke is
# more damaging than a failed call.
keyboard_focus_strategy = "strict"
`

// Policy holds desktop-agent enforcement settings loaded from
// ~/.zpit/desktop-policy.toml. Mutate only at startup; the proxy reads it
// concurrently across many tool calls.
type Policy struct {
	AllowedTools          []string `toml:"allowed_tools"`
	AllowBundles          []string `toml:"allow_bundles"`
	DenyKeys              []string `toml:"deny_keys"`
	AllowRunScript        bool     `toml:"allow_run_script"`
	KeyboardFocusStrategy string   `toml:"keyboard_focus_strategy"`
}

// DefaultAllowedTools is the canonical 42-tool allowlist (AC-2). Escape
// hatches (run_script, filesystem, process_kill, registry, virtual-desktop
// tools) are intentionally omitted.
var DefaultAllowedTools = []string{
	"screenshot", "zoom", "left_click", "right_click", "middle_click",
	"double_click", "triple_click", "mouse_move", "left_click_drag",
	"left_mouse_down", "left_mouse_up", "scroll", "cursor_position",
	"type", "key", "hold_key", "wait", "list_windows", "list_running_apps",
	"get_window", "get_cursor_window", "get_frontmost_app",
	"activate_window", "activate_app", "open_application", "hide_app",
	"unhide_app", "get_display_size", "list_displays", "read_clipboard",
	"write_clipboard", "click_element", "press_button", "set_value",
	"fill_form", "get_ui_tree", "get_focused_element", "find_element",
	"list_menu_bar", "select_menu_item", "get_app_capabilities",
	"get_tool_guide",
}

// DefaultDenyKeys lists keyboard shortcuts that are unconditionally rejected
// for `key` and `hold_key` calls. Substring match (case-insensitive).
// Rationale (per docs/architecture/desktop-agent.md):
//   - Windows Run / Task Manager / Win-key shortcuts that bypass desktop sandboxing
//   - macOS power / log-out shortcuts
//   - Alt+F4 family (window kill)
var DefaultDenyKeys = []string{
	"win+r",          // Windows Run dialog
	"ctrl+shift+esc", // Windows Task Manager
	"ctrl+alt+del",   // Windows secure attention sequence
	"cmd+q",          // macOS quit app
	"cmd+option+esc", // macOS force quit
	"cmd+ctrl+q",     // macOS lock screen
	"alt+f4",         // window kill (Windows)
}

// DefaultPolicy returns a Policy populated with the shipping defaults.
func DefaultPolicy() Policy {
	tools := make([]string, len(DefaultAllowedTools))
	copy(tools, DefaultAllowedTools)
	keys := make([]string, len(DefaultDenyKeys))
	copy(keys, DefaultDenyKeys)
	return Policy{
		AllowedTools:          tools,
		AllowBundles:          []string{},
		DenyKeys:              keys,
		AllowRunScript:        false,
		KeyboardFocusStrategy: "strict",
	}
}

// LoadPolicy reads the TOML policy file at path. If the file is absent it
// returns DefaultPolicy and (created=true, nil) after writing the template.
// Unknown top-level keys are logged at Warn via the provided logger and
// ignored — forward compatibility per CONSTRAINTS.
//
// Returns (policy, created, error). created is true when this call wrote the
// file; the proxy uses this to emit an info log on first launch.
func LoadPolicy(path string, logger *log.Logger) (Policy, bool, error) {
	logger.Printf("[desktop] LoadPolicy path=%s", path)

	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return Policy{}, false, fmt.Errorf("desktop: create policy dir: %w", mkErr)
		}
		if writeErr := os.WriteFile(path, []byte(policyTemplate), 0o644); writeErr != nil {
			return Policy{}, false, fmt.Errorf("desktop: write policy template: %w", writeErr)
		}
		p := DefaultPolicy()
		logger.Printf("[desktop] LoadPolicy status=created allowed_tools=%d deny_keys=%d",
			len(p.AllowedTools), len(p.DenyKeys))
		return p, true, nil
	}
	if err != nil {
		return Policy{}, false, fmt.Errorf("desktop: stat policy file: %w", err)
	}

	// File exists — decode it, then fill any missing fields from defaults.
	def := DefaultPolicy()
	p := def // start from defaults so omitted fields retain their values

	meta, decodeErr := toml.DecodeFile(path, &p)
	if decodeErr != nil {
		return Policy{}, false, fmt.Errorf("desktop: decode policy: %w", decodeErr)
	}

	for _, key := range meta.Undecoded() {
		logger.Printf("[desktop] LoadPolicy warn: unknown key %q ignored", key)
	}

	logger.Printf("[desktop] LoadPolicy status=loaded allowed_tools=%d deny_keys=%d",
		len(p.AllowedTools), len(p.DenyKeys))
	return p, false, nil
}

// Validate returns nil when the policy values are usable.
//
// Notes:
//   - An empty AllowedTools list is valid (denies all tools — panic mode).
//     Validate logs a Warn when this occurs but does not return an error.
//   - KeyboardFocusStrategy must be one of: strict, best_effort, none, prepare_display.
func (p Policy) Validate() error {
	strat := p.KeyboardFocusStrategy
	for _, v := range validFocusStrategies {
		if strat == v {
			return nil
		}
	}
	return fmt.Errorf("desktop: invalid keyboard_focus_strategy %q: must be one of %s",
		strat, strings.Join(validFocusStrategies, ", "))
}

// IsToolAllowed returns true when name appears in p.AllowedTools.
func (p Policy) IsToolAllowed(name string) bool {
	for _, t := range p.AllowedTools {
		if t == name {
			return true
		}
	}
	return false
}

// MatchDenyKey returns the first DenyKeys entry that is a case-insensitive
// substring of text, or "" when no entry matches. Substring (not prefix)
// match per AC-3 — "ctrl+alt+t+then+a" must trip on "ctrl+alt+t".
func (p Policy) MatchDenyKey(text string) string {
	lower := strings.ToLower(text)
	for _, entry := range p.DenyKeys {
		if strings.Contains(lower, strings.ToLower(entry)) {
			return entry
		}
	}
	return ""
}

// IsBundleAllowed returns true when the target_app value is permitted.
// When AllowBundles is empty, any value is permitted (the AC-4 contract).
// Calls without a target_app field must not reach this method — that is the
// caller's responsibility per AC-4.
func (p Policy) IsBundleAllowed(targetApp string) bool {
	if len(p.AllowBundles) == 0 {
		return true
	}
	for _, b := range p.AllowBundles {
		if b == targetApp {
			return true
		}
	}
	return false
}
