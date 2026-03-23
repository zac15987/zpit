package platform

import (
	"os"
	"runtime"
	"strings"
)

// Environment represents the detected runtime environment.
type Environment int

const (
	EnvWindowsTerminal Environment = iota
	EnvWSLTmux
	EnvLinuxTmux
	EnvUnknown
)

func (e Environment) String() string {
	switch e {
	case EnvWindowsTerminal:
		return "Windows Terminal"
	case EnvWSLTmux:
		return "WSL"
	case EnvLinuxTmux:
		return "Linux (tmux)"
	default:
		return "Unknown"
	}
}

// readFileFunc is overridable for testing.
var readFileFunc = os.ReadFile

// Detect determines the current runtime environment.
func Detect() Environment {
	if runtime.GOOS == "windows" {
		return EnvWindowsTerminal
	}

	if isWSL() {
		return EnvWSLTmux
	}

	if insideTmux() {
		return EnvLinuxTmux
	}

	return EnvUnknown
}

// IsWindows returns true if running on native Windows.
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// ResolvePath picks the right project path for the current OS.
func ResolvePath(windowsPath, wslPath string) string {
	if IsWindows() {
		return windowsPath
	}
	return wslPath
}

func isWSL() bool {
	data, err := readFileFunc("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

func insideTmux() bool {
	return os.Getenv("TMUX") != ""
}
