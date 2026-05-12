package desktop

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// discardLogger returns a *log.Logger that drops all output.
func discardLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// bufLogger returns a *log.Logger that writes to a bytes.Buffer.
func bufLogger(buf *bytes.Buffer) *log.Logger {
	return log.New(buf, "", 0)
}

// TestLoadPolicy_AutoCreatesFile verifies that when the target path does not
// exist, LoadPolicy writes the template, returns created=true, and the
// returned Policy equals DefaultPolicy().
func TestLoadPolicy_AutoCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", PolicyFileName)

	policy, created, err := LoadPolicy(path, discardLogger())
	if err != nil {
		t.Fatalf("LoadPolicy returned error: %v", err)
	}
	if !created {
		t.Error("expected created=true when file did not exist")
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected policy file to exist at %s: %v", path, statErr)
	}

	def := DefaultPolicy()
	if !stringSlicesEqual(policy.AllowedTools, def.AllowedTools) {
		t.Errorf("AllowedTools mismatch: got %v, want %v", policy.AllowedTools, def.AllowedTools)
	}
	if !stringSlicesEqual(policy.DenyKeys, def.DenyKeys) {
		t.Errorf("DenyKeys mismatch: got %v, want %v", policy.DenyKeys, def.DenyKeys)
	}
	if policy.AllowRunScript != def.AllowRunScript {
		t.Errorf("AllowRunScript: got %v, want %v", policy.AllowRunScript, def.AllowRunScript)
	}
	if policy.KeyboardFocusStrategy != def.KeyboardFocusStrategy {
		t.Errorf("KeyboardFocusStrategy: got %q, want %q", policy.KeyboardFocusStrategy, def.KeyboardFocusStrategy)
	}
}

// TestLoadPolicy_PreservesExistingFile verifies that LoadPolicy on an existing
// file does NOT overwrite it. It checks that the file's mtime is unchanged and
// that the returned values reflect the custom content.
func TestLoadPolicy_PreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, PolicyFileName)

	custom := `allowed_tools = ["screenshot"]
deny_keys = []
allow_run_script = true
keyboard_focus_strategy = "none"
`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("setup: write custom file: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("setup: stat: %v", err)
	}
	mtimeBefore := info.ModTime()

	// Ensure at least 1ms separation so any write would show a newer mtime.
	time.Sleep(5 * time.Millisecond)

	policy, created, loadErr := LoadPolicy(path, discardLogger())
	if loadErr != nil {
		t.Fatalf("LoadPolicy error: %v", loadErr)
	}
	if created {
		t.Error("expected created=false for existing file")
	}

	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("post-load stat: %v", err)
	}
	if !info2.ModTime().Equal(mtimeBefore) {
		t.Errorf("file mtime changed: before=%v after=%v", mtimeBefore, info2.ModTime())
	}

	if len(policy.AllowedTools) != 1 || policy.AllowedTools[0] != "screenshot" {
		t.Errorf("AllowedTools: got %v, want [screenshot]", policy.AllowedTools)
	}
	if !policy.AllowRunScript {
		t.Error("AllowRunScript: expected true")
	}
	if policy.KeyboardFocusStrategy != "none" {
		t.Errorf("KeyboardFocusStrategy: got %q, want %q", policy.KeyboardFocusStrategy, "none")
	}
}

// TestLoadPolicy_FillsMissingFields verifies that when a partial TOML is on
// disk (only deny_keys set), missing fields fall back to DefaultPolicy values.
func TestLoadPolicy_FillsMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, PolicyFileName)

	partial := `deny_keys = []
`
	if err := os.WriteFile(path, []byte(partial), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	policy, created, err := LoadPolicy(path, discardLogger())
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}
	if created {
		t.Error("expected created=false for existing file")
	}

	def := DefaultPolicy()

	// deny_keys was explicitly set to empty — must reflect the file.
	if len(policy.DenyKeys) != 0 {
		t.Errorf("DenyKeys: got %v, want []", policy.DenyKeys)
	}

	// AllowedTools was absent — must fall back to default.
	if !stringSlicesEqual(policy.AllowedTools, def.AllowedTools) {
		t.Errorf("AllowedTools: got %v, want default", policy.AllowedTools)
	}

	// KeyboardFocusStrategy was absent — must fall back to default.
	if policy.KeyboardFocusStrategy != def.KeyboardFocusStrategy {
		t.Errorf("KeyboardFocusStrategy: got %q, want %q", policy.KeyboardFocusStrategy, def.KeyboardFocusStrategy)
	}
}

// TestLoadPolicy_IgnoresUnknownKeys verifies that a TOML with unknown
// top-level keys does not error; the unknown key is logged at Warn and the
// policy decodes normally.
func TestLoadPolicy_IgnoresUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, PolicyFileName)

	content := `foo_bar = "ignored"
keyboard_focus_strategy = "best_effort"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var buf bytes.Buffer
	logger := bufLogger(&buf)

	policy, _, err := LoadPolicy(path, logger)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}

	logOutput := buf.String()
	if !bytes.Contains([]byte(logOutput), []byte("foo_bar")) {
		t.Errorf("expected log to mention unknown key foo_bar; got: %s", logOutput)
	}

	if policy.KeyboardFocusStrategy != "best_effort" {
		t.Errorf("KeyboardFocusStrategy: got %q, want best_effort", policy.KeyboardFocusStrategy)
	}
}

// TestPolicy_IsToolAllowed is table-driven and covers: a tool in the list, a
// tool not in the list, and an empty allowlist (denies everything).
func TestPolicy_IsToolAllowed(t *testing.T) {
	cases := []struct {
		name     string
		tools    []string
		query    string
		expected bool
	}{
		{
			name:     "tool in list",
			tools:    []string{"screenshot", "zoom"},
			query:    "screenshot",
			expected: true,
		},
		{
			name:     "tool not in list",
			tools:    []string{"screenshot", "zoom"},
			query:    "run_script",
			expected: false,
		},
		{
			name:     "empty allowlist denies everything",
			tools:    []string{},
			query:    "screenshot",
			expected: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Policy{AllowedTools: tc.tools}
			if got := p.IsToolAllowed(tc.query); got != tc.expected {
				t.Errorf("IsToolAllowed(%q) = %v, want %v", tc.query, got, tc.expected)
			}
		})
	}
}

// TestPolicy_MatchDenyKey is table-driven and covers: exact match,
// case-insensitive match, substring match, no-match, empty DenyKeys,
// and empty input text.
func TestPolicy_MatchDenyKey(t *testing.T) {
	defaultKeys := []string{"win+r", "ctrl+alt+del", "ctrl+alt+t", "alt+f4"}

	cases := []struct {
		name    string
		keys    []string
		text    string
		wantHit bool
		wantKey string
	}{
		{
			name:    "exact match",
			keys:    defaultKeys,
			text:    "win+r",
			wantHit: true,
			wantKey: "win+r",
		},
		{
			name:    "case-insensitive match",
			keys:    defaultKeys,
			text:    "Ctrl+Alt+Del",
			wantHit: true,
			wantKey: "ctrl+alt+del",
		},
		{
			name:    "substring match",
			keys:    defaultKeys,
			text:    "ctrl+alt+t+then+a",
			wantHit: true,
			wantKey: "ctrl+alt+t",
		},
		{
			name:    "no match",
			keys:    defaultKeys,
			text:    "ctrl+c",
			wantHit: false,
			wantKey: "",
		},
		{
			name:    "empty DenyKeys list always returns empty",
			keys:    []string{},
			text:    "ctrl+alt+del",
			wantHit: false,
			wantKey: "",
		},
		{
			name:    "empty input text always returns empty",
			keys:    defaultKeys,
			text:    "",
			wantHit: false,
			wantKey: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Policy{DenyKeys: tc.keys}
			got := p.MatchDenyKey(tc.text)
			if tc.wantHit && got == "" {
				t.Errorf("MatchDenyKey(%q) = %q, want non-empty hit", tc.text, got)
			}
			if !tc.wantHit && got != "" {
				t.Errorf("MatchDenyKey(%q) = %q, want empty (no match)", tc.text, got)
			}
			if tc.wantKey != "" && got != tc.wantKey {
				t.Errorf("MatchDenyKey(%q) = %q, want %q", tc.text, got, tc.wantKey)
			}
		})
	}
}

// TestPolicy_IsBundleAllowed verifies that empty AllowBundles permits any
// value, and non-empty AllowBundles requires an exact match.
func TestPolicy_IsBundleAllowed(t *testing.T) {
	cases := []struct {
		name      string
		bundles   []string
		targetApp string
		expected  bool
	}{
		{
			name:      "empty bundles allows anything",
			bundles:   []string{},
			targetApp: "com.example.App",
			expected:  true,
		},
		{
			name:      "exact match allowed",
			bundles:   []string{"com.apple.Safari", "com.example.App"},
			targetApp: "com.example.App",
			expected:  true,
		},
		{
			name:      "non-member denied",
			bundles:   []string{"com.apple.Safari"},
			targetApp: "com.example.Unknown",
			expected:  false,
		},
		{
			name:      "partial match not sufficient",
			bundles:   []string{"com.apple.Safari"},
			targetApp: "com.apple.SafariExtension",
			expected:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Policy{AllowBundles: tc.bundles}
			if got := p.IsBundleAllowed(tc.targetApp); got != tc.expected {
				t.Errorf("IsBundleAllowed(%q) = %v, want %v", tc.targetApp, got, tc.expected)
			}
		})
	}
}

// TestPolicy_Validate verifies that a valid policy returns nil and an invalid
// KeyboardFocusStrategy returns an error mentioning the bad value.
func TestPolicy_Validate(t *testing.T) {
	t.Run("valid strategies", func(t *testing.T) {
		for _, strat := range []string{"strict", "best_effort", "none", "prepare_display"} {
			p := Policy{KeyboardFocusStrategy: strat}
			if err := p.Validate(); err != nil {
				t.Errorf("Validate() with strategy %q returned error: %v", strat, err)
			}
		}
	})

	t.Run("invalid strategy", func(t *testing.T) {
		p := Policy{KeyboardFocusStrategy: "turbo_mode"}
		err := p.Validate()
		if err == nil {
			t.Fatal("expected error for invalid KeyboardFocusStrategy, got nil")
		}
		if !bytes.Contains([]byte(err.Error()), []byte("turbo_mode")) {
			t.Errorf("error message does not mention bad value: %v", err)
		}
	})
}

// stringSlicesEqual returns true if two string slices have identical content.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
