package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/worktree"
)

// findLoopSlot looks up the LoopState and Slot for a project+issue.
// Returns nil, nil if not found.
func (m Model) findLoopSlot(projectID, issueID string) (*loop.LoopState, *loop.Slot) {
	ls, ok := m.loops[projectID]
	if !ok {
		return nil, nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	return ls, slot
}

func (m Model) handleLoopPoll(msg LoopPollMsg) (tea.Model, tea.Cmd) {
	ls, ok := m.loops[msg.ProjectID]
	if !ok || !ls.Active {
		return m, nil
	}
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Loop poll error: %s", msg.Err))
		return m, m.loopSchedulePoll(msg.ProjectID)
	}

	// Check existing worktrees to detect resumed issues.
	project := m.findProject(msg.ProjectID)
	var existingWorktrees []worktree.WorktreeInfo
	if project != nil {
		projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
		existingWorktrees, _ = m.wtManager.List(projectPath)
	}

	var cmds []tea.Cmd

	for _, issue := range msg.Issues {
		key := loop.SlotKey(msg.ProjectID, issue.ID)
		if _, exists := ls.Slots[key]; exists {
			continue // already processing
		}
		if len(ls.Slots) >= m.cfg.Worktree.MaxPerProject {
			break // at capacity
		}

		// Resolve effective base branch: Issue Spec > project config.
		effectiveBranch := project.BaseBranch
		if spec, err := tracker.ParseIssueSpec(issue.Body); err == nil && spec.Branch != "" {
			effectiveBranch = spec.Branch
		}

		// Check if a worktree already exists for this issue (resumed from previous session).
		if slot := findLoopSlotFromWorktree(msg.ProjectID, issue, existingWorktrees); slot != nil {
			slot.BaseBranch = effectiveBranch
			ls.Slots[key] = slot
			cmds = append(cmds, m.loopSchedulePRPoll(msg.ProjectID, issue.ID))
			continue
		}

		slot := &loop.Slot{
			ProjectID:  msg.ProjectID,
			IssueID:    issue.ID,
			IssueTitle: issue.Title,
			BaseBranch: effectiveBranch,
			State:      loop.SlotCreatingWorktree,
		}
		ls.Slots[key] = slot
		cmds = append(cmds, m.loopCreateWorktreeCmd(msg.ProjectID, issue.ID, issue.Title))
	}

	// Schedule next poll
	cmds = append(cmds, m.loopSchedulePoll(msg.ProjectID))
	return m, tea.Batch(cmds...)
}

func (m Model) handleLoopWorktreeCreated(msg LoopWorktreeCreatedMsg) (tea.Model, tea.Cmd) {
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		return m, nil
	}

	if msg.Err != nil {
		slot.State = loop.SlotError
		slot.Error = msg.Err
		m.setStatus(fmt.Sprintf("Worktree error #%s: %s", msg.IssueID, msg.Err))
		return m, nil
	}

	slot.WorktreePath = msg.WorktreePath
	slot.BranchName = msg.BranchName
	slot.State = loop.SlotWritingAgent
	return m, m.loopWriteAgentCmd(msg.ProjectID, msg.IssueID)
}

func (m Model) handleLoopAgentWritten(msg LoopAgentWrittenMsg) (tea.Model, tea.Cmd) {
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		return m, nil
	}

	if msg.Err != nil {
		slot.State = loop.SlotError
		slot.Error = msg.Err
		m.setStatus(fmt.Sprintf("Agent write error #%s: %s", msg.IssueID, msg.Err))
		return m, nil
	}

	slot.State = loop.SlotLaunchingCoder
	return m, m.loopLaunchCoderCmd(msg.ProjectID, msg.IssueID)
}

func (m Model) handleLoopAgentLaunched(msg LoopAgentLaunchedMsg) (tea.Model, tea.Cmd) {
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		return m, nil
	}

	if msg.Err != nil {
		slot.State = loop.SlotError
		slot.Error = msg.Err
		m.setStatus(fmt.Sprintf("Launch error #%s (%s): %s", msg.IssueID, msg.Role, msg.Err))
		return m, nil
	}

	if msg.Role == "coder" {
		slot.State = loop.SlotCoding
		// Poll for PR creation as completion signal (terminal stays open)
		return m, m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID)
	}

	slot.State = loop.SlotReviewing
	// Reviewer uses PID monitoring (terminal close = done)
	return m, m.loopStartWatcherCmd(msg.ProjectID, msg.IssueID, msg.Role)
}

func (m Model) handleLoopAgentExited(msg LoopAgentExitedMsg) (tea.Model, tea.Cmd) {
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		return m, nil
	}

	// Reviewer PID died → check labels to determine verdict.
	slot.State = loop.SlotCheckingReview
	return m, m.loopCheckReviewResultCmd(msg.ProjectID, msg.IssueID)
}

func (m Model) handleLoopReviewResult(msg LoopReviewResultMsg) (tea.Model, tea.Cmd) {
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		return m, nil
	}

	if msg.Err != nil {
		slot.State = loop.SlotError
		slot.Error = fmt.Errorf("check review result: %w", msg.Err)
		return m, nil
	}

	switch msg.Verdict {
	case loop.VerdictApproved:
		slot.State = loop.SlotWaitingPRMerge
		return m, m.loopPollPRCmd(msg.ProjectID, msg.IssueID)

	case loop.VerdictNeedsChanges:
		maxRounds := m.cfg.Worktree.MaxReviewRounds
		if slot.ReviewRound >= maxRounds {
			slot.State = loop.SlotNeedsHuman
			m.setStatus(fmt.Sprintf("Issue #%s: %d review rounds exhausted, needs human", msg.IssueID, maxRounds))
			projectName := m.projectName(msg.ProjectID)
			m.notifier.NotifyWaiting(msg.ProjectID, projectName,
				fmt.Sprintf("Issue #%s exceeded %d review rounds", msg.IssueID, maxRounds))
			return m, nil
		}
		slot.ReviewRound++
		slot.State = loop.SlotWritingAgent
		m.setStatus(fmt.Sprintf("Issue #%s needs changes (round %d/%d), re-launching coder",
			msg.IssueID, slot.ReviewRound, maxRounds))
		return m, m.loopWriteRevisionAgentCmd(msg.ProjectID, msg.IssueID)

	default: // VerdictUnknown
		slot.State = loop.SlotError
		slot.Error = fmt.Errorf("reviewer exited without setting verdict label")
		return m, nil
	}
}

func (m Model) handleLoopPRStatus(msg LoopPRStatusMsg) (tea.Model, tea.Cmd) {
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		return m, nil
	}

	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("PR poll error #%s: %s", msg.IssueID, msg.Err))
		return m, m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID)
	}

	// Coding stage: PR appearing = coding done → launch reviewer
	if slot.State == loop.SlotCoding {
		if msg.PR != nil {
			slot.State = loop.SlotLaunchingReviewer
			return m, m.loopWriteAndLaunchReviewerCmd(msg.ProjectID, msg.IssueID)
		}
		// No PR yet, keep polling
		return m, m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID)
	}

	// Reviewer stage: PR merged = done → cleanup
	if msg.PR == nil || msg.PR.State == "open" {
		return m, m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID)
	}

	if msg.PR.State == "merged" {
		slot.State = loop.SlotCleaningUp
		return m, m.loopCleanupCmd(msg.ProjectID, msg.IssueID)
	}

	slot.State = loop.SlotError
	slot.Error = fmt.Errorf("PR closed without merge")
	return m, nil
}

func (m Model) handleLoopCleanup(msg LoopCleanupMsg) (tea.Model, tea.Cmd) {
	ls, ok := m.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}

	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Cleanup error #%s: %s", msg.IssueID, msg.Err))
	} else {
		m.setStatus(fmt.Sprintf("Issue #%s done, worktree cleaned", msg.IssueID))
	}

	delete(ls.Slots, loop.SlotKey(msg.ProjectID, msg.IssueID))
	return m, nil
}

// findLoopSlotFromWorktree checks if a worktree with matching issue ID already exists.
// If found, returns a slot in SlotCoding state (will poll for PR).
func findLoopSlotFromWorktree(projectID string, issue tracker.Issue, worktrees []worktree.WorktreeInfo) *loop.Slot {
	for _, wt := range worktrees {
		if !strings.Contains(wt.Branch, issue.ID) {
			continue
		}
		return &loop.Slot{
			ProjectID:    projectID,
			IssueID:      issue.ID,
			IssueTitle:   issue.Title,
			BranchName:   wt.Branch,
			WorktreePath: wt.Path,
			State:        loop.SlotCoding, // PR poll will determine next step
		}
	}
	return nil
}

func (m Model) handleLoopOpenPRs(msg LoopOpenPRsMsg) (tea.Model, tea.Cmd) {
	ls, ok := m.loops[msg.ProjectID]
	if !ok || !ls.Active {
		return m, nil
	}
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Open PR scan error: %s", msg.Err))
		return m, nil
	}

	var cmds []tea.Cmd
	for _, pr := range msg.PRs {
		issueID := extractIssueID(pr.Branch)
		if issueID == "" {
			continue // not a Zpit-managed branch
		}
		key := loop.SlotKey(msg.ProjectID, issueID)
		if _, exists := ls.Slots[key]; exists {
			continue // already tracked
		}
		ls.Slots[key] = &loop.Slot{
			ProjectID:  msg.ProjectID,
			IssueID:    issueID,
			IssueTitle: pr.Title,
			BranchName: pr.Branch,
			State:      loop.SlotWaitingPRMerge,
		}
		cmds = append(cmds, m.loopSchedulePRPoll(msg.ProjectID, issueID))
	}
	return m, tea.Batch(cmds...)
}
