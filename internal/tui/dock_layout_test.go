package tui

import (
	"testing"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

// makeDockTestModel produces a Model with a minimal AppState for dock-sizing tests.
func makeDockTestModel(t *testing.T, withTerms, withLoops bool) Model {
	t.Helper()
	cfg := &config.Config{
		Projects: []config.ProjectConfig{
			{ID: "p1", Name: "Project 1"},
		},
	}
	state := NewAppState(cfg, nil, nil, nil, nil, nil, nil, worktree.HookScripts{}, nil)
	if withTerms {
		state.activeTerminals["p1"] = &ActiveTerminal{State: watcher.StateWorking}
	}
	if withLoops {
		state.loops["p1"] = &loop.LoopState{
			Active: true,
			Slots: map[string]*loop.Slot{
				"p1#1": {IssueID: "1", State: loop.SlotCoding},
			},
		}
	}
	return NewModel(state)
}

func TestComputePanelRects(t *testing.T) {
	tests := []struct {
		name            string
		width           int
		contentH        int
		withTerms       bool
		withLoops       bool
		wantNoTerms     bool // terminals rect should be zero-height
		wantNoLoop      bool // loop rect should be zero-height
		wantSumHeights  bool // projects + terminals + loop == contentH
		wantMinProjectH bool // projects h >= dockMinPanelHeight
	}{
		{
			name: "wide + empty — projects fills left column",
			width: 160, contentH: 40,
			wantNoTerms: true, wantNoLoop: true,
			wantSumHeights: true, wantMinProjectH: true,
		},
		{
			name: "wide + all panels — weighted split",
			width: 160, contentH: 40, withTerms: true, withLoops: true,
			wantSumHeights: true, wantMinProjectH: true,
		},
		{
			name: "wide + terms only — no loop panel",
			width: 160, contentH: 40, withTerms: true,
			wantNoLoop: true, wantSumHeights: true, wantMinProjectH: true,
		},
		{
			name: "wide + loops only — no terminals panel",
			width: 160, contentH: 40, withLoops: true,
			wantNoTerms: true, wantSumHeights: true, wantMinProjectH: true,
		},
		{
			name: "narrow terminal still keeps hotkeys docked right",
			width: 80, contentH: 40,
			wantNoTerms: true, wantNoLoop: true, wantSumHeights: true, wantMinProjectH: true,
		},
		{
			name: "very narrow + all panels",
			width: 60, contentH: 40, withTerms: true, withLoops: true,
			wantSumHeights: true, wantMinProjectH: true,
		},
		{
			name: "tiny content height still respects min panel height",
			width: 160, contentH: 6, withTerms: true, withLoops: true,
			wantMinProjectH: true,
		},
		{
			name: "hotkeys column clamped to min width",
			width: 100, contentH: 40,
			wantNoTerms: true, wantNoLoop: true, wantSumHeights: true, wantMinProjectH: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := makeDockTestModel(t, tt.withTerms, tt.withLoops)
			r := m.computePanelRects(tt.width, tt.contentH)

			leftW := r.projects.w
			if tt.withTerms && leftW != r.terminals.w {
				t.Errorf("terminals.w=%d not aligned with projects.w=%d", r.terminals.w, leftW)
			}
			if tt.withLoops && leftW != r.loop.w {
				t.Errorf("loop.w=%d not aligned with projects.w=%d", r.loop.w, leftW)
			}
			if leftW+r.hotkeys.w != tt.width {
				t.Errorf("leftW(%d) + rightW(%d) = %d, want %d", leftW, r.hotkeys.w, leftW+r.hotkeys.w, tt.width)
			}
			if r.hotkeys.w < dockMinRightWidth {
				t.Errorf("hotkeys.w=%d < dockMinRightWidth=%d", r.hotkeys.w, dockMinRightWidth)
			}
			if tt.wantSumHeights {
				sum := r.projects.h + r.terminals.h + r.loop.h
				if sum != tt.contentH {
					t.Errorf("projects.h+terminals.h+loop.h = %d, want %d", sum, tt.contentH)
				}
			}

			if tt.wantNoTerms && r.terminals.h != 0 {
				t.Errorf("terminals.h = %d, want 0 (no terminals)", r.terminals.h)
			}
			if tt.wantNoLoop && r.loop.h != 0 {
				t.Errorf("loop.h = %d, want 0 (no loops)", r.loop.h)
			}
			if tt.wantMinProjectH && r.projects.h < dockMinPanelHeight {
				t.Errorf("projects.h = %d, want >= %d", r.projects.h, dockMinPanelHeight)
			}
			if tt.withTerms && !tt.wantNoTerms && r.terminals.h < dockMinPanelHeight {
				t.Errorf("terminals.h = %d, want >= %d", r.terminals.h, dockMinPanelHeight)
			}
			if tt.withLoops && !tt.wantNoLoop && r.loop.h < dockMinPanelHeight {
				t.Errorf("loop.h = %d, want >= %d", r.loop.h, dockMinPanelHeight)
			}
		})
	}
}
