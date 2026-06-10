package zplex_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zac15987/zpit/internal/zplex"
)

// TestHealth_OK verifies that Health returns nil when the daemon responds 200.
func TestHealth_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := zplex.NewWithBaseURL(srv.URL)
	if err := c.Health(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestHealth_NonOK verifies that Health returns an error when the daemon
// responds with a non-200 status code.
func TestHealth_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := zplex.NewWithBaseURL(srv.URL)
	if err := c.Health(); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// TestCreateSession verifies method, path, request body fields, and that the
// returned id matches the daemon's JSON response.
func TestCreateSession(t *testing.T) {
	const wantID = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/sessions" {
			t.Errorf("expected path /api/sessions, got %s", r.URL.Path)
		}

		var body zplex.CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body.Shell != "/bin/bash" {
			t.Errorf("expected shell=/bin/bash, got %q", body.Shell)
		}
		if body.Title != "test-session" {
			t.Errorf("expected title=test-session, got %q", body.Title)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":     wantID,
			"ws_url": "/ws/" + wantID,
		})
	}))
	defer srv.Close()

	c := zplex.NewWithBaseURL(srv.URL)
	gotID, err := c.CreateSession(zplex.CreateSessionRequest{
		Shell: "/bin/bash",
		Title: "test-session",
	})
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if gotID != wantID {
		t.Errorf("expected id=%q, got %q", wantID, gotID)
	}
}

// TestPatchAgentState verifies method, path, and that the request body
// contains the expected agent_state value.
func TestPatchAgentState(t *testing.T) {
	const sessionID = "abc123"
	const wantState = "waiting"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		wantPath := "/api/sessions/" + sessionID
		if r.URL.Path != wantPath {
			t.Errorf("expected path %s, got %s", wantPath, r.URL.Path)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body["agent_state"] != wantState {
			t.Errorf("expected agent_state=%q, got %q", wantState, body["agent_state"])
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := zplex.NewWithBaseURL(srv.URL)
	if err := c.PatchAgentState(sessionID, wantState); err != nil {
		t.Fatalf("PatchAgentState returned error: %v", err)
	}
}
