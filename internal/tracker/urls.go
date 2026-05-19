package tracker

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/config"
)

// BuildIssueURL constructs a browser-openable URL for a specific issue.
func BuildIssueURL(provider config.ProviderEntry, repo, issueID string) string {
	switch provider.Type {
	case "forgejo_issues":
		return fmt.Sprintf("%s/%s/issues/%s", strings.TrimRight(provider.URL, "/"), repo, issueID)
	case "github_issues":
		return fmt.Sprintf("https://github.com/%s/issues/%s", repo, issueID)
	default:
		return ""
	}
}

// BuildTrackerURL constructs the issue list URL for a project.
func BuildTrackerURL(provider config.ProviderEntry, repo string) string {
	switch provider.Type {
	case "forgejo_issues":
		return fmt.Sprintf("%s/%s/issues", strings.TrimRight(provider.URL, "/"), repo)
	case "github_issues":
		return fmt.Sprintf("https://github.com/%s/issues", repo)
	default:
		return ""
	}
}

// BuildPRListURL constructs the pull request list URL for a project.
func BuildPRListURL(provider config.ProviderEntry, repo string) string {
	switch provider.Type {
	case "forgejo_issues":
		return fmt.Sprintf("%s/%s/pulls", strings.TrimRight(provider.URL, "/"), repo)
	case "github_issues":
		return fmt.Sprintf("https://github.com/%s/pulls", repo)
	default:
		return ""
	}
}

// BuildPRFilterURL constructs a PR list URL filtered by head branch.
// Used as a fallback when the API can't pinpoint a specific PR.
func BuildPRFilterURL(provider config.ProviderEntry, repo, branch string) string {
	base := BuildPRListURL(provider, repo)
	if base == "" || branch == "" {
		return base
	}
	return fmt.Sprintf("%s?state=all&head=%s", base, branch)
}
