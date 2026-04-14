package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/watcher"
)

// UI symbols.
const (
	iconMachine  = "⚙️ "
	iconWeb      = "🌐 "
	iconDesktop  = "🖥️ "
	iconAndroid  = "📱 "
	iconWorking    = "🟢"
	iconWaiting    = "🟡"
	iconPermission = "🟠"
	iconEnded      = "⚫"
	cursorMarker = " › "
	boxHoriz     = "─"
	boxVert      = "│"
)

var profileIcons = map[string]string{
	"machine": iconMachine,
	"web":     iconWeb,
	"desktop": iconDesktop,
	"android": iconAndroid,
}

func (m Model) viewProjects() string {
	header := m.renderProjectsHeader()
	footer := m.renderProjectsFooter()
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if contentHeight < 1 {
		contentHeight = 1
	}
	content := lipgloss.NewStyle().Width(m.width).Height(contentHeight).Render(m.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderProjectsHeader returns the fixed header above the scrollable area.
func (m Model) renderProjectsHeader() string {
	return m.renderHeader() + "\n\n"
}

// renderProjectsScrollable returns the scrollable content for the projects view.
func (m Model) renderProjectsScrollable() string {
	var b strings.Builder

	// Two-column: project list (left) + hotkeys (right-aligned)
	left := m.renderProjectList()
	right := m.renderHotkeys()
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 4 {
		gap = 4
	}
	columns := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
	b.WriteString(columns)

	// Active terminals (if any)
	if len(m.state.activeTerminals) > 0 {
		b.WriteString("\n\n")
		b.WriteString(m.renderActiveTerminals())
	}

	// Loop status (if any)
	loopStatus := m.renderLoopStatus()
	if loopStatus != "" {
		b.WriteString("\n\n")
		b.WriteString(loopStatus)
	}

	return b.String()
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

func (m Model) renderProjectList() string {
	var b strings.Builder
	titleStyle := sectionTitleStyle
	if m.focusedPanel != FocusProjects {
		titleStyle = detailStyle
	}
	b.WriteString(titleStyle.Render(locale.T(locale.KeyProjects)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 32) + "\n\n")

	for i, p := range m.state.projects {
		icon := profileIcons[p.Profile]
		if icon == "" {
			icon = "  "
		}

		cursor := "   "
		if i == m.cursor {
			cursor = cursorMarker
		}

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

		b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, icon, name))
		b.WriteString(fmt.Sprintf("     %s",
			detailStyle.Render(p.Profile)))
		if tags != "" {
			b.WriteString(fmt.Sprintf(" %s %s", boxVert, tags))
		}
		b.WriteString("\n\n")
	}

	return b.String()
}

func (m Model) renderHotkeys() string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render(locale.T(locale.KeyHotkeys)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 26) + "\n\n")

	hotkeys := []struct {
		key  string
		desc string
		sep  bool // insert blank line before this entry
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
		{"m", locale.T(locale.KeyChannelComm), false},
		{"g", locale.T(locale.KeyGitStatusHotkeyLabel), false},
		{"a", locale.T(locale.KeyAddProject), true},
		{"e", locale.T(locale.KeyEditConfig), false},
		{"x", locale.T(locale.KeyCloseTerminal), true},
		{"Tab", locale.T(locale.KeySwitchPanel), false},
		{"?", locale.T(locale.KeyHelp), false},
		{"q", locale.T(locale.KeyQuit), false},
	}

	for _, h := range hotkeys {
		if h.sep {
			b.WriteString("\n")
		}
		k := hotkeyLabelStyle.Render(fmt.Sprintf("[%s]", h.key))
		d := hotkeyDescStyle.Render(h.desc)
		b.WriteString(fmt.Sprintf("  %s %s\n", k, d))
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

func (m Model) renderActiveTerminals() string {
	var b strings.Builder
	titleStyle := detailStyle
	if m.focusedPanel == FocusTerminals {
		titleStyle = sectionTitleStyle
	}
	b.WriteString(titleStyle.Render(locale.T(locale.KeyActiveTerminals)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 50) + "\n")

	// Sort keys for stable render order.
	// Use locked variant — caller (syncViewportContent) already holds RLock.
	termKeys := m.sortedTerminalKeysLocked()

	i := 1
	for _, projectID := range termKeys {
		at := m.state.activeTerminals[projectID]
		// Status icon and text.
		statusIcon, statusText := renderAgentStatus(at)

		// Elapsed time since last state change.
		elapsed := formatElapsed(time.Since(at.StateChangedAt))

		// Build project display name with optional worktree branch indicator.
		displayName := m.projectName(projectID)
		if at.WorktreeBranch != "" {
			displayName += " 🌿" + at.WorktreeBranch
		}

		prefix := "  "
		if m.focusedPanel == FocusTerminals && (i-1) == m.termCursor {
			prefix = " ›"
		}
		b.WriteString(fmt.Sprintf("%s[%d] %s %s %s %s\n",
			prefix,
			i,
			selectedStyle.Render(displayName),
			boxVert,
			statusText,
			detailStyle.Render(elapsed),
		))

		// Context preview (question or permission message).
		if prefix, text := agentContextPreview(at, statusIcon); text != "" {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				detailStyle.Render(prefix),
				questionStyle.Render(text),
			))
		}

		// Switch hint.
		if at.LaunchResult != nil && at.LaunchResult.SwitchHint != "" {
			b.WriteString(fmt.Sprintf("      %s\n",
				detailStyle.Render(at.LaunchResult.SwitchHint),
			))
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
			}
		}

		i++
	}

	return b.String()
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

func (m Model) renderLoopStatus() string {
	var b strings.Builder
	hasContent := false

	// Sort project IDs for stable render order.
	projectIDs := make([]string, 0, len(m.state.loops))
	for pid := range m.state.loops {
		projectIDs = append(projectIDs, pid)
	}
	sort.Strings(projectIDs)

	for _, projectID := range projectIDs {
		ls := m.state.loops[projectID]
		if !ls.Active && len(ls.Slots) == 0 {
			continue
		}
		isFocused := m.focusedPanel == FocusLoopSlots && projectID == m.focusProjectID

		if !hasContent {
			titleStyle := detailStyle
			if m.focusedPanel == FocusLoopSlots {
				titleStyle = sectionTitleStyle
			}
			b.WriteString(titleStyle.Render(locale.T(locale.KeyLoopStatus)))
			b.WriteString("\n")
			b.WriteString("  " + strings.Repeat(boxHoriz, 50) + "\n")
			hasContent = true
		}

		projectName := m.projectName(projectID)
		status := locale.T(locale.KeyLoopRunning)
		if !ls.Active {
			status = locale.T(locale.KeyLoopStopping)
		}
		b.WriteString(fmt.Sprintf("  %s (%s)\n",
			selectedStyle.Render(projectName),
			detailStyle.Render(status),
		))

		if len(ls.Slots) == 0 {
			b.WriteString(fmt.Sprintf("    %s\n", detailStyle.Render(locale.T(locale.KeyPollingIssues))))
			continue
		}

		slotKeys := m.sortedSlotKeys(projectID)

		for idx, key := range slotKeys {
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

			cursor := "    "
			titleText := truncate(slot.IssueTitle, 35)
			if isFocused && idx == m.loopCursor {
				cursor = "  " + cursorMarker[1:]
				titleText = selectedStyle.Render(titleText)
			}

			b.WriteString(fmt.Sprintf("%s%s #%s %s  %s\n",
				cursor, icon, slot.IssueID,
				titleText,
				detailStyle.Render(stateText),
			))
			if slot.Error != nil {
				b.WriteString(fmt.Sprintf("      %s\n",
					detailStyle.Render(slot.Error.Error()),
				))
			}

			// Channel event counts (artifact/message) for this issue.
			artCount, msgCount := countChannelEvents(m.state.channelEvents[projectID], slot.IssueID)
			if artCount > 0 || msgCount > 0 {
				b.WriteString(fmt.Sprintf("      📦 %d artifacts  💬 %d messages\n", artCount, msgCount))
			}
		}
	}

	return b.String()
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
