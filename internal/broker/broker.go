package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Broker is an in-memory HTTP broker for cross-worktree agent communication.
// It provides REST endpoints for artifacts and messages, plus SSE for real-time events.
// Binds to 127.0.0.1:<port> using the configured broker_port (default 17731).
type Broker struct {
	mu        sync.RWMutex
	artifacts map[string][]Artifact // project → artifacts
	messages  map[string][]Message  // project → messages
	sseConns  map[string]int        // project → active SSE connection count

	bus      *eventBus
	listener net.Listener
	server   *http.Server
	logger   *log.Logger
}

// New creates a new Broker, binds to the specified port on localhost, and starts serving.
// The caller must call Close() to stop the broker.
func New(logger *log.Logger, port int) (*Broker, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	logger.Printf("broker: starting on port %d", port)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Printf("broker: listen failed on port %d: %v", port, err)
		return nil, fmt.Errorf("broker listen: %w", err)
	}

	b := &Broker{
		artifacts: make(map[string][]Artifact),
		messages:  make(map[string][]Message),
		sseConns:  make(map[string]int),
		bus:       newEventBus(),
		listener:  ln,
		logger:    logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/artifacts/{project}/{issue_id}", b.handlePostArtifact)
	mux.HandleFunc("GET /api/artifacts/{project}", b.handleListArtifacts)
	mux.HandleFunc("POST /api/messages/{project}/{to}", b.handlePostMessage)
	mux.HandleFunc("GET /api/messages/{project}/{issue_id}", b.handleGetMessages)
	mux.HandleFunc("GET /api/events/{project}", b.handleSSE)
	mux.HandleFunc("GET /api/projects", b.handleListProjects)

	b.server = &http.Server{Handler: mux}

	go func() {
		if err := b.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Printf("broker: serve error: %v", err)
		}
	}()

	logger.Printf("broker: listening on %s", ln.Addr().String())
	return b, nil
}

// Addr returns the actual listen address (e.g. "127.0.0.1:54321").
func (b *Broker) Addr() string {
	return b.listener.Addr().String()
}

// Events returns the EventBus for subscribing to broker events.
func (b *Broker) Events() EventBus {
	return b.bus
}

// Close gracefully shuts down the broker.
func (b *Broker) Close() error {
	b.logger.Println("broker: shutting down")
	b.listener.Close() // stop accepting new connections
	b.bus.closeAll()    // unblock all SSE handlers
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.server.Shutdown(ctx)
}

// --- HTTP handlers ---

// postArtifactRequest is the JSON body for POST /api/artifacts/{project}/{issue_id}.
type postArtifactRequest struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	SenderID  string `json:"sender_id"`
	AgentName string `json:"agent_name"`
}

func (b *Broker) handlePostArtifact(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	issueID := r.PathValue("issue_id")
	b.logger.Printf("broker: POST /api/artifacts/%s/%s", project, issueID)

	var req postArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		b.logger.Printf("broker: bad request body: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	artifact := Artifact{
		IssueID:   issueID,
		Type:      req.Type,
		Content:   req.Content,
		SenderID:  req.SenderID,
		AgentName: req.AgentName,
		Timestamp: time.Now(),
	}

	b.mu.Lock()
	b.artifacts[project] = append(b.artifacts[project], artifact)
	b.mu.Unlock()

	// Publish event to SSE subscribers and EventBus.
	payload, _ := json.Marshal(artifact)
	event := Event{Type: "artifact", Payload: payload}
	b.bus.publish(project, event)

	b.logger.Printf("broker: artifact stored project=%s issue=%s type=%s", project, issueID, req.Type)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (b *Broker) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	b.logger.Printf("broker: GET /api/artifacts/%s", project)

	b.mu.RLock()
	arts := b.artifacts[project]
	b.mu.RUnlock()

	if arts == nil {
		arts = []Artifact{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(arts)
	b.logger.Printf("broker: listed %d artifacts for project=%s", len(arts), project)
}

// postMessageRequest is the JSON body for POST /api/messages/{project}/{to}.
type postMessageRequest struct {
	From      string `json:"from"`
	Content   string `json:"content"`
	SenderID  string `json:"sender_id"`
	AgentName string `json:"agent_name"`
}

func (b *Broker) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	to := r.PathValue("to")
	b.logger.Printf("broker: POST /api/messages/%s/%s", project, to)

	var req postMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		b.logger.Printf("broker: bad request body: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	msg := Message{
		From:      req.From,
		To:        to,
		Content:   req.Content,
		SenderID:  req.SenderID,
		AgentName: req.AgentName,
		Timestamp: time.Now(),
	}

	b.mu.Lock()
	b.messages[project] = append(b.messages[project], msg)
	b.mu.Unlock()

	// Publish event to SSE subscribers and EventBus.
	payload, _ := json.Marshal(msg)
	event := Event{Type: "message", Payload: payload}
	b.bus.publish(project, event)

	b.logger.Printf("broker: message stored project=%s from=%s to=%s", project, req.From, to)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (b *Broker) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	issueID := r.PathValue("issue_id")
	b.logger.Printf("broker: GET /api/messages/%s/%s", project, issueID)

	b.mu.RLock()
	allMsgs := b.messages[project]
	b.mu.RUnlock()

	// Filter messages addressed to this issue.
	var filtered []Message
	for _, msg := range allMsgs {
		if msg.To == issueID {
			filtered = append(filtered, msg)
		}
	}
	if filtered == nil {
		filtered = []Message{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filtered)
	b.logger.Printf("broker: listed %d messages for project=%s issue=%s", len(filtered), project, issueID)
}

// handleSSE provides a Server-Sent Events stream for real-time event delivery.
// On connect, it first pushes all existing artifacts as initial state (like MQTT retained messages),
// then streams new events as they arrive.
func (b *Broker) handleSSE(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	b.logger.Printf("broker: SSE connect project=%s", project)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Send existing artifacts as initial state.
	b.mu.RLock()
	existing := b.artifacts[project]
	b.mu.RUnlock()

	for _, art := range existing {
		payload, _ := json.Marshal(art)
		event := Event{Type: "artifact", Payload: payload}
		if err := writeSSEEvent(w, flusher, event); err != nil {
			b.logger.Printf("broker: SSE write error (initial): %v", err)
			return
		}
	}
	b.logger.Printf("broker: SSE sent %d initial artifacts for project=%s", len(existing), project)

	// Track active SSE connection count for discovery endpoint.
	b.mu.Lock()
	b.sseConns[project]++
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.sseConns[project]--
		if b.sseConns[project] == 0 {
			delete(b.sseConns, project)
		}
		b.mu.Unlock()
	}()

	// Subscribe to new events.
	ch := b.bus.Subscribe(project)
	defer b.bus.Unsubscribe(project, ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			b.logger.Printf("broker: SSE disconnect project=%s", project)
			return
		case event, ok := <-ch:
			if !ok {
				b.logger.Printf("broker: SSE channel closed project=%s", project)
				return
			}
			if err := writeSSEEvent(w, flusher, event); err != nil {
				b.logger.Printf("broker: SSE write error: %v", err)
				return
			}
		}
	}
}

// projectInfo describes an active project for the discovery endpoint.
type projectInfo struct {
	ID         string   `json:"id"`
	IssueIDs   []string `json:"issue_ids"`
	AgentCount int      `json:"agent_count"`
}

func (b *Broker) handleListProjects(w http.ResponseWriter, r *http.Request) {
	b.logger.Println("broker: GET /api/projects")

	// Snapshot data under RLock, then release before processing.
	b.mu.RLock()
	type projectSnapshot struct {
		artifacts []Artifact
		messages  []Message
		sseCount  int
	}
	// Collect unique project keys.
	projects := make(map[string]struct{})
	for p := range b.artifacts {
		projects[p] = struct{}{}
	}
	for p := range b.messages {
		projects[p] = struct{}{}
	}
	for p := range b.sseConns {
		projects[p] = struct{}{}
	}
	snapshots := make(map[string]projectSnapshot, len(projects))
	for p := range projects {
		snapshots[p] = projectSnapshot{
			artifacts: b.artifacts[p],
			messages:  b.messages[p],
			sseCount:  b.sseConns[p],
		}
	}
	b.mu.RUnlock()

	// Build result outside the lock.
	result := make([]projectInfo, 0, len(snapshots))
	for p, snap := range snapshots {
		issueSet := make(map[string]struct{})
		for _, art := range snap.artifacts {
			issueSet[art.IssueID] = struct{}{}
		}
		for _, msg := range snap.messages {
			if msg.From != "" {
				issueSet[msg.From] = struct{}{}
			}
			if msg.To != "" {
				issueSet[msg.To] = struct{}{}
			}
		}
		issueIDs := make([]string, 0, len(issueSet))
		for id := range issueSet {
			issueIDs = append(issueIDs, id)
		}
		sort.Strings(issueIDs)
		result = append(result, projectInfo{
			ID:         p,
			IssueIDs:   issueIDs,
			AgentCount: snap.sseCount,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	b.logger.Printf("broker: listed %d projects", len(result))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// writeSSEEvent writes a single SSE event in the format: "data: {json}\n\n".
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	// SSE data must not contain embedded newlines — JSON marshal produces single-line output.
	line := fmt.Sprintf("data: %s\n\n", strings.TrimSpace(string(data)))
	if _, err := io.WriteString(w, line); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
