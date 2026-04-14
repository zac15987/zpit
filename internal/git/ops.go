package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// BranchInfo bundles the results of parsing local and remote branches.
type BranchInfo struct {
	Local      []LocalBranch
	RemoteOnly []string      // sorted alphabetically; "origin/HEAD" filtered out
	Detached   *DetachedHead // non-nil if HEAD is detached (no branch checked out)
}

// LocalBranch describes one local branch.
type LocalBranch struct {
	Name      string // e.g. "main", "feat/89-foo"
	IsCurrent bool   // true if this is HEAD
	Upstream  string // e.g. "origin/main", or "" if no upstream
	Ahead     int    // commits ahead of upstream (0 if no upstream)
	Behind    int    // commits behind upstream
}

// DetachedHead represents a detached HEAD state.
type DetachedHead struct {
	ShortHash string // e.g. "a1b2c3d"
}

// Fetch runs `git fetch --all --prune` in cwd. Returns stderr output on non-zero exit.
func Fetch(ctx context.Context, cwd string) (stdout, stderr string, err error) {
	return runGitSeparate(ctx, cwd, "fetch", "--all", "--prune")
}

// Pull runs `git pull --ff-only` in cwd.
func Pull(ctx context.Context, cwd string) (stdout, stderr string, err error) {
	return runGitSeparate(ctx, cwd, "pull", "--ff-only")
}

// LogGraph runs `git log --graph --oneline --all --decorate --color=always -n 50`.
// Returns the raw (ANSI-colored) stdout. If stderr contains "does not have any commits yet"
// or exit code indicates no commits, return ("", nil) so the caller can render a sentinel.
func LogGraph(ctx context.Context, cwd string) (string, error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "log", "--graph", "--oneline", "--all", "--decorate", "--color=always", "-n", "50")
	cmd.Dir = cwd
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		se := stderrBuf.String()
		if strings.Contains(se, "does not have any commits yet") {
			return "", nil
		}
		// Some git versions exit non-zero for empty repos without the specific message.
		// Treat empty stdout with an error as "no commits".
		if stdoutBuf.Len() == 0 {
			return "", nil
		}
		return "", fmt.Errorf("git log: %w: %s", err, se)
	}
	return stdoutBuf.String(), nil
}

// Branches inspects local + remote refs and returns structured data.
func Branches(ctx context.Context, cwd string) (BranchInfo, error) {
	var info BranchInfo

	// Determine current branch (or detect detached HEAD).
	currentBranch, err := currentBranchName(ctx, cwd)
	if err != nil {
		return info, err
	}
	if currentBranch == "" {
		// Detached HEAD: get the short hash.
		hash, hashErr := shortHEAD(ctx, cwd)
		if hashErr != nil {
			return info, hashErr
		}
		info.Detached = &DetachedHead{ShortHash: hash}
	}

	// List local branches with upstream info.
	branchesOut, _, err := runGitSeparate(ctx, cwd,
		"for-each-ref", "refs/heads/",
		"--format=%(refname:short)\t%(upstream:short)")
	if err != nil {
		return info, fmt.Errorf("list local branches: %w", err)
	}
	info.Local = parseLocalBranches(branchesOut, currentBranch)

	// Fill in ahead/behind counts per branch that has an upstream.
	for i := range info.Local {
		lb := &info.Local[i]
		if lb.Upstream == "" {
			continue
		}
		revOut, _, revErr := runGitSeparate(ctx, cwd,
			"rev-list", "--left-right", "--count",
			lb.Upstream+"..."+lb.Name)
		if revErr != nil {
			// If rev-list fails (e.g. upstream ref is gone), skip silently.
			continue
		}
		ahead, behind, parseErr := parseAheadBehind(revOut)
		if parseErr != nil {
			continue
		}
		lb.Ahead = ahead
		lb.Behind = behind
	}

	// List remote refs.
	remotesOut, _, err := runGitSeparate(ctx, cwd,
		"for-each-ref", "refs/remotes/",
		"--format=%(refname:short)")
	if err != nil {
		return info, fmt.Errorf("list remote branches: %w", err)
	}

	localNames := make(map[string]bool, len(info.Local))
	for _, lb := range info.Local {
		localNames[lb.Name] = true
	}
	info.RemoteOnly = parseRemoteOnly(remotesOut, localNames)

	return info, nil
}

// currentBranchName returns the current branch name, or "" if HEAD is detached.
func currentBranchName(ctx context.Context, cwd string) (string, error) {
	stdout, stderr, err := runGitSeparate(ctx, cwd, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		// Exit code 128 means detached HEAD — not an error for our purposes.
		if isExitCode(err, 128) {
			return "", nil
		}
		// Exit code 1 also occurs on some git versions for detached HEAD.
		if isExitCode(err, 1) && strings.Contains(stderr, "not a symbolic ref") {
			return "", nil
		}
		return "", fmt.Errorf("git symbolic-ref: %w: %s", err, stderr)
	}
	return strings.TrimSpace(stdout), nil
}

// shortHEAD returns the short hash of HEAD.
func shortHEAD(ctx context.Context, cwd string) (string, error) {
	stdout, stderr, err := runGitSeparate(ctx, cwd, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --short HEAD: %w: %s", err, stderr)
	}
	return strings.TrimSpace(stdout), nil
}

// --- Internal parsers (pure functions, no I/O) ---

// parseLocalBranches parses `git for-each-ref refs/heads/` output with NUL-separated fields.
// Each line has format: "branchName\tupstream" (upstream may be empty).
// currentName is the name of the current branch (empty string if detached).
func parseLocalBranches(branchesOut string, currentName string) []LocalBranch {
	var result []LocalBranch
	for _, line := range strings.Split(branchesOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		lb := LocalBranch{
			Name:      name,
			IsCurrent: name == currentName,
		}
		if len(parts) == 2 {
			lb.Upstream = strings.TrimSpace(parts[1])
		}
		result = append(result, lb)
	}
	return result
}

// parseRemoteOnly filters remote ref names to only those that are "remote-only".
// It excludes "origin/HEAD" and any ref whose name (after stripping the first "remote/"
// prefix, e.g. "origin/") matches a local branch name. Returns a sorted slice.
func parseRemoteOnly(remotesOut string, localNames map[string]bool) []string {
	var result []string
	for _, line := range strings.Split(remotesOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Filter origin/HEAD.
		if line == "origin/HEAD" {
			continue
		}
		// Strip the remote prefix (everything up to and including the first "/").
		idx := strings.Index(line, "/")
		if idx < 0 {
			continue
		}
		shortName := line[idx+1:]
		if localNames[shortName] {
			continue
		}
		result = append(result, line)
	}
	sort.Strings(result)
	return result
}

// parseAheadBehind parses output from `git rev-list --left-right --count a...b`.
// Output is a single line like "3\t2" (ahead\tbehind from the left side's perspective).
// Note: --left-right --count with upstream...branch gives upstream-ahead \t branch-ahead.
// So the first number is how many commits the upstream is ahead (= branch is behind),
// and the second is how many the branch is ahead.
func parseAheadBehind(revListOut string) (ahead, behind int, err error) {
	line := strings.TrimSpace(revListOut)
	if line == "" {
		return 0, 0, nil
	}
	parts := strings.Split(line, "\t")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %q", line)
	}
	// upstream...branch: first field = upstream ahead (= our behind), second = our ahead.
	behind64, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind count: %w", err)
	}
	ahead64, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead count: %w", err)
	}
	return int(ahead64), int(behind64), nil
}

// --- Helpers ---

// runGitSeparate runs a git command with separate stdout/stderr capture.
func runGitSeparate(ctx context.Context, cwd string, args ...string) (stdout, stderr string, err error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// isExitCode checks whether err is an *exec.ExitError with the given code.
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == code
	}
	return false
}
