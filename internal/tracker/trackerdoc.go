package tracker

import "fmt"

// BuildTrackerDoc generates a markdown document describing the tracker configuration.
// Agents read this file to know which API to use for issue/PR operations.
func BuildTrackerDoc(providerType, baseURL, repo, tokenEnv, baseBranch string) string {
	switch providerType {
	case "forgejo_issues":
		owner, repoName := splitRepo(repo)
		apiBase := baseURL + "/api/v1/repos/" + repo
		authHeader := "Authorization: token $" + tokenEnv
		return fmt.Sprintf(`# Tracker Configuration

- Type: Forgejo
- URL: %s
- Repo: %s
- Auth: environment variable %s

## Branch Strategy

Default base branch for this project: %s
When creating a PR, the base (target) branch must be set to this value, unless the issue's ## BRANCH section specifies otherwise.

## How to Operate

**Prefer the Gitea MCP server** (tool name prefix: "gitea").
Only fall back to curl + REST API when MCP is unavailable.
**Do not use gh CLI** (this project is not on GitHub).

**Important: Whether using MCP or REST API, always write long text content (issue body, PR body, comment)
to a temporary file first (e.g. /tmp/issue_body.md) using the Write tool, then read it back with the Read tool before passing it in.
Never embed long text directly in MCP parameters or bash commands.**

## MCP Operations (Preferred)

If the gitea MCP server is connected, use these MCP tools (owner="%s", repo="%s"):

Create issue:
  → issue_write tool: owner="%s", repo="%s", title="...", body="..." (write body to temp file first, then read and pass in)
  → After creation, use label_write to set labels

Create PR:
  → pull_request_write tool: owner="%s", repo="%s", title="...", body="...", head="feat/ISSUE-ID-slug", base="%s"

Add comment:
  → issue_write tool (comment function): owner="%s", repo="%s", index={number}, body="..."

Query issues:
  → list_issues or issue_read tool: owner="%s", repo="%s"

Manage labels:
  → label_read tool to query, label_write tool to create/set

## REST API Fallback

Use only when MCP is unavailable.

Create issue:
`+"```"+`
curl -X POST "%s/issues" \
  -H "%s" \
  -H "Content-Type: application/json" \
  -d '{"title":"...","body":"...","labels":["pending"]}'
`+"```"+`

Create PR:
`+"```"+`
curl -X POST "%s/pulls" \
  -H "%s" \
  -H "Content-Type: application/json" \
  -d '{"title":"...","body":"...","head":"feat/ISSUE-ID-slug","base":"%s"}'
`+"```"+`

Add comment:
`+"```"+`
curl -X POST "%s/issues/{number}/comments" \
  -H "%s" \
  -H "Content-Type: application/json" \
  -d '{"body":"..."}'
`+"```"+`

## Label Management

Before operating on a label, first check whether it exists. If it does not exist, create it before using it.
Do not skip or error out just because a label does not exist.

List all labels:
`+"```"+`
curl -s "%s/labels" -H "%s"
`+"```"+`

Create label:
`+"```"+`
curl -X POST "%s/labels" \
  -H "%s" \
  -H "Content-Type: application/json" \
  -d '{"name":"wip","color":"#0E8A16"}'
`+"```"+`
`, baseURL, repo, tokenEnv, baseBranch,
			owner, repoName,
			owner, repoName,
			owner, repoName, baseBranch,
			owner, repoName,
			owner, repoName,
			apiBase, authHeader,
			apiBase, authHeader, baseBranch,
			apiBase, authHeader,
			apiBase, authHeader,
			apiBase, authHeader)

	case "github_issues":
		return fmt.Sprintf(`# Tracker Configuration

- Type: GitHub
- Repo: %s
- Auth: environment variable %s

## Branch Strategy

Default base branch for this project: %s
When creating a PR, the base (target) branch must be set to this value, unless the issue's ## BRANCH section specifies otherwise.

## How to Operate

Prefer gh CLI (if installed).
If gh is unavailable, fall back to curl + GitHub REST API.

**Important: Whether using gh CLI or REST API, always write long text content (issue body, PR body, comment)
to a temporary file first (e.g. /tmp/issue_body.md) using the Write tool, then read it back before passing it to the API.
Never embed long text directly in bash commands.**

## gh CLI Examples

Create issue:
`+"```"+`
gh issue create --repo %s --title "..." --body "..." --label "pending"
`+"```"+`

Create PR:
`+"```"+`
gh pr create --repo %s --title "..." --body "..." --head feat/ISSUE-ID-slug --base %s
`+"```"+`

Add comment:
`+"```"+`
gh issue comment {number} --repo %s --body "..."
`+"```"+`

## Label Management

Before operating on a label, first confirm whether it exists. If it does not exist, create it before using it.
Do not skip or error out just because a label does not exist.

`+"```"+`
gh label create "wip" --repo %s --color "0E8A16" 2>/dev/null || true
`+"```"+`

## REST API Fallback

`+"```"+`
curl -X POST "https://api.github.com/repos/%s/issues" \
  -H "Authorization: Bearer $%s" \
  -H "Accept: application/vnd.github+json" \
  -d '{"title":"...","body":"...","labels":["pending"]}'
`+"```"+`
`, repo, tokenEnv, baseBranch,
			repo,
			repo, baseBranch, repo, repo,
			repo, tokenEnv)

	default:
		return fmt.Sprintf("# Tracker Configuration\n\nUnknown tracker type: %s\n", providerType)
	}
}
