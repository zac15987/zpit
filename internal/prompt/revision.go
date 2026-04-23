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
9. **Self-check against reviewer feedback and ACCEPTANCE_CRITERIA** (mandatory pre-commit gate) — walk through each item one at a time, do NOT batch this into a single "looks good" pass:
   a. For each reviewer MUST FIX (🔴) item: quote the reviewer's wording verbatim, then point to the concrete file:line or test name that addresses it
   b. Re-read each originally-FAILED AC that the MUST FIX items map to — verify the fix now satisfies the AC wording, not just the reviewer's literal request
   c. Treat words like **exactly**, **without**, **only**, **must not**, **never** in both the AC and the reviewer's feedback as strict constraints — no interpretation, no "close enough"
   d. For log-format ACs, write out the actual log string your code produces and compare **character by character** against the AC spec — an extra field or wrong order counts as FAIL
   e. Regression check — scan the ACs you did NOT touch in this revision; for each, confirm you haven't broken it (especially if the fix spans files that other ACs also reference)
   f. If any reviewer item cannot be traced to a concrete fix, or if regressions are detected, STOP — do not commit, do not push. Post a PR comment describing the gap and ask for guidance.
10. Use git add + git commit to commit changes
11. Commit message format: [%s] fix: {brief description of fix}
12. **Push the commit to the remote PR branch**: run ` + "`git push`" + ` in the worktree. The PR branch's upstream was set when the initial coding session opened the PR via ` + "`gh pr create`" + `, so plain ` + "`git push`" + ` is enough. If push is rejected because the upstream is unset (rare — only if the PR branch was recreated), use ` + "`git push -u origin <current-branch>`" + `. **Skipping this step is a silent failure mode**: your commits stay local, the reviewer fetches the remote PR and sees the old code, and the loop advances on stale state.
13. Write a Revision Summary to both the PR comment AND the issue comment, covering:
   - Which reviewer issues were addressed (reference by item number or quote)
   - How each was fixed (brief: file changed, what changed)
   - Any reviewer issues intentionally NOT addressed, with reason
14. Before starting fixes, update issue label: remove "needs-changes", add "wip"
15. After fixes are complete, update issue label: remove "wip", add "review"

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

Before performing any tracker operation (label, comment), you MUST first read .claude/docs/tracker.md.
Use ONLY the tools and methods specified in tracker.md — do not use other MCP servers or CLIs not listed there.
Never embed long text directly in bash commands or MCP parameters.
Write long content to a temp file first (e.g. ./tmp_body.md), then pass it via --body-file or read it back before sending.
Delete the temp file after use.
`, p.IssueID, p.BaseBranch)

	return b.String()
}
