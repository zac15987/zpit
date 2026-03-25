package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/locale"
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

	b.WriteString(locale.ResponseInstruction())

	fmt.Fprintf(&b, "You are reviewing the implementation of issue %s: %s.\n", p.IssueID, p.IssueTitle)

	b.WriteString("\n## Original Requirements\n\n")
	b.WriteString(p.Spec.Context)

	b.WriteString("\n\n## Expected Approach\n\n")
	b.WriteString(p.Spec.Approach)

	b.WriteString("\n\n## Acceptance Criteria (check each item, mark each as PASS / FAIL)\n\n")
	b.WriteString(strings.Join(p.Spec.AcceptanceCriteria, "\n"))

	b.WriteString("\n\n## Allowed File Scope\n\n")
	b.WriteString(formatScope(p.Spec.Scope))

	b.WriteString("\n## Constraints\n\n")
	b.WriteString(p.Spec.Constraints)

	fmt.Fprintf(&b, "\n\n## Logging Policy\n\n%s", logPolicyText(p.LogPolicy))

	fmt.Fprintf(&b, `

## Your Review Process

1. Read CLAUDE.md to understand the project's conventions
   Read .claude/docs/tracker.md to understand how to operate the tracker (write comment, update label)
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
2. Use git diff %s...HEAD to view all changes
3. Check each ACCEPTANCE_CRITERIA item, marking each as PASS or FAIL
4. Check whether any changes touch files outside the SCOPE
5. Check whether the PR's target branch is `+"`%s`"+`; if not, mark as FAIL and note it in the Review Report
6. Check whether any CONSTRAINTS are violated
7. Check whether logging complies with the CLAUDE.md policy
8. Read `+"`"+`.claude/docs/code-construction-principles.md`+"`"+`, and spot-check code quality
9. Produce the Review Report (see format below)
10. Write the Review Report to both the PR comment and the issue comment
11. If PASS, update issue label: remove "review", add "ai-review"
12. If NEEDS CHANGES, update issue label: remove "review", add "needs-changes"

## Tracker Operation Notes

When writing the Review Report to the tracker (comment), follow the instructions in .claude/docs/tracker.md.
Prefer MCP tools — pass content directly as a parameter.
If MCP is unavailable, use Bash heredoc to write to a temp file, then curl with @file.
Never embed long text directly in bash commands.
`, p.BaseBranch, p.BaseBranch)

	return b.String()
}
