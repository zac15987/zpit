package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ServerConfig holds the configuration for a Channel MCP stdio server.
type ServerConfig struct {
	BrokerURL string // e.g. "http://127.0.0.1:54321"
	ProjectID string // project identifier
	IssueID   string // this agent's issue ID
}

// ReadConfigFromEnv reads server configuration from environment variables.
// Returns error if any required variable is missing.
func ReadConfigFromEnv() (ServerConfig, error) {
	brokerURL := os.Getenv("ZPIT_BROKER_URL")
	projectID := os.Getenv("ZPIT_PROJECT_ID")
	issueID := os.Getenv("ZPIT_ISSUE_ID")

	var missing []string
	if brokerURL == "" {
		missing = append(missing, "ZPIT_BROKER_URL")
	}
	if projectID == "" {
		missing = append(missing, "ZPIT_PROJECT_ID")
	}
	if issueID == "" {
		missing = append(missing, "ZPIT_ISSUE_ID")
	}
	if len(missing) > 0 {
		return ServerConfig{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return ServerConfig{BrokerURL: brokerURL, ProjectID: projectID, IssueID: issueID}, nil
}

// channelTools returns the three MCP tools for cross-agent communication.
func channelTools() []Tool {
	return []Tool{
		{
			Name:        "publish_artifact",
			Description: "Publish an artifact (interface definition, type spec, etc.) to the shared broker for other agents to consume.",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"issue_id": {Type: "string", Description: "The issue ID this artifact belongs to"},
					"type":     {Type: "string", Description: "Artifact type (e.g. interface, type, schema, config)"},
					"content":  {Type: "string", Description: "The artifact content (code, definition, etc.)"},
				},
				Required: []string{"issue_id", "type", "content"},
			},
		},
		{
			Name:        "list_artifacts",
			Description: "List all published artifacts from all agents in the current project.",
			InputSchema: JSONSchema{
				Type: "object",
			},
		},
		{
			Name:        "send_message",
			Description: "Send a message to another agent identified by their issue ID.",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"to_issue_id": {Type: "string", Description: "The target agent's issue ID"},
					"content":     {Type: "string", Description: "Message content"},
				},
				Required: []string{"to_issue_id", "content"},
			},
		},
	}
}

// Server is a Channel MCP stdio server that bridges Claude Code agents with the HTTP broker.
type Server struct {
	config    ServerConfig
	logger    *log.Logger
	stdin     io.Reader
	stdout    io.Writer
	client    *http.Client
	sseCancel context.CancelFunc // cancels the SSE listener goroutine
}

// NewServer creates a new MCP server with the given configuration and I/O streams.
func NewServer(cfg ServerConfig, logger *log.Logger, stdin io.Reader, stdout io.Writer) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Server{
		config: cfg,
		logger: logger,
		stdin:  stdin,
		stdout: stdout,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Run starts the MCP stdio server. It reads JSON-RPC messages from stdin and writes responses to stdout.
// It also starts an SSE listener to push channel notifications.
// This method blocks until stdin is closed.
func (s *Server) Run() error {
	s.logger.Println("mcp: server starting")
	s.logger.Printf("mcp: broker=%s project=%s issue=%s", s.config.BrokerURL, s.config.ProjectID, s.config.IssueID)

	// Start SSE listener in background with cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	s.sseCancel = cancel
	go s.listenSSE(ctx)
	defer cancel()

	scanner := bufio.NewScanner(s.stdin)
	// Increase buffer to handle large MCP messages.
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		s.handleMessage(line)
	}

	if err := scanner.Err(); err != nil {
		s.logger.Printf("mcp: stdin read error: %v", err)
		return err
	}
	s.logger.Println("mcp: server stopped (stdin closed)")
	return nil
}

// handleMessage processes a single JSON-RPC message from stdin.
func (s *Server) handleMessage(raw []byte) {
	// Try to determine if this is a notification (no id) or a request (has id).
	var probe struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		s.logger.Printf("mcp: invalid JSON: %v", err)
		s.writeResponse(newErrorResponse(nil, ErrCodeInvalidRequest, "invalid JSON"))
		return
	}

	// Notification: no id field (or id is null/absent).
	if probe.ID == nil || string(probe.ID) == "null" {
		s.handleNotification(probe.Method)
		return
	}

	// Request: has id.
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		s.logger.Printf("mcp: failed to parse request: %v", err)
		s.writeResponse(newErrorResponse(probe.ID, ErrCodeInvalidRequest, "invalid request"))
		return
	}

	s.handleRequest(req)
}

// handleNotification processes JSON-RPC notifications (no response expected).
func (s *Server) handleNotification(method string) {
	switch method {
	case "notifications/initialized":
		s.logger.Println("mcp: initialized")
	default:
		s.logger.Printf("mcp: ignoring notification method=%s", method)
	}
}

// handleRequest processes JSON-RPC requests and writes responses.
func (s *Server) handleRequest(req Request) {
	s.logger.Printf("mcp: request method=%s id=%s", req.Method, string(req.ID))

	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(req)
	default:
		s.logger.Printf("mcp: method not found: %s", req.Method)
		s.writeResponse(newErrorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method)))
	}
}

func (s *Server) handleInitialize(req Request) {
	s.logger.Println("mcp: handling initialize")
	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: Capabilities{
			Experimental: map[string]any{
				"claude/channel": map[string]any{},
			},
			Tools: map[string]any{},
		},
		ServerInfo: ServerInfo{
			Name:    "zpit-channel",
			Version: "0.1.0",
		},
	}
	s.writeResponse(newResponse(req.ID, result))
	s.logger.Println("mcp: initialize response sent")
}

func (s *Server) handleToolsList(req Request) {
	s.logger.Println("mcp: handling tools/list")
	result := ToolsListResult{Tools: channelTools()}
	s.writeResponse(newResponse(req.ID, result))
	s.logger.Printf("mcp: tools/list returned %d tools", len(result.Tools))
}

func (s *Server) handleToolsCall(req Request) {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.logger.Printf("mcp: invalid tools/call params: %v", err)
		s.writeResponse(newErrorResponse(req.ID, ErrCodeInvalidRequest, "invalid tools/call params"))
		return
	}

	s.logger.Printf("mcp: tools/call name=%s", params.Name)

	switch params.Name {
	case "publish_artifact":
		s.callPublishArtifact(req.ID, params.Arguments)
	case "list_artifacts":
		s.callListArtifacts(req.ID)
	case "send_message":
		s.callSendMessage(req.ID, params.Arguments)
	default:
		s.logger.Printf("mcp: unknown tool: %s", params.Name)
		s.writeResponse(newResponse(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", params.Name)}},
			IsError: true,
		}))
	}
}

// --- Tool implementations (HTTP calls to broker) ---

type publishArtifactArgs struct {
	IssueID string `json:"issue_id"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (s *Server) callPublishArtifact(id json.RawMessage, args json.RawMessage) {
	var a publishArtifactArgs
	if err := json.Unmarshal(args, &a); err != nil {
		s.logger.Printf("mcp: publish_artifact bad args: %v", err)
		s.writeToolError(id, "invalid arguments: "+err.Error())
		return
	}

	url := fmt.Sprintf("%s/api/artifacts/%s/%s", s.config.BrokerURL, s.config.ProjectID, a.IssueID)
	body, _ := json.Marshal(map[string]string{"type": a.Type, "content": a.Content})

	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		s.logger.Printf("mcp: publish_artifact HTTP error: %v", err)
		s.writeToolError(id, "broker request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		s.logger.Printf("mcp: publish_artifact broker status=%d", resp.StatusCode)
		s.writeToolError(id, fmt.Sprintf("broker returned status %d", resp.StatusCode))
		return
	}

	s.logger.Printf("mcp: publish_artifact success issue=%s type=%s", a.IssueID, a.Type)
	s.writeResponse(newResponse(id, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Artifact published: issue=%s type=%s", a.IssueID, a.Type)}},
	}))
}

func (s *Server) callListArtifacts(id json.RawMessage) {
	url := fmt.Sprintf("%s/api/artifacts/%s", s.config.BrokerURL, s.config.ProjectID)

	resp, err := s.client.Get(url)
	if err != nil {
		s.logger.Printf("mcp: list_artifacts HTTP error: %v", err)
		s.writeToolError(id, "broker request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Printf("mcp: list_artifacts read body error: %v", err)
		s.writeToolError(id, "failed to read response: "+err.Error())
		return
	}

	s.logger.Printf("mcp: list_artifacts success (%d bytes)", len(data))
	s.writeResponse(newResponse(id, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(data)}},
	}))
}

type sendMessageArgs struct {
	ToIssueID string `json:"to_issue_id"`
	Content   string `json:"content"`
}

func (s *Server) callSendMessage(id json.RawMessage, args json.RawMessage) {
	var a sendMessageArgs
	if err := json.Unmarshal(args, &a); err != nil {
		s.logger.Printf("mcp: send_message bad args: %v", err)
		s.writeToolError(id, "invalid arguments: "+err.Error())
		return
	}

	url := fmt.Sprintf("%s/api/messages/%s/%s", s.config.BrokerURL, s.config.ProjectID, a.ToIssueID)
	body, _ := json.Marshal(map[string]string{"from": s.config.IssueID, "content": a.Content})

	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		s.logger.Printf("mcp: send_message HTTP error: %v", err)
		s.writeToolError(id, "broker request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		s.logger.Printf("mcp: send_message broker status=%d", resp.StatusCode)
		s.writeToolError(id, fmt.Sprintf("broker returned status %d", resp.StatusCode))
		return
	}

	s.logger.Printf("mcp: send_message success to=%s", a.ToIssueID)
	s.writeResponse(newResponse(id, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Message sent to %s", a.ToIssueID)}},
	}))
}

// --- SSE listener ---

// listenSSE connects to the broker's SSE endpoint and forwards events as MCP channel notifications.
// Reconnects on failure with a 3-second backoff. Stops when ctx is cancelled.
func (s *Server) listenSSE(ctx context.Context) {
	url := fmt.Sprintf("%s/api/events/%s", s.config.BrokerURL, s.config.ProjectID)
	s.logger.Printf("mcp: SSE connecting to %s", url)

	for {
		err := s.consumeSSE(ctx, url)
		if ctx.Err() != nil {
			s.logger.Println("mcp: SSE stopped (context cancelled)")
			return
		}
		if err != nil {
			s.logger.Printf("mcp: SSE error: %v", err)
		}
		s.logger.Println("mcp: SSE reconnecting to broker")
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// consumeSSE reads from a single SSE connection and pushes channel notifications.
func (s *Server) consumeSSE(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("SSE request create: %w", err)
	}
	// Use a separate client without timeout for the long-lived SSE connection.
	sseClient := &http.Client{}
	resp, err := sseClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE status: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// Parse the event to build a readable notification.
		var event struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			s.logger.Printf("mcp: SSE bad event: %v", err)
			continue
		}

		content := fmt.Sprintf("[%s] %s", event.Type, string(event.Payload))
		meta := map[string]any{
			"source":  "zpit-broker",
			"type":    event.Type,
			"project": s.config.ProjectID,
		}
		notification := newChannelNotification(content, meta)
		s.writeNotification(notification)
	}
	return scanner.Err()
}

// --- I/O helpers ---

// writeResponse writes a JSON-RPC response to stdout (newline-delimited).
func (s *Server) writeResponse(resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		s.logger.Printf("mcp: marshal response error: %v", err)
		return
	}
	fmt.Fprintf(s.stdout, "%s\n", data)
}

// writeNotification writes a JSON-RPC notification to stdout (newline-delimited).
func (s *Server) writeNotification(n Notification) {
	data, err := json.Marshal(n)
	if err != nil {
		s.logger.Printf("mcp: marshal notification error: %v", err)
		return
	}
	fmt.Fprintf(s.stdout, "%s\n", data)
}

// writeToolError writes a tool error response.
func (s *Server) writeToolError(id json.RawMessage, msg string) {
	s.writeResponse(newResponse(id, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: msg}},
		IsError: true,
	}))
}

// RunFromEnv reads config from environment and starts the server on os.Stdin/os.Stdout.
// This is the main entry point for the serve-channel subcommand.
func RunFromEnv() error {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	cfg, err := ReadConfigFromEnv()
	if err != nil {
		logger.Printf("mcp: config error: %v", err)
		return err
	}

	srv := NewServer(cfg, logger, os.Stdin, os.Stdout)
	return srv.Run()
}
