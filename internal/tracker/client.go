package tracker

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	forgejoPageLimit = 50
	githubPageLimit  = 100
)

// TrackerClient defines Zpit's read/write operations against an issue tracker.
type TrackerClient interface {
	ListIssues(ctx context.Context, repo string) ([]Issue, error)
	GetIssue(ctx context.Context, repo string, id string) (*Issue, error)
	UpdateLabels(ctx context.Context, repo string, id string, add, remove []string) error
	CloseIssue(ctx context.Context, repo string, id string) error
	GetPRStatus(ctx context.Context, repo string, prID string) (*PRStatus, error)
	FindPRByBranch(ctx context.Context, repo string, branch string) (*PRStatus, error)
	ListOpenPRs(ctx context.Context, repo string) ([]PRInfo, error)
}

// NewClient creates a TrackerClient for the given provider type.
// tokenEnv is the name of the environment variable holding the API token.
func NewClient(providerType, baseURL, tokenEnv string) (TrackerClient, error) {
	token := os.Getenv(tokenEnv)
	if token == "" {
		return nil, fmt.Errorf("env var %s not set", tokenEnv)
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	switch providerType {
	case "forgejo_issues":
		return &ForgejoClient{restClient: restClient{
			baseURL:    strings.TrimRight(baseURL, "/"),
			token:      token,
			authScheme: "token",
			httpClient: httpClient,
		}}, nil
	case "github_issues":
		return &GitHubClient{restClient: restClient{
			baseURL:    "https://api.github.com",
			token:      token,
			authScheme: "Bearer",
			accept:     "application/vnd.github+json",
			httpClient: httpClient,
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported tracker type: %s", providerType)
	}
}

// MapLabelsToStatus converts provider-specific labels to a canonical status.
func MapLabelsToStatus(state string, labels []string) string {
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

	return "open"
}
