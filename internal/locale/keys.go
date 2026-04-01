package locale

// Key constants for all TUI display strings.
const (
	// Section titles
	KeyProjects        Key = "projects"
	KeyHotkeys         Key = "hotkeys"
	KeyActiveTerminals Key = "active_terminals"
	KeyLoopStatus      Key = "loop_status"
	KeyIssues          Key = "issues"

	// Hotkey descriptions (view_projects)
	KeyLaunchClaude    Key = "launch_claude"
	KeyClarifyReq      Key = "clarify_requirement"
	KeyLoopAutoImpl    Key = "loop_auto_implement"
	KeyReviewChanges   Key = "review_changes"
	KeyStatusOverview  Key = "status_overview"
	KeyOpenFolder      Key = "open_project_folder"
	KeyOpenTracker     Key = "open_issue_tracker"
	KeyAddProject      Key = "add_project"
	KeyEditConfig      Key = "edit_config"
	KeyUndeploy        Key = "undeploy"
	KeyHelp            Key = "help"
	KeyQuit            Key = "quit"

	// Agent status text (view_projects)
	KeySessionEnded    Key = "session_ended"
	KeyWaitingForInput Key = "waiting_for_input"
	KeyPermissionWait  Key = "permission_wait"
	KeyWorking         Key = "working"
	KeyLaunched        Key = "launched"

	// Loop status text (view_projects)
	KeyLoopRunning     Key = "loop_running"
	KeyLoopStopping    Key = "loop_stopping"
	KeyPollingIssues   Key = "polling_issues"

	// Footer (view_projects)
	KeyHelpFooter Key = "help_footer"

	// Status view hotkey descriptions (view_status)
	KeyConfirmPending Key = "confirm_pending"
	KeyOpenInBrowser  Key = "open_in_browser"
	KeyBack           Key = "back"

	// Status view loading/error/empty (view_status)
	KeyLoadingIssues Key = "loading_issues"
	KeyNoIssuesFound Key = "no_issues_found"

	// Status messages (model.go)
	KeyNoTrackerConfigured Key = "no_tracker_configured"
	KeyTrackerTokenNotSet  Key = "tracker_token_not_set"
	KeyAddProjectStub      Key = "add_project_stub"
	KeyEditConfigStub      Key = "edit_config_stub"
	KeyHelpStub            Key = "help_stub"

	// Confirm dialogs (model.go)
	KeyClarifierNotDeployed Key = "clarifier_not_deployed"
	KeyDeployAndLaunch      Key = "deploy_and_launch"
	KeyCancel               Key = "cancel"
	KeyReviewerNotDeployed  Key = "reviewer_not_deployed"

	// Undeploy confirm dialogs (model.go)
	KeyUndeployConfirm Key = "undeploy_confirm"
	KeyUndeployButton  Key = "undeploy_button"
	KeyUndeployDone    Key = "undeploy_done"
	KeyUndeployNoop    Key = "undeploy_noop"

	// Issue confirm dialog (model.go)
	KeyIssueConfirmTitle  Key = "issue_confirm_title"
	KeyIssueConfirmButton Key = "issue_confirm_button"

	// Label confirm dialogs (model.go)
	KeyLabelsMissing  Key = "labels_missing"
	KeyCreateLabels   Key = "create_labels"
	KeyCheckingLabels          Key = "checking_labels"
	KeyTrackerLabelNotSupported Key = "tracker_label_not_supported"

	// Focus panel (loop slot selection)
	KeyFocusSlot      Key = "focus_slot"
	KeyCannotLaunch   Key = "cannot_launch"
	KeyNoWorktreePath Key = "no_worktree_path"
	KeyLoopSlotHelp   Key = "loop_slot_help"

	// Agent init messages (loop_cmds)
	KeyInitCoding         Key = "init_coding"
	KeyInitRevisionCoding Key = "init_revision_coding"
	KeyInitReview         Key = "init_review"
	KeyInitRevisionReview Key = "init_revision_review"

	// Config validation errors (error overlay)
	KeyErrConfigTitle       Key = "err_config_title"
	KeyErrDismissHint       Key = "err_dismiss_hint"
	KeyErrPathEmpty         Key = "err_path_empty"
	KeyErrRepoEmpty         Key = "err_repo_empty"
	KeyErrTrackerURLEmpty   Key = "err_tracker_url_empty"
	KeyErrWorktreeBaseEmpty Key = "err_worktree_base_empty"
	KeyErrWorktreeMissing   Key = "err_worktree_missing"
)
