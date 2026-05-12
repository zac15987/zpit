package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zac15987/zpit/internal/config"
)

// keyboardWritingTools is the set of tool names for which the proxy overrides
// focus_strategy with policy.KeyboardFocusStrategy (AC-5).
var keyboardWritingTools = map[string]bool{
	"type":      true,
	"key":       true,
	"hold_key":  true,
	"set_value": true,
	"fill_form": true,
}

// DesktopLogger writes lines in the AC-5/AC-10 format:
//
//	[yyyy-MM-dd HH:mm:ss.fff] [<Level>] [desktop-proxy] <body>
//
// Concurrency-safe. Uses its own timestamp formatter rather than log.LstdFlags
// because log.Logger's format does not match the required AC millisecond precision.
type DesktopLogger struct {
	mu sync.Mutex
	w  io.Writer
}

// NewDesktopLogger constructs a DesktopLogger that writes to w.
func NewDesktopLogger(w io.Writer) *DesktopLogger {
	return &DesktopLogger{w: w}
}

func (l *DesktopLogger) log(level, format string, args ...any) {
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	body := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] [%s] [desktop-proxy] %s\n", ts, level, body)
	l.mu.Lock()
	_, _ = io.WriteString(l.w, line)
	l.mu.Unlock()
}

// Infof writes a line at Info level.
func (l *DesktopLogger) Infof(format string, args ...any) {
	l.log("Info", format, args...)
}

// Warnf writes a line at Warn level.
func (l *DesktopLogger) Warnf(format string, args ...any) {
	l.log("Warn", format, args...)
}

// Errorf writes a line at Error level.
func (l *DesktopLogger) Errorf(format string, args ...any) {
	l.log("Error", format, args...)
}

// Proxy runs the stdio MCP forwarder. It reads JSON-RPC frames from `in`,
// writes responses to `out`, and forwards approved tool calls to an
// upstream `npx @zavora-ai/computer-use-mcp` subprocess managed by Proxy.
//
// The proxy lifecycle is tied to ctx — cancelling the context kills the
// upstream subprocess and shuts down both forwarder goroutines.
type Proxy struct {
	policy    Policy
	logger    *DesktopLogger
	in        io.Reader // Claude Code's stdout -> us
	out       io.Writer // us -> Claude Code's stdin
	agentName string    // from ZPIT_AGENT_NAME env, used in log lines

	outMu sync.Mutex // protects concurrent writes to out

	// Set after Run is called (subprocess lifecycle).
	cmd       *exec.Cmd
	upstream  io.WriteCloser // upstream stdin
	upstreamR io.ReadCloser  // upstream stdout
}

// NewProxy constructs a Proxy bound to the given streams and policy.
// The `agentName` is included in every decision log line (AC-10).
func NewProxy(in io.Reader, out io.Writer, policy Policy, logger *DesktopLogger, agentName string) *Proxy {
	return &Proxy{
		policy:    policy,
		logger:    logger,
		in:        in,
		out:       out,
		agentName: agentName,
	}
}

// Run starts the upstream subprocess and blocks forwarding frames until
// either ctx is cancelled, in EOFs, or the upstream subprocess exits.
// On exit, kills the upstream subprocess and waits for it.
// Returns the first non-EOF error encountered, or nil on clean shutdown.
func (p *Proxy) Run(ctx context.Context) error {
	p.logger.Infof("desktop-proxy: starting upstream subprocess")

	npxPath, npxArgs, err := resolveNpx()
	if err != nil {
		p.logger.Errorf("desktop-proxy: resolveNpx failed: %v", err)
		return fmt.Errorf("desktop-proxy: %w", err)
	}

	p.cmd = exec.CommandContext(ctx, npxPath, npxArgs...)

	upstreamIn, err := p.cmd.StdinPipe()
	if err != nil {
		p.logger.Errorf("desktop-proxy: StdinPipe error: %v", err)
		return fmt.Errorf("desktop-proxy: stdin pipe: %w", err)
	}
	upstreamOut, err := p.cmd.StdoutPipe()
	if err != nil {
		p.logger.Errorf("desktop-proxy: StdoutPipe error: %v", err)
		return fmt.Errorf("desktop-proxy: stdout pipe: %w", err)
	}
	// Tee upstream stderr to our logger at Warn level.
	p.cmd.Stderr = &desktopStderrWriter{logger: p.logger}

	if err := p.cmd.Start(); err != nil {
		p.logger.Errorf("desktop-proxy: subprocess start error: %v", err)
		return fmt.Errorf("desktop-proxy: start subprocess: %w", err)
	}
	p.logger.Infof("desktop-proxy: upstream subprocess started pid=%d", p.cmd.Process.Pid)

	p.upstream = upstreamIn
	p.upstreamR = upstreamOut

	runErr := p.runWithUpstream(ctx, upstreamIn, upstreamOut)

	// Wait for subprocess to exit.
	_ = p.cmd.Wait()
	p.logger.Infof("desktop-proxy: upstream subprocess exited reason=%v", runErr)
	return runErr
}

// runWithUpstream is the testable core that takes the upstream pipes directly
// instead of spawning a subprocess. Run() wraps this.
func (p *Proxy) runWithUpstream(ctx context.Context, upstreamIn io.WriteCloser, upstreamOut io.ReadCloser) error {
	p.logger.Infof("desktop-proxy: runWithUpstream starting")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	// Goroutine A: read from Claude Code (p.in) → policy gate → forward to upstream or synthesize denial.
	go func() {
		err := p.forwardFromClaude(ctx, upstreamIn)
		cancel() // signal the other goroutine to stop
		errCh <- err
	}()

	// Goroutine B: read from upstream → filter tools/list → forward to Claude Code (p.out).
	go func() {
		err := p.forwardFromUpstream(ctx, upstreamOut)
		cancel()
		errCh <- err
	}()

	// Wait for both goroutines to finish.
	var firstErr error
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	p.logger.Infof("desktop-proxy: runWithUpstream stopped reason=%v", firstErr)
	return firstErr
}

// forwardFromClaude reads JSON-RPC frames from Claude Code (p.in),
// applies the policy gate for tools/call, and forwards to upstreamIn.
// Non-tools/call frames are passed through verbatim.
func (p *Proxy) forwardFromClaude(ctx context.Context, upstreamIn io.WriteCloser) error {
	sc := bufio.NewScanner(p.in)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024) // 16 MB max line for screenshots

	for sc.Scan() {
		if ctx.Err() != nil {
			return context.Canceled
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}

		var frame map[string]any
		if err := json.Unmarshal(line, &frame); err != nil {
			// Non-JSON line — pass through verbatim (belt-and-suspenders).
			p.writeToOut(line)
			continue
		}

		method, _ := frame["method"].(string)
		if method == "tools/call" {
			p.handleToolsCall(ctx, frame, upstreamIn)
		} else {
			// All other methods (initialize, notifications/*, ping, etc.) pass through verbatim.
			p.forwardToUpstream(upstreamIn, line)
		}
	}

	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

// handleToolsCall applies the policy gate for a tools/call request.
// If denied, writes a denial response directly to p.out.
// If allowed (and possibly mutated), forwards to upstreamIn.
func (p *Proxy) handleToolsCall(_ context.Context, frame map[string]any, upstreamIn io.WriteCloser) {
	id := frame["id"]
	params, _ := frame["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	toolName, _ := params["name"].(string)
	arguments, _ := params["arguments"].(map[string]any)
	if arguments == nil {
		arguments = map[string]any{}
	}

	// --- Policy checks ---

	// 1. Tool allowlist check (AC-2).
	if !p.policy.IsToolAllowed(toolName) {
		reason := "tool_not_allowed"
		p.logDecision("deny", toolName, reason)
		p.writeDenial(id, fmt.Sprintf("denied: tool '%s' is not in the desktop agent allowlist (see ~/.zpit/desktop-policy.toml)", toolName))
		return
	}

	// 2. run_script gating (AC-6 / run_script_disabled).
	if toolName == "run_script" && !p.policy.AllowRunScript {
		reason := "run_script_disabled"
		p.logDecision("deny", toolName, reason)
		p.writeDenial(id, fmt.Sprintf("denied: tool '%s' is not in the desktop agent allowlist (see ~/.zpit/desktop-policy.toml)", toolName))
		return
	}

	// 3. DenyKeys check for key/hold_key (AC-3).
	if toolName == "key" || toolName == "hold_key" {
		text, _ := arguments["text"].(string)
		if matched := p.policy.MatchDenyKey(text); matched != "" {
			reason := "key_denied"
			p.logDecision("deny", toolName, reason)
			p.writeDenial(id, fmt.Sprintf("denied: key combo '%s' matches deny_keys entry '%s'", text, matched))
			return
		}
	}

	// 4. Bundle check (AC-4).
	if targetApp, ok := arguments["target_app"].(string); ok && targetApp != "" {
		if !p.policy.IsBundleAllowed(targetApp) {
			reason := "bundle_denied"
			p.logDecision("deny", toolName, reason)
			p.writeDenial(id, fmt.Sprintf("denied: target_app '%s' is not in allow_bundles", targetApp))
			return
		}
	}

	// 5. Keyboard focus strategy override (AC-5).
	if keyboardWritingTools[toolName] {
		newStrat := p.policy.KeyboardFocusStrategy
		if oldStrat, ok := arguments["focus_strategy"].(string); ok && oldStrat != newStrat {
			p.logger.Infof("forced focus_strategy: %s (agent supplied: %s) tool=%s", newStrat, oldStrat, toolName)
		}
		arguments["focus_strategy"] = newStrat
		params["arguments"] = arguments
		frame["params"] = params
	}

	// All checks passed.
	p.logDecision("allow", toolName, "allowed")

	// Re-marshal (may have mutated focus_strategy).
	data, err := json.Marshal(frame)
	if err != nil {
		p.logger.Errorf("desktop-proxy: re-marshal tools/call error: %v", err)
		p.writeDenial(id, "internal error: failed to re-marshal request")
		return
	}
	p.forwardToUpstream(upstreamIn, data)
}

// forwardFromUpstream reads JSON-RPC frames from the upstream subprocess,
// filters tools/list responses, and forwards to p.out (Claude Code).
func (p *Proxy) forwardFromUpstream(_ context.Context, upstreamOut io.ReadCloser) error {
	sc := bufio.NewScanner(upstreamOut)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024) // 16 MB for screenshots

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}

		var frame map[string]any
		if err := json.Unmarshal(line, &frame); err != nil {
			// Non-JSON — pass through verbatim.
			p.writeToOut(line)
			continue
		}

		// Check if this is a tools/list response (has "result" with "tools" array).
		if result, ok := frame["result"].(map[string]any); ok {
			if tools, ok := result["tools"]; ok {
				filtered := p.filterToolsList(tools)
				result["tools"] = filtered
				frame["result"] = result
				data, err := json.Marshal(frame)
				if err != nil {
					p.logger.Errorf("desktop-proxy: re-marshal tools/list error: %v", err)
					p.writeToOut(line)
					continue
				}
				p.writeToOut(data)
				continue
			}
		}

		p.writeToOut(line)
	}

	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

// filterToolsList filters the tools array from an upstream tools/list response
// to only include tools in policy.AllowedTools.
func (p *Proxy) filterToolsList(tools any) []any {
	toolsSlice, ok := tools.([]any)
	if !ok {
		return nil
	}
	allowed := make(map[string]bool, len(p.policy.AllowedTools))
	for _, t := range p.policy.AllowedTools {
		allowed[t] = true
	}
	var out []any
	for _, entry := range toolsSlice {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		if allowed[name] {
			out = append(out, entry)
		}
	}
	return out
}

// logDecision logs AC-10 format: decision=<allow|deny> tool=<name> agent=<agentName> reason=<reason>.
func (p *Proxy) logDecision(decision, tool, reason string) {
	p.logger.Infof("decision=%s tool=%s agent=%s reason=%s", decision, tool, p.agentName, reason)
}

// writeDenial writes a CallToolResult with isError: true and the given text to p.out.
// The id is copied from the incoming request.
func (p *Proxy) writeDenial(id any, text string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []any{
				map[string]any{
					"type": "text",
					"text": text,
				},
			},
			"isError": true,
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		p.logger.Errorf("desktop-proxy: writeDenial marshal error: %v", err)
		return
	}
	p.writeToOut(data)
}

// writeToOut writes a newline-terminated frame to p.out under the output mutex.
func (p *Proxy) writeToOut(data []byte) {
	p.outMu.Lock()
	_, _ = p.out.Write(append(data, '\n'))
	p.outMu.Unlock()
}

// forwardToUpstream writes a newline-terminated frame to upstreamIn.
func (p *Proxy) forwardToUpstream(upstreamIn io.Writer, data []byte) {
	_, err := upstreamIn.Write(append(data, '\n'))
	if err != nil {
		p.logger.Warnf("desktop-proxy: write to upstream error: %v", err)
	}
}

// resolveNpx finds the npx binary for the current platform.
// On Windows, falls back to cmd.exe /c npx ... when LookPath cannot find npx directly.
func resolveNpx() (string, []string, error) {
	if p, err := exec.LookPath("npx"); err == nil {
		return p, []string{"--yes", "--prefer-offline", "@zavora-ai/computer-use-mcp"}, nil
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("cmd.exe"); err == nil {
			return p, []string{"/c", "npx", "--yes", "--prefer-offline", "@zavora-ai/computer-use-mcp"}, nil
		}
	}
	return "", nil, errors.New("npx not found in PATH — install Node.js to use the desktop agent")
}

// desktopStderrWriter adapts a DesktopLogger for use as subprocess stderr.
type desktopStderrWriter struct {
	logger *DesktopLogger
}

func (w *desktopStderrWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\r\n")
	if msg != "" {
		w.logger.Warnf("upstream stderr: %s", msg)
	}
	return len(p), nil
}

// RunFromEnv is the entry point used by `zpit serve-desktop-proxy`.
// Reads policy from ~/.zpit/desktop-policy.toml (auto-create on first run per AC-1),
// constructs a logger writing to ~/.zpit/logs/zpit-YYYY-MM-DD.log (daily-rotation),
// reads ZPIT_AGENT_NAME from env, and runs the proxy on os.Stdin / os.Stdout with
// a context that cancels on SIGINT/SIGTERM.
func RunFromEnv(ctx context.Context) error {
	baseDir, err := config.BaseDir()
	if err != nil {
		return fmt.Errorf("desktop-proxy: resolve base dir: %w", err)
	}

	// Open daily-rotation log file (matching loadConfigAndLog pattern in main.go).
	logDir := filepath.Join(baseDir, "logs")
	if mkErr := os.MkdirAll(logDir, 0o755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "desktop-proxy: warning: cannot create log dir: %v\n", mkErr)
	}
	today := time.Now().Format("2006-01-02")
	logFile, err := os.OpenFile(
		filepath.Join(logDir, "zpit-"+today+".log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "desktop-proxy: warning: cannot open log file: %v\n", err)
	}

	var logDest io.Writer
	if logFile != nil {
		logDest = io.MultiWriter(logFile, os.Stderr)
		defer logFile.Close()
	} else {
		logDest = os.Stderr
	}

	deskLogger := NewDesktopLogger(logDest)

	// stdlib logger for LoadPolicy (matches existing signature).
	stdLogger := log.New(logDest, "", 0)

	policyPath := filepath.Join(baseDir, PolicyFileName)
	policy, created, err := LoadPolicy(policyPath, stdLogger)
	if err != nil {
		deskLogger.Errorf("failed to load policy: %v", err)
		return fmt.Errorf("desktop-proxy: load policy: %w", err)
	}
	if created {
		deskLogger.Infof("policy file auto-created at %s", policyPath)
	}
	if valErr := policy.Validate(); valErr != nil {
		deskLogger.Errorf("invalid policy: %v", valErr)
		return fmt.Errorf("desktop-proxy: invalid policy: %w", valErr)
	}

	agentName := os.Getenv("ZPIT_AGENT_NAME")

	deskLogger.Infof("starting proxy agent=%s policy_path=%s", agentName, policyPath)

	// Set up signal-based cancellation (mirrors internal/mcp pattern).
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	proxy := NewProxy(os.Stdin, os.Stdout, policy, deskLogger, agentName)
	runErr := proxy.Run(ctx)
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, io.EOF) {
		deskLogger.Errorf("proxy exited with error: %v", runErr)
		return runErr
	}
	deskLogger.Infof("proxy stopped cleanly")
	return nil
}
