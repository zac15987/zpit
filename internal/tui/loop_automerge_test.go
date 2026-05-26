package tui

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/worktree"
)

// Regression tests for loopAutoMergeCmd. The 2026-05-26 incident traced to:
//   1. Slow tracker (Synology NAS) made the merge response arrive after the
//      30s client timeout — attempt 1 timed out but the merge actually
//      completed server-side.
//   2. Attempt 2 hit the same endpoint, Forgejo returned 405 "already
//      merged" — classified as permanent by classifyMergeErr.
//   3. Slot escalated to NeedsHuman despite the PR being merged.
//
// Fix: before returning permanent / transient_exhausted, GetPRStatus is
// consulted; if the PR is in state "merged", report success. These tests
// pin that recovery and guard against (a) recovery being lost, and (b)
// recovery papering over a genuine failure (PR still open).

const (
	amProjectID = "proj-am"
	amIssueID   = "41"
	amTracker   = "t1"
	amRepo      = "owner/repo"
	amBranch    = "feat/41-test"
)

// mockMergeClient implements tracker.TrackerClient with hooks for the three
// methods loopAutoMergeCmd uses. The other interface methods t.Fatalf — if
// loopAutoMergeCmd ever starts calling them, the test author must decide
// whether to extend the mock or narrow the scope, never silently no-op.
type mockMergeClient struct {
	t *testing.T

	findPRFn  func(ctx context.Context, repo, branch string) (*tracker.PRStatus, error)
	mergePRFn func(ctx context.Context, repo, prID, method, commitTitle string) (*tracker.PRStatus, error)
	getPRFn   func(ctx context.Context, repo, prID string) (*tracker.PRStatus, error)

	lastMergeCommitTitle string
	lastMergeMethod      string
	mergeCallCount       int
	getPRCallCount       int
}

func (c *mockMergeClient) FindPRByBranch(ctx context.Context, repo, branch string) (*tracker.PRStatus, error) {
	return c.findPRFn(ctx, repo, branch)
}

func (c *mockMergeClient) MergePR(ctx context.Context, repo, prID, method, commitTitle string) (*tracker.PRStatus, error) {
	c.mergeCallCount++
	c.lastMergeCommitTitle = commitTitle
	c.lastMergeMethod = method
	return c.mergePRFn(ctx, repo, prID, method, commitTitle)
}

func (c *mockMergeClient) GetPRStatus(ctx context.Context, repo, prID string) (*tracker.PRStatus, error) {
	c.getPRCallCount++
	return c.getPRFn(ctx, repo, prID)
}

func (c *mockMergeClient) ListIssues(ctx context.Context, repo string) ([]tracker.Issue, error) {
	c.t.Fatalf("ListIssues should not be called from loopAutoMergeCmd")
	return nil, nil
}
func (c *mockMergeClient) GetIssue(ctx context.Context, repo, id string) (*tracker.Issue, error) {
	c.t.Fatalf("GetIssue should not be called from loopAutoMergeCmd")
	return nil, nil
}
func (c *mockMergeClient) UpdateLabels(ctx context.Context, repo, id string, add, remove []string) error {
	c.t.Fatalf("UpdateLabels should not be called from loopAutoMergeCmd")
	return nil
}
func (c *mockMergeClient) CloseIssue(ctx context.Context, repo, id string) error {
	c.t.Fatalf("CloseIssue should not be called from loopAutoMergeCmd")
	return nil
}
func (c *mockMergeClient) ListOpenPRs(ctx context.Context, repo string) ([]tracker.PRInfo, error) {
	c.t.Fatalf("ListOpenPRs should not be called from loopAutoMergeCmd")
	return nil, nil
}

// makeAutoMergeModel builds a Model with one project (auto_merge=true) and
// one active loop slot in SlotAutoMerging, ready for loopAutoMergeCmd to run.
func makeAutoMergeModel(t *testing.T, client tracker.TrackerClient) Model {
	t.Helper()
	cfg := &config.Config{
		Worktree: config.WorktreeConfig{
			MergeTimeoutSeconds: 30, // mock returns immediately, so any positive value works
		},
		Projects: []config.ProjectConfig{
			{
				ID:          amProjectID,
				Tracker:     amTracker,
				Repo:        amRepo,
				AutoMerge:   true,
				MergeMethod: "squash",
			},
		},
	}
	state := NewAppState(cfg, nil, nil, nil, nil, nil, nil, nil, worktree.HookScripts{}, io.Discard)
	state.clients[amTracker] = client

	state.Lock()
	ls := &loop.LoopState{Active: true, Slots: make(map[string]*loop.Slot)}
	ls.Slots[loop.SlotKey(amProjectID, amIssueID)] = &loop.Slot{
		ProjectID:  amProjectID,
		IssueID:    amIssueID,
		IssueTitle: "Test feature",
		BranchName: amBranch,
		State:      loop.SlotAutoMerging,
	}
	state.loops[amProjectID] = ls
	state.Unlock()

	return NewModel(state)
}

// TestLoopAutoMerge_RaceRecovery_Permanent reproduces the 2026-05-26 failure:
// MergePR returns status 405 (classified permanent — "already merged" because
// a prior attempt completed server-side after client-timeout), but GetPRStatus
// confirms the PR is merged. checkAlreadyMerged should turn this into success.
func TestLoopAutoMerge_RaceRecovery_Permanent(t *testing.T) {
	client := &mockMergeClient{
		t: t,
		findPRFn: func(ctx context.Context, repo, branch string) (*tracker.PRStatus, error) {
			return &tracker.PRStatus{ID: "42", State: "open", Title: "Test feature"}, nil
		},
		mergePRFn: func(ctx context.Context, repo, prID, method, commitTitle string) (*tracker.PRStatus, error) {
			return nil, errors.New("merge PR: http POST /api/v1/repos/owner/repo/pulls/42/merge: status 405: {}")
		},
		getPRFn: func(ctx context.Context, repo, prID string) (*tracker.PRStatus, error) {
			return &tracker.PRStatus{ID: "42", State: "merged", URL: "http://example.com/pulls/42"}, nil
		},
	}
	m := makeAutoMergeModel(t, client)

	cmd := m.loopAutoMergeCmd(amProjectID, amIssueID)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg, ok := cmd().(LoopAutoMergeMsg)
	if !ok {
		t.Fatalf("expected LoopAutoMergeMsg, got %T", cmd())
	}

	if msg.FailureKind != "" {
		t.Errorf("race recovery should report success (empty FailureKind), got %q err=%v", msg.FailureKind, msg.Err)
	}
	if msg.PR == nil {
		t.Fatal("expected PR set on recovered success, got nil")
	}
	if msg.PR.State != "merged" {
		t.Errorf("expected PR state merged, got %q", msg.PR.State)
	}
	if client.getPRCallCount == 0 {
		t.Error("expected GetPRStatus to be called for race recovery; got 0 calls")
	}
}

// TestLoopAutoMerge_PermanentFail_NoFalsePositive ensures a genuine permanent
// failure (PR still open after merge call rejected) still escalates to
// NeedsHuman. checkAlreadyMerged must not paper over real failures.
func TestLoopAutoMerge_PermanentFail_NoFalsePositive(t *testing.T) {
	client := &mockMergeClient{
		t: t,
		findPRFn: func(ctx context.Context, repo, branch string) (*tracker.PRStatus, error) {
			return &tracker.PRStatus{ID: "42", State: "open", Title: "Test feature"}, nil
		},
		mergePRFn: func(ctx context.Context, repo, prID, method, commitTitle string) (*tracker.PRStatus, error) {
			return nil, errors.New("merge PR: http POST /api/v1/repos/owner/repo/pulls/42/merge: status 409: conflict")
		},
		getPRFn: func(ctx context.Context, repo, prID string) (*tracker.PRStatus, error) {
			return &tracker.PRStatus{ID: "42", State: "open"}, nil // still open — not merged
		},
	}
	m := makeAutoMergeModel(t, client)

	msg, ok := m.loopAutoMergeCmd(amProjectID, amIssueID)().(LoopAutoMergeMsg)
	if !ok {
		t.Fatal("expected LoopAutoMergeMsg")
	}
	if msg.FailureKind != "permanent" {
		t.Errorf("expected FailureKind=permanent, got %q (PR still open should NOT trigger recovery)", msg.FailureKind)
	}
	if msg.Err == nil {
		t.Error("expected Err set on permanent failure")
	}
	if client.getPRCallCount == 0 {
		t.Error("expected GetPRStatus to be called (recovery attempt), got 0 calls")
	}
}

// TestLoopAutoMerge_HappyPath guards the success path against the
// race-recovery code (GetPRStatus must not be called when MergePR succeeds)
// and verifies the merge commit subject format.
func TestLoopAutoMerge_HappyPath(t *testing.T) {
	client := &mockMergeClient{
		t: t,
		findPRFn: func(ctx context.Context, repo, branch string) (*tracker.PRStatus, error) {
			return &tracker.PRStatus{ID: "42", State: "open", Title: "Add new feature X"}, nil
		},
		mergePRFn: func(ctx context.Context, repo, prID, method, commitTitle string) (*tracker.PRStatus, error) {
			return &tracker.PRStatus{ID: prID, State: "merged", URL: "http://example.com/pulls/42"}, nil
		},
		getPRFn: func(ctx context.Context, repo, prID string) (*tracker.PRStatus, error) {
			t.Fatalf("GetPRStatus should not be called on the happy path (no recovery needed)")
			return nil, nil
		},
	}
	m := makeAutoMergeModel(t, client)

	msg, ok := m.loopAutoMergeCmd(amProjectID, amIssueID)().(LoopAutoMergeMsg)
	if !ok {
		t.Fatal("expected LoopAutoMergeMsg")
	}
	if msg.FailureKind != "" {
		t.Errorf("expected success, got FailureKind=%q err=%v", msg.FailureKind, msg.Err)
	}
	if msg.PR == nil || msg.PR.State != "merged" {
		t.Errorf("expected merged PR, got %+v", msg.PR)
	}

	wantTitle := "merge: Add new feature X"
	if client.lastMergeCommitTitle != wantTitle {
		t.Errorf("commit title mismatch:\n  want %q\n  got  %q", wantTitle, client.lastMergeCommitTitle)
	}
	if client.lastMergeMethod != "squash" {
		t.Errorf("expected merge method squash, got %q", client.lastMergeMethod)
	}
	if client.mergeCallCount != 1 {
		t.Errorf("expected exactly 1 merge call on happy path, got %d", client.mergeCallCount)
	}
}
