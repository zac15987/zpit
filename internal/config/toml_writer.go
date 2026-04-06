package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// headerRe matches TOML table/array-of-tables headers like [[projects]] or [section].
var headerRe = regexp.MustCompile(`^\s*\[`)

// UpdateProjectField updates a single field in the [[projects]] block matching
// projectID. Supports field types: bool and []string. Preserves comments,
// formatting, and all other content in the file.
func UpdateProjectField(path, projectID, fieldName string, value interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	updated, err := updateLines(lines, projectID, fieldName, value)
	if err != nil {
		return err
	}

	output := strings.Join(updated, "\n")
	return os.WriteFile(path, []byte(output), 0o644)
}

// updateLines performs the targeted update on raw lines.
// Returns modified lines or error.
func updateLines(lines []string, projectID, fieldName string, value interface{}) ([]string, error) {
	if err := validateFieldValue(fieldName, value); err != nil {
		return nil, err
	}

	blockStart, blockEnd, found := findProjectBlock(lines, projectID)
	if !found {
		return nil, fmt.Errorf("project %q not found in config", projectID)
	}

	fieldIdx := findFieldInBlock(lines, blockStart, blockEnd, fieldName)

	formatted := formatValue(value)

	if fieldIdx >= 0 {
		// Replace existing field value, preserving trailing comment.
		lines[fieldIdx] = replaceFieldValue(lines[fieldIdx], fieldName, formatted)
	} else {
		// Insert new field in the main key-value area of the block,
		// before any sub-table header (e.g. [projects.path]) or trailing
		// blank lines at the boundary.
		insertIdx := findInsertionPoint(lines, blockStart, blockEnd)
		newLine := fieldName + " = " + formatted
		lines = insertLine(lines, insertIdx, newLine)
	}

	return lines, nil
}

// validateFieldValue checks that the field name and value type are supported.
func validateFieldValue(fieldName string, value interface{}) error {
	switch fieldName {
	case "channel_enabled":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("channel_enabled requires bool value, got %T", value)
		}
	case "channel_listen":
		if _, ok := value.([]string); !ok {
			return fmt.Errorf("channel_listen requires []string value, got %T", value)
		}
	default:
		return fmt.Errorf("unsupported field %q for targeted TOML writer", fieldName)
	}
	return nil
}

// findProjectBlock locates the [[projects]] block containing the given project ID.
// Returns the line index of the [[projects]] header, the exclusive end index
// (next header or len(lines)), and whether the block was found.
func findProjectBlock(lines []string, projectID string) (start, end int, found bool) {
	// Build a regex that matches: id = "projectID" or id = 'projectID'
	idPattern := regexp.MustCompile(
		`^\s*id\s*=\s*["']` + regexp.QuoteMeta(projectID) + `["']`,
	)

	inProjectsBlock := false
	blockStart := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "[[projects]]" {
			// Start of a new [[projects]] block.
			// If we were already tracking a block, close it.
			if inProjectsBlock && blockStart >= 0 {
				// Previous block ended at this line.
			}
			inProjectsBlock = true
			blockStart = i
			continue
		}

		// A different header ends the current block.
		if headerRe.MatchString(line) && trimmed != "[[projects]]" {
			inProjectsBlock = false
			blockStart = -1
			continue
		}

		if inProjectsBlock && idPattern.MatchString(line) {
			// Found the matching project. Now find the block's end.
			end := findBlockEnd(lines, blockStart, i)
			return blockStart, end, true
		}
	}

	return 0, 0, false
}

// findBlockEnd returns the exclusive end index for a [[projects]] block that
// starts at blockStart. The end is the next [[ or [ header line, or len(lines).
func findBlockEnd(lines []string, blockStart, idLine int) int {
	for i := blockStart + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if headerRe.MatchString(trimmed) && trimmed != "" {
			// A sub-table like [projects.path] within the same block is fine.
			// Only a new top-level header or another [[projects]] ends the block.
			if strings.HasPrefix(trimmed, "[[") || !strings.HasPrefix(trimmed, "[projects.") {
				return i
			}
		}
	}
	return len(lines)
}

// findInsertionPoint returns the line index where a new field should be inserted
// within a [[projects]] block. It places the field after the last key=value line
// in the main section, before any sub-table header or trailing blank lines.
func findInsertionPoint(lines []string, blockStart, blockEnd int) int {
	// Find the first sub-table header within the block (e.g. [projects.path]).
	subTableIdx := -1
	for i := blockStart + 1; i < blockEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") {
			subTableIdx = i
			break
		}
	}

	// The main section ends at either the sub-table or the block end.
	mainEnd := blockEnd
	if subTableIdx >= 0 {
		mainEnd = subTableIdx
	}

	// Walk backwards past blank lines to find the last content line.
	insertIdx := mainEnd
	for insertIdx > blockStart && isBlankLine(lines[insertIdx-1]) {
		insertIdx--
	}
	return insertIdx
}

// findFieldInBlock searches for a line matching "fieldName = ..." within
// [blockStart, blockEnd). Returns the line index or -1 if not found.
func findFieldInBlock(lines []string, blockStart, blockEnd int, fieldName string) int {
	fieldPattern := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(fieldName) + `\s*=`)
	for i := blockStart; i < blockEnd; i++ {
		if fieldPattern.MatchString(lines[i]) {
			return i
		}
	}
	return -1
}

// replaceFieldValue replaces the value portion of a "field = value" line,
// preserving any trailing inline comment.
func replaceFieldValue(line, fieldName, newValue string) string {
	// Split on the first '=' to get the key part.
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return line
	}

	keyPart := line[:eqIdx+1] // includes the '='

	// The rest is " value" possibly followed by "# comment".
	rest := line[eqIdx+1:]

	// Find trailing comment. We need to be careful: '#' inside strings
	// should not be treated as comments. For our supported types (bool and
	// []string with simple values), we can look for '#' that appears after
	// the value portion.
	comment := extractTrailingComment(rest)

	if comment != "" {
		return keyPart + " " + newValue + "  " + comment
	}
	return keyPart + " " + newValue
}

// extractTrailingComment extracts a trailing "# ..." comment from the value
// portion of a TOML line. Returns the comment including '#', or empty string.
func extractTrailingComment(valuePart string) string {
	// For bool values, the value is simple (true/false), so # after that is a comment.
	// For array values like ["a", "b"], we need to skip past the closing ']'.

	trimmed := strings.TrimSpace(valuePart)

	if strings.HasPrefix(trimmed, "[") {
		// Array value: find the closing ']', then look for '#'.
		bracketIdx := strings.LastIndex(trimmed, "]")
		if bracketIdx >= 0 {
			after := trimmed[bracketIdx+1:]
			hashIdx := strings.Index(after, "#")
			if hashIdx >= 0 {
				return strings.TrimSpace(after[hashIdx:])
			}
		}
		return ""
	}

	// Simple value (bool, string, number): find '#' in the value part.
	hashIdx := strings.Index(trimmed, "#")
	if hashIdx > 0 {
		return strings.TrimSpace(trimmed[hashIdx:])
	}
	return ""
}

// formatValue converts a Go value to its TOML representation.
func formatValue(value interface{}) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []string:
		if len(v) == 0 {
			return "[]"
		}
		quoted := make([]string, len(v))
		for i, s := range v {
			quoted[i] = `"` + s + `"`
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// isBlankLine returns true if a line contains only whitespace.
func isBlankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

// insertLine inserts a new line at the given index.
func insertLine(lines []string, idx int, newLine string) []string {
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:idx]...)
	result = append(result, newLine)
	result = append(result, lines[idx:]...)
	return result
}
