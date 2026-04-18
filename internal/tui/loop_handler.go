package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/worktree"
)

// hasLabel checks if a label list contains the given label (case-insensitive).
func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, target) {
			return true
		}
	}
	return false
}

// findLoopSlot looks up the LoopState and Slot for a project+issue.
// Returns nil, nil if not found.
// Caller must hold at least a read lock on state.
func (m Model) findLoopSlot(projectID, issueID string) (*loop.LoopState, *loop.Slot) {
	ls, ok := m.state.loops[projectID]
	if !ok {
		return nil, nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	return ls, slot
}

func (m Model) handleLoopPoll(msg LoopPollMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	ls, ok := m.state.loops[msg.ProjectID]
	if !ok || !ls.Active {
		m.state.Unlock()
		return m, nil
	}
	if msg.Err != nil {
		m.state.Unlock()
		m.state.logger.Printf("loop: poll error key=%s err=%v", msg.ProjectID, msg.Err)
		m.setStatus(fmt.Sprintf("Loop poll error: %s", msg.Err))
		// Tick handler has already rescheduled the next poll.
		return m, nil
	}

	// Deduplicate circular dependency logging: only log on first detection or change.
	cycleKey := strings.Join(msg.CycleIssueIDs, ",")
	if cycleKey != ls.ReportedCycleKey {
		if cycleKey != "" {
			ids := make([]string, len(msg.CycleIssueIDs))
			for i, id := range msg.CycleIssueIDs {
				ids[i] = "#" + id
			}
			m.state.logger.Printf("loop: circular dependency detected among issues: %s", strings.Join(ids, ", "))
		} else if ls.ReportedCycleKey != "" {
			prev := strings.Split(ls.ReportedCycleKey, ",")
			for i, id := range prev {
				prev[i] = "#" + id
			}
			m.state.logger.Printf("loop: circular dependency resolved (previously: %s)", strings.Join(prev, ", "))
		}
		ls.ReportedCycleKey = cycleKey
	}

	// Check existing worktrees to detect resumed issues.
	project := m.findProject(msg.ProjectID)
	var existingWorktrees []worktree.WorktreeInfo
	if project != nil {
		projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
		existingWorktrees, _ = m.state.wtManager.List(projectPath)
	}

	// Track which slots need cmd creation after unlock.
	type slotAction struct {
		issueID    string
		issueTitle string
		isResume   bool
	}
	var actions []slotAction

	for _, issue := range msg.Issues {
		key := loop.SlotKey(msg.ProjectID, issue.ID)
		if _, exists := ls.Slots[key]; exists {
			continue // already processing
		}
		if len(ls.Slots) >= m.state.cfg.Worktree.MaxPerProject {
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
			m.state.logger.Printf("loop: resume #%s from existing worktree (branch=%s)", issue.ID, slot.BranchName)
			actions = append(actions, slotAction{issueID: issue.ID, isResume: true})
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
		m.state.logger.Printf("loop: dispatch #%s → creating worktree", issue.ID)
		actions = append(actions, slotAction{issueID: issue.ID, issueTitle: issue.Title})
	}
	m.state.NotifyAll()
	m.state.Unlock()

	// Create cmds after releasing lock (cmd methods acquire their own RLock).
	// Worktree creation cmds are sequenced (not batched) to avoid concurrent
	// git operations that conflict on .git/config file locks (Windows).
	var cmds []tea.Cmd
	var wtCmds []tea.Cmd
	for _, a := range actions {
		if a.isResume {
			cmds = append(cmds, m.loopSchedulePRPoll(msg.ProjectID, a.issueID))
		} else {
			wtCmds = append(wtCmds, m.loopCreateWorktreeCmd(msg.ProjectID, a.issueID, a.issueTitle))
		}
	}
	if len(wtCmds) > 0 {
		cmds = append(cmds, tea.Sequence(wtCmds...))
	}
	// Tick handler has already rescheduled the next poll — no reschedule here.
	return m, tea.Batch(cmds...)
}

func (m Model) handleLoopWorktreeCreated(msg LoopWorktreeCreatedMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		m.state.Unlock()
		return m, nil
	}

	if msg.Err != nil {
		slot.State = loop.SlotError
		slot.Error = msg.Err
		m.state.logger.Printf("loop: worktree error #%s: %s", msg.IssueID, msg.Err)
		m.state.NotifyAll()
		m.state.Unlock()
		m.setStatus(fmt.Sprintf("Worktree error #%s: %s", msg.IssueID, msg.Err))
		return m, nil
	}

	slot.WorktreePath = msg.WorktreePath
	slot.BranchName = msg.BranchName
	slot.State = loop.SlotWritingAgent
	m.state.logger.Printf("loop: worktree created #%s path=%s branch=%s", msg.IssueID, msg.WorktreePath, msg.BranchName)
	m.state.NotifyAll()
	m.state.Unlock()
	return m, m.loopWriteAgentCmd(msg.ProjectID, msg.IssueID)
}

func (m Model) handleLoopAgentWritten(msg LoopAgentWrittenMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		m.state.Unlock()
		return m, nil
	}

	if msg.Err != nil {
		slot.State = loop.SlotError
		slot.Error = msg.Err
		m.state.logger.Printf("loop: agent write error #%s: %s", msg.IssueID, msg.Err)
		m.state.NotifyAll()
		m.state.Unlock()
		m.setStatus(fmt.Sprintf("Agent write error #%s: %s", msg.IssueID, msg.Err))
		return m, nil
	}

	slot.State = loop.SlotLaunchingCoder
	m.state.logger.Printf("loop: agent written #%s → launching coder", msg.IssueID)
	m.state.NotifyAll()
	m.state.Unlock()
	return m, m.loopLaunchCoderCmd(msg.ProjectID, msg.IssueID)
}

func (m Model) handleLoopAgentLaunched(msg LoopAgentLaunchedMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	defer m.state.Unlock()

	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		return m, nil
	}

	if msg.Err != nil {
		slot.State = loop.SlotError
		slot.Error = msg.Err
		m.state.logger.Printf("loop: %s launch error #%s: %s", msg.Role, msg.IssueID, msg.Err)
		m.state.NotifyAll()
		m.setStatus(fmt.Sprintf("Launch error #%s (%s): %s", msg.IssueID, msg.Role, msg.Err))
		return m, nil
	}

	slot.LaunchedAt = msg.LaunchedAt

	// Log and display any non-fatal warnings (e.g. WT profile resolution failures).
	if msg.Result != nil {
		for _, w := range msg.Result.Warnings {
			m.state.logger.Printf("loop: %s launch warning #%s: %s", msg.Role, msg.IssueID, w)
			m.setStatus(fmt.Sprintf("Warning (#%s %s): %s", msg.IssueID, msg.Role, w))
		}
	}

	if msg.Role == "coder" {
		slot.State = loop.SlotCoding
		m.state.logger.Printf("loop: coder launched #%s (round=%d)", msg.IssueID, slot.ReviewRound)
		m.state.NotifyAll()
		// loopScheduleLabelPoll only reads cfg (read-only), safe under write lock.
		return m, m.loopScheduleLabelPoll(msg.ProjectID, msg.IssueID)
	}

	slot.State = loop.SlotReviewing
	m.state.logger.Printf("loop: reviewer launched #%s (round=%d)", msg.IssueID, slot.ReviewRound)
	m.state.NotifyAll()
	return m, m.loopScheduleLabelPoll(msg.ProjectID, msg.IssueID)
}

func (m Model) handleLoopLabelPoll(msg LoopLabelPollMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		m.state.Unlock()
		return m, nil
	}

	if slot.State != loop.SlotCoding && slot.State != loop.SlotReviewing {
		m.state.Unlock()
		return m, nil
	}

	if msg.Err != nil {
		m.state.Unlock()
		m.state.logger.Printf("loop: label poll error key=%s issue=#%s err=%v", msg.ProjectID, msg.IssueID, msg.Err)
		m.setStatus(fmt.Sprintf("Label poll error #%s: %s", msg.IssueID, msg.Err))
		// Tick handler has already rescheduled.
		return m, nil
	}

	switch slot.State {
	case loop.SlotCoding:
		if hasLabel(msg.Labels, "review") {
			slot.State = loop.SlotLaunchingReviewer
			m.state.logger.Printf("loop: label 'review' found #%s → launching reviewer", msg.IssueID)
			m.state.NotifyAll()
			m.state.Unlock()
			return m, m.loopWriteAndLaunchReviewerCmd(msg.ProjectID, msg.IssueID)
		}
		m.state.Unlock()
		// Tick handler has already rescheduled.
		return m, nil

	case loop.SlotReviewing:
		if hasLabel(msg.Labels, "ai-review") {
			slot.State = loop.SlotWaitingPRMerge
			m.state.logger.Printf("loop: label 'ai-review' found #%s → waiting PR merge", msg.IssueID)
			m.state.NotifyAll()
			m.state.Unlock()
			return m, m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID)
		}
		if hasLabel(msg.Labels, "needs-changes") {
			maxRounds := m.state.cfg.Worktree.MaxReviewRounds
			if slot.ReviewRound >= maxRounds {
				slot.State = loop.SlotNeedsHuman
				m.state.logger.Printf("loop: #%s review rounds exhausted (%d), needs human", msg.IssueID, maxRounds)
				m.state.NotifyAll()
				m.state.Unlock()
				m.setStatus(fmt.Sprintf("Issue #%s: %d review rounds exhausted, needs human", msg.IssueID, maxRounds))
				projectName := m.projectName(msg.ProjectID)
				m.state.notifier.NotifyWaiting(msg.ProjectID, projectName,
					fmt.Sprintf("Issue #%s exceeded %d review rounds", msg.IssueID, maxRounds))
				if w := m.state.notifier.ConsumeWarning(); w != "" {
					m.setStatus(fmt.Sprintf(locale.T(locale.KeySoundFileNotFound), m.state.cfg.Notification.SoundFile))
				}
				return m, nil
			}
			slot.ReviewRound++
			slot.State = loop.SlotWritingAgent
			m.state.logger.Printf("loop: #%s needs changes, starting revision round %d/%d", msg.IssueID, slot.ReviewRound, maxRounds)
			m.state.NotifyAll()
			m.state.Unlock()
			m.setStatus(fmt.Sprintf("Issue #%s needs changes (round %d/%d), re-launching coder",
				msg.IssueID, slot.ReviewRound, maxRounds))
			return m, m.loopWriteRevisionAgentCmd(msg.ProjectID, msg.IssueID)
		}
		m.state.Unlock()
		// Tick handler has already rescheduled.
		return m, nil
	}

	m.state.Unlock()
	return m, nil
}

func (m Model) handleLoopPRStatus(msg LoopPRStatusMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	_, slot := m.findLoopSlot(msg.ProjectID, msg.IssueID)
	if slot == nil {
		m.state.Unlock()
		return m, nil
	}

	// Only process PR poll in WaitingPRMerge state; ignore stale ticks from other states.
	if slot.State != loop.SlotWaitingPRMerge {
		m.state.Unlock()
		return m, nil
	}

	if msg.Err != nil {
		m.state.Unlock()
		m.state.logger.Printf("loop: PR poll error key=%s issue=#%s err=%v", msg.ProjectID, msg.IssueID, msg.Err)
		m.setStatus(fmt.Sprintf("PR poll error #%s: %s", msg.IssueID, msg.Err))
		// Tick handler has already rescheduled.
		return m, nil
	}

	// PR merged = done → cleanup
	if msg.PR == nil || msg.PR.State == "open" {
		m.state.Unlock()
		// Tick handler has already rescheduled.
		return m, nil
	}

	if msg.PR.State == "merged" {
		slot.State = loop.SlotCleaningUp
		m.state.logger.Printf("loop: PR merged #%s → cleaning up", msg.IssueID)
		m.state.NotifyAll()
		m.state.Unlock()
		return m, m.loopCleanupCmd(msg.ProjectID, msg.IssueID)
	}

	slot.State = loop.SlotError
	slot.Error = fmt.Errorf("PR closed without merge")
	m.state.logger.Printf("loop: PR closed without merge #%s", msg.IssueID)
	m.state.NotifyAll()
	m.state.Unlock()
	return m, nil
}

// handleLoopPollTick is the tick-driven heartbeat for the todo-issue poll chain.
// Reschedules the next tick up-front so the chain survives any nil/error path
// in the poll cmd or handleLoopPoll. Stops only when the loop is inactive.
func (m Model) handleLoopPollTick(msg loopPollTickMsg) (tea.Model, tea.Cmd) {
	m.state.RLock()
	ls, ok := m.state.loops[msg.ProjectID]
	active := ok && ls.Active
	m.state.RUnlock()
	if !active {
		return m, nil
	}
	return m, tea.Batch(
		m.loopPollCmd(msg.ProjectID),
		m.loopSchedulePoll(msg.ProjectID),
	)
}

// handleLoopPRPollTick is the tick-driven heartbeat for the PR-status poll chain.
// Keeps ticking only while the slot is still in SlotWaitingPRMerge; state
// transitions (cleanup, error) are owned by handleLoopPRStatus.
func (m Model) handleLoopPRPollTick(msg loopPRPollTickMsg) (tea.Model, tea.Cmd) {
	m.state.RLock()
	keep := false
	if ls, ok := m.state.loops[msg.ProjectID]; ok && ls.Active {
		if slot, ok := ls.Slots[loop.SlotKey(msg.ProjectID, msg.IssueID)]; ok && slot.State == loop.SlotWaitingPRMerge {
			keep = true
		}
	}
	m.state.RUnlock()
	if !keep {
		return m, nil
	}
	return m, tea.Batch(
		m.loopPollPRCmd(msg.ProjectID, msg.IssueID),
		m.loopSchedulePRPoll(msg.ProjectID, msg.IssueID),
	)
}

// handleLoopLabelPollTick is the tick-driven heartbeat for the label-poll chain.
// Keeps ticking while the slot is in SlotCoding or SlotReviewing; any other
// state means the transition fired elsewhere and we stop.
func (m Model) handleLoopLabelPollTick(msg loopLabelPollTickMsg) (tea.Model, tea.Cmd) {
	m.state.RLock()
	keep := false
	if ls, ok := m.state.loops[msg.ProjectID]; ok && ls.Active {
		if slot, ok := ls.Slots[loop.SlotKey(msg.ProjectID, msg.IssueID)]; ok && (slot.State == loop.SlotCoding || slot.State == loop.SlotReviewing) {
			keep = true
		}
	}
	m.state.RUnlock()
	if !keep {
		return m, nil
	}
	return m, tea.Batch(
		m.loopPollLabelsCmd(msg.ProjectID, msg.IssueID),
		m.loopScheduleLabelPoll(msg.ProjectID, msg.IssueID),
	)
}

func (m Model) handleLoopCleanup(msg LoopCleanupMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	defer m.state.Unlock()

	ls, ok := m.state.loops[msg.ProjectID]
	if !ok {
		return m, nil
	}

	if msg.Err != nil {
		m.state.logger.Printf("loop: cleanup done #%s (worktree removal failed: %s)", msg.IssueID, msg.Err)
		m.setStatus(fmt.Sprintf("Issue #%s done (worktree removal failed: %s)", msg.IssueID, msg.Err))
	} else {
		m.state.logger.Printf("loop: cleanup done #%s", msg.IssueID)
		m.setStatus(fmt.Sprintf("Issue #%s done, worktree cleaned", msg.IssueID))
	}

	delete(ls.Slots, loop.SlotKey(msg.ProjectID, msg.IssueID))
	m.state.NotifyAll()
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
	m.state.Lock()
	ls, ok := m.state.loops[msg.ProjectID]
	if !ok || !ls.Active {
		m.state.Unlock()
		return m, nil
	}
	if msg.Err != nil {
		m.state.Unlock()
		m.state.logger.Printf("loop: open PR scan error key=%s err=%v", msg.ProjectID, msg.Err)
		m.setStatus(fmt.Sprintf("Open PR scan error: %s", msg.Err))
		return m, nil
	}

	// Query existing worktrees to populate WorktreePath for cleanup.
	project := m.findProject(msg.ProjectID)
	var worktrees []worktree.WorktreeInfo
	if project != nil {
		projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
		worktrees, _ = m.state.wtManager.List(projectPath)
	}

	// Track actions for cmd creation after unlock.
	type resumeAction struct {
		issueID string
		kind    string // "revision", "reviewer", "labelPoll", "prPoll"
	}
	var actions []resumeAction

	for _, pr := range msg.PRs {
		issueID := extractIssueID(pr.Branch)
		if issueID == "" {
			continue // not a Zpit-managed branch
		}
		key := loop.SlotKey(msg.ProjectID, issueID)
		if _, exists := ls.Slots[key]; exists {
			continue // already tracked
		}

		// Match worktree by branch name.
		var wtPath string
		for _, wt := range worktrees {
			if wt.Branch == pr.Branch {
				wtPath = wt.Path
				break
			}
		}

		slot := &loop.Slot{
			ProjectID:    msg.ProjectID,
			IssueID:      issueID,
			IssueTitle:   pr.Title,
			BranchName:   pr.Branch,
			WorktreePath: wtPath,
		}
		ls.Slots[key] = slot

		// Determine resume state from issue labels.
		labels := msg.IssueLabels[issueID]
		switch {
		case hasLabel(labels, "needs-changes"):
			slot.State = loop.SlotWritingAgent
			slot.ReviewRound = 1 // at least one round has passed
			m.state.logger.Printf("loop: resume #%s (branch=%s, label=needs-changes) → revision coder", issueID, pr.Branch)
			actions = append(actions, resumeAction{issueID: issueID, kind: "revision"})

		case hasLabel(labels, "review"):
			slot.State = loop.SlotLaunchingReviewer
			m.state.logger.Printf("loop: resume #%s (branch=%s, label=review) → launching reviewer", issueID, pr.Branch)
			actions = append(actions, resumeAction{issueID: issueID, kind: "reviewer"})

		case hasLabel(labels, "wip"):
			slot.State = loop.SlotCoding
			m.state.logger.Printf("loop: resume #%s (branch=%s, label=wip) → label poll", issueID, pr.Branch)
			actions = append(actions, resumeAction{issueID: issueID, kind: "labelPoll"})

		default: // ai-review or no matching label → waiting for merge
			slot.State = loop.SlotWaitingPRMerge
			m.state.logger.Printf("loop: resume #%s (branch=%s) → waiting PR merge", issueID, pr.Branch)
			actions = append(actions, resumeAction{issueID: issueID, kind: "prPoll"})
		}
	}
	m.state.NotifyAll()
	m.state.Unlock()

	// Create cmds after releasing lock.
	var cmds []tea.Cmd
	for _, a := range actions {
		switch a.kind {
		case "revision":
			cmds = append(cmds, m.loopWriteRevisionAgentCmd(msg.ProjectID, a.issueID))
		case "reviewer":
			cmds = append(cmds, m.loopWriteAndLaunchReviewerCmd(msg.ProjectID, a.issueID))
		case "labelPoll":
			cmds = append(cmds, m.loopScheduleLabelPoll(msg.ProjectID, a.issueID))
		case "prPoll":
			cmds = append(cmds, m.loopSchedulePRPoll(msg.ProjectID, a.issueID))
		}
	}
	return m, tea.Batch(cmds...)
}
