package watcher

import (
	"bytes"
	"encoding/json"
	"os"
	"time"
)

// AgentState represents the current state of a Claude Code agent.
type AgentState int

const (
	StateUnknown    AgentState = iota
	StateWorking               // stop_reason="tool_use"
	StateWaiting               // stop_reason="end_turn" — needs user input
	StateStreaming              // stop_reason=null — response in progress
	StateEnded                 // session process exited
	StatePermission            // waiting for user to approve/deny tool permission
)

func (s AgentState) String() string {
	switch s {
	case StateWorking:
		return "Working"
	case StateWaiting:
		return "Waiting"
	case StateStreaming:
		return "Streaming"
	case StateEnded:
		return "Ended"
	case StatePermission:
		return "Permission"
	default:
		return "Unknown"
	}
}

// SessionEvent is a parsed JSONL line from a Claude Code session log.
type SessionEvent struct {
	Type         string     // "user", "assistant", "system", "progress", "last-prompt"
	State        AgentState // only meaningful for type="assistant"
	QuestionText string     // extracted text when State==StateWaiting
	Timestamp    time.Time
}

// sessionLine is the minimal JSON structure for unmarshalling.
type sessionLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp,omitempty"`
	Message   *sessionMessage `json:"message,omitempty"`
}

type sessionMessage struct {
	StopReason *string         `json:"stop_reason"`
	Content    json.RawMessage `json:"content"` // string or []contentBlock
}

// parseContent handles content being either a string or array of blocks.
func parseContent(raw json.RawMessage) []contentBlock {
	if len(raw) == 0 {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	// Content is a plain string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []contentBlock{{Type: "text", Text: s}}
	}
	return nil
}

type contentBlock struct {
	Type string `json:"type"` // "text", "tool_use", "thinking"
	Text string `json:"text,omitempty"`
}

// ParseLine parses a single JSONL line from a Claude Code session log.
func ParseLine(line []byte) (SessionEvent, error) {
	var raw sessionLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return SessionEvent{}, err
	}

	ev := SessionEvent{
		Type:      raw.Type,
		Timestamp: time.Now(),
	}

	if raw.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw.Timestamp); err == nil {
			ev.Timestamp = t
		}
	}

	if raw.Type == "assistant" && raw.Message != nil {
		ev.State = deriveState(raw.Message)
		if ev.State == StateWaiting {
			blocks := parseContent(raw.Message.Content)
			ev.QuestionText = extractLastText(blocks)
		}
	}

	return ev, nil
}

func deriveState(msg *sessionMessage) AgentState {
	if msg.StopReason == nil {
		return StateStreaming
	}
	switch *msg.StopReason {
	case "end_turn":
		return StateWaiting
	case "tool_use":
		return StateWorking
	default:
		return StateUnknown
	}
}

// ReadLastState reads a session log file and returns the last known agent state and question text.
func ReadLastState(logPath string) (AgentState, string) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return StateUnknown, ""
	}
	state := StateUnknown
	question := ""
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		ev, err := ParseLine(line)
		if err != nil || ev.Type != "assistant" {
			continue
		}
		state = ev.State
		if ev.State == StateWaiting {
			question = ev.QuestionText
		} else {
			question = ""
		}
	}
	return state, question
}

func extractLastText(blocks []contentBlock) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Type == "text" && blocks[i].Text != "" {
			return blocks[i].Text
		}
	}
	return ""
}
