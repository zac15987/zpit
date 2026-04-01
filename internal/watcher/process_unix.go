//go:build !windows

package watcher

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// isClaudeProcess checks if a PID belongs to a running claude process on Unix.
func isClaudeProcess(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(string(out))), "claude")
}
