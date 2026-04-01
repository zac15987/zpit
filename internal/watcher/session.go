package watcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SessionInfo represents an active Claude Code session found on disk.
type SessionInfo struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	StartedAt int64  `json:"startedAt"`
}

// isClaudeProcessFunc is overridable for testing.
var isClaudeProcessFunc = isClaudeProcess

// FindActiveSessions scans ~/.claude/sessions/*.json for sessions
// whose cwd matches projectPath and whose PID is still alive.
func FindActiveSessions(claudeHome, projectPath string) ([]SessionInfo, error) {
	sessDir := filepath.Join(claudeHome, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil, fmt.Errorf("reading sessions directory: %w", err)
	}

	normalizedProject := normalizePath(projectPath)
	var result []SessionInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}

		var info SessionInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}

		if normalizePath(info.Cwd) != normalizedProject {
			continue
		}

		if !isClaudeProcessFunc(info.PID) {
			continue
		}

		result = append(result, info)
	}

	return result, nil
}

// LogFilePath returns the full path to a session's JSONL log file.
func LogFilePath(claudeHome, projectPath, sessionID string) string {
	encoded := EncodeCwd(projectPath)
	return filepath.Join(claudeHome, "projects", encoded, sessionID+".jsonl")
}

// ReadSessionByPID reads the session file for a specific PID.
// Returns the parsed SessionInfo or an error if the file doesn't exist or is invalid.
func ReadSessionByPID(claudeHome string, pid int) (*SessionInfo, error) {
	sessFile := filepath.Join(claudeHome, "sessions", fmt.Sprintf("%d.json", pid))
	data, err := os.ReadFile(sessFile)
	if err != nil {
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	var info SessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parsing session file: %w", err)
	}
	return &info, nil
}

// IsClaudeProcess checks if the given PID belongs to a running Claude Code process.
func IsClaudeProcess(pid int) bool {
	return isClaudeProcessFunc(pid)
}

// normalizePath normalizes path separators for comparison.
func normalizePath(p string) string {
	return strings.ReplaceAll(strings.ToLower(p), "\\", "/")
}
