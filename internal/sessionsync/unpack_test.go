package sessionsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zac15987/zpit/internal/watcher"
)

// buildBundle is a helper that packs a synthetic source tree and returns the
// path to the resulting zip. Sessions is a map of sessionID -> JSONL content
// (each content string is written as-is to the .jsonl file). memoryFiles is a
// map of relative path -> content for the memory/ subtree; pass nil to skip
// memory. sourceCwd and sourceEncodedCwd describe the source project location.
func buildBundle(t *testing.T, sourceCwd, sourceEncodedCwd string, sessions map[string]string, memoryFiles map[string]string) string {
	t.Helper()

	src := t.TempDir()

	var sessionIDs []string
	for id, content := range sessions {
		writeFile(t, filepath.Join(src, id+".jsonl"), []byte(content))
		sessionIDs = append(sessionIDs, id)
	}

	includeMemory := false
	if memoryFiles != nil {
		includeMemory = true
		for rel, content := range memoryFiles {
			writeFile(t, filepath.Join(src, "memory", rel), []byte(content))
		}
	}

	outZip := filepath.Join(t.TempDir(), "bundle.zip")

	_, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: sourceEncodedCwd,
		SessionIDs:       sessionIDs,
		IncludeMemory:    includeMemory,
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("buildBundle Pack: %v", err)
	}

	return outZip
}

// buildBundleOrdered is like buildBundle but accepts sessions as an ordered
// slice of [id, content] pairs to preserve manifest session ordering.
func buildBundleOrdered(t *testing.T, sourceCwd, sourceEncodedCwd string, sessions [][2]string, memoryFiles map[string]string) string {
	t.Helper()

	src := t.TempDir()

	sessionIDs := make([]string, 0, len(sessions))
	for _, pair := range sessions {
		id, content := pair[0], pair[1]
		writeFile(t, filepath.Join(src, id+".jsonl"), []byte(content))
		sessionIDs = append(sessionIDs, id)
	}

	includeMemory := false
	if memoryFiles != nil {
		includeMemory = true
		for rel, content := range memoryFiles {
			writeFile(t, filepath.Join(src, "memory", rel), []byte(content))
		}
	}

	outZip := filepath.Join(t.TempDir(), "bundle.zip")

	_, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: sourceEncodedCwd,
		SessionIDs:       sessionIDs,
		IncludeMemory:    includeMemory,
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("buildBundleOrdered Pack: %v", err)
	}

	return outZip
}

// TestLoadManifest creates a small bundle via Pack and verifies LoadManifest
// returns correct manifest fields.
func TestLoadManifest(t *testing.T) {
	sourceCwd := `D:\Documents\foo`
	encoded := "D--Documents-foo"

	bundlePath := buildBundle(t, sourceCwd, encoded, map[string]string{
		"sess-1": `{"type":"summary","cwd":"D:\\Documents\\foo"}` + "\n",
	}, nil)

	mf, err := LoadManifest(bundlePath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if mf.SourceCwd != sourceCwd {
		t.Errorf("SourceCwd: want %q, got %q", sourceCwd, mf.SourceCwd)
	}
	if mf.SourceEncodedCwd != encoded {
		t.Errorf("SourceEncodedCwd: want %q, got %q", encoded, mf.SourceEncodedCwd)
	}
	if mf.FormatVersion != FormatVersion {
		t.Errorf("FormatVersion: want %q, got %q", FormatVersion, mf.FormatVersion)
	}
	if mf.IncludeMemory {
		t.Error("IncludeMemory: expected false")
	}
	if len(mf.Sessions) == 0 {
		t.Error("Sessions: expected at least one session")
	}
}

// TestDetectCollisions creates some <id>.jsonl files in a dest dir and verifies
// DetectCollisions returns only those that exist.
func TestDetectCollisions(t *testing.T) {
	dest := t.TempDir()

	writeFile(t, filepath.Join(dest, "sess-A.jsonl"), []byte("existing"))
	writeFile(t, filepath.Join(dest, "sess-C.jsonl"), []byte("existing"))

	collisions := DetectCollisions(dest, []string{"sess-A", "sess-B", "sess-C", "sess-D"})

	if len(collisions) != 2 {
		t.Fatalf("expected 2 collisions, got %d: %v", len(collisions), collisions)
	}

	found := map[string]bool{}
	for _, id := range collisions {
		found[id] = true
	}
	if !found["sess-A"] {
		t.Error("expected sess-A in collisions")
	}
	if !found["sess-C"] {
		t.Error("expected sess-C in collisions")
	}
	if found["sess-B"] || found["sess-D"] {
		t.Error("sess-B and sess-D should not be in collisions")
	}
}

// TestUnpack_CwdRewriteWindowsToLinux packs a session with three JSONL lines:
// - Line 1 has cwd == sourceCwd (should be rewritten)
// - Line 2 has no cwd field (should be untouched)
// - Line 3 has cwd with a different value (should be untouched)
func TestUnpack_CwdRewriteWindowsToLinux(t *testing.T) {
	sourceCwd := `D:\Documents\foo`
	destCwd := "/home/x/foo"

	line1 := `{"type":"summary","cwd":"D:\\Documents\\foo","gitBranch":"dev"}`
	line2 := `{"type":"user","content":"path D:\\Documents\\foo not in cwd"}`
	line3 := `{"type":"assistant","cwd":"different-path"}`

	bundlePath := buildBundleOrdered(t, sourceCwd, "D--Documents-foo", [][2]string{
		{"sess-1", line1 + "\n" + line2 + "\n" + line3 + "\n"},
	}, nil)

	dest := t.TempDir()

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    destCwd,
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if result.Written != 1 {
		t.Errorf("Written: want 1, got %d", result.Written)
	}

	// Read and parse the resulting JSONL.
	outData, err := os.ReadFile(filepath.Join(dest, "sess-1.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(outData), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	// Line 1: cwd should be rewritten; gitBranch must be preserved.
	var obj1 map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &obj1); err != nil {
		t.Fatalf("parse line 1: %v", err)
	}
	if obj1["cwd"] != destCwd {
		t.Errorf("line 1 cwd: want %q, got %q", destCwd, obj1["cwd"])
	}
	if obj1["gitBranch"] != "dev" {
		t.Errorf("line 1 gitBranch: want \"dev\", got %v", obj1["gitBranch"])
	}

	// Line 2: content must be unchanged (still contains the literal Windows path string).
	var obj2 map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &obj2); err != nil {
		t.Fatalf("parse line 2: %v", err)
	}
	if _, hasCwd := obj2["cwd"]; hasCwd {
		t.Error("line 2: should have no cwd field")
	}
	if !strings.Contains(lines[1], `D:\\Documents\\foo`) {
		t.Errorf("line 2: expected literal Windows path preserved, got: %s", lines[1])
	}

	// Line 3: cwd value is "different-path", not sourceCwd — must be untouched.
	var obj3 map[string]interface{}
	if err := json.Unmarshal([]byte(lines[2]), &obj3); err != nil {
		t.Fatalf("parse line 3: %v", err)
	}
	if obj3["cwd"] != "different-path" {
		t.Errorf("line 3 cwd: want \"different-path\", got %v", obj3["cwd"])
	}
}

// TestUnpack_CrossOSRoundTrip packs a Windows-cwd bundle and unpacks it with a
// Linux-style DestCwd. Every output line must parse via watcher.ParseLine
// without error.
func TestUnpack_CrossOSRoundTrip(t *testing.T) {
	sourceCwd := `D:\Documents\proj`

	// Provide valid JSONL lines that ParseLine can handle.
	jsonlContent := strings.Join([]string{
		`{"type":"summary","cwd":"D:\\Documents\\proj","gitBranch":"main"}`,
		`{"type":"user","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":"hi","stop_reason":"end_turn"}}`,
	}, "\n") + "\n"

	bundlePath := buildBundleOrdered(t, sourceCwd, "D--Documents-proj", [][2]string{
		{"sess-rt", jsonlContent},
	}, nil)

	dest := t.TempDir()

	_, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/home/user/proj",
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	outData, err := os.ReadFile(filepath.Join(dest, "sess-rt.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	for i, line := range strings.Split(strings.TrimRight(string(outData), "\n"), "\n") {
		if line == "" {
			continue
		}
		if _, parseErr := watcher.ParseLine([]byte(line)); parseErr != nil {
			t.Errorf("line %d failed watcher.ParseLine: %v\nline: %s", i+1, parseErr, line)
		}
	}
}

// TestUnpack_MalformedLinePassthrough verifies that a malformed JSONL line is
// passed through verbatim to the destination, the preceding valid line is
// rewritten correctly, and Unpack returns no error.
func TestUnpack_MalformedLinePassthrough(t *testing.T) {
	sourceCwd := "/src/project"

	line1 := `{"type":"summary","cwd":"/src/project"}`
	line2 := `not valid json {{`

	bundlePath := buildBundleOrdered(t, sourceCwd, "-src-project", [][2]string{
		{"sess-mal", line1 + "\n" + line2 + "\n"},
	}, nil)

	dest := t.TempDir()

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/dst/project",
	})
	if err != nil {
		t.Fatalf("Unpack returned error: %v", err)
	}
	if result.Written != 1 {
		t.Errorf("Written: want 1, got %d", result.Written)
	}

	outData, err := os.ReadFile(filepath.Join(dest, "sess-mal.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(outData), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}

	// Line 1: cwd should be rewritten.
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &obj); err != nil {
		t.Fatalf("parse line 1: %v", err)
	}
	if obj["cwd"] != "/dst/project" {
		t.Errorf("line 1 cwd: want \"/dst/project\", got %v", obj["cwd"])
	}

	// Line 2: malformed line appears verbatim.
	if lines[1] != line2 {
		t.Errorf("line 2: want verbatim %q, got %q", line2, lines[1])
	}
}

// TestUnpack_CollisionSkip verifies that when OnCollision returns DecisionSkip,
// the pre-existing dest file is left unchanged and Written=0, Skipped=1.
func TestUnpack_CollisionSkip(t *testing.T) {
	sourceCwd := "/src/a"

	bundlePath := buildBundleOrdered(t, sourceCwd, "-src-a", [][2]string{
		{"sess-A", `{"type":"summary","cwd":"/src/a"}` + "\n"},
	}, nil)

	dest := t.TempDir()

	// Pre-create dest file.
	writeFile(t, filepath.Join(dest, "sess-A.jsonl"), []byte("PRE"))

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/dst/a",
		OnCollision: func(sessionID string, isMemory bool) CollisionDecision {
			return DecisionSkip
		},
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	if result.Written != 0 {
		t.Errorf("Written: want 0, got %d", result.Written)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped: want 1, got %d", result.Skipped)
	}

	// dest file content must be unchanged.
	content, err := os.ReadFile(filepath.Join(dest, "sess-A.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "PRE" {
		t.Errorf("dest file content: want \"PRE\", got %q", string(content))
	}
}

// TestUnpack_CollisionOverwrite verifies that DecisionOverwrite replaces the
// dest file and removes any pre-existing subagents dir.
func TestUnpack_CollisionOverwrite(t *testing.T) {
	sourceCwd := "/src/b"

	bundlePath := buildBundleOrdered(t, sourceCwd, "-src-b", [][2]string{
		{"sess-A", `{"type":"summary","cwd":"/src/b"}` + "\n"},
	}, nil)

	dest := t.TempDir()

	// Pre-create dest file and an old subagents file.
	writeFile(t, filepath.Join(dest, "sess-A.jsonl"), []byte("PRE"))
	writeFile(t, filepath.Join(dest, "sess-A", "subagents", "old.jsonl"), []byte("OLD"))

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/dst/b",
		OnCollision: func(sessionID string, isMemory bool) CollisionDecision {
			return DecisionOverwrite
		},
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	if result.Written != 1 {
		t.Errorf("Written: want 1, got %d", result.Written)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped: want 0, got %d", result.Skipped)
	}

	// dest file should be replaced (not "PRE" anymore).
	content, err := os.ReadFile(filepath.Join(dest, "sess-A.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile dest: %v", err)
	}
	if string(content) == "PRE" {
		t.Error("dest file content should have been replaced, still says PRE")
	}

	// old subagents/old.jsonl should be removed (the dir was wiped by DecisionOverwrite).
	oldPath := filepath.Join(dest, "sess-A", "subagents", "old.jsonl")
	if _, statErr := os.Stat(oldPath); statErr == nil {
		t.Errorf("old subagent file %s should have been removed", oldPath)
	}
}

// TestUnpack_CollisionCancelAll verifies that DecisionCancelAll stops
// processing and the Cancelled count equals total_in_selection - Written - Skipped.
// Bundle has 3 sessions [A, B, C]. Pre-create dest/A.jsonl. Resolver returns
// CancelAll on A. Expected: Written=0, Skipped=0, Cancelled=3.
func TestUnpack_CollisionCancelAll(t *testing.T) {
	sourceCwd := "/src/c"

	bundlePath := buildBundleOrdered(t, sourceCwd, "-src-c", [][2]string{
		{"sess-A", `{"type":"summary","cwd":"/src/c"}` + "\n"},
		{"sess-B", `{"type":"user"}` + "\n"},
		{"sess-C", `{"type":"user"}` + "\n"},
	}, nil)

	dest := t.TempDir()

	// Pre-create dest/A.jsonl to trigger collision on A.
	writeFile(t, filepath.Join(dest, "sess-A.jsonl"), []byte("EXISTING"))

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/dst/c",
		OnCollision: func(sessionID string, isMemory bool) CollisionDecision {
			return DecisionCancelAll
		},
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	if result.Written != 0 {
		t.Errorf("Written: want 0, got %d", result.Written)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped: want 0, got %d", result.Skipped)
	}
	// Cancelled = total_in_selection - Written - Skipped = 3 - 0 - 0 = 3
	if result.Cancelled != 3 {
		t.Errorf("Cancelled: want 3, got %d", result.Cancelled)
	}

	// The pre-existing sess-A.jsonl must remain untouched.
	content, err := os.ReadFile(filepath.Join(dest, "sess-A.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile sess-A: %v", err)
	}
	if string(content) != "EXISTING" {
		t.Errorf("sess-A.jsonl: want \"EXISTING\", got %q", string(content))
	}
}

// TestUnpack_MemoryNotIncluded verifies that MemoryStatus == "not-included"
// when the bundle was packed without memory/ entries.
func TestUnpack_MemoryNotIncluded(t *testing.T) {
	bundlePath := buildBundleOrdered(t, "/src/proj", "-src-proj", [][2]string{
		{"sess-1", `{"type":"user"}` + "\n"},
	}, nil) // nil memoryFiles -> IncludeMemory=false

	dest := t.TempDir()

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/dst/proj",
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	if result.MemoryStatus != "not-included" {
		t.Errorf("MemoryStatus: want \"not-included\", got %q", result.MemoryStatus)
	}
}

// TestUnpack_MemoryCollisionSkip verifies that when the resolver returns
// DecisionSkip for a memory collision, the existing memory dir is preserved
// and MemoryStatus == "skipped".
func TestUnpack_MemoryCollisionSkip(t *testing.T) {
	bundlePath := buildBundleOrdered(t, "/src/proj", "-src-proj", [][2]string{
		{"sess-1", `{"type":"user"}` + "\n"},
	}, map[string]string{
		"MEMORY.md": "# Bundle Memory\n",
	})

	dest := t.TempDir()

	// Pre-create dest/memory/old.md to trigger a collision.
	writeFile(t, filepath.Join(dest, "memory", "old.md"), []byte("OLD MEMORY"))

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/dst/proj",
		OnCollision: func(sessionID string, isMemory bool) CollisionDecision {
			if isMemory {
				return DecisionSkip
			}
			return DecisionOverwrite
		},
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	if result.MemoryStatus != "skipped" {
		t.Errorf("MemoryStatus: want \"skipped\", got %q", result.MemoryStatus)
	}

	// old.md must still be present.
	content, err := os.ReadFile(filepath.Join(dest, "memory", "old.md"))
	if err != nil {
		t.Fatalf("ReadFile old.md: %v", err)
	}
	if string(content) != "OLD MEMORY" {
		t.Errorf("old.md content: want \"OLD MEMORY\", got %q", string(content))
	}
}

// TestUnpack_MemoryCollisionOverwrite verifies that DecisionOverwrite for a
// memory collision removes the existing memory dir and replaces it with the
// bundle's contents. MemoryStatus == "written".
func TestUnpack_MemoryCollisionOverwrite(t *testing.T) {
	bundlePath := buildBundleOrdered(t, "/src/proj", "-src-proj", [][2]string{
		{"sess-1", `{"type":"user"}` + "\n"},
	}, map[string]string{
		"MEMORY.md": "# Bundle Memory\n",
	})

	dest := t.TempDir()

	// Pre-create dest/memory/old.md to trigger a collision.
	writeFile(t, filepath.Join(dest, "memory", "old.md"), []byte("OLD MEMORY"))

	result, err := Unpack(UnpackOptions{
		BundlePath: bundlePath,
		DestDir:    dest,
		DestCwd:    "/dst/proj",
		OnCollision: func(sessionID string, isMemory bool) CollisionDecision {
			if isMemory {
				return DecisionOverwrite
			}
			return DecisionOverwrite
		},
	})
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	if result.MemoryStatus != "written" {
		t.Errorf("MemoryStatus: want \"written\", got %q", result.MemoryStatus)
	}

	// old.md should be gone (replaced by bundle contents).
	if _, statErr := os.Stat(filepath.Join(dest, "memory", "old.md")); statErr == nil {
		t.Error("old.md should have been removed by DecisionOverwrite")
	}

	// Bundle's MEMORY.md should be present.
	content, err := os.ReadFile(filepath.Join(dest, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatalf("ReadFile MEMORY.md: %v", err)
	}
	if string(content) != "# Bundle Memory\n" {
		t.Errorf("MEMORY.md content: want \"# Bundle Memory\", got %q", string(content))
	}
}
