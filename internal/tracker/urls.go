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
