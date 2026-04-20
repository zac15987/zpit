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
		b.WriteString(channelToolsSection(p.Spec.CoordinatesWith))
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

	if p.ChannelEnabled && len(p.Spec.CoordinatesWith) > 0 {
		b.WriteString(coordinationReviewGate(p.Spec.CoordinatesWith))
	}
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

	if p.ChannelEnabled && len(p.Spec.CoordinatesWith) > 0 {
		b.WriteString(coordinationReviewGate(p.Spec.CoordinatesWith))
	}
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

	b.WriteString("#### Parallel Commit Protocol (required for every `[P]` teammate)\n\n")
	b.WriteString("Parallel teammates share one linked worktree, so naive `git add` / `git commit` races on the shared staging index and on `refs/heads/<branch>.lock`. This has caused real commit-content corruption (and mass-delete commits) in the past. You MUST brief every teammate in its spawn prompt so it can follow `.claude/agents/task-runner.md` → **Parallel Commit Protocol**.\n\n")
	b.WriteString("**Important:** in a linked worktree, `.git` is a pointer file — not a directory. Paths MUST be resolved via `git rev-parse --git-dir` / `git rev-parse --git-common-dir`; hard-coding `.git/...` fails with `fatal: Unable to create '.git/...': No such file or directory`.\n\n")
	b.WriteString("Each teammate's spawn prompt must include, on its own line:\n\n")
	b.WriteString("```\nparallel_task_id: T{N}\n```\n\n")
	b.WriteString("(substitute the teammate's task ID). The `task-runner` agent reads this line as the signal to switch on the protocol. In the same spawn prompt, include the explicit `files:` / `paths:` list from the task — teammates use it as the `git add -- <files>` pathspec.\n\n")
	b.WriteString("Summary of what every teammate will do — the agent doc has the exact commands, and they MUST be run as a SINGLE Bash invocation (the Bash tool starts a fresh shell per call, so `export GIT_INDEX_FILE` does not persist across calls):\n\n")
	b.WriteString("1. Resolve paths: `GIT_DIR=$(git rev-parse --git-dir)` and `GIT_COMMON_DIR=$(git rev-parse --git-common-dir)`; define `IDX=\"$GIT_DIR/index.zpit.T{N}\"` and `LOCK=\"$GIT_COMMON_DIR/zpit-commit.lock\"`.\n")
	b.WriteString("2. Seed the private index from HEAD: `GIT_INDEX_FILE=\"$IDX\" git read-tree HEAD` — without this, the commit's tree contains ONLY the newly-staged files and records every other path as deleted.\n")
	b.WriteString("3. Stage declared files only: `GIT_INDEX_FILE=\"$IDX\" git add -- <declared files>` (pathspec required by hook and by this protocol).\n")
	b.WriteString("4. Serialize commit: retry `mkdir \"$LOCK\"` up to 5× with jittered sleep, then `GIT_INDEX_FILE=\"$IDX\" git commit ...`, then `rmdir \"$LOCK\"`.\n")
	b.WriteString("5. Clean up: `rm -f \"$IDX\"` on both success and failure paths.\n\n")
	b.WriteString("If any teammate fails all 5 lock attempts or returns without a commit, stop the group — do NOT force-remove the lock and do NOT retry the batch automatically. Report the failure and wait for the user.\n\n")
}

// coordinationReviewGate returns the review gate text for when CoordinatesWith is non-empty.
// It instructs the agent to verify all CHANNEL_ASSUMPTION comments are resolved before
// transitioning to review.
func coordinationReviewGate(coordinatesWith []string) string {
	refs := make([]string, len(coordinatesWith))
	for i, id := range coordinatesWith {
		refs[i] = "#" + id
	}
	issueList := strings.Join(refs, ", ")

	return fmt.Sprintf(`
## Coordination Review Gate

Before adding the "review" label, you MUST verify all channel assumptions are resolved:

1. Search the entire codebase for `+"`[CHANNEL_ASSUMPTION]`"+` comments
2. If any remain:
   a. Call `+"`list_artifacts`"+` to check if the needed artifacts are now available
   b. Call `+"`send_message`"+` to %s requesting the missing artifacts
   c. If artifacts are now available, verify and clean up the assumptions
   d. Repeat up to 3 cumulative attempts total
3. After 3 attempts, if `+"`[CHANNEL_ASSUMPTION]`"+` comments still remain:
   - Do NOT add the "review" label
   - Post an issue comment listing all unresolved assumptions and their locations
   - Wait for the user to decide how to proceed
4. Only when ALL `+"`[CHANNEL_ASSUMPTION]`"+` comments have been resolved (deleted) may you add the "review" label
`, issueList)
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
// Only injected when ChannelEnabled is true. When coordinatesWith is non-empty,
// appends the Dependency Coordination Protocol with specific issue references.
func channelToolsSection(coordinatesWith []string) string {
	var b strings.Builder
	b.WriteString(`

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
`)

	if len(coordinatesWith) > 0 {
		refs := make([]string, len(coordinatesWith))
		for i, id := range coordinatesWith {
			refs[i] = "#" + id
		}
		issueList := strings.Join(refs, ", ")

		fmt.Fprintf(&b, `
### Dependency Coordination Protocol

You are coordinating with parallel agents on issues: %s (from COORDINATES_WITH).
Follow this protocol throughout your implementation:

**1. Startup Probe:**
- Call `+"`list_artifacts`"+` to check what's already published
- Call `+"`list_projects`"+` to discover active agents
- For each COORDINATES_WITH issue (%s), call `+"`send_message`"+` announcing which interfaces/types you plan to define or consume

**2. Assumption Marking:**
- When you need an artifact (interface, type, schema) from a coordinating agent but it's not yet available via `+"`list_artifacts`"+`, proceed with your best inference
- Mark EVERY such inference with a comment: `+"`// [CHANNEL_ASSUMPTION] <description, pending artifact from #N>`"+`
- Example: `+"`// [CHANNEL_ASSUMPTION] Using inferred InventoryReader interface, pending artifact from #%s`"+`
- Continue implementation — do NOT block waiting

**3. Verification & Cleanup:**
- When you receive a channel notification about a new artifact, search your codebase for related `+"`[CHANNEL_ASSUMPTION]`"+` comments
- Compare the published artifact against your assumption
- If consistent: delete the `+"`[CHANNEL_ASSUMPTION]`"+` comment
- If inconsistent: update your implementation to match the published artifact, then delete the comment

**4. Publish Obligation:**
- After defining any interface, type, or schema that coordinating agents may depend on, immediately call `+"`publish_artifact`"+`
- Do not wait until implementation is complete — publish as early as possible
`, issueList, issueList, coordinatesWith[0])
	}

	return b.String()
}
