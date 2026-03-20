package tracker

import (
	"strings"
)

var requiredSections = []string{
	"## CONTEXT",
	"## APPROACH",
	"## ACCEPTANCE_CRITERIA",
	"## SCOPE",
	"## CONSTRAINTS",
}

// IssueSpec is the structured representation of an issue body.
type IssueSpec struct {
	Context            string
	Approach           string
	AcceptanceCriteria []string // individual "AC-N: ..." lines
	Scope              []ScopeEntry
	Constraints        string
	References         string // optional section
}

// ScopeEntry represents a single file scope line.
type ScopeEntry struct {
	Action string // "modify" | "create" | "delete"
	Path   string
	Reason string
}

// ValidateIssueSpec returns a list of missing required section headers.
// An empty slice means the body is valid.
func ValidateIssueSpec(body string) []string {
	var missing []string
	for _, section := range requiredSections {
		if !strings.Contains(body, section) {
			missing = append(missing, section)
		}
	}
	return missing
}

// ParseIssueSpec splits the body by ## headers and parses each section.
func ParseIssueSpec(body string) (*IssueSpec, error) {
	sections := splitBySections(body)
	spec := &IssueSpec{
		Context:     sections["CONTEXT"],
		Approach:    sections["APPROACH"],
		Constraints: sections["CONSTRAINTS"],
		References:  sections["REFERENCES"],
	}

	// Parse AC entries
	for _, line := range strings.Split(sections["ACCEPTANCE_CRITERIA"], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "AC-") {
			spec.AcceptanceCriteria = append(spec.AcceptanceCriteria, trimmed)
		}
	}

	// Parse SCOPE entries
	for _, line := range strings.Split(sections["SCOPE"], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			spec.Scope = append(spec.Scope, parseScopeEntry(trimmed))
		}
	}

	return spec, nil
}

// splitBySections splits body by "## SECTION_NAME" markers and returns
// a map of section name → content (trimmed).
func splitBySections(body string) map[string]string {
	sections := make(map[string]string)
	lines := strings.Split(body, "\n")

	var currentSection string
	var currentLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			// Save previous section
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(strings.Join(currentLines, "\n"))
			}
			currentSection = strings.TrimPrefix(trimmed, "## ")
			currentLines = nil
		} else if currentSection != "" {
			currentLines = append(currentLines, line)
		}
	}

	// Save last section
	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(strings.Join(currentLines, "\n"))
	}

	return sections
}

// parseScopeEntry parses a line like "[modify] path/to/file (reason)".
func parseScopeEntry(line string) ScopeEntry {
	entry := ScopeEntry{}

	// Extract action from brackets
	closeBracket := strings.Index(line, "]")
	if closeBracket < 0 {
		return entry
	}
	entry.Action = strings.TrimPrefix(line[0:closeBracket+1], "[")
	entry.Action = strings.TrimSuffix(entry.Action, "]")

	rest := strings.TrimSpace(line[closeBracket+1:])

	// Split path and reason at "("
	if parenIdx := strings.Index(rest, "("); parenIdx >= 0 {
		entry.Path = strings.TrimSpace(rest[:parenIdx])
		reason := rest[parenIdx+1:]
		reason = strings.TrimSuffix(reason, ")")
		entry.Reason = strings.TrimSpace(reason)
	} else {
		entry.Path = rest
	}

	return entry
}
