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
3. Re-read the reviewer's original comment carefully — understand the root concern behind each item, not just the surface description
4. List each issue raised by the reviewer (prioritize MUST FIX items)
5. **Research before fixing (mandatory):**
   a. If the issue's REFERENCES contain any URLs, use WebFetch to retrieve and read each URL
   b. Use WebSearch to research any unfamiliar concepts or APIs mentioned in the reviewer's feedback
   c. If fixes depend on external libraries or APIs not fully documented in REFERENCES, use WebSearch to verify current API signatures and version compatibility before coding
6. Fix the code for each issue
7. During fixes, ensure all changes comply with CLAUDE.md conventions and the code quality baseline
8. After fixing, re-read each modified file to verify your changes are consistent and no unintended edits remain
9. After completion, self-check against each ACCEPTANCE_CRITERIA item
10. Use git add + git commit to commit changes
11. Commit message format: [%s] fix: {brief description of fix}
12. Write a Revision Summary to both the PR comment AND the issue comment, covering:
   - Which reviewer issues were addressed (reference by item number or quote)
   - How each was fixed (brief: file changed, what changed)
   - Any reviewer issues intentionally NOT addressed, with reason
13. Before starting fixes, update issue label: remove "needs-changes", add "wip"
14. After fixes are complete, update issue label: remove "wip", add "review"

Note: This PR's target branch is `+"`%s`"+`. If you find the PR targets the wrong branch,
stop immediately and notify the user; do not continue working.

## When to Stop and Ask the User

- You are unsure how to fix an issue raised by the reviewer
- The fix requires modifying files outside the SCOPE
- The reviewer's feedback conflicts with the CONSTRAINTS
- The reviewer's feedback is vague or lacks a clear direction for what to change — ask for clarification before implementing a guess
- You believe the reviewer's feedback is incorrect or contradicts the project's conventions — present your reasoning and wait for resolution; compliance without agreement is not acceptable
- Implementing a reviewer's suggestion would degrade code quality, break an existing AC, or violate a CONSTRAINT — flag the conflict explicitly

## Tracker Operation Notes

When updating labels or reading comments, follow the instructions in .claude/docs/tracker.md.
Prefer MCP tools — pass content directly as a parameter.
If MCP is unavailable, use the Write tool + --body-file pattern:
1. Write content to a temp file in the working directory (e.g. ./tmp_body.md) using the Write tool
2. Use gh with --body-file ./tmp_body.md or curl with -d @./tmp_body.md
3. Delete the temp file: rm ./tmp_body.md
Never embed long text directly in bash commands.
`, p.IssueID, p.BaseBranch)

	return b.String()
}
