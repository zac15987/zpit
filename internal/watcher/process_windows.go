//go:build windows

package watcher

import (
	"os/exec"
	"strconv"
	"strings"
)

// isClaudeProcess checks if a PID belongs to a running claude.exe process on Windows.
func isClaudeProcess(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	output := strings.ToLower(string(out))
	return !strings.Contains(output, "no tasks") &&
		strings.Contains(output, "claude.exe")
}
