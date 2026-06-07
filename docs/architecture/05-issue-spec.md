# 5. Issue Spec — Structured Contract Between Agents

---

## 5.1 Why a Strict Format Is Required

The Issue is the **only communication interface** between Clarifier → Coding Agent → Reviewer.
The three agents never talk directly; they pass intent solely through the Issue Spec.

```
Clarifier ──writes──▸ Issue Spec ──reads──▸ Coding Agent
                          │
                          └──reads──▸ Reviewer (verifies whether AC is met)
```

If the format is ambiguous, the Coding Agent may:
- Misunderstand what the problem is (`CONTEXT` missing → edits the wrong place)
- Misunderstand how to solve it (`APPROACH` missing → picks its own solution)
- Misunderstand when it is done (`ACCEPTANCE_CRITERIA` vague → under- or over-delivers)
- Misunderstand which files it may touch (`SCOPE` missing → modifies things it should not)

Therefore every section of the Issue Spec uses `## SECTION_NAME` as an explicit marker.
Omitting, merging, or renaming sections is not allowed.

---

## 5.2 Issue Spec Format Definition

The following is the complete format written into the Tracker issue body.
The Clarifier must follow this strictly when producing output; Zpit parses it using `##` markers.

```markdown
## CONTEXT
<!-- Current state of the problem: what the existing behavior is and why it is a problem -->
<!-- Must include: specific file names, method names, behavior descriptions -->

## APPROACH
<!-- Chosen implementation plan: how to do it, and why this approach was selected -->
<!-- If multiple approaches were considered, briefly explain why the others were rejected -->

## ACCEPTANCE_CRITERIA
<!-- Each entry format: AC-N: specific description (vague words like "appropriate" or "reasonable" are not allowed) -->
AC-1: ...
AC-2: ...

## SCOPE
<!-- Format: [modify|create|delete] file-path (reason for change) -->
[modify] src/Services/EtherCatService.cs (primary modification)
[modify] src/Alarms/AlarmManager.cs (add alarm code)

## CONSTRAINTS
<!-- Hard constraints during implementation -->
<!-- If there are none, write "No additional constraints; follow CLAUDE.md" -->

## REFERENCES
<!-- Optional. Related reference material -->

## BASE_BRANCH
<!-- Required. The orchestrator worktree forks from this branch. In 99% of cases identical to PR_TARGET. -->
dev

## PR_TARGET
<!-- Required. The PR merges into this branch. In 99% of cases identical to BASE_BRANCH; the two differ only in the rare asymmetric scenario (fork from a feature branch, PR back to an integration branch). -->
dev

<!-- Legacy issues still using a single `## BRANCH` section are also accepted; the parser handles both forms and logs a deprecation warning at Loop startup (mapping to BASE_BRANCH=PR_TARGET). -->

## TASKS
<!-- Optional. Task breakdown for ordered execution of large issues -->
<!-- Format: T{N}: [P] description [action] file-path (depends: T{M}, T{N}) -->
T1: [P] Add retry backoff to ReconnectAsync [modify] src/Services/EtherCatService.cs (depends: none)
T2: [P] Add alarm code constant [modify] src/Alarms/AlarmManager.cs (depends: none)
T3: Wire alarm trigger into retry flow [modify] src/Services/EtherCatService.cs (depends: T1, T2)

## COORDINATES_WITH
<!-- Optional. Issue numbers of parallel collaborating counterparts -->
#42
#43
```

---

## 5.3 Section Rules Overview

| Section | Required | Consumer | Purpose |
|---------|----------|----------|---------|
| CONTEXT | ✓ | Coding Agent | Understand what the problem is |
| APPROACH | ✓ | Coding Agent | Understand how to solve it |
| ACCEPTANCE_CRITERIA | ✓ | Coding Agent + Reviewer | Understand when the work is done |
| SCOPE | ✓ | Coding Agent + Hook (path-guard) | Constrain the range of changes |
| CONSTRAINTS | ✓ | Coding Agent | Hard limits that must not be violated |
| REFERENCES | Optional | Coding Agent | Reference material |
| BRANCH | Optional | Coding Agent + Reviewer | PR target branch (overrides project default) |
| TASKS | Optional | Coding Agent | Task breakdown and execution order for large issues |
| COORDINATES_WITH | Optional | Coding Agent | Parallel collaborating counterparts (triggers channel coordination protocol) |

**TASKS section format rules:**
- `T{N}:` — task ID (T followed by a number)
- `[P]` — parallel marker. When consecutive tasks share the same dependency set and modify different files, all of them must be marked `[P]`; the engine groups consecutive `[P]` tasks into a single parallel batch
- `[modify|create|delete] path` — file(s) involved (multiple allowed)
- `(depends: T{M}, ...)` — dependency list; `(depends: none)` means no dependencies
- File paths in TASKS are cross-validated against SCOPE

**COORDINATES_WITH section format rules:**
- One `#N` per line, where N is the issue number of the parallel collaborating counterpart
- Non-blocking: the Loop engine does not wait on any entry in COORDINATES_WITH (contrast with DEPENDS_ON's sequential blocking)
- Pure prompt-layer signal: when present, it triggers the Dependency Coordination Protocol (see 12-channel.md)
- Can coexist with DEPENDS_ON — the semantics differ (DEPENDS_ON = sequential blocking, COORDINATES_WITH = parallel coordination)
- Validation: lines that are not in `#N` format produce a warning

**Format enforcement rules:**
- The issue body produced by the Clarifier must include all required sections
- Section headings use `## SECTION_NAME` (uppercase English); renaming or translating them is not allowed
- The Clarifier pushes directly to the Tracker via MCP tools; the user must confirm the content in the terminal before the push
- Zpit's `[s]` status screen fetches issues from the Tracker and can be used to verify the format

---

## 5.4 Issue Spec Validation

Implementation is in `internal/tracker/issuespec.go`.

Validation has two levels:
- **Errors** (hard block): the Loop engine refuses to execute
- **Warnings** (soft notice): displayed in the TUI; the Loop still executes

```go
type ValidationResult struct {
    Errors   []string
    Warnings []string
}

func ValidateIssueSpec(body string) ValidationResult
```

**Error checks:**
- Missing required section (CONTEXT, APPROACH, ACCEPTANCE_CRITERIA, SCOPE, CONSTRAINTS)
- Unresolved `[UNRESOLVED: ...]` marker
- Malformed SCOPE entry (missing `[modify]`/`[create]`/`[delete]` prefix, unclosed parenthesis, invalid action)

**Warning checks:**
- Vague words in AC ("appropriate", "reasonable", "sufficient", "when necessary")
- Gaps in AC numbering (e.g. AC-1, AC-3 but AC-2 missing)
- Files listed in SCOPE not mentioned by any AC
- File paths in TASKS not present in SCOPE
- Lines in COORDINATES_WITH not in `#N` format

**Parse function:**

```go
func ParseIssueSpec(body string) (*IssueSpec, error)
```

Parses `## SECTION_NAME` markers and returns a structured `IssueSpec` (containing Context, Approach, AcceptanceCriteria, Scope, Constraints, References, Branch, Tasks, DependsOn, CoordinatesWith).

---

## 5.5 Coding Agent Prompt Template

When the Loop launches a coding agent, the prompt is assembled by `BuildCodingPrompt()` (`internal/prompt/coding.go`).
Each section of the Issue Spec is injected at a well-defined position; the coding agent does not need to parse it itself.

**Prompt structure:**

1. Language instruction (injected by `locale.ResponseInstruction()`)
2. Issue ID + title
3. **Problem to Solve** ← CONTEXT
4. **Implementation Approach** ← APPROACH
5. **Acceptance Criteria** ← AC list
6. **Allowed File Scope** ← SCOPE (must stop and ask the user if scope is exceeded)
7. **Constraints** ← CONSTRAINTS
8. **References** ← REFERENCES (optional)
9. **Logging Policy** ← generated text from the project's `log_policy`
10. **Task Decomposition** ← TASKS (optional; switches to task-oriented workflow when present)
11. **Your Workflow** — workflow steps (read CLAUDE.md → read tracker.md → read guidelines → implement → self-check → commit → update labels → open PR)
12. **When to Stop and Ask** — situations that require stopping to ask the user
13. **Tracker Operation Notes** — MCP/API operation hints

**Differences when TASKS is present:**
- Additional **Task Decomposition** and **Execution Strategy** sections are injected
- The coding agent acts as an **orchestrator** — it does not implement tasks itself; instead it delegates each task to a `task-runner` subagent (context isolation)
- Sequential tasks (no `[P]`): delegated one by one to `task-runner` subagents via the Agent tool
- Parallel tasks (with `[P]`): dispatched as a "parallel subagent batch" — one `task-runner` subagent per `[P]` task (using the standard Claude Code subagent path + `isolation: "worktree"`, not the Claude Code Agent Team mechanism)
- Each parallel subagent gets its own child worktree (the orchestrator calls the Agent tool with `isolation: "worktree"`, which triggers zpit's `WorktreeCreate` hook to fork a child worktree from the orchestrator's HEAD under `.zpit-children/<slug>`), and commits normally on its own branch. The Agent tool return value only contains `worktreePath` (Claude Code does not propagate `worktreeBranch` — see known-issues §3), so the orchestrator first resolves each subagent's branch name via `git -C <path> rev-parse --abbrev-ref HEAD`, then cherry-picks onto the parent branch. Cleanup is split into two independent Bash calls: `git worktree remove --force <path>` and `git branch -D <branch>` (never chained with `&&`, to prevent a hook blocking one from taking down the other; see known-issues §4). Cherry-pick conflicts (spec bug: two `[P]` tasks writing the same file) are caught immediately by `cherry-pick --abort` and not silently reverted. See `docs/architecture/06-agents.md` §6.3
- Mixed scenarios: dispatched in dependency order — sequential tasks and parallel batches interleave
- Commit format: `[ISSUE-ID] T{N}: {description}`
- After each subagent completes, the orchestrator verifies the commit; retries once on failure; stops and notifies (does not open a PR) if it still fails
- After all tasks are complete, the orchestrator performs a full ACCEPTANCE_CRITERIA self-check before opening the PR

---

## 5.6 Reviewer Acceptance Template

Assembled by `BuildReviewerPrompt()` (`internal/prompt/reviewer.go`).
Supports two modes: initial review and revision review.

**Initial Review (ReviewRound == 0):**

1. Language instruction
2. Issue ID + title
3. **Original Requirements** ← CONTEXT
4. **Expected Approach** ← APPROACH
5. **Acceptance Criteria** ← AC list (PASS / FAIL per item)
6. **Allowed File Scope** ← SCOPE
7. **Constraints** ← CONSTRAINTS
8. **Logging Policy**
9. **Your Review Process** — read CLAUDE.md → read issue/PR comments → `git diff base...HEAD` → verify each AC → check SCOPE violations → verify PR target branch → check CONSTRAINTS → check logging → read code-construction-principles → produce Report
10. **Verdict**: any AC ❌ or SCOPE/CONSTRAINTS violation → NEEDS CHANGES; all ✅ → PASS
11. **Label update**: PASS → remove "review" add "ai-review"; NEEDS CHANGES → remove "review" add "needs-changes"

**Revision Review (ReviewRound > 0):**

Focuses on the delta rather than a full re-review:
1. Read the MUST FIX (🔴) items from the previous review
2. Inspect only the delta from the revision commits
3. Verify item-by-item whether each previous MUST FIX has been addressed
4. Spot-check the full diff to confirm no regressions
5. Produce a Revision Review Report

---

## 5.7 Revision Coding Prompt Template

Assembled by `BuildRevisionPrompt()` (`internal/prompt/revision.go`).
Activated when the reviewer determines NEEDS CHANGES and `max_review_rounds` has not been exceeded.

**Differences from the initial coding prompt:**
- Explicitly marked as a revision round (round N)
- Workflow begins by reading the Review Report on the PR and listing MUST FIX items
- Must stop and ask the user if the reviewer feedback is unclear
- Must also stop if the reviewer feedback appears incorrect — do not follow it blindly
- Commit format: `[ISSUE-ID] fix: {description}`
- Label update: before fixing, remove "needs-changes" add "wip"; after fixing, remove "wip" add "review"
