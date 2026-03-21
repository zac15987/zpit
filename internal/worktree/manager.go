package worktree

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
)

// WorktreeInfo describes an active worktree.
type WorktreeInfo struct {
	Path   string // absolute path to worktree directory
	Branch string // branch name, e.g. "feat/ASE-47-ethercat-reconnect"
}

// Manager handles git worktree lifecycle operations.
type Manager struct {
	cfg config.WorktreeConfig
}

// NewManager creates a Manager with the given worktree config.
func NewManager(cfg config.WorktreeConfig) *Manager {
	return &Manager{cfg: cfg}
}

// ResolvePath computes the worktree directory path from config template.
func (m *Manager) ResolvePath(projectID, issueID, slug string) string {
	baseDir := platform.ResolvePath(m.cfg.BaseDirWindows, m.cfg.BaseDirWSL)
	subpath := m.cfg.DirFormat
	subpath = strings.ReplaceAll(subpath, "{project_id}", projectID)
	subpath = strings.ReplaceAll(subpath, "{issue_id}", issueID)
	subpath = strings.ReplaceAll(subpath, "{slug}", slug)
	return filepath.Join(baseDir, subpath)
}

// Create creates a new branch from baseBranch and a worktree for it.
// Returns the absolute path to the created worktree.
func (m *Manager) Create(repoPath, baseBranch, branchName, projectID, issueID, slug string) (string, error) {
	// Check max worktree limit.
	existing, err := m.List(repoPath)
	if err != nil {
		return "", fmt.Errorf("listing worktrees: %w", err)
	}
	if m.cfg.MaxPerProject > 0 && len(existing) >= m.cfg.MaxPerProject {
		return "", fmt.Errorf("max worktrees per project reached (%d)", m.cfg.MaxPerProject)
	}

	wtPath := m.ResolvePath(projectID, issueID, slug)

	// Create branch from baseBranch.
	if _, err := runGit(repoPath, "branch", branchName, baseBranch); err != nil {
		return "", fmt.Errorf("creating branch %s: %w", branchName, err)
	}

	// Create worktree.
	if _, err := runGit(repoPath, "worktree", "add", wtPath, branchName); err != nil {
		// Clean up branch on failure.
		_, _ = runGit(repoPath, "branch", "-D", branchName)
		return "", fmt.Errorf("creating worktree: %w", err)
	}

	return wtPath, nil
}

// Remove removes a worktree and optionally deletes the associated branch.
func (m *Manager) Remove(repoPath, worktreePath string, deleteBranch bool) error {
	// Find branch name before removing worktree.
	var branchName string
	if deleteBranch {
		worktrees, err := m.List(repoPath)
		if err == nil {
			normalized := normalizePath(worktreePath)
			for _, wt := range worktrees {
				if normalizePath(wt.Path) == normalized {
					branchName = wt.Branch
					break
				}
			}
		}
	}

	if _, err := runGit(repoPath, "worktree", "remove", worktreePath); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}

	if deleteBranch && branchName != "" {
		if _, err := runGit(repoPath, "branch", "-d", branchName); err != nil {
			return fmt.Errorf("deleting branch %s: %w", branchName, err)
		}
	}

	return nil
}

// List returns all non-main worktrees for a given project repository.
func (m *Manager) List(repoPath string) ([]WorktreeInfo, error) {
	out, err := runGit(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(out, repoPath), nil
}

// parseWorktreeList parses `git worktree list --porcelain` output.
// Skips the main worktree (whose path matches repoPath).
func parseWorktreeList(output, repoPath string) []WorktreeInfo {
	normalizedRepo := normalizePath(repoPath)
	var result []WorktreeInfo
	var current WorktreeInfo

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			// Block separator — flush current.
			if current.Path != "" && normalizePath(current.Path) != normalizedRepo && current.Branch != "" {
				result = append(result, current)
			}
			current = WorktreeInfo{}
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	// Flush last block.
	if current.Path != "" && normalizePath(current.Path) != normalizedRepo && current.Branch != "" {
		result = append(result, current)
	}

	return result
}

// normalizePath returns a cleaned, forward-slash path for cross-platform comparison.
func normalizePath(p string) string {
	return strings.ToLower(filepath.Clean(p))
}

// runGit executes a git command in the given directory and returns stdout.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}
