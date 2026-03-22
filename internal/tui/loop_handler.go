package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/loop"
)

func (m Model) handleLoopPoll(msg LoopPollMsg) (tea.Model, tea.Cmd) {
	ls, ok := m.loops[msg.ProjectID]
	if !ok || !ls.Active {
		return m, nil
	}
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Loop poll error: %s", msg.Err))
		return m, m.loopSchedulePoll(msg.ProjectID)
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
		slot := &loop.Slot{
			ProjectID:  msg.ProjectID,
			IssueID:    issue.ID,
			IssueTitle: issue.Title,
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
	ls, ok := m.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}
	key := loop.SlotKey(msg.ProjectID, msg.IssueID)
	slot, ok := ls.Slots[key]
	if !ok {
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
	ls, ok := m.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}
	key := loop.SlotKey(msg.ProjectID, msg.IssueID)
	slot, ok := ls.Slots[key]
	if !ok {
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
	ls, ok := m.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}
	key := loop.SlotKey(msg.ProjectID, msg.IssueID)
	slot, ok := ls.Slots[key]
	if !ok {
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
	} else {
		slot.State = loop.SlotReviewing
	}

	return m, m.loopStartWatcherCmd(msg.ProjectID, msg.IssueID, msg.Role)
}

func (m Model) handleLoopAgentExited(msg LoopAgentExitedMsg) (tea.Model, tea.Cmd) {
	ls, ok := m.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}
	key := loop.SlotKey(msg.ProjectID, msg.IssueID)
	slot, ok := ls.Slots[key]
	if !ok {
		return m, nil
	}

	if msg.Role == "coder" {
		// Coding done → launch reviewer
		slot.State = loop.SlotLaunchingReviewer
		return m, m.loopWriteAndLaunchReviewerCmd(msg.ProjectID, msg.IssueID)
	}
	// Reviewer done → poll for PR merge
	slot.State = loop.SlotWaitingPRMerge
	return m, m.loopPollPRCmd(msg.ProjectID, msg.IssueID)
}

func (m Model) handleLoopPRStatus(msg LoopPRStatusMsg) (tea.Model, tea.Cmd) {
	ls, ok := m.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}
	key := loop.SlotKey(msg.ProjectID, msg.IssueID)
	slot, ok := ls.Slots[key]
	if !ok {
		return m, nil
	}

	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("PR poll error #%s: %s", msg.IssueID, msg.Err))
		return m, m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID)
	}

	if msg.PR == nil || msg.PR.State == "open" {
		// Not merged yet, keep polling
		return m, m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID)
	}

	if msg.PR.State == "merged" {
		slot.State = loop.SlotCleaningUp
		return m, m.loopCleanupCmd(msg.ProjectID, msg.IssueID)
	}

	// PR closed without merge
	slot.State = loop.SlotError
	slot.Error = fmt.Errorf("PR closed without merge")
	return m, nil
}

func (m Model) handleLoopCleanup(msg LoopCleanupMsg) (tea.Model, tea.Cmd) {
	ls, ok := m.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}
	key := loop.SlotKey(msg.ProjectID, msg.IssueID)

	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Cleanup error #%s: %s", msg.IssueID, msg.Err))
	} else {
		m.setStatus(fmt.Sprintf("Issue #%s done, worktree cleaned", msg.IssueID))
	}

	delete(ls.Slots, key)
	return m, nil
}
