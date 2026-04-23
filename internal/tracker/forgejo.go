package tracker

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// ForgejoClient implements TrackerClient for Forgejo/Gitea REST API v1.
type ForgejoClient struct {
	restClient
}

// forgejoIssue is the JSON shape returned by the Gitea/Forgejo issues API.
type forgejoIssue struct {
	Number  int            `json:"number"`
	Title   string         `json:"title"`
	Body    string         `json:"body"`
	State   string         `json:"state"`
	HTMLURL string         `json:"html_url"`
	Labels  []forgejoLabel `json:"labels"`
}

type forgejoLabel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// forgejoPR is the JSON shape returned by the Gitea/Forgejo pulls API.
type forgejoPR struct {
	Number  int          `json:"number"`
	Title   string       `json:"title"`
	State   string       `json:"state"`
	Merged  bool         `json:"merged"`
	HTMLURL string       `json:"html_url"`
	Head    forgejoPRRef `json:"head"`
}

type forgejoPRRef struct {
	Ref string `json:"ref"` // branch name, e.g. "feat/13-refactor-json-toml"
}

func (c *ForgejoClient) ListIssues(ctx context.Context, repo string) ([]Issue, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues?state=open&type=issues&limit=%d", owner, name, forgejoPageLimit)

	var items []forgejoIssue
	if err := c.get(ctx, path, &items); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	issues := make([]Issue, 0, len(items))
	for _, item := range items {
		issues = append(issues, forgejoIssueToIssue(item))
	}
	return issues, nil
}

func (c *ForgejoClient) GetIssue(ctx context.Context, repo string, id string) (*Issue, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%s", owner, name, id)

	var item forgejoIssue
	if err := c.get(ctx, path, &item); err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}

	issue := forgejoIssueToIssue(item)
	return &issue, nil
}

func (c *ForgejoClient) CloseIssue(ctx context.Context, repo string, id string) error {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%s", owner, name, id)
	body := struct {
		State string `json:"state"`
	}{State: "closed"}
	if err := c.doJSON(ctx, http.MethodPatch, path, body, nil); err != nil {
		return fmt.Errorf("close issue: %w", err)
	}
	return nil
}

func (c *ForgejoClient) UpdateLabels(ctx context.Context, repo string, id string, add, remove []string) error {
	owner, name := splitRepo(repo)
	labelsPath := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%s/labels", owner, name, id)
	repoLabelsPath := fmt.Sprintf("/api/v1/repos/%s/%s/labels", owner, name)

	var currentLabels []forgejoLabel
	if err := c.get(ctx, labelsPath, &currentLabels); err != nil {
		return fmt.Errorf("get current labels: %w", err)
	}

	var repoLabels []forgejoLabel
	if err := c.get(ctx, repoLabelsPath, &repoLabels); err != nil {
		return fmt.Errorf("get repo labels: %w", err)
	}

	newIDs := resolveNewLabelIDs(currentLabels, repoLabels, add, remove)

	body := struct {
		Labels []int64 `json:"labels"`
	}{Labels: newIDs}
	if err := c.doJSON(ctx, http.MethodPut, labelsPath, body, nil); err != nil {
		return fmt.Errorf("update labels: %w", err)
	}
	return nil
}

func (c *ForgejoClient) FindPRByBranch(ctx context.Context, repo string, branch string) (*PRStatus, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls?state=all&head=%s&limit=10", owner, name, branch)

	var prs []forgejoPR
	if err := c.get(ctx, path, &prs); err != nil {
		return nil, fmt.Errorf("find PR by branch: %w", err)
	}

	// Client-side validation: head.ref must match exactly.
	for _, pr := range prs {
		if pr.Head.Ref != branch {
			continue
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
	return nil, nil // no matching PR
}

func (c *ForgejoClient) GetPRStatus(ctx context.Context, repo string, prID string) (*PRStatus, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s", owner, name, prID)

	var pr forgejoPR
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

func (c *ForgejoClient) ListOpenPRs(ctx context.Context, repo string) ([]PRInfo, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls?state=open&limit=50", owner, name)

	var prs []forgejoPR
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

func (c *ForgejoClient) MergePR(ctx context.Context, repo string, prID string, method string, commitTitle string) (*PRStatus, error) {
	switch method {
	case "squash", "merge", "rebase":
	default:
		return nil, fmt.Errorf("invalid merge method: %q (want squash|merge|rebase)", method)
	}
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%s/merge", owner, name, prID)
	body := struct {
		Do              string `json:"Do"`
		MergeTitleField string `json:"MergeTitleField,omitempty"`
	}{Do: method, MergeTitleField: commitTitle}

	// Forgejo returns HTTP 200 with empty body on success. Per AC-6 we do
	// NOT re-fetch to populate URL — the merged-state signal is sufficient
	// for the loop engine, and avoiding a second API call keeps the hot
	// path lean. Callers that need the PR URL can fetch it separately.
	if err := c.doJSON(ctx, http.MethodPost, path, body, nil); err != nil {
		return nil, fmt.Errorf("merge PR: %w", err)
	}
	return &PRStatus{ID: prID, State: "merged"}, nil
}

// forgejoIssueToIssue converts the API response to a canonical Issue.
func forgejoIssueToIssue(item forgejoIssue) Issue {
	labels := make([]string, len(item.Labels))
	for i, l := range item.Labels {
		labels[i] = l.Name
	}
	return Issue{
		ID:     fmt.Sprintf("%d", item.Number),
		Title:  item.Title,
		Status: MapLabelsToStatus(item.State, labels),
		Labels: labels,
		Body:   item.Body,
	}
}

// resolveNewLabelIDs computes the new label ID set after adding and removing labels.
func resolveNewLabelIDs(current, repoLabels []forgejoLabel, add, remove []string) []int64 {
	removeSet := make(map[string]bool, len(remove))
	for _, r := range remove {
		removeSet[strings.ToLower(r)] = true
	}

	// Keep current labels that aren't being removed.
	kept := make(map[int64]bool)
	for _, l := range current {
		if !removeSet[strings.ToLower(l.Name)] {
			kept[l.ID] = true
		}
	}

	// Resolve add names to IDs from repo labels.
	repoLabelByName := make(map[string]int64, len(repoLabels))
	for _, l := range repoLabels {
		repoLabelByName[strings.ToLower(l.Name)] = l.ID
	}
	for _, a := range add {
		if id, ok := repoLabelByName[strings.ToLower(a)]; ok {
			kept[id] = true
		}
	}

	ids := make([]int64, 0, len(kept))
	for id := range kept {
		ids = append(ids, id)
	}
	return ids
}

func (c *ForgejoClient) ListRepoLabels(ctx context.Context, repo string) ([]string, error) {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/labels?limit=%d", owner, name, forgejoPageLimit)

	var labels []forgejoLabel
	if err := c.get(ctx, path, &labels); err != nil {
		return nil, fmt.Errorf("list repo labels: %w", err)
	}

	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names, nil
}

func (c *ForgejoClient) CreateLabel(ctx context.Context, repo string, label LabelDef) error {
	owner, name := splitRepo(repo)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/labels", owner, name)

	body := struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}{Name: label.Name, Color: label.Color}

	return c.doJSON(ctx, http.MethodPost, path, body, nil)
}
