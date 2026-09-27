package hooks_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ── Session scratchpad + drive-letter path handling ──
//
// Claude Code gives each session a private scratchpad at
// <temp-root>/claude/<encoded-cwd>/<session_id>/scratchpad/. Every role may
// write there; the hooks anchor the match to a real temp root and the exact
// session_id from hook stdin. The drive-letter tests guard the old bug where
// OS-native Windows paths (C:\...) were mistaken for relative paths and
// slipped through the worktree whitelist.

const testSessionID = "0d390c51-7fd0-4651-acaf-b292792acee2"

// scratchpadRoot returns (tempRoot, scratchpadDir). tempRoot is passed to the
// hook as CLAUDE_CODE_TMPDIR so the test does not depend on the host's TEMP.
func scratchpadRoot(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	sp := filepath.Join(root, "claude", "D--Documents-proj", testSessionID, "scratchpad")
	if err := os.MkdirAll(sp, 0o755); err != nil {
		t.Fatalf("mkdir scratchpad: %v", err)
	}
	return root, sp
}

func scratchEnv(root, agentType string) map[string]string {
	env := agentEnv(map[string]string{"CLAUDE_CODE_TMPDIR": root})
	if agentType != "" {
		env["ZPIT_AGENT_TYPE"] = agentType
	}
	return env
}

func fileInput(sessionID, path string) string {
	return `{"session_id":` + jsonQuote(sessionID) + `,"tool_input":{"file_path":` + jsonQuote(path) + `}}`
}

func cmdInput(sessionID, command string) string {
	return `{"session_id":` + jsonQuote(sessionID) + `,"tool_input":{"command":` + jsonQuote(command) + `}}`
}

// outsideRepo returns a native absolute path outside both the zpit repo
// (git toplevel of the hooks dir) and any temp root.
func outsideRepo(elem ...string) string {
	repo := filepath.Dir(hooksDir())
	return filepath.Join(append([]string{filepath.Dir(repo)}, elem...)...)
}

// ── path-guard.sh ──

func TestPathGuard_Scratchpad_ClarifierAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "path-guard.sh",
		fileInput(testSessionID, filepath.Join(sp, "issue_drafts.md")),
		scratchEnv(root, "clarifier"))
	if code != 0 {
		t.Errorf("expected exit 0 for clarifier writing own scratchpad, got %d: %s", code, msg)
	}
}

func TestPathGuard_Scratchpad_CodingAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "path-guard.sh",
		fileInput(testSessionID, filepath.Join(sp, "notes", "plan.go")),
		scratchEnv(root, "coding"))
	if code != 0 {
		t.Errorf("expected exit 0 for coding writing own scratchpad, got %d: %s", code, msg)
	}
}

func TestPathGuard_Scratchpad_ForwardSlashForm(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "path-guard.sh",
		fileInput(testSessionID, filepath.ToSlash(filepath.Join(sp, "a.md"))),
		scratchEnv(root, "clarifier"))
	if code != 0 {
		t.Errorf("expected exit 0 for forward-slash scratchpad path, got %d: %s", code, msg)
	}
}

func TestPathGuard_Scratchpad_OtherSessionBlocked(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "path-guard.sh",
		fileInput("another-session", filepath.Join(sp, "a.md")),
		scratchEnv(root, "clarifier"))
	if code != 2 {
		t.Errorf("expected exit 2 for another session's scratchpad, got %d: %s", code, msg)
	}
}

func TestPathGuard_Scratchpad_MissingSessionIDBlocked(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "path-guard.sh",
		`{"tool_input":{"file_path":`+jsonQuote(filepath.Join(sp, "a.md"))+`}}`,
		scratchEnv(root, "clarifier"))
	if code != 2 {
		t.Errorf("expected exit 2 without session_id, got %d: %s", code, msg)
	}
}

func TestPathGuard_Scratchpad_TraversalBlocked(t *testing.T) {
	root, sp := scratchpadRoot(t)
	// Climbs out of the scratchpad; .. must be collapsed before matching.
	code, msg := runHook(t, "path-guard.sh",
		fileInput(testSessionID, sp+string(filepath.Separator)+filepath.Join("..", "..", "..", "..", "escape.go")),
		scratchEnv(root, "coding"))
	if code != 2 {
		t.Errorf("expected exit 2 for scratchpad traversal, got %d: %s", code, msg)
	}
}

func TestPathGuard_Scratchpad_LookAlikeOutsideTempBlocked(t *testing.T) {
	root, _ := scratchpadRoot(t)
	// Same shape as a scratchpad, but not under a temp root.
	fake := outsideRepo("claude", "x", testSessionID, "scratchpad", "a.go")
	code, msg := runHook(t, "path-guard.sh",
		fileInput(testSessionID, fake),
		scratchEnv(root, "coding"))
	if code != 2 {
		t.Errorf("expected exit 2 for look-alike scratchpad outside temp root, got %d: %s", code, msg)
	}
}

func TestPathGuard_NativeAbsoluteOutsideWorktreeBlocked(t *testing.T) {
	// Regression: OS-native absolute paths used to be glued onto the worktree
	// root (bash saw C:\... as relative) and pass the whitelist.
	code, msg := runHook(t, "path-guard.sh",
		fileInput(testSessionID, outsideRepo("outside.go")),
		agentEnv(nil))
	if code != 2 {
		t.Errorf("expected exit 2 for native absolute path outside worktree, got %d: %s", code, msg)
	}
}

func TestPathGuard_NativeAbsoluteInsideWorktreeAllowed(t *testing.T) {
	code, msg := runHook(t, "path-guard.sh",
		fileInput(testSessionID, filepath.Join(filepath.Dir(hooksDir()), "internal", "x.go")),
		agentEnv(nil))
	if code != 0 {
		t.Errorf("expected exit 0 for native absolute path inside worktree, got %d: %s", code, msg)
	}
}

func TestPathGuard_LowercaseDriveLetterInsideWorktreeAllowed(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters are Windows-only")
	}
	p := filepath.Join(filepath.Dir(hooksDir()), "internal", "x.go")
	p = strings.ToLower(p[:1]) + p[1:]
	code, msg := runHook(t, "path-guard.sh", fileInput(testSessionID, p), agentEnv(nil))
	if code != 0 {
		t.Errorf("expected exit 0 for lowercase drive letter inside worktree, got %d: %s", code, msg)
	}
}

// ── bash-firewall.sh ──

func TestBashFirewall_Clarifier_Scratchpad_RmAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput(testSessionID, `rm "`+filepath.ToSlash(filepath.Join(sp, "issue_drafts.md"))+`"`),
		scratchEnv(root, "clarifier"))
	if code != 0 {
		t.Errorf("expected exit 0 for rm in scratchpad, got %d: %s", code, msg)
	}
}

func TestBashFirewall_Clarifier_Scratchpad_RedirectAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput(testSessionID, `echo body > `+filepath.ToSlash(filepath.Join(sp, "draft.md"))),
		scratchEnv(root, "clarifier"))
	if code != 0 {
		t.Errorf("expected exit 0 for redirect into scratchpad, got %d: %s", code, msg)
	}
}

func TestBashFirewall_Clarifier_Scratchpad_OtherSessionRmBlocked(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput("another-session", `rm `+filepath.ToSlash(filepath.Join(sp, "draft.md"))),
		scratchEnv(root, "clarifier"))
	if code != 2 {
		t.Errorf("expected exit 2 for rm in another session's scratchpad, got %d: %s", code, msg)
	}
}

// Real-world case: clarifier fixing a typo in its own draft issue body.
func TestBashFirewall_Clarifier_SedInPlaceTmpAllowed(t *testing.T) {
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput(testSessionID, `cd "D:/Documents/proj"; sed -i 's/iterates a list of all 12 commands (one row each/iterates a list of all 14 command rows (one row each/' tmp_issue2.md; grep -c "14 command rows" tmp_issue2.md`),
		clarifierEnv(worktreeEnv))
	if code != 0 {
		t.Errorf("expected exit 0 for sed -i on tmp_*.md, got %d: %s", code, msg)
	}
}

func TestBashFirewall_Clarifier_SedInPlaceScratchpadAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	target := filepath.Join(sp, "issue2.md")
	cases := map[string]string{
		"-e, quoted native path": `sed -i -e 's/12/14/' "` + target + `"`,
		"forward-slash path":     `sed -i 's/12/14/' ` + filepath.ToSlash(target),
		"unquoted script":        `sed -i s/12/14/ ` + filepath.ToSlash(target),
	}
	for name, command := range cases {
		code, msg := runHook(t, "bash-firewall.sh", cmdInput(testSessionID, command), scratchEnv(root, "clarifier"))
		if code != 0 {
			t.Errorf("%s: expected exit 0 for %q, got %d: %s", name, command, code, msg)
		}
	}
}

func TestBashFirewall_Clarifier_SedInPlaceBlocked(t *testing.T) {
	cases := map[string]string{
		"source file":            `sed -i 's/a/b/' internal/tui/model.go`,
		"tmp plus source":        `sed -i 's/a/b/' tmp_a.md internal/tui/model.go`,
		"-e script, source file": `sed -i -e 's/a/b/' internal/tui/model.go`,
		"script named like tmp":  `sed -i tmp_a.md internal/tui/model.go`,
		"redirect ride-along":    `sed -i 's/a/b/' tmp_a.md > internal/x.go`,
		"no target":              `sed -i 's/a/b/'`,
		"unbalanced quote":       `sed -i 's/a/b/ tmp_a.md`,
	}
	for name, command := range cases {
		code, msg := runHook(t, "bash-firewall.sh", cmdInput(testSessionID, command), clarifierEnv(worktreeEnv))
		if code != 2 {
			t.Errorf("%s: expected exit 2 for %q, got %d: %s", name, command, code, msg)
		}
	}
}

func TestBashFirewall_Scratchpad_RedirectAllowedForCoding(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput(testSessionID, `go test ./... > `+filepath.ToSlash(filepath.Join(sp, "test.log"))+` 2>&1`),
		scratchEnv(root, "coding"))
	if code != 0 {
		t.Errorf("expected exit 0 for redirect into scratchpad, got %d: %s", code, msg)
	}
}

func TestBashFirewall_DriveLetterEscapeBlocked(t *testing.T) {
	// Regression: drive-letter targets bypassed the `/*`-only escape check.
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput(testSessionID, `echo pwned > `+filepath.ToSlash(outsideRepo("escape.txt"))),
		agentEnv(nil))
	if code != 2 {
		t.Errorf("expected exit 2 for drive-letter/absolute escape, got %d: %s", code, msg)
	}
}

func TestBashFirewall_QuotedEscapeBlocked(t *testing.T) {
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput(testSessionID, `echo pwned > "`+filepath.ToSlash(outsideRepo("escape.txt"))+`"`),
		agentEnv(nil))
	if code != 2 {
		t.Errorf("expected exit 2 for quoted absolute escape, got %d: %s", code, msg)
	}
}

func TestBashFirewall_AbsoluteInsideWorktreeAllowed(t *testing.T) {
	code, msg := runHook(t, "bash-firewall.sh",
		cmdInput(testSessionID, `echo log > `+filepath.ToSlash(filepath.Join(filepath.Dir(hooksDir()), "build.log"))),
		agentEnv(nil))
	if code != 0 {
		t.Errorf("expected exit 0 for absolute redirect inside worktree, got %d: %s", code, msg)
	}
}

// ── pwsh-firewall.sh ──

func TestPwshFirewall_Clarifier_Scratchpad_RemoveItemAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "pwsh-firewall.sh",
		cmdInput(testSessionID, `Remove-Item `+filepath.Join(sp, "issue_drafts.md")),
		scratchEnv(root, "clarifier"))
	if code != 0 {
		t.Errorf("expected exit 0 for Remove-Item in scratchpad, got %d: %s", code, msg)
	}
}

func TestPwshFirewall_Clarifier_Scratchpad_SetContentAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "pwsh-firewall.sh",
		cmdInput(testSessionID, `Set-Content -Path `+filepath.Join(sp, "draft.md")+` -Value 'body'`),
		scratchEnv(root, "clarifier"))
	if code != 0 {
		t.Errorf("expected exit 0 for Set-Content in scratchpad, got %d: %s", code, msg)
	}
}

func TestPwshFirewall_Clarifier_Scratchpad_RedirectAllowed(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "pwsh-firewall.sh",
		cmdInput(testSessionID, `'body' > `+filepath.Join(sp, "draft.md")),
		scratchEnv(root, "clarifier"))
	if code != 0 {
		t.Errorf("expected exit 0 for redirect into scratchpad, got %d: %s", code, msg)
	}
}

func TestPwshFirewall_Clarifier_Scratchpad_OtherSessionBlocked(t *testing.T) {
	root, sp := scratchpadRoot(t)
	code, msg := runHook(t, "pwsh-firewall.sh",
		cmdInput("another-session", `Set-Content -Path `+filepath.Join(sp, "draft.md")+` -Value 'body'`),
		scratchEnv(root, "clarifier"))
	if code != 2 {
		t.Errorf("expected exit 2 for another session's scratchpad, got %d: %s", code, msg)
	}
}

func TestPwshFirewall_DriveLetterEscapeBlocked(t *testing.T) {
	code, msg := runHook(t, "pwsh-firewall.sh",
		cmdInput(testSessionID, `'pwned' > `+outsideRepo("escape.txt")),
		agentEnv(nil))
	if code != 2 {
		t.Errorf("expected exit 2 for native absolute escape, got %d: %s", code, msg)
	}
}

// ── scratchpad_dir (primary) ──
//
// Current Claude Code passes `scratchpad_dir` in hook stdin (verified: the
// main session and its subagents receive the same value). When present it is
// the sole source of truth — an exact prefix match, no path reconstruction.

func spDirFileInput(spDir, path string) string {
	return `{"session_id":` + jsonQuote(testSessionID) + `,"scratchpad_dir":` + jsonQuote(spDir) +
		`,"tool_input":{"file_path":` + jsonQuote(path) + `}}`
}

func spDirCmdInput(spDir, command string) string {
	return `{"session_id":` + jsonQuote(testSessionID) + `,"scratchpad_dir":` + jsonQuote(spDir) +
		`,"tool_input":{"command":` + jsonQuote(command) + `}}`
}

func TestPathGuard_ScratchpadDir_Allowed(t *testing.T) {
	// Arbitrary location that does NOT fit the fallback shape — proves the
	// field alone is honoured.
	spDir := filepath.Join(t.TempDir(), "custom-scratch")
	code, msg := runHook(t, "path-guard.sh",
		spDirFileInput(spDir, filepath.Join(spDir, "issue_drafts.md")),
		agentEnv(map[string]string{"ZPIT_AGENT_TYPE": "clarifier"}))
	if code != 0 {
		t.Errorf("expected exit 0 inside scratchpad_dir, got %d: %s", code, msg)
	}
}

func TestPathGuard_ScratchpadDir_ExclusiveOverFallback(t *testing.T) {
	// Path fits the reconstructed fallback shape, but scratchpad_dir names a
	// different directory — the field wins, so this is blocked.
	root, sp := scratchpadRoot(t)
	spDir := filepath.Join(t.TempDir(), "real-scratch")
	code, msg := runHook(t, "path-guard.sh",
		spDirFileInput(spDir, filepath.Join(sp, "a.md")),
		scratchEnv(root, "clarifier"))
	if code != 2 {
		t.Errorf("expected exit 2 outside scratchpad_dir, got %d: %s", code, msg)
	}
}

func TestPathGuard_ScratchpadDir_TraversalBlocked(t *testing.T) {
	spDir := filepath.Join(t.TempDir(), "scratch")
	code, msg := runHook(t, "path-guard.sh",
		spDirFileInput(spDir, spDir+string(filepath.Separator)+filepath.Join("..", "escape.md")),
		agentEnv(map[string]string{"ZPIT_AGENT_TYPE": "clarifier"}))
	if code != 2 {
		t.Errorf("expected exit 2 for traversal out of scratchpad_dir, got %d: %s", code, msg)
	}
}

func TestPathGuard_ScratchpadDir_SiblingPrefixBlocked(t *testing.T) {
	// "<sp>-evil/x" shares a string prefix with "<sp>" but is not inside it.
	spDir := filepath.Join(t.TempDir(), "scratch")
	code, msg := runHook(t, "path-guard.sh",
		spDirFileInput(spDir, spDir+"-evil"+string(filepath.Separator)+"a.md"),
		agentEnv(map[string]string{"ZPIT_AGENT_TYPE": "clarifier"}))
	if code != 2 {
		t.Errorf("expected exit 2 for sibling-prefix dir, got %d: %s", code, msg)
	}
}

func TestBashFirewall_Clarifier_ScratchpadDir_RmAndSedAllowed(t *testing.T) {
	spDir := filepath.Join(t.TempDir(), "scratch")
	target := filepath.Join(spDir, "issue2.md")
	for _, command := range []string{
		`rm "` + target + `"`,
		`sed -i 's/12/14/' "` + target + `"`,
		`echo body > ` + filepath.ToSlash(target),
	} {
		code, msg := runHook(t, "bash-firewall.sh", spDirCmdInput(spDir, command), clarifierEnv(worktreeEnv))
		if code != 0 {
			t.Errorf("expected exit 0 for %q, got %d: %s", command, code, msg)
		}
	}
}

func TestPwshFirewall_Clarifier_ScratchpadDir_SetContentAllowed(t *testing.T) {
	spDir := filepath.Join(t.TempDir(), "scratch")
	code, msg := runHook(t, "pwsh-firewall.sh",
		spDirCmdInput(spDir, `Set-Content -Path `+filepath.Join(spDir, "draft.md")+` -Value 'body'`),
		clarifierEnv(worktreeEnv))
	if code != 0 {
		t.Errorf("expected exit 0 for Set-Content in scratchpad_dir, got %d: %s", code, msg)
	}
}
