package sessionsync

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// zipEntryNames opens the zip at zipPath and returns all entry names, sorted.
func zipEntryNames(t *testing.T, zipPath string) []string {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("zip.OpenReader %s: %v", zipPath, err)
	}
	defer r.Close()

	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

// readZipEntry reads the full bytes of the named entry from the zip at zipPath.
func readZipEntry(t *testing.T, zipPath, entryName string) []byte {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("zip.OpenReader %s: %v", zipPath, err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != entryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", entryName, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read zip entry %s: %v", entryName, err)
		}
		return data
	}
	t.Fatalf("zip entry %q not found in %s", entryName, zipPath)
	return nil
}

// TestPack_BasicTwoSessions builds a synthetic source tree with two sessions,
// one of which has a subagents subdirectory. Verifies the zip entries and the
// manifest content (SourceCwd extracted from sess-aaa's first JSONL line).
func TestPack_BasicTwoSessions(t *testing.T) {
	src := t.TempDir()

	// sess-aaa.jsonl: first line has cwd, two more lines without.
	writeFile(t, filepath.Join(src, "sess-aaa.jsonl"), []byte(
		`{"type":"summary","cwd":"D:\\Documents\\foo"}`+"\n"+
			`{"type":"user","content":"hello"}`+"\n"+
			`{"type":"assistant","content":"hi"}`+"\n",
	))

	// sess-bbb.jsonl: single line, no cwd.
	writeFile(t, filepath.Join(src, "sess-bbb.jsonl"), []byte(
		`{"type":"user"}`+"\n",
	))

	// sess-aaa subagents tree.
	writeFile(t, filepath.Join(src, "sess-aaa", "subagents", "agent-x.jsonl"), []byte(`{"type":"user"}`+"\n"))
	writeFile(t, filepath.Join(src, "sess-aaa", "subagents", "agent-x.meta.json"), []byte("{}"))

	outZip := filepath.Join(t.TempDir(), "bundle.zip")

	result, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: "D--Documents-foo",
		SessionIDs:       []string{"sess-aaa", "sess-bbb"},
		IncludeMemory:    false,
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("Pack: unexpected error: %v", err)
	}

	if result.BytesWritten == 0 {
		t.Error("PackResult.BytesWritten should be > 0")
	}

	// Verify expected zip entries (sorted).
	got := zipEntryNames(t, outZip)
	want := []string{
		"manifest.json",
		"sess-aaa.jsonl",
		"sess-aaa/subagents/agent-x.jsonl",
		"sess-aaa/subagents/agent-x.meta.json",
		"sess-bbb.jsonl",
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("zip entries mismatch:\n  want: %v\n   got: %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry[%d]: want %q, got %q", i, want[i], got[i])
		}
	}

	// Re-open and parse manifest.
	manifestData := readZipEntry(t, outZip, "manifest.json")
	mf, err := UnmarshalManifest(manifestData)
	if err != nil {
		t.Fatalf("UnmarshalManifest: %v", err)
	}

	if mf.SourceCwd != `D:\Documents\foo` {
		t.Errorf("SourceCwd: want %q, got %q", `D:\Documents\foo`, mf.SourceCwd)
	}
	if mf.SourceEncodedCwd != "D--Documents-foo" {
		t.Errorf("SourceEncodedCwd: want %q, got %q", "D--Documents-foo", mf.SourceEncodedCwd)
	}
	if len(mf.Sessions) != 2 || mf.Sessions[0] != "sess-aaa" || mf.Sessions[1] != "sess-bbb" {
		t.Errorf("Sessions: want [sess-aaa sess-bbb], got %v", mf.Sessions)
	}
	if mf.IncludeMemory {
		t.Error("IncludeMemory: expected false")
	}
	if mf.FormatVersion != FormatVersion {
		t.Errorf("FormatVersion: want %q, got %q", FormatVersion, mf.FormatVersion)
	}
}

// TestPack_WithMemory adds a memory/ subtree and verifies memory entries appear
// in the zip and the manifest reports IncludeMemory=true.
func TestPack_WithMemory(t *testing.T) {
	src := t.TempDir()

	writeFile(t, filepath.Join(src, "sess-1.jsonl"), []byte(`{"type":"user"}`+"\n"))
	writeFile(t, filepath.Join(src, "memory", "MEMORY.md"), []byte("# memory\n"))
	writeFile(t, filepath.Join(src, "memory", "feedback_x.md"), []byte("# feedback\n"))

	outZip := filepath.Join(t.TempDir(), "bundle.zip")

	result, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: "test-proj",
		SessionIDs:       []string{"sess-1"},
		IncludeMemory:    true,
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("Pack: unexpected error: %v", err)
	}
	if result.Manifest.IncludeMemory != true {
		t.Error("Manifest.IncludeMemory should be true")
	}

	names := zipEntryNames(t, outZip)

	hasEntry := func(want string) bool {
		for _, n := range names {
			if n == want {
				return true
			}
		}
		return false
	}

	for _, expected := range []string{
		"sess-1.jsonl",
		"memory/MEMORY.md",
		"memory/feedback_x.md",
		"manifest.json",
	} {
		if !hasEntry(expected) {
			t.Errorf("expected zip entry %q not found; got: %v", expected, names)
		}
	}
}

// TestPack_NoSubagents packs a single session that has no subagents/ directory.
// Expects only the JSONL and manifest entries; no subagent entries.
func TestPack_NoSubagents(t *testing.T) {
	src := t.TempDir()

	writeFile(t, filepath.Join(src, "only-session.jsonl"), []byte(`{"type":"user"}`+"\n"))

	outZip := filepath.Join(t.TempDir(), "bundle.zip")

	_, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: "some-project",
		SessionIDs:       []string{"only-session"},
		IncludeMemory:    false,
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("Pack: unexpected error: %v", err)
	}

	names := zipEntryNames(t, outZip)
	if len(names) != 2 {
		t.Fatalf("expected 2 zip entries (jsonl + manifest), got %d: %v", len(names), names)
	}

	for _, expected := range []string{"only-session.jsonl", "manifest.json"} {
		found := false
		for _, n := range names {
			if n == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected entry %q not found in %v", expected, names)
		}
	}
}

// TestPack_FallbackSourceCwd verifies that when no JSONL line contains a "cwd"
// field, the manifest SourceCwd is the lossy reverse-derivation of
// SourceEncodedCwd.
func TestPack_FallbackSourceCwd(t *testing.T) {
	src := t.TempDir()
	encodedCwd := "D--Documents-foo"

	// JSONL with no cwd field at all.
	writeFile(t, filepath.Join(src, "sess-x.jsonl"), []byte(
		`{"type":"user","content":"no cwd here"}`+"\n"+
			`{"type":"assistant","content":"also no cwd"}`+"\n",
	))

	outZip := filepath.Join(t.TempDir(), "bundle.zip")

	_, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: encodedCwd,
		SessionIDs:       []string{"sess-x"},
		IncludeMemory:    false,
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("Pack: unexpected error: %v", err)
	}

	manifestData := readZipEntry(t, outZip, "manifest.json")
	mf, err := UnmarshalManifest(manifestData)
	if err != nil {
		t.Fatalf("UnmarshalManifest: %v", err)
	}

	expected := reverseDeriveSourceCwd(encodedCwd)
	if mf.SourceCwd != expected {
		t.Errorf("SourceCwd fallback: want %q, got %q", expected, mf.SourceCwd)
	}
}

// TestPack_PreservesSessionOrder packs sessions in non-alphabetical order and
// verifies the manifest Sessions slice preserves the input order exactly.
func TestPack_PreservesSessionOrder(t *testing.T) {
	src := t.TempDir()

	writeFile(t, filepath.Join(src, "sess-zzz.jsonl"), []byte(`{"type":"user"}`+"\n"))
	writeFile(t, filepath.Join(src, "sess-aaa.jsonl"), []byte(`{"type":"user"}`+"\n"))

	outZip := filepath.Join(t.TempDir(), "bundle.zip")

	result, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: "order-test",
		SessionIDs:       []string{"sess-zzz", "sess-aaa"},
		IncludeMemory:    false,
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("Pack: unexpected error: %v", err)
	}

	if len(result.Manifest.Sessions) != 2 {
		t.Fatalf("expected 2 sessions in manifest, got %d", len(result.Manifest.Sessions))
	}
	if result.Manifest.Sessions[0] != "sess-zzz" {
		t.Errorf("Sessions[0]: want sess-zzz, got %s", result.Manifest.Sessions[0])
	}
	if result.Manifest.Sessions[1] != "sess-aaa" {
		t.Errorf("Sessions[1]: want sess-aaa, got %s", result.Manifest.Sessions[1])
	}

	// Also verify via re-reading the persisted manifest from the zip.
	manifestData := readZipEntry(t, outZip, "manifest.json")
	mf, err := UnmarshalManifest(manifestData)
	if err != nil {
		t.Fatalf("UnmarshalManifest: %v", err)
	}
	if len(mf.Sessions) != 2 || mf.Sessions[0] != "sess-zzz" || mf.Sessions[1] != "sess-aaa" {
		t.Errorf("persisted Sessions order wrong: %v", mf.Sessions)
	}
}

// TestReverseDeriveSourceCwd exercises the lossy fallback function directly.
func TestReverseDeriveSourceCwd(t *testing.T) {
	cases := []struct {
		encoded string
		want    string
	}{
		{"D--Documents-foo", "/D//Documents/foo"},
		{"-home-alice-projects-myapp", "/home/alice/projects/myapp"},
		{"already-fine", "/already/fine"},
	}

	for _, tc := range cases {
		got := reverseDeriveSourceCwd(tc.encoded)
		if got != tc.want {
			t.Errorf("reverseDeriveSourceCwd(%q) = %q, want %q", tc.encoded, got, tc.want)
		}
	}
}

// TestExtractCwdFromJSONLine exercises the cwd extraction helper.
func TestExtractCwdFromJSONLine(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		wantCwd string
		wantOK  bool
	}{
		{
			name:    "has cwd",
			line:    `{"type":"summary","cwd":"/home/user/proj"}`,
			wantCwd: "/home/user/proj",
			wantOK:  true,
		},
		{
			name:   "no cwd field",
			line:   `{"type":"user","content":"hello"}`,
			wantOK: false,
		},
		{
			name:   "invalid json",
			line:   `{not json}`,
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   ``,
			wantOK: false,
		},
		{
			name:   "cwd is empty string",
			line:   `{"cwd":""}`,
			wantOK: false,
		},
		{
			name:    "windows path cwd",
			line:    `{"cwd":"C:\\Users\\alice\\project"}`,
			wantCwd: `C:\Users\alice\project`,
			wantOK:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractCwdFromJSONLine([]byte(tc.line))
			if ok != tc.wantOK {
				t.Errorf("ok: want %v, got %v", tc.wantOK, ok)
			}
			if ok && got != tc.wantCwd {
				t.Errorf("cwd: want %q, got %q", tc.wantCwd, got)
			}
		})
	}
}

// TestPack_OutputDirCreated verifies Pack creates parent directories of
// OutputZipPath when they do not yet exist.
func TestPack_OutputDirCreated(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "s.jsonl"), []byte(`{"type":"user"}`+"\n"))

	// OutputZipPath points to a deeply nested non-existent directory.
	outDir := filepath.Join(t.TempDir(), "a", "b", "c")
	outZip := filepath.Join(outDir, "bundle.zip")

	_, err := Pack(PackOptions{
		SourceFolder:     src,
		SourceEncodedCwd: "proj",
		SessionIDs:       []string{"s"},
		OutputZipPath:    outZip,
	})
	if err != nil {
		t.Fatalf("Pack: unexpected error: %v", err)
	}

	if _, err := os.Stat(outZip); err != nil {
		t.Errorf("expected output zip to exist at %s: %v", outZip, err)
	}
}
