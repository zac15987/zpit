package tui

// view_gitstatus.go -- Git Status page rendering (pure view functions, no state mutation).
//
// Lock protocol:
//   All render methods in this file are called from syncViewportContent (which
//   holds RLock) or from View() (which also holds RLock). They must NOT acquire
//   locks themselves.
//
// Model fields read (added by T7):
//   gitProjectID  string        -- project ID shown in the git status view
//   gitData       *GitData      -- loaded branch + graph data, nil when loading
//   gitError      string        -- error message, cleared on successful load
//   gitOp         string        -- "" | "fetch" | "pull" | "refresh"
//   spinner       spinner.Model -- Bubble Tea spinner for async operation indicator

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/git"
	"github.com/zac15987/zpit/internal/locale"
)

// viewGitStatus renders the complete git status screen (header + viewport + footer).
func (m Model) viewGitStatus() string {
	header := m.renderGitStatusHeader()
	footer := m.renderGitStatusFooter()
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if contentHeight < 1 {
		contentHeight = 1
	}
	content := lipgloss.NewStyle().Width(m.width).Height(contentHeight).Render(m.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderGitStatusHeader returns the fixed header for the git status view.
func (m Model) renderGitStatusHeader() string {
	projectName := locale.T(locale.KeyGitStatus)
	branchHint := ""

	p := m.findProject(m.gitProjectID)
	if p != nil {
		projectName = p.Name
	}

	if m.gitData != nil {
		branchHint = currentBranchDisplay(m.gitData)
	}

	title := fmt.Sprintf(locale.T(locale.KeyGitStatusTitle), projectName)
	if branchHint != "" {
		title += "  " + detailStyle.Render("(branch: "+branchHint+")")
	}

	return fmt.Sprintf(" %s\n %s\n", title, strings.Repeat(boxHoriz, lipgloss.Width(title)+2))
}

// currentBranchDisplay returns the current branch name or detached hash for the header.
func currentBranchDisplay(data *GitData) string {
	if data.Branches.Detached != nil {
		return "detached:" + data.Branches.Detached.ShortHash
	}
	for _, lb := range data.Branches.Local {
		if lb.IsCurrent {
			return lb.Name
		}
	}
	return ""
}

// renderGitStatusScrollable returns the scrollable content for the git status view.
// Assigned to the viewport via syncViewportContent.
func (m Model) renderGitStatusScrollable() string {
	var b strings.Builder

	// Loading placeholder.
	if m.gitData == nil && m.gitError == "" {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyGitStatusRefreshing)) + "\n")
		return b.String()
	}

	// Error banner (rendered even when partial data is available).
	if m.gitError != "" {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(colorError).Render("Error: "+m.gitError) + "\n\n")
	}

	if m.gitData == nil {
		return b.String()
	}

	b.WriteString(m.renderLocalBranches())
	b.WriteString("\n")
	b.WriteString(m.renderRemoteOnlyBranches())
	b.WriteString("\n")
	b.WriteString(m.renderCommitGraph())

	return b.String()
}

// renderLocalBranches renders the "Local Branches" section.
func (m Model) renderLocalBranches() string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render(locale.T(locale.KeyGitStatusLocalBranches)))
	b.WriteString("\n")

	branches := sortedLocalBranches(m.gitData.Branches.Local)

	// Detached HEAD line (before any branch lines).
	if m.gitData.Branches.Detached != nil {
		line := fmt.Sprintf(locale.T(locale.KeyGitStatusDetached), m.gitData.Branches.Detached.ShortHash)
		b.WriteString("  " + workingStyle.Render(line) + "\n")
	}

	for _, lb := range branches {
		b.WriteString(renderLocalBranchLine(lb))
	}

	if len(branches) == 0 && m.gitData.Branches.Detached == nil {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyGitStatusNone)) + "\n")
	}

	return b.String()
}

// renderLocalBranchLine formats a single local branch entry.
func renderLocalBranchLine(lb git.LocalBranch) string {
	var b strings.Builder

	// Prefix: current indicator.
	if lb.IsCurrent {
		b.WriteString("  " + workingStyle.Bold(true).Render("* "))
	} else {
		b.WriteString("    ")
	}

	// Branch name.
	if lb.IsCurrent {
		b.WriteString(workingStyle.Bold(true).Render(lb.Name))
	} else {
		b.WriteString(normalStyle.Render(lb.Name))
	}

	// Upstream info or "(no upstream)".
	if lb.Upstream == "" {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyGitStatusNoUpstream)))
	} else {
		aheadBehind := fmt.Sprintf("  \u2191%d \u2193%d", lb.Ahead, lb.Behind)
		b.WriteString(detailStyle.Render(aheadBehind))
		b.WriteString(detailStyle.Render("   \u2192 " + lb.Upstream))
	}

	b.WriteString("\n")
	return b.String()
}

// sortedLocalBranches returns a copy of local branches sorted: current first, then alphabetical.
func sortedLocalBranches(branches []git.LocalBranch) []git.LocalBranch {
	sorted := make([]git.LocalBranch, len(branches))
	copy(sorted, branches)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].IsCurrent != sorted[j].IsCurrent {
			return sorted[i].IsCurrent
		}
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

// renderRemoteOnlyBranches renders the "Remote-only Branches" section.
func (m Model) renderRemoteOnlyBranches() string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render(locale.T(locale.KeyGitStatusRemoteOnly)))
	b.WriteString("\n")

	if len(m.gitData.Branches.RemoteOnly) == 0 {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyGitStatusNone)) + "\n")
		return b.String()
	}

	for _, name := range m.gitData.Branches.RemoteOnly {
		b.WriteString("  " + detailStyle.Render(name+"  "+locale.T(locale.KeyGitStatusRemoteOnlyTag)) + "\n")
	}

	return b.String()
}

// renderCommitGraph renders the "Commit Graph" section.
func (m Model) renderCommitGraph() string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render(locale.T(locale.KeyGitStatusGraph)))
	b.WriteString("\n")

	if m.gitData.Graph == "" {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyGitStatusNoCommits)) + "\n")
		return b.String()
	}

	// Render graph with ANSI passthrough (lipgloss/viewport handle this natively).
	b.WriteString(m.gitData.Graph)
	if !strings.HasSuffix(m.gitData.Graph, "\n") {
		b.WriteString("\n")
	}

	return b.String()
}

// renderGitStatusFooter returns the fixed footer for the git status view.
func (m Model) renderGitStatusFooter() string {
	var b strings.Builder

	// Left side: operation status or transient status message.
	leftContent := ""
	if m.gitOp != "" {
		opMsg := gitOpStatusText(m.gitOp)
		leftContent = m.spinner.View() + " " + opMsg
	} else if m.statusMessage != "" && time.Now().Before(m.statusExpiry) {
		leftContent = m.statusMessage
	}

	// Right side: hotkey hints.
	rightContent := styledHotkeys(locale.T(locale.KeyGitStatusHotkeys))

	if leftContent != "" {
		leftRendered := statusBarStyle.Render(" " + leftContent + " ")
		gap := m.width - lipgloss.Width(leftRendered) - lipgloss.Width(rightContent)
		if gap < 2 {
			gap = 2
		}
		b.WriteString(leftRendered + strings.Repeat(" ", gap) + rightContent)
	} else {
		b.WriteString(rightContent)
	}

	b.WriteString("\n")
	return b.String()
}

// gitOpStatusText returns the localized status text for the current git operation.
func gitOpStatusText(op string) string {
	switch op {
	case "fetch":
		return locale.T(locale.KeyGitStatusFetching)
	case "pull":
		return locale.T(locale.KeyGitStatusPulling)
	case "refresh":
		return locale.T(locale.KeyGitStatusRefreshing)
	default:
		return op
	}
}
