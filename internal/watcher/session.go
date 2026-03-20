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

// processAliveFunc is overridable for testing.
var processAliveFunc = isProcessAlive

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

		if !processAliveFunc(info.PID) {
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

// IsProcessAlive checks if the given PID is still running.
func IsProcessAlive(pid int) bool {
	return processAliveFunc(pid)
}

// normalizePath normalizes path separators for comparison.
func normalizePath(p string) string {
	return strings.ReplaceAll(strings.ToLower(p), "\\", "/")
}
