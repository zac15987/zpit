// Package sessionsync provides read-only helpers for scanning the
// ~/.claude/projects/ directory tree that Claude Code uses to store session
// JSONL logs.  The functions in this package perform filesystem I/O only;
// they do not modify any files and they do not hold any locks.
//
// Logging is intentionally omitted here: these are pure I/O helpers.
// The caller (typically a TUI handler) is responsible for logging entry,
// exit, and error events using the application logger.
package sessionsync

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FolderInfo summarises one encoded-cwd directory directly under
// ~/.claude/projects/.  Each directory name is the encoded form of a
// project's working directory (non-alphanumeric characters replaced with
// dashes by Claude Code).
type FolderInfo struct {
	// Name is the encoded folder name, e.g. "D--Documents-MyProjects-zpit".
	Name string

	// Path is the absolute filesystem path to the folder.
	Path string

	// SessionCount is the number of *.jsonl files found at the folder root
	// (non-recursive).
	SessionCount int

	// TotalBytes is the sum of the sizes (in bytes) of all *.jsonl files at
	// the folder root.  int64 is used to handle multi-GB folders safely.
	TotalBytes int64

	// LastModified is the newest mtime among the *.jsonl files in this
	// folder.  It is the zero value of time.Time when SessionCount == 0.
	LastModified time.Time
}

// SessionInfo summarises one session JSONL file inside an encoded-cwd folder.
type SessionInfo struct {
	// SessionID is the session UUID derived from the filename by stripping
	// the ".jsonl" extension.
	SessionID string

	// Path is the absolute filesystem path to the .jsonl file.
	Path string

	// Size is the file size in bytes.
	Size int64

	// LastModified is the mtime of the .jsonl file.
	LastModified time.Time

	// HasSubagents is true when a sibling directory named
	// "<session-id>/subagents/" exists alongside the .jsonl file.
	// Detection is a single os.Stat call — no recursive walk is performed.
	HasSubagents bool
}

// ProjectsRoot returns the canonical absolute path to ~/.claude/projects/.
// It resolves the user's home directory via os.UserHomeDir and appends the
// well-known sub-path that Claude Code uses.
func ProjectsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// ScanFolders lists every directory directly under projectsRoot and returns
// one FolderInfo per directory, sorted alphabetically by Name.
//
// Plain files at the top level of projectsRoot are silently skipped.
// Returns a nil slice and a non-nil error if projectsRoot cannot be read.
// An empty projectsRoot (no entries) returns an empty slice and nil error.
func ScanFolders(projectsRoot string) ([]FolderInfo, error) {
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		return nil, err
	}

	folders := make([]FolderInfo, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		info := buildFolderInfo(entry.Name(), filepath.Join(projectsRoot, entry.Name()))
		folders = append(folders, info)
	}

	sort.Slice(folders, func(i, j int) bool {
		return folders[i].Name < folders[j].Name
	})

	return folders, nil
}

// buildFolderInfo aggregates *.jsonl stats for a single folder.
// Non-jsonl entries and sub-directories are ignored (top-level only).
func buildFolderInfo(name, folderPath string) FolderInfo {
	info := FolderInfo{
		Name: name,
		Path: folderPath,
	}

	children, err := os.ReadDir(folderPath)
	if err != nil {
		// Return the partially-populated struct; caller sees zero counts.
		return info
	}

	for _, child := range children {
		if child.IsDir() {
			continue
		}
		if filepath.Ext(child.Name()) != ".jsonl" {
			continue
		}

		fi, err := child.Info()
		if err != nil {
			continue
		}

		info.SessionCount++
		info.TotalBytes += fi.Size()
		if fi.ModTime().After(info.LastModified) {
			info.LastModified = fi.ModTime()
		}
	}

	return info
}

// ScanSessions lists every "*.jsonl" file directly under folderPath and
// returns one SessionInfo per file, sorted alphabetically by SessionID.
//
// Files in sub-directories are not included.  Non-jsonl files are silently
// skipped.  For each session, a single os.Stat call checks whether a
// sibling directory named "<session-id>/subagents/" exists; HasSubagents is
// set to true only when that stat succeeds and the path is a directory.
//
// Returns a nil slice and a non-nil error if folderPath cannot be read.
// An empty folderPath returns an empty slice and nil error.
func ScanSessions(folderPath string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return nil, err
	}

	sessions := make([]SessionInfo, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}

		fi, err := entry.Info()
		if err != nil {
			continue
		}

		sessionID := entry.Name()[:len(entry.Name())-len(".jsonl")]
		sessionPath := filepath.Join(folderPath, entry.Name())

		sessions = append(sessions, SessionInfo{
			SessionID:    sessionID,
			Path:         sessionPath,
			Size:         fi.Size(),
			LastModified: fi.ModTime(),
			HasSubagents: hasSubagentsDir(folderPath, sessionID),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SessionID < sessions[j].SessionID
	})

	return sessions, nil
}

// hasSubagentsDir reports whether a directory at
// folderPath/<sessionID>/subagents/ exists.
// A single os.Stat call is used — no recursive walk.
func hasSubagentsDir(folderPath, sessionID string) bool {
	subagentsPath := filepath.Join(folderPath, sessionID, "subagents")
	fi, err := os.Stat(subagentsPath)
	if err != nil {
		return false
	}
	return fi.IsDir()
}
