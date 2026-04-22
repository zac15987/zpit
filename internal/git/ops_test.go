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

func TestFetchBranch(t *testing.T) {
	skipIfNoGit(t)

	t.Run("success: remote has new commit, local ref is updated", func(t *testing.T) {
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

		// Verify the new commit appears in the local dev ref.
		cmd := exec.Command("git", "log", "--oneline", "-1", "dev")
		cmd.Dir = localDir
		out, logErr := cmd.CombinedOutput()
		if logErr != nil {
			t.Fatalf("git log dev: %s: %v", out, logErr)
		}
		if !strings.Contains(string(out), "add new feature after merge") {
			t.Errorf("local dev log = %q, expected to contain %q", string(out), "add new feature after merge")
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

	t.Run("branch does not exist on remote: returns non-nil error", func(t *testing.T) {
		localDir, _ := initFetchTestRepo(t)
		ctx := context.Background()

		_, _, err := FetchBranch(ctx, localDir, "nonexistent-branch")
		if err == nil {
			t.Fatal("FetchBranch expected non-nil error for nonexistent remote branch, got nil")
		}
	})
}
