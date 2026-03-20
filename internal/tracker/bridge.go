package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/zac15987/zpit/internal/config"
)

const (
	defaultModel     = "sonnet"
	defaultMaxBudget = "1.00"
	defaultTimeout   = 60 * time.Second
)

// ExecFunc is the signature for executing an external command with stdin.
type ExecFunc func(ctx context.Context, name string, args []string, stdin string) ([]byte, error)

// TrackerBridge executes claude -p with MCP tools to interact with issue trackers.
type TrackerBridge struct {
	Model     string
	MaxBudget string
	Timeout   time.Duration
	ExecFunc  ExecFunc
}

// NewBridge creates a TrackerBridge with default settings.
func NewBridge() *TrackerBridge {
	return &TrackerBridge{
		Model:     defaultModel,
		MaxBudget: defaultMaxBudget,
		Timeout:   defaultTimeout,
		ExecFunc:  defaultExec,
	}
}

// claudeResponse is the JSON envelope returned by claude -p --output-format json.
type claudeResponse struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// issueListOutput is the schema-constrained output for ListIssues.
type issueListOutput struct {
	Issues []issueItem `json:"issues"`
}

type issueItem struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	State  string   `json:"state"`
	Labels []string `json:"labels"`
	URL    string   `json:"url"`
}

// JSON schemas for constrained decoding.
const issueListSchema = `{"type":"object","properties":{"issues":{"type":"array","items":{"type":"object","properties":{"number":{"type":"integer"},"title":{"type":"string"},"state":{"type":"string"},"labels":{"type":"array","items":{"type":"string"}},"url":{"type":"string"}},"required":["number","title","state"]}}},"required":["issues"]}`

const confirmResultSchema = `{"type":"object","properties":{"success":{"type":"boolean"},"message":{"type":"string"}},"required":["success"]}`

// ListIssues retrieves open issues from the tracker via claude -p + MCP.
func (b *TrackerBridge) ListIssues(ctx context.Context, project config.ProjectConfig, provider config.ProviderEntry) ([]Issue, error) {
	if provider.MCPServer == "" {
		return nil, fmt.Errorf("no MCP server configured for tracker %q", project.Tracker)
	}

	tools := mcpTools(provider, "list")
	prompt := fmt.Sprintf("List all open issues (not pull requests) from repository %s. Return every issue with its number, title, state, labels, and url. Do not include issue body.", project.Repo)

	raw, err := b.execClaude(ctx, prompt, issueListSchema, tools)
	if err != nil {
		return nil, fmt.Errorf("ListIssues: %w", err)
	}

	var output issueListOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return nil, fmt.Errorf("ListIssues: parsing structured_output: %w", err)
	}

	issues := make([]Issue, 0, len(output.Issues))
	for _, item := range output.Issues {
		issues = append(issues, Issue{
			ID:     fmt.Sprintf("%d", item.Number),
			Title:  item.Title,
			Status: mapLabelsToStatus(item.State, item.Labels),
			Labels: item.Labels,
		})
	}

	return issues, nil
}

// ConfirmIssue changes an issue status from pending_confirm to todo.
func (b *TrackerBridge) ConfirmIssue(ctx context.Context, project config.ProjectConfig, provider config.ProviderEntry, issueID string) error {
	if provider.MCPServer == "" {
		return fmt.Errorf("no MCP server configured for tracker %q", project.Tracker)
	}

	tools := mcpTools(provider, "confirm")
	prompt := fmt.Sprintf(
		`For repository %s, issue #%s:
1. Remove the label "pending" if it exists
2. Add the label "todo" if it doesn't exist
Return success=true when done.`,
		project.Repo, issueID,
	)

	raw, err := b.execClaude(ctx, prompt, confirmResultSchema, tools)
	if err != nil {
		return fmt.Errorf("ConfirmIssue: %w", err)
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("ConfirmIssue: parsing response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("ConfirmIssue: %s", result.Message)
	}

	return nil
}

// CheckMCP verifies that the named MCP server is available.
func CheckMCP(serverName string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "claude", "mcp", "list").Output()
	if err != nil {
		return false, fmt.Errorf("running 'claude mcp list': %w", err)
	}

	// Each line in the output contains a server name.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, serverName) {
			return true, nil
		}
	}
	return false, nil
}

// execClaude runs claude -p with the given prompt, schema, and allowed tools.
// Returns the structured_output field from the JSON response.
func (b *TrackerBridge) execClaude(ctx context.Context, prompt, jsonSchema, allowedTools string) (json.RawMessage, error) {
	args := []string{
		"-p",
		"--output-format", "json",
		"--model", b.Model,
		"--json-schema", jsonSchema,
		"--allowedTools", allowedTools,
		"--max-budget-usd", b.MaxBudget,
		"--no-session-persistence",
	}

	out, err := b.ExecFunc(ctx, "claude", args, prompt)
	if err != nil {
		return nil, fmt.Errorf("exec claude: %w", err)
	}

	var resp claudeResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		// Include first 200 bytes of output for debugging
		snippet := string(out)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("parsing claude response: %w (output: %s)", err, snippet)
	}

	if resp.IsError {
		return nil, fmt.Errorf("claude returned error: %s", resp.Result)
	}

	if len(resp.StructuredOutput) == 0 {
		return nil, fmt.Errorf("no structured_output in claude response")
	}

	return resp.StructuredOutput, nil
}

// mcpTools returns the allowed MCP tool names for a given provider and operation.
func mcpTools(provider config.ProviderEntry, operation string) string {
	server := provider.MCPServer

	switch operation {
	case "list":
		return fmt.Sprintf("mcp__%s__list_issues", server)
	case "confirm":
		switch provider.Type {
		case "forgejo_issues":
			return fmt.Sprintf("mcp__%s__issue_read mcp__%s__issue_write mcp__%s__label_read mcp__%s__label_write", server, server, server, server)
		case "github_issues":
			return fmt.Sprintf("mcp__%s__list_issues mcp__%s__update_issue mcp__%s__add_issue_labels mcp__%s__remove_issue_label", server, server, server, server)
		default:
			return fmt.Sprintf("mcp__%s__issue_read mcp__%s__issue_write mcp__%s__label_read mcp__%s__label_write", server, server, server, server)
		}
	default:
		return ""
	}
}

// mapLabelsToStatus converts provider-specific labels to a canonical status.
func mapLabelsToStatus(state string, labels []string) string {
	if state == "closed" {
		return StatusDone
	}

	labelSet := make(map[string]bool)
	for _, l := range labels {
		labelSet[strings.ToLower(l)] = true
	}

	// Priority order: more specific statuses first.
	statusLabels := []struct {
		status string
		label  string
	}{
		{StatusNeedsVerify, "verify"},
		{StatusWaitingReview, "review"},
		{StatusAIReview, "ai-review"},
		{StatusInProgress, "wip"},
		{StatusTodo, "todo"},
		{StatusPendingConfirm, "pending"},
	}

	for _, sl := range statusLabels {
		if labelSet[sl.label] {
			return sl.status
		}
	}

	// No recognized status label — return open state as-is.
	return "open"
}

// defaultExec runs a command with stdin piped. Captures stderr on failure.
func defaultExec(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return nil, fmt.Errorf("%w (stderr: %s)", err, stderr.String())
	}
	return out, err
}
