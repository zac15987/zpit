package prompt

import "strings"

// FrontmatterField extracts the value of a named field from YAML-like
// frontmatter delimited by "---\n" markers. Returns the trimmed value
// if found, or empty string if the field or frontmatter is absent.
func FrontmatterField(md []byte, field string) string {
	s := strings.ReplaceAll(string(md), "\r\n", "\n")

	const marker = "---\n"
	first := strings.Index(s, marker)
	if first < 0 {
		return ""
	}
	after := first + len(marker)
	second := strings.Index(s[after:], marker)
	if second < 0 {
		return ""
	}
	body := s[after : after+second]

	prefix := field + ":"
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}
