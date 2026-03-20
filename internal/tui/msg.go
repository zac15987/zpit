package tui

import (
	"time"

	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
)

// LaunchResultMsg is sent when a terminal launch completes.
type LaunchResultMsg struct {
	ProjectID string
	Result    *terminal.LaunchResult
	Err       error
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

// IssuesLoadedMsg carries the result of a TrackerBridge.ListIssues call.
type IssuesLoadedMsg struct {
	ProjectID string
	Issues    []tracker.Issue
	Err       error
}

// IssueConfirmedMsg carries the result of a ConfirmIssue call.
type IssueConfirmedMsg struct {
	ProjectID string
	IssueID   string
	Err       error
}

// MCPCheckResultMsg carries MCP availability warnings from startup check.
type MCPCheckResultMsg struct {
	Warnings []string
}
