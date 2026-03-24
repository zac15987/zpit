package locale

var en = map[Key]string{
	// Section titles
	KeyProjects:        "Projects",
	KeyHotkeys:         "Hotkeys",
	KeyActiveTerminals: "Active Terminals",
	KeyLoopStatus:      "Loop Status",
	KeyIssues:          "Issues",

	// Hotkey descriptions
	KeyLaunchClaude:   "Launch Claude Code",
	KeyClarifyReq:     "Clarify requirement",
	KeyLoopAutoImpl:   "Loop auto-implement",
	KeyReviewChanges:  "Review changes",
	KeyStatusOverview: "Status overview",
	KeyOpenFolder:     "Open project folder",
	KeyOpenTracker:    "Open Issue Tracker",
	KeyAddProject:     "Add project",
	KeyEditConfig:     "Edit config",
	KeyHelp:           "Help",
	KeyQuit:           "Quit",

	// Agent status text
	KeySessionEnded:    "Session ended",
	KeyWaitingForInput: "Waiting for input",
	KeyWorking:         "Working",
	KeyLaunched:        "Launched",

	// Loop status text
	KeyLoopRunning:   "running",
	KeyLoopStopping:  "stopping",
	KeyPollingIssues: "polling for issues...",

	// Footer
	KeyHelpFooter: "Press ? for help, q to quit",

	// Status view hotkey descriptions
	KeyConfirmPending: "Confirm (pending\u2192todo)",
	KeyOpenInBrowser:  "Open in browser",
	KeyBack:           "Back",

	// Status view loading/error/empty
	KeyLoadingIssues: "Loading issues...",
	KeyNoIssuesFound: "No issues found.",

	// Status messages
	KeyNoTrackerConfigured: "No tracker configured for this project",
	KeyTrackerTokenNotSet:  "Tracker token not set",
	KeyAddProjectStub:      "[a] Add Project \u2014 coming in M5",
	KeyEditConfigStub:      "[e] Edit Config \u2014 coming in M5",
	KeyHelpStub:            "[?] Help \u2014 coming soon",

	// Confirm dialogs
	KeyClarifierNotDeployed: "Clarifier agent not deployed to this project. Deploy?",
	KeyDeployAndLaunch:      "Deploy & Launch",
	KeyCancel:               "Cancel",
	KeyReviewerNotDeployed:  "Reviewer agent not deployed to this project. Deploy?",

	// Focus panel
	KeyFocusSlot:      "Switch to slots",
	KeyCannotLaunch:   "Cannot launch in this state",
	KeyNoWorktreePath: "Slot has no worktree",
	KeyLoopSlotHelp:   "Enter: open Claude  \u2191\u2193: navigate  Tab/Esc: back",
}
