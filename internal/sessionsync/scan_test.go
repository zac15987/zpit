package sessionsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFile creates a file at path with the given content.
func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// TestScanFolders_Empty verifies that an empty projectsRoot returns an empty
// slice and no error.
func TestScanFolders_Empty(t *testing.T) {
	root := t.TempDir()

	folders, err := ScanFolders(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(folders) != 0 {
		t.Fatalf("expected 0 folders, got %d", len(folders))
	}
}

// TestScanFolders_BasicLayout creates two project folders with known JSONL
// content and verifies that ScanFolders returns correct counts, sizes, and
// last-modified times sorted by Name.
func TestScanFolders_BasicLayout(t *testing.T) {
	root := t.TempDir()

	// folderA: two JSONL files with 100 and 200 bytes.
	folderA := filepath.Join(root, "folderA")
	writeFile(t, filepath.Join(folderA, "session1.jsonl"), make([]byte, 100))
	writeFile(t, filepath.Join(folderA, "session2.jsonl"), make([]byte, 200))

	// folderB: one JSONL file with 50 bytes.
	folderB := filepath.Join(root, "folderB")
	writeFile(t, filepath.Join(folderB, "session3.jsonl"), make([]byte, 50))

	folders, err := ScanFolders(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(folders) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(folders))
	}

	// Results must be sorted by Name: folderA < folderB.
	if folders[0].Name != "folderA" {
		t.Errorf("expected folders[0].Name = folderA, got %s", folders[0].Name)
	}
	if folders[1].Name != "folderB" {
		t.Errorf("expected folders[1].Name = folderB, got %s", folders[1].Name)
	}

	// folderA checks.
	a := folders[0]
	if a.SessionCount != 2 {
		t.Errorf("folderA: expected SessionCount 2, got %d", a.SessionCount)
	}
	if a.TotalBytes != 300 {
		t.Errorf("folderA: expected TotalBytes 300, got %d", a.TotalBytes)
	}
	if a.LastModified.IsZero() {
		t.Error("folderA: LastModified should not be zero")
	}
	if a.Path != folderA {
		t.Errorf("folderA: expected Path %s, got %s", folderA, a.Path)
	}

	// folderB checks.
	b := folders[1]
	if b.SessionCount != 1 {
		t.Errorf("folderB: expected SessionCount 1, got %d", b.SessionCount)
	}
	if b.TotalBytes != 50 {
		t.Errorf("folderB: expected TotalBytes 50, got %d", b.TotalBytes)
	}
	if b.LastModified.IsZero() {
		t.Error("folderB: LastModified should not be zero")
	}
}

// TestScanFolders_IgnoresNonDirs drops a plain file at the projectsRoot level
// and verifies it is not included in the results.
func TestScanFolders_IgnoresNonDirs(t *testing.T) {
	root := t.TempDir()

	// A real folder with one JSONL.
	folderA := filepath.Join(root, "folderA")
	writeFile(t, filepath.Join(folderA, "s.jsonl"), make([]byte, 10))

	// A plain file at root level — should be ignored.
	writeFile(t, filepath.Join(root, "stray.jsonl"), make([]byte, 5))

	folders, err := ScanFolders(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(folders))
	}
	if folders[0].Name != "folderA" {
		t.Errorf("expected folderA, got %s", folders[0].Name)
	}
}

// TestScanSessions_DetectsSubagents creates two session files.  One has a
// sibling <session-id>/subagents/ directory; the other does not.
func TestScanSessions_DetectsSubagents(t *testing.T) {
	folder := t.TempDir()

	// sess1.jsonl with a subagents sibling dir.
	writeFile(t, filepath.Join(folder, "sess1.jsonl"), make([]byte, 20))
	writeFile(t, filepath.Join(folder, "sess1", "subagents", "x.jsonl"), make([]byte, 5))

	// sess2.jsonl without subagents dir.
	writeFile(t, filepath.Join(folder, "sess2.jsonl"), make([]byte, 15))

	sessions, err := ScanSessions(folder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Sorted by SessionID: sess1 < sess2.
	if sessions[0].SessionID != "sess1" {
		t.Errorf("expected sessions[0].SessionID = sess1, got %s", sessions[0].SessionID)
	}
	if !sessions[0].HasSubagents {
		t.Error("sess1: expected HasSubagents = true")
	}

	if sessions[1].SessionID != "sess2" {
		t.Errorf("expected sessions[1].SessionID = sess2, got %s", sessions[1].SessionID)
	}
	if sessions[1].HasSubagents {
		t.Error("sess2: expected HasSubagents = false")
	}
}

// TestScanSessions_OnlyTopLevelJsonl verifies that JSONL files nested inside
// sub-directories of folderPath are not returned as sessions.
func TestScanSessions_OnlyTopLevelJsonl(t *testing.T) {
	folder := t.TempDir()

	// A top-level JSONL.
	writeFile(t, filepath.Join(folder, "top.jsonl"), make([]byte, 10))

	// A JSONL nested in a sub-directory — must be excluded.
	writeFile(t, filepath.Join(folder, "subdir", "nested.jsonl"), make([]byte, 10))

	sessions, err := ScanSessions(folder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "top" {
		t.Errorf("expected SessionID top, got %s", sessions[0].SessionID)
	}
}

// TestScanSessions_Empty verifies that an empty folder returns an empty slice
// and no error.
func TestScanSessions_Empty(t *testing.T) {
	folder := t.TempDir()

	sessions, err := ScanSessions(folder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

// TestScanSessions_SkipsNonJsonl drops .json and .txt files alongside a
// valid .jsonl; only the .jsonl should appear.
func TestScanSessions_SkipsNonJsonl(t *testing.T) {
	folder := t.TempDir()

	writeFile(t, filepath.Join(folder, "valid.jsonl"), make([]byte, 10))
	writeFile(t, filepath.Join(folder, "noext.json"), make([]byte, 10))
	writeFile(t, filepath.Join(folder, "notes.txt"), make([]byte, 10))

	sessions, err := ScanSessions(folder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "valid" {
		t.Errorf("expected SessionID valid, got %s", sessions[0].SessionID)
	}
}

// TestScanFolders_LastModifiedPicksNewest ensures LastModified reflects the
// most recently modified JSONL, not just the first one encountered.
func TestScanFolders_LastModifiedPicksNewest(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "proj")

	early := filepath.Join(folder, "early.jsonl")
	late := filepath.Join(folder, "late.jsonl")
	writeFile(t, early, make([]byte, 10))
	writeFile(t, late, make([]byte, 10))

	// Force a detectable mtime difference.
	earlyTime := time.Now().Add(-10 * time.Minute)
	lateTime := time.Now()
	if err := os.Chtimes(early, earlyTime, earlyTime); err != nil {
		t.Fatalf("Chtimes early: %v", err)
	}
	if err := os.Chtimes(late, lateTime, lateTime); err != nil {
		t.Fatalf("Chtimes late: %v", err)
	}

	folders, err := ScanFolders(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(folders))
	}

	// LastModified must be at least as recent as lateTime (within 1s tolerance
	// for filesystem mtime rounding).
	if folders[0].LastModified.Before(lateTime.Add(-time.Second)) {
		t.Errorf("LastModified %v is earlier than expected %v", folders[0].LastModified, lateTime)
	}
}
