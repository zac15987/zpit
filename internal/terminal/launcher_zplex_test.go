// Package terminal — white-box tests for the zplex backend launch functions.
// Tests use httptest.NewServer to stand up a fake zplex daemon and verify:
//   (a) fallback gate: zplexClient returns nil when health check fails or port is 0
//   (b) zero HTTP requests when ZplexPort == 0
//   (c) POST body contents for each entry-point variant
package terminal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
)

// zplexTestServer stands up a fake zplex daemon.
// It responds 200 on GET /api/health and 201 on POST /api/sessions with
// {"id":"sess-1","ws_url":"/ws/sess-1"}, capturing the last POST body.
type zplexTestServer struct {
	srv     *httptest.Server
	lastReq []byte
	reqCount atomic.Int64
}

func newZplexTestServer(t *testing.T) *zplexTestServer {
	t.Helper()
	ts := &zplexTestServer{}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.reqCount.Add(1)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/health":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions":
			var buf []byte
			if r.Body != nil {
				buf = make([]byte, 0, 512)
				tmp := make([]byte, 512)
				for {
					n, err := r.Body.Read(tmp)
					buf = append(buf, tmp[:n]...)
					if err != nil {
						break
					}
				}
			}
			ts.lastReq = buf
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"sess-1","ws_url":"/ws/sess-1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

// port returns the TCP port of the test server.
func (ts *zplexTestServer) port() int {
	u, _ := url.Parse(ts.srv.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

// decodeLastBody decodes the captured POST body into a map.
func (ts *zplexTestServer) decodeLastBody(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(ts.lastReq, &m); err != nil {
		t.Fatalf("decode POST body: %v (raw: %s)", err, ts.lastReq)
	}
	return m
}

// newFailingHealthServer creates a test server that always returns 500 on /api/health.
func newFailingHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- (a) Fallback gate tests ---

// TestZplexClient_HealthFail verifies that zplexClient returns nil when the
// server responds with a non-200 status on GET /api/health.
func TestZplexClient_HealthFail(t *testing.T) {
	srv := newFailingHealthServer(t)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	cfg := config.TerminalConfig{ZplexPort: port}
	c := zplexClient(cfg)
	if c != nil {
		t.Errorf("zplexClient expected nil on health-fail server, got non-nil")
	}
}

// TestZplexClient_UnreachablePort verifies that zplexClient returns nil when the
// port is unreachable (connection refused / timeout).
func TestZplexClient_UnreachablePort(t *testing.T) {
	// Use port 1 — a well-known reserved port that is always closed on localhost.
	// The 500 ms health timeout will expire quickly.
	cfg := config.TerminalConfig{ZplexPort: 1}
	c := zplexClient(cfg)
	if c != nil {
		t.Errorf("zplexClient expected nil for unreachable port, got non-nil")
	}
}

// --- (b) Zero-port: no HTTP requests ---

// TestZplexClient_ZeroPort verifies that zplexClient returns nil immediately
// and makes ZERO HTTP requests when ZplexPort == 0.
func TestZplexClient_ZeroPort(t *testing.T) {
	var reqCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reqCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := config.TerminalConfig{ZplexPort: 0}
	c := zplexClient(cfg)
	if c != nil {
		t.Errorf("zplexClient with ZplexPort=0 expected nil, got non-nil")
	}
	if n := reqCount.Load(); n != 0 {
		t.Errorf("expected zero HTTP requests for ZplexPort=0, got %d", n)
	}
}

// --- (c) POST body tests ---

// makeProject returns a minimal ProjectConfig for POST body tests.
func makeProject() config.ProjectConfig {
	return config.ProjectConfig{
		Name: "Proj",
		ID:   "proj1",
		Path: config.ProjectPathConfig{
			Windows: "D:/x",
			WSL:     "/x",
		},
	}
}

// TestZplexPostBody_AgentWithEnvInjection verifies the POST body for an agent
// session that requires env injection (needsAgentEnv == true, e.g. "clarifier").
// AC-15(c): body.source=="zpit", body.title=="Proj", body.project_id=="proj1",
// body.role=="clarifier", body.agent_state=="active",
// body.env contains "ZPIT_AGENT=1" and "ZPIT_AGENT_TYPE=clarifier",
// body.shell=="claude"; result.Env==EnvZplex, result.ZplexSessionID=="sess-1".
func TestZplexPostBody_AgentWithEnvInjection(t *testing.T) {
	ts := newZplexTestServer(t)
	cfg := config.TerminalConfig{ZplexPort: ts.port()}
	project := makeProject()

	result, err := LaunchClaude(project, cfg, "--agent", "clarifier")
	if err != nil {
		t.Fatalf("LaunchClaude: %v", err)
	}

	body := ts.decodeLastBody(t)

	assertEqual(t, "source", "zpit", body["source"])
	assertEqual(t, "title", "Proj", body["title"])
	assertEqual(t, "project_id", "proj1", body["project_id"])
	assertEqual(t, "role", "clarifier", body["role"])
	assertEqual(t, "agent_state", "active", body["agent_state"])
	assertEqual(t, "shell", "claude", body["shell"])

	// env must contain ZPIT_AGENT=1 and ZPIT_AGENT_TYPE=clarifier
	envList := toStringSlice(t, body["env"])
	assertContains(t, "env", envList, "ZPIT_AGENT=1")
	assertContains(t, "env", envList, "ZPIT_AGENT_TYPE=clarifier")

	// result assertions
	if result.Env != platform.EnvZplex {
		t.Errorf("result.Env: got %v, want %v", result.Env, platform.EnvZplex)
	}
	if result.ZplexSessionID != "sess-1" {
		t.Errorf("result.ZplexSessionID: got %q, want %q", result.ZplexSessionID, "sess-1")
	}
}

// TestZplexPostBody_EfficiencyAgentNoEnv verifies the POST body for the efficiency
// agent, which must NOT inject env vars (needsAgentEnv == false).
// AC-15(c): body.role=="efficiency", body.agent_state=="active", body.env absent/empty.
func TestZplexPostBody_EfficiencyAgentNoEnv(t *testing.T) {
	ts := newZplexTestServer(t)
	cfg := config.TerminalConfig{ZplexPort: ts.port()}
	project := makeProject()

	result, err := LaunchClaude(project, cfg, "--agent", "efficiency")
	if err != nil {
		t.Fatalf("LaunchClaude (efficiency): %v", err)
	}

	body := ts.decodeLastBody(t)

	assertEqual(t, "role", "efficiency", body["role"])
	assertEqual(t, "agent_state", "active", body["agent_state"])

	// env must be absent or empty
	if env, ok := body["env"]; ok && env != nil {
		envList := toStringSlice(t, env)
		if len(envList) != 0 {
			t.Errorf("efficiency agent: expected env absent/empty, got %v", envList)
		}
	}

	if result.Env != platform.EnvZplex {
		t.Errorf("result.Env: got %v, want %v", result.Env, platform.EnvZplex)
	}
	if result.ZplexSessionID != "sess-1" {
		t.Errorf("result.ZplexSessionID: got %q, want %q", result.ZplexSessionID, "sess-1")
	}
}

// TestZplexPostBody_DesktopAgentNoEnv verifies the POST body for the desktop
// agent, which must NOT inject env vars (needsAgentEnv == false).
func TestZplexPostBody_DesktopAgentNoEnv(t *testing.T) {
	ts := newZplexTestServer(t)
	cfg := config.TerminalConfig{ZplexPort: ts.port()}
	project := makeProject()

	_, err := LaunchClaude(project, cfg, "--agent", "desktop")
	if err != nil {
		t.Fatalf("LaunchClaude (desktop): %v", err)
	}

	body := ts.decodeLastBody(t)

	assertEqual(t, "role", "desktop", body["role"])
	assertEqual(t, "agent_state", "active", body["agent_state"])

	// env must be absent or empty
	if env, ok := body["env"]; ok && env != nil {
		envList := toStringSlice(t, env)
		if len(envList) != 0 {
			t.Errorf("desktop agent: expected env absent/empty, got %v", envList)
		}
	}
}

// TestZplexPostBody_PlainSession verifies the POST body for a plain claude session
// (no --agent flag).
// AC-15(c): body.role=="", body.agent_state=="", body.env empty, body.shell=="claude".
func TestZplexPostBody_PlainSession(t *testing.T) {
	ts := newZplexTestServer(t)
	cfg := config.TerminalConfig{ZplexPort: ts.port()}
	project := makeProject()

	result, err := LaunchClaude(project, cfg)
	if err != nil {
		t.Fatalf("LaunchClaude (plain): %v", err)
	}

	body := ts.decodeLastBody(t)

	assertEqual(t, "shell", "claude", body["shell"])

	// role and agent_state must be absent (omitempty) or empty string
	if role, ok := body["role"]; ok && role != "" {
		t.Errorf("plain session: expected role absent/empty, got %v", role)
	}
	if state, ok := body["agent_state"]; ok && state != "" {
		t.Errorf("plain session: expected agent_state absent/empty, got %v", state)
	}

	// env must be absent or empty
	if env, ok := body["env"]; ok && env != nil {
		envList := toStringSlice(t, env)
		if len(envList) != 0 {
			t.Errorf("plain session: expected env absent/empty, got %v", envList)
		}
	}

	if result.Env != platform.EnvZplex {
		t.Errorf("result.Env: got %v, want %v", result.Env, platform.EnvZplex)
	}
}

// TestZplexPostBody_Lazygit verifies the POST body for a lazygit session.
// AC-15(c): body.shell=="lazygit", body.cwd=="D:/work", body.title=="lazygit — Proj",
// body.source=="zpit", body.env absent/empty, body.role absent/empty,
// body.agent_state absent/empty.
func TestZplexPostBody_Lazygit(t *testing.T) {
	ts := newZplexTestServer(t)
	cfg := config.TerminalConfig{ZplexPort: ts.port()}

	result, err := LaunchLazygit("D:/work", "lazygit — Proj", cfg)
	if err != nil {
		t.Fatalf("LaunchLazygit: %v", err)
	}

	body := ts.decodeLastBody(t)

	assertEqual(t, "shell", "lazygit", body["shell"])
	assertEqual(t, "cwd", "D:/work", body["cwd"])
	assertEqual(t, "title", "lazygit — Proj", body["title"])
	assertEqual(t, "source", "zpit", body["source"])

	// env must be absent or empty
	if env, ok := body["env"]; ok && env != nil {
		envList := toStringSlice(t, env)
		if len(envList) != 0 {
			t.Errorf("lazygit: expected env absent/empty, got %v", envList)
		}
	}
	if role, ok := body["role"]; ok && role != "" {
		t.Errorf("lazygit: expected role absent/empty, got %v", role)
	}
	if state, ok := body["agent_state"]; ok && state != "" {
		t.Errorf("lazygit: expected agent_state absent/empty, got %v", state)
	}

	if result.Env != platform.EnvZplex {
		t.Errorf("result.Env: got %v, want %v", result.Env, platform.EnvZplex)
	}
	if result.ZplexSessionID != "sess-1" {
		t.Errorf("result.ZplexSessionID: got %q, want %q", result.ZplexSessionID, "sess-1")
	}
}

// TestZplexPostBody_ClaudeUpdate verifies the POST body for a claude update session.
// AC-15(c): body.shell is "cmd" on Windows else "sh"; body.title=="claude update";
// body.source=="zpit"; body.role/agent_state absent/empty.
// On Windows: body.args == ["/c","claude update & pause"].
func TestZplexPostBody_ClaudeUpdate(t *testing.T) {
	ts := newZplexTestServer(t)
	cfg := config.TerminalConfig{ZplexPort: ts.port()}

	result, err := LaunchClaudeUpdate(cfg)
	if err != nil {
		t.Fatalf("LaunchClaudeUpdate: %v", err)
	}

	body := ts.decodeLastBody(t)

	assertEqual(t, "title", "claude update", body["title"])
	assertEqual(t, "source", "zpit", body["source"])

	if role, ok := body["role"]; ok && role != "" {
		t.Errorf("claude update: expected role absent/empty, got %v", role)
	}
	if state, ok := body["agent_state"]; ok && state != "" {
		t.Errorf("claude update: expected agent_state absent/empty, got %v", state)
	}

	if runtime.GOOS == "windows" {
		assertEqual(t, "shell", "cmd", body["shell"])
		// args must be ["/c", "claude update & pause"]
		args := toStringSlice(t, body["args"])
		if len(args) != 2 || args[0] != "/c" || args[1] != "claude update & pause" {
			t.Errorf("claude update (windows): expected args=[\"/c\",\"claude update & pause\"], got %v", args)
		}
	} else {
		assertEqual(t, "shell", "sh", body["shell"])
	}

	if result.Env != platform.EnvZplex {
		t.Errorf("result.Env: got %v, want %v", result.Env, platform.EnvZplex)
	}
	if result.ZplexSessionID != "sess-1" {
		t.Errorf("result.ZplexSessionID: got %q, want %q", result.ZplexSessionID, "sess-1")
	}
}

// TestZplexPostBody_LoopMetadata verifies the POST body for a loop-initiated
// LaunchClaudeInDir call, which carries project_id, issue_id, role, and agent_state.
// AC-15(c): body.project_id=="proj1", body.issue_id=="42",
// body.role=="coding", body.agent_state=="active".
func TestZplexPostBody_LoopMetadata(t *testing.T) {
	ts := newZplexTestServer(t)
	cfg := config.TerminalConfig{ZplexPort: ts.port()}

	meta := SessionMeta{ProjectID: "proj1", IssueID: "42"}
	result, err := LaunchClaudeInDir("D:/wt", "Proj #42", cfg, meta, "--agent", "coding", "init")
	if err != nil {
		t.Fatalf("LaunchClaudeInDir: %v", err)
	}

	body := ts.decodeLastBody(t)

	assertEqual(t, "project_id", "proj1", body["project_id"])
	assertEqual(t, "issue_id", "42", body["issue_id"])
	assertEqual(t, "role", "coding", body["role"])
	assertEqual(t, "agent_state", "active", body["agent_state"])

	if result.Env != platform.EnvZplex {
		t.Errorf("result.Env: got %v, want %v", result.Env, platform.EnvZplex)
	}
	if result.ZplexSessionID != "sess-1" {
		t.Errorf("result.ZplexSessionID: got %q, want %q", result.ZplexSessionID, "sess-1")
	}
}

// --- Test helpers ---

// assertEqual reports a test failure if got != want (string comparison via fmt).
func assertEqual(t *testing.T, field string, want any, got any) {
	t.Helper()
	wantStr := toString(want)
	gotStr := toString(got)
	if wantStr != gotStr {
		t.Errorf("%s: got %q, want %q", field, gotStr, wantStr)
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// toStringSlice converts a JSON-decoded []any to []string.
func toStringSlice(t *testing.T, v any) []string {
	t.Helper()
	if v == nil {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("toStringSlice: expected []any, got %T (%v)", v, v)
	}
	result := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("toStringSlice[%d]: expected string, got %T", i, item)
		}
		result[i] = s
	}
	return result
}

// assertContains checks that the slice contains the expected value.
func assertContains(t *testing.T, field string, slice []string, expected string) {
	t.Helper()
	for _, s := range slice {
		if s == expected {
			return
		}
	}
	t.Errorf("%s: expected to contain %q, got %v", field, expected, slice)
}
