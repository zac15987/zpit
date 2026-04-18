package prompt

import (
	"strings"
	"testing"

	"github.com/zac15987/zpit/internal/tracker"
)

func testSpec() *tracker.IssueSpec {
	return &tracker.IssueSpec{
		Context:  "EtherCAT reconnect 沒有 backoff",
		Approach: "在 ReconnectAsync 加入 exponential backoff",
		AcceptanceCriteria: []string{
			"AC-1: 第一次重試間隔 1 秒",
			"AC-2: 最大重試間隔 30 秒",
			"AC-3: 重試次數上限 10 次",
		},
		Scope: []tracker.ScopeEntry{
			{Action: "modify", Path: "src/EtherCatService.cs", Reason: "加入 retry logic"},
			{Action: "create", Path: "src/RetryPolicy.cs", Reason: "新 retry 策略類別"},
		},
		Constraints: "不可改動 EtherCAT 初始化流程",
		References:  "src/PlcService.cs 有類似 retry 實作可參考",
	}
}

func TestBuildCodingPrompt_AllSections(t *testing.T) {
	p := CodingParams{
		IssueID:    "ASE-47",
		IssueTitle: "EtherCAT reconnect backoff",
		Spec:       testSpec(),
		LogPolicy:  "strict",
		BaseBranch: "dev",
	}

	result := BuildCodingPrompt(p)

	checks := []string{
		"ASE-47",
		"EtherCAT reconnect backoff",
		"EtherCAT reconnect 沒有 backoff",                      // CONTEXT
		"ReconnectAsync",                                       // APPROACH
		"AC-1:",                                                // AC
		"AC-3:",                                                // AC
		"[modify] src/EtherCatService.cs",                      // SCOPE
		"[create] src/RetryPolicy.cs",                          // SCOPE
		"不可改動",                                                // CONSTRAINTS
		"PlcService.cs",                                        // REFERENCES
		"All Service methods must have entry/exit logs",        // log policy strict
		"Commit message format: [ASE-47]",                      // workflow
		"Do not touch files outside this scope",                // scope warning
		"When to Stop and Ask the User",                        // stop conditions
		"must",                                                 // PR target branch
		"--base dev",                                           // PR target branch flag
		"WebFetch",                                             // research: fetch reference URLs
		"WebSearch",                                            // research: verify external APIs
		"re-read each modified file",                           // stale context: verify before commit
	}

	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("prompt missing %q", c)
		}
	}
}

func TestBuildCodingPrompt_NoReferences(t *testing.T) {
	spec := testSpec()
	spec.References = ""

	result := BuildCodingPrompt(CodingParams{
		IssueID:    "ASE-48",
		IssueTitle: "test",
		Spec:       spec,
		LogPolicy:  "minimal",
		BaseBranch: "dev",
	})

	if strings.Contains(result, "## References") {
		t.Error("should not contain references section when empty")
	}
}

func TestBuildCodingPrompt_LogPolicies(t *testing.T) {
	tests := []struct {
		policy string
		expect string
	}{
		{"strict", "All Service methods must have entry/exit logs"},
		{"standard", "Service methods must have entry/exit logs"},
		{"minimal", "Only log errors and critical operations"},
	}

	for _, tt := range tests {
		result := BuildCodingPrompt(CodingParams{
			IssueID:    "TEST-1",
			IssueTitle: "test",
			Spec:       testSpec(),
			LogPolicy:  tt.policy,
			BaseBranch: "dev",
		})
		if !strings.Contains(result, tt.expect) {
			t.Errorf("LogPolicy %q: missing %q", tt.policy, tt.expect)
		}
	}
}

func TestBuildCodingPrompt_BaseBranch(t *testing.T) {
	result := BuildCodingPrompt(CodingParams{
		IssueID:    "TEST-1",
		IssueTitle: "test",
		Spec:       testSpec(),
		LogPolicy:  "minimal",
		BaseBranch: "main",
	})

	if !strings.Contains(result, "`main`") {
		t.Error("coding prompt should contain target branch name")
	}
	if !strings.Contains(result, "--base main") {
		t.Error("coding prompt should contain --base flag with branch")
	}
}

func TestBuildReviewerPrompt_AllSections(t *testing.T) {
	p := ReviewerParams{
		IssueID:     "ASE-47",
		IssueTitle:  "EtherCAT reconnect backoff",
		Spec:        testSpec(),
		LogPolicy:   "strict",
		BaseBranch:  "dev",
		ReviewRound: 0,
	}

	result := BuildReviewerPrompt(p)

	checks := []string{
		"ASE-47",
		"review",
		"Original Requirements",
		"Expected Approach",
		"Acceptance Criteria",
		"AC-1:",
		"[modify] src/EtherCatService.cs",
		"Constraints",
		"git diff dev...HEAD",                            // uses base branch
		"target branch",                                  // branch verification step
		"code-construction-principles.md",                // quality check
		"All Service methods must have entry/exit logs",  // log policy
		"issue comments",                                 // read comments step
		"PR comments",                                    // read comments step
		"Re-read ACCEPTANCE_CRITERIA",                    // stale context: re-verify before verdicts
	}

	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("reviewer prompt missing %q", c)
		}
	}
}

func TestBuildReviewerPrompt_RevisionReview(t *testing.T) {
	p := ReviewerParams{
		IssueID:     "ASE-47",
		IssueTitle:  "EtherCAT reconnect backoff",
		Spec:        testSpec(),
		LogPolicy:   "strict",
		BaseBranch:  "dev",
		ReviewRound: 1,
	}

	result := BuildReviewerPrompt(p)

	mustContain := []string{
		"REVISION REVIEW",                                // revision indicator
		"MUST FIX",                                       // focus on previous must fix items
		"git log --oneline dev...HEAD",                   // identify revision commits
		"Original Requirements",                          // still has AC sections for reference
		"AC-1:",                                          // AC items present
		"PR comments",                                    // reads PR comments for previous review
		"round 1",                                        // round number
		"Re-read ACCEPTANCE_CRITERIA",                    // stale context: re-verify before regression check
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("revision reviewer prompt missing %q", c)
		}
	}

	// Revision review should NOT use git diff base...HEAD as the primary diff step
	// (it uses git log + git show on revision commits instead)
	if strings.Contains(result, "Use git diff dev...HEAD to view all changes") {
		t.Error("revision reviewer prompt should not use full diff as primary review step")
	}
}

func TestBuildReviewerPrompt_RevisionRound2(t *testing.T) {
	result := BuildReviewerPrompt(ReviewerParams{
		IssueID:     "ASE-47",
		IssueTitle:  "EtherCAT reconnect backoff",
		Spec:        testSpec(),
		LogPolicy:   "strict",
		BaseBranch:  "dev",
		ReviewRound: 2,
	})

	checks := []string{
		"REVISION REVIEW",
		"round 2",
		"MUST FIX",
	}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("revision round 2 prompt missing %q", c)
		}
	}
	if strings.Contains(result, "round 1") {
		t.Error("revision round 2 prompt should not contain 'round 1'")
	}
}

func TestBuildRevisionPrompt_AllSections(t *testing.T) {
	p := RevisionParams{
		IssueID:     "ASE-47",
		IssueTitle:  "EtherCAT reconnect backoff",
		Spec:        testSpec(),
		LogPolicy:   "strict",
		BaseBranch:  "dev",
		ReviewRound: 1,
	}

	result := BuildRevisionPrompt(p)

	checks := []string{
		"ASE-47",
		"revision round 1",
		"Original Requirements",
		"Expected Approach",
		"Acceptance Criteria",
		"AC-1:",
		"[modify] src/EtherCatService.cs",
		"Constraints",
		"Re-read the reviewer",                                    // stale context: understand root concern
		"re-read each modified file",                              // stale context: verify after fix
		"WebFetch",                                                // research: fetch reference URLs
		"WebSearch",                                               // research: verify external APIs
		"vague or lacks a clear direction",                        // sycophancy: ask for clarification
		"compliance without agreement is not acceptable",          // sycophancy: challenge incorrect feedback
		"degrade code quality",                                    // sycophancy: flag conflicts
		"When to Stop and Ask the User",
		"Commit message format: [ASE-47] fix:",
	}

	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("revision prompt missing %q", c)
		}
	}
}

func testSpecWithSequentialTasks() *tracker.IssueSpec {
	spec := testSpec()
	spec.Tasks = []tracker.TaskEntry{
		{ID: "T1", Description: "Add retry logic", Paths: []string{"src/EtherCatService.cs"}, DependsOn: nil},
		{ID: "T2", Description: "Add retry policy", Paths: []string{"src/RetryPolicy.cs"}, DependsOn: []string{"T1"}},
	}
	return spec
}

func TestBuildCodingPrompt_WithTasks(t *testing.T) {
	p := CodingParams{
		IssueID:    "ASE-47",
		IssueTitle: "EtherCAT reconnect backoff",
		Spec:       testSpecWithSequentialTasks(),
		LogPolicy:  "strict",
		BaseBranch: "dev",
	}

	result := BuildCodingPrompt(p)

	// AC-1: pure sequential tasks — prompt must contain subagent delegation, no Agent Team
	mustContain := []string{
		"Task Decomposition",
		"Commit after each task",
		"[ASE-47] T{N}:",
		"T1:",
		"T2:",
		"Execution Strategy",
		"orchestrator",
		"task-runner",
		"subagent_type",
		"Subagent Delegation",
		"Task Execution Order",
		"sequential",
		"WebFetch",
		"self-check against each ACCEPTANCE_CRITERIA",
		"stop and post issue comment",
		"do NOT open PR",
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("task prompt missing %q", c)
		}
	}

	// AC-1: pure sequential tasks must NOT contain Agent Team instructions
	// or any of the Parallel Commit Protocol strings (sequential owns the index).
	mustNotContain := []string{
		"Agent Team Delegation",
		"Parallel group",
		"Parallel Commit Protocol",
		"parallel_task_id",
		"GIT_INDEX_FILE",
		".git/zpit-commit.lock",
	}
	for _, c := range mustNotContain {
		if strings.Contains(result, c) {
			t.Errorf("sequential-only task prompt should NOT contain %q", c)
		}
	}
}

func TestBuildCodingPrompt_WithParallelTasks(t *testing.T) {
	// AC-2 + AC-3: mixed sequential and parallel tasks
	spec := testSpec()
	spec.Tasks = []tracker.TaskEntry{
		{ID: "T1", Description: "Add base struct", Paths: []string{"src/EtherCatService.cs"}, DependsOn: nil},
		{ID: "T2", Description: "Add retry logic", Parallel: true, Paths: []string{"src/RetryPolicy.cs"}, DependsOn: []string{"T1"}},
		{ID: "T3", Description: "Add retry tests", Parallel: true, Paths: []string{"src/RetryPolicy_test.cs"}, DependsOn: []string{"T1"}},
		{ID: "T4", Description: "Wire up retry", Paths: []string{"src/EtherCatService.cs"}, DependsOn: []string{"T2", "T3"}},
	}

	p := CodingParams{
		IssueID:    "ASE-47",
		IssueTitle: "EtherCAT reconnect backoff",
		Spec:       spec,
		LogPolicy:  "strict",
		BaseBranch: "dev",
	}

	result := BuildCodingPrompt(p)

	// Must contain both subagent and Agent Team delegation sections,
	// plus the Parallel Commit Protocol hand-off that protects against index/ref races.
	mustContain := []string{
		"Task Decomposition",
		"Execution Strategy",
		"Subagent Delegation",
		"Agent Team Delegation",
		"task-runner",
		"subagent_type",
		"Task Execution Order",
		"T1",
		"T2",
		"T3",
		"T4",
		"Parallel group",
		"sequential",
		"teammate",
		"self-check against each ACCEPTANCE_CRITERIA",
		"Parallel Commit Protocol",
		"parallel_task_id",
		"GIT_INDEX_FILE",
		".git/zpit-commit.lock",
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("parallel task prompt missing %q", c)
		}
	}

	// Verify the parallel group contains T2, T3
	if !strings.Contains(result, "Parallel group [T2, T3]") {
		t.Error("parallel task prompt should group T2 and T3 together")
	}
}

func TestBuildCodingPrompt_WithoutTasks_NoTaskWorkflow(t *testing.T) {
	p := CodingParams{
		IssueID:    "ASE-48",
		IssueTitle: "simple change",
		Spec:       testSpec(), // no Tasks
		LogPolicy:  "minimal",
		BaseBranch: "dev",
	}

	result := BuildCodingPrompt(p)

	// AC-6: no tasks — must NOT contain any delegation or task workflow
	mustNotContain := []string{
		"Task Decomposition",
		"Execution Strategy",
		"Subagent Delegation",
		"Agent Team Delegation",
		"task-runner",
		"subagent_type",
	}
	for _, c := range mustNotContain {
		if strings.Contains(result, c) {
			t.Errorf("prompt without tasks should NOT contain %q", c)
		}
	}
}

func TestBuildCodingPrompt_ChannelEnabled(t *testing.T) {
	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           testSpec(),
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: true,
	}

	result := BuildCodingPrompt(p)

	checks := []string{
		"Cross-Agent Communication",
		"publish_artifact",
		"list_artifacts",
		"send_message",
		"channel notification",
		"shared broker",
	}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("channel-enabled prompt missing %q", c)
		}
	}
}

func TestBuildCodingPrompt_ChannelDisabled(t *testing.T) {
	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           testSpec(),
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: false,
	}

	result := BuildCodingPrompt(p)

	if strings.Contains(result, "Cross-Agent Communication") {
		t.Error("channel-disabled prompt should NOT contain cross-agent communication section")
	}
	if strings.Contains(result, "publish_artifact") {
		t.Error("channel-disabled prompt should NOT contain publish_artifact")
	}
}

func TestBuildReviewerPrompt_BaseBranch(t *testing.T) {
	result := BuildReviewerPrompt(ReviewerParams{
		IssueID:    "TEST-1",
		IssueTitle: "test",
		Spec:       testSpec(),
		LogPolicy:  "minimal",
		BaseBranch: "main",
	})

	if !strings.Contains(result, "git diff main...HEAD") {
		t.Error("reviewer prompt should use custom base branch")
	}
}

func testSpecWithCoordinates(ids ...string) *tracker.IssueSpec {
	spec := testSpec()
	spec.CoordinatesWith = ids
	return spec
}

func TestBuildCodingPrompt_CoordinatesWith_Protocol(t *testing.T) {
	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           testSpecWithCoordinates("42", "43"),
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: true,
	}

	result := BuildCodingPrompt(p)

	mustContain := []string{
		"Dependency Coordination Protocol",
		"#42",
		"#43",
		"CHANNEL_ASSUMPTION",
		"pending artifact from #",
		"Coordination Review Gate",
		"list_artifacts",
		"send_message",
		"3 cumulative attempts",
		"Cross-Agent Communication",
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("CoordinatesWith protocol prompt missing %q", c)
		}
	}
}

func TestBuildCodingPrompt_CoordinatesWith_ReviewGate(t *testing.T) {
	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           testSpecWithCoordinates("42"),
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: true,
	}

	result := BuildCodingPrompt(p)

	mustContain := []string{
		"Coordination Review Gate",
		"[CHANNEL_ASSUMPTION]",
		"Do NOT add",
		"issue comment",
		"3 cumulative attempts",
		"Wait for the user",
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("CoordinatesWith review gate prompt missing %q", c)
		}
	}
}

func TestBuildCodingPrompt_ChannelEnabled_NoCoordinatesWith(t *testing.T) {
	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           testSpec(), // CoordinatesWith is nil
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: true,
	}

	result := BuildCodingPrompt(p)

	mustContain := []string{
		"Cross-Agent Communication",
		"publish_artifact",
		"list_artifacts",
		"send_message",
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("channel-enabled no-coordinates prompt missing %q", c)
		}
	}

	mustNotContain := []string{
		"Dependency Coordination Protocol",
		"Coordination Review Gate",
		"CHANNEL_ASSUMPTION",
	}
	for _, c := range mustNotContain {
		if strings.Contains(result, c) {
			t.Errorf("channel-enabled no-coordinates prompt should NOT contain %q", c)
		}
	}
}

func TestBuildCodingPrompt_ChannelDisabled_WithCoordinatesWith(t *testing.T) {
	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           testSpecWithCoordinates("42", "43"),
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: false,
	}

	result := BuildCodingPrompt(p)

	mustNotContain := []string{
		"Cross-Agent Communication",
		"Dependency Coordination Protocol",
		"Coordination Review Gate",
		"publish_artifact",
		"CHANNEL_ASSUMPTION",
	}
	for _, c := range mustNotContain {
		if strings.Contains(result, c) {
			t.Errorf("channel-disabled prompt should NOT contain %q", c)
		}
	}
}

func TestBuildCodingPrompt_CoordinatesWith_TaskWorkflow(t *testing.T) {
	spec := testSpecWithCoordinates("42")
	spec.Tasks = []tracker.TaskEntry{
		{ID: "T1", Description: "Add retry logic", Paths: []string{"src/EtherCatService.cs"}, DependsOn: nil},
		{ID: "T2", Description: "Add retry policy", Paths: []string{"src/RetryPolicy.cs"}, DependsOn: []string{"T1"}},
	}

	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           spec,
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: true,
	}

	result := BuildCodingPrompt(p)

	mustContain := []string{
		"Dependency Coordination Protocol",
		"Coordination Review Gate",
		"Task Decomposition",
		"CHANNEL_ASSUMPTION",
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("CoordinatesWith task workflow prompt missing %q", c)
		}
	}
}

func TestBuildCodingPrompt_CoordinatesWith_StandardWorkflow_ReviewGate(t *testing.T) {
	p := CodingParams{
		IssueID:        "TEST-1",
		IssueTitle:     "test",
		Spec:           testSpecWithCoordinates("42"),
		LogPolicy:      "minimal",
		BaseBranch:     "dev",
		ChannelEnabled: true,
	}

	result := BuildCodingPrompt(p)

	// Standard workflow (no tasks) should also include the review gate
	if !strings.Contains(result, "Coordination Review Gate") {
		t.Error("standard workflow with CoordinatesWith should contain Coordination Review Gate")
	}
}
