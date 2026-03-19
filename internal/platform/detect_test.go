package platform

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestDetect_CurrentPlatform(t *testing.T) {
	env := Detect()
	if runtime.GOOS == "windows" {
		if env != EnvWindowsTerminal {
			t.Errorf("on Windows, got %v", env)
		}
	}
	// On non-Windows, result depends on WSL / tmux state — just ensure no panic
	t.Logf("detected: %v", env)
}

func TestIsWindows(t *testing.T) {
	got := IsWindows()
	want := runtime.GOOS == "windows"
	if got != want {
		t.Errorf("IsWindows() = %v, want %v", got, want)
	}
}

func TestResolvePath(t *testing.T) {
	winPath := "D:/Projects/Foo"
	wslPath := "/mnt/d/Projects/Foo"

	got := ResolvePath(winPath, wslPath)
	if runtime.GOOS == "windows" {
		if got != winPath {
			t.Errorf("on Windows, ResolvePath = %q, want %q", got, winPath)
		}
	} else {
		if got != wslPath {
			t.Errorf("on Linux, ResolvePath = %q, want %q", got, wslPath)
		}
	}
}

func TestIsWSL_WithMockProcVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isWSL only relevant on Linux")
	}

	tests := []struct {
		content string
		want    bool
	}{
		{"Linux version 5.15.0-1-Microsoft", true},
		{"Linux version 5.15.0 (gcc) #1 SMP", false},
		{"linux version 5.15.0-wsl2", true},
	}

	for _, tt := range tests {
		t.Run(tt.content[:20], func(t *testing.T) {
			old := readFileFunc
			readFileFunc = func(name string) ([]byte, error) {
				if name == "/proc/version" {
					return []byte(tt.content), nil
				}
				return nil, fmt.Errorf("not found")
			}
			defer func() { readFileFunc = old }()

			got := isWSL()
			if got != tt.want {
				t.Errorf("isWSL() with %q = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestEnvironment_String(t *testing.T) {
	tests := []struct {
		env  Environment
		want string
	}{
		{EnvWindowsTerminal, "Windows Terminal"},
		{EnvWSLTmux, "WSL"},
		{EnvLinuxTmux, "Linux (tmux)"},
		{EnvUnknown, "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.env.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestInsideTmux(t *testing.T) {
	old := os.Getenv("TMUX")
	defer os.Setenv("TMUX", old)

	os.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	if !insideTmux() {
		t.Error("insideTmux should be true when TMUX is set")
	}

	os.Unsetenv("TMUX")
	if insideTmux() {
		t.Error("insideTmux should be false when TMUX is unset")
	}
}
