//go:build windows

package terminal

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shellExes are processes safe to kill when closing a WT tab.
var shellExes = map[string]bool{
	"cmd.exe":        true,
	"powershell.exe": true,
	"pwsh.exe":       true,
}

// FindParentShell walks up the process tree from pid and returns the topmost
// consecutive shell ancestor (cmd, powershell, pwsh). This handles cases where
// Claude Code has intermediate shell layers (e.g. pwsh → cmd.exe → node.exe).
// Returns (0, "") if no shell ancestor exists. Must be called while pid is alive.
func FindParentShell(pid int) (int, string) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, ""
	}
	defer windows.CloseHandle(snapshot)

	procs := make(map[uint32]windows.ProcessEntry32)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		procs[entry.ProcessID] = entry
	}

	target, ok := procs[uint32(pid)]
	if !ok {
		return 0, ""
	}

	// Walk up through consecutive shell ancestors. Stop at the first non-shell.
	// Example: node.exe → cmd.exe → pwsh.exe → WindowsTerminal.exe
	//          We return pwsh.exe (the topmost shell before WT).
	var topPID int
	var topName string
	current := target.ParentProcessID
	seen := map[uint32]bool{uint32(pid): true}

	for {
		if seen[current] {
			break
		}
		seen[current] = true

		proc, ok := procs[current]
		if !ok {
			break
		}
		name := strings.ToLower(windows.UTF16ToString(proc.ExeFile[:]))
		if !shellExes[name] {
			break
		}
		topPID = int(proc.ProcessID)
		topName = name
		current = proc.ParentProcessID
	}

	return topPID, topName
}

// KillWithZeroExit terminates a process with exit code 0. This is critical on
// Windows because WT's closeOnExit: "graceful" (default) only closes the tab
// when the process exits with code 0. Go's proc.Kill() uses exit code 1.
func KillWithZeroExit(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.TerminateProcess(handle, 0)
}

// KillProcess terminates the given PID with exit code 0. Errors are silently ignored.
func KillProcess(pid int) {
	KillWithZeroExit(pid)
}
