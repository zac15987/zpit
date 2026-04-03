package tui

// session.go — Session lifecycle: discovery, monitoring, liveness, permission detection.
//
// Lock protocol:
//   - Handler methods (handleExistingSessions, handleSessionFound, handleWatcherReady,
//     handleSessionLost): acquire Lock for writes + call NotifyAll.
//   - Cmd factory methods (scanExistingSessionsCmd, startWatcherDirCmd,
//     startWatcherDirCmdWithExcludes, waitForLogCmd, watchNextCmd): pure tea.Cmd
//     constructors. startWatcherDirCmd acquires RLock to snapshot trackedPIDs;
//     startWatcherDirCmdWithExcludes takes pre-computed excludes (no lock).
//     waitForLogCmd and watchNextCmd are lock-free (capture values in closure).
//   - Tick-driven methods (checkSessionLiveness, checkPermissionSignals,
//     checkNewSessions): acquire Lock internally for interval gating and state mutation.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/watcher"
)

// ActiveTerminal tracks a launched terminal and its agent state.
type ActiveTerminal struct {
	LaunchResult      *terminal.LaunchResult
	SessionPID        int
	SessionID         string // current session ID (for /resume change detection)
	WorkDir           string // project work directory (needed to recompute logPath on session switch)
	State             watcher.AgentState
	LastQuestion      string
	PermissionMessage string // message from permission signal (e.g., "Claude needs your permission to use Bash")
	StateChangedAt    time.Time
	Watcher           *watcher.Watcher
}

// Session-only message types.

// sessionFoundMsg is sent when the session PID is discovered but JSONL may not exist yet.
type sessionFoundMsg struct {
	ProjectID string
	PID       int
	SessionID string
	LogPath   string
}

// existingSessionEntry represents a session found during startup scan.
type existingSessionEntry struct {
	ProjectID string
	PID       int
	SessionID string
	WorkDir   string
	LogPath   string
}

// existingSessionsMsg carries results of scanning for already-running sessions.
// Source distinguishes "startup" (initial scan) from "periodic" (tick-driven scan).
type existingSessionsMsg struct {
	Source  string // "startup" or "periodic"
	Entries []existingSessionEntry
}

// watcherReadyMsg is an internal message to attach a watcher to an ActiveTerminal.
type watcherReadyMsg struct {
	ProjectID string
	SessionID string
	Watcher   *watcher.Watcher
	LogPath   string
}

// permissionSignal is the parsed content of a permission signal file.
type permissionSignal struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// Session constants.
const (
	tickInterval            = 1 * time.Second
	livenessCheckInterval   = 5 * time.Second
	permissionCheckInterval = 2 * time.Second
	endedDisplayDuration    = 3 * time.Second
	sessionScanInterval     = 10 * time.Second
	sessionRetryInterval    = 2 * time.Second
	sessionRetryMax         = 8  // 8 * 2s = 16s max wait
	logWaitWarnAfter        = 15 // log a warning after 15 * 2s = 30s, but keep waiting
)

// === Msg handlers ===

func (m Model) handleTick() (tea.Model, tea.Cmd) {
	cmds := m.checkSessionLiveness()
	m.checkPermissionSignals()
	if scanCmd := m.checkNewSessions(); scanCmd != nil {
		cmds = append(cmds, scanCmd)
	}
	cmds = append(cmds, tickCmd())
	return m, tea.Batch(cmds...)
}

func (m Model) handleExistingSessions(msg existingSessionsMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	m.state.logger.Printf("session scan [%s]: found %d session(s)", msg.Source, len(msg.Entries))

	// Dedup: build filters from current state (not stale scan-time snapshot).
	currentPIDs := m.trackedPIDs()
	pendingWorkDirs := make(map[string]bool)
	for _, at := range m.state.activeTerminals {
		if at.SessionPID == 0 && at.WorkDir != "" {
			pendingWorkDirs[at.WorkDir] = true
		}
	}

	var cmds []tea.Cmd
	for _, entry := range msg.Entries {
		if currentPIDs[entry.PID] {
			m.state.logger.Printf("  skip: PID=%d already tracked", entry.PID)
			continue
		}
		if pendingWorkDirs[entry.WorkDir] {
			m.state.logger.Printf("  skip: PID=%d workDir has pending discovery", entry.PID)
			continue
		}
		key := m.nextTrackingKey(entry.ProjectID)
		m.state.logger.Printf("  attach: key=%s PID=%d sessionID=%s", key, entry.PID, entry.SessionID)
		m.state.activeTerminals[key] = &ActiveTerminal{
			State:          watcher.StateUnknown,
			SessionPID:     entry.PID,
			SessionID:      entry.SessionID,
			WorkDir:        entry.WorkDir,
			StateChangedAt: time.Now(),
		}
		currentPIDs[entry.PID] = true
		cmds = append(cmds, waitForLogCmd(key, entry.PID, entry.SessionID, entry.LogPath, entry.WorkDir, m.state.logger))
	}
	m.state.NotifyAll()
	m.state.Unlock()
	return m, tea.Batch(cmds...)
}

func (m Model) handleSessionFound(msg sessionFoundMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	m.state.logger.Printf("session found: key=%s PID=%d sessionID=%s", msg.ProjectID, msg.PID, msg.SessionID)
	at, ok := m.state.activeTerminals[msg.ProjectID]
	if !ok {
		m.state.Unlock()
		return m, nil
	}
	at.SessionPID = msg.PID
	at.SessionID = msg.SessionID
	workDir := at.WorkDir
	m.state.NotifyAll()
	m.state.Unlock()
	return m, waitForLogCmd(msg.ProjectID, msg.PID, msg.SessionID, msg.LogPath, workDir, m.state.logger)
}

func (m Model) handleWatcherReady(msg watcherReadyMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	if at, ok := m.state.activeTerminals[msg.ProjectID]; ok {
		// Stale guard: if session has already switched past this msg, discard.
		if at.SessionID != "" && msg.SessionID != "" && at.SessionID != msg.SessionID {
			m.state.logger.Printf("watcher ready: key=%s STALE (at=%s, msg=%s), discarding",
				msg.ProjectID, at.SessionID, msg.SessionID)
			m.state.Unlock()
			msg.Watcher.Stop()
			return m, nil
		}
		at.SessionID = msg.SessionID
		at.Watcher = msg.Watcher
		if at.State == watcher.StateUnknown && msg.LogPath != "" {
			state, question := watcher.ReadLastState(msg.LogPath)
			if state != watcher.StateUnknown {
				at.State = state
				at.LastQuestion = question
				at.StateChangedAt = time.Now()
			}
		}
		m.state.logger.Printf("watcher ready: key=%s state=%s", msg.ProjectID, at.State)
		m.state.NotifyAll()
		m.state.Unlock()
		return m, watchNextCmd(msg.ProjectID, msg.Watcher)
	}
	m.state.Unlock()
	msg.Watcher.Stop()
	return m, nil
}

func (m Model) handleSessionLost(msg sessionLostMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	m.state.logger.Printf("session lost: %s — %s", msg.ProjectID, msg.Text)
	if at, ok := m.state.activeTerminals[msg.ProjectID]; ok {
		at.State = watcher.StateEnded
		at.StateChangedAt = time.Now()
		if at.Watcher != nil {
			at.Watcher.Stop()
		}
		m.state.NotifyAll()
	}
	m.state.Unlock()
	return m, nil
}

// === Cmd factories ===

// RunServerInit performs server-init logic synchronously (for zpit serve startup).
// Runs session scan and provider validation on the AppState.
func RunServerInit(state *AppState) {
	seenMissing := make(map[string]bool)
	var missingProviders []string

	for _, project := range state.projects {
		if project.Tracker == "" || project.Repo == "" {
			continue
		}
		if _, ok := state.clients[project.Tracker]; !ok {
			if !seenMissing[project.Tracker] {
				seenMissing[project.Tracker] = true
				missingProviders = append(missingProviders, project.Tracker)
			}
		}
	}
	if len(missingProviders) > 0 {
		state.logger.Printf("Tracker unavailable (token not set?): %s", strings.Join(missingProviders, ", "))
	}
}

// serverInitCmds returns tea.Cmd slices for server-init tasks (used by local TUI Init).
func (m Model) serverInitCmds() []tea.Cmd {
	var cmds []tea.Cmd

	seenMissing := make(map[string]bool)
	var missingProviders []string

	for _, project := range m.state.projects {
		if project.Tracker == "" || project.Repo == "" {
			continue
		}
		if _, ok := m.state.clients[project.Tracker]; !ok {
			if !seenMissing[project.Tracker] {
				seenMissing[project.Tracker] = true
				missingProviders = append(missingProviders, project.Tracker)
			}
		}
	}

	if len(missingProviders) > 0 {
		msg := fmt.Sprintf("Tracker unavailable (token not set?): %s", strings.Join(missingProviders, ", "))
		m.state.logger.Println(msg)
		cmds = append(cmds, func() tea.Msg {
			return StatusMsg{Text: msg}
		})
	}

	// Scan for already-running Claude Code sessions.
	cmds = append(cmds, m.scanExistingSessionsCmd())

	return cmds
}

// scanExistingSessionsCmd scans all projects for already-running Claude Code sessions at startup.
func (m Model) scanExistingSessionsCmd() tea.Cmd {
	type projectInfo struct {
		id   string
		path string
	}
	seen := make(map[string]bool)
	var projects []projectInfo
	for _, p := range m.state.projects {
		path := platform.ResolvePath(p.Path.Windows, p.Path.WSL)
		if path == "" {
			m.state.logger.Printf("session scan: skipping project %q (empty path)", p.ID)
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		projects = append(projects, projectInfo{id: p.ID, path: path})
	}

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return existingSessionsMsg{Source: "startup"}
		}
		var entries []existingSessionEntry
		for _, p := range projects {
			sessions, err := watcher.FindActiveSessions(claudeHome, p.path)
			if err != nil || len(sessions) == 0 {
				continue
			}
			// Track ALL active sessions per project, not just the latest.
			for _, s := range sessions {
				logPath := watcher.LogFilePath(claudeHome, p.path, s.SessionID)
				entries = append(entries, existingSessionEntry{
					ProjectID: p.id,
					PID:       s.PID,
					SessionID: s.SessionID,
					WorkDir:   p.path,
					LogPath:   logPath,
				})
			}
		}
		return existingSessionsMsg{Source: "startup", Entries: entries}
	}
}

// startWatcherDirCmd starts session discovery for a given tracking key and work directory.
// Acquires RLock to snapshot tracked PIDs. Must NOT be called while holding a write lock.
func (m Model) startWatcherDirCmd(trackingKey, workDir string) tea.Cmd {
	// Snapshot already-tracked PIDs so we can exclude them when picking a session.
	m.state.RLock()
	excludePIDs := m.trackedPIDs()
	m.state.RUnlock()
	return m.startWatcherDirCmdWithExcludes(trackingKey, workDir, excludePIDs)
}

// startWatcherDirCmdWithExcludes starts session discovery with pre-computed exclude PIDs.
// Used when the caller already holds a lock and has snapshotted the PIDs.
func (m Model) startWatcherDirCmdWithExcludes(trackingKey, workDir string, excludePIDs map[int]bool) tea.Cmd {
	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return WatcherErrorMsg{ProjectID: trackingKey, Err: err}
		}

		var candidates []watcher.SessionInfo
		for attempt := range sessionRetryMax {
			sessions, err := watcher.FindActiveSessions(claudeHome, workDir)
			if err != nil {
				return WatcherErrorMsg{ProjectID: trackingKey, Err: err}
			}
			// Filter out sessions already tracked by other ActiveTerminals.
			candidates = candidates[:0]
			for _, s := range sessions {
				if !excludePIDs[s.PID] {
					candidates = append(candidates, s)
				}
			}
			if len(candidates) > 0 {
				break
			}
			// If all sessions are tracked but some exist, a new one may appear soon.
			if attempt < sessionRetryMax-1 {
				time.Sleep(sessionRetryInterval)
			}
		}

		if len(candidates) == 0 {
			return sessionLostMsg{ProjectID: trackingKey, Text: "no active session found (waited 30s)"}
		}

		latest := candidates[0]
		for _, s := range candidates[1:] {
			if s.StartedAt > latest.StartedAt {
				latest = s
			}
		}

		logPath := watcher.LogFilePath(claudeHome, workDir, latest.SessionID)
		return sessionFoundMsg{ProjectID: trackingKey, PID: latest.PID, SessionID: latest.SessionID, LogPath: logPath}
	}
}

// waitForLogCmd phase 2: wait for the JSONL file to be created, then start the watcher.
// Re-reads {pid}.json each iteration to detect /resume session switches.
func waitForLogCmd(projectID string, pid int, sessionID, logPath, workDir string, logger *log.Logger) tea.Cmd {
	return func() tea.Msg {
		logger.Printf("waitForLog: key=%s pid=%d sessionID=%s path=%s", projectID, pid, sessionID, logPath)

		claudeHome, _ := watcher.ClaudeHome() // best-effort; empty means skip re-check
		warned := false

		for attempt := 0; ; attempt++ {
			if !watcher.IsClaudeProcess(pid) {
				logger.Printf("waitForLog: key=%s pid=%d died at attempt %d", projectID, pid, attempt)
				return sessionLostMsg{ProjectID: projectID, Text: "session ended before log created"}
			}

			// Re-check session file for /resume detection.
			// On switch, return sessionFoundMsg to let update loop sync AT.SessionID first.
			if claudeHome != "" && workDir != "" {
				if info, err := watcher.ReadSessionByPID(claudeHome, pid); err == nil {
					if info.SessionID != sessionID {
						newLogPath := watcher.LogFilePath(claudeHome, workDir, info.SessionID)
						logger.Printf("waitForLog: key=%s session switched %s → %s",
							projectID, sessionID, info.SessionID)
						return sessionFoundMsg{
							ProjectID: projectID,
							PID:       pid,
							SessionID: info.SessionID,
							LogPath:   newLogPath,
						}
					}
				} else {
					logger.Printf("waitForLog: key=%s ReadSessionByPID failed: %v", projectID, err)
				}
			} else {
				logger.Printf("waitForLog: key=%s skip re-check (claudeHome=%q workDir=%q)", projectID, claudeHome, workDir)
			}

			if _, err := os.Stat(logPath); err == nil {
				logger.Printf("waitForLog: key=%s file found at attempt %d (sessionID=%s)", projectID, attempt, sessionID)
				w, err := watcher.New(projectID, logPath)
				if err != nil {
					logger.Printf("waitForLog: key=%s watcher creation failed: %v", projectID, err)
					return WatcherErrorMsg{ProjectID: projectID, Err: err}
				}
				return watcherReadyMsg{ProjectID: projectID, SessionID: sessionID, Watcher: w, LogPath: logPath}
			}

			if !warned && attempt >= logWaitWarnAfter {
				logger.Printf("waitForLog: key=%s still waiting after %d attempts, PID %d alive — continuing",
					projectID, attempt, pid)
				warned = true
			}
			time.Sleep(sessionRetryInterval)
		}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func watchNextCmd(projectID string, w *watcher.Watcher) tea.Cmd {
	return func() tea.Msg {
		events, err := w.WatchOnce()
		if err != nil {
			return WatcherErrorMsg{ProjectID: projectID, Err: err}
		}
		if events == nil {
			return nil
		}
		return AgentEventMsg{ProjectID: projectID, Events: events}
	}
}

// === Tick-driven monitoring ===

func (m *Model) checkSessionLiveness() []tea.Cmd {
	m.state.Lock()
	now := time.Now()
	if now.Sub(m.state.lastLivenessCheck) < livenessCheckInterval {
		m.state.Unlock()
		return nil
	}
	m.state.lastLivenessCheck = now

	claudeHome, _ := watcher.ClaudeHome() // best-effort; empty means skip /resume detection

	var cmds []tea.Cmd
	changed := false

	for projectID, at := range m.state.activeTerminals {
		// Clean up ended sessions after display duration.
		if at.State == watcher.StateEnded {
			if now.Sub(at.StateChangedAt) >= endedDisplayDuration {
				m.state.logger.Printf("session removed: key=%s", projectID)
				delete(m.state.activeTerminals, projectID)
				changed = true
			}
			continue
		}

		if at.SessionPID <= 0 {
			continue
		}
		if !watcher.IsClaudeProcess(at.SessionPID) {
			m.state.logger.Printf("session PID %d ended: %s", at.SessionPID, projectID)
			at.State = watcher.StateEnded
			at.StateChangedAt = now
			at.LastQuestion = ""
			at.PermissionMessage = ""
			deletePermissionSignal(at.SessionID)
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
			changed = true
			continue
		}

		// /resume detection: re-read {pid}.json and check if sessionId changed.
		if claudeHome != "" && at.SessionID != "" && at.WorkDir != "" {
			if info, err := watcher.ReadSessionByPID(claudeHome, at.SessionPID); err == nil {
				if info.SessionID != at.SessionID {
					newLogPath := watcher.LogFilePath(claudeHome, at.WorkDir, info.SessionID)
					m.state.logger.Printf("session switch detected: key=%s old=%s new=%s",
						projectID, at.SessionID, info.SessionID)

					// Stop old watcher and restart for new session.
					if at.Watcher != nil {
						at.Watcher.Stop()
						at.Watcher = nil
					}
					at.SessionID = info.SessionID
					at.State = watcher.StateUnknown
					at.StateChangedAt = now
					at.LastQuestion = ""
					changed = true

					cmds = append(cmds, waitForLogCmd(
						projectID, at.SessionPID, info.SessionID, newLogPath, at.WorkDir, m.state.logger))
				}
			} else {
				m.state.logger.Printf("liveness: key=%s ReadSessionByPID(%d) failed: %v", projectID, at.SessionPID, err)
			}
		} else if claudeHome != "" {
			m.state.logger.Printf("liveness: key=%s skip resume check (sessionID=%q workDir=%q)", projectID, at.SessionID, at.WorkDir)
		}
	}

	if changed {
		m.state.NotifyAll()
	}
	m.state.Unlock()
	return cmds
}

// checkPermissionSignals scans ~/.zpit/signals/ for permission signal files
// and updates matching ActiveTerminals to StatePermission.
func (m *Model) checkPermissionSignals() {
	m.state.Lock()
	now := time.Now()
	if now.Sub(m.state.lastPermissionCheck) < permissionCheckInterval {
		m.state.Unlock()
		return
	}
	m.state.lastPermissionCheck = now
	m.state.Unlock()

	dir := signalDir()
	if dir == "" {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory may not exist yet
	}

	// Collect parsed signals from filesystem (no lock needed for I/O).
	type parsedSignal struct {
		sig  permissionSignal
		name string
	}
	var signals []parsedSignal
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "permission-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var sig permissionSignal
		if err := json.Unmarshal(data, &sig); err != nil || sig.SessionID == "" {
			continue
		}
		signals = append(signals, parsedSignal{sig: sig, name: name})
	}

	if len(signals) == 0 {
		return
	}

	// Match signals to active terminals under lock.
	m.state.Lock()
	changed := false
	var staleFiles []string
	for _, ps := range signals {
		matched := false
		for projectID, at := range m.state.activeTerminals {
			if at.SessionID != ps.sig.SessionID {
				continue
			}
			matched = true
			if at.State == watcher.StatePermission {
				break // already in permission state
			}
			m.state.logger.Printf("permission detected: key=%s session=%s msg=%q", projectID, ps.sig.SessionID, ps.sig.Message)
			at.State = watcher.StatePermission
			at.PermissionMessage = ps.sig.Message
			at.StateChangedAt = now
			changed = true
			projectName := m.projectName(projectID)
			m.state.notifier.NotifyWaiting(projectID, projectName, ps.sig.Message)
			break
		}
		if !matched {
			staleFiles = append(staleFiles, ps.name)
		}
	}
	if changed {
		m.state.NotifyAll()
	}
	m.state.Unlock()

	// Clean up stale signal files outside lock.
	for _, name := range staleFiles {
		os.Remove(filepath.Join(dir, name))
	}
}

// checkNewSessions checks if sessionScanInterval has elapsed and, if so, returns a tea.Cmd
// that scans for externally-launched Claude Code sessions not yet tracked in activeTerminals.
// Follows the same tick + interval + lock pattern as checkSessionLiveness.
func (m *Model) checkNewSessions() tea.Cmd {
	m.state.Lock()
	now := time.Now()
	if now.Sub(m.state.lastSessionScan) < sessionScanInterval {
		m.state.Unlock()
		return nil
	}
	m.state.lastSessionScan = now
	trackedPIDs := m.trackedPIDs()
	m.state.Unlock()

	// Build project list (same logic as scanExistingSessionsCmd).
	type projectInfo struct {
		id   string
		path string
	}
	seen := make(map[string]bool)
	var projects []projectInfo
	m.state.RLock()
	for _, p := range m.state.projects {
		path := platform.ResolvePath(p.Path.Windows, p.Path.WSL)
		if path == "" {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		projects = append(projects, projectInfo{id: p.ID, path: path})
	}
	logger := m.state.logger
	m.state.RUnlock()

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			logger.Printf("periodic session scan: ClaudeHome error: %v", err)
			return nil
		}
		var entries []existingSessionEntry
		for _, p := range projects {
			sessions, err := watcher.FindActiveSessions(claudeHome, p.path)
			if err != nil || len(sessions) == 0 {
				continue
			}
			for _, s := range sessions {
				if trackedPIDs[s.PID] {
					continue
				}
				logPath := watcher.LogFilePath(claudeHome, p.path, s.SessionID)
				entries = append(entries, existingSessionEntry{
					ProjectID: p.id,
					PID:       s.PID,
					SessionID: s.SessionID,
					WorkDir:   p.path,
					LogPath:   logPath,
				})
			}
		}
		if len(entries) == 0 {
			return nil
		}
		logger.Printf("periodic session scan: found %d new session(s)", len(entries))
		return existingSessionsMsg{Source: "periodic", Entries: entries}
	}
}

// === Helpers ===

// nextTrackingKey returns a unique key for activeTerminals.
// First session uses baseKey as-is; subsequent ones get "#2", "#3", etc.
func (m Model) nextTrackingKey(baseKey string) string {
	if _, exists := m.state.activeTerminals[baseKey]; !exists {
		return baseKey
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s#%d", baseKey, i)
		if _, exists := m.state.activeTerminals[candidate]; !exists {
			return candidate
		}
	}
}

// trackedPIDs returns all PIDs currently tracked in activeTerminals.
func (m Model) trackedPIDs() map[int]bool {
	pids := make(map[int]bool, len(m.state.activeTerminals))
	for _, at := range m.state.activeTerminals {
		if at.SessionPID != 0 {
			pids[at.SessionPID] = true
		}
	}
	return pids
}

// signalDir returns the path to the permission signal directory (~/.zpit/signals/).
func signalDir() string {
	base, err := config.BaseDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "signals")
}

// deletePermissionSignal removes the permission signal file for the given session ID.
func deletePermissionSignal(sessionID string) {
	if sessionID == "" {
		return
	}
	dir := signalDir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, "permission-"+sessionID+".json"))
}
