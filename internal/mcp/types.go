package mcp

import "encoding/json"

// JSON-RPC 2.0 message types for MCP Channel stdio server.
// Hand-written because the official Go MCP SDK (v1.4.1) does not support
// sending custom notification methods like notifications/claude/channel.

const jsonRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request (contains id).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response (contains id, result or error).
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no id field).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is the error object in a JSON-RPC 2.0 response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInternalError  = -32603
)

// MCP protocol version (current latest stable).
const ProtocolVersion = "2025-06-18"

// --- MCP initialize handshake types ---

// InitializeParams is the params for initialize request.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

// ClientInfo describes the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the result for initialize response.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

// Capabilities declares the server's MCP capabilities.
type Capabilities struct {
	Experimental map[string]any `json:"experimental"`
	Tools        map[string]any `json:"tools"`
}

// ServerInfo describes the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// --- MCP tools types ---

// Tool describes a single MCP tool.
type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

// JSONSchema is a minimal JSON Schema representation for tool input.
type JSONSchema struct {
	Type       string                `json:"type"`
	Properties map[string]SchemaProperty `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
}

// SchemaProperty describes a single property in a JSON Schema.
type SchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ToolsListResult is the result for tools/list response.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams is the params for tools/call request.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// CallToolResult is the result for tools/call response.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a single content item in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Channel notification types ---

// ChannelNotificationParams is the params for notifications/claude/channel.
type ChannelNotificationParams struct {
	Content string         `json:"content"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// newResponse creates a success response for the given request ID.
func newResponse(id json.RawMessage, result any) Response {
	data, _ := json.Marshal(result)
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  data,
	}
}

// newErrorResponse creates an error response for the given request ID.
func newErrorResponse(id json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

// newChannelNotification creates a notifications/claude/channel message.
func newChannelNotification(content string, meta map[string]any) Notification {
	params := ChannelNotificationParams{
		Content: content,
		Meta:    meta,
	}
	data, _ := json.Marshal(params)
	return Notification{
		JSONRPC: jsonRPCVersion,
		Method:  "notifications/claude/channel",
		Params:  data,
	}
}
