package tracker

import "fmt"

// BuildTrackerDoc generates a markdown document describing the tracker configuration.
// Agents read this file to know which API to use for issue/PR operations.
func BuildTrackerDoc(providerType, baseURL, repo, tokenEnv string) string {
	switch providerType {
	case "forgejo_issues":
		apiBase := baseURL + "/api/v1/repos/" + repo
		authHeader := "Authorization: token $" + tokenEnv
		return fmt.Sprintf(`# Tracker 設定

- 類型: Forgejo
- URL: %s
- Repo: %s
- Auth: 環境變數 %s

## 操作方式

優先使用 Gitea/Forgejo MCP server（名稱通常為 "gitea"，如已安裝）。
如果 MCP 不可用，改用 curl + Forgejo REST API v1。
**不要使用 gh CLI**（此專案不是 GitHub）。

**重要：不論使用 MCP 或 REST API，長文字內容（issue body、PR body、comment）
一律先用 Write tool 寫到暫存檔（如 /tmp/issue_body.md），再用 Read tool 讀取內容傳入 API。
絕對不要在 bash 命令或 MCP 參數裡直接內嵌長文字。**

## REST API 範例

建立 issue:
`+"```"+`
curl -X POST "%s/issues" \
  -H "%s" \
  -H "Content-Type: application/json" \
  -d '{"title":"...","body":"...","labels":["pending"]}'
`+"```"+`

建立 PR:
`+"```"+`
curl -X POST "%s/pulls" \
  -H "%s" \
  -H "Content-Type: application/json" \
  -d '{"title":"...","body":"...","head":"feat/ISSUE-ID-slug","base":"dev"}'
`+"```"+`

新增 comment:
`+"```"+`
curl -X POST "%s/issues/{number}/comments" \
  -H "%s" \
  -H "Content-Type: application/json" \
  -d '{"body":"..."}'
`+"```"+`
`, baseURL, repo, tokenEnv,
			apiBase, authHeader,
			apiBase, authHeader,
			apiBase, authHeader)

	case "github_issues":
		return fmt.Sprintf(`# Tracker 設定

- 類型: GitHub
- Repo: %s
- Auth: 環境變數 %s

## 操作方式

優先使用 gh CLI（如已安裝）。
如果 gh 不可用，改用 curl + GitHub REST API。

**重要：不論使用 gh CLI 或 REST API，長文字內容（issue body、PR body、comment）
一律先用 Write tool 寫到暫存檔（如 /tmp/issue_body.md），再用 Read tool 讀取內容傳入 API。
絕對不要在 bash 命令裡直接內嵌長文字。**

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
