package tracker

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// GitHubClient implements TrackerClient for GitHub REST API.
type GitHubClient struct {
	restClient
}

// githubIssue is the JSON shape returned by the GitHub issues API.
type githubIssue struct {
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Body        *string       `json:"body"` // nullable
	State       string        `json:"state"`
	HTMLURL     string        `json:"html_url"`
	Labels      []githubLabel `json:"labels"`
	PullRequest *struct{}     `json:"pull_request"` // non-null means it's a PR
}

type githubLabel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// githubPR is the JSON shape returned by the GitHub pulls API.
type githubPR struct {
	Number   int         `json:"number"`
	Title    string      `json:"title"`
	State    string      `json:"state"`
	Merged   bool        `json:"merged"`
	MergedAt *string     `json:"merged_at"` // list endpoint omits "merged"; use as fallback
	HTMLURL  string      `json:"html_url"`
	Head     githubPRRef `json:"head"`
}

type githubPRRef struct {
	Ref string `json:"ref"` // branch name
}

func (c *GitHubClient) ListIssues(ctx context.Context, repo string) ([]Issue, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/issues?state=open&per_page=%d", owner, name, githubPageLimit)

	var items []githubIssue
	if err := c.get(ctx, path, &items); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	issues := make([]Issue, 0, len(items))
	for _, item := range items {
		if item.PullRequest != nil {
			continue
		}
		issues = append(issues, githubIssueToIssue(item))
	}
	return issues, nil
}

func (c *GitHubClient) GetIssue(ctx context.Context, repo string, id string) (*Issue, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/issues/%s", owner, name, id)

	var item githubIssue
	if err := c.get(ctx, path, &item); err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}

	issue := githubIssueToIssue(item)
	return &issue, nil
}

func (c *GitHubClient) CloseIssue(ctx context.Context, repo string, id string) error {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/issues/%s", owner, name, id)
	body := struct {
		State string `json:"state"`
	}{State: "closed"}
	if err := c.doJSON(ctx, http.MethodPatch, path, body, nil); err != nil {
		return fmt.Errorf("close issue: %w", err)
	}
	return nil
}

func (c *GitHubClient) UpdateLabels(ctx context.Context, repo string, id string, add, remove []string) error {
	owner, name := splitRepo(repo)

	if len(add) > 0 {
		addPath := fmt.Sprintf("/repos/%s/%s/issues/%s/labels", owner, name, id)
		body := struct {
			Labels []string `json:"labels"`
		}{Labels: add}
		if err := c.doJSON(ctx, http.MethodPost, addPath, body, nil); err != nil {
			return fmt.Errorf("add labels: %w", err)
		}
	}

	for _, label := range remove {
		removePath := fmt.Sprintf("/repos/%s/%s/issues/%s/labels/%s", owner, name, id, label)
		if err := c.doJSON(ctx, http.MethodDelete, removePath, nil, nil); err != nil {
			// 404 is OK — label might not exist on the issue.
			if !strings.Contains(err.Error(), "status 404") {
				return fmt.Errorf("remove label %q: %w", label, err)
			}
		}
	}
	return nil
}

func (c *GitHubClient) FindPRByBranch(ctx context.Context, repo string, branch string) (*PRStatus, error) {
	owner, name := splitRepo(repo)
	head := fmt.Sprintf("%s:%s", owner, branch)
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=all&head=%s&per_page=10", owner, name, head)

	var prs []githubPR
	if err := c.get(ctx, path, &prs); err != nil {
		return nil, fmt.Errorf("find PR by branch: %w", err)
	}

	// Client-side validation: head.ref must match exactly.
	for _, pr := range prs {
		if pr.Head.Ref != branch {
			continue
		}
		state := pr.State
		if pr.Merged || pr.MergedAt != nil {
			state = "merged"
		}
		return &PRStatus{
			ID:    fmt.Sprintf("%d", pr.Number),
			State: state,
			URL:   pr.HTMLURL,
		}, nil
	}
	return nil, nil // no matching PR
}

func (c *GitHubClient) GetPRStatus(ctx context.Context, repo string, prID string) (*PRStatus, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/pulls/%s", owner, name, prID)

	var pr githubPR
	if err := c.get(ctx, path, &pr); err != nil {
		return nil, fmt.Errorf("get PR status: %w", err)
	}

	state := pr.State
	if pr.Merged {
		state = "merged"
	}
	return &PRStatus{
		ID:    fmt.Sprintf("%d", pr.Number),
		State: state,
		URL:   pr.HTMLURL,
	}, nil
}

func (c *GitHubClient) ListOpenPRs(ctx context.Context, repo string) ([]PRInfo, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=open&per_page=50", owner, name)

	var prs []githubPR
	if err := c.get(ctx, path, &prs); err != nil {
		return nil, fmt.Errorf("list open PRs: %w", err)
	}

	var result []PRInfo
	for _, pr := range prs {
		result = append(result, PRInfo{
			ID:     fmt.Sprintf("%d", pr.Number),
			Title:  pr.Title,
			Branch: pr.Head.Ref,
			State:  pr.State,
			URL:    pr.HTMLURL,
		})
	}
	return result, nil
}

func (c *GitHubClient) MergePR(ctx context.Context, repo string, prID string, method string, commitTitle string) (*PRStatus, error) {
	// Validate method without making any HTTP call.
	switch method {
	case "squash", "merge", "rebase":
	default:
		return nil, fmt.Errorf("invalid merge method: %q (want squash|merge|rebase)", method)
	}
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/pulls/%s/merge", owner, name, prID)
	body := struct {
		MergeMethod string `json:"merge_method"`
		CommitTitle string `json:"commit_title,omitempty"`
	}{MergeMethod: method, CommitTitle: commitTitle}

	var resp struct {
		Merged  bool   `json:"merged"`
		Message string `json:"message"`
		SHA     string `json:"sha"`
	}
	if err := c.doJSON(ctx, http.MethodPut, path, body, &resp); err != nil {
		return nil, fmt.Errorf("merge PR: %w", err)
	}
	// Merge succeeded; re-fetch to obtain the PR URL for PRStatus.
	pr, err := c.GetPRStatus(ctx, repo, prID)
	if err != nil {
		// Merge succeeded but couldn't fetch URL; return merged status without URL.
		return &PRStatus{ID: prID, State: "merged"}, nil
	}
	pr.State = "merged"
	return pr, nil
}

func (c *GitHubClient) ListRepoLabels(ctx context.Context, repo string) ([]string, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/labels?per_page=%d", owner, name, githubPageLimit)

	var labels []githubLabel
	if err := c.get(ctx, path, &labels); err != nil {
		return nil, fmt.Errorf("list repo labels: %w", err)
	}

	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names, nil
}

func (c *GitHubClient) CreateLabel(ctx context.Context, repo string, label LabelDef) error {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/repos/%s/%s/labels", owner, name)

	color := strings.TrimPrefix(label.Color, "#")
	body := struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}{Name: label.Name, Color: color}

	return c.doJSON(ctx, http.MethodPost, path, body, nil)
}

// githubIssueToIssue converts the API response to a canonical Issue.
func githubIssueToIssue(item githubIssue) Issue {
	labels := make([]string, len(item.Labels))
	for i, l := range item.Labels {
		labels[i] = l.Name
	}
	body := ""
	if item.Body != nil {
		body = *item.Body
	}
	return Issue{
		ID:     fmt.Sprintf("%d", item.Number),
		Title:  item.Title,
		Status: MapLabelsToStatus(item.State, labels),
		Labels: labels,
		Body:   body,
	}
}
