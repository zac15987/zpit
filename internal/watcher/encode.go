package watcher

import (
	"os"
	"path/filepath"
	"unicode"
)

// EncodeCwd converts an absolute path to Claude Code's encoded-cwd directory name.
// All non-alphanumeric characters are replaced with "-".
func EncodeCwd(absPath string) string {
	var out []byte
	for _, r := range absPath {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, byte(r))
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

// ClaudeHome returns the path to ~/.claude.
func ClaudeHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}
