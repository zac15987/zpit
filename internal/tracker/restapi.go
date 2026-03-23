package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// restClient is the shared HTTP helper for ForgejoClient and GitHubClient.
type restClient struct {
	baseURL    string
	token      string
	authScheme string // "token" (Forgejo) or "Bearer" (GitHub)
	accept     string // optional Accept header (e.g. "application/vnd.github+json")
	httpClient *http.Client
}

func (c *restClient) get(ctx context.Context, path string, dest interface{}) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, dest)
}

func (c *restClient) doJSON(ctx context.Context, method, path string, reqBody, respDest interface{}) error {
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", c.authScheme+" "+c.token)
	if c.accept != "" {
		req.Header.Set("Accept", c.accept)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("http %s %s: status %d: %s", method, path, resp.StatusCode, string(body))
	}

	if respDest != nil {
		if err := json.NewDecoder(resp.Body).Decode(respDest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// splitRepo splits "owner/repo" into owner and repo name.
func splitRepo(repo string) (string, string) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return repo, ""
	}
	return parts[0], parts[1]
}
