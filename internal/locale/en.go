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
	KeyReviewChanges:   "Review changes",
	KeyEfficiencyAgent: "Efficiency agent",
	KeyStatusOverview: "Status overview",
	KeyOpenFolder:     "Open project folder",
	KeyOpenTracker:    "Open Issue Tracker",
	KeyOpenPR:         "Open PR",
	KeyLazygit:        "Open lazygit",
	KeyClaudeUpdate:   "Run claude update",
	KeyAddProject:     "Add project",
	KeyEditConfig:     "Edit config",
	KeyUndeploy:       "Undeploy agents",
	KeyRedeploy:       "Redeploy all agents",
	KeyChannelComm:    "Channel communication",
	KeyHelp:           "Help",
	KeyQuit:           "Quit",

	// Agent status text
	KeySessionEnded:    "Session ended",
	KeyWaitingForInput: "Waiting for input",
	KeyPermissionWait:  "Waiting for permission",
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
	KeyReviewerNotDeployed:   "Reviewer agent not deployed to this project. Deploy?",
	KeyEfficiencyNotDeployed: "Efficiency agent not deployed to this project. Deploy?",

	// Undeploy confirm dialogs
	KeyUndeployConfirm: "Remove all Zpit-deployed files from this project?\n\n.claude/agents/  .claude/docs/  .claude/hooks/\n.mcp.json  settings.json (hooks)  settings.local.json",
	KeyUndeployButton:  "Undeploy",
	KeyUndeployDone:    "Undeployed %d item(s) from %s",
	KeyUndeployNoop:    "No deployed files found (%s)",

	// Redeploy confirm dialog + deploy status tag
	KeyRedeployConfirm:     "Redeploy all Zpit files to this project?\n\nWill remove existing deploy and re-write:\n.claude/agents/ (4 agents)  .claude/docs/  .claude/hooks/",
	KeyRedeployButton:      "Redeploy",
	KeyRedeployDone:        "Redeployed to %s",
	KeyDeployStatusFull:    "deployed",
	KeyDeployStatusPartial: "partial",
	KeyDeployStatusNone:    "not deployed",

	// Issue confirm dialog
	KeyIssueConfirmTitle:  "Set #%s to todo?\n「%s」",
	KeyIssueConfirmButton: "Confirm",

	// Label confirm dialogs
	KeyLabelsMissing:  "Zpit and its agents depend on the following labels to track issue status.\nThese labels are missing in %s:\n\n%s\n\nCreate them to continue?",
	KeyCreateLabels:   "Create Labels",
	KeyCheckingLabels:          "Checking labels...",
	KeyTrackerLabelNotSupported: "Tracker does not support label management",

	// Focus panel
	KeyFocusSlot:      "Switch to slots",
	KeyCannotLaunch:   "Cannot launch in this state",
	KeyNoWorktreePath: "Slot has no worktree",
	KeyLoopSlotHelp:   "Enter: open Claude  o: folder  i: issue  p: PR  G: lazygit  \u2191\u2193: navigate  Tab/Esc: back",

	// Terminal focus panel
	KeyKillTerminal:         "Terminal killed: %s (PID %d)",
	KeyKillTerminalConfirm:  "Kill terminal %s (PID %d)?\nThis will force-terminate the Claude Code process.",
	KeyKillFailed:           "Kill failed: %s",
	KeyKillButton:           "Kill",
	KeyTerminalNoPID:        "Terminal has no active process",
	KeyTerminalAlreadyEnded: "Terminal already ended",
	KeySwitchPanel:          "Switch Panel",
	KeyTerminalHelp:         "\u2191\u2193 select \u2502 x close \u2502 Tab switch \u2502 Esc back",
	KeyCloseTerminal:        "Close Terminal",

	// Sound file warning
	KeySoundFileNotFound: "Sound file not found: %s",

	// Config validation errors
	// Agent init messages
	KeyInitCoding:         "Start implementation",
	KeyInitRevisionCoding: "Read PR review comments and fix the issues",
	KeyInitReview:         "Start review",
	KeyInitRevisionReview: "Start revision review, focus on previous MUST FIX items",

	// Channel view
	KeyChannelTitle:          "Channel — %s",
	KeyChannelNoActivity:     "No channel activity yet",
	KeyChannelDisabled:       "Channel is not enabled for this project. Set channel_enabled = true in config.",
	KeyChannelScroll:         "Scroll",
	KeyChannelBack:           "Back",
	KeyChannelGlobalTitle:    "Global Channel",
	KeyChannelGlobalNoEvents: "No global channel activity",

	KeyErrConfigTitle:       "Configuration Error",
	KeyErrDismissHint:       "Press Esc or Enter to dismiss",
	KeyErrPathEmpty:         "Project path is not configured",
	KeyErrRepoEmpty:         "Project repo is not configured",
	KeyErrTrackerURLEmpty:   "Tracker URL is not configured",
	KeyErrWorktreeBaseEmpty: "Worktree base directory is not configured",
	KeyErrWorktreeMissing:   "Worktree directory no longer exists",

	// Edit config sub-menu
	KeyEditConfigTitle:        "Edit Config — %s",
	KeyEditConfigOption1:      "[1] Toggle channel",
	KeyEditConfigOption2:      "[2] Edit channel_listen",
	KeyEditConfigOption3:      "[3] Open config in editor",
	KeyEditConfigFooter:       "[1/2/3] Select  [r] Reload  [Esc] Back",

	// Channel toggle
	KeyChannelToggleOn:        "channel: ON (%s)",
	KeyChannelToggleOff:       "channel: OFF (%s)",
	KeyChannelBrokerStartFail: "Failed to start broker: %s",

	// Channel listen multi-select
	KeyChannelListenTitle:     "Edit channel_listen — %s",
	KeyChannelListenFooter:    "↑↓ Navigate  Space Toggle  Enter Confirm  Esc Cancel",
	KeyChannelListenUpdated:   "channel_listen updated: %s",
	KeyChannelListenNoChange:  "channel_listen: no changes",

	// Editor launch
	KeyEditorLaunching:        "Opening %s...",
	KeyEditorFallbackVim:      "No $EDITOR set, using vim",

	// Config reload
	KeyConfigReloaded:         "Config reloaded",
	KeyConfigReloadError:      "Config reload error: %s",
	KeyConfigRestartRequired:  "Restart required for: %s",
	KeyConfigNoChanges:        "Config: no changes detected",

	// SSH remote mode
	KeyConfigPathHint:         "Config path: %s — edit externally, press [r] to reload",
	KeyConfigReloadManual:     "Press [r] to reload config",

	// Additional edit config strings
	KeyConfigPathNotFound:     "Config path not found",
	KeyEditorError:            "Editor error: %s",
	KeyGlobal:                 "Global",

	// Git status page
	KeyGitStatus:              "Git Status",
	KeyGitStatusTitle:         "Git Status — %s",
	KeyGitStatusLocalBranches: "Local Branches",
	KeyGitStatusRemoteOnly:    "Remote-only Branches",
	KeyGitStatusGraph:         "Commit Graph",
	KeyGitStatusNone:          "(none)",
	KeyGitStatusNoUpstream:    "(no upstream)",
	KeyGitStatusRemoteOnlyTag: "(remote only)",
	KeyGitStatusNoCommits:     "(no commits yet)",
	KeyGitStatusDetached:      "* (detached at %s)",
	KeyGitStatusFetching:      "fetching (origin)...",
	KeyGitStatusPulling:       "pulling (--ff-only)...",
	KeyGitStatusRefreshing:    "refreshing...",
	KeyGitStatusFetchOK:       "fetch OK",
	KeyGitStatusPullOK:        "pull OK",
	KeyGitStatusFetchFailed:   "fetch failed: %s",
	KeyGitStatusPullFailed:    "pull failed: %s",
	KeyGitStatusOpInProgress:  "%s already in progress",
	KeyGitStatusNotGitRepo:    "not a git repository: %s",
	KeyGitStatusPathNotConfigured: "path not configured",
	KeyGitStatusHotkeys:       "[f] Fetch  [p] Pull  [r] Refresh  [Esc] Back",
	KeyGitStatusHotkeyLabel:   "View git status",
}
