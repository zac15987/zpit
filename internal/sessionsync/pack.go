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
	"time"
)

// PackOptions describes one export operation.
type PackOptions struct {
	// SourceFolder is the absolute path to ~/.claude/projects/<encoded-cwd>/.
	SourceFolder string

	// SourceEncodedCwd is the encoded folder name itself (basename of SourceFolder).
	SourceEncodedCwd string

	// SessionIDs lists session IDs (without ".jsonl" suffix) in selection order.
	SessionIDs []string

	// IncludeMemory controls whether the memory/ subtree is copied into the bundle.
	IncludeMemory bool

	// OutputZipPath is the absolute path of the zip to write.
	OutputZipPath string

	// Logger may be nil; entry/exit logs are printed when non-nil.
	Logger *log.Logger
}

// PackResult summarises a successful pack operation.
type PackResult struct {
	BytesWritten int64
	Manifest     *Manifest
}

// Pack creates the zip bundle at opts.OutputZipPath. It returns the manifest
// embedded in the archive plus the on-disk size of the resulting zip.
//
// Returns an error if any source session JSONL is unreadable, the zip cannot
// be created, or memory/ is requested but cannot be walked. Subagent
// directories that do not exist or are not directories are silently skipped
// (a session may have no subagents/).
//
// SourceCwd in the manifest is taken from the first JSONL line in any session
// that contains a "cwd" string field. If no such line is found across all
// selected sessions, reverseDeriveSourceCwd(opts.SourceEncodedCwd) is used as
// a lossy fallback.
func Pack(opts PackOptions) (*PackResult, error) {
	if opts.Logger != nil {
		opts.Logger.Printf("sessionsync: pack start sessions=%d memory=%v output=%s",
			len(opts.SessionIDs), opts.IncludeMemory, opts.OutputZipPath)
	}

	// 1. Create output file (parent dir created if needed).
	if err := os.MkdirAll(filepath.Dir(opts.OutputZipPath), 0o755); err != nil {
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: create output dir: %w", err)
	}

	outFile, err := os.Create(opts.OutputZipPath)
	if err != nil {
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: create output zip: %w", err)
	}
	// We close outFile explicitly at the end; on error paths we close immediately.

	// 2. Wrap with zip writer.
	zw := zip.NewWriter(outFile)

	// manifestSourceCwd will be populated from the first JSONL line containing "cwd".
	var manifestSourceCwd string

	// 3. For each session ID: stream JSONL and optional subagents tree.
	for _, id := range opts.SessionIDs {
		jsonlPath := filepath.Join(opts.SourceFolder, id+".jsonl")

		cwd, err := packSession(zw, id, jsonlPath)
		if err != nil {
			_ = zw.Close()
			_ = outFile.Close()
			if opts.Logger != nil {
				opts.Logger.Printf("sessionsync: pack error: %v", err)
			}
			return nil, fmt.Errorf("sessionsync: pack session %s: %w", id, err)
		}

		// Capture first cwd encountered across all sessions.
		if manifestSourceCwd == "" && cwd != "" {
			manifestSourceCwd = cwd
		}

		// 3d. Optionally walk subagents tree.
		subagentsDir := filepath.Join(opts.SourceFolder, id, "subagents")
		if fi, statErr := os.Stat(subagentsDir); statErr == nil && fi.IsDir() {
			if err := packSubagents(zw, subagentsDir, id); err != nil {
				_ = zw.Close()
				_ = outFile.Close()
				if opts.Logger != nil {
					opts.Logger.Printf("sessionsync: pack error: %v", err)
				}
				return nil, fmt.Errorf("sessionsync: pack subagents for %s: %w", id, err)
			}
		}
		// If subagentsDir does not exist or is not a dir, silently skip.
	}

	// 4. Optionally walk memory/ subtree.
	if opts.IncludeMemory {
		memoryDir := filepath.Join(opts.SourceFolder, "memory")
		if err := packMemory(zw, memoryDir); err != nil {
			_ = zw.Close()
			_ = outFile.Close()
			if opts.Logger != nil {
				opts.Logger.Printf("sessionsync: pack error: %v", err)
			}
			return nil, fmt.Errorf("sessionsync: pack memory: %w", err)
		}
	}

	// 5. Build the Manifest.
	if manifestSourceCwd == "" {
		manifestSourceCwd = reverseDeriveSourceCwd(opts.SourceEncodedCwd)
	}

	mf := &Manifest{
		FormatVersion:    FormatVersion,
		SourceOS:         DetectSourceOS(),
		SourceCwd:        manifestSourceCwd,
		SourceEncodedCwd: opts.SourceEncodedCwd,
		Sessions:         append([]string(nil), opts.SessionIDs...),
		ExportedAt:       time.Now().UTC().Format(time.RFC3339),
		IncludeMemory:    opts.IncludeMemory,
	}

	// 6. Write manifest.json into the zip.
	manifestBytes, err := MarshalManifest(mf)
	if err != nil {
		_ = zw.Close()
		_ = outFile.Close()
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: marshal manifest: %w", err)
	}

	w, err := zw.Create("manifest.json")
	if err != nil {
		_ = zw.Close()
		_ = outFile.Close()
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: create manifest.json zip entry: %w", err)
	}
	if _, err := w.Write(manifestBytes); err != nil {
		_ = zw.Close()
		_ = outFile.Close()
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: write manifest.json: %w", err)
	}

	// 7. Close zip writer then output file, then stat final size.
	if err := zw.Close(); err != nil {
		_ = outFile.Close()
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: close zip writer: %w", err)
	}
	if err := outFile.Close(); err != nil {
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: close output file: %w", err)
	}

	fi, err := os.Stat(opts.OutputZipPath)
	if err != nil {
		if opts.Logger != nil {
			opts.Logger.Printf("sessionsync: pack error: %v", err)
		}
		return nil, fmt.Errorf("sessionsync: stat output zip: %w", err)
	}

	if opts.Logger != nil {
		opts.Logger.Printf("sessionsync: pack done sessions=%d bytes=%d path=%s",
			len(opts.SessionIDs), fi.Size(), opts.OutputZipPath)
	}

	return &PackResult{
		BytesWritten: fi.Size(),
		Manifest:     mf,
	}, nil
}

// packSession streams the JSONL file for sessionID into the zip as
// "<sessionID>.jsonl". While streaming it probes the first line that contains
// a JSON "cwd" field and returns it. If no such line exists, cwd is "".
// Returns an error if the source file cannot be opened or the zip entry cannot
// be written.
func packSession(zw *zip.Writer, sessionID, jsonlPath string) (cwd string, err error) {
	src, err := os.Open(jsonlPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", jsonlPath, err)
	}
	defer src.Close()

	w, err := zw.Create(sessionID + ".jsonl")
	if err != nil {
		return "", fmt.Errorf("create zip entry %s.jsonl: %w", sessionID, err)
	}

	// Stream line-by-line using a buffered scanner with a 64 MB per-line limit.
	// A tee reader feeds bytes to the zip writer while the scanner reads lines
	// for cwd extraction, so we traverse the file exactly once.
	//
	// However, bufio.Scanner does not expose an io.Reader interface we can tee
	// from. Instead we use a two-pass conceptual design implemented as a single
	// pass: read each line, write raw bytes + newline to the zip, and probe cwd
	// on the first hit.
	//
	// We reopen the file for the zip write (below via io.Copy) approach, but
	// that would require two passes. Instead we use a pipe approach: scan each
	// raw line, write it directly, and probe.

	// We need the raw file bytes to be faithful (preserve trailing whitespace,
	// exact line endings). bufio.Scanner.Bytes() returns the line content without
	// the terminator, so we need to append it back. We use "\n" because Claude
	// Code writes JSONL with LF line endings on all platforms.
	//
	// To avoid loading the full file: scan line by line, write line+"\n" to zip.

	// Reset src to beginning (it was just opened, so it is at offset 0 already).
	scanner := bufio.NewScanner(src)
	buf := make([]byte, 64*1024*1024) // 64 MB buffer
	scanner.Buffer(buf, 64*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Write line + newline to zip entry.
		if _, writeErr := w.Write(line); writeErr != nil {
			return cwd, fmt.Errorf("write zip entry %s.jsonl: %w", sessionID, writeErr)
		}
		if _, writeErr := w.Write([]byte("\n")); writeErr != nil {
			return cwd, fmt.Errorf("write zip entry %s.jsonl newline: %w", sessionID, writeErr)
		}

		// Probe cwd from the first line that has it.
		if cwd == "" {
			if extracted, ok := extractCwdFromJSONLine(line); ok {
				cwd = extracted
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return cwd, fmt.Errorf("scan %s: %w", jsonlPath, err)
	}

	return cwd, nil
}

// packSubagents walks subagentsDir recursively and adds every regular file as
// a zip entry "<sessionID>/subagents/<rel-path>".
func packSubagents(zw *zip.Writer, subagentsDir, sessionID string) error {
	return filepath.WalkDir(subagentsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			// Skip symlinks — only regular files.
			return nil
		}

		rel, err := filepath.Rel(subagentsDir, path)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", path, err)
		}
		// Use forward slashes in zip entry names (zip spec requires /; on Windows
		// filepath.Rel uses backslashes).
		entryName := sessionID + "/subagents/" + filepath.ToSlash(rel)

		if err := addFileToZip(zw, entryName, path); err != nil {
			return err
		}
		return nil
	})
}

// packMemory walks memoryDir recursively and adds every regular file as a zip
// entry "memory/<rel-path>". Returns an error if memoryDir cannot be walked.
func packMemory(zw *zip.Writer, memoryDir string) error {
	return filepath.WalkDir(memoryDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		rel, err := filepath.Rel(memoryDir, path)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", path, err)
		}
		entryName := "memory/" + filepath.ToSlash(rel)

		if err := addFileToZip(zw, entryName, path); err != nil {
			return err
		}
		return nil
	})
}

// addFileToZip creates a new zip entry named entryName and streams the contents
// of srcPath into it.
func addFileToZip(zw *zip.Writer, entryName, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer src.Close()

	w, err := zw.Create(entryName)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", entryName, err)
	}

	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", srcPath, entryName, err)
	}
	return nil
}

// extractCwdFromJSONLine parses one line as a JSON object and returns the
// "cwd" string field if present; ok=false if absent or parse fails.
func extractCwdFromJSONLine(line []byte) (cwd string, ok bool) {
	if len(line) == 0 {
		return "", false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		return "", false
	}
	raw, exists := obj["cwd"]
	if !exists {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	if s == "" {
		return "", false
	}
	return s, true
}

// reverseDeriveSourceCwd is the lossy fallback for when no JSONL line contained
// a "cwd" field. It replaces every "-" in the encoded name with "/" and prepends
// "/" if the result does not already start with one.
func reverseDeriveSourceCwd(encoded string) string {
	derived := strings.ReplaceAll(encoded, "-", "/")
	if !strings.HasPrefix(derived, "/") {
		derived = "/" + derived
	}
	return derived
}
