package broker

import (
	"encoding/json"
	"sync"
	"time"
)

// Artifact represents a published artifact from an agent (e.g. interface definition, type spec).
type Artifact struct {
	IssueID   string    `json:"issue_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	SenderID  string    `json:"sender_id,omitempty"`
	AgentName string    `json:"agent_name,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Message represents a directed message between agents.
type Message struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Content   string    `json:"content"`
	SenderID  string    `json:"sender_id,omitempty"`
	AgentName string    `json:"agent_name,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Event represents a broker event pushed via SSE.
type Event struct {
	Type    string          `json:"type"`    // "artifact" or "message"
	Payload json.RawMessage `json:"payload"` // JSON-encoded Artifact or Message
}

// EventBus provides a pub/sub interface for broker events.
// TUI or other consumers can subscribe to receive events in real time.
type EventBus interface {
	// Subscribe returns a channel that receives events for the given project.
	// The caller must call Unsubscribe when done to avoid goroutine leaks.
	Subscribe(project string) <-chan Event

	// Unsubscribe removes the given channel from the project's subscriber list.
	Unsubscribe(project string, ch <-chan Event)
}

// eventBus is the in-memory implementation of EventBus.
// It maintains a mapping from receive-only channels (returned to callers)
// to their underlying writable channels for internal publish/close operations.
type eventBus struct {
	mu      sync.Mutex
	subs    map[string]map[chan Event]struct{}    // project → set of writable channels
	mapping map[<-chan Event]chan Event            // read-only view → writable channel
}

func newEventBus() *eventBus {
	return &eventBus{
		subs:    make(map[string]map[chan Event]struct{}),
		mapping: make(map[<-chan Event]chan Event),
	}
}

// Subscribe registers a new subscriber for the given project's events.
func (eb *eventBus) Subscribe(project string) <-chan Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	ch := make(chan Event, 64) // buffered to avoid blocking publisher
	if eb.subs[project] == nil {
		eb.subs[project] = make(map[chan Event]struct{})
	}
	eb.subs[project][ch] = struct{}{}
	eb.mapping[ch] = ch
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (eb *eventBus) Unsubscribe(project string, ch <-chan Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	wch, ok := eb.mapping[ch]
	if !ok {
		return
	}
	delete(eb.mapping, ch)
	if subs, ok := eb.subs[project]; ok {
		delete(subs, wch)
		if len(subs) == 0 {
			delete(eb.subs, project)
		}
	}
	close(wch)
}

// closeAll closes all subscriber channels so blocked readers unblock immediately.
// Safe to call before server.Shutdown() — deferred Unsubscribe calls become no-ops.
func (eb *eventBus) closeAll() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for _, wch := range eb.mapping {
		close(wch)
	}
	eb.subs = make(map[string]map[chan Event]struct{})
	eb.mapping = make(map[<-chan Event]chan Event)
}

// publish sends an event to all subscribers of the given project.
// Non-blocking: if a subscriber's buffer is full, the event is dropped for that subscriber.
func (eb *eventBus) publish(project string, event Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for ch := range eb.subs[project] {
		select {
		case ch <- event:
		default:
			// subscriber too slow, drop event
		}
	}
}
