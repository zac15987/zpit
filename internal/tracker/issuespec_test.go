package tracker

import (
	"strings"
	"testing"
)

const fullIssueBody = `## CONTEXT
EtherCatService.ReconnectAsync() 斷線後立即重試且無 backoff。

## APPROACH
在 ReconnectAsync 內部加入 retry loop，採用指數退避策略。

## ACCEPTANCE_CRITERIA
AC-1: retry 間隔按指數退避遞增：1s → 2s → 4s，最多重試 3 次
AC-2: 每次 retry 設定 timeout 為 3 秒
AC-3: 3 次全部失敗後觸發 alarm

## SCOPE
[modify] src/Services/EtherCatService.cs (主要修改：ReconnectAsync 方法)
[create] src/Alarms/AlarmCodes.cs (新增 alarm code)
[delete] src/Legacy/OldReconnect.cs (移除舊實作)

## CONSTRAINTS
不引入新的 NuGet 套件

## BRANCH
dev

## REFERENCES
[官方文件] https://example.com — EtherCAT 文件
`

func TestValidateIssueSpec_AllPresent(t *testing.T) {
	result := ValidateIssueSpec(fullIssueBody)
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
}

func TestValidateIssueSpec_MissingSections(t *testing.T) {
	body := "## CONTEXT\nSome context\n\n## APPROACH\nSome approach\n"
	result := ValidateIssueSpec(body)
	if len(result.Errors) != 3 {
		t.Fatalf("expected 3 errors for missing sections, got %d: %v", len(result.Errors), result.Errors)
	}
	for _, err := range result.Errors {
		if !strings.Contains(err, "missing required section") {
			t.Errorf("unexpected error message: %q", err)
		}
	}
}

func TestValidateIssueSpec_EmptyBody(t *testing.T) {
	result := ValidateIssueSpec("")
	if len(result.Errors) != 5 {
		t.Errorf("expected 5 errors for empty body, got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestParseIssueSpec_FullBody(t *testing.T) {
	spec, err := ParseIssueSpec(fullIssueBody)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}

	if !strings.Contains(spec.Context, "EtherCatService") {
		t.Errorf("Context missing expected content: %q", spec.Context)
	}
	if !strings.Contains(spec.Approach, "retry loop") {
		t.Errorf("Approach missing expected content: %q", spec.Approach)
	}
	if !strings.Contains(spec.Constraints, "NuGet") {
		t.Errorf("Constraints missing expected content: %q", spec.Constraints)
	}
	if !strings.Contains(spec.References, "example.com") {
		t.Errorf("References missing expected content: %q", spec.References)
	}
}

func TestParseIssueSpec_ACParsing(t *testing.T) {
	spec, err := ParseIssueSpec(fullIssueBody)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if len(spec.AcceptanceCriteria) != 3 {
		t.Fatalf("expected 3 AC entries, got %d", len(spec.AcceptanceCriteria))
	}
	if !strings.HasPrefix(spec.AcceptanceCriteria[0], "AC-1:") {
		t.Errorf("AC[0] = %q", spec.AcceptanceCriteria[0])
	}
	if !strings.HasPrefix(spec.AcceptanceCriteria[2], "AC-3:") {
		t.Errorf("AC[2] = %q", spec.AcceptanceCriteria[2])
	}
}

func TestParseIssueSpec_ScopeParsing(t *testing.T) {
	spec, err := ParseIssueSpec(fullIssueBody)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if len(spec.Scope) != 3 {
		t.Fatalf("expected 3 scope entries, got %d", len(spec.Scope))
	}

	tests := []struct {
		action, path, reason string
	}{
		{"modify", "src/Services/EtherCatService.cs", "主要修改：ReconnectAsync 方法"},
		{"create", "src/Alarms/AlarmCodes.cs", "新增 alarm code"},
		{"delete", "src/Legacy/OldReconnect.cs", "移除舊實作"},
	}

	for i, tt := range tests {
		s := spec.Scope[i]
		if s.Action != tt.action {
			t.Errorf("Scope[%d].Action = %q, want %q", i, s.Action, tt.action)
		}
		if s.Path != tt.path {
			t.Errorf("Scope[%d].Path = %q, want %q", i, s.Path, tt.path)
		}
		if s.Reason != tt.reason {
			t.Errorf("Scope[%d].Reason = %q, want %q", i, s.Reason, tt.reason)
		}
	}
}

func TestParseIssueSpec_OptionalReferences(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: test\n\n## SCOPE\n[modify] f.go (reason)\n\n## CONSTRAINTS\nnone\n"
	spec, err := ParseIssueSpec(body)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if spec.References != "" {
		t.Errorf("expected empty references, got %q", spec.References)
	}
}

func TestParseIssueSpec_ExtraWhitespace(t *testing.T) {
	body := "## CONTEXT\n\n  Some context with spaces  \n\n\n## APPROACH\n\napproach\n\n## ACCEPTANCE_CRITERIA\n\nAC-1: first\n\nAC-2: second\n\n## SCOPE\n\n[modify] file.go (reason)\n\n## CONSTRAINTS\nnone\n"
	spec, err := ParseIssueSpec(body)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if !strings.Contains(spec.Context, "Some context") {
		t.Errorf("Context = %q", spec.Context)
	}
	if len(spec.AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 AC, got %d", len(spec.AcceptanceCriteria))
	}
}

func TestParseScopeEntry_AllActions(t *testing.T) {
	tests := []struct {
		line   string
		action string
		path   string
		reason string
	}{
		{"[modify] src/main.go (entry point)", "modify", "src/main.go", "entry point"},
		{"[create] src/new.go (new file)", "create", "src/new.go", "new file"},
		{"[delete] src/old.go (unused)", "delete", "src/old.go", "unused"},
	}
	for _, tt := range tests {
		entry := parseScopeEntry(tt.line)
		if entry.Action != tt.action {
			t.Errorf("Action = %q, want %q for %q", entry.Action, tt.action, tt.line)
		}
		if entry.Path != tt.path {
			t.Errorf("Path = %q, want %q for %q", entry.Path, tt.path, tt.line)
		}
		if entry.Reason != tt.reason {
			t.Errorf("Reason = %q, want %q for %q", entry.Reason, tt.reason, tt.line)
		}
	}
}

func TestParseScopeEntry_NoReason(t *testing.T) {
	entry := parseScopeEntry("[modify] src/main.go")
	if entry.Action != "modify" {
		t.Errorf("Action = %q", entry.Action)
	}
	if entry.Path != "src/main.go" {
		t.Errorf("Path = %q", entry.Path)
	}
	if entry.Reason != "" {
		t.Errorf("Reason = %q, want empty", entry.Reason)
	}
}

func TestParseScopeEntry_MalformedLine(t *testing.T) {
	entry := parseScopeEntry("not a scope line")
	if entry.Action != "" {
		t.Errorf("expected empty action for malformed line, got %q", entry.Action)
	}
}

func TestParseIssueSpec_BranchSection(t *testing.T) {
	spec, err := ParseIssueSpec(fullIssueBody)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if spec.Branch != "dev" {
		t.Errorf("Branch = %q, want %q", spec.Branch, "dev")
	}
}

func TestParseIssueSpec_BranchEmpty(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: test\n\n## SCOPE\n[modify] f.go (reason)\n\n## CONSTRAINTS\nnone\n"
	spec, err := ParseIssueSpec(body)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if spec.Branch != "" {
		t.Errorf("expected empty branch when ## BRANCH absent, got %q", spec.Branch)
	}
}

func TestParseIssueSpec_BranchCustom(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: test\n\n## SCOPE\n[modify] f.go (reason)\n\n## CONSTRAINTS\nnone\n\n## BRANCH\nmain\n"
	spec, err := ParseIssueSpec(body)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if spec.Branch != "main" {
		t.Errorf("Branch = %q, want %q", spec.Branch, "main")
	}
}

// --- New validation tests (AC-2 through AC-8, AC-13) ---

func TestValidateIssueSpec_UnresolvedPresent(t *testing.T) {
	body := "## CONTEXT\nSome context\n\n## APPROACH\n[UNRESOLVED: should we use polling or websocket?]\n\n## ACCEPTANCE_CRITERIA\nAC-1: test passes\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "UNRESOLVED") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an error containing 'UNRESOLVED', got errors: %v", result.Errors)
	}
}

func TestValidateIssueSpec_UnresolvedAbsent(t *testing.T) {
	body := "## CONTEXT\nSome context\n\n## APPROACH\nUse polling because user confirmed.\n\n## ACCEPTANCE_CRITERIA\nAC-1: test passes\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	for _, e := range result.Errors {
		if strings.Contains(e, "UNRESOLVED") {
			t.Errorf("unexpected UNRESOLVED error: %q", e)
		}
	}
}

func TestValidateIssueSpec_VagueWord_Appropriate(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: use Appropriate error handling\nAC-2: timeout of 3s\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "AC-1") && strings.Contains(w, "appropriate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for AC-1 containing 'appropriate', got warnings: %v", result.Warnings)
	}
}

func TestValidateIssueSpec_VagueWord_Reasonable(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: set a reasonable timeout\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "AC-1") && strings.Contains(w, "reasonable") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for AC-1 containing 'reasonable', got warnings: %v", result.Warnings)
	}
}

func TestValidateIssueSpec_VagueWord_Sufficient(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: add sufficient logging\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "AC-1") && strings.Contains(w, "sufficient") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for AC-1 containing 'sufficient', got warnings: %v", result.Warnings)
	}
}

func TestValidateIssueSpec_VagueWord_WhenNecessary(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: retry when necessary\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "AC-1") && strings.Contains(w, "when necessary") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for AC-1 containing 'when necessary', got warnings: %v", result.Warnings)
	}
}

func TestValidateIssueSpec_ScopeFormatError(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: test model.go changes\n\n## SCOPE\nmodify internal/tui/model.go\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "SCOPE format") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE format error, got errors: %v", result.Errors)
	}
}

func TestValidateIssueSpec_ScopeACCoverageWarning(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: the handler returns 200\n\n## SCOPE\n[modify] internal/tui/model.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "model.go") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about uncovered SCOPE entry model.go, got warnings: %v", result.Warnings)
	}
}

func TestValidateIssueSpec_ACNumberingGap(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: first\nAC-3: third\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "AC-2") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about AC-2 gap, got warnings: %v", result.Warnings)
	}
}

func TestValidateIssueSpec_CleanBody_ZeroErrorsAndWarnings(t *testing.T) {
	body := "## CONTEXT\nSome context\n\n## APPROACH\nUse polling.\n\n## ACCEPTANCE_CRITERIA\nAC-1: retry interval in EtherCatService.cs increases: 1s, 2s, 4s with max 3 retries\nAC-2: each retry has a 3-second timeout\nAC-3: after 3 failures trigger alarm via AlarmCodes.cs\n\n## SCOPE\n[modify] src/Services/EtherCatService.cs (add retry logic)\n[create] src/Alarms/AlarmCodes.cs (new alarm codes)\n\n## CONSTRAINTS\nNo new dependencies\n"
	result := ValidateIssueSpec(body)
	if len(result.Errors) != 0 {
		t.Errorf("expected zero errors, got %v", result.Errors)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected zero warnings, got %v", result.Warnings)
	}
}

func TestValidateIssueSpec_MultipleUnresolved(t *testing.T) {
	body := "## CONTEXT\n[UNRESOLVED: what version?]\n\n## APPROACH\n[UNRESOLVED: which approach?]\n\n## ACCEPTANCE_CRITERIA\nAC-1: test\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	unresolvedCount := 0
	for _, e := range result.Errors {
		if strings.Contains(e, "UNRESOLVED") {
			unresolvedCount++
		}
	}
	if unresolvedCount != 2 {
		t.Errorf("expected 2 UNRESOLVED errors, got %d: %v", unresolvedCount, result.Errors)
	}
}

func TestValidateIssueSpec_InvalidScopeAction(t *testing.T) {
	body := "## CONTEXT\nctx\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: test\n\n## SCOPE\n[update] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "SCOPE format") && strings.Contains(e, "update") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE format error for invalid action [update], got errors: %v", result.Errors)
	}
}

func TestValidateIssueSpec_NoFalsePositiveOnUnresolvedIssue(t *testing.T) {
	// [UNRESOLVED_ISSUE] should NOT trigger — requires "[UNRESOLVED: " with colon and space
	body := "## CONTEXT\n[UNRESOLVED_ISSUE] is not a marker\n\n## APPROACH\napproach\n\n## ACCEPTANCE_CRITERIA\nAC-1: test\n\n## SCOPE\n[modify] main.go (reason)\n\n## CONSTRAINTS\nnone\n"
	result := ValidateIssueSpec(body)
	for _, e := range result.Errors {
		if strings.Contains(e, "UNRESOLVED") {
			t.Errorf("false positive: [UNRESOLVED_ISSUE] should not trigger UNRESOLVED error, got: %q", e)
		}
	}
}
