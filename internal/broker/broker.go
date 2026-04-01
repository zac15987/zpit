package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Broker is an in-memory HTTP broker for cross-worktree agent communication.
// It provides REST endpoints for artifacts and messages, plus SSE for real-time events.
// Binds to 127.0.0.1:0 (dynamic port) to avoid conflicts between multiple Zpit instances.
type Broker struct {
	mu        sync.RWMutex
	artifacts map[string][]Artifact // project → artifacts
	messages  map[string][]Message  // project → messages

	bus      *eventBus
	listener net.Listener
	server   *http.Server
	logger   *log.Logger
}

// New creates a new Broker, binds to a dynamic port on localhost, and starts serving.
// The caller must call Close() to stop the broker.
func New(logger *log.Logger) (*Broker, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	logger.Println("broker: starting")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logger.Printf("broker: listen failed: %v", err)
		return nil, fmt.Errorf("broker listen: %w", err)
	}

	b := &Broker{
		artifacts: make(map[string][]Artifact),
		messages:  make(map[string][]Message),
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.server.Shutdown(ctx)
}

// --- HTTP handlers ---

// postArtifactRequest is the JSON body for POST /api/artifacts/{project}/{issue_id}.
type postArtifactRequest struct {
	Type    string `json:"type"`
	Content string `json:"content"`
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
	From    string `json:"from"`
	Content string `json:"content"`
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
