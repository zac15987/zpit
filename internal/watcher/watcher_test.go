package watcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --- EncodeCwd ---

func TestEncodeCwd(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"windows drive path", `D:\Documents\MyProjects\zpit`, "D--Documents-MyProjects-zpit"},
		{"wsl path", "/mnt/d/Documents/MyProjects/zpit", "-mnt-d-Documents-MyProjects-zpit"},
		{"linux home", "/home/user/project", "-home-user-project"},
		{"path with dots", "/home/user/.config/zpit", "-home-user--config-zpit"},
		{"path with spaces", `C:\My Projects\app`, "C--My-Projects-app"},
		{"path with hyphens preserved", "/home/user/my-project", "-home-user-my-project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeCwd(tt.path)
			if got != tt.want {
				t.Errorf("EncodeCwd(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// --- ParseLine ---

func TestParseLine_AssistantEndTurn(t *testing.T) {
	line := `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"Should I use option A or B?"}]}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine failed: %v", err)
	}
	if ev.Type != "assistant" {
		t.Errorf("Type = %q, want %q", ev.Type, "assistant")
	}
	if ev.State != StateWaiting {
		t.Errorf("State = %v, want StateWaiting", ev.State)
	}
	if ev.QuestionText != "Should I use option A or B?" {
		t.Errorf("QuestionText = %q", ev.QuestionText)
	}
}

func TestParseLine_AssistantToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_01","name":"Read","input":{}}]}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine failed: %v", err)
	}
	if ev.State != StateWorking {
		t.Errorf("State = %v, want StateWorking", ev.State)
	}
}

func TestParseLine_Streaming(t *testing.T) {
	line := `{"type":"assistant","message":{"stop_reason":null,"content":[{"type":"thinking","thinking":"analyzing..."}]}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine failed: %v", err)
	}
	if ev.State != StateStreaming {
		t.Errorf("State = %v, want StateStreaming", ev.State)
	}
}

func TestParseLine_UserMessage(t *testing.T) {
	line := `{"type":"user","message":{"role":"user","content":"hello"}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine failed: %v", err)
	}
	if ev.Type != "user" {
		t.Errorf("Type = %q, want %q", ev.Type, "user")
	}
	if ev.State != StateUnknown {
		t.Errorf("State = %v, want StateUnknown", ev.State)
	}
}

func TestParseLine_SystemMessage(t *testing.T) {
	line := `{"type":"system","subtype":"turn_duration","durationMs":5000}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine failed: %v", err)
	}
	if ev.Type != "system" {
		t.Errorf("Type = %q, want %q", ev.Type, "system")
	}
}

func TestParseLine_InvalidJSON(t *testing.T) {
	_, err := ParseLine([]byte("not json at all"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseLine_MultipleTextBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"First paragraph."},{"type":"text","text":"The actual question?"}]}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine failed: %v", err)
	}
	if ev.QuestionText != "The actual question?" {
		t.Errorf("QuestionText = %q, want last text block", ev.QuestionText)
	}
}

func TestParseLine_Timestamp(t *testing.T) {
	line := `{"type":"user","timestamp":"2026-03-20T10:00:00.000Z","message":{"role":"user","content":"hi"}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine failed: %v", err)
	}
	if ev.Timestamp.Year() != 2026 {
		t.Errorf("Timestamp year = %d, want 2026", ev.Timestamp.Year())
	}
}

// --- LogFilePath ---

func TestLogFilePath(t *testing.T) {
	home := "/home/user/.claude"
	projectPath := "/home/user/my-project"
	sessionID := "abc-123"

	got := LogFilePath(home, projectPath, sessionID)

	wantEncoded := EncodeCwd(projectPath)
	want := filepath.Join(home, "projects", wantEncoded, "abc-123.jsonl")
	if got != want {
		t.Errorf("LogFilePath = %q, want %q", got, want)
	}
}

// --- FindActiveSessions ---

func TestFindActiveSessions(t *testing.T) {
	// Create a temp dir mimicking ~/.claude/sessions/
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Current PID is guaranteed alive.
	alivePID := os.Getpid()
	deadPID := 99999999

	projectPath := `D:\Documents\MyProjects\zpit`
	if runtime.GOOS != "windows" {
		projectPath = "/home/user/zpit"
	}

	// Write an alive session matching our project.
	writeSessionJSON(t, sessDir, "1.json", SessionInfo{
		PID:       alivePID,
		SessionID: "session-alive",
		Cwd:       projectPath,
		StartedAt: 1000,
	})

	// Write a dead session matching our project.
	writeSessionJSON(t, sessDir, "2.json", SessionInfo{
		PID:       deadPID,
		SessionID: "session-dead",
		Cwd:       projectPath,
		StartedAt: 2000,
	})

	// Write an alive session for a different project.
	writeSessionJSON(t, sessDir, "3.json", SessionInfo{
		PID:       alivePID,
		SessionID: "session-other",
		Cwd:       "/other/project",
		StartedAt: 3000,
	})

	// Override processAliveFunc for testing.
	origFunc := processAliveFunc
	processAliveFunc = func(pid int) bool { return pid == alivePID }
	defer func() { processAliveFunc = origFunc }()

	sessions, err := FindActiveSessions(tmpDir, projectPath)
	if err != nil {
		t.Fatalf("FindActiveSessions failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].SessionID != "session-alive" {
		t.Errorf("SessionID = %q, want %q", sessions[0].SessionID, "session-alive")
	}
}

func TestFindActiveSessions_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessions, err := FindActiveSessions(tmpDir, "/some/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sessions))
	}
}

func TestFindActiveSessions_MissingDir(t *testing.T) {
	_, err := FindActiveSessions("/nonexistent", "/some/project")
	if err == nil {
		t.Error("expected error for missing sessions directory")
	}
}

func writeSessionJSON(t *testing.T, dir, name string, info SessionInfo) {
	t.Helper()
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- AgentState.String ---

func TestAgentState_String(t *testing.T) {
	tests := []struct {
		state AgentState
		want  string
	}{
		{StateUnknown, "Unknown"},
		{StateWorking, "Working"},
		{StateWaiting, "Waiting"},
		{StateStreaming, "Streaming"},
		{StateEnded, "Ended"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("AgentState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// --- IsProcessAlive ---

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	// Current PID is always alive.
	if !IsProcessAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

func TestIsProcessAlive_DeadProcess(t *testing.T) {
	origFunc := processAliveFunc
	processAliveFunc = func(pid int) bool { return false }
	defer func() { processAliveFunc = origFunc }()

	if IsProcessAlive(99999999) {
		t.Error("dead PID should not be alive")
	}
}
