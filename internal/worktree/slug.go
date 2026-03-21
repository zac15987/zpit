package worktree

import "strings"

const defaultMaxSlugLen = 50

// Slugify converts an issue title to a URL-safe slug for branch/directory naming.
// Non-ASCII and non-alphanumeric characters are replaced with hyphens.
// Consecutive hyphens are collapsed, leading/trailing hyphens trimmed.
// If maxLen <= 0, defaults to 50.
func Slugify(title string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = defaultMaxSlugLen
	}

	var b strings.Builder
	prevHyphen := false

	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen && b.Len() > 0 {
			b.WriteByte('-')
			prevHyphen = true
		}
	}

	slug := strings.TrimRight(b.String(), "-")

	if len(slug) > maxLen {
		slug = slug[:maxLen]
		slug = strings.TrimRight(slug, "-")
	}

	return slug
}
