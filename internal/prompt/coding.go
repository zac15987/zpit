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
	b.WriteString("<acceptance_criteria>\n")
	b.WriteString(strings.Join(p.Spec.AcceptanceCriteria, "\n"))
	b.WriteString("\n</acceptance_criteria>")

	b.WriteString("\n\n## Allowed File Scope\n\n")
	b.WriteString("<scope>\n")
	b.WriteString(formatScope(p.Spec.Scope))
	b.WriteString("</scope>\n")
	b.WriteString("\nDo not touch files outside this scope. If you find that you must modify files outside the scope to complete the task,\n")
	b.WriteString("stop immediately, explain the reason, and wait for the user's decision.")

	b.WriteString("\n\n## Constraints (must not violate)\n\n")
	b.WriteString("<constraints>\n")
	b.WriteString(p.Spec.Constraints)
	b.WriteString("\n</constraints>")

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
7. **Self-check against each ACCEPTANCE_CRITERIA item** (mandatory pre-commit gate) — walk through them one at a time, do NOT batch this into a single "looks good" pass:
   a. Quote the AC text verbatim (do not paraphrase in your head)
   b. Point to the concrete file:line or test name that satisfies it
   c. Treat words like **exactly**, **without**, **only**, **must not**, **never** as strict constraints — no interpretation, no "close enough"
   d. For log-format ACs, write out the actual log string your code produces and compare **character by character** against the AC spec — an extra field or wrong order counts as FAIL
   e. For "add test" / "test coverage" ACs, confirm the test file exists AND the test actually exercises the new code path (not just compiles green)
   f. If any AC cannot be traced to a concrete artifact, STOP — do not commit, do not open PR. Post an issue comment naming the unaccounted AC and wait for clarification
%s8. Before committing, re-read each modified file to verify your changes are consistent and no unintended edits remain
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
`, acSelfCheckExample("initial"), p.IssueID, p.BaseBranch, p.BaseBranch)

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
// Generates subagent delegation for sequential tasks and parallel-subagent-batch
// delegation for [P] task groups.
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
		buildParallelSubagentDelegation(b, p)
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
			fmt.Fprintf(b, "%d. **Parallel group [%s]**: Dispatch a parallel subagent batch. For each task in the group, spawn a subagent using `task-runner` subagent type with `isolation: \"worktree\"`. ", step, strings.Join(ids, ", "))
			b.WriteString("Each subagent receives its task assignment as the spawn prompt and runs inside an isolated child worktree. Wait for all subagents to complete, capture each `worktreePath` returned by the Agent tool, and verify each commit.\n")
			b.WriteString("   **Before dispatching this batch, capture the parent HEAD.** Run ONE Bash call from your parent worktree root FIRST (before spawning any subagent):\n")
			b.WriteString("   ```\n")
			b.WriteString("   PARENT_HEAD=$(git rev-parse HEAD)\n")
			b.WriteString("   echo \"PARENT_HEAD=$PARENT_HEAD\"\n")
			b.WriteString("   ```\n")
			b.WriteString("   Record the echoed value — you will need it for the sanity check below. (Bash state does NOT persist across tool calls, so treat `$PARENT_HEAD` as a literal value to paste in, not as a variable that survives.)\n")
			b.WriteString("   **After all subagents return, discover each subagent's branch AND verify it actually committed.** Run ONE Bash call from your parent worktree root:\n")
			b.WriteString("   ```\n")
			b.WriteString("   PARENT_HEAD=<the-value-you-captured-above>\n")
			b.WriteString("   for path in <worktreePath-T{N1}> <worktreePath-T{N2}> ...; do\n")
			b.WriteString("     branch=$(git -C \"$path\" rev-parse --abbrev-ref HEAD)\n")
			b.WriteString("     tip=$(git -C \"$path\" rev-parse HEAD)\n")
			b.WriteString("     if [ \"$tip\" = \"$PARENT_HEAD\" ]; then\n")
			b.WriteString("       echo \"ABORT: parallel subagent branch $branch (worktree: $path) still points at PARENT_HEAD $PARENT_HEAD — subagent did not commit in its child worktree.\"\n")
			b.WriteString("       exit 1\n")
			b.WriteString("     fi\n")
			b.WriteString("     echo \"$path => $branch @ $tip\"\n")
			b.WriteString("   done\n")
			b.WriteString("   ```\n")
			b.WriteString("   The Agent tool does NOT return `worktreeBranch` (Claude Code's WorktreeCreate-hook path returns only `worktreePath` — see docs/known-issues.md §3), so discovery via `rev-parse --abbrev-ref HEAD` is authoritative. The additional `$tip = $PARENT_HEAD` guard catches subagents that spawned correctly but bypassed their child worktree (e.g. `cd`-ed to the parent — see docs/known-issues.md §6). If the loop aborts, STOP the batch: post an issue comment naming the misbehaving subagent(s) and their worktree paths, do NOT cherry-pick, do NOT run `git cherry-pick --skip`. Hand off to the user.\n")
			b.WriteString("   Capture each echoed `branch` value (the `@ $tip` is just for confirmation — use the branch name for the steps below).\n")
			b.WriteString("   **Integrate the batch.** Run as ONE Bash call in your parent worktree root (in task-ID order T{N1}, T{N2}, …):\n")
			b.WriteString("   ```\n")
			b.WriteString("   git cherry-pick <worktreeBranch-T{N1}> <worktreeBranch-T{N2}> ...\n")
			b.WriteString("   ```\n")
			b.WriteString("   Each cherry-pick lands that subagent's single commit on your branch, preserving its original message. **If any cherry-pick fails with a conflict**, immediately run `git cherry-pick --abort`, then STOP: post an issue comment listing the conflicting paths (spec bug — two `[P]` tasks that share a file), and do NOT retry automatically. **Do NOT run `git cherry-pick --skip` under any circumstances** — `--skip` silently drops a subagent's commit. The sanity check above already verified every subagent branch has real work, so any later \"empty commit\" error indicates something is genuinely wrong (spec bug or concurrent edit), not a `--skip`-able state. Surface the error and let the user investigate.\n")
			b.WriteString("   **Cleanup — TWO SEPARATE Bash tool calls** (never chained with `&&` — a hook block on one must not kill the other):\n\n")
			b.WriteString("   Bash call 1 — remove worktrees:\n")
			b.WriteString("   ```\n")
			b.WriteString("   for path in <worktreePath-T{N1}> <worktreePath-T{N2}> ...; do git worktree remove --force \"$path\"; done\n")
			b.WriteString("   ```\n")
			b.WriteString("   Always pass `--force` from the start — child worktrees contain the copied `.claude/` directory and will not remove without it.\n\n")
			b.WriteString("   Bash call 2 — delete parallel subagent branches:\n")
			b.WriteString("   ```\n")
			b.WriteString("   git branch -D <worktreeBranch-T{N1}> <worktreeBranch-T{N2}> ...\n")
			b.WriteString("   ```\n")
			b.WriteString("   If Bash call 2 is blocked by a hook or fails for any other reason, retry it as a standalone Bash call before moving on — do NOT skip branch deletion just because worktree removal already succeeded. Leaked parallel-subagent branches pollute the local branch list.\n")
			b.WriteString("   Only after this cleanup should you proceed to the next step. The parent worktree's HEAD has advanced by N commits (one per subagent), so subsequent sequential tasks and final-adjustment commits run against the correct tree.\n")
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
6. Execute tasks according to the **Task Execution Order** above, delegating each task to a `+"`task-runner`"+` subagent (or a parallel subagent batch for `+"`[P]`"+` groups).
   For each delegation:
   a. Provide the subagent with: issue ID, task ID, task description, file scope, the full APPROACH section, and the commit format [%s] T{N}: {short description}
   b. After the subagent completes, verify the commit exists and the changes are consistent
   c. If a subagent reports failure, retry the delegation once. If still failing, stop and post issue comment explaining what failed — do NOT open PR
7. After ALL tasks complete (whether sequential or a parallel batch), **self-check against each ACCEPTANCE_CRITERIA item** (mandatory pre-commit gate) — walk through them one at a time, do NOT batch this into a single "looks good" pass:
   a. Quote the AC text verbatim (do not paraphrase in your head)
   b. Point to the concrete file:line or test name that satisfies it
   c. Treat words like **exactly**, **without**, **only**, **must not**, **never** as strict constraints — no interpretation, no "close enough"
   d. For log-format ACs, write out the actual log string your code produces and compare **character by character** against the AC spec — an extra field or wrong order counts as FAIL
   e. For "add test" / "test coverage" ACs, confirm the test file exists AND the test actually exercises the new code path (not just compiles green)
   f. If any AC cannot be traced to a concrete artifact, STOP — do not commit, do not open PR. Post an issue comment naming the unaccounted AC and wait for clarification
%s8. Use git add + git commit for any final adjustments
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
`, acSelfCheckExample("initial"), p.IssueID, p.BaseBranch, p.BaseBranch)

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

// buildParallelSubagentDelegation writes the delegation instructions for
// parallel [P] task groups. Each [P] task spawns its own task-runner subagent
// with isolation: "worktree" — these are regular Claude Code subagents, not
// Claude Code's Agent Team (team_name + name) mechanism.
func buildParallelSubagentDelegation(b *strings.Builder, p CodingParams) {
	b.WriteString("### Parallel Subagent Delegation ([P] Tasks)\n\n")
	b.WriteString("When a group of `[P]` tasks is ready (all dependencies satisfied), dispatch a parallel subagent batch:\n\n")
	b.WriteString("- Spawn one **subagent per `[P]` task**, each using `task-runner` subagent type.\n")
	b.WriteString("- Each subagent receives its specific task assignment as the spawn prompt.\n")
	b.WriteString("- Subagents work in parallel, each in its own context window.\n")
	fmt.Fprintf(b, "- Each subagent commits independently with format `[%s] T{N}: {short description}`.\n", p.IssueID)
	b.WriteString("- Wait for ALL subagents to complete before proceeding to the next task group.\n")
	b.WriteString("- After the batch finishes, verify all commits exist and are consistent.\n\n")

	b.WriteString("#### Worktree Isolation (required for every `[P]` subagent)\n\n")
	b.WriteString("Parallel subagents previously shared your worktree and raced on the staging index + branch ref. That model is retired. Each `[P]` subagent now runs in its own git worktree forked from your current HEAD via Claude Code's `isolation: \"worktree\"` mechanism, so commits can't collide.\n\n")
	b.WriteString("For each subagent, call the Agent tool with:\n\n")
	b.WriteString("```\n")
	b.WriteString("Agent tool parameters:\n")
	b.WriteString("  subagent_type: \"task-runner\"\n")
	b.WriteString("  isolation: \"worktree\"\n")
	b.WriteString("  description: \"[ISSUE-ID] T{N}: {short description}\"\n")
	b.WriteString("  prompt: <full task context including issue ID, task ID, description, file paths, APPROACH, and commit format>\n")
	b.WriteString("```\n\n")
	b.WriteString("**Important — do NOT embed worktree paths or `cd` instructions in the subagent's `prompt` argument.** Claude Code's `isolation: \"worktree\"` automatically sets the subagent's CWD to the child worktree. If you write phrases like \"work in D:\\...\\worktrees\\<issue>\" or \"cd to the project root\" in the spawn prompt, or embed any absolute path that points at the parent worktree, the subagent may `cd` there and commit in the shared parent worktree instead of its isolated child — this defeats the isolation entirely and will trigger the Task Execution Order sanity check to ABORT the batch (see docs/known-issues.md §6 for the incident that led to this warning). Keep the `prompt` strictly about the task: issue ID, task ID, description, file paths (SCOPE as repo-relative paths like `internal/git/ops.go`, never absolute), APPROACH, commit format. No cwd hints. No absolute paths. No shell commands. The subagent has its own CWD and does not need to know where it is.\n\n")
	b.WriteString("Claude Code invokes zpit's `WorktreeCreate` hook (`.claude/hooks/worktree-create.sh`), which forks a child worktree under `.zpit-children/<slug>` from your current HEAD (not `origin/<defaultBranch>` — that's why we bypass Claude Code's built-in path), deploys the `.claude/` directory, and hands the path to the subagent. The subagent commits normally inside the child worktree on branch `<your-branch>-<slug>`.\n\n")
	b.WriteString("The Agent tool result for each subagent includes `worktreePath`. **It does NOT include `worktreeBranch`** — Claude Code's `WorktreeCreate`-hook path returns only the path (`executeWorktreeCreateHook` in Claude Code's `src/utils/hooks.ts` does not propagate branch names). The Task Execution Order section below instructs you to discover each subagent's branch via `git -C <worktreePath> rev-parse --abbrev-ref HEAD` before cleanup — do not try to read `worktreeBranch` from the Agent tool result. If a subagent returns without `worktreePath`, its worktree creation failed and you must abort the batch.\n\n")
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
