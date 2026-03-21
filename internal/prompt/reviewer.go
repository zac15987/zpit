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
2. 用 git diff %s...HEAD 查看所有改動
3. 逐條比對 ACCEPTANCE_CRITERIA，每條標記 ✅ 或 ❌
4. 檢查是否有改動超出 SCOPE 範圍的檔案
5. 檢查是否違反 CONSTRAINTS
6. 檢查 logging 是否符合 CLAUDE.md 規範
7. 讀取 `+"`"+`.claude/docs/code-construction-principles.md`+"`"+`，抽樣檢查 code 品質
8. 產出 Review Report（見下方格式）
`, p.BaseBranch)

	return b.String()
}
