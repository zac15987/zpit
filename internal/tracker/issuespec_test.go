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

const fullIssueBodyWithTasks = `## CONTEXT
Large implementation requiring task decomposition.

## APPROACH
Implement in ordered tasks for reliability.

## ACCEPTANCE_CRITERIA
AC-1: TaskEntry struct exists in issuespec.go
AC-2: IssueSpec has Tasks field
AC-3: coding.go includes task workflow

## SCOPE
[modify] internal/tracker/issuespec.go (add TaskEntry struct)
[modify] internal/tracker/issuespec_test.go (add tests)
[modify] internal/prompt/coding.go (task workflow)

## CONSTRAINTS
No new libraries

## TASKS
T1: Add TaskEntry struct and parsing [modify] internal/tracker/issuespec.go (depends: none)
T2: [P] Add tests for task parsing [modify] internal/tracker/issuespec_test.go (depends: T1)
T3: Update coding prompt with task workflow [modify] internal/prompt/coding.go (depends: T1, T2)
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

// --- TASKS parsing tests ---

func TestParseIssueSpec_TasksParsing_FullBody(t *testing.T) {
	spec, err := ParseIssueSpec(fullIssueBodyWithTasks)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if len(spec.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(spec.Tasks))
	}

	// T1: sequential, one path, depends: none
	if spec.Tasks[0].ID != "T1" {
		t.Errorf("Tasks[0].ID = %q, want T1", spec.Tasks[0].ID)
	}
	if spec.Tasks[0].Parallel {
		t.Error("Tasks[0].Parallel should be false")
	}
	if len(spec.Tasks[0].Paths) != 1 || spec.Tasks[0].Paths[0] != "internal/tracker/issuespec.go" {
		t.Errorf("Tasks[0].Paths = %v, want [internal/tracker/issuespec.go]", spec.Tasks[0].Paths)
	}
	if len(spec.Tasks[0].DependsOn) != 0 {
		t.Errorf("Tasks[0].DependsOn = %v, want empty", spec.Tasks[0].DependsOn)
	}

	// T2: parallel, one path, depends: T1
	if spec.Tasks[1].ID != "T2" {
		t.Errorf("Tasks[1].ID = %q, want T2", spec.Tasks[1].ID)
	}
	if !spec.Tasks[1].Parallel {
		t.Error("Tasks[1].Parallel should be true")
	}

	// T3: sequential, one path, depends: T1, T2
	if spec.Tasks[2].ID != "T3" {
		t.Errorf("Tasks[2].ID = %q, want T3", spec.Tasks[2].ID)
	}
}

func TestParseIssueSpec_TasksAbsent_Nil(t *testing.T) {
	spec, err := ParseIssueSpec(fullIssueBody)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if spec.Tasks != nil {
		t.Errorf("expected nil Tasks when ## TASKS absent, got %v", spec.Tasks)
	}
}

func TestParseTaskEntry_ParallelFlag(t *testing.T) {
	line := "T2: [P] Update prompt generation [modify] coding.go (depends: T1)"
	entry, ok := parseTaskEntry(line)
	if !ok {
		t.Fatal("parseTaskEntry returned false for valid line")
	}
	if entry.ID != "T2" {
		t.Errorf("ID = %q, want T2", entry.ID)
	}
	if !entry.Parallel {
		t.Error("Parallel should be true")
	}
	if len(entry.Paths) != 1 || entry.Paths[0] != "coding.go" {
		t.Errorf("Paths = %v, want [coding.go]", entry.Paths)
	}
	if len(entry.DependsOn) != 1 || entry.DependsOn[0] != "T1" {
		t.Errorf("DependsOn = %v, want [T1]", entry.DependsOn)
	}
}

func TestParseTaskEntry_MultipleDependencies(t *testing.T) {
	line := "T3: Integrate changes [modify] main.go (depends: T1, T2)"
	entry, ok := parseTaskEntry(line)
	if !ok {
		t.Fatal("parseTaskEntry returned false")
	}
	if entry.ID != "T3" {
		t.Errorf("ID = %q, want T3", entry.ID)
	}
	if len(entry.DependsOn) != 2 {
		t.Fatalf("DependsOn length = %d, want 2", len(entry.DependsOn))
	}
	if entry.DependsOn[0] != "T1" || entry.DependsOn[1] != "T2" {
		t.Errorf("DependsOn = %v, want [T1 T2]", entry.DependsOn)
	}
}

func TestParseTaskEntry_DependsNone(t *testing.T) {
	line := "T1: Initial setup [create] newfile.go (depends: none)"
	entry, ok := parseTaskEntry(line)
	if !ok {
		t.Fatal("parseTaskEntry returned false")
	}
	if len(entry.DependsOn) != 0 {
		t.Errorf("DependsOn = %v, want empty slice (not containing 'none')", entry.DependsOn)
	}
}

func TestParseTaskEntry_MultipleFileActions(t *testing.T) {
	line := "T2: [P] Update both files [create] x.go [modify] y.go (depends: T1)"
	entry, ok := parseTaskEntry(line)
	if !ok {
		t.Fatal("parseTaskEntry returned false")
	}
	if len(entry.Paths) != 2 {
		t.Fatalf("Paths length = %d, want 2", len(entry.Paths))
	}
	if entry.Paths[0] != "x.go" || entry.Paths[1] != "y.go" {
		t.Errorf("Paths = %v, want [x.go y.go]", entry.Paths)
	}
	if !entry.Parallel {
		t.Error("Parallel should be true")
	}
	if len(entry.DependsOn) != 1 || entry.DependsOn[0] != "T1" {
		t.Errorf("DependsOn = %v, want [T1]", entry.DependsOn)
	}
}

func TestValidateIssueSpec_TasksScopeCrossValidation(t *testing.T) {
	body := `## CONTEXT
ctx

## APPROACH
approach

## ACCEPTANCE_CRITERIA
AC-1: test issuespec.go changes

## SCOPE
[modify] internal/tracker/issuespec.go (reason)

## CONSTRAINTS
none

## TASKS
T1: Update spec [modify] internal/tracker/issuespec.go (depends: none)
T2: Update unknown file [modify] unknown/file.go (depends: T1)
`
	result := ValidateIssueSpec(body)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "T2") && strings.Contains(w, "unknown/file.go") && strings.Contains(w, "not found in SCOPE") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about T2 referencing unknown/file.go not in SCOPE, got warnings: %v", result.Warnings)
	}

	// T1's path IS in SCOPE — should NOT produce a warning for it
	for _, w := range result.Warnings {
		if strings.Contains(w, "T1") && strings.Contains(w, "issuespec.go") && strings.Contains(w, "not found in SCOPE") {
			t.Errorf("unexpected warning for T1's valid SCOPE path: %q", w)
		}
	}
}

// --- DEPENDS_ON parsing tests ---

func TestParseIssueSpec_DependsOn_Basic(t *testing.T) {
	body := `## CONTEXT
ctx

## APPROACH
approach

## ACCEPTANCE_CRITERIA
AC-1: test

## SCOPE
[modify] main.go (reason)

## CONSTRAINTS
none

## DEPENDS_ON
#10
#42
`
	spec, err := ParseIssueSpec(body)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if len(spec.DependsOn) != 2 {
		t.Fatalf("DependsOn length = %d, want 2", len(spec.DependsOn))
	}
	if spec.DependsOn[0] != "10" {
		t.Errorf("DependsOn[0] = %q, want %q", spec.DependsOn[0], "10")
	}
	if spec.DependsOn[1] != "42" {
		t.Errorf("DependsOn[1] = %q, want %q", spec.DependsOn[1], "42")
	}
}

func TestParseIssueSpec_DependsOn_EmptyLines(t *testing.T) {
	body := `## CONTEXT
ctx

## APPROACH
approach

## ACCEPTANCE_CRITERIA
AC-1: test

## SCOPE
[modify] main.go (reason)

## CONSTRAINTS
none

## DEPENDS_ON

#5

#8

`
	spec, err := ParseIssueSpec(body)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if len(spec.DependsOn) != 2 {
		t.Fatalf("DependsOn length = %d, want 2", len(spec.DependsOn))
	}
	if spec.DependsOn[0] != "5" || spec.DependsOn[1] != "8" {
		t.Errorf("DependsOn = %v, want [5 8]", spec.DependsOn)
	}
}

func TestParseIssueSpec_DependsOn_IgnoresNonHashLines(t *testing.T) {
	body := `## CONTEXT
ctx

## APPROACH
approach

## ACCEPTANCE_CRITERIA
AC-1: test

## SCOPE
[modify] main.go (reason)

## CONSTRAINTS
none

## DEPENDS_ON
#10
some text without hash
#20
`
	spec, err := ParseIssueSpec(body)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if len(spec.DependsOn) != 2 {
		t.Fatalf("DependsOn length = %d, want 2", len(spec.DependsOn))
	}
	if spec.DependsOn[0] != "10" || spec.DependsOn[1] != "20" {
		t.Errorf("DependsOn = %v, want [10 20]", spec.DependsOn)
	}
}

func TestParseIssueSpec_DependsOn_Absent(t *testing.T) {
	spec, err := ParseIssueSpec(fullIssueBody)
	if err != nil {
		t.Fatalf("ParseIssueSpec failed: %v", err)
	}
	if spec.DependsOn != nil {
		t.Errorf("expected nil DependsOn when ## DEPENDS_ON absent, got %v", spec.DependsOn)
	}
}

func TestValidateIssueSpec_DependsOn_MalformedLines(t *testing.T) {
	body := `## CONTEXT
ctx

## APPROACH
approach

## ACCEPTANCE_CRITERIA
AC-1: test

## SCOPE
[modify] main.go (reason)

## CONSTRAINTS
none

## DEPENDS_ON
#10
#abc
not-a-hash
#20
`
	result := ValidateIssueSpec(body)

	// Should have 2 warnings: "#abc" and "not-a-hash"
	warnCount := 0
	for _, w := range result.Warnings {
		if strings.Contains(w, "DEPENDS_ON format warning") {
			warnCount++
		}
	}
	if warnCount != 2 {
		t.Errorf("expected 2 DEPENDS_ON format warnings, got %d: %v", warnCount, result.Warnings)
	}
}

func TestValidateIssueSpec_DependsOn_AllValid_NoWarnings(t *testing.T) {
	body := `## CONTEXT
ctx

## APPROACH
approach

## ACCEPTANCE_CRITERIA
AC-1: test main.go changes

## SCOPE
[modify] main.go (reason)

## CONSTRAINTS
none

## DEPENDS_ON
#10
#20
`
	result := ValidateIssueSpec(body)
	for _, w := range result.Warnings {
		if strings.Contains(w, "DEPENDS_ON") {
			t.Errorf("unexpected DEPENDS_ON warning: %q", w)
		}
	}
}
