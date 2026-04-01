package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/zac15987/zpit/internal/broker"
)

// testServer creates a server wired to a real broker, with controllable stdin/stdout.
func testServer(t *testing.T) (*Server, *broker.Broker, *io.PipeWriter, *bytes.Buffer) {
	t.Helper()
	logger := log.New(io.Discard, "", 0)
	b, err := broker.New(logger)
	if err != nil {
		t.Fatalf("broker: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	cfg := ServerConfig{
		BrokerURL: "http://" + b.Addr(),
		ProjectID: "test-proj",
		IssueID:   "42",
	}

	pr, pw := io.Pipe()
	var stdout bytes.Buffer
	srv := NewServer(cfg, logger, pr, &stdout)
	return srv, b, pw, &stdout
}

// sendAndClose writes lines to the pipe, then closes it so the server's Run exits.
func sendAndClose(pw *io.PipeWriter, lines ...string) {
	for _, line := range lines {
		fmt.Fprintln(pw, line)
	}
	pw.Close()
}

// parseResponses parses all JSON-RPC responses from the output buffer.
// Only includes lines that have a non-null id field (filters out notifications).
func parseResponses(t *testing.T, buf *bytes.Buffer) []Response {
	t.Helper()
	var responses []Response
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Logf("skipping unparseable line: %s", line)
			continue
		}
		// Skip notifications (no id field).
		if resp.ID == nil || string(resp.ID) == "null" {
			continue
		}
		responses = append(responses, resp)
	}
	return responses
}

func TestServer_Initialize(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude","version":"1.0"}}}`

	go sendAndClose(pw, req)
	srv.Run()

	responses := parseResponses(t, stdout)
	if len(responses) == 0 {
		t.Fatal("no responses")
	}

	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion: got %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
	if _, ok := result.Capabilities.Experimental["claude/channel"]; !ok {
		t.Error("missing claude/channel capability")
	}
	if result.Capabilities.Tools == nil {
		t.Error("missing tools capability")
	}
	if result.ServerInfo.Name != "zpit-channel" {
		t.Errorf("server name: got %q", result.ServerInfo.Name)
	}
}

func TestServer_Initialized_Notification(t *testing.T) {
	srv, _, pw, stdout := testServer(t)
	// Use a logger that captures output to verify the log message.
	var logBuf bytes.Buffer
	srv.logger = log.New(&logBuf, "", 0)

	notification := `{"jsonrpc":"2.0","method":"notifications/initialized"}`

	go sendAndClose(pw, notification)
	srv.Run()

	// Notification should not produce a response.
	if strings.TrimSpace(stdout.String()) != "" {
		t.Errorf("expected no response for notification, got: %s", stdout.String())
	}

	if !strings.Contains(logBuf.String(), "mcp: initialized") {
		t.Errorf("expected 'mcp: initialized' in log, got: %s", logBuf.String())
	}
}

func TestServer_ToolsList(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	req := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

	go sendAndClose(pw, req)
	srv.Run()

	responses := parseResponses(t, stdout)
	if len(responses) == 0 {
		t.Fatal("no responses")
	}

	var result ToolsListResult
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result.Tools))
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
		if tool.InputSchema.Type != "object" {
			t.Errorf("tool %s: schema type %q, want object", tool.Name, tool.InputSchema.Type)
		}
	}
	for _, expected := range []string{"publish_artifact", "list_artifacts", "send_message"} {
		if !names[expected] {
			t.Errorf("missing tool: %s", expected)
		}
	}

	// Verify publish_artifact has required params.
	for _, tool := range result.Tools {
		if tool.Name == "publish_artifact" {
			if len(tool.InputSchema.Required) != 3 {
				t.Errorf("publish_artifact required: got %v", tool.InputSchema.Required)
			}
			if _, ok := tool.InputSchema.Properties["issue_id"]; !ok {
				t.Error("publish_artifact missing issue_id property")
			}
		}
		if tool.Name == "send_message" {
			if len(tool.InputSchema.Required) != 2 {
				t.Errorf("send_message required: got %v", tool.InputSchema.Required)
			}
		}
	}
}

func TestServer_PublishArtifact(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish_artifact","arguments":{"issue_id":"42","type":"interface","content":"type Foo struct{}"}}}`

	go sendAndClose(pw, req)
	srv.Run()

	responses := parseResponses(t, stdout)
	if len(responses) == 0 {
		t.Fatal("no responses")
	}

	var result CallToolResult
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.IsError {
		t.Errorf("unexpected tool error: %v", result.Content)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "Artifact published") {
		t.Errorf("unexpected result: %v", result.Content)
	}
}

func TestServer_ListArtifacts(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	// Publish first, then list.
	publish := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish_artifact","arguments":{"issue_id":"42","type":"interface","content":"type Foo struct{}"}}}`
	list := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_artifacts","arguments":{}}}`

	go sendAndClose(pw, publish, list)
	srv.Run()

	responses := parseResponses(t, stdout)
	if len(responses) < 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}

	// The list response should contain the artifact.
	var result CallToolResult
	if err := json.Unmarshal(responses[1].Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.IsError {
		t.Errorf("list_artifacts returned error: %v", result.Content)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "type Foo struct{}") {
		t.Errorf("list should contain artifact content: %v", result.Content)
	}
}

func TestServer_SendMessage(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"send_message","arguments":{"to_issue_id":"43","content":"use Foo interface"}}}`

	go sendAndClose(pw, req)
	srv.Run()

	responses := parseResponses(t, stdout)
	if len(responses) == 0 {
		t.Fatal("no responses")
	}

	var result CallToolResult
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.IsError {
		t.Errorf("send_message returned error: %v", result.Content)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "Message sent to 43") {
		t.Errorf("unexpected result: %v", result.Content)
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	req := `{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`

	go sendAndClose(pw, req)
	srv.Run()

	responses := parseResponses(t, stdout)
	if len(responses) == 0 {
		t.Fatal("no responses")
	}

	if responses[0].Error == nil {
		t.Fatal("expected error response")
	}
	if responses[0].Error.Code != ErrCodeMethodNotFound {
		t.Errorf("error code: got %d, want %d", responses[0].Error.Code, ErrCodeMethodNotFound)
	}
}

func TestServer_UnknownTool(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`

	go sendAndClose(pw, req)
	srv.Run()

	responses := parseResponses(t, stdout)
	if len(responses) == 0 {
		t.Fatal("no responses")
	}

	var result CallToolResult
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.IsError {
		t.Error("expected isError=true for unknown tool")
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	go sendAndClose(pw, "not json at all")
	srv.Run()

	// Invalid JSON produces an error response with null id, which parseResponses filters out.
	// Parse all lines directly to find the error response.
	found := false
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Error != nil && resp.Error.Code == ErrCodeInvalidRequest {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error response with code %d, got output: %s", ErrCodeInvalidRequest, stdout.String())
	}
}

func TestServer_MultipleRequests(t *testing.T) {
	srv, _, pw, stdout := testServer(t)

	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	initialized := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	tools := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

	go sendAndClose(pw, init, initialized, tools)
	srv.Run()

	responses := parseResponses(t, stdout)
	// Should have 2 responses (initialize + tools/list), notification produces no response.
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(responses), stdout.String())
	}
}

func TestReadConfigFromEnv(t *testing.T) {
	// Test missing variables.
	t.Setenv("ZPIT_BROKER_URL", "")
	t.Setenv("ZPIT_PROJECT_ID", "")
	t.Setenv("ZPIT_ISSUE_ID", "")

	_, err := ReadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error for missing env vars")
	}
	if !strings.Contains(err.Error(), "ZPIT_BROKER_URL") {
		t.Errorf("error should mention ZPIT_BROKER_URL: %v", err)
	}

	// Test all present.
	t.Setenv("ZPIT_BROKER_URL", "http://localhost:9999")
	t.Setenv("ZPIT_PROJECT_ID", "proj1")
	t.Setenv("ZPIT_ISSUE_ID", "42")

	cfg, err := ReadConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BrokerURL != "http://localhost:9999" {
		t.Errorf("BrokerURL: %s", cfg.BrokerURL)
	}
	if cfg.ProjectID != "proj1" {
		t.Errorf("ProjectID: %s", cfg.ProjectID)
	}
	if cfg.IssueID != "42" {
		t.Errorf("IssueID: %s", cfg.IssueID)
	}
}

func TestReadConfigFromEnv_PartialMissing(t *testing.T) {
	t.Setenv("ZPIT_BROKER_URL", "http://localhost:9999")
	t.Setenv("ZPIT_PROJECT_ID", "")
	t.Setenv("ZPIT_ISSUE_ID", "42")

	_, err := ReadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ZPIT_PROJECT_ID") {
		t.Errorf("error should mention ZPIT_PROJECT_ID: %v", err)
	}
}

func TestServer_SSE_ChannelNotification(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	b, err := broker.New(logger)
	if err != nil {
		t.Fatalf("broker: %v", err)
	}
	defer b.Close()

	cfg := ServerConfig{
		BrokerURL: "http://" + b.Addr(),
		ProjectID: "test-proj",
		IssueID:   "42",
	}

	pr, pw := io.Pipe()
	stdoutPr, stdoutPw := io.Pipe()
	srv := NewServer(cfg, logger, pr, stdoutPw)

	// Run server in background.
	done := make(chan error, 1)
	go func() { done <- srv.Run() }()

	// Give SSE time to connect.
	time.Sleep(200 * time.Millisecond)

	// Post an artifact to the broker — should appear as channel notification in stdout.
	postBody := strings.NewReader(`{"type":"interface","content":"type Bar int"}`)
	resp, err := srv.client.Post(cfg.BrokerURL+"/api/artifacts/test-proj/99", "application/json", postBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// Read from stdout pipe (with timeout).
	readDone := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := stdoutPr.Read(buf)
		readDone <- string(buf[:n])
	}()

	select {
	case output := <-readDone:
		// Should contain a channel notification.
		if !strings.Contains(output, "notifications/claude/channel") {
			t.Errorf("expected channel notification, got: %s", output)
		}
		if !strings.Contains(output, "zpit-broker") {
			t.Errorf("expected meta source 'zpit-broker', got: %s", output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for channel notification")
	}

	pw.Close()
	stdoutPw.Close()
}

func TestChannelTools(t *testing.T) {
	tools := channelTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Verify each tool has a non-empty description.
	for _, tool := range tools {
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
	}
}
