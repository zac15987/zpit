package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// skipIfNoGit skips the test if git is not available in PATH.
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
}

// initFetchTestRepo creates a local repo backed by a bare remote suitable for FetchBranch tests.
// Returns (localRepoPath, bareRemotePath). The local repo has "origin" pointing to the bare
// remote with a "dev" branch and one initial commit already pushed and in sync.
func initFetchTestRepo(t *testing.T) (localDir, bareDir string) {
	t.Helper()

	// Create bare repo as origin.
	bareDir = t.TempDir()
	{
		cmd := exec.Command("git", "init", "--bare")
		cmd.Dir = bareDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bare repo init: %s: %v", out, err)
		}
	}

	// Clone bare repo to create local.
	localDir = filepath.Join(t.TempDir(), "local")
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
	// Then create and check out a separate "main" branch so that "dev" is a local
	// branch but not currently checked out — git refuses to update a checked-out
	// branch via `fetch origin dev:dev`, so the test must run with HEAD elsewhere.
	if err := os.WriteFile(filepath.Join(localDir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "checkout", "-b", "dev"},
		{"git", "add", "README.md"},
		{"git", "commit", "-m", "initial"},
		{"git", "push", "-u", "origin", "dev"},
		// Leave HEAD on a separate branch so "dev" is not checked out.
		{"git", "checkout", "-b", "main"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = localDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args[1:], out, err)
		}
	}

	return localDir, bareDir
}

func TestParseLocalBranches(t *testing.T) {
	tests := []struct {
		name        string
		branchesOut string
		currentName string
		want        []LocalBranch
	}{
		{
			name:        "empty input returns nil slice",
			branchesOut: "",
			currentName: "main",
			want:        nil,
		},
		{
			name:        "single branch marked as current",
			branchesOut: "main\torigin/main\n",
			currentName: "main",
			want: []LocalBranch{
				{Name: "main", IsCurrent: true, Upstream: "origin/main"},
			},
		},
		{
			name:        "single branch not current",
			branchesOut: "main\torigin/main\n",
			currentName: "dev",
			want: []LocalBranch{
				{Name: "main", IsCurrent: false, Upstream: "origin/main"},
			},
		},
		{
			name:        "branch with no upstream",
			branchesOut: "feature-x\t\n",
			currentName: "feature-x",
			want: []LocalBranch{
				{Name: "feature-x", IsCurrent: true, Upstream: ""},
			},
		},
		{
			name: "multiple branches with mixed upstream",
			branchesOut: "main\torigin/main\n" +
				"dev\torigin/dev\n" +
				"feat/89-foo\t\n",
			currentName: "dev",
			want: []LocalBranch{
				{Name: "main", IsCurrent: false, Upstream: "origin/main"},
				{Name: "dev", IsCurrent: true, Upstream: "origin/dev"},
				{Name: "feat/89-foo", IsCurrent: false, Upstream: ""},
			},
		},
		{
			name:        "detached HEAD (empty currentName)",
			branchesOut: "main\torigin/main\ndev\torigin/dev\n",
			currentName: "",
			want: []LocalBranch{
				{Name: "main", IsCurrent: false, Upstream: "origin/main"},
				{Name: "dev", IsCurrent: false, Upstream: "origin/dev"},
			},
		},
		{
			name:        "whitespace-only lines are skipped",
			branchesOut: "  \n\nmain\torigin/main\n  \n",
			currentName: "main",
			want: []LocalBranch{
				{Name: "main", IsCurrent: true, Upstream: "origin/main"},
			},
		},
		{
			name:        "no trailing newline",
			branchesOut: "main\torigin/main",
			currentName: "main",
			want: []LocalBranch{
				{Name: "main", IsCurrent: true, Upstream: "origin/main"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLocalBranches(tc.branchesOut, tc.currentName)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseLocalBranches() =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

func TestParseRemoteOnly(t *testing.T) {
	tests := []struct {
		name       string
		remotesOut string
		localNames map[string]bool
		want       []string
	}{
		{
			name:       "empty input returns nil slice",
			remotesOut: "",
			localNames: map[string]bool{},
			want:       nil,
		},
		{
			name:       "filters out origin/HEAD",
			remotesOut: "origin/HEAD\norigin/feature-a\n",
			localNames: map[string]bool{},
			want:       []string{"origin/feature-a"},
		},
		{
			name:       "filters out refs matching local branches",
			remotesOut: "origin/main\norigin/dev\norigin/feature-a\n",
			localNames: map[string]bool{"main": true, "dev": true},
			want:       []string{"origin/feature-a"},
		},
		{
			name:       "returns sorted alphabetically",
			remotesOut: "origin/zulu\norigin/alpha\norigin/mike\n",
			localNames: map[string]bool{},
			want:       []string{"origin/alpha", "origin/mike", "origin/zulu"},
		},
		{
			name:       "all branches excluded returns nil slice",
			remotesOut: "origin/HEAD\norigin/main\n",
			localNames: map[string]bool{"main": true},
			want:       nil,
		},
		{
			name:       "multiple remotes with different prefixes",
			remotesOut: "origin/feature-a\nupstream/feature-b\n",
			localNames: map[string]bool{},
			want:       []string{"origin/feature-a", "upstream/feature-b"},
		},
		{
			name:       "ref without slash is skipped",
			remotesOut: "badref\norigin/good\n",
			localNames: map[string]bool{},
			want:       []string{"origin/good"},
		},
		{
			name: "filters local match across different remotes",
			remotesOut: "origin/main\nupstream/main\norigin/feature-x\n",
			localNames: map[string]bool{"main": true},
			want:       []string{"origin/feature-x"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRemoteOnly(tc.remotesOut, tc.localNames)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseRemoteOnly() =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}

func TestParseAheadBehind(t *testing.T) {
	// Note: parseAheadBehind treats the first field as "upstream ahead" (our behind)
	// and the second field as "our ahead". So "3\t2" means ahead=2, behind=3.
	tests := []struct {
		name       string
		revListOut string
		wantAhead  int
		wantBehind int
		wantErr    bool
	}{
		{
			name:       "well-formed input with trailing newline",
			revListOut: "3\t2\n",
			wantAhead:  2,
			wantBehind: 3,
			wantErr:    false,
		},
		{
			name:       "zero values",
			revListOut: "0\t0\n",
			wantAhead:  0,
			wantBehind: 0,
			wantErr:    false,
		},
		{
			name:       "missing trailing newline",
			revListOut: "3\t2",
			wantAhead:  2,
			wantBehind: 3,
			wantErr:    false,
		},
		{
			name:       "empty input returns zero values and no error",
			revListOut: "",
			wantAhead:  0,
			wantBehind: 0,
			wantErr:    false,
		},
		{
			name:       "whitespace-only input returns zero values and no error",
			revListOut: "   \n",
			wantAhead:  0,
			wantBehind: 0,
			wantErr:    false,
		},
		{
			name:       "malformed input no tab separator",
			revListOut: "abc",
			wantErr:    true,
		},
		{
			name:       "non-numeric first field",
			revListOut: "abc\t2",
			wantErr:    true,
		},
		{
			name:       "non-numeric second field",
			revListOut: "3\tabc",
			wantErr:    true,
		},
		{
			name:       "too many fields",
			revListOut: "1\t2\t3",
			wantErr:    true,
		},
		{
			name:       "large values",
			revListOut: "100\t200\n",
			wantAhead:  200,
			wantBehind: 100,
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ahead, behind, err := parseAheadBehind(tc.revListOut)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseAheadBehind(%q) expected error, got ahead=%d behind=%d",
						tc.revListOut, ahead, behind)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAheadBehind(%q) unexpected error: %v", tc.revListOut, err)
			}
			if ahead != tc.wantAhead {
				t.Errorf("ahead = %d, want %d", ahead, tc.wantAhead)
			}
			if behind != tc.wantBehind {
				t.Errorf("behind = %d, want %d", behind, tc.wantBehind)
			}
		})
	}
}

// gitRevParse runs `git rev-parse <ref>` in dir and returns the trimmed SHA.
func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestFetchBranch(t *testing.T) {
	skipIfNoGit(t)

	t.Run("fast-forward: remote has new commit, local ref advances to match origin/dev", func(t *testing.T) {
		localDir, bareDir := initFetchTestRepo(t)
		ctx := context.Background()

		// Clone bare repo into a second working copy and push a new commit to origin/dev.
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
		if err := os.WriteFile(filepath.Join(otherDir, "new-feature.txt"), []byte("new work"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"git", "add", "new-feature.txt"},
			{"git", "commit", "-m", "add new feature after merge"},
			{"git", "push", "origin", "dev"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = otherDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("other git %v: %s: %v", args[1:], out, err)
			}
		}

		// Local repo has not fetched yet — FetchBranch should update the local dev ref.
		stdout, stderr, err := FetchBranch(ctx, localDir, "dev")
		if err != nil {
			t.Fatalf("FetchBranch returned error: %v (stdout=%q stderr=%q)", err, stdout, stderr)
		}

		// Verify local dev ref now matches origin/dev exactly.
		devSHA := gitRevParse(t, localDir, "dev")
		originDevSHA := gitRevParse(t, localDir, "origin/dev")
		if devSHA != originDevSHA {
			t.Errorf("after fetch: dev=%s != origin/dev=%s", devSHA, originDevSHA)
		}
	})

	t.Run("no new commits: remote and local are in sync, returns nil error", func(t *testing.T) {
		localDir, _ := initFetchTestRepo(t)
		ctx := context.Background()

		// Local and remote are already in sync after initFetchTestRepo.
		_, _, err := FetchBranch(ctx, localDir, "dev")
		if err != nil {
			t.Fatalf("FetchBranch returned error on already-synced branch: %v", err)
		}
	})

	t.Run("diverged: local dev has commit not in remote, returns non-nil error and leaves ref unchanged", func(t *testing.T) {
		localDir, _ := initFetchTestRepo(t)
		ctx := context.Background()

		// Create a local commit on dev that is not pushed to the bare remote.
		for _, args := range [][]string{
			{"git", "checkout", "dev"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = localDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s: %v", args[1:], out, err)
			}
		}
		if err := os.WriteFile(filepath.Join(localDir, "local-only.txt"), []byte("local work"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"git", "add", "local-only.txt"},
			{"git", "commit", "-m", "diverge: local-only commit"},
			// Return to main so dev is not checked out during FetchBranch.
			{"git", "checkout", "main"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = localDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s: %v", args[1:], out, err)
			}
		}

		// Capture local dev SHA before the (expected-to-fail) fetch.
		beforeSHA := gitRevParse(t, localDir, "dev")

		// FetchBranch must return non-nil err: git refuses a non-fast-forward update.
		_, _, fetchErr := FetchBranch(ctx, localDir, "dev")
		if fetchErr == nil {
			t.Fatal("FetchBranch expected non-nil error on diverged branch, got nil")
		}

		// Local dev ref must remain unchanged after the rejection.
		afterSHA := gitRevParse(t, localDir, "dev")
		if afterSHA != beforeSHA {
			t.Errorf("local dev ref changed after rejected fetch: before=%s after=%s", beforeSHA, afterSHA)
		}
	})

	t.Run("head-on-branch: dev is currently checked out, propagates git exit status", func(t *testing.T) {
		localDir, _ := initFetchTestRepo(t)
		ctx := context.Background()

		// Check out dev so HEAD points at it; git refuses to update a checked-out branch.
		checkoutCmd := exec.Command("git", "checkout", "dev")
		checkoutCmd.Dir = localDir
		if out, err := checkoutCmd.CombinedOutput(); err != nil {
			t.Fatalf("git checkout dev: %s: %v", out, err)
		}

		// FetchBranch must propagate git's non-zero exit: no panic, err != nil.
		stdout, stderr, err := FetchBranch(ctx, localDir, "dev")
		if err == nil {
			t.Fatalf("FetchBranch expected non-nil error when dev is checked out, got nil (stdout=%q stderr=%q)", stdout, stderr)
		}
	})

	t.Run("branch does not exist on remote: returns non-nil error", func(t *testing.T) {
		localDir, _ := initFetchTestRepo(t)
		ctx := context.Background()

		_, _, err := FetchBranch(ctx, localDir, "nonexistent-branch")
		if err == nil {
			t.Fatal("FetchBranch expected non-nil error for nonexistent remote branch, got nil")
		}
	})
}

// advanceRemoteDev clones bareDir into a fresh temp directory, adds one commit on dev,
// and pushes it back to bareDir. Returns nothing; the bare remote's dev branch is advanced.
func advanceRemoteDev(t *testing.T, bareDir, filename, content string) {
	t.Helper()
	otherDir := filepath.Join(t.TempDir(), "other")
	cmd := exec.Command("git", "clone", bareDir, otherDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone other: %s: %v", out, err)
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
	if err := os.WriteFile(filepath.Join(otherDir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", filename},
		{"git", "commit", "-m", "advance: " + filename},
		{"git", "push", "origin", "dev"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = otherDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("other git %v: %s: %v", args[1:], out, err)
		}
	}
}

// gitRun runs a git command in cwd and fails the test on non-zero exit.
func gitRun(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s: %v", args, cwd, out, err)
	}
}

func TestSyncLocalBranch(t *testing.T) {
	skipIfNoGit(t)

	t.Run("head-on-base-fast-forward: pull --ff-only advances checked-out dev", func(t *testing.T) {
		localDir, bareDir := initFetchTestRepo(t)
		ctx := context.Background()

		// Put HEAD on dev so the pull path is exercised.
		gitRun(t, localDir, "checkout", "dev")

		advanceRemoteDev(t, bareDir, "new-feature.txt", "new work")

		_, _, err := SyncLocalBranch(ctx, localDir, "dev")
		if err != nil {
			t.Fatalf("SyncLocalBranch returned error: %v", err)
		}

		devSHA := gitRevParse(t, localDir, "dev")
		originDevSHA := gitRevParse(t, localDir, "origin/dev")
		if devSHA != originDevSHA {
			t.Errorf("after sync: dev=%s != origin/dev=%s", devSHA, originDevSHA)
		}
	})

	t.Run("head-on-base-already-in-sync: returns nil error", func(t *testing.T) {
		localDir, _ := initFetchTestRepo(t)
		ctx := context.Background()

		gitRun(t, localDir, "checkout", "dev")

		_, _, err := SyncLocalBranch(ctx, localDir, "dev")
		if err != nil {
			t.Fatalf("SyncLocalBranch returned error on already-synced branch: %v", err)
		}
	})

	t.Run("head-on-base-diverged: non-fast-forward returns error with stderr in message", func(t *testing.T) {
		localDir, bareDir := initFetchTestRepo(t)
		ctx := context.Background()

		// Put HEAD on dev and add a local-only commit.
		gitRun(t, localDir, "checkout", "dev")
		if err := os.WriteFile(filepath.Join(localDir, "local-only.txt"), []byte("local work"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, localDir, "add", "local-only.txt")
		gitRun(t, localDir, "commit", "-m", "diverge: local-only commit")

		// Push a conflicting commit to remote so dev is truly diverged.
		advanceRemoteDev(t, bareDir, "remote-only.txt", "remote work")

		beforeSHA := gitRevParse(t, localDir, "dev")
		_, _, err := SyncLocalBranch(ctx, localDir, "dev")
		if err == nil {
			t.Fatal("SyncLocalBranch expected non-nil error on diverged branch, got nil")
		}
		if !strings.Contains(err.Error(), "git pull") {
			t.Errorf("error message missing %q: got %q", "git pull", err.Error())
		}
		// Error should include git's stderr fragment beyond bare "exit status N".
		// Git's non-ff message varies by version but typically contains "fast-forward" or "diverged".
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "fast-forward") && !strings.Contains(lower, "diverg") && !strings.Contains(lower, "rejected") {
			t.Errorf("error message lacks stderr diagnostic fragment: %q", err.Error())
		}

		// Local dev ref must remain unchanged after the rejected pull.
		afterSHA := gitRevParse(t, localDir, "dev")
		if afterSHA != beforeSHA {
			t.Errorf("local dev ref changed after rejected pull: before=%s after=%s", beforeSHA, afterSHA)
		}
	})

	t.Run("head-on-other-branch: delegates to fetch refspec and updates dev ref", func(t *testing.T) {
		localDir, bareDir := initFetchTestRepo(t)
		ctx := context.Background()

		// HEAD is already on main after initFetchTestRepo — leave it there.
		advanceRemoteDev(t, bareDir, "new-feature.txt", "new work")

		_, _, err := SyncLocalBranch(ctx, localDir, "dev")
		if err != nil {
			t.Fatalf("SyncLocalBranch returned error: %v", err)
		}

		devSHA := gitRevParse(t, localDir, "dev")
		originDevSHA := gitRevParse(t, localDir, "origin/dev")
		if devSHA != originDevSHA {
			t.Errorf("after sync: dev=%s != origin/dev=%s", devSHA, originDevSHA)
		}
	})

	t.Run("detached-head: delegates to fetch refspec and updates dev ref", func(t *testing.T) {
		localDir, bareDir := initFetchTestRepo(t)
		ctx := context.Background()

		// Detach HEAD from any branch.
		headSHA := gitRevParse(t, localDir, "HEAD")
		gitRun(t, localDir, "checkout", headSHA)

		advanceRemoteDev(t, bareDir, "new-feature.txt", "new work")

		_, _, err := SyncLocalBranch(ctx, localDir, "dev")
		if err != nil {
			t.Fatalf("SyncLocalBranch returned error on detached HEAD: %v", err)
		}

		devSHA := gitRevParse(t, localDir, "dev")
		originDevSHA := gitRevParse(t, localDir, "origin/dev")
		if devSHA != originDevSHA {
			t.Errorf("after sync: dev=%s != origin/dev=%s", devSHA, originDevSHA)
		}
	})

	t.Run("nonexistent-branch: error includes stderr fragment", func(t *testing.T) {
		localDir, _ := initFetchTestRepo(t)
		ctx := context.Background()

		_, _, err := SyncLocalBranch(ctx, localDir, "nonexistent-branch")
		if err == nil {
			t.Fatal("SyncLocalBranch expected non-nil error for nonexistent remote branch, got nil")
		}
		// HEAD is on main, so this goes through the fetch path — error prefix should reflect that.
		if !strings.Contains(err.Error(), "git fetch") {
			t.Errorf("error message missing %q: got %q", "git fetch", err.Error())
		}
		// Should include git's stderr diagnostic, not just "exit status N".
		if !strings.Contains(err.Error(), "nonexistent-branch") && !strings.Contains(strings.ToLower(err.Error()), "couldn't find") && !strings.Contains(strings.ToLower(err.Error()), "not found") {
			t.Errorf("error message lacks stderr diagnostic: %q", err.Error())
		}
	})
}
