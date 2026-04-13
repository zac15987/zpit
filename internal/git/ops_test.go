package git

import (
	"reflect"
	"testing"
)

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
			branchesOut: "main\x00origin/main\n",
			currentName: "main",
			want: []LocalBranch{
				{Name: "main", IsCurrent: true, Upstream: "origin/main"},
			},
		},
		{
			name:        "single branch not current",
			branchesOut: "main\x00origin/main\n",
			currentName: "dev",
			want: []LocalBranch{
				{Name: "main", IsCurrent: false, Upstream: "origin/main"},
			},
		},
		{
			name:        "branch with no upstream",
			branchesOut: "feature-x\x00\n",
			currentName: "feature-x",
			want: []LocalBranch{
				{Name: "feature-x", IsCurrent: true, Upstream: ""},
			},
		},
		{
			name: "multiple branches with mixed upstream",
			branchesOut: "main\x00origin/main\n" +
				"dev\x00origin/dev\n" +
				"feat/89-foo\x00\n",
			currentName: "dev",
			want: []LocalBranch{
				{Name: "main", IsCurrent: false, Upstream: "origin/main"},
				{Name: "dev", IsCurrent: true, Upstream: "origin/dev"},
				{Name: "feat/89-foo", IsCurrent: false, Upstream: ""},
			},
		},
		{
			name:        "detached HEAD (empty currentName)",
			branchesOut: "main\x00origin/main\ndev\x00origin/dev\n",
			currentName: "",
			want: []LocalBranch{
				{Name: "main", IsCurrent: false, Upstream: "origin/main"},
				{Name: "dev", IsCurrent: false, Upstream: "origin/dev"},
			},
		},
		{
			name:        "whitespace-only lines are skipped",
			branchesOut: "  \n\nmain\x00origin/main\n  \n",
			currentName: "main",
			want: []LocalBranch{
				{Name: "main", IsCurrent: true, Upstream: "origin/main"},
			},
		},
		{
			name:        "no trailing newline",
			branchesOut: "main\x00origin/main",
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
