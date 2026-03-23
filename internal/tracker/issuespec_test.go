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
	missing := ValidateIssueSpec(fullIssueBody)
	if len(missing) != 0 {
		t.Errorf("expected no missing sections, got %v", missing)
	}
}

func TestValidateIssueSpec_MissingSections(t *testing.T) {
	body := "## CONTEXT\nSome context\n\n## APPROACH\nSome approach\n"
	missing := ValidateIssueSpec(body)
	if len(missing) != 3 {
		t.Fatalf("expected 3 missing sections, got %d: %v", len(missing), missing)
	}
	expected := []string{"## ACCEPTANCE_CRITERIA", "## SCOPE", "## CONSTRAINTS"}
	for i, m := range missing {
		if m != expected[i] {
			t.Errorf("missing[%d] = %q, want %q", i, m, expected[i])
		}
	}
}

func TestValidateIssueSpec_EmptyBody(t *testing.T) {
	missing := ValidateIssueSpec("")
	if len(missing) != 5 {
		t.Errorf("expected 5 missing sections, got %d", len(missing))
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
