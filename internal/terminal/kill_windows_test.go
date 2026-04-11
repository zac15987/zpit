//go:build windows

package terminal

import (
	"os"
	"testing"
)

func TestFindParentShell_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	parentPID, parentName := FindParentShell(pid)
	// In a test context the parent is typically go test's process (go.exe),
	// not a shell — so we expect (0, ""). The important thing is it doesn't panic.
	t.Logf("FindParentShell(%d) = (%d, %q)", pid, parentPID, parentName)
}

func TestFindParentShell_NonexistentPID(t *testing.T) {
	parentPID, parentName := FindParentShell(0)
	if parentPID != 0 || parentName != "" {
		t.Errorf("expected (0, \"\") for PID 0, got (%d, %q)", parentPID, parentName)
	}
}

func TestKillProcess_NonexistentPID(t *testing.T) {
	// Should not panic on a nonexistent PID.
	KillProcess(99999999)
}

func TestKillWithZeroExit_NonexistentPID(t *testing.T) {
	err := KillWithZeroExit(99999999)
	if err == nil {
		t.Error("expected error for nonexistent PID")
	}
}
