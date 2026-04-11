//go:build !windows

package terminal

import "os"

// FindParentShell is a no-op on non-Windows platforms.
// tmux windows do not have the WT closeOnExit issue.
func FindParentShell(_ int) (int, string) { return 0, "" }

// KillWithZeroExit terminates a process. On non-Windows platforms the exit code
// does not matter, so this is equivalent to proc.Kill().
func KillWithZeroExit(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// KillProcess terminates the given PID. Errors are silently ignored.
func KillProcess(pid int) {
	KillWithZeroExit(pid)
}
