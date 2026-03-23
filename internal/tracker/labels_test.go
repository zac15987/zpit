package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- EnsureLabels unit tests (mock LabelManager) ---

type mockLabelManager struct {
	labels      []string
	listErr     error
	createErr   error
	createCalls []LabelDef
}

func (m *mockLabelManager) ListRepoLabels(ctx context.Context, repo string) ([]string, error) {
	return m.labels, m.listErr
}

func (m *mockLabelManager) CreateLabel(ctx context.Context, repo string, label LabelDef) error {
	m.createCalls = append(m.createCalls, label)
	return m.createErr
}

func TestEnsureLabels_AllExist(t *testing.T) {
	lm := &mockLabelManager{labels: []string{"pending", "todo", "wip", "review", "ai-review", "needs-changes"}}
	created, err := EnsureLabels(context.Background(), lm, "org/repo", RequiredLabels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(created) != 0 {
		t.Errorf("expected no labels created, got %v", created)
	}
	if len(lm.createCalls) != 0 {
		t.Errorf("expected no CreateLabel calls, got %d", len(lm.createCalls))
	}
}

func TestEnsureLabels_SomeMissing(t *testing.T) {
	lm := &mockLabelManager{labels: []string{"pending", "todo", "wip"}}
	created, err := EnsureLabels(context.Background(), lm, "org/repo", RequiredLabels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("expected 3 labels created, got %v", created)
	}
	want := map[string]bool{"review": true, "ai-review": true, "needs-changes": true}
	for _, name := range created {
		if !want[name] {
			t.Errorf("unexpected label created: %q", name)
		}
	}
}

func TestEnsureLabels_CaseInsensitive(t *testing.T) {
	lm := &mockLabelManager{labels: []string{"TODO", "Pending", "WIP", "Review", "AI-Review", "Needs-Changes"}}
	created, err := EnsureLabels(context.Background(), lm, "org/repo", RequiredLabels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(created) != 0 {
		t.Errorf("expected no labels created (case-insensitive), got %v", created)
	}
}

func TestEnsureLabels_ListFails(t *testing.T) {
	lm := &mockLabelManager{listErr: fmt.Errorf("network error")}
	_, err := EnsureLabels(context.Background(), lm, "org/repo", RequiredLabels)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(lm.createCalls) != 0 {
		t.Error("should not call CreateLabel when ListRepoLabels fails")
	}
}

func TestEnsureLabels_CreateFails(t *testing.T) {
	lm := &mockLabelManager{
		labels:    []string{"pending", "todo", "wip"},
		createErr: fmt.Errorf("forbidden"),
	}
	created, err := EnsureLabels(context.Background(), lm, "org/repo", RequiredLabels)
	if err == nil {
		t.Fatal("expected error")
	}
	// First missing label is "review" — CreateLabel fails immediately, no partial success.
	if len(created) != 0 {
		t.Errorf("expected no labels created before failure, got %v", created)
	}
}

// --- Forgejo httptest tests ---

func TestForgejoListRepoLabels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/org/repo/labels" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]forgejoLabel{
			{ID: 1, Name: "bug"},
			{ID: 2, Name: "todo"},
		})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	names, err := client.ListRepoLabels(context.Background(), "org/repo")
	if err != nil {
		t.Fatalf("ListRepoLabels: %v", err)
	}
	if len(names) != 2 || names[0] != "bug" || names[1] != "todo" {
		t.Errorf("got %v, want [bug todo]", names)
	}
}

func TestForgejoCreateLabel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/repos/org/repo/labels" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		json.Unmarshal(body, &req)
		if req.Name != "wip" {
			t.Errorf("name = %q, want wip", req.Name)
		}
		if req.Color != "#e4e669" {
			t.Errorf("color = %q, want #e4e669 (Forgejo keeps #)", req.Color)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(forgejoLabel{ID: 99, Name: "wip"})
	}))
	defer ts.Close()

	client := &ForgejoClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "token", httpClient: ts.Client()}}
	err := client.CreateLabel(context.Background(), "org/repo", LabelDef{Name: "wip", Color: "#e4e669"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
}

// --- GitHub httptest tests ---

func TestGitHubListRepoLabels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/labels" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]githubLabel{
			{ID: 1, Name: "enhancement"},
			{ID: 2, Name: "review"},
		})
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	names, err := client.ListRepoLabels(context.Background(), "org/repo")
	if err != nil {
		t.Fatalf("ListRepoLabels: %v", err)
	}
	if len(names) != 2 || names[0] != "enhancement" || names[1] != "review" {
		t.Errorf("got %v, want [enhancement review]", names)
	}
}

func TestGitHubCreateLabel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		json.Unmarshal(body, &req)
		if req.Name != "ai-review" {
			t.Errorf("name = %q, want ai-review", req.Name)
		}
		if req.Color != "0e8a16" {
			t.Errorf("color = %q, want 0e8a16 (GitHub strips #)", req.Color)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	client := &GitHubClient{restClient: restClient{baseURL: ts.URL, token: "t", authScheme: "Bearer", httpClient: ts.Client()}}
	err := client.CreateLabel(context.Background(), "org/repo", LabelDef{Name: "ai-review", Color: "#0e8a16"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
}
