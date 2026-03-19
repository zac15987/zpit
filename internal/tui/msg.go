package tui

import "github.com/zac15987/zpit/internal/terminal"

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
