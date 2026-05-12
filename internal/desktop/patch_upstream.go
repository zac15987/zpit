package desktop

// patch_upstream.go — Stopgap for upstream bug in @zavora-ai/computer-use-mcp
// where src/server.ts uses `endsWith('/server.js')` to detect the stdio
// entrypoint. Node normalizes process.argv[1] to backslashes on Windows, so
// the check is structurally unreachable — the server silently exits without
// starting StdioServerTransport. Upstream fix:
//
//   https://github.com/zavora-ai/computer-use-mcp/pull/9
//
// REMOVE THIS FILE and its callsite in proxy.go once upstream releases the
// fix (likely 6.2+) and we pin to that version. Search for "patchMarkerV1"
// to find related sites.
//
// Strategy: prepend a tiny argv-normalization shim to the installed
// server.js. The shim is a no-op on macOS (where argv[1] already uses '/')
// and rewrites '\\' → '/' on Windows before the buggy endsWith runs.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// patchMarkerV1 is the idempotency marker. Anywhere in the file's first 200
// bytes means "already patched", skip.
const patchMarkerV1 = "/* zpit-cu-mcp-windows-argv-shim v1 */"

// patchShimV1 is the code we prepend (immediately after the shebang line).
// Kept terse so the patched file diff stays small if a human inspects it.
const patchShimV1 = patchMarkerV1 + `
if (typeof process !== 'undefined' && process.argv && typeof process.argv[1] === 'string' && process.argv[1].indexOf('\\') !== -1) {
  process.argv[1] = process.argv[1].replace(/\\/g, '/');
}
`

// PatchInstalledUpstream locates the globally-installed
// @zavora-ai/computer-use-mcp server.js and prepends an argv-normalization
// shim if not already patched. Idempotent. No-op on non-Windows hosts since
// the bug only triggers on Windows.
//
// Returns (path, true, nil) when a patch was applied this call.
// Returns (path, false, nil) when the file was already patched or the host
// is not Windows. Returns ("", false, err) when the package cannot be
// located. Locate errors are non-fatal at the proxy level — npx may still
// pull a usable copy from its own cache on macOS.
func PatchInstalledUpstream() (path string, patched bool, err error) {
	if runtime.GOOS != "windows" {
		return "", false, nil
	}
	serverPath, err := findGlobalUpstreamServerJS()
	if err != nil {
		return "", false, err
	}
	patched, err = patchServerJSInPlace(serverPath)
	if err != nil {
		return serverPath, false, err
	}
	return serverPath, patched, nil
}

// findGlobalUpstreamServerJS resolves the global node_modules dir via
// `npm root -g`, then constructs the expected path. Returns an error if the
// package is not installed there. (We do not chase the npx cache — it's
// hashed and ephemeral; relying on a stable global install is what makes
// the stopgap predictable.)
func findGlobalUpstreamServerJS() (string, error) {
	npmExe, err := lookupNpmExecutable()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(npmExe, "root", "-g").Output()
	if err != nil {
		return "", fmt.Errorf("npm root -g: %w", err)
	}
	globalDir := strings.TrimSpace(string(out))
	if globalDir == "" {
		return "", errors.New("npm root -g returned empty path")
	}
	candidate := filepath.Join(globalDir, "@zavora-ai", "computer-use-mcp", "dist", "server.js")
	if _, statErr := os.Stat(candidate); statErr != nil {
		return "", fmt.Errorf("@zavora-ai/computer-use-mcp not installed at %s (run `npm install -g @zavora-ai/computer-use-mcp` to enable Windows stopgap)", candidate)
	}
	return candidate, nil
}

// lookupNpmExecutable resolves npm.cmd on Windows / npm on POSIX. exec.LookPath
// on Windows often resolves to npm.ps1 which can't be spawned directly via
// Process.Start (we hit this earlier with npx).
func lookupNpmExecutable() (string, error) {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("npm.cmd"); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("npm"); err == nil {
		return p, nil
	}
	return "", errors.New("npm not found in PATH")
}

// patchServerJSInPlace reads the file, checks for the idempotency marker,
// and writes back with the shim prepended if not already patched.
// Preserves an existing #! shebang on the first line.
func patchServerJSInPlace(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	// Idempotency: bail if marker is already present anywhere in the file.
	if bytes.Contains(data, []byte(patchMarkerV1)) {
		return false, nil
	}
	newData := injectShim(data)
	if err := os.WriteFile(path, newData, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// injectShim returns data with patchShimV1 inserted just after the shebang
// line if one exists, otherwise at the very top. Pure function for testing.
func injectShim(data []byte) []byte {
	if bytes.HasPrefix(data, []byte("#!")) {
		if nl := bytes.IndexByte(data, '\n'); nl > 0 {
			out := make([]byte, 0, len(data)+len(patchShimV1))
			out = append(out, data[:nl+1]...)
			out = append(out, []byte(patchShimV1)...)
			out = append(out, data[nl+1:]...)
			return out
		}
	}
	out := make([]byte, 0, len(data)+len(patchShimV1))
	out = append(out, []byte(patchShimV1)...)
	out = append(out, data...)
	return out
}
