package tui

// sessions.go — History (Session Browser) view: cmd factories + msg handlers.
//
// Lock protocol:
//   - Cmd factory methods (scanHistoryFoldersCmd, scanHistorySessionsCmd,
//     exportSessionsCmd, importBundleCmd, loadHistoryManifestCmd):
//     pure tea.Cmd constructors; the closure runs filesystem I/O off the
//     main goroutine. They DO NOT touch AppState directly.
//   - Handler methods (handleHistoryFoldersScanned, handleHistorySessionsScanned,
//     handleExportStarted, handleExportCompleted, handleImportStarted,
//     handleImportProgress, handleImportCompleted, handleCollisionPrompt):
//     mutate per-connection Model fields only; AppState is read-only here
//     for active-session detection. Each acquires RLock for the brief read.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/sessionsync"
	"github.com/zac15987/zpit/internal/watcher"
)

// === Private message types ===

// manifestLoadedMsg is a private message used between loadHistoryManifestCmd
// and the import-step 1 transition.
type manifestLoadedMsg struct {
	Manifest   *sessionsync.Manifest
	BundlePath string
	Err        error
}

// sessionCollisionsMsg carries the result of a pre-import collision scan.
// It drives the collision-prompt queue in the TUI before the actual import run.
type sessionCollisionsMsg struct {
	SessionCollisions []string // session IDs that collide with destination
	MemoryCollision   bool     // true when memory/ dir exists in destination
}

// === Cmd factories ===

// scanHistoryFoldersCmd returns a tea.Cmd that lists encoded folders under
// ~/.claude/projects/ and computes which encoded folders contain at least one
// currently alive Claude Code session. The active map drives the 🟢 marker on
// each folder row in the History view (AC-1).
func (m Model) scanHistoryFoldersCmd() tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		root, err := sessionsync.ProjectsRoot()
		if err != nil {
			logger.Printf("history: scan folders error: %v", err)
			return HistoryFoldersScannedMsg{Err: err}
		}
		folders, err := sessionsync.ScanFolders(root)
		if err != nil {
			logger.Printf("history: scan folders error: %v", err)
			return HistoryFoldersScannedMsg{Err: err}
		}
		activeByFolder := computeActivePIDsByFolder(logger)
		return HistoryFoldersScannedMsg{
			Folders:            folders,
			ActivePIDsByFolder: activeByFolder,
		}
	}
}

// computeActivePIDsByFolder reads ~/.claude/sessions/*.json once and returns a
// map from encoded folder name to true for every folder that has at least one
// session whose backing PID is alive. The encoded folder is derived from the
// session file's `cwd` field via watcher.EncodeCwd.
func computeActivePIDsByFolder(logger *log.Logger) map[string]bool {
	result := make(map[string]bool)
	claudeHome, err := watcher.ClaudeHome()
	if err != nil {
		return result
	}
	sessDir := filepath.Join(claudeHome, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessDir, e.Name()))
		if err != nil {
			continue
		}
		var info struct {
			PID int    `json:"pid"`
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		if info.Cwd == "" {
			continue
		}
		if !watcher.IsClaudeProcess(info.PID) {
			continue
		}
		encoded := watcher.EncodeCwd(info.Cwd)
		result[encoded] = true
	}
	return result
}

// scanHistorySessionsCmd returns a tea.Cmd that lists sessions inside one
// encoded folder and detects which ones are currently alive.
func (m Model) scanHistorySessionsCmd(folderName string) tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		root, err := sessionsync.ProjectsRoot()
		if err != nil {
			return HistorySessionsScannedMsg{FolderName: folderName, Err: err}
		}
		folderPath := filepath.Join(root, folderName)
		sessions, err := sessionsync.ScanSessions(folderPath)
		if err != nil {
			logger.Printf("history: scan sessions error folder=%s err=%v", folderName, err)
			return HistorySessionsScannedMsg{FolderName: folderName, Err: err}
		}
		// Active-PID detection: scan ~/.claude/sessions/*.json for any session
		// whose ID appears in this folder. Walking ~/.claude/sessions/ directly
		// is more robust than calling watcher.FindActiveSessions(claudeHome, projectPath)
		// because we'd have to reverse-derive projectPath from the encoded folder
		// (lossy). Instead we filter session files by SessionID.
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return HistorySessionsScannedMsg{FolderName: folderName, Sessions: sessions, Err: err}
		}
		active := detectActiveSessionsInFolder(claudeHome, sessions, logger, folderName)
		return HistorySessionsScannedMsg{FolderName: folderName, Sessions: sessions, ActiveSessionIDs: active}
	}
}

// detectActiveSessionsInFolder reads ~/.claude/sessions/*.json and returns
// session IDs from sessions whose PID is alive AND SessionID matches one of
// the provided sessions.
func detectActiveSessionsInFolder(claudeHome string, sessions []sessionsync.SessionInfo, logger *log.Logger, folderName string) []string {
	sessDir := filepath.Join(claudeHome, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil
	}

	sessSet := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		sessSet[s.SessionID] = true
	}

	var active []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessDir, e.Name()))
		if err != nil {
			continue
		}
		var info struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		if !sessSet[info.SessionID] {
			continue
		}
		if !watcher.IsClaudeProcess(info.PID) {
			continue
		}
		active = append(active, info.SessionID)
	}

	if len(active) > 0 {
		logger.Printf("history: detected %d active session(s) in folder %s: %s",
			len(active), folderName, strings.Join(active, ", "))
	}
	return active
}

// exportSessionsCmd packs the selected sessions into a zip bundle.
func (m Model) exportSessionsCmd(folderName string, sessionIDs []string, includeMemory bool, outputPath string) tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		root, err := sessionsync.ProjectsRoot()
		if err != nil {
			return ExportCompletedMsg{OutputPath: outputPath, Sessions: sessionIDs, Err: err}
		}
		folderPath := filepath.Join(root, folderName)
		logger.Printf("history: export start sessions=%d memory=%v output=%s",
			len(sessionIDs), includeMemory, outputPath)
		result, err := sessionsync.Pack(sessionsync.PackOptions{
			SourceFolder:     folderPath,
			SourceEncodedCwd: folderName,
			SessionIDs:       sessionIDs,
			IncludeMemory:    includeMemory,
			OutputZipPath:    outputPath,
			Logger:           logger,
		})
		if err != nil {
			logger.Printf("history: export error: %v", err)
			return ExportCompletedMsg{OutputPath: outputPath, Sessions: sessionIDs, Err: err}
		}
		logger.Printf("history: export done sessions=%d bytes=%d path=%s",
			len(sessionIDs), result.BytesWritten, outputPath)
		return ExportCompletedMsg{
			OutputPath:   outputPath,
			Sessions:     sessionIDs,
			BytesWritten: result.BytesWritten,
		}
	}
}

// loadHistoryManifestCmd peeks at a bundle's manifest before showing the
// preview screen. Used between import-step 0 and import-step 1.
func (m Model) loadHistoryManifestCmd(bundlePath string) tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		manifest, err := sessionsync.LoadManifest(bundlePath)
		if err != nil {
			logger.Printf("history: load manifest error bundle=%s err=%v", bundlePath, err)
			return manifestLoadedMsg{BundlePath: bundlePath, Err: err}
		}
		return manifestLoadedMsg{Manifest: manifest, BundlePath: bundlePath}
	}
}

// importBundleCmd runs pass-2 of the import operation: the actual Unpack with
// pre-resolved collision decisions. The TUI must call detectImportCollisionsCmd
// first (pass-1) and let the user answer all collision prompts before calling
// this cmd.
func (m Model) importBundleCmd(
	bundlePath string,
	destDir string,
	destCwd string,
	selection map[string]bool,
	decisions map[string]sessionsync.CollisionDecision,
	memoryDecision sessionsync.CollisionDecision,
) tea.Cmd {
	logger := m.state.logger
	encoded := filepath.Base(destDir)
	return func() tea.Msg {
		logger.Printf("history: import start bundle=%s dest=%s", bundlePath, encoded)
		resolver := func(sessionID string, isMemory bool) sessionsync.CollisionDecision {
			if isMemory {
				return memoryDecision
			}
			if d, ok := decisions[sessionID]; ok {
				return d
			}
			// Default: should never happen if the pre-pass was thorough.
			return sessionsync.DecisionOverwrite
		}
		result, err := sessionsync.Unpack(sessionsync.UnpackOptions{
			BundlePath:  bundlePath,
			DestDir:     destDir,
			DestCwd:     destCwd,
			Selection:   selection,
			OnCollision: resolver,
			Logger:      logger,
		})
		if err != nil {
			logger.Printf("history: import error: %v", err)
			return ImportCompletedMsg{DestEncoded: encoded, Err: err}
		}
		logger.Printf("history: import done written=%d skipped=%d cancelled=%d",
			result.Written, result.Skipped, result.Cancelled)
		return ImportCompletedMsg{
			DestEncoded:  encoded,
			Written:      result.Written,
			Skipped:      result.Skipped,
			Cancelled:    result.Cancelled,
			MemoryStatus: result.MemoryStatus,
		}
	}
}

// detectImportCollisionsCmd returns a sessionCollisionsMsg so the TUI can
// stage collision prompts before committing to the import run. This is pass-1;
// importBundleCmd is pass-2.
func (m Model) detectImportCollisionsCmd(destDir string, sessionIDs []string, includeMemory bool) tea.Cmd {
	return func() tea.Msg {
		collisions := sessionsync.DetectCollisions(destDir, sessionIDs)
		memoryCollision := false
		if includeMemory {
			if info, err := os.Stat(filepath.Join(destDir, "memory")); err == nil && info.IsDir() {
				memoryCollision = true
			}
		}
		return sessionCollisionsMsg{SessionCollisions: collisions, MemoryCollision: memoryCollision}
	}
}

// === Msg handlers ===

func (m Model) handleHistoryFoldersScanned(msg HistoryFoldersScannedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryScanFailed), msg.Err))
		return m, nil
	}
	m.historyFolders = msg.Folders
	if msg.ActivePIDsByFolder != nil {
		m.historyActivePIDsByFolder = msg.ActivePIDsByFolder
	}
	// +1 accounts for the "[+] Import bundle..." pseudo-row that the view prepends.
	if m.historyFolderCursor >= len(m.historyFolders)+1 {
		m.historyFolderCursor = 0
	}
	return m, nil
}

func (m Model) handleHistorySessionsScanned(msg HistorySessionsScannedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryScanSessionsFailed), msg.Err))
		return m, nil
	}
	m.historySessions = msg.Sessions
	m.historyActiveIDs = msg.ActiveSessionIDs
	m.historyDrilledFolder = msg.FolderName
	if m.historySelected == nil {
		m.historySelected = make(map[string]bool)
	} else {
		// Reset selection on new drill-in so stale IDs from the previous folder are cleared.
		for k := range m.historySelected {
			delete(m.historySelected, k)
		}
	}
	if m.historySessionCursor >= len(m.historySessions) {
		m.historySessionCursor = 0
	}
	return m, nil
}

func (m Model) handleExportCompleted(msg ExportCompletedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryExportFailure), msg.Err))
		return m, nil
	}
	m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryExportSuccess),
		len(msg.Sessions), msg.OutputPath))
	return m, nil
}

func (m Model) handleManifestLoaded(msg manifestLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryLoadManifestFailed), msg.Err))
		m.historyImportStep = 0 // back to path entry
		return m, nil
	}
	m.historyImportManifest = msg.Manifest
	m.historyImportBundlePath = msg.BundlePath
	if m.historyImportSelection == nil {
		m.historyImportSelection = make(map[string]bool)
	}
	// Default: all sessions in the bundle are checked for import.
	for _, id := range msg.Manifest.Sessions {
		m.historyImportSelection[id] = true
	}
	m.historyImportStep = 1 // advance to preview screen
	return m, nil
}

func (m Model) handleImportCompleted(msg ImportCompletedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryImportFailed), msg.Err))
		m.historyImportStep = 0
		return m, nil
	}
	m.historyImportResult = &sessionsync.UnpackResult{
		Written:      msg.Written,
		Skipped:      msg.Skipped,
		Cancelled:    msg.Cancelled,
		MemoryStatus: msg.MemoryStatus,
	}
	// Step 5 = summary modal (view renders tallies here).
	m.historyImportStep = 5
	// Re-scan folder list so destination folder's session count updates on return.
	return m, m.scanHistoryFoldersCmd()
}

func (m Model) handleSessionCollisions(msg sessionCollisionsMsg) (tea.Model, tea.Cmd) {
	m.historyCollisionQueue = msg.SessionCollisions
	m.historyCollisionMemory = msg.MemoryCollision
	if len(msg.SessionCollisions) == 0 && !msg.MemoryCollision {
		// No collisions — proceed directly to the import run.
		return m.startImportRun()
	}
	// Collision prompts exist; the view (T9) will render a modal for each entry.
	return m, nil
}

// === Helpers ===

// defaultExportOutputPath returns ~/.zpit/exports/<folderName>-<timestamp>.zip.
// The exports directory is created if it does not already exist.
func defaultExportOutputPath(folderName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".zpit", "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ts := time.Now().Format("20060102-150405")
	return filepath.Join(dir, fmt.Sprintf("%s-%s.zip", folderName, ts)), nil
}

// startImportRun is called when all collision decisions are resolved and the
// actual Unpack pass should begin. T10 wires the full implementation using the
// collision decision map collected from handleSessionCollisions + view prompts.
func (m Model) startImportRun() (tea.Model, tea.Cmd) {
	// T10 fills this in with importBundleCmd dispatch.
	return m, nil
}
