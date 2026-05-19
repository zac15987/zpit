package tracker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
			{Number: 5, State: "open", Merged: false, HTMLURL: "http://git.local/o/r/pulls/5",
				Head: forgejoPRRef{Ref: "feat/ASE-47-reconnect"}},
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

func TestForgejoFindPRByBranch_WrongBranch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API returns a PR but head.ref doesn't match the requested branch
		json.NewEncoder(w).Encode([]forgejoPR{
			{Number: 99, State: "open", Merged: false, HTMLURL: "http://git.local/o/r/pulls/99",
				Head: forgejoPRRef{Ref: "feat/other-branch"}},
		})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.FindPRByBranch(context.Background(), "org/repo", "feat/ASE-47-reconnect")
	if err != nil {
		t.Fatalf("FindPRByBranch: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil for wrong branch, got %+v", pr)
	}
}

func TestGitHubFindPRByBranch_Merged(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]githubPR{
			{Number: 10, State: "closed", Merged: true, HTMLURL: "https://github.com/o/r/pull/10",
				Head: githubPRRef{Ref: "feat/ISSUE-1-test"}},
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

func TestGitHubFindPRByBranch_MergedViaTimestamp(t *testing.T) {
	// GitHub list endpoint returns "merged":null but includes "merged_at".
	mergedAt := "2026-01-01T00:00:00Z"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]githubPR{
			{Number: 9, State: "closed", Merged: false, MergedAt: &mergedAt, HTMLURL: "https://github.com/o/r/pull/9",
				Head: githubPRRef{Ref: "feat/6-test"}},
		})
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	pr, err := client.FindPRByBranch(context.Background(), "owner/repo", "feat/6-test")
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

// --- CloseIssue tests ---

func TestGitHubCloseIssue(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotBody struct {
		State string `json:"state"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	err := client.CloseIssue(context.Background(), "owner/repo", "42")
	if err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if gotMethod != "PATCH" {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/repos/owner/repo/issues/42" {
		t.Errorf("path = %q, want /repos/owner/repo/issues/42", gotPath)
	}
	if gotBody.State != "closed" {
		t.Errorf("body.state = %q, want closed", gotBody.State)
	}
}

func TestForgejoCloseIssue(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotBody struct {
		State string `json:"state"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	err := client.CloseIssue(context.Background(), "org/repo", "15")
	if err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if gotMethod != "PATCH" {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/repos/org/repo/issues/15" {
		t.Errorf("path = %q, want /api/v1/repos/org/repo/issues/15", gotPath)
	}
	if gotBody.State != "closed" {
		t.Errorf("body.state = %q, want closed", gotBody.State)
	}
}

func TestGitHubCloseIssue_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	err := client.CloseIssue(context.Background(), "owner/repo", "999")
	if err == nil {
		t.Fatal("expected error for 404")
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

// --- MergePR tests ---

// githubMergeServer builds a test server that handles the PUT merge call and
// the follow-up GET PR re-fetch, asserting the body sent with PUT matches.
func githubMergeServer(t *testing.T, wantMethod, wantCommitTitle string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT" && r.URL.Path == "/repos/owner/repo/pulls/42/merge":
			var got struct {
				MergeMethod string `json:"merge_method"`
				CommitTitle string `json:"commit_title"`
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &got); err != nil {
				t.Errorf("unmarshal PUT body: %v", err)
			}
			if got.MergeMethod != wantMethod {
				t.Errorf("merge_method = %q, want %q", got.MergeMethod, wantMethod)
			}
			if got.CommitTitle != wantCommitTitle {
				t.Errorf("commit_title = %q, want %q", got.CommitTitle, wantCommitTitle)
			}
			w.WriteHeader(200)
			w.Write([]byte(`{"merged":true,"sha":"abc"}`))
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls/42":
			json.NewEncoder(w).Encode(githubPR{
				Number: 42, State: "closed", Merged: true,
				HTMLURL: "https://github.com/owner/repo/pull/42",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

func TestGitHubMergePR_Success_Squash(t *testing.T) {
	ts := githubMergeServer(t, "squash", "[42] Test title")
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	pr, err := client.MergePR(context.Background(), "owner/repo", "42", "squash", "[42] Test title")
	if err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
	if pr.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("URL = %q", pr.URL)
	}
	if pr.ID != "42" {
		t.Errorf("ID = %q, want 42", pr.ID)
	}
}

func TestGitHubMergePR_Success_Merge(t *testing.T) {
	ts := githubMergeServer(t, "merge", "[42] Test title")
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	pr, err := client.MergePR(context.Background(), "owner/repo", "42", "merge", "[42] Test title")
	if err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
	if pr.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("URL = %q", pr.URL)
	}
}

func TestGitHubMergePR_Success_Rebase(t *testing.T) {
	ts := githubMergeServer(t, "rebase", "[42] Test title")
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	pr, err := client.MergePR(context.Background(), "owner/repo", "42", "rebase", "[42] Test title")
	if err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
}

func TestGitHubMergePR_Conflict(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		w.Write([]byte(`{"message":"merge conflict"}`))
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	_, err := client.MergePR(context.Background(), "owner/repo", "42", "squash", "[42] Test")
	if err == nil {
		t.Fatal("expected error for 409")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error = %q, want to contain 409", err.Error())
	}
}

func TestGitHubMergePR_AuthError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	_, err := client.MergePR(context.Background(), "owner/repo", "42", "squash", "[42] Test")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %q, want to contain 403", err.Error())
	}
}

func TestGitHubMergePR_InvalidMethod(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(200)
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	_, err := client.MergePR(context.Background(), "owner/repo", "42", "bogus", "title")
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
	if calls != 0 {
		t.Errorf("expected 0 HTTP calls for invalid method, got %d", calls)
	}
}

// forgejoMergeServer handles only the POST merge call (empty-body 200
// response), asserting the body sent with POST matches. Per AC-6 the client
// does NOT re-fetch — any GET request here is a bug and fails the test.
func forgejoMergeServer(t *testing.T, wantDo, wantMergeTitle string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/repos/org/repo/pulls/7/merge":
			var got struct {
				Do              string `json:"Do"`
				MergeTitleField string `json:"MergeTitleField"`
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &got); err != nil {
				t.Errorf("unmarshal POST body: %v", err)
			}
			if got.Do != wantDo {
				t.Errorf("Do = %q, want %q", got.Do, wantDo)
			}
			if got.MergeTitleField != wantMergeTitle {
				t.Errorf("MergeTitleField = %q, want %q", got.MergeTitleField, wantMergeTitle)
			}
			// Forgejo returns HTTP 200 with empty body on success.
			w.WriteHeader(200)
		default:
			t.Errorf("unexpected request (AC-6 forbids re-fetch): %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

func TestForgejoMergePR_Success_Squash(t *testing.T) {
	ts := forgejoMergeServer(t, "squash", "[7] Test")
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.MergePR(context.Background(), "org/repo", "7", "squash", "[7] Test")
	if err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
	// AC-6: no re-fetch, so URL is intentionally empty from MergePR.
	if pr.URL != "" {
		t.Errorf("URL = %q, want empty (no re-fetch)", pr.URL)
	}
	if pr.ID != "7" {
		t.Errorf("ID = %q, want 7", pr.ID)
	}
}

func TestForgejoMergePR_Success_Rebase(t *testing.T) {
	ts := forgejoMergeServer(t, "rebase", "[7] Test")
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	pr, err := client.MergePR(context.Background(), "org/repo", "7", "rebase", "[7] Test")
	if err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
}

func TestForgejoMergePR_Conflict(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(409)
			w.Write([]byte(`{"message":"merge conflict"}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	_, err := client.MergePR(context.Background(), "org/repo", "7", "squash", "[7] Test")
	if err == nil {
		t.Fatal("expected error for 409")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error = %q, want to contain 409", err.Error())
	}
}

func TestForgejoMergePR_AuthError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(403)
			w.Write([]byte(`{"message":"Forbidden"}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	_, err := client.MergePR(context.Background(), "org/repo", "7", "squash", "[7] Test")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %q, want to contain 403", err.Error())
	}
}

func TestForgejoMergePR_InvalidMethod(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(200)
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	_, err := client.MergePR(context.Background(), "org/repo", "7", "bogus", "title")
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
	if calls != 0 {
		t.Errorf("expected 0 HTTP calls for invalid method, got %d", calls)
	}
}
