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
	KeyUndeploy:       "Undeploy agents",
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

	// Undeploy confirm dialogs
	KeyUndeployConfirm: "Remove all Zpit-deployed files from this project?\n\n.claude/agents/  .claude/docs/  .claude/hooks/",
	KeyUndeployButton:  "Undeploy",
	KeyUndeployDone:    "Undeployed %d item(s) from %s",
	KeyUndeployNoop:    "No deployed files found (%s)",

	// Label confirm dialogs
	KeyLabelsMissing:  "Zpit and its agents depend on the following labels to track issue status.\nThese labels are missing in %s:\n\n%s\n\nCreate them to continue?",
	KeyCreateLabels:   "Create Labels",
	KeyCheckingLabels:          "Checking labels...",
	KeyTrackerLabelNotSupported: "Tracker does not support label management",

	// Focus panel
	KeyFocusSlot:      "Switch to slots",
	KeyCannotLaunch:   "Cannot launch in this state",
	KeyNoWorktreePath: "Slot has no worktree",
	KeyLoopSlotHelp:   "Enter: open Claude  \u2191\u2193: navigate  Tab/Esc: back",

	// Config validation errors
	// Agent init messages
	KeyInitCoding:         "Start implementation",
	KeyInitRevisionCoding: "Read PR review comments and fix the issues",
	KeyInitReview:         "Start review",
	KeyInitRevisionReview: "Start revision review, focus on previous MUST FIX items",

	KeyErrConfigTitle:       "Configuration Error",
	KeyErrDismissHint:       "Press Esc or Enter to dismiss",
	KeyErrPathEmpty:         "Project path is not configured",
	KeyErrRepoEmpty:         "Project repo is not configured",
	KeyErrTrackerURLEmpty:   "Tracker URL is not configured",
	KeyErrWorktreeBaseEmpty: "Worktree base directory is not configured",
	KeyErrWorktreeMissing:   "Worktree directory no longer exists",
}
