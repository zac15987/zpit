package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/watcher"
)

// UI symbols.
// Profile icons use Nerd Font glyphs (requires a patched font like CascadiaCode NF);
// all other icons stay as emoji so the TUI degrades gracefully without Nerd Fonts.
const (
	iconMachine  = "\uf275 " // nf-fa-industry
	iconWeb      = "\uf0ac " // nf-fa-globe
	iconDesktop  = "\uf108 " // nf-fa-desktop
	iconAndroid  = "\uf17b " // nf-fa-android
	iconTerminal = "\uf120 " // nf-fa-terminal
	iconWorking    = "🟢"
	iconWaiting    = "🟡"
	iconPermission = "🟠"
	iconEnded      = "⚫"
	iconDeployFull    = "🟢"
	iconDeployPartial = "🟡"
	iconDeployNone    = "⚪"
	cursorMarker = " › "
	boxHoriz     = "─"
	boxVert      = "│"

	// Dock layout constants.
	focusBarChar        = "▎"
	panelRuleChar       = "─"
	dockMinRightWidth   = 22   // hotkeys column minimum width — keeps it docked right even on narrow terminals
	dockMinLeftWidth    = 18   // left column minimum before we surrender the split
	dockMinPanelHeight  = 4    // title + rule + ≥2 body rows
	dockLeftColumnRatio = 0.70 // ideal left/right split before min-width clamps
	panelBodyPrefixLen  = 2    // "▎ " focus bar OR "  " blank, rendered as chrome prefix
	panelRuleWidth      = 6    // width of the short rule under each panel title
	loopIssueTitleMaxLen = 35  // per-slot title truncation in the Loop panel

	// Panel height weights (relative shares when all three left-column panels are present).
	panelWeightProjects  = 3
	panelWeightTerminals = 2
	panelWeightLoop      = 2
)

// panelRect describes the placement of a single dock panel.
type panelRect struct {
	x, y, w, h int
}

// dockRects describes the layout of all four dock panels for a given terminal size.
type dockRects struct {
	projects  panelRect
	terminals panelRect
	loop      panelRect
	hotkeys   panelRect
}

// dockPanel bundles everything renderPanelChrome and the renderOne composer need
// for one panel. Packing the fields avoids a 7-parameter signature at the call site.
type dockPanel struct {
	rect      panelRect
	vp        viewport.Model
	title     string
	count     int
	focused   bool
	focusable bool
	stacked   bool
}

// panelInnerSize returns the content-area width/height carved out of a panel
// rect (after subtracting chrome rows and the body's leading prefix). ok=false
// signals an empty rect — the caller should skip rendering.
func panelInnerSize(r panelRect, stacked bool) (innerW, innerH int, ok bool) {
	if r.w == 0 || r.h == 0 {
		return 0, 0, false
	}
	innerW = r.w - panelBodyPrefixLen
	if innerW < 1 {
		innerW = 1
	}
	innerH = r.h - panelChromeRows(stacked)
	if innerH < 1 {
		innerH = 1
	}
	return innerW, innerH, true
}

// DeployStatus indicates whether Zpit-managed files are fully, partially, or not present in a project.
type DeployStatus int

const (
	DeployNone DeployStatus = iota
	DeployPartial
	DeployFull
)

// deployedFiles lists every file that a full `[d]` redeploy writes. deployStatus
// counts how many of these are present on disk and classifies into None/Partial/Full.
// Keep in sync with deployAllCmd (launch.go).
var deployedFiles = []string{
	".claude/agents/clarifier.md",
	".claude/agents/reviewer.md",
	".claude/agents/task-runner.md",
	".claude/agents/efficiency.md",
	".claude/docs/agent-guidelines.md",
	".claude/docs/code-construction-principles.md",
	".claude/hooks/path-guard.sh",
	".claude/hooks/bash-firewall.sh",
	".claude/hooks/git-guard.sh",
	".claude/hooks/notify-permission.sh",
}

// deployStatus checks the project's filesystem and returns DeployFull (all files
// present), DeployNone (no files present), or DeployPartial (mixed).
// Stateless filesystem call; safe to run every render.
func deployStatus(projectPath string) DeployStatus {
	if projectPath == "" {
		return DeployNone
	}
	found := 0
	for _, rel := range deployedFiles {
		if _, err := os.Stat(filepath.Join(projectPath, rel)); err == nil {
			found++
		}
	}
	switch {
	case found == 0:
		return DeployNone
	case found == len(deployedFiles):
		return DeployFull
	default:
		return DeployPartial
	}
}

// renderDeployTag returns the styled status tag rendered next to the project name.
func renderDeployTag(s DeployStatus) string {
	switch s {
	case DeployFull:
		return workingStyle.Render(iconDeployFull + " " + locale.T(locale.KeyDeployStatusFull))
	case DeployPartial:
		return waitingStyle.Render(iconDeployPartial + " " + locale.T(locale.KeyDeployStatusPartial))
	default:
		return detailStyle.Render(iconDeployNone + " " + locale.T(locale.KeyDeployStatusNone))
	}
}

var profileIcons = map[string]string{
	"machine":  iconMachine,
	"web":      iconWeb,
	"desktop":  iconDesktop,
	"android":  iconAndroid,
	"terminal": iconTerminal,
}

func (m Model) viewProjects() string {
	header := m.renderProjectsHeader()
	footer := m.renderProjectsFooter()
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if contentHeight < 1 {
		contentHeight = 1
	}
	body := m.composeDockLayout(m.width, contentHeight)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// renderProjectsHeader returns the fixed header above the dock area.
func (m Model) renderProjectsHeader() string {
	return m.renderHeader() + "\n"
}

// computePanelRects calculates the layout rectangles for all four dock panels.
// Hotkeys always dock to the right column — even on narrow terminals — mirroring
// lazygit's behavior. Caller must hold m.state RLock if accessing state.
func (m Model) computePanelRects(width, contentHeight int) dockRects {
	nTerms := len(m.state.activeTerminals)
	hasLoop := false
	for _, ls := range m.state.loops {
		if ls.Active || len(ls.Slots) > 0 {
			hasLoop = true
			break
		}
	}

	// Column split: dockLeftColumnRatio ideal; clamp hotkeys to min width and keep left readable.
	leftW := int(float64(width) * dockLeftColumnRatio)
	rightW := width - leftW
	if rightW < dockMinRightWidth {
		rightW = dockMinRightWidth
		leftW = width - rightW
	}
	// If the terminal is narrower than both minimums combined, shrink hotkeys
	// so the left column still has at least dockMinLeftWidth to render.
	if leftW < dockMinLeftWidth && width > dockMinLeftWidth {
		leftW = dockMinLeftWidth
		rightW = width - leftW
		if rightW < 1 {
			rightW = 0
		}
	}
	if leftW < 0 {
		leftW = 0
	}

	wP := panelWeightProjects
	wT, wL := 0, 0
	if nTerms > 0 {
		wT = panelWeightTerminals
	}
	if hasLoop {
		wL = panelWeightLoop
	}
	total := wP + wT + wL
	if total == 0 {
		total = 1
	}

	pH := contentHeight * wP / total
	tH := contentHeight * wT / total
	lH := contentHeight - pH - tH
	if wL == 0 {
		lH = 0
	}
	if wT == 0 {
		tH = 0
	}

	// Clamp minimums on present panels; projects absorbs slack.
	if tH > 0 && tH < dockMinPanelHeight {
		tH = dockMinPanelHeight
	}
	if lH > 0 && lH < dockMinPanelHeight {
		lH = dockMinPanelHeight
	}
	pH = contentHeight - tH - lH
	if pH < dockMinPanelHeight {
		pH = dockMinPanelHeight
	}

	return dockRects{
		projects:  panelRect{0, 0, leftW, pH},
		terminals: panelRect{0, pH, leftW, tH},
		loop:      panelRect{0, pH + tH, leftW, lH},
		hotkeys:   panelRect{leftW, 0, rightW, contentHeight},
	}
}

// layoutDockPanels computes rects and syncs all four panel viewports.
// Must be called with m.state RLock held (reads activeTerminals, loops).
// Panels stacked below the first in a column get a 1-row top gutter for breathing room.
func (m *Model) layoutDockPanels(width, contentHeight int) {
	r := m.computePanelRects(width, contentHeight)
	m.lastDockRects = r

	// In the left column, projects is always first (no gutter). Terminals sits
	// below projects only when it has a non-zero rect; likewise loop below either.
	m.syncProjectsPanel(r.projects, false)
	m.syncTerminalsPanel(r.terminals, r.terminals.h > 0)
	m.syncLoopPanel(r.loop, r.loop.h > 0)
	// Hotkeys is the only panel in the right column — no stack above it.
	m.syncHotkeysPanel(r.hotkeys, false)
}

// panelChromeRows returns the number of rows consumed by panel chrome.
// Stacked (non-first) panels get a leading blank row as a gutter separator.
func panelChromeRows(stacked bool) int {
	if stacked {
		return 3 // blank + title + rule
	}
	return 2 // title + rule
}

// composeDockLayout renders the dock by combining each panel's chrome + viewport view.
// Reads m.lastDockRects (populated by layoutDockPanels before this is called).
func (m Model) composeDockLayout(width, contentHeight int) string {
	r := m.lastDockRects
	renderOne := func(p dockPanel) string {
		if p.rect.w == 0 || p.rect.h == 0 {
			return ""
		}
		chrome := m.renderPanelChrome(p)
		return lipgloss.NewStyle().Width(p.rect.w).Height(p.rect.h).Render(
			lipgloss.JoinVertical(lipgloss.Left, chrome, p.vp.View()),
		)
	}
	pStr := renderOne(dockPanel{
		rect: r.projects, vp: m.projectsVP,
		title: locale.T(locale.KeyProjects), count: len(m.state.projects),
		focused: m.focusedPanel == FocusProjects, focusable: true,
	})
	tStr := renderOne(dockPanel{
		rect: r.terminals, vp: m.terminalsVP,
		title: locale.T(locale.KeyActiveTerminals), count: len(m.state.activeTerminals),
		focused: m.focusedPanel == FocusTerminals, focusable: true,
		stacked: r.terminals.h > 0,
	})
	lStr := renderOne(dockPanel{
		rect: r.loop, vp: m.loopVP,
		title: locale.T(locale.KeyLoopStatus), count: m.totalLoopSlots(),
		focused: m.focusedPanel == FocusLoopSlots, focusable: true,
		stacked: r.loop.h > 0,
	})
	hStr := renderOne(dockPanel{
		rect: r.hotkeys, vp: m.hotkeysVP,
		title: locale.T(locale.KeyHotkeys),
	})

	// Filter empty panels before JoinVertical — lipgloss treats "" as a blank
	// row and would otherwise inflate the left column beyond contentHeight,
	// pushing the header off the top of the alt screen.
	leftParts := make([]string, 0, 3)
	for _, s := range []string{pStr, tStr, lStr} {
		if s != "" {
			leftParts = append(leftParts, s)
		}
	}
	left := lipgloss.JoinVertical(lipgloss.Left, leftParts...)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, hStr)
}

// renderPanelChrome returns the title + rule rows that sit above each panel body.
// When p.stacked is true, prepends a blank gutter row (used for panels below the
// first in a column). When p.focusable is false (Hotkeys), the focus bar column
// stays blank regardless of focus state.
func (m Model) renderPanelChrome(p dockPanel) string {
	bar := "  "
	if p.focusable && p.focused {
		bar = focusBarStyle.Render(focusBarChar) + " "
	}
	titleStyle := panelTitleBlurredStyle
	if p.focused {
		titleStyle = panelTitleFocusedStyle
	}
	countStr := ""
	if p.count > 0 {
		countStr = "  " + panelCountStyle.Render(fmt.Sprintf("%d", p.count))
	}
	head := bar + titleStyle.Render(strings.ToUpper(p.title)) + countStr
	ruleLen := panelRuleWidth
	if p.rect.w-panelBodyPrefixLen < ruleLen {
		ruleLen = p.rect.w - panelBodyPrefixLen
	}
	if ruleLen < 1 {
		ruleLen = 1
	}
	rule := "  " + panelRuleStyle.Render(strings.Repeat(panelRuleChar, ruleLen))
	if p.stacked {
		return lipgloss.JoinVertical(lipgloss.Left, "", head, rule)
	}
	return lipgloss.JoinVertical(lipgloss.Left, head, rule)
}

// totalLoopSlots returns the count of slots across all active loops (for the panel count badge).
func (m Model) totalLoopSlots() int {
	total := 0
	for _, ls := range m.state.loops {
		total += len(ls.Slots)
	}
	return total
}

// applyPanelContent pushes body + dimensions into a panel viewport and re-clamps
// YOffset after the content change. Shared epilogue across all four sync* funcs.
func applyPanelContent(vp *viewport.Model, rectW, innerH int, body string) {
	vp.Width = rectW
	vp.Height = innerH
	vp.SetContent(body)
	vp.SetYOffset(vp.YOffset) // force clamp after content change
}

// syncProjectsPanel re-renders the Projects panel body into projectsVP and keeps
// the cursor row visible.
func (m *Model) syncProjectsPanel(r panelRect, stacked bool) {
	innerW, innerH, ok := panelInnerSize(r, stacked)
	if !ok {
		return
	}
	applyPanelContent(&m.projectsVP, r.w, innerH, m.renderProjectListBody(innerW))
	m.ensureCursorInPanel(&m.projectsVP, m.cursor*3)
}

// syncTerminalsPanel re-renders the Active Terminals panel body into terminalsVP and
// refreshes m.termLineStarts for variable-stride cursor follow.
func (m *Model) syncTerminalsPanel(r panelRect, stacked bool) {
	innerW, innerH, ok := panelInnerSize(r, stacked)
	if !ok {
		m.termLineStarts = nil
		return
	}
	body, starts := m.renderTerminalsBody(innerW)
	m.termLineStarts = starts
	applyPanelContent(&m.terminalsVP, r.w, innerH, body)
	if m.focusedPanel == FocusTerminals && m.termCursor >= 0 && m.termCursor < len(starts) {
		m.ensureCursorInPanel(&m.terminalsVP, starts[m.termCursor])
	}
}

// syncLoopPanel re-renders the Loop Engine panel body into loopVP and refreshes
// m.loopLineStarts for the currently focused project's slots.
func (m *Model) syncLoopPanel(r panelRect, stacked bool) {
	innerW, innerH, ok := panelInnerSize(r, stacked)
	if !ok {
		m.loopLineStarts = nil
		return
	}
	body, starts := m.renderLoopBody(innerW)
	m.loopLineStarts = starts
	applyPanelContent(&m.loopVP, r.w, innerH, body)
	if m.focusedPanel == FocusLoopSlots && m.loopCursor >= 0 && m.loopCursor < len(starts) {
		m.ensureCursorInPanel(&m.loopVP, starts[m.loopCursor])
	}
}

// syncHotkeysPanel re-renders the Hotkeys reference panel into hotkeysVP.
func (m *Model) syncHotkeysPanel(r panelRect, stacked bool) {
	innerW, innerH, ok := panelInnerSize(r, stacked)
	if !ok {
		return
	}
	applyPanelContent(&m.hotkeysVP, r.w, innerH, m.renderHotkeysBody(innerW, innerH))
}

// renderProjectsFooter returns the fixed footer below the scrollable area.
func (m Model) renderProjectsFooter() string {
	var b strings.Builder
	if m.statusMessage != "" && time.Now().Before(m.statusExpiry) {
		b.WriteString(statusBarStyle.Render(" " + m.statusMessage + " "))
	}
	b.WriteString("\n")
	switch m.focusedPanel {
	case FocusTerminals:
		b.WriteString(helpStyle.Render(locale.T(locale.KeyTerminalHelp)))
	case FocusLoopSlots:
		b.WriteString(helpStyle.Render(locale.T(locale.KeyLoopSlotHelp)))
	default:
		b.WriteString(helpStyle.Render(locale.T(locale.KeyHelpFooter)))
	}
	return b.String()
}

func (m Model) renderHeader() string {
	now := time.Now().Format("01/02 15:04")
	env := m.state.env.String()
	left := "Zpit v0.1"
	right := fmt.Sprintf("%s  %s", now, env)
	// headerBoxStyle has Padding(0,1) — inner width is m.width - 2
	inner := m.width - 2
	gap := inner - len(left) - len(right)
	if gap < 2 {
		gap = 2
	}
	title := left + strings.Repeat(" ", gap) + right
	return headerBoxStyle.Width(m.width).Render(title)
}

// renderProjectListBody emits the project rows for the Projects panel.
// Each project takes 3 lines (name, detail, blank), matching the stride used by
// ensureCursorInPanel(&m.projectsVP, m.cursor*3).
func (m Model) renderProjectListBody(innerWidth int) string {
	var b strings.Builder

	for i, p := range m.state.projects {
		icon := profileIcons[p.Profile]
		if icon == "" {
			icon = "  "
		}

		// 3-cell prefix: two spaces + selection marker. The focus bar (`▎`) lives
		// on the panel chrome, not on every body row.
		selMarker := " "
		if i == m.cursor {
			selMarker = "›"
		}
		prefix := "  " + selMarker

		name := p.Name
		if i == m.cursor {
			name = selectedStyle.Render(name)
		} else {
			name = normalStyle.Render(name)
		}

		tags := ""
		if len(p.Tags) > 0 {
			tags = tagStyle.Render(strings.Join(p.Tags, ", "))
		}

		deployTag := renderDeployTag(deployStatus(platform.ResolvePath(p.Path.Windows, p.Path.WSL)))

		b.WriteString(fmt.Sprintf("%s%s%s  %s\n", prefix, icon, name, deployTag))
		b.WriteString(fmt.Sprintf("     %s",
			detailStyle.Render(p.Profile)))
		if tags != "" {
			b.WriteString(fmt.Sprintf(" %s %s", boxVert, tags))
		}
		b.WriteString("\n\n")
	}

	return b.String()
}

// renderHotkeysBody emits the hotkey reference rows.
// Auto-shrink: when innerHeight is tight, drops the blank-row separators; if still
// overflowing, truncates the tail and shows a `…` marker.
func (m Model) renderHotkeysBody(innerWidth, innerHeight int) string {
	hotkeys := []struct {
		key  string
		desc string
		sep  bool // preferred blank line before this entry
	}{
		{"Enter", locale.T(locale.KeyLaunchClaude), false},
		{"c", locale.T(locale.KeyClarifyReq), false},
		{"l", locale.T(locale.KeyLoopAutoImpl), false},
		{"r", locale.T(locale.KeyReviewChanges), false},
		{"f", locale.T(locale.KeyEfficiencyAgent), false},
		{"s", locale.T(locale.KeyStatusOverview), false},
		{"o", locale.T(locale.KeyOpenFolder), false},
		{"p", locale.T(locale.KeyOpenTracker), false},
		{"u", locale.T(locale.KeyUndeploy), false},
		{"d", locale.T(locale.KeyRedeploy), false},
		{"m", locale.T(locale.KeyChannelComm), false},
		{"g", locale.T(locale.KeyGitStatusHotkeyLabel), false},
		{"a", locale.T(locale.KeyAddProject), true},
		{"e", locale.T(locale.KeyEditConfig), false},
		{"x", locale.T(locale.KeyCloseTerminal), true},
		{"Tab", locale.T(locale.KeySwitchPanel), false},
		{"?", locale.T(locale.KeyHelp), false},
		{"q", locale.T(locale.KeyQuit), false},
	}

	// Decide whether blank separator rows fit.
	sepCount := 0
	for _, h := range hotkeys {
		if h.sep {
			sepCount++
		}
	}
	keepSeps := innerHeight <= 0 || len(hotkeys)+sepCount <= innerHeight

	// Build rows.
	type row struct {
		blank bool
		text  string
	}
	rows := make([]row, 0, len(hotkeys)+sepCount)
	for _, h := range hotkeys {
		if h.sep && keepSeps {
			rows = append(rows, row{blank: true})
		}
		k := hotkeyLabelStyle.Render(fmt.Sprintf("[%s]", h.key))
		d := hotkeyDescStyle.Render(h.desc)
		rows = append(rows, row{text: fmt.Sprintf("  %s %s", k, d)})
	}

	// Truncate to innerHeight, appending `…` on overflow.
	if innerHeight > 0 && len(rows) > innerHeight {
		rows = rows[:innerHeight-1]
		rows = append(rows, row{text: detailStyle.Render("  …")})
	}

	var b strings.Builder
	for _, r := range rows {
		if r.blank {
			b.WriteString("\n")
			continue
		}
		b.WriteString(r.text)
		b.WriteString("\n")
	}
	return b.String()
}

// baseProjectID extracts the original project ID from a tracking key.
// Handles formats: "projectID", "projectID#N", "focus:projectID:issueID",
// and "focus:projectID:issueID#N".
func baseProjectID(trackingKey string) string {
	key := trackingKey
	// Strip "#N" multi-session suffix first.
	if idx := strings.Index(key, "#"); idx != -1 {
		key = key[:idx]
	}
	// Handle "focus:projectID:issueID" format.
	if strings.HasPrefix(key, "focus:") {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return key
}

func (m Model) projectName(id string) string {
	lookupID := baseProjectID(id)
	for _, p := range m.state.projects {
		if p.ID == lookupID {
			return p.Name
		}
	}
	return id
}

// renderTerminalsBody emits the active-terminal rows. Returns the rendered body plus
// a cumulative line-start slice (one entry per terminal) so ensureCursorInPanel can
// locate the line for m.termCursor given variable stride.
func (m Model) renderTerminalsBody(innerWidth int) (string, []int) {
	var b strings.Builder

	// Sort keys for stable render order.
	// Caller (syncViewportContent via layoutDockPanels) already holds RLock.
	termKeys := m.sortedTerminalKeysLocked()
	lineStarts := make([]int, 0, len(termKeys))
	line := 0

	for i, projectID := range termKeys {
		lineStarts = append(lineStarts, line)

		at := m.state.activeTerminals[projectID]
		statusIcon, statusText := renderAgentStatus(at)
		elapsed := formatElapsed(time.Since(at.StateChangedAt))

		displayName := m.projectName(projectID)
		if at.WorktreeBranch != "" {
			displayName += " 🌿" + at.WorktreeBranch
		}

		selMarker := " "
		if i == m.termCursor {
			selMarker = "›"
		}
		prefix := "  " + selMarker

		b.WriteString(fmt.Sprintf("%s[%d] %s %s %s %s\n",
			prefix,
			i+1,
			selectedStyle.Render(displayName),
			boxVert,
			statusText,
			detailStyle.Render(elapsed),
		))
		line++

		// Context preview (question or permission message).
		if pfx, text := agentContextPreview(at, statusIcon); text != "" {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				detailStyle.Render(pfx),
				questionStyle.Render(text),
			))
			line++
		}

		// Switch hint.
		if at.LaunchResult != nil && at.LaunchResult.SwitchHint != "" {
			b.WriteString(fmt.Sprintf("      %s\n",
				detailStyle.Render(at.LaunchResult.SwitchHint),
			))
			line++
		}

		// Channel event counts (own + listen projects) for this project.
		pid := baseProjectID(projectID)
		var allEvents []broker.Event
		allEvents = append(allEvents, m.state.channelEvents[pid]...)
		if proj := m.findProject(pid); proj != nil {
			for _, lk := range proj.ChannelListen {
				allEvents = append(allEvents, m.state.channelEvents[lk]...)
			}
		}
		if len(allEvents) > 0 {
			artCount, msgCount := countAllChannelEvents(allEvents)
			if artCount > 0 || msgCount > 0 {
				b.WriteString(fmt.Sprintf("      📦 %d artifacts  💬 %d messages\n", artCount, msgCount))
				line++
			}
		}
	}

	return b.String(), lineStarts
}

func renderAgentStatus(at *ActiveTerminal) (string, string) {
	switch at.State {
	case watcher.StateEnded:
		return iconEnded, detailStyle.Render(iconEnded + " " + locale.T(locale.KeySessionEnded))
	case watcher.StatePermission:
		return iconPermission, waitingStyle.Render(iconPermission + " " + locale.T(locale.KeyPermissionWait))
	case watcher.StateWaiting:
		return iconWaiting, waitingStyle.Render(iconWaiting + " " + locale.T(locale.KeyWaitingForInput))
	case watcher.StateWorking:
		return iconWorking, workingStyle.Render(iconWorking + " " + locale.T(locale.KeyWorking))
	case watcher.StateStreaming:
		return iconWorking, workingStyle.Render(iconWorking + " " + locale.T(locale.KeyWorking))
	default:
		return iconWorking, detailStyle.Render(iconWorking + " " + locale.T(locale.KeyLaunched))
	}
}

func formatElapsed(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

// renderLoopBody emits the loop-engine rows (all active loops, stable project order).
// Returns body plus a cumulative line-start slice indexed by slot ordinal within the
// currently focused project (m.focusProjectID). Slots outside that project map to -1.
func (m Model) renderLoopBody(innerWidth int) (string, []int) {
	focusedPanel := m.focusedPanel == FocusLoopSlots
	var b strings.Builder

	projectIDs := make([]string, 0, len(m.state.loops))
	for pid := range m.state.loops {
		projectIDs = append(projectIDs, pid)
	}
	sort.Strings(projectIDs)

	// Line starts for slots in the focused project only (loopCursor indexes this).
	var lineStarts []int
	line := 0

	for _, projectID := range projectIDs {
		ls := m.state.loops[projectID]
		if !ls.Active && len(ls.Slots) == 0 {
			continue
		}
		isFocusedProject := focusedPanel && projectID == m.focusProjectID

		// Per-project subheading row (1 line, counts toward stride).
		projectName := m.projectName(projectID)
		status := locale.T(locale.KeyLoopRunning)
		if !ls.Active {
			status = locale.T(locale.KeyLoopStopping)
		}
		b.WriteString(fmt.Sprintf("  %s (%s)\n",
			selectedStyle.Render(projectName),
			detailStyle.Render(status),
		))
		line++

		if len(ls.Slots) == 0 {
			b.WriteString(fmt.Sprintf("    %s\n", detailStyle.Render(locale.T(locale.KeyPollingIssues))))
			line++
			continue
		}

		slotKeys := m.sortedSlotKeys(projectID)

		for idx, key := range slotKeys {
			if isFocusedProject {
				lineStarts = append(lineStarts, line)
			}

			slot := ls.Slots[key]
			icon := iconWorking
			switch slot.State {
			case loop.SlotError:
				icon = "🔴"
			case loop.SlotNeedsHuman:
				icon = "🟠"
			case loop.SlotWaitingPRMerge:
				icon = iconWaiting
			case loop.SlotDone:
				icon = "✅"
			}
			stateText := slot.State.String()
			if slot.ReviewRound > 0 {
				stateText += fmt.Sprintf(" (round %d/%d)", slot.ReviewRound, m.state.cfg.Worktree.MaxReviewRounds)
			}

			selMarker := " "
			if isFocusedProject && idx == m.loopCursor {
				selMarker = "›"
			}
			prefix := "  " + selMarker

			titleText := truncate(slot.IssueTitle, loopIssueTitleMaxLen)
			if isFocusedProject && idx == m.loopCursor {
				titleText = selectedStyle.Render(titleText)
			}

			b.WriteString(fmt.Sprintf("%s%s #%s %s  %s\n",
				prefix, icon, slot.IssueID,
				titleText,
				detailStyle.Render(stateText),
			))
			line++

			if slot.Error != nil {
				b.WriteString(fmt.Sprintf("      %s\n",
					detailStyle.Render(slot.Error.Error()),
				))
				line++
			}

			artCount, msgCount := countChannelEvents(m.state.channelEvents[projectID], slot.IssueID)
			if artCount > 0 || msgCount > 0 {
				b.WriteString(fmt.Sprintf("      📦 %d artifacts  💬 %d messages\n", artCount, msgCount))
				line++
			}
		}
	}

	return b.String(), lineStarts
}

// countChannelEvents counts artifact and message events matching the given issueID.
// For artifacts, matches on IssueID. For messages, matches on From or To.
func countChannelEvents(events []broker.Event, issueID string) (artifacts, messages int) {
	for _, ev := range events {
		switch ev.Type {
		case "artifact":
			var art broker.Artifact
			if err := json.Unmarshal(ev.Payload, &art); err == nil && art.IssueID == issueID {
				artifacts++
			}
		case "message":
			var msg broker.Message
			if err := json.Unmarshal(ev.Payload, &msg); err == nil && (msg.From == issueID || msg.To == issueID) {
				messages++
			}
		}
	}
	return
}

// countAllChannelEvents counts all artifact and message events regardless of issue ID.
// Used by Active Terminals which have no per-issue context.
func countAllChannelEvents(events []broker.Event) (artifacts, messages int) {
	for _, ev := range events {
		switch ev.Type {
		case "artifact":
			artifacts++
		case "message":
			messages++
		}
	}
	return
}

// agentContextPreview returns a prefix and truncated one-line preview for the
// active terminal's current context (question or permission message).
func agentContextPreview(at *ActiveTerminal, statusIcon string) (string, string) {
	var prefix, raw string
	switch {
	case at.LastQuestion != "" && statusIcon == iconWaiting:
		prefix, raw = "Q:", at.LastQuestion
	case at.PermissionMessage != "" && statusIcon == iconPermission:
		prefix, raw = "P:", at.PermissionMessage
	default:
		return "", ""
	}
	oneline := strings.Join(strings.Fields(raw), " ")
	return prefix, truncate(oneline, 80)
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
