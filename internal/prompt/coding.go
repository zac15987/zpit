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
	for _, s := range p.Spec.Scope {
		fmt.Fprintf(&b, "[%s] %s", s.Action, s.Path)
		if s.Reason != "" {
			fmt.Fprintf(&b, " (%s)", s.Reason)
		}
		b.WriteByte('\n')
	}
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
2. 讀取 SCOPE 中列出的所有檔案，理解現有 code 結構
3. 如果參考資料有列出參考檔案，也一起讀
4. 按照 APPROACH 描述的方案實作
5. 實作過程中，確保所有新 code 符合 CLAUDE.md 的 logging 規範
6. 完成後，逐條對照 ACCEPTANCE_CRITERIA 自我檢查
7. 用 git add + git commit 提交改動
8. Commit message 格式: [%s] {簡短描述}

## 停下來問使用者的時機

- APPROACH 的描述不夠清楚，你不確定該怎麼做
- 你發現需要修改 SCOPE 範圍外的檔案
- 你發現 CONSTRAINTS 中的限制跟 APPROACH 衝突
- 你遇到不確定的技術決策（多種寫法都可以時）
- 任何硬體相關的邏輯你不確定的（timeout 值、安全狀態行為等）
`, p.IssueID)

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
