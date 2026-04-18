package tui

import (
	"testing"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/worktree"
)

// Regression tests for the tick-driven heartbeat fix (loop engine hang).
//
// Invariant under test: handleLoop{Poll,PRPoll,LabelPoll}Tick must return a
// non-nil Cmd (which schedules the next tick) whenever its gate conditions are
// met, and must return nil only when the chain should terminate. If any future
// change lets the tick handler silently return nil while the gate should be
// open, the loop will hang until the user manually restarts it (the original
// bug observed in 2026-04-18 logs).

const (
	tickTestProjectID = "p1"
	tickTestIssueID   = "42"
)

func makeTickTestModel(t *testing.T) Model {
	t.Helper()
	cfg := &config.Config{
		Worktree: config.WorktreeConfig{
			PollSeconds:   1,
			PRPollSeconds: 1,
			MaxPerProject: 3,
		},
		Projects: []config.ProjectConfig{
			{ID: tickTestProjectID, Name: "Project 1", Tracker: "t1"},
		},
	}
	state := NewAppState(cfg, nil, nil, nil, nil, nil, nil, worktree.HookScripts{}, nil)
	return NewModel(state)
}

// seedLoop installs a LoopState (with optional slot) into m.state.loops.
func seedLoop(m Model, active bool, slot *loop.Slot) {
	m.state.Lock()
	defer m.state.Unlock()
	ls := &loop.LoopState{
		Active: active,
		Slots:  make(map[string]*loop.Slot),
	}
	if slot != nil {
		ls.Slots[loop.SlotKey(tickTestProjectID, slot.IssueID)] = slot
	}
	m.state.loops[tickTestProjectID] = ls
}

// --- handleLoopPollTick ---

func TestHandleLoopPollTick_NoLoop_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	_, cmd := m.handleLoopPollTick(loopPollTickMsg{ProjectID: tickTestProjectID})
	if cmd != nil {
		t.Error("expected nil cmd when loop is not registered")
	}
}

func TestHandleLoopPollTick_Inactive_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, false, nil)

	_, cmd := m.handleLoopPollTick(loopPollTickMsg{ProjectID: tickTestProjectID})
	if cmd != nil {
		t.Error("expected nil cmd when loop is inactive")
	}
}

func TestHandleLoopPollTick_Active_ReturnsCmd(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, true, nil)

	_, cmd := m.handleLoopPollTick(loopPollTickMsg{ProjectID: tickTestProjectID})
	if cmd == nil {
		t.Fatal("expected non-nil cmd (heartbeat must survive); got nil — chain would die")
	}
}

// --- handleLoopPRPollTick ---

func TestHandleLoopPRPollTick_NoLoop_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	_, cmd := m.handleLoopPRPollTick(loopPRPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd != nil {
		t.Error("expected nil cmd when loop is not registered")
	}
}

func TestHandleLoopPRPollTick_Inactive_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, false, &loop.Slot{IssueID: tickTestIssueID, State: loop.SlotWaitingPRMerge})

	_, cmd := m.handleLoopPRPollTick(loopPRPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd != nil {
		t.Error("expected nil cmd when loop inactive, even if slot state matches")
	}
}

func TestHandleLoopPRPollTick_NoSlot_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, true, nil)

	_, cmd := m.handleLoopPRPollTick(loopPRPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd != nil {
		t.Error("expected nil cmd when slot is missing")
	}
}

func TestHandleLoopPRPollTick_WrongState_ReturnsNil(t *testing.T) {
	// Any state other than WaitingPRMerge should stop the PR poll chain,
	// because it means handleLoopPRStatus already drove the transition.
	otherStates := []loop.SlotState{
		loop.SlotCreatingWorktree,
		loop.SlotCoding,
		loop.SlotReviewing,
		loop.SlotCleaningUp,
		loop.SlotDone,
		loop.SlotError,
		loop.SlotNeedsHuman,
	}
	for _, st := range otherStates {
		m := makeTickTestModel(t)
		seedLoop(m, true, &loop.Slot{IssueID: tickTestIssueID, State: st})
		_, cmd := m.handleLoopPRPollTick(loopPRPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
		if cmd != nil {
			t.Errorf("state=%v: expected nil cmd (stale tick), got non-nil", st)
		}
	}
}

func TestHandleLoopPRPollTick_WaitingPRMerge_ReturnsCmd(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, true, &loop.Slot{IssueID: tickTestIssueID, State: loop.SlotWaitingPRMerge})

	_, cmd := m.handleLoopPRPollTick(loopPRPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for slot in WaitingPRMerge; got nil — PR poll chain would die and merge would go undetected")
	}
}

// --- handleLoopLabelPollTick ---

func TestHandleLoopLabelPollTick_NoLoop_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	_, cmd := m.handleLoopLabelPollTick(loopLabelPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd != nil {
		t.Error("expected nil cmd when loop is not registered")
	}
}

func TestHandleLoopLabelPollTick_Inactive_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, false, &loop.Slot{IssueID: tickTestIssueID, State: loop.SlotCoding})

	_, cmd := m.handleLoopLabelPollTick(loopLabelPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd != nil {
		t.Error("expected nil cmd when loop inactive")
	}
}

func TestHandleLoopLabelPollTick_NoSlot_ReturnsNil(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, true, nil)

	_, cmd := m.handleLoopLabelPollTick(loopLabelPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd != nil {
		t.Error("expected nil cmd when slot is missing")
	}
}

func TestHandleLoopLabelPollTick_Coding_ReturnsCmd(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, true, &loop.Slot{IssueID: tickTestIssueID, State: loop.SlotCoding})

	_, cmd := m.handleLoopLabelPollTick(loopLabelPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for slot in Coding; got nil — label poll chain would die")
	}
}

func TestHandleLoopLabelPollTick_Reviewing_ReturnsCmd(t *testing.T) {
	m := makeTickTestModel(t)
	seedLoop(m, true, &loop.Slot{IssueID: tickTestIssueID, State: loop.SlotReviewing})

	_, cmd := m.handleLoopLabelPollTick(loopLabelPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for slot in Reviewing; got nil — label poll chain would die")
	}
}

func TestHandleLoopLabelPollTick_WrongState_ReturnsNil(t *testing.T) {
	// Any state other than Coding/Reviewing means a transition fired elsewhere;
	// the tick handler must stop rather than keep polling stale state.
	otherStates := []loop.SlotState{
		loop.SlotCreatingWorktree,
		loop.SlotWritingAgent,
		loop.SlotLaunchingCoder,
		loop.SlotLaunchingReviewer,
		loop.SlotWaitingPRMerge,
		loop.SlotCleaningUp,
		loop.SlotDone,
		loop.SlotError,
		loop.SlotNeedsHuman,
	}
	for _, st := range otherStates {
		m := makeTickTestModel(t)
		seedLoop(m, true, &loop.Slot{IssueID: tickTestIssueID, State: st})
		_, cmd := m.handleLoopLabelPollTick(loopLabelPollTickMsg{ProjectID: tickTestProjectID, IssueID: tickTestIssueID})
		if cmd != nil {
			t.Errorf("state=%v: expected nil cmd (stale tick), got non-nil", st)
		}
	}
}
