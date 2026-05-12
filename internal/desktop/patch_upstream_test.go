package desktop

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestInjectShim_WithShebang(t *testing.T) {
	in := []byte("#!/usr/bin/env node\nimport { McpServer } from 'foo'\n")
	out := injectShim(in)
	lines := strings.SplitN(string(out), "\n", 3)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "#!/usr/bin/env node" {
		t.Errorf("shebang not preserved as first line: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "/* zpit-cu-mcp-windows-argv-shim v1 */") {
		t.Errorf("shim should start immediately after shebang, got line 2: %q", lines[1])
	}
}

func TestInjectShim_WithoutShebang(t *testing.T) {
	in := []byte("import { foo } from 'bar'\nconsole.log('hi')\n")
	out := injectShim(in)
	if !bytes.HasPrefix(out, []byte("/* zpit-cu-mcp-windows-argv-shim v1 */")) {
		t.Errorf("shim should be at top when no shebang; got: %q", out[:80])
	}
	if !bytes.Contains(out, []byte("import { foo } from 'bar'")) {
		t.Errorf("original content lost: %q", out)
	}
}

func TestInjectShim_EmptyInput(t *testing.T) {
	out := injectShim(nil)
	if !bytes.HasPrefix(out, []byte("/* zpit-cu-mcp-windows-argv-shim v1 */")) {
		t.Errorf("shim should still be inserted for empty input")
	}
}

func TestInjectShim_PrependsArgvRewrite(t *testing.T) {
	out := injectShim([]byte("// file\n"))
	if !bytes.Contains(out, []byte("process.argv[1].replace(/\\\\/g, '/')")) {
		t.Errorf("shim should rewrite backslashes; got: %q", out)
	}
	if !bytes.Contains(out, []byte("process.argv[1].indexOf('\\\\')")) {
		t.Errorf("shim should test for backslash presence; got: %q", out)
	}
}

func TestPatchServerJSInPlace_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/server.js"
	// Simulate the upstream file shape (shebang + plausible content).
	original := []byte("#!/usr/bin/env node\n// upstream content\nif (process.argv[1]?.endsWith('/server.js')) {}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	patched, err := patchServerJSInPlace(path)
	if err != nil {
		t.Fatalf("first patch: %v", err)
	}
	if !patched {
		t.Fatalf("first call should have patched")
	}

	patched2, err := patchServerJSInPlace(path)
	if err != nil {
		t.Fatalf("second patch: %v", err)
	}
	if patched2 {
		t.Errorf("second call should be no-op (idempotent), but reported patched=true")
	}

	// Verify file content has exactly one shim marker.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after patch: %v", err)
	}
	if bytes.Count(got, []byte(patchMarkerV1)) != 1 {
		t.Errorf("expected exactly one shim marker after two patch calls, got %d",
			bytes.Count(got, []byte(patchMarkerV1)))
	}
	if !bytes.Contains(got, []byte("// upstream content")) {
		t.Errorf("original content lost after patching")
	}
}
