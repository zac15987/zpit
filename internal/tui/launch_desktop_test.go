package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/watcher"
)

// newTestAppState creates a minimal AppState suitable for precondition tests.
func newTestAppState() *AppState {
	cfg := &config.Config{}
	return &AppState{
		mu:              sync.RWMutex{},
		cfg:             cfg,
		logger:          log.New(io.Discard, "", 0),
		activeTerminals: make(map[string]*ActiveTerminal),
		subscribers:     make(map[int]chan struct{}),
	}
}

// TestEvaluateDesktopLaunchGuards_Linux verifies that evaluateDesktopLaunchGuards
// returns the Linux-unsupported block message on Linux (AC-11).
// On non-Linux, verifies the Linux message is NOT produced.
func TestEvaluateDesktopLaunchGuards_Linux(t *testing.T) {
	state := newTestAppState()
	alwaysFalse := func(_ int) bool { return false }

	blockText, ok := evaluateDesktopLaunchGuards(state, alwaysFalse)

	if runtime.GOOS == "linux" {
		if ok {
			t.Fatal("expected launch to be blocked on Linux, but it was allowed")
		}
		want := locale.T(locale.KeyDesktopLinuxUnsupported)
		if blockText != want {
			t.Errorf("blockText = %q, want %q", blockText, want)
		}
	} else {
		// On non-Linux, the linux guard must NOT fire; activeDesktopAgent is nil so it proceeds.
		if !ok {
			t.Fatalf("unexpected block on non-Linux: %q", blockText)
		}
		linuxMsg := locale.T(locale.KeyDesktopLinuxUnsupported)
		if blockText == linuxMsg {
			t.Errorf("got linux-unsupported message on non-Linux OS")
		}
	}
}

// TestEvaluateDesktopLaunchGuards_SingleInstance verifies that a second launch
// attempt is blocked when activeDesktopAgent has a live PID (AC-7).
func TestEvaluateDesktopLaunchGuards_SingleInstance(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("single-instance test not applicable on Linux (linux guard fires first)")
	}

	livePID := os.Getpid() // current test process PID — guaranteed alive

	state := newTestAppState()
	state.activeDesktopAgent = &ActiveTerminal{
		SessionPID: livePID,
		State:      watcher.StateUnknown,
	}

	// isAlive always returns true for any pid (simulates the live PID).
	alwaysAlive := func(_ int) bool { return true }

	blockText, ok := evaluateDesktopLaunchGuards(state, alwaysAlive)
	if ok {
		t.Fatal("expected launch to be blocked (single-instance), but it was allowed")
	}

	want := fmt.Sprintf(locale.T(locale.KeyDesktopAlreadyRunning), livePID)
	if blockText != want {
		t.Errorf("blockText = %q, want %q", blockText, want)
	}
}

// TestEvaluateDesktopLaunchGuards_StaleEntryOverwrites verifies that a launch
// proceeds when activeDesktopAgent has a dead PID (stale entry — AC-7 inverse).
func TestEvaluateDesktopLaunchGuards_StaleEntryOverwrites(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("stale-entry test not applicable on Linux (linux guard fires first)")
	}

	// SessionPID = 0 means "not yet discovered"; isAlive returns false for 0.
	state := newTestAppState()
	state.activeDesktopAgent = &ActiveTerminal{
		SessionPID: 0,
		State:      watcher.StateUnknown,
	}

	alwaysDead := func(_ int) bool { return false }

	_, ok := evaluateDesktopLaunchGuards(state, alwaysDead)
	if !ok {
		t.Fatal("expected stale entry to be overwritten (launch allowed), but it was blocked")
	}
}

// TestWriteDesktopMCPConfig verifies that writeDesktopMCPConfig creates the
// correct .mcp.json structure under <homeDir>/.zpit/.mcp.json.
func TestWriteDesktopMCPConfig(t *testing.T) {
	homeDir := t.TempDir()
	zpitBin := "/usr/local/bin/zpit"
	agentName := "desktop-a3f7"

	if err := writeDesktopMCPConfig(homeDir, zpitBin, agentName); err != nil {
		t.Fatalf("writeDesktopMCPConfig: %v", err)
	}

	// Verify the file exists.
	mcpPath := filepath.Join(homeDir, ".zpit", ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}

	// Parse and verify structure.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing .mcp.json: %v", err)
	}

	servers, ok := parsed["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not found or wrong type")
	}
	if len(servers) != 1 {
		t.Errorf("expected exactly 1 MCP server, got %d", len(servers))
	}
	proxy, ok := servers["desktop-proxy"].(map[string]any)
	if !ok {
		t.Fatal("desktop-proxy entry not found or wrong type")
	}

	if cmd, _ := proxy["command"].(string); cmd != zpitBin {
		t.Errorf("command = %q, want %q", cmd, zpitBin)
	}

	rawArgs, _ := proxy["args"].([]any)
	if len(rawArgs) != 1 || rawArgs[0] != "serve-desktop-proxy" {
		t.Errorf("args = %v, want [serve-desktop-proxy]", rawArgs)
	}

	env, ok := proxy["env"].(map[string]any)
	if !ok {
		t.Fatal("env not found or wrong type")
	}
	if env["ZPIT_AGENT_NAME"] != agentName {
		t.Errorf("ZPIT_AGENT_NAME = %v, want %q", env["ZPIT_AGENT_NAME"], agentName)
	}
	if env["ZPIT_AGENT_TYPE"] != "desktop" {
		t.Errorf("ZPIT_AGENT_TYPE = %v, want \"desktop\"", env["ZPIT_AGENT_TYPE"])
	}
	// Must NOT contain broker or listen-project fields.
	if _, found := env["ZPIT_BROKER_URL"]; found {
		t.Error("unexpected ZPIT_BROKER_URL in desktop .mcp.json")
	}
	if _, found := env["ZPIT_LISTEN_PROJECTS"]; found {
		t.Error("unexpected ZPIT_LISTEN_PROJECTS in desktop .mcp.json")
	}
}

// TestWriteDesktopMCPConfig_CreatesZpitDir verifies that writeDesktopMCPConfig
// creates the ~/.zpit/ directory if it does not yet exist.
func TestWriteDesktopMCPConfig_CreatesZpitDir(t *testing.T) {
	homeDir := t.TempDir()
	// Ensure .zpit does NOT pre-exist.
	zpitDir := filepath.Join(homeDir, ".zpit")
	if _, err := os.Stat(zpitDir); err == nil {
		t.Fatal("pre-condition failed: .zpit already exists")
	}

	if err := writeDesktopMCPConfig(homeDir, "zpit", "desktop-0000"); err != nil {
		t.Fatalf("writeDesktopMCPConfig: %v", err)
	}

	if _, err := os.Stat(zpitDir); err != nil {
		t.Errorf(".zpit dir not created: %v", err)
	}
}

