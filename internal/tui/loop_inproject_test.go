package tui

import (
	"testing"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/worktree"
)

// makeIsolationTestModel builds a Model whose single project uses the given
// isolation mode, with max_per_project=3 so the worktree cap does not interfere.
func makeIsolationTestModel(t *testing.T, isolation string) Model {
	t.Helper()
	cfg := &config.Config{
		Worktree: config.WorktreeConfig{
			PollSeconds:   1,
			PRPollSeconds: 1,
			MaxPerProject: 3,
		},
		Projects: []config.ProjectConfig{
			{ID: tickTestProjectID, Name: "Project 1", Tracker: "t1", Isolation: isolation, BaseBranch: "dev"},
		},
	}
	state := NewAppState(cfg, nil, nil, nil, nil, nil, nil, nil, worktree.HookScripts{}, nil)
	return NewModel(state)
}

// TestHandleLoopPoll_InProjectCapsAtOne asserts that an in_project project never
// dispatches a second concurrent slot, even though max_per_project is 3.
func TestHandleLoopPoll_InProjectCapsAtOne(t *testing.T) {
	m := makeIsolationTestModel(t, "in_project")
	// Seed an active loop already working issue 42.
	seedLoop(m, true, &loop.Slot{ProjectID: tickTestProjectID, IssueID: tickTestIssueID, State: loop.SlotCoding})

	m.handleLoopPoll(LoopPollMsg{
		ProjectID: tickTestProjectID,
		Issues: []tracker.Issue{
			{ID: tickTestIssueID, Title: "existing"},
			{ID: "43", Title: "new one"},
		},
	})

	m.state.RLock()
	defer m.state.RUnlock()
	got := len(m.state.loops[tickTestProjectID].Slots)
	if got != 1 {
		t.Errorf("in_project should cap at 1 slot, got %d", got)
	}
}

// TestHandleLoopPoll_WorktreeAllowsConcurrent is the control: a worktree-mode
// project with the same max_per_project=3 dispatches the second issue.
func TestHandleLoopPoll_WorktreeAllowsConcurrent(t *testing.T) {
	m := makeIsolationTestModel(t, "worktree")
	seedLoop(m, true, &loop.Slot{ProjectID: tickTestProjectID, IssueID: tickTestIssueID, State: loop.SlotCoding})

	m.handleLoopPoll(LoopPollMsg{
		ProjectID: tickTestProjectID,
		Issues: []tracker.Issue{
			{ID: tickTestIssueID, Title: "existing"},
			{ID: "43", Title: "new one"},
		},
	})

	m.state.RLock()
	defer m.state.RUnlock()
	got := len(m.state.loops[tickTestProjectID].Slots)
	if got != 2 {
		t.Errorf("worktree mode (max=3) should dispatch the second issue, got %d slot(s)", got)
	}
}
