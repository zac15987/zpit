package tui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/worktree"
)

// makeTestAppState creates a minimal AppState for testing.
func makeTestAppState() *AppState {
	cfg := &config.Config{
		Worktree: config.WorktreeConfig{MaxPerProject: 3},
	}
	return NewAppState(cfg, nil, nil, nil, nil, worktree.HookScripts{}, nil)
}

func makeArtifactEvent(issueID, artType, content string) broker.Event {
	art := broker.Artifact{
		IssueID:   issueID,
		Type:      artType,
		Content:   content,
		Timestamp: time.Now(),
	}
	payload, _ := json.Marshal(art)
	return broker.Event{Type: "artifact", Payload: payload}
}

func makeMessageEvent(from, to, content string) broker.Event {
	msg := broker.Message{
		From:      from,
		To:        to,
		Content:   content,
		Timestamp: time.Now(),
	}
	payload, _ := json.Marshal(msg)
	return broker.Event{Type: "message", Payload: payload}
}

// --- AC-3: AppState channelEvents tests ---

func TestAppendChannelEvent(t *testing.T) {
	state := makeTestAppState()

	ev := makeArtifactEvent("42", "interface", "type Foo struct{}")
	state.AppendChannelEvent("proj1", ev)

	events := state.ChannelEvents("proj1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "artifact" {
		t.Errorf("expected type artifact, got %s", events[0].Type)
	}
}

func TestChannelEventsReturnsCopy(t *testing.T) {
	state := makeTestAppState()

	ev := makeArtifactEvent("42", "interface", "type Foo struct{}")
	state.AppendChannelEvent("proj1", ev)

	events := state.ChannelEvents("proj1")
	events[0].Type = "modified"

	original := state.ChannelEvents("proj1")
	if original[0].Type != "artifact" {
		t.Errorf("ChannelEvents should return a copy; original was modified")
	}
}

func TestChannelEventsEmptyProject(t *testing.T) {
	state := makeTestAppState()

	events := state.ChannelEvents("nonexistent")
	if events != nil {
		t.Errorf("expected nil for nonexistent project, got %v", events)
	}
}

func TestAppendChannelEventMultipleProjects(t *testing.T) {
	state := makeTestAppState()

	state.AppendChannelEvent("proj1", makeArtifactEvent("42", "interface", "content1"))
	state.AppendChannelEvent("proj2", makeMessageEvent("43", "42", "hello"))
	state.AppendChannelEvent("proj1", makeMessageEvent("42", "43", "reply"))

	proj1Events := state.ChannelEvents("proj1")
	if len(proj1Events) != 2 {
		t.Fatalf("expected 2 events for proj1, got %d", len(proj1Events))
	}

	proj2Events := state.ChannelEvents("proj2")
	if len(proj2Events) != 1 {
		t.Fatalf("expected 1 event for proj2, got %d", len(proj2Events))
	}
}

// --- AC-6: countChannelEvents tests ---

func TestCountChannelEvents(t *testing.T) {
	events := []broker.Event{
		makeArtifactEvent("42", "interface", "type A"),
		makeArtifactEvent("42", "schema", "schema B"),
		makeArtifactEvent("43", "interface", "type C"),
		makeMessageEvent("42", "43", "hey"),
		makeMessageEvent("43", "42", "reply"),
	}

	artCount, msgCount := countChannelEvents(events, "42")
	if artCount != 2 {
		t.Errorf("expected 2 artifacts for issue 42, got %d", artCount)
	}
	if msgCount != 2 {
		t.Errorf("expected 2 messages for issue 42, got %d", msgCount)
	}

	artCount43, msgCount43 := countChannelEvents(events, "43")
	if artCount43 != 1 {
		t.Errorf("expected 1 artifact for issue 43, got %d", artCount43)
	}
	if msgCount43 != 2 {
		t.Errorf("expected 2 messages for issue 43, got %d", msgCount43)
	}
}

func TestCountChannelEventsZero(t *testing.T) {
	artCount, msgCount := countChannelEvents(nil, "99")
	if artCount != 0 || msgCount != 0 {
		t.Errorf("expected 0/0 for nil events, got %d/%d", artCount, msgCount)
	}

	artCount, msgCount = countChannelEvents([]broker.Event{}, "99")
	if artCount != 0 || msgCount != 0 {
		t.Errorf("expected 0/0 for empty events, got %d/%d", artCount, msgCount)
	}
}

// --- AC-7: formatChannelEvent tests ---

func TestFormatChannelEventArtifact(t *testing.T) {
	ev := makeArtifactEvent("42", "interface", "type Foo struct{}")
	result := formatChannelEvent(ev, "")
	if result == "" {
		t.Error("expected non-empty result")
	}
	// Should contain the issue ID and icon
	if !containsStr(result, "#42") {
		t.Error("expected result to contain #42")
	}
}

func TestFormatChannelEventMessage(t *testing.T) {
	ev := makeMessageEvent("43", "42", "need your interface def")
	result := formatChannelEvent(ev, "")
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !containsStr(result, "#43") {
		t.Error("expected result to contain #43")
	}
}

// --- AC-7: truncateChannel tests ---

func TestTruncateChannel(t *testing.T) {
	short := "hello"
	if truncateChannel(short, 120) != "hello" {
		t.Errorf("short string should not be truncated")
	}

	long := string(make([]rune, 200))
	for i := range long {
		_ = i
	}
	// Use actual string
	longStr := "a"
	for i := 0; i < 199; i++ {
		longStr += "b"
	}
	result := truncateChannel(longStr, 120)
	runes := []rune(result)
	// 120 chars + "..."
	if len(runes) != 123 {
		t.Errorf("expected 123 runes (120 + ...), got %d", len(runes))
	}
}

func TestTruncateChannelCollapsesWhitespace(t *testing.T) {
	input := "hello\n  world\t\tfoo"
	result := truncateChannel(input, 120)
	if result != "hello world foo" {
		t.Errorf("expected collapsed whitespace, got %q", result)
	}
}

// --- AC-1/AC-2: ViewChannel enum and key binding tests ---

func TestViewChannelEnum(t *testing.T) {
	// Verify ViewChannel is the third view (after ViewProjects=0, ViewStatus=1).
	if ViewChannel != 2 {
		t.Errorf("expected ViewChannel=2, got %d", ViewChannel)
	}
}

func TestChannelKeyBinding(t *testing.T) {
	km := DefaultKeyMap()
	if km.Channel.Keys() == nil || len(km.Channel.Keys()) == 0 {
		t.Error("Channel key binding should have keys")
	}
	if km.Channel.Keys()[0] != "m" {
		t.Errorf("expected Channel key 'm', got %q", km.Channel.Keys()[0])
	}
}

// containsStr is a helper that checks if a string contains a substring.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && findSubstr(s, substr)
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
