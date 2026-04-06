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
	KeyChannelComm     Key = "channel_comm"
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

	// Channel view
	KeyChannelTitle          Key = "channel_title"
	KeyChannelNoActivity     Key = "channel_no_activity"
	KeyChannelDisabled       Key = "channel_disabled"
	KeyChannelScroll         Key = "channel_scroll"
	KeyChannelBack           Key = "channel_back"
	KeyChannelGlobalTitle    Key = "channel_global_title"
	KeyChannelGlobalNoEvents Key = "channel_global_no_events"

	// Agent init messages (loop_cmds)
	KeyInitCoding         Key = "init_coding"
	KeyInitRevisionCoding Key = "init_revision_coding"
	KeyInitReview         Key = "init_review"
	KeyInitRevisionReview Key = "init_revision_review"

	// Sound file warning
	KeySoundFileNotFound Key = "sound_file_not_found"

	// Config validation errors (error overlay)
	KeyErrConfigTitle       Key = "err_config_title"
	KeyErrDismissHint       Key = "err_dismiss_hint"
	KeyErrPathEmpty         Key = "err_path_empty"
	KeyErrRepoEmpty         Key = "err_repo_empty"
	KeyErrTrackerURLEmpty   Key = "err_tracker_url_empty"
	KeyErrWorktreeBaseEmpty Key = "err_worktree_base_empty"
	KeyErrWorktreeMissing   Key = "err_worktree_missing"

	// Edit config sub-menu
	KeyEditConfigTitle        Key = "edit_config_title"
	KeyEditConfigOption1      Key = "edit_config_option1"
	KeyEditConfigOption2      Key = "edit_config_option2"
	KeyEditConfigOption3      Key = "edit_config_option3"
	KeyEditConfigFooter       Key = "edit_config_footer"

	// Channel toggle
	KeyChannelToggleOn        Key = "channel_toggle_on"
	KeyChannelToggleOff       Key = "channel_toggle_off"
	KeyChannelBrokerStartFail Key = "channel_broker_start_fail"

	// Channel listen multi-select
	KeyChannelListenTitle     Key = "channel_listen_title"
	KeyChannelListenFooter    Key = "channel_listen_footer"
	KeyChannelListenUpdated   Key = "channel_listen_updated"
	KeyChannelListenNoChange  Key = "channel_listen_no_change"

	// Editor launch
	KeyEditorLaunching        Key = "editor_launching"
	KeyEditorFallbackVim      Key = "editor_fallback_vim"

	// Config reload
	KeyConfigReloaded         Key = "config_reloaded"
	KeyConfigReloadError      Key = "config_reload_error"
	KeyConfigRestartRequired  Key = "config_restart_required"
	KeyConfigNoChanges        Key = "config_no_changes"

	// SSH remote mode
	KeyConfigPathHint         Key = "config_path_hint"
	KeyConfigReloadManual     Key = "config_reload_manual"
)
