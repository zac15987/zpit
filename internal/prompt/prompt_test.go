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
		"WebSearch",                                            // tool-first: verify external APIs
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
		"WebSearch",                                               // tool underuse: verify external APIs
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

func testSpecWithTasks() *tracker.IssueSpec {
	spec := testSpec()
	spec.Tasks = []tracker.TaskEntry{
		{ID: "T1", Description: "Add retry logic", Paths: []string{"src/EtherCatService.cs"}, DependsOn: nil},
		{ID: "T2", Description: "Add retry policy", Parallel: true, Paths: []string{"src/RetryPolicy.cs"}, DependsOn: []string{"T1"}},
	}
	return spec
}

func TestBuildCodingPrompt_WithTasks(t *testing.T) {
	p := CodingParams{
		IssueID:    "ASE-47",
		IssueTitle: "EtherCAT reconnect backoff",
		Spec:       testSpecWithTasks(),
		LogPolicy:  "strict",
		BaseBranch: "dev",
	}

	result := BuildCodingPrompt(p)

	mustContain := []string{
		"Execute tasks in order",
		"Commit after each task",
		"[ASE-47] T{N}:",
		"T1:",
		"T2:",
		"re-read modified files",
		"Verify relevant ACs",
		"retry once",
		"stop and post issue comment",
		"do NOT open PR",
	}
	for _, c := range mustContain {
		if !strings.Contains(result, c) {
			t.Errorf("task prompt missing %q", c)
		}
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

	if strings.Contains(result, "Execute tasks in order") {
		t.Error("prompt without tasks should NOT contain 'Execute tasks in order'")
	}
	if strings.Contains(result, "Task Decomposition") {
		t.Error("prompt without tasks should NOT contain 'Task Decomposition'")
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
