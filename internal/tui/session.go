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
	"github.com/zac15987/zpit/internal/locale"
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
	WorktreeBranch    string // non-empty when session runs in a git worktree (e.g. "feat/19-slug")
	ZplexSessionID    string // non-empty only when launched via the zplex backend
	zplexWaiting      bool   // true after we PATCH agent_state=waiting for the current permission episode; used by T8 to issue the matching active PATCH
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
	ProjectID      string
	PID            int
	SessionID      string
	WorkDir        string
	LogPath        string
	WorktreeBranch string // non-empty when session was found in a git worktree
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
	cmds = append(cmds, m.checkPermissionSignals()...)
	cmds = append(cmds, m.checkZplexActiveResync()...)
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
		// Desktop agent fill-in: the entry already exists in activeTerminals with
		// SessionPID==0 (created by handleDesktopAgentLaunched). Periodic scan
		// finds the spawned claude.exe and back-fills PID/SessionID so the
		// liveness sweep can later detect terminal close.
		if strings.HasPrefix(entry.ProjectID, "desktop:") {
			if at, ok := m.state.activeTerminals[entry.ProjectID]; ok && at.SessionPID == 0 {
				m.state.logger.Printf("  desktop fill-in: key=%s PID=%d sessionID=%s", entry.ProjectID, entry.PID, entry.SessionID)
				at.SessionPID = entry.PID
				at.SessionID = entry.SessionID
				at.StateChangedAt = time.Now()
				currentPIDs[entry.PID] = true
				cmds = append(cmds, waitForLogCmd(entry.ProjectID, entry.PID, entry.SessionID, entry.LogPath, entry.WorkDir, m.state.logger))
				continue
			}
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
			WorktreeBranch: entry.WorktreeBranch,
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
		// If initial state is already Waiting, trigger notification now.
		// The watcher only monitors new events (offset = current file size),
		// so if Claude Code entered Waiting before the watcher started,
		// no new event will arrive to trigger notification via handleAgentEvent.
		if at.State == watcher.StateWaiting {
			projectName := m.projectName(msg.ProjectID)
			if m.state.notifier.NotifyWaiting(msg.ProjectID, projectName, at.LastQuestion) {
				m.state.logger.Printf("watcher ready: notification sent: key=%s state=Waiting", msg.ProjectID)
			} else {
				m.state.logger.Printf("watcher ready: notification suppressed by cooldown: key=%s", msg.ProjectID)
			}
			if w := m.state.notifier.ConsumeWarning(); w != "" {
				m.setStatus(fmt.Sprintf(locale.T(locale.KeySoundFileNotFound), m.state.cfg.Notification.SoundFile))
			}
		}
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
// Also enumerates each project's git worktrees and scans those paths for sessions.
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

	// wtManager is read-only after init — safe to capture without lock.
	wtManager := m.state.wtManager
	logger := m.state.logger

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return existingSessionsMsg{Source: "startup"}
		}
		var entries []existingSessionEntry
		for _, p := range projects {
			// Scan main project directory.
			sessions, err := watcher.FindActiveSessions(claudeHome, p.path)
			if err == nil {
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

			// Scan worktree directories.
			worktrees, err := wtManager.List(p.path)
			if err != nil {
				logger.Printf("session scan: listing worktrees for %s failed: %v", p.path, err)
				continue
			}
			for _, wt := range worktrees {
				wtSessions, err := watcher.FindActiveSessions(claudeHome, wt.Path)
				if err != nil || len(wtSessions) == 0 {
					continue
				}
				for _, s := range wtSessions {
					logPath := watcher.LogFilePath(claudeHome, wt.Path, s.SessionID)
					entries = append(entries, existingSessionEntry{
						ProjectID:      p.id,
						PID:            s.PID,
						SessionID:      s.SessionID,
						WorkDir:        wt.Path,
						LogPath:        logPath,
						WorktreeBranch: wt.Branch,
					})
				}
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
				w, err := watcher.New(projectID, logPath, logger)
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

// killTerminalCmd terminates the given PID with exit code 0 (via KillWithZeroExit).
// On Windows, also kills the parent shell (cmd/powershell/pwsh) to close the WT tab.
// Exit code 0 is critical: WT's closeOnExit "graceful" (default) only closes on exit 0.
// Must NOT be called while holding a lock.
func (m Model) killTerminalCmd(trackingKey, displayName string, pid int) tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		logger.Printf("terminal kill: key=%s pid=%d", trackingKey, pid)

		// Find parent shell BEFORE killing (process must exist for snapshot lookup).
		parentPID, parentName := terminal.FindParentShell(pid)

		if err := terminal.KillWithZeroExit(pid); err != nil {
			logger.Printf("terminal kill: failed key=%s pid=%d err=%v", trackingKey, pid, err)
			return KillTerminalMsg{TrackingKey: trackingKey, Err: fmt.Errorf("kill process %d: %w", pid, err)}
		}
		logger.Printf("terminal kill: success key=%s pid=%d", trackingKey, pid)

		// Kill parent shell to close the WT tab.
		if parentPID > 0 {
			logger.Printf("terminal kill: closing WT tab parent=%s pid=%d", parentName, parentPID)
			terminal.KillProcess(parentPID)
		}

		return KillTerminalMsg{TrackingKey: trackingKey, Err: nil}
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

	// Capture desktop agent identity BEFORE the cleanup loop. The cleanup pass
	// below may delete the desktop terminal entry when it has been StateEnded
	// for longer than endedDisplayDuration; once deleted, the tracking key is
	// gone and the AC-8 exit log would render with empty agent name and PID 0
	// (the bug AC-12(j) catches). Snapshotting here preserves the values for
	// the post-loop clear block.
	var desktopAgentKey, desktopAgentName string
	var desktopAgentPID int
	if m.state.activeDesktopAgent != nil {
		da := m.state.activeDesktopAgent
		for key, at := range m.state.activeTerminals {
			if at == da && strings.HasPrefix(key, "desktop:") {
				desktopAgentKey = key
				desktopAgentName = strings.TrimPrefix(key, "desktop:")
				desktopAgentPID = at.SessionPID
				break
			}
		}
	}

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

	// Desktop agent exit detection (AC-8):
	// If activeDesktopAgent is set and its tracking entry has been marked StateEnded
	// (or the entry was removed by the cleanup loop above), clear activeDesktopAgent
	// and dispatch the exit message. The agent identity (key/name/PID) was captured
	// before the loop ran, so the log line is correct even when the entry was deleted.
	if m.state.activeDesktopAgent != nil {
		da := m.state.activeDesktopAgent
		// Re-check the live tracking state after the cleanup loop.
		_, stillTracked := m.state.activeTerminals[desktopAgentKey]

		shouldClear := false
		switch {
		case desktopAgentKey == "":
			// activeDesktopAgent was set but no matching entry existed even at the start
			// of this pass — treat as stale and clear (with whatever info we have, which
			// is empty in this edge case).
			shouldClear = true
		case !stillTracked:
			// Cleanup loop removed the entry — captured info is still valid.
			shouldClear = true
		case da.State == watcher.StateEnded:
			// Entry still present but marked ended — first liveness pass after PID death.
			shouldClear = true
		}

		if shouldClear {
			m.state.logger.Printf("desktop agent %s (PID %d) exited", desktopAgentName, desktopAgentPID)
			m.state.activeDesktopAgent = nil
			changed = true
			agentNameCopy := desktopAgentName
			pidCopy := desktopAgentPID
			cmds = append(cmds, func() tea.Msg {
				return DesktopAgentExitedMsg{AgentName: agentNameCopy, PID: pidCopy}
			})
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
// Returns fire-and-forget PATCH cmds for any terminals that newly entered permission state.
func (m *Model) checkPermissionSignals() []tea.Cmd {
	m.state.Lock()
	now := time.Now()
	if now.Sub(m.state.lastPermissionCheck) < permissionCheckInterval {
		m.state.Unlock()
		return nil
	}
	m.state.lastPermissionCheck = now
	m.state.Unlock()

	dir := signalDir()
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // directory may not exist yet
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
		return nil
	}

	// Match signals to active terminals under lock. Collect session IDs that newly
	// entered permission state so we can build PATCH cmds after unlocking.
	m.state.Lock()
	changed := false
	var staleFiles []string
	var waitingIDs []string // zplex session IDs needing a "waiting" PATCH
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
			if !m.state.notifier.NotifyWaiting(projectID, projectName, ps.sig.Message) {
				m.state.logger.Printf("notification suppressed by cooldown: key=%s", projectID)
			}
			if w := m.state.notifier.ConsumeWarning(); w != "" {
				m.setStatus(fmt.Sprintf(locale.T(locale.KeySoundFileNotFound), m.state.cfg.Notification.SoundFile))
			}
			// AC-8: issue waiting PATCH if terminal has a zplex session and is not already marked waiting.
			if at.ZplexSessionID != "" && !at.zplexWaiting {
				at.zplexWaiting = true
				waitingIDs = append(waitingIDs, at.ZplexSessionID)
			}
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

	// Build fire-and-forget PATCH cmds after releasing the lock.
	var cmds []tea.Cmd
	for _, id := range waitingIDs {
		cmds = append(cmds, m.patchZplexStateCmd(id, "waiting"))
	}
	return cmds
}

// checkZplexActiveResync issues an active PATCH for any terminal that was
// patched to waiting but has since left the permission state.
func (m *Model) checkZplexActiveResync() []tea.Cmd {
	m.state.Lock()
	var ids []string
	for _, at := range m.state.activeTerminals {
		if at.zplexWaiting && at.State != watcher.StatePermission && at.ZplexSessionID != "" {
			ids = append(ids, at.ZplexSessionID)
			at.zplexWaiting = false
		}
	}
	m.state.Unlock()
	var cmds []tea.Cmd
	for _, id := range ids {
		cmds = append(cmds, m.patchZplexStateCmd(id, "active"))
	}
	return cmds
}

// checkNewSessions checks if sessionScanInterval has elapsed and, if so, returns a tea.Cmd
// that scans for externally-launched Claude Code sessions not yet tracked in activeTerminals.
// Also enumerates each project's git worktrees and scans those paths for sessions.
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
	var desktopWorkDir, desktopKey string
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
	// Desktop agent's cwd ($HOME) is not in m.state.projects, so the per-project
	// scan above never finds its session. Capture the desktop entry's WorkDir
	// + tracking key while we still pre-resolve the entry that is missing a PID.
	if m.state.activeDesktopAgent != nil {
		da := m.state.activeDesktopAgent
		if da.SessionPID == 0 && da.WorkDir != "" {
			for key, at := range m.state.activeTerminals {
				if at == da && strings.HasPrefix(key, "desktop:") {
					desktopKey = key
					desktopWorkDir = da.WorkDir
					break
				}
			}
		}
	}
	logger := m.state.logger
	m.state.RUnlock()

	// wtManager is read-only after init — safe to capture without lock.
	wtManager := m.state.wtManager

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			logger.Printf("periodic session scan: ClaudeHome error: %v", err)
			return nil
		}
		var entries []existingSessionEntry
		for _, p := range projects {
			// Scan main project directory.
			sessions, err := watcher.FindActiveSessions(claudeHome, p.path)
			if err == nil {
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

			// Scan worktree directories.
			worktrees, err := wtManager.List(p.path)
			if err != nil {
				logger.Printf("session scan: listing worktrees for %s failed: %v", p.path, err)
				continue
			}
			for _, wt := range worktrees {
				wtSessions, err := watcher.FindActiveSessions(claudeHome, wt.Path)
				if err != nil || len(wtSessions) == 0 {
					continue
				}
				for _, s := range wtSessions {
					if trackedPIDs[s.PID] {
						continue
					}
					logPath := watcher.LogFilePath(claudeHome, wt.Path, s.SessionID)
					entries = append(entries, existingSessionEntry{
						ProjectID:      p.id,
						PID:            s.PID,
						SessionID:      s.SessionID,
						WorkDir:        wt.Path,
						LogPath:        logPath,
						WorktreeBranch: wt.Branch,
					})
				}
			}
		}
		// Desktop agent: scan its cwd ($HOME) and emit entries with the
		// "desktop:" tracking key so handleExistingSessions fills in the existing
		// entry rather than creating a new one.
		if desktopWorkDir != "" {
			deskSessions, err := watcher.FindActiveSessions(claudeHome, desktopWorkDir)
			if err == nil {
				for _, s := range deskSessions {
					if trackedPIDs[s.PID] {
						continue
					}
					logPath := watcher.LogFilePath(claudeHome, desktopWorkDir, s.SessionID)
					entries = append(entries, existingSessionEntry{
						ProjectID: desktopKey,
						PID:       s.PID,
						SessionID: s.SessionID,
						WorkDir:   desktopWorkDir,
						LogPath:   logPath,
					})
				}
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
