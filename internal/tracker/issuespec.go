package tracker

import (
	"fmt"
	"strconv"
	"strings"
)

var requiredSections = []string{
	"## CONTEXT",
	"## APPROACH",
	"## ACCEPTANCE_CRITERIA",
	"## SCOPE",
	"## CONSTRAINTS",
}

// validScopeActions lists the allowed action keywords in SCOPE entries.
var validScopeActions = []string{"modify", "create", "delete"}

// vagueWords lists forbidden vague words/phrases in acceptance criteria.
var vagueWords = []string{"appropriate", "reasonable", "sufficient", "when necessary"}

// TaskEntry represents a single task line in the ## TASKS section.
type TaskEntry struct {
	ID          string   // e.g. "T1", "T2"
	Description string   // task description text
	Parallel    bool     // true if [P] marker is present
	Paths       []string // file paths extracted from [create], [modify], [delete] actions
	DependsOn   []string // task IDs this task depends on; empty if (depends: none)
}

// IssueSpec is the structured representation of an issue body.
type IssueSpec struct {
	Context            string
	Approach           string
	AcceptanceCriteria []string // individual "AC-N: ..." lines
	Scope              []ScopeEntry
	Constraints        string
	References         string // optional section
	Branch             string // optional: PR target branch (from ## BRANCH section)
	Tasks              []TaskEntry // optional: task decomposition from ## TASKS section
}

// ScopeEntry represents a single file scope line.
type ScopeEntry struct {
	Action string // "modify" | "create" | "delete"
	Path   string
	Reason string
}

// ValidationResult holds structured validation output with two severity levels.
// Errors are hard blocks (Loop engine refuses to execute).
// Warnings are soft signals (TUI can display, Loop proceeds).
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// ValidateIssueSpec performs comprehensive validation on an issue body and
// returns a ValidationResult with Errors (hard block) and Warnings (soft).
func ValidateIssueSpec(body string) ValidationResult {
	result := ValidationResult{}

	// --- Errors ---

	// Check required sections
	for _, section := range requiredSections {
		if !strings.Contains(body, section) {
			result.Errors = append(result.Errors, fmt.Sprintf("missing required section: %s", section))
		}
	}

	// Check for unresolved markers
	checkUnresolvedMarkers(body, &result)

	// Check SCOPE entry format
	checkScopeFormat(body, &result)

	// --- Warnings ---

	// Check vague words in AC lines
	checkVagueWords(body, &result)

	// Check AC numbering gaps
	checkACNumberingGaps(body, &result)

	// Check SCOPE-AC coverage
	checkScopeACCoverage(body, &result)

	// Check TASKS-SCOPE cross-validation
	checkTasksScopeCoverage(body, &result)

	return result
}

// checkUnresolvedMarkers detects [UNRESOLVED: ...] markers left in the body.
func checkUnresolvedMarkers(body string, result *ValidationResult) {
	idx := 0
	for {
		pos := strings.Index(body[idx:], "[UNRESOLVED: ")
		if pos < 0 {
			break
		}
		absPos := idx + pos
		// Extract the marker text up to the closing bracket
		endPos := strings.Index(body[absPos:], "]")
		marker := body[absPos:]
		if endPos >= 0 {
			marker = body[absPos : absPos+endPos+1]
		}
		result.Errors = append(result.Errors,
			fmt.Sprintf("UNRESOLVED marker found: %s", marker))
		idx = absPos + 1
	}
}

// checkScopeFormat validates that lines in the SCOPE section start with a valid
// bracketed action keyword ([modify], [create], or [delete]).
func checkScopeFormat(body string, result *ValidationResult) {
	sections := splitBySections(body)
	scopeContent, ok := sections["SCOPE"]
	if !ok || scopeContent == "" {
		return
	}

	for _, line := range strings.Split(scopeContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Lines that look like scope entries (contain a path-like string) but
		// don't start with a valid bracket action are format errors.
		if !strings.HasPrefix(trimmed, "[") {
			result.Errors = append(result.Errors,
				fmt.Sprintf("SCOPE format error: line does not start with [modify], [create], or [delete]: %s", trimmed))
			continue
		}

		// Has bracket — extract action and validate
		closeBracket := strings.Index(trimmed, "]")
		if closeBracket < 0 {
			result.Errors = append(result.Errors,
				fmt.Sprintf("SCOPE format error: unclosed bracket: %s", trimmed))
			continue
		}
		action := strings.ToLower(trimmed[1:closeBracket])
		isValid := false
		for _, valid := range validScopeActions {
			if action == valid {
				isValid = true
				break
			}
		}
		if !isValid {
			result.Errors = append(result.Errors,
				fmt.Sprintf("SCOPE format error: invalid action [%s], must be [modify], [create], or [delete]: %s", action, trimmed))
		}
	}
}

// checkVagueWords scans AC lines for forbidden vague words/phrases.
func checkVagueWords(body string, result *ValidationResult) {
	sections := splitBySections(body)
	acContent, ok := sections["ACCEPTANCE_CRITERIA"]
	if !ok || acContent == "" {
		return
	}

	for _, line := range strings.Split(acContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "AC-") {
			continue
		}

		// Extract AC identifier (e.g., "AC-2")
		acID := extractACID(trimmed)

		lower := strings.ToLower(trimmed)
		for _, vw := range vagueWords {
			if strings.Contains(lower, vw) {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("vague word in %s: \"%s\"", acID, vw))
			}
		}
	}
}

// checkACNumberingGaps detects gaps in AC-N numbering (e.g., AC-1 and AC-3 but no AC-2).
func checkACNumberingGaps(body string, result *ValidationResult) {
	sections := splitBySections(body)
	acContent, ok := sections["ACCEPTANCE_CRITERIA"]
	if !ok || acContent == "" {
		return
	}

	// Collect all AC numbers
	var numbers []int
	for _, line := range strings.Split(acContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "AC-") {
			continue
		}
		// Parse number after "AC-"
		rest := trimmed[3:]
		numStr := ""
		for _, ch := range rest {
			if ch >= '0' && ch <= '9' {
				numStr += string(ch)
			} else {
				break
			}
		}
		if num, err := strconv.Atoi(numStr); err == nil {
			numbers = append(numbers, num)
		}
	}

	if len(numbers) == 0 {
		return
	}

	// Find min and max
	minN, maxN := numbers[0], numbers[0]
	present := make(map[int]bool)
	for _, n := range numbers {
		present[n] = true
		if n < minN {
			minN = n
		}
		if n > maxN {
			maxN = n
		}
	}

	// Check for gaps between min and max
	for i := minN; i <= maxN; i++ {
		if !present[i] {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("AC numbering gap: AC-%d is missing", i))
		}
	}
}

// checkScopeACCoverage warns when a SCOPE entry's filename does not appear in any AC line.
func checkScopeACCoverage(body string, result *ValidationResult) {
	sections := splitBySections(body)
	scopeContent := sections["SCOPE"]
	acContent := sections["ACCEPTANCE_CRITERIA"]

	if scopeContent == "" || acContent == "" {
		return
	}

	// Collect all AC lines for substring matching
	acLower := strings.ToLower(acContent)

	for _, line := range strings.Split(scopeContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") {
			continue
		}
		entry := parseScopeEntry(trimmed)
		if entry.Path == "" {
			continue
		}

		// Extract the filename (last segment of path)
		filename := entry.Path
		if lastSlash := strings.LastIndex(entry.Path, "/"); lastSlash >= 0 {
			filename = entry.Path[lastSlash+1:]
		}

		if !strings.Contains(acLower, strings.ToLower(filename)) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("SCOPE entry not covered by any AC: %s", entry.Path))
		}
	}
}

// extractACID extracts the "AC-N" identifier from an AC line.
func extractACID(line string) string {
	// line starts with "AC-", extract "AC-N"
	rest := line[3:]
	numStr := ""
	for _, ch := range rest {
		if ch >= '0' && ch <= '9' {
			numStr += string(ch)
		} else {
			break
		}
	}
	if numStr == "" {
		return "AC-?"
	}
	return "AC-" + numStr
}

// ParseIssueSpec splits the body by ## headers and parses each section.
func ParseIssueSpec(body string) (*IssueSpec, error) {
	sections := splitBySections(body)
	spec := &IssueSpec{
		Context:     sections["CONTEXT"],
		Approach:    sections["APPROACH"],
		Constraints: sections["CONSTRAINTS"],
		References:  sections["REFERENCES"],
		Branch:      sections["BRANCH"],
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

	// Parse TASKS entries (optional section)
	if tasksContent, ok := sections["TASKS"]; ok && tasksContent != "" {
		for _, line := range strings.Split(tasksContent, "\n") {
			trimmed := strings.TrimSpace(line)
			if entry, ok := parseTaskEntry(trimmed); ok {
				spec.Tasks = append(spec.Tasks, entry)
			}
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

// checkTasksScopeCoverage warns when a TASKS entry references a file path
// not found in any SCOPE entry.
func checkTasksScopeCoverage(body string, result *ValidationResult) {
	sections := splitBySections(body)
	tasksContent, ok := sections["TASKS"]
	if !ok || tasksContent == "" {
		return
	}
	scopeContent := sections["SCOPE"]
	if scopeContent == "" {
		return
	}

	// Collect all SCOPE file paths
	scopePaths := make(map[string]bool)
	for _, line := range strings.Split(scopeContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			entry := parseScopeEntry(trimmed)
			if entry.Path != "" {
				scopePaths[entry.Path] = true
			}
		}
	}

	// Check each task's paths against SCOPE
	for _, line := range strings.Split(tasksContent, "\n") {
		trimmed := strings.TrimSpace(line)
		entry, ok := parseTaskEntry(trimmed)
		if !ok {
			continue
		}
		for _, p := range entry.Paths {
			if !scopePaths[p] {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("TASKS entry %s references file %s not found in SCOPE", entry.ID, p))
			}
		}
	}
}

// parseTaskEntry parses a task line like:
// T1: Add TaskEntry struct [modify] issuespec.go (depends: none)
// T2: [P] Update prompt [modify] coding.go [modify] common.go (depends: T1)
// Returns the parsed entry and true if the line is a valid task line.
func parseTaskEntry(line string) (TaskEntry, bool) {
	// Task lines start with T followed by a number and a colon
	if !strings.HasPrefix(line, "T") {
		return TaskEntry{}, false
	}

	// Extract ID (e.g., "T1", "T12")
	colonIdx := strings.Index(line, ":")
	if colonIdx < 2 {
		return TaskEntry{}, false
	}
	id := line[:colonIdx]
	// Validate ID is T followed by digits
	numPart := id[1:]
	if _, err := strconv.Atoi(numPart); err != nil {
		return TaskEntry{}, false
	}

	rest := strings.TrimSpace(line[colonIdx+1:])

	entry := TaskEntry{ID: id}

	// Check for [P] parallel marker at the start of rest
	if strings.HasPrefix(rest, "[P]") {
		entry.Parallel = true
		rest = strings.TrimSpace(rest[3:])
	}

	// Extract (depends: ...) from the end
	if depStart := strings.LastIndex(rest, "(depends:"); depStart >= 0 {
		depSection := rest[depStart:]
		rest = strings.TrimSpace(rest[:depStart])

		// Extract the content between "depends:" and ")"
		depContent := strings.TrimPrefix(depSection, "(depends:")
		depContent = strings.TrimSuffix(strings.TrimSpace(depContent), ")")
		depContent = strings.TrimSpace(depContent)

		if depContent != "none" && depContent != "" {
			for _, dep := range strings.Split(depContent, ",") {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					entry.DependsOn = append(entry.DependsOn, dep)
				}
			}
		}
	}

	// Extract file paths from bracket patterns: [create], [modify], [delete]
	// and collect the description (everything that's not a file action)
	var descParts []string
	remaining := rest
	for remaining != "" {
		bracketIdx := strings.Index(remaining, "[")
		if bracketIdx < 0 {
			// No more brackets — rest is description
			if part := strings.TrimSpace(remaining); part != "" {
				descParts = append(descParts, part)
			}
			break
		}

		// Text before the bracket is description
		if bracketIdx > 0 {
			if part := strings.TrimSpace(remaining[:bracketIdx]); part != "" {
				descParts = append(descParts, part)
			}
		}

		closeBracket := strings.Index(remaining[bracketIdx:], "]")
		if closeBracket < 0 {
			// Malformed bracket — treat rest as description
			if part := strings.TrimSpace(remaining[bracketIdx:]); part != "" {
				descParts = append(descParts, part)
			}
			break
		}
		closeBracket += bracketIdx

		action := strings.ToLower(remaining[bracketIdx+1 : closeBracket])
		isFileAction := false
		for _, valid := range validScopeActions {
			if action == valid {
				isFileAction = true
				break
			}
		}

		if isFileAction {
			// Extract the file path (next whitespace-delimited token)
			afterAction := strings.TrimSpace(remaining[closeBracket+1:])
			spaceIdx := strings.IndexAny(afterAction, " \t")
			var filePath string
			if spaceIdx >= 0 {
				filePath = afterAction[:spaceIdx]
				remaining = afterAction[spaceIdx:]
			} else {
				filePath = afterAction
				remaining = ""
			}
			if filePath != "" {
				entry.Paths = append(entry.Paths, filePath)
			}
		} else {
			// Not a file action bracket — treat as description
			if part := strings.TrimSpace(remaining[bracketIdx : closeBracket+1]); part != "" {
				descParts = append(descParts, part)
			}
			remaining = remaining[closeBracket+1:]
		}
	}

	entry.Description = strings.Join(descParts, " ")

	return entry, true
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
