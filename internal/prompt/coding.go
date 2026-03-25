package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/locale"
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

	b.WriteString(locale.ResponseInstruction())

	fmt.Fprintf(&b, "You are working on issue %s: %s\n", p.IssueID, p.IssueTitle)

	b.WriteString("\n## Problem to Solve\n\n")
	b.WriteString(p.Spec.Context)

	b.WriteString("\n\n## Implementation Approach\n\n")
	b.WriteString(p.Spec.Approach)

	b.WriteString("\n\n## Acceptance Criteria (every item must be met; self-check each one after completion)\n\n")
	b.WriteString(strings.Join(p.Spec.AcceptanceCriteria, "\n"))

	b.WriteString("\n\n## Allowed File Scope\n\n")
	b.WriteString(formatScope(p.Spec.Scope))
	b.WriteString("\nDo not touch files outside this scope. If you find that you must modify files outside the scope to complete the task,\n")
	b.WriteString("stop immediately, explain the reason, and wait for the user's decision.")

	b.WriteString("\n\n## Constraints (must not violate)\n\n")
	b.WriteString(p.Spec.Constraints)

	if p.Spec.References != "" {
		b.WriteString("\n\n## References\n\n")
		b.WriteString(p.Spec.References)
	}

	fmt.Fprintf(&b, "\n\n## Logging Policy\n\n%s", logPolicyText(p.LogPolicy))

	fmt.Fprintf(&b, `

## Your Workflow

1. Read CLAUDE.md to understand the project's architecture principles and logging policy
   Read .claude/docs/tracker.md to understand how to operate the tracker (open PR, update status)
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
   Read .claude/docs/code-construction-principles.md to understand the code quality baseline
2. Read all files listed in SCOPE to understand the existing code structure
3. If references list any reference files, read those too
4. Implement according to the approach described in APPROACH
5. During implementation, ensure all new code follows the logging policy in CLAUDE.md and the code quality baseline
6. After completion, self-check against each ACCEPTANCE_CRITERIA item
7. Use git add + git commit to commit changes
8. Commit message format: [%s] {short description}
9. Before starting implementation, update issue label: remove "todo", add "wip"
10. When opening a PR, you **must** target the `+"`%s`"+` branch (--base %s).
    Targeting any other branch is strictly forbidden. If unsure, stop and confirm before opening the PR.
11. After opening the PR, update issue label: remove "wip", add "review"

## When to Stop and Ask the User

- The APPROACH description is unclear and you are unsure how to proceed
- You find that you need to modify files outside the SCOPE
- You find that a CONSTRAINT conflicts with the APPROACH
- You encounter an uncertain technical decision (multiple valid approaches)
- Any hardware-related logic you are unsure about (timeout values, safe-state behavior, etc.)

## Tracker Operation Notes

When opening a PR or updating status, follow the instructions in .claude/docs/tracker.md.
Prefer MCP tools — pass content directly as a parameter.
If MCP is unavailable, use Bash heredoc to write to a temp file, then curl with @file.
Never embed long text directly in bash commands.
`, p.IssueID, p.BaseBranch, p.BaseBranch)

	return b.String()
}

func logPolicyText(policy string) string {
	switch policy {
	case "strict":
		return "All Service methods must have entry/exit logs, hardware operations must have command/response logs, state machine transitions must have before/after state logs."
	case "standard":
		return "Service methods must have entry/exit logs, exceptions must have full logs."
	case "minimal":
		return "Only log errors and critical operations."
	default:
		return ""
	}
}
