// Package zplex provides an HTTP client for the zplex session daemon.
// The daemon exposes a REST API for creating and managing terminal sessions.
// The client does not log; callers should inspect returned errors.
package zplex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	healthTimeout        = 500 * time.Millisecond
	createSessionTimeout = 5 * time.Second
	patchStateTimeout    = 3 * time.Second
)

// CreateSessionRequest is the JSON body sent to POST /api/sessions.
type CreateSessionRequest struct {
	Shell      string   `json:"shell"`
	Title      string   `json:"title"`
	Args       []string `json:"args,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	Env        []string `json:"env,omitempty"`
	Cols       int      `json:"cols,omitempty"`
	Rows       int      `json:"rows,omitempty"`
	Source     string   `json:"source,omitempty"`
	ProjectID  string   `json:"project_id,omitempty"`
	IssueID    string   `json:"issue_id,omitempty"`
	Role       string   `json:"role,omitempty"`
	AgentState string   `json:"agent_state,omitempty"`
}

// createSessionResponse is the JSON body returned by POST /api/sessions.
type createSessionResponse struct {
	ID    string `json:"id"`
	WsURL string `json:"ws_url"`
}

// patchAgentStateBody is the JSON body sent to PATCH /api/sessions/{id}.
type patchAgentStateBody struct {
	AgentState string `json:"agent_state"`
}

// Client communicates with the zplex daemon on localhost.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a Client that targets the zplex daemon on the given port.
func New(port int) *Client {
	return &Client{
		baseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		httpClient: &http.Client{},
	}
}

// NewWithBaseURL creates a Client with an explicit base URL.
// Intended for testing against httptest servers.
func NewWithBaseURL(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// Health performs GET /api/health and returns an error if the daemon is
// unreachable, times out (500 ms), or responds with a non-200 status code.
func (c *Client) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/health", nil)
	if err != nil {
		return fmt.Errorf("zplex: build health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("zplex: health: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("zplex: health: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// CreateSession posts a new session to POST /api/sessions and returns the
// session id from the daemon's response. Returns an error on non-2xx status.
func (c *Client) CreateSession(req CreateSessionRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("zplex: marshal create session request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), createSessionTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("zplex: build create session request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("zplex: create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("zplex: create session: unexpected status %d", resp.StatusCode)
	}

	var result createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("zplex: decode create session response: %w", err)
	}
	return result.ID, nil
}

// PatchAgentState sends PATCH /api/sessions/{id} with {"agent_state": state}.
// Valid states are "", "active", "waiting", and "done". Returns an error on
// non-2xx status.
func (c *Client) PatchAgentState(id, state string) error {
	body, err := json.Marshal(patchAgentStateBody{AgentState: state})
	if err != nil {
		return fmt.Errorf("zplex: marshal patch agent state: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), patchStateTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		c.baseURL+"/api/sessions/"+id, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("zplex: build patch agent state request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("zplex: patch agent state: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("zplex: patch agent state: unexpected status %d", resp.StatusCode)
	}
	return nil
}
