package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/tracker"
)

// CodingParams holds all data needed to assemble a coding agent prompt.
type CodingParams struct {
	IssueID        string
	IssueTitle     string
	Spec           *tracker.IssueSpec
	LogPolicy      string // "strict" | "standard" | "minimal"
	BaseBranch     string // e.g. "dev"
	ChannelEnabled bool   // true when cross-agent channel communication is active
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

	if p.ChannelEnabled {
		b.WriteString(channelToolsSection())
	}

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
4. **Research before coding (mandatory):**
   a. If REFERENCES contain any URLs, use WebFetch to retrieve and read each URL
   b. Use WebSearch to research the problem domain described in CONTEXT and the implementation approach — gather up-to-date information, common pitfalls, and best practices relevant to this task
   c. If implementation depends on external libraries or APIs, use WebSearch to verify current API signatures and version compatibility — do not code against training-data assumptions
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

// taskGroup represents a batch of tasks to execute together.
// A sequential group contains exactly one task; a parallel group contains multiple [P] tasks.
type taskGroup struct {
	parallel bool
	tasks    []tracker.TaskEntry
}

// groupTasks partitions an ordered task list into sequential singletons and parallel batches.
// Consecutive tasks with Parallel=true are grouped together; all others are singletons.
func groupTasks(tasks []tracker.TaskEntry) []taskGroup {
	var groups []taskGroup
	i := 0
	for i < len(tasks) {
		if tasks[i].Parallel {
			// Collect consecutive parallel tasks into one group.
			batch := []tracker.TaskEntry{tasks[i]}
			i++
			for i < len(tasks) && tasks[i].Parallel {
				batch = append(batch, tasks[i])
				i++
			}
			groups = append(groups, taskGroup{parallel: true, tasks: batch})
		} else {
			groups = append(groups, taskGroup{parallel: false, tasks: []tracker.TaskEntry{tasks[i]}})
			i++
		}
	}
	return groups
}

// hasParallelTasks returns true if any task in the list has the Parallel flag.
func hasParallelTasks(tasks []tracker.TaskEntry) bool {
	for _, t := range tasks {
		if t.Parallel {
			return true
		}
	}
	return false
}

// buildTaskWorkflow writes the task-ordered coding workflow when TASKS is present.
// Generates subagent delegation for sequential tasks and Agent Team delegation for parallel tasks.
func buildTaskWorkflow(b *strings.Builder, p CodingParams) {
	b.WriteString("\n\n## Task Decomposition\n\n")
	b.WriteString("This issue has been decomposed into ordered tasks.\n")
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

	// Execution strategy section
	b.WriteString("\n## Execution Strategy\n\n")
	b.WriteString("You are the **orchestrator**. Do NOT implement tasks yourself — delegate each task to a `task-runner` subagent.\n")
	b.WriteString("Each subagent runs in its own context window, providing context isolation between tasks.\n\n")

	buildSubagentDelegation(b, p)

	if hasParallelTasks(p.Spec.Tasks) {
		buildTeamDelegation(b, p)
	}

	// Task execution order
	groups := groupTasks(p.Spec.Tasks)
	b.WriteString("### Task Execution Order\n\n")
	step := 1
	for _, g := range groups {
		if g.parallel {
			ids := make([]string, len(g.tasks))
			for i, t := range g.tasks {
				ids[i] = t.ID
			}
			fmt.Fprintf(b, "%d. **Parallel group [%s]**: Create an Agent Team. For each task in the group, spawn a teammate using `task-runner` subagent type. ", step, strings.Join(ids, ", "))
			b.WriteString("Each teammate receives its task assignment as the spawn prompt. Wait for all teammates to complete and verify each commit.\n")
		} else {
			t := g.tasks[0]
			fmt.Fprintf(b, "%d. **%s** (sequential): Delegate to a `task-runner` subagent via the Agent tool. ", step, t.ID)
			b.WriteString("Wait for completion, verify the commit, then proceed to the next step.\n")
		}
		step++
	}

	fmt.Fprintf(b, `
## Your Workflow

1. Read CLAUDE.md to understand the project's architecture principles and logging policy
   Read .claude/docs/tracker.md to understand how to operate the tracker (open PR, update status)
   Read .claude/docs/agent-guidelines.md to understand the behavioral rules for AI agents
   Read .claude/docs/code-construction-principles.md to understand the code quality baseline
2. Read all files listed in SCOPE to understand the existing code structure
3. If references list any reference files, read those too
4. **Research before coding (mandatory):**
   a. If REFERENCES contain any URLs, use WebFetch to retrieve and read each URL
   b. Use WebSearch to research the problem domain described in CONTEXT and the implementation approach — gather up-to-date information, common pitfalls, and best practices relevant to this task
   c. If implementation depends on external libraries or APIs, use WebSearch to verify current API signatures and version compatibility — do not code against training-data assumptions
5. Before starting implementation, update issue label: remove "todo", add "wip"
6. Execute tasks according to the **Task Execution Order** above, delegating each task to a `+"`task-runner`"+` subagent (or Agent Team for parallel groups).
   For each delegation:
   a. Provide the subagent with: issue ID, task ID, task description, file scope, the full APPROACH section, and the commit format [%s] T{N}: {short description}
   b. After the subagent completes, verify the commit exists and the changes are consistent
   c. If a subagent reports failure, retry the delegation once. If still failing, stop and post issue comment explaining what failed — do NOT open PR
7. After ALL tasks complete (whether via subagent or Agent Team), self-check against each ACCEPTANCE_CRITERIA item
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

// buildSubagentDelegation writes the delegation instructions for sequential tasks.
func buildSubagentDelegation(b *strings.Builder, p CodingParams) {
	b.WriteString("### Subagent Delegation (Sequential Tasks)\n\n")
	b.WriteString("For each **sequential task** (without `[P]` marker), use the Agent tool to delegate to a `task-runner` subagent:\n\n")
	b.WriteString("```\n")
	b.WriteString("Agent tool parameters:\n")
	b.WriteString("  subagent_type: \"task-runner\"\n")
	b.WriteString("  description: \"[ISSUE-ID] T{N}: {short description}\"\n")
	b.WriteString("  prompt: <full task context including issue ID, task ID, description, file paths, APPROACH, and commit format>\n")
	b.WriteString("```\n\n")
	fmt.Fprintf(b, "The subagent will implement the task, commit with format `[%s] T{N}: {short description}`, and report results.\n", p.IssueID)
	b.WriteString("After each subagent returns, verify the commit and check for errors before proceeding.\n\n")
}

// buildTeamDelegation writes the delegation instructions for parallel [P] tasks using Agent Teams.
func buildTeamDelegation(b *strings.Builder, p CodingParams) {
	b.WriteString("### Agent Team Delegation (Parallel Tasks)\n\n")
	b.WriteString("When a group of `[P]` tasks is ready (all dependencies satisfied), create an Agent Team:\n\n")
	b.WriteString("- Spawn one **teammate per `[P]` task**, each using `task-runner` subagent type.\n")
	b.WriteString("- Each teammate receives its specific task assignment as the spawn prompt.\n")
	b.WriteString("- Teammates work in parallel, each in its own context window.\n")
	fmt.Fprintf(b, "- Each teammate commits independently with format `[%s] T{N}: {short description}`.\n", p.IssueID)
	b.WriteString("- Wait for ALL teammates to complete before proceeding to the next task group.\n")
	b.WriteString("- After the team finishes, verify all commits exist and are consistent.\n\n")
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

// channelToolsSection returns the cross-agent communication section for the coding prompt.
// Only injected when ChannelEnabled is true.
func channelToolsSection() string {
	return `

## Cross-Agent Communication

This project has cross-agent channel communication enabled. You have access to three MCP tools
for coordinating with other agents working on parallel issues:

- **publish_artifact**: After completing a key interface definition, type spec, schema, or config
  that other agents may depend on, call this tool to publish it to the shared broker.
  Other agents will receive a channel notification with your artifact content.
  Parameters: issue_id (your issue ID), type (e.g. "interface", "type", "schema"), content (the definition).

- **list_artifacts**: Call this tool to see all artifacts published by other agents in this project.
  Use this at the start of implementation to check if relevant interfaces or types have already been defined,
  and periodically during implementation to stay in sync.

- **send_message**: Send a direct message to another agent by their issue ID.
  Use this to request specific information, flag potential conflicts, or coordinate timing.
  Parameters: to_issue_id (target agent's issue ID), content (message text).

**When to use these tools:**
- After defining or modifying a shared interface/type, call publish_artifact so other agents can align.
- When you receive a channel notification about an artifact from another agent, review it and adapt your implementation if needed.
- If you discover a potential conflict with another agent's work, use send_message to coordinate.
- At the start of implementation, call list_artifacts to see what's already been published.
`
}
