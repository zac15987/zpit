package tui

// view_sessions.go — History (Session Browser) view rendering.
//
// Lock protocol:
//   - All render functions are pure: they read Model fields only, no AppState.
//     The caller (Update via syncViewportContent) holds RLock during render.
//   - No mutations here.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/sessionsync"
)

// viewHistory is the top-level entry point for the History view.
// Picks folder-list vs session-list based on m.historyDrilledFolder.
func (m Model) viewHistory() string {
	header := m.renderHistoryHeader()
	footer := m.renderHistoryFooter()
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if contentHeight < 1 {
		contentHeight = 1
	}
	content := lipgloss.NewStyle().Width(m.width).Height(contentHeight).Render(m.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderHistoryHeader returns the fixed header for the History view.
// The subtitle switches between folder-list and session-list based on drill state.
func (m Model) renderHistoryHeader() string {
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")
	var title string
	if m.historyDrilledFolder != "" {
		title = fmt.Sprintf(locale.T(locale.KeyHistorySessionListTitle), m.historyDrilledFolder)
	} else {
		title = locale.T(locale.KeyHistoryFolderListTitle)
	}
	b.WriteString(sectionTitleStyle.Render(title))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 60) + "\n\n")
	return b.String()
}

// renderHistoryScrollable picks the correct body renderer based on drill state.
// Called by the viewport-sync logic in T10 to populate m.viewport content.
func (m Model) renderHistoryScrollable() string {
	if m.historyDrilledFolder == "" {
		return m.renderHistoryFolderList()
	}
	return m.renderHistorySessionList()
}

// renderHistoryFolderList renders the top-level folder list, with the
// "[+] Import bundle..." pseudo-row always at index 0.
func (m Model) renderHistoryFolderList() string {
	var b strings.Builder

	// Row 0 is the import pseudo-row — always present.
	b.WriteString(renderHistoryFolderRow(m.historyFolderCursor == 0,
		locale.T(locale.KeyHistoryImportRow), ""))

	if len(m.historyFolders) == 0 {
		b.WriteString(detailStyle.Render("  " + locale.T(locale.KeyHistoryEmptyFolders)))
		b.WriteString("\n")
		return b.String()
	}

	for i, f := range m.historyFolders {
		rowIdx := i + 1 // +1 because row 0 is the import pseudo-row
		focused := rowIdx == m.historyFolderCursor
		activeMarker := ""
		if historyFolderHasActive(m, f) {
			activeMarker = "🟢 "
		}
		size := formatBytes(f.TotalBytes)
		mtime := f.LastModified.Format("01/02 15:04")
		detail := fmt.Sprintf(locale.T(locale.KeyHistoryFolderRow), f.SessionCount, size, mtime)
		label := activeMarker + f.Name
		b.WriteString(renderHistoryFolderRowDetail(focused, label, detail))
	}
	return b.String()
}

// renderHistoryFolderRow renders the import pseudo-row (row 0) which has no
// detail sub-line.
func renderHistoryFolderRow(focused bool, label, sub string) string {
	cursor := "   "
	if focused {
		cursor = cursorMarker
	}
	name := label
	if focused {
		name = selectedStyle.Render(name)
	} else {
		name = normalStyle.Render(name)
	}
	var b strings.Builder
	b.WriteString(cursor + name + "\n")
	if sub != "" {
		b.WriteString("       " + detailStyle.Render(sub) + "\n")
	}
	return b.String()
}

// renderHistoryFolderRowDetail renders one real folder row with a detail
// sub-line showing session count, total size, and last-modified timestamp.
func renderHistoryFolderRowDetail(focused bool, line, detail string) string {
	cursor := "   "
	if focused {
		cursor = cursorMarker
	}
	name := line
	if focused {
		name = selectedStyle.Render(name)
	} else {
		name = normalStyle.Render(name)
	}
	var b strings.Builder
	b.WriteString(cursor + name + "\n")
	b.WriteString("       " + detailStyle.Render(detail) + "\n")
	return b.String()
}

// historyFolderHasActive returns true if the folder has at least one alive PID.
// The map is built once per [h] entry by T10 and refreshed after import.
// For folders the user has never drilled into, the map entry is absent (false).
func historyFolderHasActive(m Model, f sessionsync.FolderInfo) bool {
	return m.historyActivePIDsByFolder[f.Name]
}

// renderHistorySessionList renders the drilled-in session list for
// m.historyDrilledFolder, with multi-select checkboxes and active markers.
func (m Model) renderHistorySessionList() string {
	var b strings.Builder

	if len(m.historySessions) == 0 {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyHistoryEmptySessions)) + "\n")
		return b.String()
	}

	activeSet := make(map[string]bool, len(m.historyActiveIDs))
	for _, id := range m.historyActiveIDs {
		activeSet[id] = true
	}

	for i, s := range m.historySessions {
		focused := i == m.historySessionCursor
		checked := m.historySelected[s.SessionID]

		cursor := "   "
		if focused {
			cursor = cursorMarker
		}
		check := "[ ]"
		if checked {
			check = "[x]"
		}

		markers := ""
		if s.HasSubagents {
			markers += "🧩 "
		}
		if activeSet[s.SessionID] {
			markers += "🟢 "
		}

		size := formatBytes(s.Size)
		mtime := s.LastModified.Format("01/02 15:04")

		idText := s.SessionID
		if focused {
			idText = selectedStyle.Render(idText)
		} else {
			idText = normalStyle.Render(idText)
		}

		line := fmt.Sprintf("%s%s %s%s", cursor, check, markers, idText)
		sub := fmt.Sprintf("%s  %s", size, mtime)
		b.WriteString(line + "\n")
		b.WriteString("       " + detailStyle.Render(sub) + "\n")
	}

	selCount := 0
	for _, v := range m.historySelected {
		if v {
			selCount++
		}
	}
	b.WriteString("\n")
	b.WriteString("  " + detailStyle.Render(fmt.Sprintf(locale.T(locale.KeyHistorySelectedCount), selCount)))
	b.WriteString("\n")
	return b.String()
}

// renderHistoryFooter returns the key-hint footer for the History view.
// The hint line switches between folder-list and session-list contexts.
func (m Model) renderHistoryFooter() string {
	var b strings.Builder
	var help string
	if m.historyDrilledFolder == "" {
		help = locale.T(locale.KeyHistoryFooterFolderList)
	} else {
		help = locale.T(locale.KeyHistoryFooterSessionList)
	}
	b.WriteString(styledHotkeys(help))
	b.WriteString("\n\n")
	if m.statusMessage != "" && time.Now().Before(m.statusExpiry) {
		b.WriteString(statusBarStyle.Render(" " + m.statusMessage + " "))
	}
	return b.String()
}

// === Modal rendering ===

// renderHistoryModal returns the modal body to overlay, or "" when none active.
// T10 wraps this in confirmOverlayStyle and composites via overlay.Composite.
// Export modals take precedence over import modals; active-session warning is
// rendered before the export-confirm step.
func (m Model) renderHistoryModal() string {
	// Active-session warning takes precedence (export step 1).
	if m.historyExportStep == 1 && len(m.historyActivePending) > 0 {
		return renderHistoryActiveWarningModal(m.historyActivePending)
	}
	// Export confirm modal (step 2).
	if m.historyExportStep == 2 {
		return m.renderHistoryExportConfirmModal()
	}
	// Import preview (step 1): manifest loaded, awaiting user selection.
	if m.historyImportStep == 1 && m.historyImportManifest != nil {
		return m.renderHistoryImportPreviewModal()
	}
	// Import destination entry (step 2).
	if m.historyImportStep == 2 {
		return m.renderHistoryImportDestModal()
	}
	// Import final preview (step 3): shows resolved destination + session count.
	if m.historyImportStep == 3 {
		return m.renderHistoryImportFinalModal()
	}
	// Import running (step 4): importBundleCmd is in-flight.
	if m.historyImportStep == 4 {
		return "  Importing..."
	}
	// Import summary (step 5): tallies from UnpackResult.
	if m.historyImportStep == 5 && m.historyImportResult != nil {
		return m.renderHistoryImportSummaryModal()
	}
	// Collision modal — session collisions processed front-to-back.
	if len(m.historyCollisionQueue) > 0 {
		return m.renderHistoryCollisionModal(m.historyCollisionQueue[0], false)
	}
	// Memory-directory collision.
	if m.historyCollisionMemory {
		return m.renderHistoryCollisionModal("", true)
	}
	return ""
}

// renderHistoryActiveWarningModal warns the user that one or more sessions in
// the current export selection are attached to alive Claude Code processes.
func renderHistoryActiveWarningModal(activeIDs []string) string {
	title := selectedStyle.Render(locale.T(locale.KeyHistoryActiveWarningTitle))
	body := fmt.Sprintf(locale.T(locale.KeyHistoryActiveWarningBody), strings.Join(activeIDs, ", "))
	btns := fmt.Sprintf("  [%s]  [%s]",
		hotkeyLabelStyle.Render(locale.T(locale.KeyHistoryActiveWarningContinue)),
		hotkeyLabelStyle.Render(locale.T(locale.KeyCancel)))
	return strings.Join([]string{title, "", body, "", btns}, "\n")
}

// renderHistoryExportConfirmModal shows the export confirmation step with
// session count, estimated total size, memory checkbox, and output path.
func (m Model) renderHistoryExportConfirmModal() string {
	title := selectedStyle.Render(locale.T(locale.KeyHistoryExportConfirmTitle))

	selCount := 0
	var totalBytes int64
	for _, s := range m.historySessions {
		if m.historySelected[s.SessionID] {
			selCount++
			totalBytes += s.Size
		}
	}
	body := fmt.Sprintf(locale.T(locale.KeyHistoryExportConfirmBody), selCount, formatBytes(totalBytes))

	chk := "[ ]"
	if m.historyExportIncludeMemory {
		chk = "[x]"
	}
	memLine := chk + " " + locale.T(locale.KeyHistoryExportIncludeMemory)

	// Render the output path as a textinput so the user can edit it (AC-4).
	// The path field is the historyExportPathInput textinput; when not focused
	// (default), the checkbox is the active control. Tab toggles focus.
	pathLine := locale.T(locale.KeyHistoryExportPathLabel) + " " + m.historyExportPathInput.View()

	btns := fmt.Sprintf("  [%s]  [%s]",
		hotkeyLabelStyle.Render(locale.T(locale.KeyHistoryExportButton)),
		hotkeyLabelStyle.Render(locale.T(locale.KeyCancel)))
	return strings.Join([]string{title, "", body, "", memLine, "", pathLine, "", btns}, "\n")
}

// renderHistoryImportPreviewModal shows the bundle manifest summary with a
// per-session multi-select checklist so the user can deselect individual sessions.
func (m Model) renderHistoryImportPreviewModal() string {
	title := selectedStyle.Render(locale.T(locale.KeyHistoryImportPreviewTitle))
	mf := m.historyImportManifest
	memText := locale.T(locale.KeyHistoryNo)
	if mf.IncludeMemory {
		memText = locale.T(locale.KeyHistoryYes)
	}
	body := fmt.Sprintf(locale.T(locale.KeyHistoryImportPreviewBody),
		mf.SourceCwd, mf.SourceOS, len(mf.Sessions), memText)

	var rows strings.Builder
	for i, id := range mf.Sessions {
		chk := "[ ]"
		if m.historyImportSelection[id] {
			chk = "[x]"
		}
		cursor := "   "
		if m.historyImportSessionCursor == i {
			cursor = cursorMarker
		}
		rows.WriteString(fmt.Sprintf("%s%s %s\n", cursor, chk, id))
	}
	return strings.Join([]string{title, "", body, "", rows.String()}, "\n")
}

// renderHistoryImportDestModal prompts the user to enter the absolute path of
// the destination project so the importer can derive the encoded folder name.
func (m Model) renderHistoryImportDestModal() string {
	title := selectedStyle.Render(locale.T(locale.KeyHistoryImportDestLabel))
	return strings.Join([]string{title, "", "  " + m.historyImportDestPath}, "\n")
}

// renderHistoryImportFinalModal shows the resolved destination encoded path,
// the number of sessions to import, and the memory include state before
// committing to the import run.
func (m Model) renderHistoryImportFinalModal() string {
	title := selectedStyle.Render(locale.T(locale.KeyHistoryImportPreviewTitle))
	mf := m.historyImportManifest
	selCount := 0
	for _, id := range mf.Sessions {
		if m.historyImportSelection[id] {
			selCount++
		}
	}
	memText := locale.T(locale.KeyHistoryNo)
	if mf.IncludeMemory {
		memText = locale.T(locale.KeyHistoryYes)
	}
	encoded := encodeCwdForPreview(m.historyImportDestPath)
	body := fmt.Sprintf(locale.T(locale.KeyHistoryImportFinalPreview), encoded, selCount, memText)

	btns := fmt.Sprintf("  [%s]  [%s]",
		hotkeyLabelStyle.Render(locale.T(locale.KeyHistoryImportButton)),
		hotkeyLabelStyle.Render(locale.T(locale.KeyCancel)))
	return strings.Join([]string{title, "", body, "", btns}, "\n")
}

// renderHistoryImportSummaryModal shows the final written/skipped/cancelled
// tallies from the completed import operation.
func (m Model) renderHistoryImportSummaryModal() string {
	r := m.historyImportResult
	return fmt.Sprintf(locale.T(locale.KeyHistoryImportSummary),
		r.Written, r.Skipped, r.Cancelled, r.MemoryStatus)
}

// renderHistoryCollisionModal renders a 3-button modal for either a session
// collision (isMemory=false) or a memory-directory collision (isMemory=true).
func (m Model) renderHistoryCollisionModal(sessionID string, isMemory bool) string {
	var title, body string
	if isMemory {
		title = selectedStyle.Render(locale.T(locale.KeyHistoryCollisionMemoryTitle))
		body = locale.T(locale.KeyHistoryCollisionMemoryBody)
	} else {
		title = selectedStyle.Render(locale.T(locale.KeyHistoryCollisionTitle))
		body = fmt.Sprintf(locale.T(locale.KeyHistoryCollisionBody), sessionID)
	}
	btns := fmt.Sprintf("  [%s]  [%s]  [%s]",
		hotkeyLabelStyle.Render(locale.T(locale.KeyHistoryCollisionOverwrite)),
		hotkeyLabelStyle.Render(locale.T(locale.KeyHistoryCollisionSkip)),
		hotkeyLabelStyle.Render(locale.T(locale.KeyHistoryCollisionCancelAll)))
	return strings.Join([]string{title, "", body, "", btns}, "\n")
}

// === Helpers ===

// encodeCwdForPreview converts an absolute project path to the encoded form
// used by Claude Code for its ~/.claude/projects/ directory names: every
// non-alphanumeric rune is replaced with a hyphen. This mirrors the logic in
// internal/watcher.EncodeCwd but is kept local to avoid a cross-package import
// in a pure rendering file.
func encodeCwdForPreview(absPath string) string {
	out := make([]byte, 0, len(absPath))
	for _, r := range absPath {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, byte(r))
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

// formatBytes returns a human-readable byte count, e.g. "12.4 KB", "1.2 MB".
// Uses binary (1024-based) units matching the convention for file sizes.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for k := n / unit; k >= unit; k /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
