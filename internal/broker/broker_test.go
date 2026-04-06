package broker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func newTestBroker(t *testing.T) *Broker {
	t.Helper()
	logger := log.New(io.Discard, "", 0)
	b, err := New(logger, 0) // port 0 = OS-assigned for test isolation
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func brokerURL(b *Broker) string {
	return "http://" + b.Addr()
}

func TestBroker_Addr(t *testing.T) {
	b := newTestBroker(t)
	addr := b.Addr()
	if addr == "" {
		t.Fatal("Addr() returned empty string")
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("expected localhost addr, got %s", addr)
	}
}

func TestBroker_PostAndListArtifacts(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// Post an artifact.
	body := `{"type":"interface","content":"type Foo struct{}"}`
	resp, err := http.Post(base+"/api/artifacts/proj1/issue-1", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST artifact: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	// Post another artifact.
	body2 := `{"type":"type","content":"type Bar int"}`
	resp2, err := http.Post(base+"/api/artifacts/proj1/issue-2", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatalf("POST artifact 2: %v", err)
	}
	resp2.Body.Close()

	// List artifacts.
	resp3, err := http.Get(base + "/api/artifacts/proj1")
	if err != nil {
		t.Fatalf("GET artifacts: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("GET status: got %d, want %d", resp3.StatusCode, http.StatusOK)
	}

	var arts []Artifact
	if err := json.NewDecoder(resp3.Body).Decode(&arts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}
	if arts[0].IssueID != "issue-1" || arts[0].Type != "interface" {
		t.Errorf("artifact 0: %+v", arts[0])
	}
	if arts[1].IssueID != "issue-2" || arts[1].Type != "type" {
		t.Errorf("artifact 1: %+v", arts[1])
	}
}

func TestBroker_ListArtifacts_Empty(t *testing.T) {
	b := newTestBroker(t)
	resp, err := http.Get(brokerURL(b) + "/api/artifacts/empty-project")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var arts []Artifact
	if err := json.NewDecoder(resp.Body).Decode(&arts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(arts) != 0 {
		t.Errorf("expected empty list, got %d", len(arts))
	}
}

func TestBroker_PostAndGetMessages(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// Send a message from issue-1 to issue-2.
	body := `{"from":"issue-1","content":"please use Foo interface"}`
	resp, err := http.Post(base+"/api/messages/proj1/issue-2", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST message: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	// Send another message from issue-3 to issue-2.
	body2 := `{"from":"issue-3","content":"FYI: type Bar changed"}`
	resp2, err := http.Post(base+"/api/messages/proj1/issue-2", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatalf("POST message 2: %v", err)
	}
	resp2.Body.Close()

	// Send a message to a different recipient (should not appear in issue-2 query).
	body3 := `{"from":"issue-1","content":"unrelated"}`
	resp3, err := http.Post(base+"/api/messages/proj1/issue-9", "application/json", strings.NewReader(body3))
	if err != nil {
		t.Fatalf("POST message 3: %v", err)
	}
	resp3.Body.Close()

	// Get messages for issue-2.
	resp4, err := http.Get(base + "/api/messages/proj1/issue-2")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp4.Body.Close()

	var msgs []Message
	if err := json.NewDecoder(resp4.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].From != "issue-1" || msgs[0].Content != "please use Foo interface" {
		t.Errorf("msg 0: %+v", msgs[0])
	}
}

func TestBroker_GetMessages_Empty(t *testing.T) {
	b := newTestBroker(t)
	resp, err := http.Get(brokerURL(b) + "/api/messages/proj1/no-one")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var msgs []Message
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty list, got %d", len(msgs))
	}
}

func TestBroker_SSE_InitialStateAndEvents(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// Pre-populate an artifact so SSE sends it as initial state.
	body := `{"type":"interface","content":"existing"}`
	resp, err := http.Post(base+"/api/artifacts/proj1/issue-1", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// Connect to SSE.
	sseResp, err := http.Get(base + "/api/events/proj1")
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer sseResp.Body.Close()

	if ct := sseResp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(sseResp.Body)

	// Read initial artifact event.
	event1 := readSSEEvent(t, scanner)
	if event1.Type != "artifact" {
		t.Errorf("initial event type: got %q, want artifact", event1.Type)
	}
	var art Artifact
	if err := json.Unmarshal(event1.Payload, &art); err != nil {
		t.Fatalf("unmarshal initial artifact: %v", err)
	}
	if art.Content != "existing" {
		t.Errorf("initial artifact content: got %q, want existing", art.Content)
	}

	// Post a new artifact — should appear in SSE stream.
	body2 := `{"type":"type","content":"new-artifact"}`
	resp2, err := http.Post(base+"/api/artifacts/proj1/issue-2", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatalf("POST artifact 2: %v", err)
	}
	resp2.Body.Close()

	event2 := readSSEEvent(t, scanner)
	if event2.Type != "artifact" {
		t.Errorf("new event type: got %q, want artifact", event2.Type)
	}
	var art2 Artifact
	if err := json.Unmarshal(event2.Payload, &art2); err != nil {
		t.Fatalf("unmarshal new artifact: %v", err)
	}
	if art2.Content != "new-artifact" {
		t.Errorf("new artifact content: got %q, want new-artifact", art2.Content)
	}

	// Post a message — should also appear.
	body3 := `{"from":"issue-1","content":"hello"}`
	resp3, err := http.Post(base+"/api/messages/proj1/issue-2", "application/json", strings.NewReader(body3))
	if err != nil {
		t.Fatalf("POST message: %v", err)
	}
	resp3.Body.Close()

	event3 := readSSEEvent(t, scanner)
	if event3.Type != "message" {
		t.Errorf("message event type: got %q, want message", event3.Type)
	}
}

func TestBroker_PostArtifact_BadBody(t *testing.T) {
	b := newTestBroker(t)
	resp, err := http.Post(
		brokerURL(b)+"/api/artifacts/proj1/issue-1",
		"application/json",
		strings.NewReader("not json"),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestBroker_PostMessage_BadBody(t *testing.T) {
	b := newTestBroker(t)
	resp, err := http.Post(
		brokerURL(b)+"/api/messages/proj1/issue-1",
		"application/json",
		strings.NewReader("{invalid"),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestBroker_ProjectIsolation(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// Post to proj-a.
	body := `{"type":"x","content":"proj-a data"}`
	resp, _ := http.Post(base+"/api/artifacts/proj-a/1", "application/json", strings.NewReader(body))
	resp.Body.Close()

	// List proj-b should be empty.
	resp2, _ := http.Get(base + "/api/artifacts/proj-b")
	defer resp2.Body.Close()
	var arts []Artifact
	json.NewDecoder(resp2.Body).Decode(&arts)
	if len(arts) != 0 {
		t.Errorf("proj-b should have 0 artifacts, got %d", len(arts))
	}
}

func TestEventBus_SubscribeUnsubscribe(t *testing.T) {
	eb := newEventBus()
	ch := eb.Subscribe("proj1")
	if ch == nil {
		t.Fatal("Subscribe returned nil")
	}

	// Publish an event.
	payload, _ := json.Marshal(Artifact{IssueID: "1", Type: "test"})
	eb.publish("proj1", Event{Type: "artifact", Payload: payload})

	select {
	case event := <-ch:
		if event.Type != "artifact" {
			t.Errorf("event type: got %q, want artifact", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Unsubscribe.
	eb.Unsubscribe("proj1", ch)

	// Channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	eb := newEventBus()
	ch1 := eb.Subscribe("proj1")
	ch2 := eb.Subscribe("proj1")

	payload, _ := json.Marshal(Artifact{IssueID: "1"})
	eb.publish("proj1", Event{Type: "artifact", Payload: payload})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case event := <-ch:
			if event.Type != "artifact" {
				t.Errorf("subscriber %d: got type %q", i, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timeout", i)
		}
	}

	eb.Unsubscribe("proj1", ch1)
	eb.Unsubscribe("proj1", ch2)
}

func TestBroker_New_LoggerOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	b, err := New(logger, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	output := buf.String()
	if !strings.Contains(output, "broker: starting on port") {
		t.Errorf("expected 'broker: starting on port' in log, got: %s", output)
	}
	if !strings.Contains(output, "broker: listening on") {
		t.Errorf("expected 'broker: listening on' in log, got: %s", output)
	}
}

func TestBroker_New_FixedPort(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	// Find a free port to use as fixed port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // release so broker can bind

	b, err := New(logger, port)
	if err != nil {
		t.Fatalf("New with port %d: %v", port, err)
	}
	defer b.Close()

	// Verify the broker is listening on the exact port we specified.
	expectedSuffix := fmt.Sprintf(":%d", port)
	if !strings.HasSuffix(b.Addr(), expectedSuffix) {
		t.Errorf("expected addr ending with %s, got %s", expectedSuffix, b.Addr())
	}

	// Verify the broker is reachable on the fixed port.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/artifacts/test-project", port))
	if err != nil {
		t.Fatalf("GET on fixed port: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBroker_New_PortConflict(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	// Occupy a port first.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	// Attempt to create broker on occupied port — should fail.
	b, err := New(logger, port)
	if err == nil {
		b.Close()
		t.Fatal("expected error when port is occupied, got nil")
	}
	if !strings.Contains(err.Error(), "broker listen") {
		t.Errorf("unexpected error: %v", err)
	}
}

// readSSEEvent reads the next SSE event from the scanner with a timeout.
func readSSEEvent(t *testing.T, scanner *bufio.Scanner) Event {
	t.Helper()
	done := make(chan Event, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var event Event
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue
				}
				done <- event
				return
			}
		}
	}()
	select {
	case event := <-done:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for SSE event")
		return Event{} // unreachable
	}
}

func TestBroker_SSE_DifferentProjectIsolation(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// Connect SSE for proj-a.
	sseResp, err := http.Get(base + "/api/events/proj-a")
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer sseResp.Body.Close()
	scanner := bufio.NewScanner(sseResp.Body)

	// Post to proj-b — should NOT appear in proj-a SSE.
	body := `{"type":"x","content":"proj-b data"}`
	resp, _ := http.Post(base+"/api/artifacts/proj-b/1", "application/json", strings.NewReader(body))
	resp.Body.Close()

	// Post to proj-a — should appear.
	body2 := `{"type":"y","content":"proj-a data"}`
	resp2, _ := http.Post(base+"/api/artifacts/proj-a/1", "application/json", strings.NewReader(body2))
	resp2.Body.Close()

	event := readSSEEvent(t, scanner)
	var art Artifact
	json.Unmarshal(event.Payload, &art)
	if art.Content != "proj-a data" {
		t.Errorf("expected proj-a data, got %q", art.Content)
	}
}

func TestBroker_ListProjects_Empty(t *testing.T) {
	b := newTestBroker(t)
	resp, err := http.Get(brokerURL(b) + "/api/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var projects []projectInfo
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected empty list, got %d", len(projects))
	}
}

func TestBroker_ListProjects_WithData(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// Post artifacts to two projects.
	resp, _ := http.Post(base+"/api/artifacts/proj-a/3", "application/json", strings.NewReader(`{"type":"x","content":"a"}`))
	resp.Body.Close()
	resp, _ = http.Post(base+"/api/artifacts/proj-a/5", "application/json", strings.NewReader(`{"type":"y","content":"b"}`))
	resp.Body.Close()
	resp, _ = http.Post(base+"/api/artifacts/proj-b/10", "application/json", strings.NewReader(`{"type":"z","content":"c"}`))
	resp.Body.Close()

	// Send a message in proj-b.
	resp, _ = http.Post(base+"/api/messages/proj-b/10", "application/json", strings.NewReader(`{"from":"7","content":"hi"}`))
	resp.Body.Close()

	resp2, err := http.Get(base + "/api/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()

	var projects []projectInfo
	if err := json.NewDecoder(resp2.Body).Decode(&projects); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	// Results are sorted by ID.
	if projects[0].ID != "proj-a" {
		t.Errorf("project 0: got %q, want proj-a", projects[0].ID)
	}
	if len(projects[0].IssueIDs) != 2 {
		t.Errorf("proj-a issues: got %d, want 2", len(projects[0].IssueIDs))
	}

	if projects[1].ID != "proj-b" {
		t.Errorf("project 1: got %q, want proj-b", projects[1].ID)
	}
	// proj-b has issue 10 from artifact + issues 7 (from) and 10 (to) from message.
	if len(projects[1].IssueIDs) != 2 { // "7" and "10"
		t.Errorf("proj-b issues: got %v, want [7, 10]", projects[1].IssueIDs)
	}
}

func TestBroker_ListProjects_SSEAgentCount(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// Post an artifact so project exists.
	resp, _ := http.Post(base+"/api/artifacts/proj-x/1", "application/json", strings.NewReader(`{"type":"x","content":"a"}`))
	resp.Body.Close()

	// No SSE connections yet.
	resp2, _ := http.Get(base + "/api/projects")
	var projects []projectInfo
	json.NewDecoder(resp2.Body).Decode(&projects)
	resp2.Body.Close()
	if len(projects) != 1 || len(projects[0].Agents) != 0 {
		t.Fatalf("before SSE: got %+v", projects)
	}

	// Connect SSE with agent_type.
	sseResp, err := http.Get(base + "/api/events/proj-x?agent_type=clarifier")
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}

	// Wait briefly for SSE handler to register.
	time.Sleep(100 * time.Millisecond)

	resp3, _ := http.Get(base + "/api/projects")
	var projects2 []projectInfo
	json.NewDecoder(resp3.Body).Decode(&projects2)
	resp3.Body.Close()
	if len(projects2) != 1 || projects2[0].Agents["clarifier"] != 1 {
		t.Fatalf("during SSE: got %+v", projects2)
	}

	// Disconnect SSE.
	sseResp.Body.Close()
	time.Sleep(100 * time.Millisecond)

	resp4, _ := http.Get(base + "/api/projects")
	var projects3 []projectInfo
	json.NewDecoder(resp4.Body).Decode(&projects3)
	resp4.Body.Close()
	if len(projects3) != 1 || len(projects3[0].Agents) != 0 {
		t.Fatalf("after SSE close: got %+v", projects3)
	}
}

func TestBroker_Close(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	b, err := New(logger, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr := b.Addr()

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify server is no longer accepting connections.
	_, err = http.Get(fmt.Sprintf("http://%s/api/artifacts/test", addr))
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestBroker_ArtifactAgentNameRoundTrip(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// POST an artifact with agent_name.
	body := `{"type":"interface","content":"type Foo struct{}","agent_name":"clarifier-a3f7"}`
	resp, err := http.Post(base+"/api/artifacts/proj-an/issue-1", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST artifact: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	// GET artifacts and verify agent_name round-trips.
	resp2, err := http.Get(base + "/api/artifacts/proj-an")
	if err != nil {
		t.Fatalf("GET artifacts: %v", err)
	}
	defer resp2.Body.Close()

	var arts []Artifact
	if err := json.NewDecoder(resp2.Body).Decode(&arts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].AgentName != "clarifier-a3f7" {
		t.Errorf("AgentName: got %q, want %q", arts[0].AgentName, "clarifier-a3f7")
	}
	if arts[0].Type != "interface" {
		t.Errorf("Type: got %q, want %q", arts[0].Type, "interface")
	}
}

func TestBroker_MessageAgentNameRoundTrip(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// POST a message with agent_name.
	body := `{"from":"issue-1","content":"hello","agent_name":"reviewer-c41d"}`
	resp, err := http.Post(base+"/api/messages/proj-mn/issue-2", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST message: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	// GET messages and verify agent_name round-trips.
	resp2, err := http.Get(base + "/api/messages/proj-mn/issue-2")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp2.Body.Close()

	var msgs []Message
	if err := json.NewDecoder(resp2.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].AgentName != "reviewer-c41d" {
		t.Errorf("AgentName: got %q, want %q", msgs[0].AgentName, "reviewer-c41d")
	}
	if msgs[0].From != "issue-1" {
		t.Errorf("From: got %q, want %q", msgs[0].From, "issue-1")
	}
}

func TestBroker_ArtifactNoAgentName(t *testing.T) {
	b := newTestBroker(t)
	base := brokerURL(b)

	// POST an artifact WITHOUT agent_name (backward compatibility).
	body := `{"type":"schema","content":"data"}`
	resp, err := http.Post(base+"/api/artifacts/proj-compat/issue-1", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST artifact: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	// GET artifacts and verify agent_name is empty (zero value, no error).
	resp2, err := http.Get(base + "/api/artifacts/proj-compat")
	if err != nil {
		t.Fatalf("GET artifacts: %v", err)
	}
	defer resp2.Body.Close()

	var arts []Artifact
	if err := json.NewDecoder(resp2.Body).Decode(&arts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].AgentName != "" {
		t.Errorf("AgentName: got %q, want empty string", arts[0].AgentName)
	}
	if arts[0].Type != "schema" {
		t.Errorf("Type: got %q, want %q", arts[0].Type, "schema")
	}
	if arts[0].Content != "data" {
		t.Errorf("Content: got %q, want %q", arts[0].Content, "data")
	}
}
