// Package desktop manages the policy for the desktop-control agent proxy.
// The proxy forwards MCP tool calls to the zpit-desktop-mcp Node subprocess
// (zpit's fork of @zavora-ai/computer-use-mcp; original work by
// James Karanja Maina / zavora.ai, MIT) subject to the constraints defined
// in Policy. See docs/architecture/desktop-agent.md for the full design
// rationale and upstream credit.
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

// Profile names. The active profile supplies default values for AllowedTools,
// DeniedTools, DenyKeys, AllowRunScript, and KeyboardFocusStrategy; explicit
// fields in the policy file override those defaults via MetaData.IsDefined.
const (
	ProfileReadOnly   = "read-only"
	ProfileStrict     = "strict"
	ProfileStandard   = "standard" // default when `profile` is omitted
	ProfileNoneScript = "none-script"
	ProfileTrusted    = "trusted"
)

// WildcardTool is the sentinel value in AllowedTools that bypasses the
// allowlist filter entirely. Used by the `trusted` profile so users do not
// have to enumerate every upstream tool name.
const WildcardTool = "*"

// policyTemplate is written verbatim when LoadPolicy creates a new file.
// The header documents the five profile presets so users can pick a level
// without having to read the architecture docs first.
const policyTemplate = `# Zpit Desktop Agent Policy
# Auto-created on first run; you own this file thereafter.
#
# ─── Profile presets ───────────────────────────────────────────────
# profile = "<one of: read-only | strict | standard | none-script | trusted>"
#
#   read-only    Observation only. Agent can screenshot, query the UI tree,
#                read the clipboard. No clicks, keys, app activation,
#                clipboard writes. Use for "just look at my screen"
#                diagnosis tasks.
#
#   strict       Maximum restrictions while still functional. 42 tools,
#                16 denied, 8 deny_keys including alt+f4. Choose when you
#                are not sure what the agent will need to do.
#
#   standard     (default) Daily automation defaults. Drops audit-only
#                blocks (snapshot, multi_edit, multi_select, resize_window,
#                list_spaces, get_active_space) and allows alt+f4. All
#                shell / filesystem / registry / process_kill escape
#                hatches remain blocked.
#
#   none-script  Trust the agent to drive the GUI freely but never reach
#                shell, filesystem, registry, or process_kill. Opens
#                win+r and cmd+space — the agent can launch any GUI app
#                but cannot execute arbitrary commands directly.
#
#   trusted      All upstream tools forwarded, including run_script,
#                filesystem, registry, process_kill. Equivalent to
#                mounting computer-use-mcp without the proxy. Use only
#                when you fully trust the task and the session.
#
# Explicit fields below override the chosen profile's defaults. Any field
# you leave commented-out inherits the profile's value.
# ───────────────────────────────────────────────────────────────────

profile = "standard"

# Uncomment any of the following to override the profile's defaults.
#
# allowed_tools = []
# denied_tools = []
# allow_bundles = []
# deny_keys = []
# allow_run_script = false
# keyboard_focus_strategy = "strict"   # strict | best_effort | none | prepare_display
`

// Policy holds desktop-agent enforcement settings loaded from
// ~/.zpit/desktop-policy.toml. Mutate only at startup; the proxy reads it
// concurrently across many tool calls.
type Policy struct {
	Profile               string   `toml:"profile"`
	AllowedTools          []string `toml:"allowed_tools"`
	DeniedTools           []string `toml:"denied_tools"`
	AllowBundles          []string `toml:"allow_bundles"`
	DenyKeys              []string `toml:"deny_keys"`
	AllowRunScript        bool     `toml:"allow_run_script"`
	KeyboardFocusStrategy string   `toml:"keyboard_focus_strategy"`
}

// DefaultAllowedTools is the canonical 42-tool allowlist used by the `strict`
// profile (AC-2). Escape hatches (run_script, filesystem, process_kill,
// registry, virtual-desktop tools) are intentionally omitted.
//
// The `standard` profile extends this list with six audit-only tools
// (see standardAllowedToolsExtra); the `none-script` profile additionally
// adds the four virtual-desktop mutation tools.
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

// DefaultDeniedTools is the 16-entry unconditional deny list used by the
// `strict` profile (AC-1). These are the upstream escape hatches and
// virtual-desktop tools that either bypass the policy entirely
// (run_script, filesystem, process_kill, registry) or fundamentally
// break the single-shared-desktop model the proxy assumes.
//
// Deny precedence: when a name appears in both DefaultAllowedTools and
// DefaultDeniedTools, DefaultDeniedTools wins. Policy.IsToolDenied is checked
// before Policy.IsToolAllowed in the proxy gate.
var DefaultDeniedTools = []string{
	"run_script", "filesystem", "process_kill", "registry",
	"notification", "scrape", "multi_edit", "multi_select", "snapshot",
	"list_spaces", "get_active_space", "create_agent_space",
	"destroy_space", "move_window_to_space", "remove_window_from_space",
	"resize_window",
}

// DefaultDenyKeys lists keyboard shortcuts that are unconditionally rejected
// for `key` and `hold_key` calls under the `strict` profile. Substring match
// (case-insensitive).
//
//   - Linux/X11 terminal launcher (ctrl+alt+t) and lock (super+l)
//   - Windows secure attention (ctrl+alt+delete), Run dialog (win+r), lock (win+l)
//   - macOS quit app (cmd+q) and Spotlight (cmd+space)
//   - Alt+F4 family (window kill)
var DefaultDenyKeys = []string{
	"ctrl+alt+t",      // Linux/X11 terminal launcher
	"ctrl+alt+delete", // Windows secure attention sequence
	"alt+f4",          // window kill (Windows)
	"super+l",         // Linux/GNOME lock screen
	"cmd+q",           // macOS quit app
	"cmd+space",       // macOS Spotlight
	"win+r",           // Windows Run dialog
	"win+l",           // Windows lock screen
}

// readOnlyAllowedTools is the observation-only allowlist for the `read-only`
// profile. Pointer, keyboard, AX actions, and write_clipboard are excluded —
// the agent can perceive screen state but cannot mutate anything.
var readOnlyAllowedTools = []string{
	"screenshot", "zoom",
	"cursor_position", "wait",
	"list_windows", "list_running_apps",
	"get_window", "get_cursor_window", "get_frontmost_app",
	"get_display_size", "list_displays",
	"read_clipboard",
	"get_ui_tree", "get_focused_element", "find_element",
	"list_menu_bar",
	"get_app_capabilities", "get_tool_guide",
}

// standardAllowedToolsExtra lists the six tools the `standard` profile adds
// on top of DefaultAllowedTools (and correspondingly removes from
// DefaultDeniedTools). These were originally denied for audit-granularity
// reasons (snapshot, multi_*) or annoyance reasons (resize_window); the
// standard profile decides the trade-off is worth it for daily use.
var standardAllowedToolsExtra = []string{
	"snapshot", "multi_edit", "multi_select",
	"resize_window",
	"list_spaces", "get_active_space",
}

// noneScriptAllowedToolsExtra lists the four additional virtual-desktop
// mutation tools opened up by the `none-script` profile.
var noneScriptAllowedToolsExtra = []string{
	"create_agent_space", "destroy_space",
	"move_window_to_space", "remove_window_from_space",
}

// profilePresets is the source of truth for each profile's default values.
// Keys correspond to the Profile* constants. ProfilePolicy and LoadPolicy
// both consult this map; mutate only via buildProfilePresets at init.
var profilePresets = buildProfilePresets()

func buildProfilePresets() map[string]Policy {
	// Build standard's allowed_tools = strict + 6 extras.
	standardAllowed := make([]string, 0, len(DefaultAllowedTools)+len(standardAllowedToolsExtra))
	standardAllowed = append(standardAllowed, DefaultAllowedTools...)
	standardAllowed = append(standardAllowed, standardAllowedToolsExtra...)

	// Build standard's denied_tools = strict minus the 6 extras.
	standardDeniedSet := make(map[string]bool, len(standardAllowedToolsExtra))
	for _, t := range standardAllowedToolsExtra {
		standardDeniedSet[t] = true
	}
	standardDenied := make([]string, 0, len(DefaultDeniedTools)-len(standardAllowedToolsExtra))
	for _, t := range DefaultDeniedTools {
		if !standardDeniedSet[t] {
			standardDenied = append(standardDenied, t)
		}
	}

	// Build standard's deny_keys = strict minus alt+f4.
	standardDenyKeys := make([]string, 0, len(DefaultDenyKeys)-1)
	for _, k := range DefaultDenyKeys {
		if k == "alt+f4" {
			continue
		}
		standardDenyKeys = append(standardDenyKeys, k)
	}

	// Build none-script's allowed_tools = standard + 4 virtual-desktop tools.
	noneScriptAllowed := make([]string, 0, len(standardAllowed)+len(noneScriptAllowedToolsExtra))
	noneScriptAllowed = append(noneScriptAllowed, standardAllowed...)
	noneScriptAllowed = append(noneScriptAllowed, noneScriptAllowedToolsExtra...)

	// Build none-script's denied_tools — only the true escape hatches.
	noneScriptDenied := []string{
		"run_script", "filesystem", "registry", "process_kill",
		"scrape", "notification",
	}

	// Build none-script's deny_keys — only what bricks the user out.
	noneScriptDenyKeys := []string{
		"ctrl+alt+delete", "super+l", "win+l",
	}

	return map[string]Policy{
		ProfileReadOnly: {
			AllowedTools:          dup(readOnlyAllowedTools),
			DeniedTools:           dup(DefaultDeniedTools),
			AllowBundles:          []string{},
			DenyKeys:              []string{},
			AllowRunScript:        false,
			KeyboardFocusStrategy: "strict",
		},
		ProfileStrict: {
			AllowedTools:          dup(DefaultAllowedTools),
			DeniedTools:           dup(DefaultDeniedTools),
			AllowBundles:          []string{},
			DenyKeys:              dup(DefaultDenyKeys),
			AllowRunScript:        false,
			KeyboardFocusStrategy: "strict",
		},
		ProfileStandard: {
			AllowedTools:          standardAllowed,
			DeniedTools:           standardDenied,
			AllowBundles:          []string{},
			DenyKeys:              standardDenyKeys,
			AllowRunScript:        false,
			KeyboardFocusStrategy: "strict",
		},
		ProfileNoneScript: {
			AllowedTools:          noneScriptAllowed,
			DeniedTools:           noneScriptDenied,
			AllowBundles:          []string{},
			DenyKeys:              noneScriptDenyKeys,
			AllowRunScript:        false,
			KeyboardFocusStrategy: "best_effort",
		},
		ProfileTrusted: {
			AllowedTools:          []string{WildcardTool},
			DeniedTools:           []string{},
			AllowBundles:          []string{},
			DenyKeys:              []string{},
			AllowRunScript:        true,
			KeyboardFocusStrategy: "none",
		},
	}
}

// dup returns a defensive copy of a string slice. Used to keep profilePresets
// immutable from the caller's perspective.
func dup(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// clonePolicy returns a deep copy of p with fresh slice backing arrays.
// LoadPolicy and ProfilePolicy both return clones so callers cannot mutate
// profilePresets in place.
func clonePolicy(p Policy) Policy {
	return Policy{
		Profile:               p.Profile,
		AllowedTools:          dup(p.AllowedTools),
		DeniedTools:           dup(p.DeniedTools),
		AllowBundles:          dup(p.AllowBundles),
		DenyKeys:              dup(p.DenyKeys),
		AllowRunScript:        p.AllowRunScript,
		KeyboardFocusStrategy: p.KeyboardFocusStrategy,
	}
}

// ProfilePolicy returns a defensive copy of the named profile's preset.
// Returns (Policy{}, false) when name is not a recognised profile.
//
// Valid names are the Profile* constants (read-only, strict, standard,
// none-script, trusted). The returned Policy has its Profile field set.
func ProfilePolicy(name string) (Policy, bool) {
	base, ok := profilePresets[name]
	if !ok {
		return Policy{}, false
	}
	p := clonePolicy(base)
	p.Profile = name
	return p, true
}

// DefaultPolicy returns the shipping default — a clone of the `standard`
// profile preset. The auto-created policy template also resolves to standard
// (it writes `profile = "standard"` and no overrides), so what users get on
// first run matches what DefaultPolicy returns.
func DefaultPolicy() Policy {
	p, _ := ProfilePolicy(ProfileStandard)
	return p
}

// LoadPolicy reads the TOML policy file at path. If the file is absent it
// returns DefaultPolicy and (created=true, nil) after writing the template.
//
// Field resolution order: the named profile's preset supplies defaults, and
// any field that the TOML file explicitly defines (per BurntSushi/toml's
// MetaData.IsDefined) overrides the preset. Unknown profile names log a
// warning and fall back to `standard`. Unknown top-level keys are logged
// and ignored for forward compatibility.
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
		logger.Printf("[desktop] LoadPolicy status=created profile=%s allowed_tools=%d deny_keys=%d",
			p.Profile, len(p.AllowedTools), len(p.DenyKeys))
		return p, true, nil
	}
	if err != nil {
		return Policy{}, false, fmt.Errorf("desktop: stat policy file: %w", err)
	}

	// Decode raw first so we can inspect MetaData.IsDefined before overlaying.
	var raw Policy
	meta, decodeErr := toml.DecodeFile(path, &raw)
	if decodeErr != nil {
		return Policy{}, false, fmt.Errorf("desktop: decode policy file %s: %w", path, decodeErr)
	}

	// Resolve the profile. Unknown profile names log a warning and fall back
	// to standard so a typo cannot brick the agent.
	profileName := raw.Profile
	if profileName == "" {
		profileName = ProfileStandard
	}
	base, ok := profilePresets[profileName]
	if !ok {
		logger.Printf("[desktop] LoadPolicy warn: unknown profile %q, falling back to %q", profileName, ProfileStandard)
		profileName = ProfileStandard
		base = profilePresets[ProfileStandard]
	}
	result := clonePolicy(base)
	result.Profile = profileName

	// Overlay explicitly-defined fields. Anything the user did not write in
	// the file inherits from the profile preset.
	if meta.IsDefined("allowed_tools") {
		result.AllowedTools = dup(raw.AllowedTools)
	}
	if meta.IsDefined("denied_tools") {
		result.DeniedTools = dup(raw.DeniedTools)
	}
	if meta.IsDefined("allow_bundles") {
		result.AllowBundles = dup(raw.AllowBundles)
	}
	if meta.IsDefined("deny_keys") {
		result.DenyKeys = dup(raw.DenyKeys)
	}
	if meta.IsDefined("allow_run_script") {
		result.AllowRunScript = raw.AllowRunScript
	}
	if meta.IsDefined("keyboard_focus_strategy") {
		result.KeyboardFocusStrategy = raw.KeyboardFocusStrategy
	}

	for _, key := range meta.Undecoded() {
		logger.Printf("[desktop] LoadPolicy warn: unknown key %q ignored", key)
	}

	logger.Printf("[desktop] LoadPolicy status=loaded profile=%s allowed_tools=%d deny_keys=%d",
		result.Profile, len(result.AllowedTools), len(result.DenyKeys))
	return result, false, nil
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

// IsToolAllowed returns true when name appears in p.AllowedTools, or when
// p.AllowedTools contains the WildcardTool sentinel ("*"). The wildcard is
// used by the `trusted` profile to forward every upstream tool without
// having to enumerate them.
//
// Note: this does NOT consult DeniedTools. Callers that need the effective
// permit/deny decision (deny precedence per AC-1) must call IsToolDenied
// first and treat a true result as a hard reject, regardless of AllowedTools.
func (p Policy) IsToolAllowed(name string) bool {
	for _, t := range p.AllowedTools {
		if t == WildcardTool {
			return true
		}
		if t == name {
			return true
		}
	}
	return false
}

// IsToolDenied returns true when name appears in p.DeniedTools. Deny precedence
// (AC-1 last sentence) — when a tool name appears in both AllowedTools and
// DeniedTools, IsToolDenied wins.
func (p Policy) IsToolDenied(name string) bool {
	for _, t := range p.DeniedTools {
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
