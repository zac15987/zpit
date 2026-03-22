package tracker

import "fmt"

// BuildTrackerDoc generates a markdown document describing the tracker configuration.
// Agents read this file to know which API to use for issue/PR operations.
func BuildTrackerDoc(providerType, baseURL, repo, tokenEnv string) string {
	switch providerType {
	case "forgejo_issues":
		return fmt.Sprintf(`# Tracker 設定

- 類型: Forgejo
- URL: %s
- Repo: %s
- Auth: 環境變數 %s

## 操作方式

優先使用 Forgejo MCP server（如已安裝）。
如果 MCP 不可用，改用 curl + Forgejo REST API v1。
**不要使用 gh CLI**（此專案不是 GitHub）。

## REST API 範例

建立 issue:
`+"```"+`
curl -X POST "%s/api/v1/repos/%s/issues" \
  -H "Authorization: token $%s" \
  -H "Content-Type: application/json" \
  -d '{"title":"...","body":"...","labels":["pending"]}'
`+"```"+`

建立 PR:
`+"```"+`
curl -X POST "%s/api/v1/repos/%s/pulls" \
  -H "Authorization: token $%s" \
  -H "Content-Type: application/json" \
  -d '{"title":"...","body":"...","head":"feat/ISSUE-ID-slug","base":"dev"}'
`+"```"+`

新增 comment:
`+"```"+`
curl -X POST "%s/api/v1/repos/%s/issues/{number}/comments" \
  -H "Authorization: token $%s" \
  -H "Content-Type: application/json" \
  -d '{"body":"..."}'
`+"```"+`
`, baseURL, repo, tokenEnv,
			baseURL, repo, tokenEnv,
			baseURL, repo, tokenEnv,
			baseURL, repo, tokenEnv)

	case "github_issues":
		return fmt.Sprintf(`# Tracker 設定

- 類型: GitHub
- Repo: %s
- Auth: 環境變數 %s

## 操作方式

優先使用 gh CLI（如已安裝）。
如果 gh 不可用，改用 curl + GitHub REST API。

## gh CLI 範例

建立 issue:
`+"```"+`
gh issue create --repo %s --title "..." --body "..." --label "pending"
`+"```"+`

建立 PR:
`+"```"+`
gh pr create --repo %s --title "..." --body "..." --head feat/ISSUE-ID-slug --base dev
`+"```"+`

新增 comment:
`+"```"+`
gh issue comment {number} --repo %s --body "..."
`+"```"+`

## REST API fallback

`+"```"+`
curl -X POST "https://api.github.com/repos/%s/issues" \
  -H "Authorization: Bearer $%s" \
  -H "Accept: application/vnd.github+json" \
  -d '{"title":"...","body":"...","labels":["pending"]}'
`+"```"+`
`, repo, tokenEnv,
			repo, repo, repo,
			repo, tokenEnv)

	default:
		return fmt.Sprintf("# Tracker 設定\n\n未知的 tracker 類型: %s\n", providerType)
	}
}
