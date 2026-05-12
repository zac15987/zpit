package desktop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// ---- helpers ----------------------------------------------------------------

// writeFrame marshals msg as newline-terminated JSON and writes it to w.
func writeFrame(w io.Writer, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// readFrame reads one newline-delimited JSON frame from r.
func readFrame(r *bufio.Reader) (map[string]any, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// testProxyHandles holds all the I/O handles needed to drive a Proxy under test.
//
// Pipe layout (arrows show data flow direction):
//
//	Test writes → claudeWrite → [proxy in] → policy gate → [proxy out] → claudeRead ← Test reads
//	Test writes → upToProxyWrite → [proxy reads from upstream] → (filters) → claudeRead
//	Test reads ← proxyToUpRead ← [proxy writes to upstream]
type testProxyHandles struct {
	// claudeWrite: test writes frames here to simulate Claude Code sending to proxy.
	claudeWrite io.WriteCloser
	// claudeRead: test reads frames here that the proxy sent back to Claude Code.
	claudeRead *bufio.Reader
	// upToProxyWrite: test writes frames here to simulate upstream sending to proxy.
	upToProxyWrite io.WriteCloser
	// proxyToUpRead: test reads frames here that the proxy forwarded to upstream.
	proxyToUpRead *bufio.Reader
	// logBuf: receives all DesktopLogger output.
	logBuf *bytes.Buffer
	// proxy: the Proxy under test (started via h.run()).
	proxy *Proxy

	// internal pipe ends passed to runWithUpstream.
	proxyToUpWriteCloser io.WriteCloser // proxy writes here (upstream stdin)
	upToProxyReadCloser  io.ReadCloser  // proxy reads here (upstream stdout)
}

// newTestProxy creates a Proxy bound to in-memory pipes with the given policy.
func newTestProxy(policy Policy) *testProxyHandles {
	// Claude → proxy: test writes to clOut, proxy reads from clIn.
	clIn, clOut := io.Pipe()
	// Proxy → Claude: proxy writes to prOut, test reads from prIn.
	prIn, prOut := io.Pipe()

	// Upstream stdout → proxy: test writes to upToProxyWr, proxy reads from upToProxyRd.
	upToProxyRd, upToProxyWr := io.Pipe()
	// Proxy → upstream stdin: proxy writes to proxyToUpWr, test reads from proxyToUpRd.
	proxyToUpRd, proxyToUpWr := io.Pipe()

	logBuf := new(bytes.Buffer)
	logger := NewDesktopLogger(logBuf)

	p := NewProxy(clIn, prOut, policy, logger, "test-agent")

	return &testProxyHandles{
		claudeWrite:          clOut,
		claudeRead:           bufio.NewReader(prIn),
		upToProxyWrite:       upToProxyWr,
		proxyToUpRead:        bufio.NewReader(proxyToUpRd),
		logBuf:               logBuf,
		proxy:                p,
		proxyToUpWriteCloser: proxyToUpWr,
		upToProxyReadCloser:  upToProxyRd,
	}
}

// run starts proxy.runWithUpstream in a goroutine and returns a cancel func
// and a channel that receives the run error.
func (h *testProxyHandles) run() (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.proxy.runWithUpstream(ctx, h.proxyToUpWriteCloser, h.upToProxyReadCloser)
	}()
	return cancel, errCh
}

// toolsCallFrame constructs a minimal tools/call JSON-RPC request frame.
func toolsCallFrame(id int, toolName string, args map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(id),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
}

// ---- DesktopLogger ----------------------------------------------------------

func TestDesktopLogger_Format(t *testing.T) {
	var buf bytes.Buffer
	l := NewDesktopLogger(&buf)
	l.Infof("decision=allow tool=screenshot agent=desktop-test reason=allowed")
	got := buf.String()

	// Must start with '['.
	if !strings.HasPrefix(got, "[") {
		t.Fatalf("expected log line to start with '[', got: %q", got)
	}
	// Must contain level and prefix.
	if !strings.Contains(got, "[Info] [desktop-proxy]") {
		t.Fatalf("expected [Info] [desktop-proxy] in log line, got: %q", got)
	}
	// Must contain the message body verbatim.
	if !strings.Contains(got, "decision=allow tool=screenshot agent=desktop-test reason=allowed") {
		t.Fatalf("expected body in log line, got: %q", got)
	}
	// Must end with newline.
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected log line to end with newline, got: %q", got)
	}
	// Minimum length sanity check.
	if len(got) < 30 {
		t.Fatalf("log line too short: %q", got)
	}
}

// ---- AC-2: tool not in allowlist --------------------------------------------

func TestProxy_DenyNotAllowedTool(t *testing.T) {
	// run_script is NOT in DefaultAllowedTools.
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(1, "run_script", map[string]any{"code": "ls"})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got: %v", resp)
	}
	if result["isError"] != true {
		t.Errorf("expected isError=true, got: %v", result["isError"])
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected non-empty content array")
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	expected := "denied: tool 'run_script' is not in the desktop agent allowlist (see ~/.zpit/desktop-policy.toml)"
	if text != expected {
		t.Errorf("denial text mismatch\ngot:  %q\nwant: %q", text, expected)
	}

	h.claudeWrite.Close()

	if !strings.Contains(h.logBuf.String(), "decision=deny") {
		t.Errorf("expected decision=deny in log, got: %s", h.logBuf.String())
	}
	if !strings.Contains(h.logBuf.String(), "reason=tool_not_allowed") {
		t.Errorf("expected reason=tool_not_allowed in log, got: %s", h.logBuf.String())
	}
}

// ---- AC-3: deny_keys --------------------------------------------------------

func TestProxy_DenyKeyMatch(t *testing.T) {
	// DefaultPolicy deny_keys includes ctrl+alt+del.
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(2, "key", map[string]any{"text": "Ctrl+Alt+Del"})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}

	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("expected isError=true")
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	// AC-3: uses the ORIGINAL text value (not lowercased) in the error message.
	expected := "denied: key combo 'Ctrl+Alt+Del' matches deny_keys entry 'ctrl+alt+del'"
	if text != expected {
		t.Errorf("AC-3 text mismatch\ngot:  %q\nwant: %q", text, expected)
	}

	h.claudeWrite.Close()
}

func TestProxy_DenyKeySubstringMatch(t *testing.T) {
	// "ctrl+alt+t+then+a" contains "ctrl+alt+t" as a substring.
	policy := DefaultPolicy()
	policy.DenyKeys = []string{"ctrl+alt+t"}

	h := newTestProxy(policy)
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(3, "key", map[string]any{"text": "ctrl+alt+t+then+a"})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}

	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("expected isError=true for substring match")
	}
	h.claudeWrite.Close()
}

// ---- allow key with no match ------------------------------------------------

func TestProxy_AllowKeyNoMatch(t *testing.T) {
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(4, "key", map[string]any{"text": "enter"})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	// Proxy should forward to upstream.
	upFwd, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame from upstream (proxyToUpRead): %v", err)
	}
	method, _ := upFwd["method"].(string)
	if method != "tools/call" {
		t.Errorf("expected tools/call forwarded, got method=%q", method)
	}

	// Simulate upstream responding.
	upstreamResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(4),
		"result": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
		},
	}
	if err := writeFrame(h.upToProxyWrite, upstreamResp); err != nil {
		t.Fatalf("write upstream resp: %v", err)
	}

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame from claude: %v", err)
	}
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if text != "ok" {
		t.Errorf("expected 'ok', got %q", text)
	}

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

// ---- AC-4: bundle filtering -------------------------------------------------

func TestProxy_BundleDenied(t *testing.T) {
	policy := DefaultPolicy()
	policy.AllowBundles = []string{"com.apple.Safari"}

	h := newTestProxy(policy)
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(5, "left_click", map[string]any{
		"x": float64(100), "y": float64(200),
		"target_app": "com.apple.Notes",
	})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("expected isError=true for bundle denied")
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	expected := "denied: target_app 'com.apple.Notes' is not in allow_bundles"
	if text != expected {
		t.Errorf("AC-4 text mismatch\ngot:  %q\nwant: %q", text, expected)
	}

	h.claudeWrite.Close()
}

func TestProxy_BundleAllowed(t *testing.T) {
	policy := DefaultPolicy()
	policy.AllowBundles = []string{"com.apple.Safari"}

	h := newTestProxy(policy)
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(6, "left_click", map[string]any{
		"x": float64(100), "y": float64(200),
		"target_app": "com.apple.Safari",
	})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	upFwd, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame upstream: %v", err)
	}
	if upFwd["method"] != "tools/call" {
		t.Errorf("expected tools/call forwarded, got: %v", upFwd["method"])
	}

	upResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(6),
		"result":  map[string]any{"content": []any{map[string]any{"type": "text", "text": "clicked"}}},
	}
	writeFrame(h.upToProxyWrite, upResp)

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame claude: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["isError"] == true {
		t.Errorf("expected no error for allowed bundle")
	}

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

func TestProxy_BundleNoFilterWhenEmpty(t *testing.T) {
	// AllowBundles is empty by default → any target_app is allowed.
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(7, "left_click", map[string]any{
		"x": float64(100), "y": float64(200),
		"target_app": "com.anything.Anything",
	})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	upFwd, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame upstream: %v", err)
	}
	if upFwd["method"] != "tools/call" {
		t.Errorf("expected tools/call forwarded")
	}

	upResp := map[string]any{
		"jsonrpc": "2.0", "id": float64(7),
		"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}},
	}
	writeFrame(h.upToProxyWrite, upResp)
	readFrame(h.claudeRead)

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

func TestProxy_NoTargetAppNoFilter(t *testing.T) {
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	// No target_app key in arguments.
	req := toolsCallFrame(8, "left_click", map[string]any{"x": float64(50), "y": float64(60)})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	upFwd, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame upstream: %v", err)
	}
	if upFwd["method"] != "tools/call" {
		t.Errorf("expected tools/call forwarded")
	}

	upResp := map[string]any{
		"jsonrpc": "2.0", "id": float64(8),
		"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}},
	}
	writeFrame(h.upToProxyWrite, upResp)
	readFrame(h.claudeRead)

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

// ---- AC-5: focus strategy override ------------------------------------------

func TestProxy_FocusStrategyOverride(t *testing.T) {
	// KeyboardFocusStrategy defaults to "strict".
	policy := DefaultPolicy()

	for _, toolName := range []string{"type", "key", "hold_key", "set_value", "fill_form"} {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			h := newTestProxy(policy)
			cancel, _ := h.run()
			defer cancel()

			req := toolsCallFrame(10, toolName, map[string]any{
				"text":           "hello",
				"focus_strategy": "best_effort",
			})
			if err := writeFrame(h.claudeWrite, req); err != nil {
				t.Fatalf("writeFrame: %v", err)
			}

			upFwd, err := readFrame(h.proxyToUpRead)
			if err != nil {
				t.Fatalf("readFrame upstream: %v", err)
			}
			params := upFwd["params"].(map[string]any)
			args := params["arguments"].(map[string]any)
			if args["focus_strategy"] != "strict" {
				t.Errorf("%s: expected focus_strategy=strict in forwarded frame, got %v", toolName, args["focus_strategy"])
			}

			// Verify AC-5 override log line.
			logStr := h.logBuf.String()
			if !strings.Contains(logStr, "forced focus_strategy: strict (agent supplied: best_effort)") {
				t.Errorf("%s: expected AC-5 log line, got: %s", toolName, logStr)
			}
			if !strings.Contains(logStr, "tool="+toolName) {
				t.Errorf("%s: expected tool= in log, got: %s", toolName, logStr)
			}

			upResp := map[string]any{
				"jsonrpc": "2.0", "id": float64(10),
				"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}},
			}
			writeFrame(h.upToProxyWrite, upResp)
			readFrame(h.claudeRead)

			h.claudeWrite.Close()
			h.upToProxyWrite.Close()
		})
	}
}

func TestProxy_FocusStrategyNotOverriddenForNonKeyboardTool(t *testing.T) {
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(11, "screenshot", map[string]any{"focus_strategy": "best_effort"})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	upFwd, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame upstream: %v", err)
	}
	params := upFwd["params"].(map[string]any)
	args := params["arguments"].(map[string]any)
	// screenshot is not in keyboardWritingTools, so focus_strategy is unchanged.
	if args["focus_strategy"] != "best_effort" {
		t.Errorf("expected focus_strategy=best_effort unchanged for screenshot, got %v", args["focus_strategy"])
	}

	upResp := map[string]any{
		"jsonrpc": "2.0", "id": float64(11),
		"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}},
	}
	writeFrame(h.upToProxyWrite, upResp)
	readFrame(h.claudeRead)

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

// ---- tools/list filtering ---------------------------------------------------

func TestProxy_ToolsListFiltering(t *testing.T) {
	h := newTestProxy(DefaultPolicy())
	cancel, errCh := h.run()
	defer cancel()

	// Build 58 fake tools: first DefaultAllowedTools.count entries are in the allowlist;
	// the rest are non-allowed.
	allFakeTools := make([]any, 58)
	for i, name := range DefaultAllowedTools {
		allFakeTools[i] = map[string]any{"name": name, "description": "fake tool"}
	}
	for i := len(DefaultAllowedTools); i < 58; i++ {
		allFakeTools[i] = map[string]any{
			"name":        "non_allowed_tool_" + string(rune('a'+i-len(DefaultAllowedTools))),
			"description": "fake",
		}
	}

	// Simulate upstream sending a tools/list response.
	toolsListResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result": map[string]any{
			"tools": allFakeTools,
		},
	}
	if err := writeFrame(h.upToProxyWrite, toolsListResp); err != nil {
		t.Fatalf("write tools/list to upstream pipe: %v", err)
	}

	// Read the filtered response on Claude side.
	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame claude: %v", err)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object")
	}
	filteredTools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array in result, got %T: %v", result["tools"], result["tools"])
	}

	if len(filteredTools) != len(DefaultAllowedTools) {
		t.Errorf("expected %d tools after filtering, got %d", len(DefaultAllowedTools), len(filteredTools))
	}

	allowedSet := make(map[string]bool, len(DefaultAllowedTools))
	for _, n := range DefaultAllowedTools {
		allowedSet[n] = true
	}
	for _, entry := range filteredTools {
		obj, ok := entry.(map[string]any)
		if !ok {
			t.Errorf("tool entry not an object: %v", entry)
			continue
		}
		name, _ := obj["name"].(string)
		if !allowedSet[name] {
			t.Errorf("filtered list contains disallowed tool %q", name)
		}
	}

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
	<-errCh
}

// ---- passthrough non-tools/call methods -------------------------------------

func TestProxy_PassthroughInitialize(t *testing.T) {
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]any{"name": "claude-code", "version": "1.0"},
		},
	}
	if err := writeFrame(h.claudeWrite, initReq); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	// Proxy should forward verbatim to upstream.
	upFwd, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame upstream: %v", err)
	}
	if upFwd["method"] != "initialize" {
		t.Errorf("expected initialize forwarded, got method=%v", upFwd["method"])
	}

	upResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result": map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]any{"name": "computer-use-mcp", "version": "1.0"},
			"capabilities":    map[string]any{},
		},
	}
	if err := writeFrame(h.upToProxyWrite, upResp); err != nil {
		t.Fatalf("write upstream resp: %v", err)
	}

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame claude: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected passthrough initialize result, got: %v", result)
	}

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

// ---- AC-12 h: large frame handling ------------------------------------------

func TestProxy_LargeFrameHandling(t *testing.T) {
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	// Construct a 2 MB ASCII string simulating a base64-encoded screenshot payload.
	bigPayload := strings.Repeat("A", 2*1024*1024)

	largeResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(99),
		"result": map[string]any{
			"content": []any{
				map[string]any{
					"type": "image",
					"data": bigPayload,
				},
			},
		},
	}
	if err := writeFrame(h.upToProxyWrite, largeResp); err != nil {
		t.Fatalf("write large frame: %v", err)
	}

	// Read on Claude side — must not be truncated.
	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame large claude: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object in large frame")
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in large frame response")
	}
	got, _ := content[0].(map[string]any)["data"].(string)
	if len(got) != 2*1024*1024 {
		t.Errorf("large frame truncated: got %d bytes, want %d", len(got), 2*1024*1024)
	}

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

// ---- run_script disabled even when in allowlist -----------------------------

func TestProxy_RunScriptDisabledByPolicyEvenIfInAllowlist(t *testing.T) {
	// allow_run_script=false + run_script in allowed_tools → denied (run_script_disabled).
	policy := DefaultPolicy()
	policy.AllowedTools = append(policy.AllowedTools, "run_script")
	policy.AllowRunScript = false

	h := newTestProxy(policy)
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(20, "run_script", map[string]any{"code": "echo hello"})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	resp, err := readFrame(h.claudeRead)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("expected isError=true when allow_run_script=false")
	}
	if !strings.Contains(h.logBuf.String(), "reason=run_script_disabled") {
		t.Errorf("expected reason=run_script_disabled in log, got: %s", h.logBuf.String())
	}
	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

func TestProxy_RunScriptEnabledAndInAllowlist(t *testing.T) {
	// allow_run_script=true + run_script in allowed_tools → forwarded.
	policy := DefaultPolicy()
	policy.AllowedTools = append(policy.AllowedTools, "run_script")
	policy.AllowRunScript = true

	h := newTestProxy(policy)
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(21, "run_script", map[string]any{"code": "echo hello"})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	upFwd, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame upstream: %v", err)
	}
	if upFwd["method"] != "tools/call" {
		t.Errorf("expected tools/call forwarded when run_script allowed, got: %v", upFwd["method"])
	}

	upResp := map[string]any{
		"jsonrpc": "2.0", "id": float64(21),
		"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "hello"}}},
	}
	writeFrame(h.upToProxyWrite, upResp)
	readFrame(h.claudeRead)

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}

// ---- AC-10 logging in allow path --------------------------------------------

func TestProxy_DecisionLogOnAllow(t *testing.T) {
	h := newTestProxy(DefaultPolicy())
	cancel, _ := h.run()
	defer cancel()

	req := toolsCallFrame(30, "screenshot", map[string]any{})
	if err := writeFrame(h.claudeWrite, req); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	// Read the forwarded frame from upstream reader.
	_, err := readFrame(h.proxyToUpRead)
	if err != nil {
		t.Fatalf("readFrame upstream: %v", err)
	}

	logStr := h.logBuf.String()
	if !strings.Contains(logStr, "decision=allow") {
		t.Errorf("expected decision=allow in log, got: %s", logStr)
	}
	if !strings.Contains(logStr, "tool=screenshot") {
		t.Errorf("expected tool=screenshot in log, got: %s", logStr)
	}
	if !strings.Contains(logStr, "agent=test-agent") {
		t.Errorf("expected agent=test-agent in log, got: %s", logStr)
	}
	if !strings.Contains(logStr, "reason=allowed") {
		t.Errorf("expected reason=allowed in log, got: %s", logStr)
	}

	h.claudeWrite.Close()
	h.upToProxyWrite.Close()
}
