package sessionsync

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// CollisionDecision is the user's choice for one collision prompt.
type CollisionDecision int

const (
	DecisionOverwrite CollisionDecision = iota // replace destination contents
	DecisionSkip                                // skip just this session/memory
	DecisionCancelAll                           // abort the rest of the import
)

// CollisionResolver decides what to do for each collision. The TUI handler
// implements this by routing the prompt to the user. sessionID is empty when
// the collision is for memory/.
type CollisionResolver func(sessionID string, isMemory bool) CollisionDecision

// UnpackOptions describes one import operation.
type UnpackOptions struct {
	BundlePath  string            // absolute path to the .zip
	DestDir     string            // absolute path to ~/.claude/projects/<dest-encoded>/
	DestCwd     string            // user-provided destination project absolute path; replaces manifest.SourceCwd inside JSONL
	Selection   map[string]bool   // session IDs from manifest.Sessions to include; nil means "all"
	OnCollision CollisionResolver // required when collisions may occur
	Logger      *log.Logger       // may be nil
}

// UnpackResult tallies the outcome.
type UnpackResult struct {
	Manifest     *Manifest
	Written      int
	Skipped      int
	Cancelled    int    // sessions remaining when CancelAll was chosen
	MemoryStatus string // "written" | "skipped" | "not-included"
}

// LoadManifest opens a bundle zip just enough to read manifest.json.
// Used by the TUI to drive the manifest preview before the user picks
// a destination. Returns the parsed manifest or an error.
func LoadManifest(bundlePath string) (*Manifest, error) {
	r, err := zip.OpenReader(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("sessionsync: open zip %s: %w", bundlePath, err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("sessionsync: open manifest.json entry: %w", err)
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("sessionsync: read manifest.json: %w", err)
		}
		return UnmarshalManifest(data)
	}

	return nil, fmt.Errorf("sessionsync: manifest.json not found in %s", bundlePath)
}

// DetectCollisions returns the subset of sessionIDs whose
// "<destDir>/<id>.jsonl" already exists. Used by the TUI to know whether
// it must even ask the user before kicking off Unpack.
func DetectCollisions(destDir string, sessionIDs []string) []string {
	var collisions []string
	for _, id := range sessionIDs {
		dest := filepath.Join(destDir, id+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			collisions = append(collisions, id)
		}
	}
	return collisions
}

// Unpack extracts the bundle into opts.DestDir, rewriting cwd as it streams
// each session JSONL. Calls OnCollision when a destination file already
// exists. After CancelAll, writes nothing further but does not roll back
// already-written sessions. Always creates DestDir (MkdirAll 0o755) if missing.
func Unpack(opts UnpackOptions) (*UnpackResult, error) {
	if opts.Logger != nil {
		opts.Logger.Printf("sessionsync: unpack start bundle=%s dest=%s",
			opts.BundlePath, opts.DestDir)
	}

	// 1. Open zip.
	r, err := zip.OpenReader(opts.BundlePath)
	if err != nil {
		return nil, fmt.Errorf("sessionsync: open zip %s: %w", opts.BundlePath, err)
	}
	defer r.Close()

	// 2. Find and parse manifest.json.
	mf, err := findManifestInZip(r)
	if err != nil {
		return nil, err
	}

	// 3. Build sessionSet from opts.Selection (nil means all-true).
	sessionSet := opts.Selection
	if sessionSet == nil {
		sessionSet = make(map[string]bool, len(mf.Sessions))
		for _, id := range mf.Sessions {
			sessionSet[id] = true
		}
	}

	// Count total sessions in selection for Cancelled calculation.
	totalInSelection := 0
	for _, id := range mf.Sessions {
		if sessionSet[id] {
			totalInSelection++
		}
	}

	// Ensure DestDir exists.
	if err := os.MkdirAll(opts.DestDir, 0o755); err != nil {
		return nil, fmt.Errorf("sessionsync: create dest dir %s: %w", opts.DestDir, err)
	}

	result := &UnpackResult{
		Manifest:     mf,
		MemoryStatus: "not-included",
	}

	cancelled := false

	// 4. For each session ID in manifest.Sessions IN ORDER.
	for _, id := range mf.Sessions {
		if !sessionSet[id] {
			// Not in selection — does NOT count as Skipped.
			continue
		}

		if cancelled {
			break
		}

		destJSONL := filepath.Join(opts.DestDir, id+".jsonl")

		// 4c. Pre-flight collision check.
		if _, statErr := os.Stat(destJSONL); statErr == nil {
			// File exists — ask resolver.
			if opts.OnCollision != nil {
				decision := opts.OnCollision(id, false)
				switch decision {
				case DecisionSkip:
					result.Skipped++
					continue
				case DecisionCancelAll:
					// Cancelled = total in selection - Written - Skipped (including this session).
					// This session is not Written or Skipped, so it contributes to Cancelled.
					cancelled = true
					break
				case DecisionOverwrite:
					// Remove existing subagents dir if present so the bundle replaces it cleanly.
					subagentsDir := filepath.Join(opts.DestDir, id)
					if fi, sErr := os.Stat(subagentsDir); sErr == nil && fi.IsDir() {
						if rmErr := os.RemoveAll(subagentsDir); rmErr != nil {
							return nil, fmt.Errorf("sessionsync: remove existing subagents dir %s: %w", subagentsDir, rmErr)
						}
					}
				}
			}
		}

		if cancelled {
			break
		}

		// 4d. Write the JSONL with cwd rewriting.
		if err := writeRewrittenJSONL(r, id, destJSONL, mf.SourceCwd, opts.DestCwd, opts.Logger); err != nil {
			return nil, err
		}

		// 4e. Copy subagent entries verbatim.
		if err := copySubagentEntries(r, id, opts.DestDir); err != nil {
			return nil, err
		}

		// 4f. Increment Written.
		result.Written++
	}

	// Compute Cancelled: total in selection - Written - Skipped.
	// This includes the session that triggered CancelAll (it was not Written or Skipped).
	if cancelled {
		result.Cancelled = totalInSelection - result.Written - result.Skipped
	}

	// 5. Handle memory/ entries if IncludeMemory.
	if mf.IncludeMemory {
		memStatus, err := copyMemoryEntries(r, opts.DestDir, opts.OnCollision)
		if err != nil {
			return nil, err
		}
		result.MemoryStatus = memStatus
	}

	if opts.Logger != nil {
		opts.Logger.Printf("sessionsync: unpack done written=%d skipped=%d cancelled=%d memory=%s",
			result.Written, result.Skipped, result.Cancelled, result.MemoryStatus)
	}

	return result, nil
}

// findManifestInZip finds and parses manifest.json from an already-opened zip reader.
func findManifestInZip(r *zip.ReadCloser) (*Manifest, error) {
	for _, f := range r.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("sessionsync: open manifest.json in zip: %w", err)
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("sessionsync: read manifest.json in zip: %w", err)
		}
		return UnmarshalManifest(data)
	}
	return nil, fmt.Errorf("sessionsync: manifest.json not found in bundle")
}

// writeRewrittenJSONL reads <sessionID>.jsonl from the zip, rewrites cwd fields,
// and writes the result to destPath.
func writeRewrittenJSONL(r *zip.ReadCloser, sessionID, destPath, sourceCwd, destCwd string, logger *log.Logger) error {
	entryName := sessionID + ".jsonl"
	var entry *zip.File
	for _, f := range r.File {
		if f.Name == entryName {
			entry = f
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("sessionsync: zip entry %s not found", entryName)
	}

	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("sessionsync: open zip entry %s: %w", entryName, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("sessionsync: create dest file %s: %w", destPath, err)
	}
	defer out.Close()

	scanner := bufio.NewScanner(rc)
	buf := make([]byte, 64*1024*1024) // 64 MB buffer
	scanner.Buffer(buf, 64*1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := scanner.Bytes()

		rewritten, _, parseErr := rewriteCwdLine(raw, sourceCwd, destCwd)
		if parseErr != nil {
			// Malformed line: pass through verbatim, emit warning.
			if logger != nil {
				logger.Printf("sessionsync: unpack json-parse warn line=%d session=%s err=%v", lineNum, sessionID, parseErr)
			}
			rewritten = raw
		}

		if _, err := out.Write(rewritten); err != nil {
			return fmt.Errorf("sessionsync: write line to %s: %w", destPath, err)
		}
		if _, err := out.Write([]byte("\n")); err != nil {
			return fmt.Errorf("sessionsync: write newline to %s: %w", destPath, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("sessionsync: scan zip entry %s: %w", entryName, err)
	}

	return nil
}

// copySubagentEntries copies all zip entries with prefix "<sessionID>/subagents/"
// into <destDir>/<sessionID>/subagents/<rel> verbatim.
func copySubagentEntries(r *zip.ReadCloser, sessionID, destDir string) error {
	prefix := sessionID + "/subagents/"
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		if f.FileInfo().IsDir() {
			continue
		}

		// rel is the part after "<sessionID>/subagents/"
		rel := f.Name[len(prefix):]
		destPath := filepath.Join(destDir, sessionID, "subagents", filepath.FromSlash(rel))

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("sessionsync: create subagent dir %s: %w", filepath.Dir(destPath), err)
		}

		if err := copyZipEntryToFile(f, destPath); err != nil {
			return fmt.Errorf("sessionsync: copy subagent entry %s: %w", f.Name, err)
		}
	}
	return nil
}

// copyMemoryEntries copies all zip entries with prefix "memory/" into
// <destDir>/memory/<rel> verbatim. It handles collisions via the resolver.
// Returns the MemoryStatus string.
func copyMemoryEntries(r *zip.ReadCloser, destDir string, resolver CollisionResolver) (string, error) {
	// Collect memory entries first.
	var memEntries []*zip.File
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "memory/") && !f.FileInfo().IsDir() {
			memEntries = append(memEntries, f)
		}
	}

	if len(memEntries) == 0 {
		// IncludeMemory was true but no memory/ entries in zip — treat as written (empty).
		return "written", nil
	}

	// Pre-flight: check if dest/memory exists.
	memDestDir := filepath.Join(destDir, "memory")
	if _, statErr := os.Stat(memDestDir); statErr == nil {
		// Collision on memory dir.
		if resolver != nil {
			decision := resolver("", true)
			switch decision {
			case DecisionSkip:
				return "skipped", nil
			case DecisionCancelAll:
				// Cancelling at memory step — treat as skipped for MemoryStatus.
				return "skipped", nil
			case DecisionOverwrite:
				if rmErr := os.RemoveAll(memDestDir); rmErr != nil {
					return "", fmt.Errorf("sessionsync: remove existing memory dir %s: %w", memDestDir, rmErr)
				}
			}
		}
	}

	// Copy each memory/ entry.
	for _, f := range memEntries {
		rel := f.Name[len("memory/"):]
		destPath := filepath.Join(memDestDir, filepath.FromSlash(rel))

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return "", fmt.Errorf("sessionsync: create memory dir %s: %w", filepath.Dir(destPath), err)
		}

		if err := copyZipEntryToFile(f, destPath); err != nil {
			return "", fmt.Errorf("sessionsync: copy memory entry %s: %w", f.Name, err)
		}
	}

	return "written", nil
}

// copyZipEntryToFile opens a zip entry and writes its contents to destPath,
// creating or truncating the destination file.
func copyZipEntryToFile(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("copy to %s: %w", destPath, err)
	}
	return nil
}

// rewriteCwdLine returns a JSON line with cwd replaced when present + matching;
// returns the input verbatim (as is) when not applicable.
// The second return value indicates whether a rewrite was performed.
// An error is returned only for lines that cannot be JSON-parsed at all.
func rewriteCwdLine(line []byte, sourceCwd, destCwd string) ([]byte, bool, error) {
	if len(line) == 0 {
		return line, false, nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(line, &obj); err != nil {
		return nil, false, err
	}

	cwdVal, exists := obj["cwd"]
	if !exists {
		// No cwd key — return verbatim to preserve original key ordering.
		return line, false, nil
	}

	cwdStr, isStr := cwdVal.(string)
	if !isStr || cwdStr != sourceCwd {
		// cwd key exists but value doesn't match sourceCwd — return verbatim.
		return line, false, nil
	}

	// Replace cwd with destCwd and re-marshal.
	// Note: encoding/json marshals maps with keys sorted alphabetically,
	// so key order in the output may differ from the original. This is
	// acceptable per the spec which only requires the cwd field be replaced.
	obj["cwd"] = destCwd
	rewritten, err := json.Marshal(obj)
	if err != nil {
		return nil, false, fmt.Errorf("re-marshal after cwd rewrite: %w", err)
	}

	return rewritten, true, nil
}
