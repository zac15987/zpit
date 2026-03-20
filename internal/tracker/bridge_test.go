package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zac15987/zpit/internal/config"
)

func mockExec(output string, err error) ExecFunc {
	return func(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
		return []byte(output), err
	}
}

func forgejoProvider() config.ProviderEntry {
	return config.ProviderEntry{
		Type:      "forgejo_issues",
		URL:       "https://git.example.com",
		MCPServer: "gitea",
	}
}

func githubProvider() config.ProviderEntry {
	return config.ProviderEntry{
		Type:      "github_issues",
		MCPServer: "github",
	}
}

func testProject() config.ProjectConfig {
	return config.ProjectConfig{
		ID:      "test-project",
		Tracker: "forgejo-local",
		Repo:    "org/repo",
	}
}

func TestListIssues_Success(t *testing.T) {
	resp := claudeResponse{
		Type:    "result",
		Subtype: "success",
		StructuredOutput: json.RawMessage(`{"issues":[
			{"number":4,"title":"Test issue","state":"open","labels":["pending"],"body":"## CONTEXT\ntest","url":"https://example.com/4"},
			{"number":5,"title":"Another issue","state":"open","labels":["todo"],"body":"body","url":"https://example.com/5"}
		]}`),
	}
	out, _ := json.Marshal(resp)

	bridge := &TrackerBridge{
		Model:     "haiku",
		MaxBudget: "0.10",
		ExecFunc:  mockExec(string(out), nil),
	}

	issues, err := bridge.ListIssues(context.Background(), testProject(), forgejoProvider())
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[0].ID != "4" {
		t.Errorf("issues[0].ID = %q, want %q", issues[0].ID, "4")
	}
	if issues[0].Title != "Test issue" {
		t.Errorf("issues[0].Title = %q", issues[0].Title)
	}
	if issues[0].Status != StatusPendingConfirm {
		t.Errorf("issues[0].Status = %q, want %q", issues[0].Status, StatusPendingConfirm)
	}
	if issues[1].Status != StatusTodo {
		t.Errorf("issues[1].Status = %q, want %q", issues[1].Status, StatusTodo)
	}
}

func TestListIssues_EmptyResult(t *testing.T) {
	resp := claudeResponse{
		Type:             "result",
		Subtype:          "success",
		StructuredOutput: json.RawMessage(`{"issues":[]}`),
	}
	out, _ := json.Marshal(resp)

	bridge := &TrackerBridge{ExecFunc: mockExec(string(out), nil)}
	issues, err := bridge.ListIssues(context.Background(), testProject(), forgejoProvider())
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(issues))
	}
}

func TestListIssues_ExecError(t *testing.T) {
	bridge := &TrackerBridge{ExecFunc: mockExec("", fmt.Errorf("command not found"))}
	_, err := bridge.ListIssues(context.Background(), testProject(), forgejoProvider())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListIssues_InvalidJSON(t *testing.T) {
	bridge := &TrackerBridge{ExecFunc: mockExec("not json", nil)}
	_, err := bridge.ListIssues(context.Background(), testProject(), forgejoProvider())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestListIssues_NoMCPServer(t *testing.T) {
	bridge := &TrackerBridge{ExecFunc: mockExec("", nil)}
	provider := config.ProviderEntry{Type: "forgejo_issues"}
	_, err := bridge.ListIssues(context.Background(), testProject(), provider)
	if err == nil {
		t.Fatal("expected error for missing MCP server")
	}
}

func TestConfirmIssue_Success(t *testing.T) {
	resp := claudeResponse{
		Type:             "result",
		Subtype:          "success",
		StructuredOutput: json.RawMessage(`{"success":true,"message":"done"}`),
	}
	out, _ := json.Marshal(resp)

	bridge := &TrackerBridge{ExecFunc: mockExec(string(out), nil)}
	err := bridge.ConfirmIssue(context.Background(), testProject(), forgejoProvider(), "4")
	if err != nil {
		t.Fatalf("ConfirmIssue failed: %v", err)
	}
}

func TestConfirmIssue_Failure(t *testing.T) {
	resp := claudeResponse{
		Type:             "result",
		Subtype:          "success",
		StructuredOutput: json.RawMessage(`{"success":false,"message":"label not found"}`),
	}
	out, _ := json.Marshal(resp)

	bridge := &TrackerBridge{ExecFunc: mockExec(string(out), nil)}
	err := bridge.ConfirmIssue(context.Background(), testProject(), forgejoProvider(), "4")
	if err == nil {
		t.Fatal("expected error for failed confirm")
	}
}

func TestMCPTools_Gitea(t *testing.T) {
	provider := forgejoProvider()
	tools := mcpTools(provider, "list")
	if tools != "mcp__gitea__list_issues" {
		t.Errorf("list tools = %q", tools)
	}

	tools = mcpTools(provider, "confirm")
	if !strings.Contains(tools, "mcp__gitea__issue_read") {
		t.Errorf("confirm tools missing issue_read: %q", tools)
	}
	if !strings.Contains(tools, "mcp__gitea__label_write") {
		t.Errorf("confirm tools missing label_write: %q", tools)
	}
}

func TestMCPTools_GitHub(t *testing.T) {
	provider := githubProvider()
	tools := mcpTools(provider, "list")
	if tools != "mcp__github__list_issues" {
		t.Errorf("list tools = %q", tools)
	}
}

func TestMapLabelsToStatus(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		labels   []string
		expected string
	}{
		{"closed issue", "closed", nil, StatusDone},
		{"pending label", "open", []string{"pending"}, StatusPendingConfirm},
		{"todo label", "open", []string{"todo"}, StatusTodo},
		{"wip label", "open", []string{"wip"}, StatusInProgress},
		{"review label", "open", []string{"review"}, StatusWaitingReview},
		{"ai-review label", "open", []string{"ai-review"}, StatusAIReview},
		{"verify label", "open", []string{"verify"}, StatusNeedsVerify},
		{"no recognized label", "open", []string{"bug"}, "open"},
		{"empty labels", "open", nil, "open"},
		{"multiple labels priority", "open", []string{"pending", "wip"}, StatusInProgress},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := mapLabelsToStatus(tt.state, tt.labels)
			if status != tt.expected {
				t.Errorf("mapLabelsToStatus(%q, %q, %v) = %q, want %q", "forgejo_issues", tt.state, tt.labels, status, tt.expected)
			}
		})
	}
}

func TestClaudeResponseError(t *testing.T) {
	resp := claudeResponse{
		Type:    "result",
		Subtype: "error",
		IsError: true,
		Result:  "API key invalid",
	}
	out, _ := json.Marshal(resp)

	bridge := &TrackerBridge{ExecFunc: mockExec(string(out), nil)}
	_, err := bridge.ListIssues(context.Background(), testProject(), forgejoProvider())
	if err == nil {
		t.Fatal("expected error for error response")
	}
	if !strings.Contains(err.Error(), "claude returned error") {
		t.Errorf("error = %q, expected 'claude returned error'", err)
	}
}
