package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/tracker"
)

// CodingParams holds all data needed to assemble a coding agent prompt.
type CodingParams struct {
	IssueID    string
	IssueTitle string
	Spec       *tracker.IssueSpec
	LogPolicy  string // "strict" | "standard" | "minimal"
	BaseBranch string // e.g. "dev"
}

// BuildCodingPrompt assembles the full coding agent prompt from Issue Spec data.
func BuildCodingPrompt(p CodingParams) string {
	var b strings.Builder

	fmt.Fprintf(&b, "你正在處理 issue %s: %s\n", p.IssueID, p.IssueTitle)

	b.WriteString("\n## 你要解決的問題\n\n")
	b.WriteString(p.Spec.Context)

	b.WriteString("\n\n## 你要採用的實作方案\n\n")
	b.WriteString(p.Spec.Approach)

	b.WriteString("\n\n## 完成標準（每一條都必須達成，完成後逐條自我檢查）\n\n")
	b.WriteString(strings.Join(p.Spec.AcceptanceCriteria, "\n"))

	b.WriteString("\n\n## 你可以修改的檔案範圍\n\n")
	b.WriteString(formatScope(p.Spec.Scope))
	b.WriteString("\n超出此範圍的檔案不要碰。如果你發現必須修改範圍外的檔案才能完成任務，\n")
	b.WriteString("立即停下來說明原因，等待使用者決定。")

	b.WriteString("\n\n## 不可違反的限制\n\n")
	b.WriteString(p.Spec.Constraints)

	if p.Spec.References != "" {
		b.WriteString("\n\n## 參考資料\n\n")
		b.WriteString(p.Spec.References)
	}

	fmt.Fprintf(&b, "\n\n## Logging 規範\n\n%s", logPolicyText(p.LogPolicy))

	fmt.Fprintf(&b, `

## 你的工作流程

1. 先讀取 CLAUDE.md 了解此專案的架構原則和 logging 規範
   讀取 .claude/docs/tracker.md 了解如何操作 tracker（開 PR、更新 status）
2. 讀取 SCOPE 中列出的所有檔案，理解現有 code 結構
3. 如果參考資料有列出參考檔案，也一起讀
4. 按照 APPROACH 描述的方案實作
5. 實作過程中，確保所有新 code 符合 CLAUDE.md 的 logging 規範
6. 完成後，逐條對照 ACCEPTANCE_CRITERIA 自我檢查
7. 用 git add + git commit 提交改動
8. Commit message 格式: [%s] {簡短描述}
9. 開始實作前，更新 issue label: 移除 "todo"，加入 "wip"
10. 開 PR 時，**必須** target `+"`%s`"+` 分支（--base %s）。
    嚴禁 target 其他分支。如果不確定，先停下來確認再開 PR。
11. 開 PR 後，更新 issue label: 移除 "wip"，加入 "review"

## 停下來問使用者的時機

- APPROACH 的描述不夠清楚，你不確定該怎麼做
- 你發現需要修改 SCOPE 範圍外的檔案
- 你發現 CONSTRAINTS 中的限制跟 APPROACH 衝突
- 你遇到不確定的技術決策（多種寫法都可以時）
- 任何硬體相關的邏輯你不確定的（timeout 值、安全狀態行為等）

## Tracker 操作注意

開 PR、更新 status 時，依 .claude/docs/tracker.md 指示。
不論使用 MCP 或 REST API，長文字（PR body、comment）一律先用 Write tool 寫到暫存檔，
再用 Read tool 讀取內容傳入 API。絕對不要在 bash 命令或 MCP 參數裡直接內嵌長文字。
`, p.IssueID, p.BaseBranch, p.BaseBranch)

	return b.String()
}

func logPolicyText(policy string) string {
	switch policy {
	case "strict":
		return "所有 Service 方法必須有進出 log，硬體操作必須有指令/回應 log，狀態機轉換必須有前後狀態 log。"
	case "standard":
		return "Service 方法有進出 log、異常有完整 log。"
	case "minimal":
		return "只需記錄錯誤和關鍵操作。"
	default:
		return ""
	}
}
