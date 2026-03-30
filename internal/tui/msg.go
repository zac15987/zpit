package tui

import (
	"time"

	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
)

// LaunchResultMsg is sent when a terminal launch completes.
type LaunchResultMsg struct {
	ProjectID   string
	TrackingKey string // if set, use as activeTerminals key instead of ProjectID
	WorkDir     string // if set, use for session discovery instead of project path
	Result      *terminal.LaunchResult
	Err         error
}

// StatusMsg is a transient message displayed in the status bar.
type StatusMsg struct {
	Text string
}

// AgentEventMsg carries parsed agent state changes from the watcher.
type AgentEventMsg struct {
	ProjectID string
	Events    []watcher.SessionEvent
}

// TickMsg is sent every second for elapsed time display.
type TickMsg time.Time

// WatcherErrorMsg indicates the watcher encountered an error.
type WatcherErrorMsg struct {
	ProjectID string
	Err       error
}

// sessionLostMsg indicates session discovery or log wait failed; the entry should be cleaned up.
type sessionLostMsg struct {
	ProjectID string
	Text      string
}

// IssuesLoadedMsg carries the result of a TrackerClient.ListIssues call.
type IssuesLoadedMsg struct {
	ProjectID string
	Issues    []tracker.Issue
	Err       error
}

// IssueConfirmedMsg carries the result of an UpdateLabels call.
type IssueConfirmedMsg struct {
	ProjectID string
	IssueID   string
	Err       error
}

// LabelCheckResultMsg carries the result of checking whether required labels exist (read-only).
type LabelCheckResultMsg struct {
	ProjectID string
	Missing   []tracker.LabelDef
	Err       error
}

// LabelsEnsuredMsg carries the result of ensuring required labels exist in a project's tracker.
type LabelsEnsuredMsg struct {
	ProjectID string
	Created   []string
	Err       error
}

// --- Loop engine messages ---

// LoopPollMsg carries results of polling tracker for todo issues.
type LoopPollMsg struct {
	ProjectID string
	Issues    []tracker.Issue
	Err       error
}

// LoopWorktreeCreatedMsg indicates a worktree was created for an issue.
type LoopWorktreeCreatedMsg struct {
	ProjectID    string
	IssueID      string
	WorktreePath string
	BranchName   string
	Err          error
}

// LoopAgentWrittenMsg indicates the temp agent file was written.
type LoopAgentWrittenMsg struct {
	ProjectID string
	IssueID   string
	Err       error
}

// LoopAgentLaunchedMsg indicates a coding/reviewer agent was launched.
type LoopAgentLaunchedMsg struct {
	ProjectID  string
	IssueID    string
	Role       string // "coder" or "reviewer"
	LaunchedAt int64  // unix timestamp captured just before terminal launch
	Result     *terminal.LaunchResult
	Err        error
}

// LoopPRStatusMsg carries the result of polling PR status for merge detection.
type LoopPRStatusMsg struct {
	ProjectID string
	IssueID   string
	PR        *tracker.PRStatus
	Err       error
}

// LoopCleanupMsg indicates worktree cleanup completed.
type LoopCleanupMsg struct {
	ProjectID string
	IssueID   string
	Err       error
}

// LoopOpenPRsMsg carries results of scanning open PRs at loop startup.
type LoopOpenPRsMsg struct {
	ProjectID   string
	PRs         []tracker.PRInfo
	IssueLabels map[string][]string // issueID → labels (for state recovery)
	Err         error
}

// loopPollTickMsg triggers the next poll cycle (unexported).
type loopPollTickMsg struct{ ProjectID string }

// loopPRPollTickMsg triggers the next PR status poll (unexported).
type loopPRPollTickMsg struct {
	ProjectID string
	IssueID   string
}

// LoopLabelPollMsg carries results of polling issue labels for state transitions.
type LoopLabelPollMsg struct {
	ProjectID string
	IssueID   string
	Labels    []string
	Err       error
}

// loopLabelPollTickMsg triggers the next label poll (unexported).
type loopLabelPollTickMsg struct {
	ProjectID string
	IssueID   string
}

// StateRefreshMsg is sent when shared state changes and the UI needs to re-render.
// Triggered by the broadcast mechanism when another client mutates shared state.
type StateRefreshMsg struct{}
