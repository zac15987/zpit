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

// initTestRepo creates a temporary git repo with an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "dev"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args[1:], out, err)
		}
	}

	// Create a file and commit so branches can be created.
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

	wtPath, err := mgr.Create(repo, "dev", "feat/ASE-47-reconnect", "proj", "ASE-47", "reconnect")
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

	wtPath, err := mgr.Create(repo, "dev", "feat/ASE-48-vision", "proj", "ASE-48", "vision")
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
		if _, err := mgr.Create(repo, "dev", branch, "proj", issueID, "test"); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
	}

	// 4th should fail.
	_, err := mgr.Create(repo, "dev", "feat/ISSUE-4-test", "proj", "ISSUE-4", "test")
	if err == nil {
		t.Fatal("expected error for exceeding max worktrees")
	}
	if !strings.Contains(err.Error(), "max worktrees") {
		t.Errorf("unexpected error: %v", err)
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
