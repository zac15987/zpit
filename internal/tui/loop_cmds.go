package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	slug := worktree.Slugify(issueTitle, 40)
	branchName := fmt.Sprintf("feat/%s-%s", issueID, slug)
	mgr := m.wtManager
	hookMode := project.HookMode
	baseBranch := project.BaseBranch

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
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	client := m.clients[project.Tracker]
	repo := project.Repo
	wtPath := slot.WorktreePath
	logPolicy := ""
	if p, ok := m.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}
	baseBranch := project.BaseBranch

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
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	wtPath := slot.WorktreePath
	cfg := m.cfg.Terminal
	agentName := fmt.Sprintf("coding-%s", issueID)
	tabTitle := fmt.Sprintf("%s #%s", project.Name, issueID)

	return func() tea.Msg {
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg, "--agent", agentName)
		return LoopAgentLaunchedMsg{
			ProjectID: projectID, IssueID: issueID,
			Role: "coder", Result: result, Err: err,
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
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	client := m.clients[project.Tracker]
	repo := project.Repo
	wtPath := slot.WorktreePath
	cfg := m.cfg.Terminal
	logPolicy := ""
	if p, ok := m.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}
	baseBranch := project.BaseBranch
	tabTitle := fmt.Sprintf("%s #%s review", project.Name, issueID)

	return func() tea.Msg {
		// Fetch issue for reviewer prompt
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		issue, err := client.GetIssue(ctx, repo, issueID)
		if err != nil {
			return LoopAgentLaunchedMsg{ProjectID: projectID, IssueID: issueID, Role: "reviewer", Err: err}
		}

		spec, _ := tracker.ParseIssueSpec(issue.Body)
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
		content := fmt.Sprintf("---\nname: reviewer-%s\ndescription: Reviewer agent for issue %s\ntools: Read, Grep, Glob, Bash\ndisallowedTools: Write, Edit\n---\n\n%s",
			issueID, issueID, promptText)
		if err := os.WriteFile(filepath.Join(agentDir, agentFile), []byte(content), 0o644); err != nil {
			return LoopAgentLaunchedMsg{ProjectID: projectID, IssueID: issueID, Role: "reviewer", Err: err}
		}

		agentName := fmt.Sprintf("reviewer-%s", issueID)
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg, "--agent", agentName)
		return LoopAgentLaunchedMsg{
			ProjectID: projectID, IssueID: issueID,
			Role: "reviewer", Result: result, Err: err,
		}
	}
}

// loopStartWatcherCmd finds the session PID and monitors until exit.
func (m Model) loopStartWatcherCmd(projectID, issueID, role string) tea.Cmd {
	ls := m.loops[projectID]
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	wtPath := slot.WorktreePath

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return LoopAgentExitedMsg{ProjectID: projectID, IssueID: issueID, Role: role}
		}

		// Retry to find session (agent needs time to start)
		var pid int
		for attempt := 0; attempt < sessionRetryMax; attempt++ {
			sessions, err := watcher.FindActiveSessions(claudeHome, wtPath)
			if err == nil && len(sessions) > 0 {
				latest := sessions[0]
				for _, s := range sessions[1:] {
					if s.StartedAt > latest.StartedAt {
						latest = s
					}
				}
				pid = latest.PID
				break
			}
			time.Sleep(sessionRetryInterval)
		}

		if pid == 0 {
			return LoopAgentExitedMsg{ProjectID: projectID, IssueID: issueID, Role: role}
		}

		// Monitor PID until exit
		for {
			time.Sleep(5 * time.Second)
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
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	client := m.clients[project.Tracker]
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
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	wtPath := slot.WorktreePath
	mgr := m.wtManager

	return func() tea.Msg {
		err := mgr.Remove(projectPath, wtPath, true)
		return LoopCleanupMsg{ProjectID: projectID, IssueID: issueID, Err: err}
	}
}

// loopSchedulePoll schedules the next tracker poll after PollInterval.
func (m Model) loopSchedulePoll(projectID string) tea.Cmd {
	return tea.Tick(loop.PollInterval, func(t time.Time) tea.Msg {
		return loopPollTickMsg{ProjectID: projectID}
	})
}

// loopSchedulePRPoll schedules the next PR status poll after PRPollInterval.
func (m Model) loopSchedulePRPoll(projectID, issueID string) tea.Cmd {
	return tea.Tick(loop.PRPollInterval, func(t time.Time) tea.Msg {
		return loopPRPollTickMsg{ProjectID: projectID, IssueID: issueID}
	})
}
