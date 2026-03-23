package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/tracker"
)

// RevisionParams holds all data needed to assemble a revision coding prompt.
type RevisionParams struct {
	IssueID     string
	IssueTitle  string
	Spec        *tracker.IssueSpec
	LogPolicy   string // "strict" | "standard" | "minimal"
	BaseBranch  string // e.g. "dev"
	ReviewRound int    // 1-based round number
}

// BuildRevisionPrompt assembles the coding agent prompt for fixing review feedback.
func BuildRevisionPrompt(p RevisionParams) string {
	var b strings.Builder

	fmt.Fprintf(&b, "你正在修正 issue %s: %s（第 %d 輪修正）\n", p.IssueID, p.IssueTitle, p.ReviewRound)

	b.WriteString("\n## 原始需求\n\n")
	b.WriteString(p.Spec.Context)

	b.WriteString("\n\n## 預期方案\n\n")
	b.WriteString(p.Spec.Approach)

	b.WriteString("\n\n## 完成標準\n\n")
	b.WriteString(strings.Join(p.Spec.AcceptanceCriteria, "\n"))

	b.WriteString("\n\n## 你可以修改的檔案範圍\n\n")
	b.WriteString(formatScope(p.Spec.Scope))
	b.WriteString("\n超出此範圍的檔案不要碰。如果你發現必須修改範圍外的檔案才能完成任務，\n")
	b.WriteString("立即停下來說明原因，等待使用者決定。")

	b.WriteString("\n\n## 不可違反的限制\n\n")
	b.WriteString(p.Spec.Constraints)

	fmt.Fprintf(&b, "\n\n## Logging 規範\n\n%s", logPolicyText(p.LogPolicy))

	fmt.Fprintf(&b, `

## 你的工作流程

1. 先讀取 CLAUDE.md 了解此專案的架構原則和 logging 規範
   讀取 .claude/docs/tracker.md 了解如何操作 tracker
2. 讀取 PR 上最新的 Review Report comment（找 NEEDS CHANGES 的那則）
3. 逐點列出 reviewer 指出的問題（🔴 MUST FIX 優先）
4. 針對每個問題修正 code
5. 修正過程中，確保所有改動符合 CLAUDE.md 規範
6. 完成後，逐條對照 ACCEPTANCE_CRITERIA 自我檢查
7. 用 git add + git commit 提交改動
8. Commit message 格式: [%s] fix: {修正內容簡述}
9. 開始修正前，更新 issue label: 移除 "needs-changes"，加入 "wip"
10. 修正完成後，更新 issue label: 移除 "wip"，加入 "review"

注意：此 PR 的 target branch 是 `+"`%s`"+`。如果發現 PR target 了錯誤的分支，
立即停下來通知使用者，不要繼續作業。

## 停下來問使用者的時機

- reviewer 指出的問題你不確定如何修正
- 修正需要改動 SCOPE 範圍外的檔案
- reviewer 的意見跟 CONSTRAINTS 衝突

## Tracker 操作注意

更新 label、讀取 comment 時，依 .claude/docs/tracker.md 指示。
不論使用 MCP 或 REST API，長文字一律先用 Write tool 寫到暫存檔，
再用 Read tool 讀取內容傳入 API。絕對不要在 bash 命令或 MCP 參數裡直接內嵌長文字。
`, p.IssueID, p.BaseBranch)

	return b.String()
}
