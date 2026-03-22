package tracker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- MapLabelsToStatus tests (ported from bridge_test.go) ---

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
			status := MapLabelsToStatus(tt.state, tt.labels)
			if status != tt.expected {
				t.Errorf("MapLabelsToStatus(%q, %v) = %q, want %q", tt.state, tt.labels, status, tt.expected)
			}
		})
	}
}

// --- NewClient tests ---

func TestNewClient_MissingToken(t *testing.T) {
	// Use env var name that is very unlikely to be set.
	_, err := NewClient("forgejo_issues", "http://localhost", "ZPIT_TEST_NONEXISTENT_TOKEN_XYZ")
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestNewClient_UnsupportedType(t *testing.T) {
	t.Setenv("ZPIT_TEST_TOKEN", "dummy")
	_, err := NewClient("linear", "http://localhost", "ZPIT_TEST_TOKEN")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

// --- Forgejo tests ---

func TestForgejoListIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/org/repo/issues" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("type") != "issues" {
			t.Error("missing type=issues query param")
		}
		if r.Header.Get("Authorization") != "token test-token" {
			t.Errorf("bad auth header: %s", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode([]forgejoIssue{
			{Number: 1, Title: "Fix bug", State: "open", Labels: []forgejoLabel{{ID: 10, Name: "pending"}}},
			{Number: 2, Title: "Add feature", State: "open", Labels: []forgejoLabel{{ID: 20, Name: "todo"}}},
		})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "test-token", authScheme: "token", httpClient: ts.Client()}}
	issues, err := client.ListIssues(context.Background(), "org/repo")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2", len(issues))
	}
	if issues[0].Status != StatusPendingConfirm {
		t.Errorf("issues[0].Status = %q, want %q", issues[0].Status, StatusPendingConfirm)
	}
	if issues[1].Status != StatusTodo {
		t.Errorf("issues[1].Status = %q, want %q", issues[1].Status, StatusTodo)
	}
}

func TestForgejoListIssues_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]forgejoIssue{})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	issues, err := client.ListIssues(context.Background(), "org/repo")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("got %d issues, want 0", len(issues))
	}
}

func TestForgejoGetIssue(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/org/repo/issues/1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(forgejoIssue{
			Number: 1,
			Title:  "Fix bug",
			Body:   "## CONTEXT\nSome context",
			State:  "open",
			Labels: []forgejoLabel{{ID: 10, Name: "todo"}},
		})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	issue, err := client.GetIssue(context.Background(), "org/repo", "1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Body != "## CONTEXT\nSome context" {
		t.Errorf("Body = %q", issue.Body)
	}
	if issue.Status != StatusTodo {
		t.Errorf("Status = %q, want %q", issue.Status, StatusTodo)
	}
}

func TestForgejoUpdateLabels(t *testing.T) {
	var putBody struct {
		Labels []int64 `json:"labels"`
	}
	callCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/repos/org/repo/issues/1/labels":
			// Current labels: pending (ID=10)
			json.NewEncoder(w).Encode([]forgejoLabel{{ID: 10, Name: "pending"}})
		case r.Method == "GET" && r.URL.Path == "/api/v1/repos/org/repo/labels":
			// All repo labels
			json.NewEncoder(w).Encode([]forgejoLabel{
				{ID: 10, Name: "pending"},
				{ID: 20, Name: "todo"},
				{ID: 30, Name: "wip"},
			})
		case r.Method == "PUT" && r.URL.Path == "/api/v1/repos/org/repo/issues/1/labels":
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &putBody)
			json.NewEncoder(w).Encode([]forgejoLabel{{ID: 20, Name: "todo"}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	err := client.UpdateLabels(context.Background(), "org/repo", "1", []string{"todo"}, []string{"pending"})
	if err != nil {
		t.Fatalf("UpdateLabels: %v", err)
	}

	// Should contain only todo (ID=20), not pending (ID=10).
	if len(putBody.Labels) != 1 {
		t.Fatalf("PUT labels count = %d, want 1", len(putBody.Labels))
	}
	if putBody.Labels[0] != 20 {
		t.Errorf("PUT labels = %v, want [20]", putBody.Labels)
	}
}

func TestForgejoGetPRStatus_Open(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(forgejoPR{Number: 5, State: "open", Merged: false, HTMLURL: "http://example.com/pr/5"})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.GetPRStatus(context.Background(), "org/repo", "5")
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if pr.State != "open" {
		t.Errorf("State = %q, want %q", pr.State, "open")
	}
}

func TestForgejoGetPRStatus_Merged(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(forgejoPR{Number: 5, State: "closed", Merged: true, HTMLURL: "http://example.com/pr/5"})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.GetPRStatus(context.Background(), "org/repo", "5")
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want %q", pr.State, "merged")
	}
}

func TestForgejoGetPRStatus_Closed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(forgejoPR{Number: 5, State: "closed", Merged: false})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.GetPRStatus(context.Background(), "org/repo", "5")
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if pr.State != "closed" {
		t.Errorf("State = %q, want %q", pr.State, "closed")
	}
}

// --- GitHub tests ---

func TestGitHubListIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("bad auth: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("bad accept: %s", r.Header.Get("Accept"))
		}
		json.NewEncoder(w).Encode([]githubIssue{
			{Number: 1, Title: "Issue", State: "open", Labels: []githubLabel{{Name: "wip"}}},
		})
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "test-token", authScheme: "Bearer", accept: "application/vnd.github+json", httpClient: ts.Client()}}
	issues, err := client.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if issues[0].Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", issues[0].Status, StatusInProgress)
	}
}

func TestGitHubListIssues_FiltersPRs(t *testing.T) {
	prMarker := struct{}{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []githubIssue{
			{Number: 1, Title: "Real issue", State: "open", Labels: []githubLabel{{Name: "todo"}}},
			{Number: 2, Title: "A PR", State: "open", PullRequest: &prMarker},
		}
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	issues, err := client.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (PR should be filtered)", len(issues))
	}
	if issues[0].Title != "Real issue" {
		t.Errorf("Title = %q", issues[0].Title)
	}
}

func TestGitHubUpdateLabels(t *testing.T) {
	var addedLabels struct {
		Labels []string `json:"labels"`
	}
	deletePath := ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &addedLabels)
			json.NewEncoder(w).Encode([]githubLabel{{Name: "todo"}})
		case "DELETE":
			deletePath = r.URL.Path
			json.NewEncoder(w).Encode([]githubLabel{})
		}
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	err := client.UpdateLabels(context.Background(), "owner/repo", "1", []string{"todo"}, []string{"pending"})
	if err != nil {
		t.Fatalf("UpdateLabels: %v", err)
	}

	if len(addedLabels.Labels) != 1 || addedLabels.Labels[0] != "todo" {
		t.Errorf("added labels = %v, want [todo]", addedLabels.Labels)
	}
	if deletePath != "/repos/owner/repo/issues/1/labels/pending" {
		t.Errorf("delete path = %q", deletePath)
	}
}

func TestGitHubGetPRStatus_Merged(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubPR{Number: 10, State: "closed", Merged: true, HTMLURL: "https://github.com/o/r/pull/10"})
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	pr, err := client.GetPRStatus(context.Background(), "owner/repo", "10")
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
	if pr.URL != "https://github.com/o/r/pull/10" {
		t.Errorf("URL = %q", pr.URL)
	}
}

func TestForgejoFindPRByBranch_Found(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]forgejoPR{
			{Number: 5, State: "open", Merged: false, HTMLURL: "http://git.local/o/r/pulls/5"},
		})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.FindPRByBranch(context.Background(), "org/repo", "feat/ASE-47-reconnect")
	if err != nil {
		t.Fatalf("FindPRByBranch: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PR, got nil")
	}
	if pr.State != "open" {
		t.Errorf("State = %q, want open", pr.State)
	}
}

func TestForgejoFindPRByBranch_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]forgejoPR{})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.FindPRByBranch(context.Background(), "org/repo", "feat/no-pr")
	if err != nil {
		t.Fatalf("FindPRByBranch: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil, got %+v", pr)
	}
}

func TestGitHubFindPRByBranch_Merged(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]githubPR{
			{Number: 10, State: "closed", Merged: true, HTMLURL: "https://github.com/o/r/pull/10"},
		})
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	pr, err := client.FindPRByBranch(context.Background(), "owner/repo", "feat/ISSUE-1-test")
	if err != nil {
		t.Fatalf("FindPRByBranch: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PR, got nil")
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
}

func TestForgejoAuthError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "bad-token", authScheme: "token", httpClient: ts.Client()}}
	_, err := client.ListIssues(context.Background(), "org/repo")
	if err == nil {
		t.Fatal("expected error for 401")
	}
}
