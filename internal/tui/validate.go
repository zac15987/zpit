package tui

import (
	"strings"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/tracker"
)

// Validation group names used by checkConfig.
const (
	valPath       = "path"
	valTracker    = "tracker"
	valTrackerURL = "trackerURL"
	valWorktree   = "worktree"
)

// selectedProject returns the currently selected project, or nil if the list is empty.
func (m Model) selectedProject() *config.ProjectConfig {
	if m.cursor >= 0 && m.cursor < len(m.state.projects) {
		return &m.state.projects[m.cursor]
	}
	return nil
}

// checkConfig runs the specified validation groups for the given project.
// If any check fails, it sets the error overlay, logs the errors, and returns false.
// op is the triggering operation label (e.g. "[o]", "[l]") used only in log output.
func (m *Model) checkConfig(op string, project config.ProjectConfig, groups ...string) bool {
	// Snapshot clients under RLock for thread-safe access.
	m.state.RLock()
	clients := m.state.clients
	m.state.RUnlock()

	var errs []string
	for _, g := range groups {
		switch g {
		case valPath:
			errs = append(errs, validatePath(project)...)
		case valTracker:
			errs = append(errs, validateTracker(project, clients)...)
		case valTrackerURL:
			trackerErrs := validateTracker(project, clients)
			errs = append(errs, trackerErrs...)
			if len(trackerErrs) == 0 {
				errs = append(errs, validateTrackerURL(project, m.state.cfg.Providers.Tracker)...)
			}
		case valWorktree:
			errs = append(errs, validateWorktree(m.state.cfg.Worktree)...)
		}
	}
	if len(errs) == 0 {
		return true
	}
	m.state.logger.Printf("config check failed %s project=%s: %s", op, project.ID, strings.Join(errs, "; "))
	m.showErrorOverlay(errs)
	return false
}

// showErrorOverlay formats error messages and sets the error overlay string.
func (m *Model) showErrorOverlay(errs []string) {
	var b strings.Builder
	b.WriteString(locale.T(locale.KeyErrConfigTitle))
	b.WriteString("\n\n")
	for _, e := range errs {
		b.WriteString("  • " + e + "\n")
	}
	b.WriteString("\n")
	b.WriteString(locale.T(locale.KeyErrDismissHint))
	m.errorOverlay = b.String()
}

// --- Validation group functions ---

func validatePath(project config.ProjectConfig) []string {
	if platform.ResolvePath(project.Path.Windows, project.Path.WSL) == "" {
		return []string{locale.T(locale.KeyErrPathEmpty)}
	}
	return nil
}

func validateTracker(project config.ProjectConfig, clients map[string]tracker.TrackerClient) []string {
	var errs []string
	if project.Tracker == "" {
		errs = append(errs, locale.T(locale.KeyNoTrackerConfigured))
		return errs // no point checking further
	}
	if _, ok := clients[project.Tracker]; !ok {
		errs = append(errs, locale.T(locale.KeyTrackerTokenNotSet))
	}
	if project.Repo == "" {
		errs = append(errs, locale.T(locale.KeyErrRepoEmpty))
	}
	return errs
}

// validateTrackerURL checks only the provider URL.
// Caller must ensure validateTracker passes first (checkConfig guarantees ordering).
func validateTrackerURL(project config.ProjectConfig, providers map[string]config.ProviderEntry) []string {
	if project.Tracker == "" {
		return nil
	}
	provider, ok := providers[project.Tracker]
	if !ok {
		return nil
	}
	if provider.URL == "" && provider.Type != "github_issues" {
		return []string{locale.T(locale.KeyErrTrackerURLEmpty)}
	}
	return nil
}

func validateWorktree(cfg config.WorktreeConfig) []string {
	if platform.ResolvePath(cfg.BaseDirWindows, cfg.BaseDirWSL) == "" {
		return []string{locale.T(locale.KeyErrWorktreeBaseEmpty)}
	}
	return nil
}
