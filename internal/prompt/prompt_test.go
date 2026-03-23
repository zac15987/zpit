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
		IssueID:    "ASE-47",
		IssueTitle: "EtherCAT reconnect backoff",
		Spec:       testSpec(),
		LogPolicy:  "strict",
		BaseBranch: "dev",
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
	}

	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("reviewer prompt missing %q", c)
		}
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
