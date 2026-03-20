package tracker

import (
	"testing"

	"github.com/zac15987/zpit/internal/config"
)

func TestBuildIssueURL_Forgejo(t *testing.T) {
	provider := config.ProviderEntry{Type: "forgejo_issues", URL: "https://git.example.com"}
	url := BuildIssueURL(provider, "org/repo", "42")
	want := "https://git.example.com/org/repo/issues/42"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestBuildIssueURL_ForejoTrailingSlash(t *testing.T) {
	provider := config.ProviderEntry{Type: "forgejo_issues", URL: "https://git.example.com/"}
	url := BuildIssueURL(provider, "org/repo", "1")
	want := "https://git.example.com/org/repo/issues/1"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestBuildIssueURL_GitHub(t *testing.T) {
	provider := config.ProviderEntry{Type: "github_issues"}
	url := BuildIssueURL(provider, "user/repo", "10")
	want := "https://github.com/user/repo/issues/10"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestBuildIssueURL_UnknownType(t *testing.T) {
	provider := config.ProviderEntry{Type: "unknown"}
	url := BuildIssueURL(provider, "org/repo", "1")
	if url != "" {
		t.Errorf("expected empty string, got %q", url)
	}
}

func TestBuildTrackerURL_Forgejo(t *testing.T) {
	provider := config.ProviderEntry{Type: "forgejo_issues", URL: "https://git.example.com"}
	url := BuildTrackerURL(provider, "org/repo")
	want := "https://git.example.com/org/repo/issues"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestBuildTrackerURL_GitHub(t *testing.T) {
	provider := config.ProviderEntry{Type: "github_issues"}
	url := BuildTrackerURL(provider, "user/repo")
	want := "https://github.com/user/repo/issues"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}
