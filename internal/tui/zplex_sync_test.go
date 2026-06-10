package tui

// zplex_sync_test.go — Handler-level zplex sync tests (AC-15).
//
// Covers:
//   - AC-8: waiting→active PATCH sequence for a permission episode
//     (via checkPermissionSignals + checkZplexActiveResync)
//   - AC-9: "done" PATCH when coder slot transitions to SlotLaunchingReviewer,
//     and when reviewer slot transitions on "ai-review" label
//   - AC-10: auto-close kill behavior for true/false and needs-changes cases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

// --- helpers ---

// fakePatcher records PatchAgentState calls for test assertions.
type fakePatcher struct {
	mu    sync.Mutex
	calls [][2]string // each entry is [sessionID, state]
}

func (f *fakePatcher) PatchAgentState(id, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, [2]string{id, state})
	return nil
}

func (f *fakePatcher) recordedCalls() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([][2]string, len(f.calls))
	copy(cp, f.calls)
	return cp
}

// runCmd executes a tea.Cmd tree synchronously so the PATCH/kill closures fire.
// tea.BatchMsg is []tea.Cmd.
func runCmd(c tea.Cmd) {
	if c == nil {
		return
	}
	msg := c()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range b {
			runCmd(sub)
		}
	}
}

// makeZplexTestModel builds a Model suitable for zplex sync tests.
// AutoMerge=true causes the ai-review branch to use loopAutoMergeCmd
// (which returns nil without a tracker client) instead of loopSchedulePRPoll
// (which produces a live timer tick).
func makeZplexTestModel(t *testing.T, autoClose bool) Model {
	t.Helper()
	cfg := &config.Config{
		Terminal:           config.TerminalConfig{ZplexPort: 17732},
		AutoCloseAfterDone: autoClose,
		Worktree: config.WorktreeConfig{
			PollSeconds:     1,
			PRPollSeconds:   1,
			MaxPerProject:   3,
			MaxReviewRounds: 3,
		},
		Projects: []config.ProjectConfig{
			{ID: "p1", Name: "Project 1", Tracker: "t1", AutoMerge: true},
		},
	}
	state := NewAppState(cfg, nil, nil, nil, nil, nil, nil, nil, worktree.HookScripts{}, nil)
	return NewModel(state)
}

// overrideZplexClient replaces newZplexClient for the duration of the test
// and restores it on cleanup.
func overrideZplexClient(t *testing.T, fake *fakePatcher) {
	t.Helper()
	orig := newZplexClient
	newZplexClient = func(int) zplexPatcher { return fake }
	t.Cleanup(func() { newZplexClient = orig })
}

// seedZplexLoop installs a LoopState with one slot into m.state.loops.
func seedZplexLoop(m Model, slot *loop.Slot) {
	m.state.Lock()
	defer m.state.Unlock()
	ls := &loop.LoopState{
		Active: true,
		Slots:  make(map[string]*loop.Slot),
	}
	if slot != nil {
		ls.Slots[loop.SlotKey(slot.ProjectID, slot.IssueID)] = slot
	}
	m.state.loops[slot.ProjectID] = ls
}

// --- AC-8: waiting→active PATCH sequence for a permission episode ---

func TestZplexSync_AC8_WaitingToActive(t *testing.T) {
	// Redirect home so signalDir() returns a tmp directory we control.
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("HOME", tmp)

	// Create the signals directory and write a permission signal file.
	sigDir := filepath.Join(tmp, ".zpit", "signals")
	if err := os.MkdirAll(sigDir, 0o755); err != nil {
		t.Fatalf("mkdir signals: %v", err)
	}
	sig := permissionSignal{SessionID: "SID", Message: "needs perm"}
	data, _ := json.Marshal(sig)
	if err := os.WriteFile(filepath.Join(sigDir, "permission-SID.json"), data, 0o644); err != nil {
		t.Fatalf("write signal: %v", err)
	}

	m := makeZplexTestModel(t, true)
	fake := &fakePatcher{}
	overrideZplexClient(t, fake)

	// Insert an ActiveTerminal with a known ZplexSessionID.
	m.state.Lock()
	m.state.activeTerminals["k"] = &ActiveTerminal{
		SessionID:      "SID",
		ZplexSessionID: "z1",
		State:          watcher.StateWorking,
	}
	// Reset interval gate so checkPermissionSignals does not skip.
	m.state.lastPermissionCheck = time.Time{}
	m.state.Unlock()

	// Step 1: detect permission signal → should emit "waiting" PATCH.
	cmds := m.checkPermissionSignals()
	for _, cmd := range cmds {
		runCmd(cmd)
	}

	calls := fake.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 PATCH call after checkPermissionSignals, got %d: %v", len(calls), calls)
	}
	if calls[0] != [2]string{"z1", "waiting"} {
		t.Errorf("expected PATCH {z1, waiting}, got %v", calls[0])
	}

	// Verify zplexWaiting flag is set.
	m.state.RLock()
	at := m.state.activeTerminals["k"]
	if !at.zplexWaiting {
		t.Error("expected zplexWaiting=true after waiting PATCH")
	}
	m.state.RUnlock()

	// Step 2: simulate permission cleared — set State back to StateWorking so
	// checkZplexActiveResync finds zplexWaiting=true and State!=StatePermission.
	m.state.Lock()
	m.state.activeTerminals["k"].State = watcher.StateWorking
	m.state.Unlock()

	cmds2 := m.checkZplexActiveResync()
	for _, cmd := range cmds2 {
		runCmd(cmd)
	}

	calls2 := fake.recordedCalls()
	if len(calls2) != 2 {
		t.Fatalf("expected 2 total PATCH calls after checkZplexActiveResync, got %d: %v", len(calls2), calls2)
	}
	if calls2[1] != [2]string{"z1", "active"} {
		t.Errorf("expected PATCH {z1, active}, got %v", calls2[1])
	}

	// Verify zplexWaiting flag is cleared.
	m.state.RLock()
	if m.state.activeTerminals["k"].zplexWaiting {
		t.Error("expected zplexWaiting=false after active PATCH")
	}
	m.state.RUnlock()
}

// --- AC-9: coder done on SlotCoding → SlotLaunchingReviewer ---

func TestZplexSync_AC9_CoderDone_OnReviewLabel(t *testing.T) {
	m := makeZplexTestModel(t, true)
	fake := &fakePatcher{}
	overrideZplexClient(t, fake)

	slot := &loop.Slot{
		ProjectID:    "p1",
		IssueID:      "42",
		State:        loop.SlotCoding,
		WorktreePath: "D:/wt",
		Sessions: []loop.SessionRef{
			{Role: "coder", ZplexSessionID: "c1", PID: 111},
		},
	}
	seedZplexLoop(m, slot)

	_, cmd := m.handleLoopLabelPoll(LoopLabelPollMsg{
		ProjectID: "p1",
		IssueID:   "42",
		Labels:    []string{"review"},
	})

	// Verify state transition.
	m.state.RLock()
	ls := m.state.loops["p1"]
	s := ls.Slots[loop.SlotKey("p1", "42")]
	gotState := s.State
	m.state.RUnlock()

	if gotState != loop.SlotLaunchingReviewer {
		t.Errorf("expected SlotLaunchingReviewer, got %v", gotState)
	}

	// Run the returned cmd tree to fire PATCH closures.
	runCmd(cmd)

	calls := fake.recordedCalls()
	found := false
	for _, c := range calls {
		if c == [2]string{"c1", "done"} {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected PATCH {c1, done} in calls, got %v", calls)
	}
}

// --- AC-9 + AC-10: reviewer done + PASS kill (AutoCloseAfterDone=true) ---

func TestZplexSync_AC9AC10_ReviewerDone_AutoClose_True(t *testing.T) {
	m := makeZplexTestModel(t, true)
	fake := &fakePatcher{}
	overrideZplexClient(t, fake)

	// Track killed PIDs.
	var killedMu sync.Mutex
	var killedPIDs []int
	origKill := killSessionFn
	killSessionFn = func(pid int) {
		killedMu.Lock()
		killedPIDs = append(killedPIDs, pid)
		killedMu.Unlock()
	}
	t.Cleanup(func() { killSessionFn = origKill })

	slot := &loop.Slot{
		ProjectID:    "p1",
		IssueID:      "42",
		State:        loop.SlotReviewing,
		WorktreePath: "D:/wt",
		Sessions: []loop.SessionRef{
			{Role: "coder", ZplexSessionID: "c1", PID: 111},
			{Role: "reviewer", ZplexSessionID: "r1", PID: 222},
		},
	}
	seedZplexLoop(m, slot)

	_, cmd := m.handleLoopLabelPoll(LoopLabelPollMsg{
		ProjectID: "p1",
		IssueID:   "42",
		Labels:    []string{"ai-review"},
	})

	// Verify state transition to SlotAutoMerging (AutoMerge=true).
	m.state.RLock()
	ls := m.state.loops["p1"]
	s := ls.Slots[loop.SlotKey("p1", "42")]
	gotState := s.State
	m.state.RUnlock()

	if gotState != loop.SlotAutoMerging {
		t.Errorf("expected SlotAutoMerging, got %v", gotState)
	}

	// Run cmd tree.
	runCmd(cmd)

	// Assert reviewer done PATCH.
	calls := fake.recordedCalls()
	foundReviewerDone := false
	for _, c := range calls {
		if c == [2]string{"r1", "done"} {
			foundReviewerDone = true
			break
		}
	}
	if !foundReviewerDone {
		t.Errorf("expected PATCH {r1, done} in calls, got %v", calls)
	}

	// Assert both PIDs were killed.
	killedMu.Lock()
	killed := make([]int, len(killedPIDs))
	copy(killed, killedPIDs)
	killedMu.Unlock()

	if len(killed) != 2 {
		t.Fatalf("expected 2 killed PIDs, got %d: %v", len(killed), killed)
	}
	has111, has222 := false, false
	for _, pid := range killed {
		if pid == 111 {
			has111 = true
		}
		if pid == 222 {
			has222 = true
		}
	}
	if !has111 || !has222 {
		t.Errorf("expected PIDs 111 and 222 killed, got %v", killed)
	}
}

// --- AC-10: PASS no-kill when AutoCloseAfterDone=false ---

func TestZplexSync_AC10_ReviewerDone_AutoClose_False(t *testing.T) {
	m := makeZplexTestModel(t, false) // AutoCloseAfterDone = false
	fake := &fakePatcher{}
	overrideZplexClient(t, fake)

	var killedMu sync.Mutex
	var killedPIDs []int
	origKill := killSessionFn
	killSessionFn = func(pid int) {
		killedMu.Lock()
		killedPIDs = append(killedPIDs, pid)
		killedMu.Unlock()
	}
	t.Cleanup(func() { killSessionFn = origKill })

	slot := &loop.Slot{
		ProjectID:    "p1",
		IssueID:      "42",
		State:        loop.SlotReviewing,
		WorktreePath: "D:/wt",
		Sessions: []loop.SessionRef{
			{Role: "coder", ZplexSessionID: "c1", PID: 111},
			{Role: "reviewer", ZplexSessionID: "r1", PID: 222},
		},
	}
	seedZplexLoop(m, slot)

	_, cmd := m.handleLoopLabelPoll(LoopLabelPollMsg{
		ProjectID: "p1",
		IssueID:   "42",
		Labels:    []string{"ai-review"},
	})

	runCmd(cmd)

	// Reviewer done PATCH must still fire.
	calls := fake.recordedCalls()
	foundReviewerDone := false
	for _, c := range calls {
		if c == [2]string{"r1", "done"} {
			foundReviewerDone = true
			break
		}
	}
	if !foundReviewerDone {
		t.Errorf("expected PATCH {r1, done} in calls, got %v", calls)
	}

	// No PIDs should have been killed.
	killedMu.Lock()
	nKilled := len(killedPIDs)
	killedMu.Unlock()

	if nKilled != 0 {
		t.Errorf("expected 0 killed PIDs when AutoCloseAfterDone=false, got %d", nKilled)
	}
}

// --- AC-10: needs-changes never kills ---

func TestZplexSync_AC10_NeedsChanges_NeverKills(t *testing.T) {
	m := makeZplexTestModel(t, true) // AutoCloseAfterDone=true — still no kill on needs-changes
	fake := &fakePatcher{}
	overrideZplexClient(t, fake)

	var killedMu sync.Mutex
	var killedPIDs []int
	origKill := killSessionFn
	killSessionFn = func(pid int) {
		killedMu.Lock()
		killedPIDs = append(killedPIDs, pid)
		killedMu.Unlock()
	}
	t.Cleanup(func() { killSessionFn = origKill })

	slot := &loop.Slot{
		ProjectID:   "p1",
		IssueID:     "42",
		State:       loop.SlotReviewing,
		ReviewRound: 0,
		Sessions: []loop.SessionRef{
			{Role: "reviewer", ZplexSessionID: "r1", PID: 222},
		},
	}
	seedZplexLoop(m, slot)

	_, cmd := m.handleLoopLabelPoll(LoopLabelPollMsg{
		ProjectID: "p1",
		IssueID:   "42",
		Labels:    []string{"needs-changes"},
	})

	// State should be SlotWritingAgent (revision path).
	m.state.RLock()
	ls := m.state.loops["p1"]
	s := ls.Slots[loop.SlotKey("p1", "42")]
	gotState := s.State
	m.state.RUnlock()

	if gotState != loop.SlotWritingAgent {
		t.Errorf("expected SlotWritingAgent on needs-changes, got %v", gotState)
	}

	runCmd(cmd)

	// Reviewer done PATCH must fire.
	calls := fake.recordedCalls()
	foundReviewerDone := false
	for _, c := range calls {
		if c == [2]string{"r1", "done"} {
			foundReviewerDone = true
			break
		}
	}
	if !foundReviewerDone {
		t.Errorf("expected PATCH {r1, done} on needs-changes, got %v", calls)
	}

	// No PIDs should be killed on needs-changes.
	killedMu.Lock()
	nKilled := len(killedPIDs)
	killedMu.Unlock()

	if nKilled != 0 {
		t.Errorf("expected 0 killed PIDs on needs-changes (AC-10), got %d", nKilled)
	}
}

// --- Production wiring: ZplexSessionID must reach ActiveTerminal without manual staging ---

// Manual launch path: handleLaunchResult must copy Result.ZplexSessionID into
// the ActiveTerminal it creates. The returned cmd tree is deliberately NOT run
// (it contains startWatcherDirCmdWithExcludes, which scans real processes).
func TestZplexWiring_HandleLaunchResult_CopiesSessionID(t *testing.T) {
	m := makeZplexTestModel(t, true)

	model, _ := m.handleLaunchResult(LaunchResultMsg{
		ProjectID: "p1",
		WorkDir:   "D:/proj",
		Result:    &terminal.LaunchResult{ZplexSessionID: "z1", SwitchHint: "hint"},
	})
	m = model.(Model)

	m.state.RLock()
	defer m.state.RUnlock()
	var found *ActiveTerminal
	for _, at := range m.state.activeTerminals {
		if at.WorkDir == "D:/proj" {
			found = at
			break
		}
	}
	if found == nil {
		t.Fatal("expected an ActiveTerminal for WorkDir D:/proj")
	}
	if found.ZplexSessionID != "z1" {
		t.Errorf("expected ZplexSessionID %q, got %q", "z1", found.ZplexSessionID)
	}
}

// Loop launch path: the session scan creates the ActiveTerminal, so
// handleExistingSessions must bind the slot's pending SessionRef —
// zplex id flows ref→AT (AC-8), PID flows entry→ref (auto-close).
func TestZplexWiring_ExistingSessions_LoopFillIn(t *testing.T) {
	m := makeZplexTestModel(t, true)

	slot := &loop.Slot{
		ProjectID:    "p1",
		IssueID:      "42",
		State:        loop.SlotCoding,
		WorktreePath: "D:/wt",
		Sessions: []loop.SessionRef{
			{Role: "coder", ZplexSessionID: "c1", PID: 0},
		},
	}
	seedZplexLoop(m, slot)

	// Cmds (waitForLogCmd) are deliberately NOT run.
	m.handleExistingSessions(existingSessionsMsg{
		Source: "periodic",
		Entries: []existingSessionEntry{
			{ProjectID: "p1", PID: 111, SessionID: "S1", WorkDir: "D:/wt", LogPath: "x.jsonl"},
		},
	})

	m.state.RLock()
	defer m.state.RUnlock()
	var found *ActiveTerminal
	for _, at := range m.state.activeTerminals {
		if at.SessionPID == 111 {
			found = at
			break
		}
	}
	if found == nil {
		t.Fatal("expected an ActiveTerminal for PID 111")
	}
	if found.ZplexSessionID != "c1" {
		t.Errorf("expected ZplexSessionID %q backfilled from slot ref, got %q", "c1", found.ZplexSessionID)
	}
	ref := m.state.loops["p1"].Slots[loop.SlotKey("p1", "42")].Sessions[0]
	if ref.PID != 111 {
		t.Errorf("expected SessionRef.PID backfilled to 111, got %d", ref.PID)
	}
}

// Auto-close PID collection must dedup: multiple refs resolving through the
// WorkDir fallback to the same ActiveTerminal must yield one kill, not N.
func TestZplexWiring_AutoClose_DedupsPIDs(t *testing.T) {
	m := makeZplexTestModel(t, true)
	fake := &fakePatcher{}
	overrideZplexClient(t, fake)

	var killedMu sync.Mutex
	var killedPIDs []int
	origKill := killSessionFn
	killSessionFn = func(pid int) {
		killedMu.Lock()
		killedPIDs = append(killedPIDs, pid)
		killedMu.Unlock()
	}
	t.Cleanup(func() { killSessionFn = origKill })

	// Two coder rounds with unresolved PIDs (wt/tmux fallback: no zplex id)
	// plus a reviewer with a known PID. Both coder refs fall back to the same
	// WorkDir-matched ActiveTerminal.
	slot := &loop.Slot{
		ProjectID:    "p1",
		IssueID:      "42",
		State:        loop.SlotReviewing,
		WorktreePath: "D:/wt",
		Sessions: []loop.SessionRef{
			{Role: "coder", PID: 0},
			{Role: "coder", PID: 0},
			{Role: "reviewer", ZplexSessionID: "r1", PID: 222},
		},
	}
	seedZplexLoop(m, slot)

	m.state.Lock()
	m.state.activeTerminals["k"] = &ActiveTerminal{
		WorkDir:    "D:/wt",
		SessionPID: 111,
		State:      watcher.StateWorking,
	}
	m.state.Unlock()

	_, cmd := m.handleLoopLabelPoll(LoopLabelPollMsg{
		ProjectID: "p1",
		IssueID:   "42",
		Labels:    []string{"ai-review"},
	})
	runCmd(cmd)

	killedMu.Lock()
	killed := make([]int, len(killedPIDs))
	copy(killed, killedPIDs)
	killedMu.Unlock()

	if len(killed) != 2 {
		t.Fatalf("expected 2 unique killed PIDs, got %d: %v", len(killed), killed)
	}
	has111, has222 := false, false
	for _, pid := range killed {
		if pid == 111 {
			has111 = true
		}
		if pid == 222 {
			has222 = true
		}
	}
	if !has111 || !has222 {
		t.Errorf("expected PIDs 111 and 222 killed exactly once each, got %v", killed)
	}
}
