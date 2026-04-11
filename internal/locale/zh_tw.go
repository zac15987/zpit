package locale

var zhTW = map[Key]string{
	// Section titles
	KeyProjects:        "專案",
	KeyHotkeys:         "快捷鍵",
	KeyActiveTerminals: "執行中終端",
	KeyLoopStatus:      "Loop 狀態",
	KeyIssues:          "Issues",

	// Hotkey descriptions
	KeyLaunchClaude:   "啟動 Claude Code",
	KeyClarifyReq:     "釐清需求",
	KeyLoopAutoImpl:   "Loop 自動實作",
	KeyReviewChanges:   "Review 變更",
	KeyEfficiencyAgent: "效率 Agent",
	KeyStatusOverview: "狀態總覽",
	KeyOpenFolder:     "開啟專案資料夾",
	KeyOpenTracker:    "開啟 Issue Tracker",
	KeyAddProject:     "新增專案",
	KeyEditConfig:     "編輯設定",
	KeyUndeploy:       "清除部署檔案",
	KeyChannelComm:    "Channel 通訊",
	KeyHelp:           "說明",
	KeyQuit:           "離開",

	// Agent status text
	KeySessionEnded:    "Session 結束",
	KeyWaitingForInput: "等待輸入",
	KeyPermissionWait:  "等待授權",
	KeyWorking:         "執行中",
	KeyLaunched:        "已啟動",

	// Loop status text
	KeyLoopRunning:   "執行中",
	KeyLoopStopping:  "停止中",
	KeyPollingIssues: "輪詢 issue 中...",

	// Footer
	KeyHelpFooter: "按 ? 查看說明，q 離開",

	// Status view hotkey descriptions
	KeyConfirmPending: "確認 (pending→todo)",
	KeyOpenInBrowser:  "在瀏覽器中開啟",
	KeyBack:           "返回",

	// Status view loading/error/empty
	KeyLoadingIssues: "載入 issue 中...",
	KeyNoIssuesFound: "找不到任何 issue。",

	// Status messages
	KeyNoTrackerConfigured: "此專案未設定 tracker",
	KeyTrackerTokenNotSet:  "Tracker token 未設定",
	KeyAddProjectStub:      "[a] 新增專案 — M5 上線",
	KeyEditConfigStub:      "[e] 編輯設定 — M5 上線",
	KeyHelpStub:            "[?] 說明 — 即將推出",

	// Confirm dialogs
	KeyClarifierNotDeployed: "Clarifier agent 未部署至此專案，是否部署？",
	KeyDeployAndLaunch:      "部署並啟動",
	KeyCancel:               "取消",
	KeyReviewerNotDeployed:   "Reviewer agent 未部署至此專案，是否部署？",
	KeyEfficiencyNotDeployed: "效率 Agent 未部署至此專案，是否部署？",

	// Undeploy confirm dialogs
	KeyUndeployConfirm: "移除此專案所有 Zpit 部署檔案？\n\n.claude/agents/  .claude/docs/  .claude/hooks/\n.mcp.json  settings.json (hooks)  settings.local.json",
	KeyUndeployButton:  "清除",
	KeyUndeployDone:    "已清除 %d 個項目（%s）",
	KeyUndeployNoop:    "無已部署檔案（%s）",

	// Issue confirm dialog
	KeyIssueConfirmTitle:  "確認將 #%s 設為 todo？\n「%s」",
	KeyIssueConfirmButton: "確認",

	// Label confirm dialogs
	KeyLabelsMissing:  "Zpit 及其 agent 依賴以下 label 追蹤 issue 狀態。\n%s 缺少以下 label：\n\n%s\n\n建立後才能繼續，是否建立？",
	KeyCreateLabels:   "建立 Label",
	KeyCheckingLabels:          "檢查 label 中...",
	KeyTrackerLabelNotSupported: "Tracker 不支援 label 管理",

	// Focus panel
	KeyFocusSlot:      "切換至 Slot",
	KeyCannotLaunch:   "此狀態無法啟動",
	KeyNoWorktreePath: "Slot 無 worktree 路徑",
	KeyLoopSlotHelp:   "Enter: 開啟 Claude  o: 開啟資料夾  p: 開啟 issue  \u2191\u2193: 選擇  Tab/Esc: 返回",

	// Sound file warning
	KeySoundFileNotFound: "找不到音效檔案：%s",

	// Config validation errors
	// Agent init messages
	KeyInitCoding:         "開始實作",
	KeyInitRevisionCoding: "讀取 PR review comment，修正問題",
	KeyInitReview:         "開始 review",
	KeyInitRevisionReview: "開始 revision review，專注檢查上次 MUST FIX 項目",

	// Channel view
	KeyChannelTitle:          "Channel — %s",
	KeyChannelNoActivity:     "尚無 Channel 活動",
	KeyChannelDisabled:       "此專案未啟用 Channel，請在設定中設定 channel_enabled = true。",
	KeyChannelScroll:         "捲動",
	KeyChannelBack:           "返回",
	KeyChannelGlobalTitle:    "全域 Channel",
	KeyChannelGlobalNoEvents: "尚無全域 Channel 活動",

	KeyErrConfigTitle:       "設定錯誤",
	KeyErrDismissHint:       "按 Esc 或 Enter 關閉",
	KeyErrPathEmpty:         "專案路徑未設定",
	KeyErrRepoEmpty:         "專案 repo 未設定",
	KeyErrTrackerURLEmpty:   "Tracker URL 未設定",
	KeyErrWorktreeBaseEmpty: "Worktree base directory 未設定",
	KeyErrWorktreeMissing:   "Worktree 目錄已不存在",

	// Edit config sub-menu
	KeyEditConfigTitle:        "編輯設定 — %s",
	KeyEditConfigOption1:      "[1] 切換 Channel",
	KeyEditConfigOption2:      "[2] 編輯 channel_listen",
	KeyEditConfigOption3:      "[3] 用編輯器開啟設定檔",
	KeyEditConfigFooter:       "[1/2/3] 選擇  [r] 重載  [Esc] 返回",

	// Channel toggle
	KeyChannelToggleOn:        "channel: 開啟 (%s)",
	KeyChannelToggleOff:       "channel: 關閉 (%s)",
	KeyChannelBrokerStartFail: "Broker 啟動失敗：%s",

	// Channel listen multi-select
	KeyChannelListenTitle:     "編輯 channel_listen — %s",
	KeyChannelListenFooter:    "↑↓ 瀏覽  空白鍵 切換  Enter 確認  Esc 取消",
	KeyChannelListenUpdated:   "channel_listen 已更新：%s",
	KeyChannelListenNoChange:  "channel_listen：無變更",

	// Editor launch
	KeyEditorLaunching:        "開啟 %s...",
	KeyEditorFallbackVim:      "未設定 $EDITOR，使用 vim",

	// Config reload
	KeyConfigReloaded:         "設定已重新載入",
	KeyConfigReloadError:      "設定重載錯誤：%s",
	KeyConfigRestartRequired:  "以下設定需重啟才能生效：%s",
	KeyConfigNoChanges:        "設定：未偵測到變更",

	// SSH remote mode
	KeyConfigPathHint:         "設定檔路徑：%s — 請在外部編輯後按 [r] 重載",
	KeyConfigReloadManual:     "按 [r] 重新載入設定",

	// Additional edit config strings
	KeyConfigPathNotFound:     "找不到設定檔路徑",
	KeyEditorError:            "編輯器錯誤：%s",
	KeyGlobal:                 "全域",
}
