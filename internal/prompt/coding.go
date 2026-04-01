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

	if len(p.Spec.Tasks) > 0 {
		buildTaskWorkflow(&b, p)
	} else {
		buildStandardWorkflow(&b, p)
	}

	return b.String()
}

// buildStandardWorkflow writes the default coding workflow (no TASKS decomposition).
func buildStandardWorkflow(b *strings.Builder, p CodingParams) {
	fmt.Fprintf(b, `

## Your Workflow

1. Read CLAUDE.md to understand the project's architecture principles and logging policy
   Read .claude/docs/tracker.md to understand how to operate the tracker (open PR, update status)
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
   Read .claude/docs/code-construction-principles.md to understand the code quality baseline
2. Read all files listed in SCOPE to understand the existing code structure
3. If references list any reference files, read those too
4. If implementation depends on external libraries or APIs not fully documented in REFERENCES, use WebSearch to verify current API signatures and version compatibility before coding — do not code against training-data assumptions
5. Implement according to the approach described in APPROACH
6. During implementation, ensure all new code follows the logging policy in CLAUDE.md and the code quality baseline
7. After completion, self-check against each ACCEPTANCE_CRITERIA item
8. Before committing, re-read each modified file to verify your changes are consistent and no unintended edits remain
9. Use git add + git commit to commit changes
10. Commit message format: [%s] {short description}
11. Before starting implementation, update issue label: remove "todo", add "wip"
12. When opening a PR, you **must** target the `+"`%s`"+` branch (--base %s).
    Targeting any other branch is strictly forbidden. If unsure, stop and confirm before opening the PR.
13. After opening the PR, update issue label: remove "wip", add "review"

## When to Stop and Ask the User

- The APPROACH description is unclear and you are unsure how to proceed
- You find that you need to modify files outside the SCOPE
- You find that a CONSTRAINT conflicts with the APPROACH
- You encounter an uncertain technical decision (multiple valid approaches)
- Any hardware-related logic you are unsure about (timeout values, safe-state behavior, etc.)
- You discover during implementation that the APPROACH has a flaw or gap not covered by the Issue Spec

## Tracker Operation Notes

Before performing any tracker operation (PR, label, comment), you MUST first read .claude/docs/tracker.md.
Use ONLY the tools and methods specified in tracker.md — do not use other MCP servers or CLIs not listed there.
Never embed long text directly in bash commands or MCP parameters.
Write long content to a temp file first (e.g. ./tmp_body.md), then pass it via --body-file or read it back before sending.
Delete the temp file after use.
`, p.IssueID, p.BaseBranch, p.BaseBranch)
}

// buildTaskWorkflow writes the task-ordered coding workflow when TASKS is present.
func buildTaskWorkflow(b *strings.Builder, p CodingParams) {
	b.WriteString("\n\n## Task Decomposition\n\n")
	b.WriteString("This issue has been decomposed into ordered tasks. Execute tasks in order.\n")
	b.WriteString("Commit after each task with format: [" + p.IssueID + "] T{N}: {short description}\n\n")
	for _, task := range p.Spec.Tasks {
		fmt.Fprintf(b, "- %s: ", task.ID)
		if task.Parallel {
			b.WriteString("[P] ")
		}
		b.WriteString(task.Description)
		if len(task.Paths) > 0 {
			b.WriteString(" — files: " + strings.Join(task.Paths, ", "))
		}
		if len(task.DependsOn) > 0 {
			b.WriteString(" (depends: " + strings.Join(task.DependsOn, ", ") + ")")
		}
		b.WriteByte('\n')
	}

	fmt.Fprintf(b, `
## Your Workflow

Execute tasks in order. Commit after each task.

1. Read CLAUDE.md to understand the project's architecture principles and logging policy
   Read .claude/docs/tracker.md to understand how to operate the tracker (open PR, update status)
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
   Read .claude/docs/code-construction-principles.md to understand the code quality baseline
2. Read all files listed in SCOPE to understand the existing code structure
3. If references list any reference files, read those too
4. If implementation depends on external libraries or APIs not fully documented in REFERENCES, use WebSearch to verify current API signatures and version compatibility before coding — do not code against training-data assumptions
5. Before starting implementation, update issue label: remove "todo", add "wip"
6. For each task (T1, T2, ...):
   a. Implement the task according to APPROACH
   b. During implementation, ensure all new code follows the logging policy in CLAUDE.md and the code quality baseline
   c. After completing the task, re-read modified files to verify consistency
   d. Verify relevant ACs that relate to this task
   e. Commit with format: [%s] T{N}: {short description}
   f. If the task fails (tests break, build error), retry once. If still failing, stop and post issue comment explaining what failed — do NOT open PR
7. After all tasks complete, self-check against each ACCEPTANCE_CRITERIA item
8. Use git add + git commit for any final adjustments
9. When opening a PR, you **must** target the `+"`%s`"+` branch (--base %s).
   Targeting any other branch is strictly forbidden. If unsure, stop and confirm before opening the PR.
10. After opening the PR, update issue label: remove "wip", add "review"

## When to Stop and Ask the User

- The APPROACH description is unclear and you are unsure how to proceed
- You find that you need to modify files outside the SCOPE
- You find that a CONSTRAINT conflicts with the APPROACH
- You encounter an uncertain technical decision (multiple valid approaches)
- Any hardware-related logic you are unsure about (timeout values, safe-state behavior, etc.)
- You discover during implementation that the APPROACH has a flaw or gap not covered by the Issue Spec

## Tracker Operation Notes

Before performing any tracker operation (PR, label, comment), you MUST first read .claude/docs/tracker.md.
Use ONLY the tools and methods specified in tracker.md — do not use other MCP servers or CLIs not listed there.
Never embed long text directly in bash commands or MCP parameters.
Write long content to a temp file first (e.g. ./tmp_body.md), then pass it via --body-file or read it back before sending.
Delete the temp file after use.
`, p.IssueID, p.BaseBranch, p.BaseBranch)
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
