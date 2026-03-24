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
	KeyReviewChanges:  "Review 變更",
	KeyStatusOverview: "狀態總覽",
	KeyOpenFolder:     "開啟專案資料夾",
	KeyOpenTracker:    "開啟 Issue Tracker",
	KeyAddProject:     "新增專案",
	KeyEditConfig:     "編輯設定",
	KeyHelp:           "說明",
	KeyQuit:           "離開",

	// Agent status text
	KeySessionEnded:    "Session 結束",
	KeyWaitingForInput: "等待輸入",
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
	KeyReviewerNotDeployed:  "Reviewer agent 未部署至此專案，是否部署？",

	// Label confirm dialogs
	KeyLabelsMissing:  "Zpit 及其 agent 依賴以下 label 追蹤 issue 狀態。\n%s 缺少以下 label：\n\n%s\n\n建立後才能繼續，是否建立？",
	KeyCreateLabels:   "建立 Label",
	KeyCheckingLabels: "檢查 label 中...",

	// Focus panel
	KeyFocusSlot:      "切換至 Slot",
	KeyCannotLaunch:   "此狀態無法啟動",
	KeyNoWorktreePath: "Slot 無 worktree 路徑",
	KeyLoopSlotHelp:   "Enter: 開啟 Claude  \u2191\u2193: 選擇  Tab/Esc: 返回",
}
