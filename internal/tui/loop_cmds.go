package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/prompt"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

// loopPollCmd polls the tracker for todo issues.
func (m Model) loopPollCmd(projectID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issues, err := client.ListIssues(ctx, repo)
		if err != nil {
			return LoopPollMsg{ProjectID: projectID, Err: err}
		}
		var todoIssues []tracker.Issue
		for _, issue := range issues {
			if issue.Status == tracker.StatusTodo {
				todoIssues = append(todoIssues, issue)
			}
		}
		return LoopPollMsg{ProjectID: projectID, Issues: todoIssues}
	}
}

// loopCreateWorktreeCmd creates a worktree for the given issue.
func (m Model) loopCreateWorktreeCmd(projectID, issueID, issueTitle string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	slug := worktree.Slugify(issueTitle, 40)
	branchName := fmt.Sprintf("feat/%s-%s", issueID, slug)
	mgr := m.wtManager
	hookMode := project.HookMode
	baseBranch := slot.BaseBranch

	return func() tea.Msg {
		wtPath, err := mgr.Create(worktree.CreateParams{
			RepoPath:   projectPath,
			BaseBranch: baseBranch,
			BranchName: branchName,
			ProjectID:  projectID,
			IssueID:    issueID,
			Slug:       slug,
		})
		if err != nil {
			return LoopWorktreeCreatedMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}
		if err := worktree.SetupHookMode(wtPath, hookMode); err != nil {
			return LoopWorktreeCreatedMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}
		return LoopWorktreeCreatedMsg{
			ProjectID:    projectID,
			IssueID:      issueID,
			WorktreePath: wtPath,
			BranchName:   branchName,
		}
	}
}

// loopWriteAgentCmd fetches the issue, parses spec, builds prompt, writes temp agent file.
func (m Model) loopWriteAgentCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	wtPath := slot.WorktreePath
	logPolicy := ""
	if p, ok := m.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}
	baseBranch := slot.BaseBranch

	// Build tracker doc content outside closure (avoid accessing m.cfg inside goroutine)
	var trackerDocContent string
	if provider, ok := m.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, repo, provider.TokenEnv, project.BaseBranch)
	}
	agentGuidelines := m.agentGuidelinesMD
	codeConstructionPrinciples := m.codeConstructionPrinciplesMD

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		issue, err := client.GetIssue(ctx, repo, issueID)
		if err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		missing := tracker.ValidateIssueSpec(issue.Body)
		if len(missing) > 0 {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID,
				Err: fmt.Errorf("issue #%s missing sections: %v", issueID, missing)}
		}

		spec, err := tracker.ParseIssueSpec(issue.Body)
		if err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		promptText := prompt.BuildCodingPrompt(prompt.CodingParams{
			IssueID:    issueID,
			IssueTitle: issue.Title,
			Spec:       spec,
			LogPolicy:  logPolicy,
			BaseBranch: baseBranch,
		})

		// Deploy docs to worktree
		if trackerDocContent != "" {
			docsDir := filepath.Join(wtPath, ".claude", "docs")
			_ = os.MkdirAll(docsDir, 0o755)
			_ = os.WriteFile(filepath.Join(docsDir, "tracker.md"), []byte(trackerDocContent), 0o644)
		}
		deployStaticDocs(wtPath, agentGuidelines, codeConstructionPrinciples)

		agentDir := filepath.Join(wtPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		agentFile := fmt.Sprintf("coding-%s.md", issueID)
		content := fmt.Sprintf("---\nname: coding-%s\ndescription: Coding agent for issue %s\n---\n\n%s",
			issueID, issueID, promptText)
		if err := os.WriteFile(filepath.Join(agentDir, agentFile), []byte(content), 0o644); err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID}
	}
}

// loopLaunchCoderCmd launches the coding agent in a new terminal.
func (m Model) loopLaunchCoderCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	wtPath := slot.WorktreePath
	cfg := m.cfg.Terminal
	agentName := fmt.Sprintf("coding-%s", issueID)
	tabTitle := fmt.Sprintf("%s #%s", project.Name, issueID)

	initMsg := "開始實作"
	if slot.ReviewRound > 0 {
		initMsg = "讀取 PR review comment，修正問題"
	}

	return func() tea.Msg {
		launchedAt := time.Now().Unix()
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg, "--agent", agentName, initMsg)
		return LoopAgentLaunchedMsg{
			ProjectID: projectID, IssueID: issueID,
			Role: "coder", LaunchedAt: launchedAt,
			Result: result, Err: err,
		}
	}
}

// loopWriteAndLaunchReviewerCmd writes the reviewer agent file and launches it.
func (m Model) loopWriteAndLaunchReviewerCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	wtPath := slot.WorktreePath
	cfg := m.cfg.Terminal
	logPolicy := ""
	if p, ok := m.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}
	baseBranch := slot.BaseBranch
	tabTitle := fmt.Sprintf("%s #%s review", project.Name, issueID)

	return func() tea.Msg {
		// Fetch issue for reviewer prompt
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		issue, err := client.GetIssue(ctx, repo, issueID)
		if err != nil {
			return LoopAgentLaunchedMsg{ProjectID: projectID, IssueID: issueID, Role: "reviewer", Err: err}
		}

		spec, err := tracker.ParseIssueSpec(issue.Body)
		if err != nil {
			return LoopAgentLaunchedMsg{ProjectID: projectID, IssueID: issueID, Role: "reviewer", Err: err}
		}
		promptText := prompt.BuildReviewerPrompt(prompt.ReviewerParams{
			IssueID:    issueID,
			IssueTitle: issue.Title,
			Spec:       spec,
			LogPolicy:  logPolicy,
			BaseBranch: baseBranch,
		})

		agentDir := filepath.Join(wtPath, ".claude", "agents")
		_ = os.MkdirAll(agentDir, 0o755)
		agentFile := fmt.Sprintf("reviewer-%s.md", issueID)
		content := fmt.Sprintf("---\nname: reviewer-%s\ndescription: Reviewer agent for issue %s\ndisallowedTools: Write, Edit\n---\n\n%s",
			issueID, issueID, promptText)
		if err := os.WriteFile(filepath.Join(agentDir, agentFile), []byte(content), 0o644); err != nil {
			return LoopAgentLaunchedMsg{ProjectID: projectID, IssueID: issueID, Role: "reviewer", Err: err}
		}

		agentName := fmt.Sprintf("reviewer-%s", issueID)
		launchedAt := time.Now().Unix()
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg, "--agent", agentName, "開始 review")
		return LoopAgentLaunchedMsg{
			ProjectID: projectID, IssueID: issueID,
			Role: "reviewer", LaunchedAt: launchedAt,
			Result: result, Err: err,
		}
	}
}

// findNewSessionPID retries finding a session started after launchedAfter.
// Returns 0 if no matching session is found within the retry window.
func findNewSessionPID(claudeHome, wtPath string, launchedAfter int64) int {
	for range sessionRetryMax {
		sessions, _ := watcher.FindActiveSessions(claudeHome, wtPath)
		var best int
		var bestStarted int64
		for _, s := range sessions {
			if s.StartedAt > launchedAfter && s.StartedAt > bestStarted {
				bestStarted = s.StartedAt
				best = s.PID
			}
		}
		if best != 0 {
			return best
		}
		time.Sleep(sessionRetryInterval)
	}
	return 0
}

// loopStartWatcherCmd finds the session PID and monitors until exit.
func (m Model) loopStartWatcherCmd(projectID, issueID, role string) tea.Cmd {
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	wtPath := slot.WorktreePath
	launchedAfter := slot.LaunchedAt
	logger := m.logger

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			logger.Printf("loop: watcher failed #%s role=%s (ClaudeHome error: %v)", issueID, role, err)
			return LoopAgentExitedMsg{ProjectID: projectID, IssueID: issueID, Role: role}
		}

		pid := findNewSessionPID(claudeHome, wtPath, launchedAfter)

		if pid == 0 {
			logger.Printf("loop: watcher failed #%s role=%s (no session found after %d)", issueID, role, launchedAfter)
			return LoopAgentExitedMsg{ProjectID: projectID, IssueID: issueID, Role: role}
		}

		logger.Printf("loop: watcher attached #%s role=%s PID=%d", issueID, role, pid)

		// Monitor PID until exit
		for {
			time.Sleep(loop.LivenessInterval)
			if !watcher.IsProcessAlive(pid) {
				return LoopAgentExitedMsg{ProjectID: projectID, IssueID: issueID, Role: role}
			}
		}
	}
}

// loopPollPRCmd polls tracker for PR by branch name.
func (m Model) loopPollPRCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	branch := slot.BranchName

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		pr, err := client.FindPRByBranch(ctx, repo, branch)
		return LoopPRStatusMsg{ProjectID: projectID, IssueID: issueID, PR: pr, Err: err}
	}
}

// loopCleanupCmd removes the worktree and branch.
func (m Model) loopCleanupCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	wtPath := slot.WorktreePath
	mgr := m.wtManager

	return func() tea.Msg {
		err := mgr.Remove(projectPath, wtPath, true)
		return LoopCleanupMsg{ProjectID: projectID, IssueID: issueID, Err: err}
	}
}

// loopCleanupMergedCmd cleans up worktrees whose PR has been merged (leftover from previous sessions).
func (m Model) loopCleanupMergedCmd(projectID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	mgr := m.wtManager

	return func() tea.Msg {
		worktrees, err := mgr.List(projectPath)
		if err != nil || len(worktrees) == 0 {
			return nil
		}

		cleaned := 0
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		for _, wt := range worktrees {
			pr, err := client.FindPRByBranch(ctx, repo, wt.Branch)
			if err != nil || pr == nil {
				continue
			}
			if pr.State == "merged" {
				if err := mgr.Remove(projectPath, wt.Path, true); err == nil {
					cleaned++
				}
			}
		}

		if cleaned > 0 {
			return StatusMsg{Text: fmt.Sprintf("Cleaned %d merged worktree(s)", cleaned)}
		}
		return nil
	}
}

// loopSchedulePoll schedules the next tracker poll after configured interval.
func (m Model) loopSchedulePoll(projectID string) tea.Cmd {
	interval := time.Duration(m.cfg.Worktree.PollSeconds) * time.Second
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return loopPollTickMsg{ProjectID: projectID}
	})
}

// loopSchedulePRPoll schedules the next PR status poll after configured interval.
func (m Model) loopSchedulePRPoll(projectID, issueID string) tea.Cmd {
	interval := time.Duration(m.cfg.Worktree.PRPollSeconds) * time.Second
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return loopPRPollTickMsg{ProjectID: projectID, IssueID: issueID}
	})
}

// loopScanOpenPRsCmd queries open PRs to detect issues waiting for merge.
func (m Model) loopScanOpenPRsCmd(projectID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		prs, err := client.ListOpenPRs(ctx, repo)
		return LoopOpenPRsMsg{ProjectID: projectID, PRs: prs, Err: err}
	}
}

// extractIssueID extracts issue ID from branch name like "feat/19-slug".
func extractIssueID(branch string) string {
	after, ok := strings.CutPrefix(branch, "feat/")
	if !ok {
		return ""
	}
	idx := strings.Index(after, "-")
	if idx < 0 {
		return after
	}
	return after[:idx]
}

// loopCheckReviewResultCmd fetches issue labels to determine reviewer verdict.
func (m Model) loopCheckReviewResultCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		issue, err := client.GetIssue(ctx, repo, issueID)
		if err != nil {
			return LoopReviewResultMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		verdict := loop.VerdictUnknown
		for _, label := range issue.Labels {
			lower := strings.ToLower(label)
			if lower == "ai-review" {
				verdict = loop.VerdictApproved
				break
			}
			if lower == "needs-changes" {
				verdict = loop.VerdictNeedsChanges
				break
			}
		}

		return LoopReviewResultMsg{ProjectID: projectID, IssueID: issueID, Verdict: verdict}
	}
}

// loopWriteRevisionAgentCmd writes a revision coding agent prompt and returns LoopAgentWrittenMsg.
func (m Model) loopWriteRevisionAgentCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.clients[project.Tracker]
	if !ok {
		return nil
	}
	ls := m.loops[projectID]
	if ls == nil {
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		return nil
	}
	repo := project.Repo
	wtPath := slot.WorktreePath
	logPolicy := ""
	if p, ok := m.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}
	baseBranch := slot.BaseBranch
	reviewRound := slot.ReviewRound

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		issue, err := client.GetIssue(ctx, repo, issueID)
		if err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		spec, err := tracker.ParseIssueSpec(issue.Body)
		if err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		promptText := prompt.BuildRevisionPrompt(prompt.RevisionParams{
			IssueID:     issueID,
			IssueTitle:  issue.Title,
			Spec:        spec,
			LogPolicy:   logPolicy,
			BaseBranch:  baseBranch,
			ReviewRound: reviewRound,
		})

		agentDir := filepath.Join(wtPath, ".claude", "agents")
		_ = os.MkdirAll(agentDir, 0o755)
		agentFile := fmt.Sprintf("coding-%s.md", issueID)
		content := fmt.Sprintf("---\nname: coding-%s\ndescription: Revision coding agent for issue %s (round %d)\n---\n\n%s",
			issueID, issueID, reviewRound, promptText)
		if err := os.WriteFile(filepath.Join(agentDir, agentFile), []byte(content), 0o644); err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID}
	}
}
