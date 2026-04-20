package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/tracker"
)

// ReviewerParams holds all data needed to assemble a reviewer prompt.
type ReviewerParams struct {
	IssueID     string
	IssueTitle  string
	Spec        *tracker.IssueSpec
	LogPolicy   string // "strict" | "standard" | "minimal"
	BaseBranch  string // e.g. "dev"
	ReviewRound int    // 0 = first review, >0 = revision review
}

const reviewerTrackerNotes = `
## Tracker Operation Notes

Before performing any tracker operation (comment, label, PR), you MUST first read .claude/docs/tracker.md.
Use ONLY the tools and methods specified in tracker.md — do not use other MCP servers or CLIs not listed there.
Never embed long text directly in bash commands or MCP parameters.
Write long content to a temp file first (e.g. ./tmp_review_report.md), then pass it via --body-file or read it back before sending.
Delete the temp file after use.
`

// BuildReviewerPrompt assembles the full reviewer agent prompt from Issue Spec data.
// When ReviewRound > 0, it generates a revision review prompt that focuses on the delta.
func BuildReviewerPrompt(p ReviewerParams) string {
	var b strings.Builder

	b.WriteString(locale.ResponseInstruction())

	// Common sections: issue info, requirements, approach, AC, scope, constraints, log policy
	if p.ReviewRound > 0 {
		fmt.Fprintf(&b, "You are performing a REVISION REVIEW (round %d) of issue %s: %s.\n", p.ReviewRound, p.IssueID, p.IssueTitle)
		b.WriteString("This is NOT a full review. Focus on whether the previous MUST FIX items were addressed.\n")
	} else {
		fmt.Fprintf(&b, "You are reviewing the implementation of issue %s: %s.\n", p.IssueID, p.IssueTitle)
	}

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

	if p.ReviewRound > 0 {
		buildRevisionReviewProcess(&b, p)
	} else {
		buildFirstReviewProcess(&b, p)
	}

	return b.String()
}

// buildFirstReviewProcess writes the review process for the first review (full scope).
func buildFirstReviewProcess(b *strings.Builder, p ReviewerParams) {
	fmt.Fprintf(b, `

## Your Review Process

1. Read CLAUDE.md to understand the project's conventions
   Read .claude/docs/tracker.md to understand how to operate the tracker (write comment, update label)
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
2. Read issue comments and PR comments to understand the full context (clarifier decisions, coding agent's change summary)
3. Use git diff %s...HEAD to view all changes
4. Re-read ACCEPTANCE_CRITERIA to confirm your understanding before marking verdicts — do not rely on your initial reading
5. Check each ACCEPTANCE_CRITERIA item, marking each as PASS or FAIL
6. Check whether any changes touch files outside the SCOPE
7. Check whether the PR's target branch is `+"`%s`"+`; if not, mark as FAIL and note it in the Review Report
8. Check whether any CONSTRAINTS are violated
9. Check whether logging complies with the CLAUDE.md policy
10. Read `+"`"+`.claude/docs/code-construction-principles.md`+"`"+`, and spot-check code quality
11. Produce the Review Report (see format below)
12. Write the Review Report to both the PR comment and the issue comment
13. If PASS, update issue label: remove "review", add "ai-review"
14. If NEEDS CHANGES, update issue label: remove "review", add "needs-changes"
`, p.BaseBranch, p.BaseBranch)

	b.WriteString(reviewerTrackerNotes)
}

// buildRevisionReviewProcess writes the review process for revision reviews (delta only).
func buildRevisionReviewProcess(b *strings.Builder, p ReviewerParams) {
	fmt.Fprintf(b, `

## Your Revision Review Process

This is a revision review. The coding agent has attempted to fix issues from the previous review.
You must focus on the delta — do NOT re-review the entire implementation from scratch.

1. Read CLAUDE.md to understand the project's conventions
   Read .claude/docs/tracker.md to understand how to operate the tracker (write comment, update label)
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
   Read .claude/docs/code-construction-principles.md for code quality reference
2. Read PR comments to find the previous NEEDS CHANGES review report
   Read issue comments for any additional context
3. List all MUST FIX (🔴) items from the previous review
4. Use git log --oneline %s...HEAD to see all commits; identify the revision commits
   (typically the latest commits with [%s] fix: prefix, added after the previous review)
5. Use git show or git diff on only the revision commits to view the delta
6. For each previous MUST FIX item, verify whether it was addressed — mark as ✅ Fixed / ❌ Still open
7. Re-read ACCEPTANCE_CRITERIA before the regression check — confirm your understanding of what each AC requires
8. Spot-check the full diff (git diff %s...HEAD) to verify no regressions in existing ACs
   You do NOT need to re-verify every AC in detail — only check for regressions introduced by the revision
9. Check scope + constraints on the new changes only
10. Produce the Revision Review Report (see format below)
11. Write the Review Report to both the PR comment and the issue comment
12. If all previous MUST FIX items are fixed and no regressions: update issue label: remove "review", add "ai-review"
13. If any MUST FIX item remains unfixed or regressions found: update issue label: remove "review", add "needs-changes"

## Revision Review Report Format

### Revision Review Summary
- Overall verdict: PASS / NEEDS CHANGES
- Round: %d

### Previous MUST FIX Verification
(List each MUST FIX item from the previous review)
- 🔴 Item 1: ✅ Fixed — [how it was fixed]
- 🔴 Item 2: ❌ Still open — [what's still missing]
...

### Regression Check
- Regressions found: ✅ None / ❌ [describe regression]

### New Findings (if any)
Severity rules match the first-review reviewer prompt: any correctness bug, dead code, dangling reference, or code-construction-principles violation introduced by the revision is 🔴 MUST FIX, **even if no AC requires otherwise**. 🟡 is reserved for genuine taste preferences only.
- 🔴 MUST FIX: [new blocking issue introduced by the revision — AC not met, broken behavior, dead code, tech debt, principles violation]
- 🟡 SUGGEST: [taste/style preference — NOT for correctness issues]

### Verdict Rules
- Any previous MUST FIX still open → NEEDS CHANGES
- Any regression found → NEEDS CHANGES
- **Any new 🔴 MUST FIX introduced by the revision → NEEDS CHANGES**, regardless of AC coverage
- All previous MUST FIX fixed + no regressions + no new 🔴 → PASS (🟡 suggestions do not block)
`, p.BaseBranch, p.IssueID, p.BaseBranch, p.ReviewRound)

	b.WriteString(reviewerTrackerNotes)
}
