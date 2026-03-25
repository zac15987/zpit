package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/locale"
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

	b.WriteString(locale.ResponseInstruction())

	fmt.Fprintf(&b, "You are fixing issue %s: %s (revision round %d)\n", p.IssueID, p.IssueTitle, p.ReviewRound)

	b.WriteString("\n## Original Requirements\n\n")
	b.WriteString(p.Spec.Context)

	b.WriteString("\n\n## Expected Approach\n\n")
	b.WriteString(p.Spec.Approach)

	b.WriteString("\n\n## Acceptance Criteria\n\n")
	b.WriteString(strings.Join(p.Spec.AcceptanceCriteria, "\n"))

	b.WriteString("\n\n## Allowed File Scope\n\n")
	b.WriteString(formatScope(p.Spec.Scope))
	b.WriteString("\nDo not touch files outside this scope. If you find that you must modify files outside the scope to complete the task,\n")
	b.WriteString("stop immediately, explain the reason, and wait for the user's decision.")

	b.WriteString("\n\n## Constraints (must not violate)\n\n")
	b.WriteString(p.Spec.Constraints)

	fmt.Fprintf(&b, "\n\n## Logging Policy\n\n%s", logPolicyText(p.LogPolicy))

	fmt.Fprintf(&b, `

## Your Workflow

1. Read CLAUDE.md to understand the project's architecture principles and logging policy
   Read .claude/docs/tracker.md to understand how to operate the tracker
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
   Read .claude/docs/code-construction-principles.md to understand the code quality baseline
2. Read the latest Review Report comment on the PR (find the one with NEEDS CHANGES)
3. List each issue raised by the reviewer (prioritize MUST FIX items)
4. Fix the code for each issue
5. During fixes, ensure all changes comply with CLAUDE.md conventions and the code quality baseline
6. After completion, self-check against each ACCEPTANCE_CRITERIA item
7. Use git add + git commit to commit changes
8. Commit message format: [%s] fix: {brief description of fix}
9. Before starting fixes, update issue label: remove "needs-changes", add "wip"
10. After fixes are complete, update issue label: remove "wip", add "review"

Note: This PR's target branch is `+"`%s`"+`. If you find the PR targets the wrong branch,
stop immediately and notify the user; do not continue working.

## When to Stop and Ask the User

- You are unsure how to fix an issue raised by the reviewer
- The fix requires modifying files outside the SCOPE
- The reviewer's feedback conflicts with the CONSTRAINTS

## Tracker Operation Notes

When updating labels or reading comments, follow the instructions in .claude/docs/tracker.md.
Prefer MCP tools — pass content directly as a parameter.
If MCP is unavailable, use Bash heredoc to write to a temp file, then curl with @file.
Never embed long text directly in bash commands.
`, p.IssueID, p.BaseBranch)

	return b.String()
}
