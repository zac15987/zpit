//go:build windows

package watcher

import (
	"os/exec"
	"strconv"
	"strings"
)

// isProcessAlive checks if a PID is still running on Windows.
func isProcessAlive(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(out) > 0 && !strings.Contains(string(out), "No tasks")
}
