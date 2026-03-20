//go:build !windows

package watcher

import (
	"os"
	"syscall"
)

// isProcessAlive checks if a PID is still running on Unix.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
