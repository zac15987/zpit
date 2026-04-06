package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/prompt"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/worktree"
)

// loopPollCmd polls the tracker for todo issues.
// After filtering for "todo" status, it parses each issue's DEPENDS_ON section,
// queries dependency issue states via GetIssue, and excludes issues with unclosed
// dependencies. Circular dependencies are detected and excluded with a log warning.
// Only reads read-only fields (clients, projects) — no lock needed.
func (m Model) loopPollCmd(projectID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	logger := m.state.logger
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

		// Build dependency graph: issueID → list of dependency issue IDs.
		depGraph := make(map[string][]string)
		for _, issue := range todoIssues {
			spec, err := tracker.ParseIssueSpec(issue.Body)
			if err != nil || len(spec.DependsOn) == 0 {
				continue
			}
			depGraph[issue.ID] = spec.DependsOn
		}

		// Detect circular dependencies via topological sort.
		cycleIssues := detectCycles(depGraph)
		var cycleIDs []string
		if len(cycleIssues) > 0 {
			cycleIDs = make([]string, 0, len(cycleIssues))
			for id := range cycleIssues {
				cycleIDs = append(cycleIDs, id)
			}
			sort.Strings(cycleIDs)
		}

		// Filter: only include issues whose dependencies are all closed.
		var eligible []tracker.Issue
		for _, issue := range todoIssues {
			if cycleIssues[issue.ID] {
				continue // part of a cycle
			}
			deps := depGraph[issue.ID]
			if len(deps) == 0 {
				eligible = append(eligible, issue)
				continue
			}
			allClosed := true
			for _, depID := range deps {
				depIssue, err := client.GetIssue(ctx, repo, depID)
				if err != nil {
					logger.Printf("loop: failed to check dependency #%s for issue #%s: %v", depID, issue.ID, err)
					allClosed = false
					break
				}
				if depIssue.Status != tracker.StatusDone {
					allClosed = false
					break
				}
			}
			if allClosed {
				eligible = append(eligible, issue)
			}
		}

		return LoopPollMsg{ProjectID: projectID, Issues: eligible, CycleIssueIDs: cycleIDs}
	}
}

// detectCycles finds all issue IDs involved in circular dependencies.
// Uses iterative DFS with three-color marking (white/gray/black).
func detectCycles(graph map[string][]string) map[string]bool {
	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully processed
	)

	color := make(map[string]int)
	parent := make(map[string]string) // tracks DFS parent for cycle extraction
	cycleNodes := make(map[string]bool)

	// Collect all nodes (both sources and targets).
	allNodes := make(map[string]bool)
	for id, deps := range graph {
		allNodes[id] = true
		for _, d := range deps {
			allNodes[d] = true
		}
	}

	for node := range allNodes {
		if color[node] != white {
			continue
		}
		// Iterative DFS using an explicit stack.
		type frame struct {
			node string
			idx  int // index into graph[node] adjacency list
		}
		stack := []frame{{node: node, idx: 0}}
		color[node] = gray

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			deps := graph[top.node]
			if top.idx >= len(deps) {
				// Done processing this node.
				color[top.node] = black
				stack = stack[:len(stack)-1]
				continue
			}
			next := deps[top.idx]
			top.idx++

			switch color[next] {
			case white:
				color[next] = gray
				parent[next] = top.node
				stack = append(stack, frame{node: next, idx: 0})
			case gray:
				// Back edge found — mark all nodes in the cycle.
				cycleNodes[next] = true
				cur := top.node
				for cur != next {
					cycleNodes[cur] = true
					cur = parent[cur]
				}
			}
		}
	}

	return cycleNodes
}

// loopCreateWorktreeCmd creates a worktree for the given issue.
// Acquires RLock to read loops map, then releases before returning the Cmd closure.
// If ChannelEnabled, writes .mcp.json to the worktree root pointing to the broker.
func (m Model) loopCreateWorktreeCmd(projectID, issueID, issueTitle string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	m.state.RLock()
	ls := m.state.loops[projectID]
	if ls == nil {
		m.state.RUnlock()
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		m.state.RUnlock()
		return nil
	}
	baseBranch := slot.BaseBranch
	m.state.RUnlock()

	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	slug := worktree.Slugify(issueTitle, 40)
	branchName := fmt.Sprintf("feat/%s-%s", issueID, slug)
	mgr := m.state.wtManager
	hookMode := project.HookMode
	hookScripts := m.state.hookScripts
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin
	logger := m.state.logger

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
		worktree.EnsureGitignore(wtPath)
		worktree.EnsureGitattributes(projectPath)
		if err := worktree.DeployHooksToWorktree(wtPath, hookMode, hookScripts); err != nil {
			return LoopWorktreeCreatedMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		// Write .mcp.json for channel communication if enabled.
		if channelEnabled && brokerAddr != "" {
			loopAgentName := fmt.Sprintf("coding-#%s", issueID)
			if err := writeMCPConfig(wtPath, brokerAddr, projectID, issueID, zpitBin, loopAgentName, "coding", channelListen); err != nil {
				logger.Printf("loop: failed to write .mcp.json for issue #%s: %v", issueID, err)
			} else {
				logger.Printf("loop: wrote .mcp.json to %s for issue #%s", wtPath, issueID)
			}
		}

		return LoopWorktreeCreatedMsg{
			ProjectID:    projectID,
			IssueID:      issueID,
			WorktreePath: wtPath,
			BranchName:   branchName,
		}
	}
}

// writeMCPConfig writes a .mcp.json file to the target directory, configuring
// the zpit-channel MCP server to connect to the broker.
// zpitBinOverride is used as the executable path if non-empty; otherwise falls back to os.Executable().
func writeMCPConfig(targetDir, brokerAddr, projectID, issueID, zpitBinOverride, agentName, agentType string, listenProjects []string) error {
	zpitBin := zpitBinOverride
	if zpitBin == "" {
		var err error
		zpitBin, err = os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
	}

	// Build the .mcp.json structure.
	mcpConfig := map[string]any{
		"mcpServers": map[string]any{
			"zpit-channel": map[string]any{
				"command": zpitBin,
				"args":    []string{"serve-channel"},
				"env": func() map[string]string {
					env := map[string]string{
						"ZPIT_BROKER_URL": "http://" + brokerAddr,
						"ZPIT_PROJECT_ID": projectID,
						"ZPIT_ISSUE_ID":   issueID,
					}
					if agentName != "" {
						env["ZPIT_AGENT_NAME"] = agentName
					}
					if agentType != "" {
						env["ZPIT_AGENT_TYPE"] = agentType
					}
					if len(listenProjects) > 0 {
						env["ZPIT_LISTEN_PROJECTS"] = strings.Join(listenProjects, ",")
					}
					return env
				}(),
			},
		},
	}

	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}

	return os.WriteFile(filepath.Join(targetDir, ".mcp.json"), data, 0o644)
}

// loopWriteAgentCmd fetches the issue, parses spec, builds prompt, writes temp agent file.
// Acquires RLock to read loops map, then releases before returning the Cmd closure.
func (m Model) loopWriteAgentCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	m.state.RLock()
	ls := m.state.loops[projectID]
	if ls == nil {
		m.state.RUnlock()
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		m.state.RUnlock()
		return nil
	}
	wtPath := slot.WorktreePath
	baseBranch := slot.BaseBranch
	m.state.RUnlock()

	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	logPolicy := ""
	if p, ok := m.state.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}

	// Build tracker doc content outside closure (avoid accessing m.state.cfg inside goroutine)
	var trackerDocContent string
	if provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, repo, provider.TokenEnv, project.BaseBranch)
	}
	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	taskRunnerMD := m.state.taskRunnerMD
	hookScripts := m.state.hookScripts
	hookMode := project.HookMode
	channelEnabled := project.ChannelEnabled
	logger := m.state.logger

	return func() tea.Msg {
		// Safety-net: ensure hooks + gitignore exist (handles resume from previous session)
		worktree.EnsureGitignore(wtPath)
		_ = worktree.DeployHooksToWorktree(wtPath, hookMode, hookScripts)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		issue, err := client.GetIssue(ctx, repo, issueID)
		if err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		validation := tracker.ValidateIssueSpec(issue.Body)
		if len(validation.Errors) > 0 {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID,
				Err: fmt.Errorf("issue #%s validation errors: %v", issueID, validation.Errors)}
		}

		spec, err := tracker.ParseIssueSpec(issue.Body)
		if err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		promptText := prompt.BuildCodingPrompt(prompt.CodingParams{
			IssueID:        issueID,
			IssueTitle:     issue.Title,
			Spec:           spec,
			LogPolicy:      logPolicy,
			BaseBranch:     baseBranch,
			ChannelEnabled: channelEnabled,
		})

		deployDocs(wtPath, trackerDocContent, agentGuidelines, codeConstructionPrinciples)

		agentDir := filepath.Join(wtPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return LoopAgentWrittenMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}

		// Deploy task-runner.md when Issue Spec contains TASKS for subagent delegation.
		if len(spec.Tasks) > 0 && len(taskRunnerMD) > 0 {
			trPath := filepath.Join(agentDir, "task-runner.md")
			if err := os.WriteFile(trPath, taskRunnerMD, 0o644); err != nil {
				logger.Printf("loop: failed to deploy task-runner.md for issue #%s: %v", issueID, err)
			} else {
				logger.Printf("loop: deployed task-runner.md to %s for issue #%s", agentDir, issueID)
			}
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
// Acquires RLock to read loops map, then releases before returning the Cmd closure.
func (m Model) loopLaunchCoderCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	m.state.RLock()
	ls := m.state.loops[projectID]
	if ls == nil {
		m.state.RUnlock()
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		m.state.RUnlock()
		return nil
	}
	wtPath := slot.WorktreePath
	reviewRound := slot.ReviewRound
	m.state.RUnlock()

	cfg := m.state.cfg.Terminal
	agentName := fmt.Sprintf("coding-%s", issueID)
	tabTitle := fmt.Sprintf("%s #%s", project.Name, issueID)
	channelEnabled := project.ChannelEnabled

	initMsg := locale.T(locale.KeyInitCoding)
	if reviewRound > 0 {
		initMsg = locale.T(locale.KeyInitRevisionCoding)
	}

	return func() tea.Msg {
		launchedAt := time.Now().Unix()
		args := []string{"--agent", agentName, initMsg}
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg, args...)
		return LoopAgentLaunchedMsg{
			ProjectID: projectID, IssueID: issueID,
			Role: "coder", LaunchedAt: launchedAt,
			Result: result, Err: err,
		}
	}
}

// loopWriteAndLaunchReviewerCmd writes the reviewer agent file and launches it.
// Acquires RLock to read loops map, then releases before returning the Cmd closure.
func (m Model) loopWriteAndLaunchReviewerCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	m.state.RLock()
	ls := m.state.loops[projectID]
	if ls == nil {
		m.state.RUnlock()
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		m.state.RUnlock()
		return nil
	}
	wtPath := slot.WorktreePath
	baseBranch := slot.BaseBranch
	reviewRound := slot.ReviewRound
	m.state.RUnlock()

	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	cfg := m.state.cfg.Terminal
	logPolicy := ""
	if p, ok := m.state.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}
	tabTitle := fmt.Sprintf("%s #%s review", project.Name, issueID)
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin
	logger := m.state.logger
	hookScripts := m.state.hookScripts
	hookMode := project.HookMode
	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	reviewerDisallowed := prompt.FrontmatterField(m.state.reviewerMD, "disallowedTools")
	var trackerDocContent string
	if provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, repo, provider.TokenEnv, project.BaseBranch)
	}

	return func() tea.Msg {
		// Safety-net: ensure hooks + docs + gitignore exist
		worktree.EnsureGitignore(wtPath)
		_ = worktree.DeployHooksToWorktree(wtPath, hookMode, hookScripts)
		deployDocs(wtPath, trackerDocContent, agentGuidelines, codeConstructionPrinciples)

		// Rewrite .mcp.json with reviewer agent type so SSE connection registers as "reviewer"
		// (the worktree's existing .mcp.json has agent_type=coding from the coding agent).
		if channelEnabled && brokerAddr != "" {
			reviewerAgentName := fmt.Sprintf("reviewer-#%s", issueID)
			if err := writeMCPConfig(wtPath, brokerAddr, projectID, issueID, zpitBin, reviewerAgentName, "reviewer", channelListen); err != nil {
				logger.Printf("loop-reviewer: failed to rewrite .mcp.json for issue #%s: %v", issueID, err)
			} else {
				logger.Printf("loop-reviewer: rewrote .mcp.json to %s for issue #%s agent=%s", wtPath, issueID, reviewerAgentName)
			}
		}

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
			IssueID:     issueID,
			IssueTitle:  issue.Title,
			Spec:        spec,
			LogPolicy:   logPolicy,
			BaseBranch:  baseBranch,
			ReviewRound: reviewRound,
		})

		agentDir := filepath.Join(wtPath, ".claude", "agents")
		_ = os.MkdirAll(agentDir, 0o755)
		agentFile := fmt.Sprintf("reviewer-%s.md", issueID)
		var disallowedLine string
		if reviewerDisallowed != "" {
			disallowedLine = fmt.Sprintf("disallowedTools: %s\n", reviewerDisallowed)
		}
		content := fmt.Sprintf("---\nname: reviewer-%s\ndescription: Reviewer agent for issue %s\n%s---\n\n%s",
			issueID, issueID, disallowedLine, promptText)
		if err := os.WriteFile(filepath.Join(agentDir, agentFile), []byte(content), 0o644); err != nil {
			return LoopAgentLaunchedMsg{ProjectID: projectID, IssueID: issueID, Role: "reviewer", Err: err}
		}

		agentName := fmt.Sprintf("reviewer-%s", issueID)
		launchedAt := time.Now().Unix()
		initMsg := locale.T(locale.KeyInitReview)
		if reviewRound > 0 {
			initMsg = locale.T(locale.KeyInitRevisionReview)
		}
		args := []string{"--agent", agentName, initMsg}
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg, args...)
		return LoopAgentLaunchedMsg{
			ProjectID: projectID, IssueID: issueID,
			Role: "reviewer", LaunchedAt: launchedAt,
			Result: result, Err: err,
		}
	}
}

// loopPollPRCmd polls tracker for PR by branch name.
// Acquires RLock to read loops map, then releases before returning the Cmd closure.
func (m Model) loopPollPRCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	m.state.RLock()
	ls := m.state.loops[projectID]
	if ls == nil {
		m.state.RUnlock()
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		m.state.RUnlock()
		return nil
	}
	branch := slot.BranchName
	m.state.RUnlock()

	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		pr, err := client.FindPRByBranch(ctx, repo, branch)
		return LoopPRStatusMsg{ProjectID: projectID, IssueID: issueID, PR: pr, Err: err}
	}
}

// loopCleanupCmd removes the worktree and branch, then closes the issue.
// Acquires RLock to read loops map, then releases before returning the Cmd closure.
func (m Model) loopCleanupCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)

	m.state.RLock()
	ls := m.state.loops[projectID]
	if ls == nil {
		m.state.RUnlock()
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		m.state.RUnlock()
		return nil
	}
	wtPath := slot.WorktreePath
	m.state.RUnlock()

	mgr := m.state.wtManager
	client, ok := m.state.clients[project.Tracker]
	repo := project.Repo
	logger := m.state.logger

	return func() tea.Msg {
		// Try worktree removal — failure is logged but does not block issue closing.
		// On Windows, Remove() often fails because the agent process still holds the CWD lock.
		removeErr := mgr.Remove(projectPath, wtPath, true)
		if removeErr != nil {
			logger.Printf("loop: worktree remove failed #%s: %v (will still close issue)", issueID, removeErr)
		}

		// Always close the issue regardless of worktree removal result.
		if ok && client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if closeErr := client.CloseIssue(ctx, repo, issueID); closeErr != nil {
				logger.Printf("loop: failed to close issue #%s: %v", issueID, closeErr)
			}
		}

		return LoopCleanupMsg{ProjectID: projectID, IssueID: issueID, Err: removeErr}
	}
}

// loopCleanupMergedCmd cleans up worktrees whose PR has been merged (leftover from previous sessions).
// Also closes the associated issue if still open.
// Only reads read-only fields (clients, wtManager) — no lock needed.
func (m Model) loopCleanupMergedCmd(projectID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	mgr := m.state.wtManager
	logger := m.state.logger

	return func() tea.Msg {
		worktrees, err := mgr.List(projectPath)
		if err != nil || len(worktrees) == 0 {
			return nil
		}

		cleaned, closed := 0, 0
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
				} else {
					logger.Printf("loop: merged worktree remove failed branch=%s: %v", wt.Branch, err)
				}

				// Close the associated issue (best-effort, independent of worktree removal).
				issueID := extractIssueID(wt.Branch)
				if issueID != "" {
					if err := client.CloseIssue(ctx, repo, issueID); err != nil {
						logger.Printf("loop: failed to close issue #%s for merged branch %s: %v", issueID, wt.Branch, err)
					} else {
						closed++
					}
				}
			}
		}

		if cleaned > 0 || closed > 0 {
			return StatusMsg{Text: fmt.Sprintf("Cleaned %d merged worktree(s), closed %d issue(s)", cleaned, closed)}
		}
		return nil
	}
}

// loopSchedulePoll schedules the next tracker poll after configured interval.
// Only reads read-only field (cfg) — no lock needed.
func (m Model) loopSchedulePoll(projectID string) tea.Cmd {
	interval := time.Duration(m.state.cfg.Worktree.PollSeconds) * time.Second
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return loopPollTickMsg{ProjectID: projectID}
	})
}

// loopSchedulePRPoll schedules the next PR status poll after configured interval.
// Only reads read-only field (cfg) — no lock needed.
func (m Model) loopSchedulePRPoll(projectID, issueID string) tea.Cmd {
	interval := time.Duration(m.state.cfg.Worktree.PRPollSeconds) * time.Second
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return loopPRPollTickMsg{ProjectID: projectID, IssueID: issueID}
	})
}

// loopPollLabelsCmd fetches issue labels from tracker for state transitions.
// Only reads read-only fields (clients) — no lock needed.
func (m Model) loopPollLabelsCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		issue, err := client.GetIssue(ctx, repo, issueID)
		if err != nil {
			return LoopLabelPollMsg{ProjectID: projectID, IssueID: issueID, Err: err}
		}
		return LoopLabelPollMsg{ProjectID: projectID, IssueID: issueID, Labels: issue.Labels}
	}
}

// loopScheduleLabelPoll schedules the next label poll after configured interval.
// Only reads read-only field (cfg) — no lock needed.
func (m Model) loopScheduleLabelPoll(projectID, issueID string) tea.Cmd {
	interval := time.Duration(m.state.cfg.Worktree.PRPollSeconds) * time.Second
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return loopLabelPollTickMsg{ProjectID: projectID, IssueID: issueID}
	})
}

// loopScanOpenPRsCmd queries open PRs to detect issues waiting for merge.
// Only reads read-only fields (clients) — no lock needed.
func (m Model) loopScanOpenPRsCmd(projectID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		prs, err := client.ListOpenPRs(ctx, repo)
		if err != nil {
			return LoopOpenPRsMsg{ProjectID: projectID, Err: err}
		}

		// Fetch issue labels for each PR to determine correct resume state.
		issueLabels := make(map[string][]string)
		for _, pr := range prs {
			issueID := extractIssueID(pr.Branch)
			if issueID == "" {
				continue
			}
			issue, err := client.GetIssue(ctx, repo, issueID)
			if err != nil {
				continue // fallback: handler will use default WaitingPRMerge
			}
			issueLabels[issueID] = issue.Labels
		}

		return LoopOpenPRsMsg{ProjectID: projectID, PRs: prs, IssueLabels: issueLabels}
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

// loopWriteRevisionAgentCmd writes a revision coding agent prompt and returns LoopAgentWrittenMsg.
// Acquires RLock to read loops map, then releases before returning the Cmd closure.
func (m Model) loopWriteRevisionAgentCmd(projectID, issueID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	m.state.RLock()
	ls := m.state.loops[projectID]
	if ls == nil {
		m.state.RUnlock()
		return nil
	}
	slot := ls.Slots[loop.SlotKey(projectID, issueID)]
	if slot == nil {
		m.state.RUnlock()
		return nil
	}
	wtPath := slot.WorktreePath
	baseBranch := slot.BaseBranch
	reviewRound := slot.ReviewRound
	m.state.RUnlock()

	repo := project.Repo
	logPolicy := ""
	if p, ok := m.state.cfg.Profiles[project.Profile]; ok {
		logPolicy = p.LogPolicy
	}
	hookScripts := m.state.hookScripts
	hookMode := project.HookMode
	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD

	return func() tea.Msg {
		// Safety-net: ensure hooks + docs + gitignore exist
		worktree.EnsureGitignore(wtPath)
		_ = worktree.DeployHooksToWorktree(wtPath, hookMode, hookScripts)
		deployDocs(wtPath, "", agentGuidelines, codeConstructionPrinciples)

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
