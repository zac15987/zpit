// Package sessionsync provides helpers for exporting and importing Claude Code
// session JSONL bundles between machines. A bundle is a zip archive containing
// one or more JSONL session files together with a manifest.json at the archive
// root that describes the source environment so that the importer can rewrite
// OS-specific paths on arrival.
//
// This file is intentionally stateless — it contains only pure data types and
// serialisation helpers. No global logger is held here; callers are expected
// to log entry/exit around calls as appropriate.
package sessionsync

import (
	"encoding/json"
	"runtime"
)

// FormatVersion is the current bundle manifest format version. Increment this
// constant when breaking changes are made to the manifest schema so importers
// can reject bundles they cannot safely handle.
const FormatVersion = "1"

// Manifest describes a session bundle. It is persisted as manifest.json at the
// root of the zip archive and must be the first entry read by the importer.
//
// All fields use lower-snake-case JSON keys so that the file is readable on
// every platform without locale issues.
type Manifest struct {
	// FormatVersion identifies the schema revision. Currently always "1".
	FormatVersion string `json:"format_version"`

	// SourceOS is the GOOS value of the machine that created the bundle.
	// Expected values are "windows", "linux", or "darwin".
	SourceOS string `json:"source_os"`

	// SourceCwd is the absolute filesystem path of the source project on the
	// originating machine (e.g. "C:\Users\alice\projects\myapp" on Windows or
	// "/home/alice/projects/myapp" on Linux).
	SourceCwd string `json:"source_cwd"`

	// SourceEncodedCwd is the directory name that Claude Code derived from
	// SourceCwd by replacing non-alphanumeric characters with hyphens. It
	// matches the folder name under ~/.claude/projects/ on the source machine
	// and is used by the importer to locate the correct JSONL files.
	SourceEncodedCwd string `json:"source_encoded_cwd"`

	// Sessions lists the session ID strings included in the bundle, in the
	// order the user selected them. Each entry corresponds to a
	// <session-id>.jsonl file inside the archive.
	Sessions []string `json:"sessions"`

	// ExportedAt is the RFC3339 timestamp recording when the bundle was created.
	ExportedAt string `json:"exported_at"`

	// IncludeMemory is true when the memory/ subtree from the source Claude
	// project directory was also included in the archive.
	IncludeMemory bool `json:"include_memory"`
}

// MarshalManifest serialises m to indented JSON bytes using two-space
// indentation. The result is suitable for writing directly to manifest.json
// inside the bundle archive.
//
// Callers should log before invoking this function if entry/exit tracing is
// desired; no logging is performed here.
func MarshalManifest(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// UnmarshalManifest parses JSON bytes read from manifest.json into a new
// Manifest value. It returns an error for malformed JSON or if data is nil.
//
// Callers should validate m.FormatVersion after a successful parse and reject
// bundles whose version they do not recognise.
func UnmarshalManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// DetectSourceOS returns a normalised operating-system identifier for the
// current runtime:
//
//   - "windows" on Microsoft Windows
//   - "linux"   on Linux
//   - "darwin"  on macOS
//
// For any other platform the raw runtime.GOOS value is returned verbatim so
// that future platforms are represented rather than silently misclassified.
func DetectSourceOS() string {
	switch runtime.GOOS {
	case "windows", "linux", "darwin":
		return runtime.GOOS
	default:
		return runtime.GOOS
	}
}
