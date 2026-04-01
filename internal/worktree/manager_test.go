package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zac15987/zpit/internal/config"
)

// initTestRepo creates a temporary git repo with an initial commit and a bare remote (origin).
// The local repo has a "dev" branch pushed to origin, so Create() can fetch from it.
func initTestRepo(t *testing.T) string {
	t.Helper()
	localDir, _ := initTestRepoWithRemote(t)
	return localDir
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	wtBase := t.TempDir()
	cfg := config.WorktreeConfig{
		DirFormat:     "{project_id}/{issue_id}--{slug}",
		MaxPerProject: 3,
	}
	if runtime.GOOS == "windows" {
		cfg.BaseDirWindows = wtBase
	} else {
		cfg.BaseDirWSL = wtBase
	}
	return NewManager(cfg)
}

func TestResolvePath(t *testing.T) {
	mgr := testManager(t)
	path := mgr.ResolvePath("myproject", "ASE-47", "ethercat-reconnect")
	if !strings.Contains(path, "myproject") {
		t.Errorf("path should contain project_id: %s", path)
	}
	if !strings.Contains(path, "ASE-47--ethercat-reconnect") {
		t.Errorf("path should contain issue_id--slug: %s", path)
	}
}

func TestCreateAndList(t *testing.T) {
	skipIfNoGit(t)
	repo := initTestRepo(t)
	mgr := testManager(t)

	wtPath, err := mgr.Create(CreateParams{
		RepoPath: repo, BaseBranch: "dev", BranchName: "feat/ASE-47-reconnect",
		ProjectID: "proj", IssueID: "ASE-47", Slug: "reconnect",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify worktree directory exists.
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree dir not found: %v", err)
	}

	// Verify List returns it.
	worktrees, err := mgr.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(worktrees) != 1 {
		t.Fatalf("List = %d worktrees, want 1", len(worktrees))
	}
	if worktrees[0].Branch != "feat/ASE-47-reconnect" {
		t.Errorf("Branch = %q", worktrees[0].Branch)
	}
}

func TestRemove(t *testing.T) {
	skipIfNoGit(t)
	repo := initTestRepo(t)
	mgr := testManager(t)

	wtPath, err := mgr.Create(CreateParams{
		RepoPath: repo, BaseBranch: "dev", BranchName: "feat/ASE-48-vision",
		ProjectID: "proj", IssueID: "ASE-48", Slug: "vision",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Remove(repo, wtPath, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Worktree should be gone.
	worktrees, err := mgr.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(worktrees) != 0 {
		t.Errorf("List = %d worktrees after remove, want 0", len(worktrees))
	}

	// Branch should be gone.
	out, _ := runGit(repo, "branch", "--list", "feat/ASE-48-vision")
	if strings.TrimSpace(out) != "" {
		t.Errorf("branch still exists after remove: %s", out)
	}
}

func TestMaxPerProject(t *testing.T) {
	skipIfNoGit(t)
	repo := initTestRepo(t)
	mgr := testManager(t)

	// Create up to max (3).
	for i := 1; i <= 3; i++ {
		branch := fmt.Sprintf("feat/ISSUE-%d-test", i)
		issueID := fmt.Sprintf("ISSUE-%d", i)
		if _, err := mgr.Create(CreateParams{
			RepoPath: repo, BaseBranch: "dev", BranchName: branch,
			ProjectID: "proj", IssueID: issueID, Slug: "test",
		}); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
	}

	// 4th should fail.
	_, err := mgr.Create(CreateParams{
		RepoPath: repo, BaseBranch: "dev", BranchName: "feat/ISSUE-4-test",
		ProjectID: "proj", IssueID: "ISSUE-4", Slug: "test",
	})
	if err == nil {
		t.Fatal("expected error for exceeding max worktrees")
	}
	if !strings.Contains(err.Error(), "max worktrees") {
		t.Errorf("unexpected error: %v", err)
	}
}

// initTestRepoWithRemote creates a local repo backed by a bare remote.
// Returns (localRepoPath, bareRemotePath). The local repo has "origin" pointing
// to the bare remote with a "dev" branch and one initial commit.
func initTestRepoWithRemote(t *testing.T) (string, string) {
	t.Helper()

	// Create bare repo as origin.
	bareDir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", "--bare"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = bareDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bare repo git %v: %s: %v", args[1:], out, err)
		}
	}

	// Clone bare repo to create local.
	localDir := filepath.Join(t.TempDir(), "local")
	{
		cmd := exec.Command("git", "clone", bareDir, localDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("clone: %s: %v", out, err)
		}
	}

	// Configure user in local.
	for _, args := range [][]string{
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = localDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args[1:], out, err)
		}
	}

	// Create dev branch with initial commit and push.
	if err := os.WriteFile(filepath.Join(localDir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "checkout", "-b", "dev"},
		{"git", "add", "README.md"},
		{"git", "commit", "-m", "initial"},
		{"git", "push", "-u", "origin", "dev"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = localDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args[1:], out, err)
		}
	}

	return localDir, bareDir
}

func TestCreateFetchesRemote(t *testing.T) {
	skipIfNoGit(t)
	localDir, bareDir := initTestRepoWithRemote(t)
	mgr := testManager(t)

	// Simulate a dependency issue's code being merged into remote dev:
	// clone the bare repo into a second working copy, commit, and push.
	otherDir := filepath.Join(t.TempDir(), "other")
	{
		cmd := exec.Command("git", "clone", bareDir, otherDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("clone other: %s: %v", out, err)
		}
	}
	for _, args := range [][]string{
		{"git", "config", "user.email", "other@test.com"},
		{"git", "config", "user.name", "Other"},
		{"git", "checkout", "dev"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = otherDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("other git %v: %s: %v", args[1:], out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(otherDir, "dep.txt"), []byte("dependency code"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "dep.txt"},
		{"git", "commit", "-m", "add dependency code"},
		{"git", "push", "origin", "dev"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = otherDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("other git %v: %s: %v", args[1:], out, err)
		}
	}

	// Local repo hasn't fetched yet — dep.txt should not be in local dev.
	// Now Create should fetch and base the new branch on origin/dev.
	wtPath, err := mgr.Create(CreateParams{
		RepoPath:   localDir,
		BaseBranch: "dev",
		BranchName: "feat/99-test-fetch",
		ProjectID:  "proj",
		IssueID:    "99",
		Slug:       "test-fetch",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify dep.txt exists in the worktree (proves fetch + origin/dev base worked).
	depFile := filepath.Join(wtPath, "dep.txt")
	if _, err := os.Stat(depFile); os.IsNotExist(err) {
		t.Fatalf("dep.txt not found in worktree — fetch or origin/dev base failed")
	}
	content, err := os.ReadFile(depFile)
	if err != nil {
		t.Fatalf("reading dep.txt: %v", err)
	}
	if string(content) != "dependency code" {
		t.Errorf("dep.txt content = %q, want %q", string(content), "dependency code")
	}
}

// initTestRepoNoRemote creates a temporary git repo with an initial commit but no remote.
func initTestRepoNoRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "dev"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args[1:], out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "README.md"},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args[1:], out, err)
		}
	}
	return dir
}

func TestCreateFetchFailure(t *testing.T) {
	skipIfNoGit(t)
	// Use a repo with no remote — fetch should fail and Create should return error.
	repo := initTestRepoNoRemote(t)
	mgr := testManager(t)

	_, err := mgr.Create(CreateParams{
		RepoPath:   repo,
		BaseBranch: "dev",
		BranchName: "feat/100-no-remote",
		ProjectID:  "proj",
		IssueID:    "100",
		Slug:       "no-remote",
	})
	if err == nil {
		t.Fatal("expected error when fetch fails (no remote)")
	}
	if !strings.Contains(err.Error(), "fetching origin/dev") {
		t.Errorf("error = %v, want error about fetching origin/dev", err)
	}
}

func TestParseWorktreeList(t *testing.T) {
	output := `worktree /repo
HEAD abc123
branch refs/heads/dev

worktree /worktrees/proj/ASE-47--reconnect
HEAD def456
branch refs/heads/feat/ASE-47-reconnect

worktree /worktrees/proj/ASE-48--vision
HEAD 789abc
branch refs/heads/fix/ASE-48-vision

`
	result := parseWorktreeList(output, "/repo")
	if len(result) != 2 {
		t.Fatalf("got %d worktrees, want 2", len(result))
	}
	if result[0].Branch != "feat/ASE-47-reconnect" {
		t.Errorf("result[0].Branch = %q", result[0].Branch)
	}
	if result[1].Branch != "fix/ASE-48-vision" {
		t.Errorf("result[1].Branch = %q", result[1].Branch)
	}
}
