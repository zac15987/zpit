package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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

// TestEvaluateDesktopLaunchGuards_LaunchInFlight verifies that a second [w]
// press is blocked while the first launch is still waiting for the periodic
// session scan to populate SessionPID. Regression: previously, SessionPID=0
// was treated as a stale entry and the second press spawned a duplicate
// session.
func TestEvaluateDesktopLaunchGuards_LaunchInFlight(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("launch-in-flight test not applicable on Linux (linux guard fires first)")
	}

	state := newTestAppState()
	state.activeDesktopAgent = &ActiveTerminal{
		SessionPID: 0, // set by handleDesktopAgentLaunched; populated later by scan
		State:      watcher.StateUnknown,
	}

	// isAlive should not be consulted on the pid==0 branch; make it fail loudly
	// if the code ever does.
	alwaysAlive := func(_ int) bool { return true }

	blockText, ok := evaluateDesktopLaunchGuards(state, alwaysAlive)
	if ok {
		t.Fatal("expected launch-in-flight to block second press, but it was allowed")
	}
	if blockText != locale.T(locale.KeyDesktopLaunching) {
		t.Errorf("blockText = %q, want launching message", blockText)
	}
}

// TestEvaluateDesktopLaunchGuards_StaleEntryOverwrites verifies that a launch
// proceeds when activeDesktopAgent has a known PID that is no longer alive
// (the previous agent terminated but the liveness sweep hasn't cleared it
// yet — AC-7 inverse).
func TestEvaluateDesktopLaunchGuards_StaleEntryOverwrites(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("stale-entry test not applicable on Linux (linux guard fires first)")
	}

	state := newTestAppState()
	state.activeDesktopAgent = &ActiveTerminal{
		SessionPID: 12345, // any non-zero PID
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

// TestCheckSessionLiveness_ClearsActiveDesktopAgentWhenEnded verifies AC-12(j):
// when the desktop agent's tracking entry is marked StateEnded and aged past
// endedDisplayDuration, the next liveness pass deletes the entry, clears
// AppState.activeDesktopAgent, dispatches DesktopAgentExitedMsg, and logs the
// documented exit line.
func TestCheckSessionLiveness_ClearsActiveDesktopAgentWhenEnded(t *testing.T) {
	state := newTestAppState()

	// Replace logger with a buffer so we can assert on the AC-8 exit log line.
	var logBuf bytes.Buffer
	state.logger = log.New(&logBuf, "", 0)

	const trackingKey = "desktop:desktop-abcd"
	const agentName = "desktop-abcd"
	const pid = 999999 // Not a real process — IsClaudeProcess would return false anyway.

	// Stage the desktop entry as already-ended and aged past endedDisplayDuration
	// so the liveness pass deletes it (which then trips the clear-activeDesktopAgent block).
	now := time.Now()
	entry := &ActiveTerminal{
		SessionPID:     pid,
		State:          watcher.StateEnded,
		StateChangedAt: now.Add(-endedDisplayDuration - time.Second),
	}
	state.activeTerminals[trackingKey] = entry
	state.activeDesktopAgent = entry
	// Force lastLivenessCheck to zero so the call runs unconditionally.

	m := &Model{state: state}
	cmds := m.checkSessionLiveness()

	// The cleared invariant: activeDesktopAgent must be nil.
	state.RLock()
	got := state.activeDesktopAgent
	_, stillTracked := state.activeTerminals[trackingKey]
	state.RUnlock()
	if got != nil {
		t.Errorf("expected activeDesktopAgent=nil after liveness clear, got %+v", got)
	}
	if stillTracked {
		t.Errorf("expected entry %q removed from activeTerminals", trackingKey)
	}

	// The AC-8 log line: `desktop agent <agentName> (PID <pid>) exited`.
	logStr := logBuf.String()
	wantLog := fmt.Sprintf("desktop agent %s (PID %d) exited", agentName, pid)
	if !strings.Contains(logStr, wantLog) {
		t.Errorf("expected log to contain %q; got: %s", wantLog, logStr)
	}

	// A DesktopAgentExitedMsg must be among the dispatched cmds.
	var found bool
	for _, c := range cmds {
		if c == nil {
			continue
		}
		msg := c()
		if exited, ok := msg.(DesktopAgentExitedMsg); ok {
			if exited.AgentName != agentName {
				t.Errorf("DesktopAgentExitedMsg.AgentName = %q, want %q", exited.AgentName, agentName)
			}
			if exited.PID != pid {
				t.Errorf("DesktopAgentExitedMsg.PID = %d, want %d", exited.PID, pid)
			}
			found = true
		}
	}
	if !found {
		t.Error("expected at least one DesktopAgentExitedMsg in cmds; got none")
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

