package tui

import (
	"time"

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/git"
	"github.com/zac15987/zpit/internal/sessionsync"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
)

// LaunchResultMsg is sent when a terminal launch completes.
type LaunchResultMsg struct {
	ProjectID      string
	TrackingKey    string // if set, use as activeTerminals key instead of ProjectID
	WorkDir        string // if set, use for session discovery instead of project path
	WorktreeBranch string // non-empty when launched in a git worktree (e.g. "feat/19-slug")
	Result         *terminal.LaunchResult
	Err            error
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
	ProjectID     string
	Issues        []tracker.Issue
	CycleIssueIDs []string // sorted issue IDs involved in circular dependencies (without # prefix)
	Err           error
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

// LoopAutoMergeMsg carries the result of the auto-merge attempt (tracker MergePR API).
// FailureKind classifies the outcome: "" (success), "transient_exhausted", "permanent", "auth".
type LoopAutoMergeMsg struct {
	ProjectID   string
	IssueID     string
	PR          *tracker.PRStatus
	Err         error
	FailureKind string
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

// --- Channel event messages ---

// ChannelEventMsg carries a single broker event received from the EventBus subscription.
type ChannelEventMsg struct {
	ProjectID string
	Event     broker.Event
}

// ChannelSubscribedMsg indicates the result of subscribing to the broker's EventBus.
type ChannelSubscribedMsg struct {
	ProjectID string
	Err       error
}

// StateRefreshMsg is sent when shared state changes and the UI needs to re-render.
// Triggered by the broadcast mechanism when another client mutates shared state.
type StateRefreshMsg struct{}

// --- Edit config messages ---

// EditorFinishedMsg is sent when the external editor process exits.
type EditorFinishedMsg struct {
	Err error
}

// ConfigReloadedMsg carries the result of reloading config.toml after editor exit.
type ConfigReloadedMsg struct {
	NewCfg *config.Config
	Diff   config.ConfigDiff
	Err    error
}

// ChannelToggledMsg carries the result of toggling channel_enabled for a project.
type ChannelToggledMsg struct {
	ProjectID string
	Enabled   bool
	Err       error
}

// ChannelListenUpdatedMsg carries the result of updating channel_listen for a project.
type ChannelListenUpdatedMsg struct {
	ProjectID string
	NewListen []string
	Err       error
}

// --- Terminal kill messages ---

// KillTerminalMsg carries the result of killing a terminal process.
type KillTerminalMsg struct {
	TrackingKey string
	Err         error
}

// --- Git status page ---

// GitDataLoadedMsg carries the result of loading branches + graph for a project.
type GitDataLoadedMsg struct {
	ProjectID string
	Branches  git.BranchInfo
	Graph     string // raw ANSI-colored log output, may be empty for no-commits repo
	Err       error
}

// GitFetchResultMsg carries the result of `git fetch --all --prune`.
type GitFetchResultMsg struct {
	ProjectID string
	Stdout    string
	Stderr    string
	Err       error
}

// GitPullResultMsg carries the result of `git pull --ff-only`.
type GitPullResultMsg struct {
	ProjectID string
	Stdout    string
	Stderr    string
	Err       error
}

// --- History (Session Browser) messages ---

// HistoryFoldersScannedMsg carries the result of scanning ~/.claude/projects/.
// Folders holds one FolderInfo per encoded project directory found on disk.
// ActivePIDsByFolder maps each encoded folder name to true when at least one
// session in that folder has a currently alive Claude Code PID, allowing the
// folder list to render the 🟢 active marker without drilling in.
// Err is non-nil when the scan itself failed (e.g. directory unreadable).
type HistoryFoldersScannedMsg struct {
	Folders            []sessionsync.FolderInfo
	ActivePIDsByFolder map[string]bool
	Err                error
}

// HistorySessionsScannedMsg carries the result of scanning sessions inside one
// encoded folder. ActiveSessionIDs lists session IDs whose PID is currently alive
// (sourced from watcher.FindActiveSessions).
type HistorySessionsScannedMsg struct {
	// FolderName is the encoded project directory name (e.g. "-home-user-myproject").
	FolderName string
	// Sessions holds the metadata for each session file found in the folder.
	Sessions []sessionsync.SessionInfo
	// ActiveSessionIDs is the subset of session IDs whose backing process is alive.
	ActiveSessionIDs []string
	// Err is non-nil when the scan failed.
	Err error
}

// ExportStartedMsg signals that a zip export has begun. The TUI uses this to
// show an in-progress indicator before ExportCompletedMsg arrives.
type ExportStartedMsg struct {
	// OutputPath is the destination .zip file path.
	OutputPath string
	// Sessions lists the session IDs included in this export.
	Sessions []string
}

// ExportCompletedMsg carries the result of a completed zip export.
// Err is non-nil when the export failed or was only partially written.
type ExportCompletedMsg struct {
	// OutputPath is the destination .zip file path.
	OutputPath string
	// Sessions lists the session IDs that were included in the export attempt.
	Sessions []string
	// BytesWritten is the total number of bytes written to the zip archive.
	BytesWritten int64
	// Err is non-nil when the export failed.
	Err error
}

// ImportStartedMsg signals that an import operation has begun. The TUI uses
// this to show an in-progress indicator before ImportCompletedMsg arrives.
type ImportStartedMsg struct {
	// BundlePath is the path to the source .zip bundle being imported.
	BundlePath string
	// DestEncoded is the encoded project directory name that will receive the sessions.
	DestEncoded string
}

// ImportProgressMsg carries one written/skipped/cancelled-step status update from
// an import operation. Progress messages let the TUI re-render incrementally
// without waiting for the entire import to finish.
type ImportProgressMsg struct {
	// SessionID identifies which session this progress update relates to.
	SessionID string
	// Outcome is one of "written", "skipped", or "cancelled".
	Outcome string
}

// ImportCompletedMsg carries the final tally for an import operation.
// Err is non-nil when the operation failed before or during processing.
type ImportCompletedMsg struct {
	// DestEncoded is the encoded project directory name that received the sessions.
	DestEncoded string
	// Written is the count of sessions successfully written.
	Written int
	// Skipped is the count of sessions that were skipped (e.g. already present).
	Skipped int
	// Cancelled is the count of sessions aborted at user request.
	Cancelled int
	// MemoryStatus reports what happened to the memory/ directory:
	// "written", "skipped", or "not-included".
	MemoryStatus string
	// Err is non-nil when the import failed.
	Err error
}

// CollisionPromptMsg signals that the import detected a collision and is awaiting
// user choice. The TUI handler shows a 3-button modal (Overwrite / Skip / Cancel All).
// When IsMemory is true the collision is on the memory/ directory rather than a session.
type CollisionPromptMsg struct {
	// SessionID identifies the colliding session, or is empty when IsMemory is true.
	SessionID string
	// IsMemory is true when the collision is on memory/ instead of a session file.
	IsMemory bool
}
