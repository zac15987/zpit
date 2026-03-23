package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/tracker"
)

// ReviewerParams holds all data needed to assemble a reviewer prompt.
type ReviewerParams struct {
	IssueID    string
	IssueTitle string
	Spec       *tracker.IssueSpec
	LogPolicy  string // "strict" | "standard" | "minimal"
	BaseBranch string // e.g. "dev"
}

// BuildReviewerPrompt assembles the full reviewer agent prompt from Issue Spec data.
func BuildReviewerPrompt(p ReviewerParams) string {
	var b strings.Builder

	fmt.Fprintf(&b, "你正在 review issue %s: %s 的實作。\n", p.IssueID, p.IssueTitle)

	b.WriteString("\n## 原始需求\n\n")
	b.WriteString(p.Spec.Context)

	b.WriteString("\n\n## 預期方案\n\n")
	b.WriteString(p.Spec.Approach)

	b.WriteString("\n\n## 驗收標準（逐條檢查，每條標記 ✅ 達成 / ❌ 未達成）\n\n")
	b.WriteString(strings.Join(p.Spec.AcceptanceCriteria, "\n"))

	b.WriteString("\n\n## 允許的修改範圍\n\n")
	b.WriteString(formatScope(p.Spec.Scope))

	b.WriteString("\n## 限制條件\n\n")
	b.WriteString(p.Spec.Constraints)

	fmt.Fprintf(&b, "\n\n## Logging 規範\n\n%s", logPolicyText(p.LogPolicy))

	fmt.Fprintf(&b, `

## 你的檢查流程

1. 讀取 CLAUDE.md 了解此專案的規範
   讀取 .claude/docs/tracker.md 了解如何操作 tracker（寫 comment、更新 label）
2. 用 git diff %s...HEAD 查看所有改動
3. 逐條比對 ACCEPTANCE_CRITERIA，每條標記 ✅ 或 ❌
4. 檢查是否有改動超出 SCOPE 範圍的檔案
5. 檢查 PR 的 target branch 是否為 `+"`%s`"+`，如果不是則標記為 ❌ 並在 Review Report 中指出
6. 檢查是否違反 CONSTRAINTS
7. 檢查 logging 是否符合 CLAUDE.md 規範
8. 讀取 `+"`"+`.claude/docs/code-construction-principles.md`+"`"+`，抽樣檢查 code 品質
9. 產出 Review Report（見下方格式）
10. 將 Review Report 同時寫到 PR comment 和 issue comment
11. 如果 PASS，更新 issue label: 移除 "review"，加入 "ai-review"
12. 如果 NEEDS CHANGES，更新 issue label: 移除 "review"，加入 "needs-changes"

## Tracker 操作注意

將 Review Report 寫到 tracker（comment）時，依 .claude/docs/tracker.md 指示。
不論使用 MCP 或 REST API，長文字一律先用 Write tool 寫到暫存檔，
再用 Read tool 讀取內容傳入 API。絕對不要在 bash 命令或 MCP 參數裡直接內嵌長文字。
`, p.BaseBranch, p.BaseBranch)

	return b.String()
}
