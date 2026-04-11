package main

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/mcp"
	zssh "github.com/zac15987/zpit/internal/ssh"
	"github.com/zac15987/zpit/internal/tui"
	"github.com/zac15987/zpit/internal/worktree"
)

//go:embed agents/clarifier.md
var clarifierAgentMD []byte

//go:embed agents/reviewer.md
var reviewerAgentMD []byte

//go:embed agents/task-runner.md
var taskRunnerMD []byte

//go:embed agents/efficiency.md
var efficiencyMD []byte

//go:embed docs/agent-guidelines.md
var agentGuidelinesMD []byte

//go:embed docs/code-construction-principles.md
var codeConstructionPrinciplesMD []byte

//go:embed hooks/path-guard.sh
var pathGuardSH []byte

//go:embed hooks/bash-firewall.sh
var bashFirewallSH []byte

//go:embed hooks/git-guard.sh
var gitGuardSH []byte

//go:embed hooks/zpit-env.cmd
var zpitEnvCMD []byte

//go:embed hooks/zpit-env.ps1
var zpitEnvPS1 []byte

//go:embed hooks/zpit-exit.cmd
var zpitExitCMD []byte

//go:embed hooks/zpit-exit.ps1
var zpitExitPS1 []byte

//go:embed hooks/notify-permission.sh
var notifyPermissionSH []byte

func main() {
	// Subcommand routing via os.Args.
	subcmd := ""
	if len(os.Args) > 1 {
		subcmd = os.Args[1]
	}

	switch subcmd {
	case "":
		runLocalTUI()
	case "serve":
		runServe()
	case "connect":
		runConnect()
	case "serve-channel":
		runServeChannel()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", subcmd)
		fmt.Fprintln(os.Stderr, "Usage: zpit [serve|connect|serve-channel]")
		os.Exit(1)
	}
}

// runLocalTUI runs the local interactive TUI, or auto-serve mode if configured.
func runLocalTUI() {
	cfg, logFile := loadConfigAndLog()
	if logFile != nil {
		defer logFile.Close()
	}

	if cfg.SSH.AutoServe {
		runAutoServe(cfg, logFile)
		return
	}

	appState := tui.NewAppState(cfg, clarifierAgentMD, reviewerAgentMD, taskRunnerMD, efficiencyMD, agentGuidelinesMD, codeConstructionPrinciplesMD, buildHookScripts(), logFile)
	p := tea.NewProgram(
		tui.NewModel(appState),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

// runServe starts the headless SSH server daemon.
func runServe() {
	cfg, logFile := loadConfigAndLog()
	if logFile != nil {
		defer logFile.Close()
	}

	// Create a combined writer for both stdout and the daily log file.
	var combined io.Writer = os.Stdout
	if logFile != nil {
		combined = io.MultiWriter(os.Stdout, logFile)
	}
	logger := log.New(combined, "", log.LstdFlags)

	// AppState also gets the combined writer so all state transitions are logged to both.
	appState := tui.NewAppState(cfg, clarifierAgentMD, reviewerAgentMD, taskRunnerMD, efficiencyMD, agentGuidelinesMD, codeConstructionPrinciplesMD, buildHookScripts(), combined)

	if err := zssh.StartServer(appState, cfg.SSH, logger); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runConnect execs the system ssh client to connect to the local Zpit SSH server.
func runConnect() {
	cfg := loadConfig()
	port := strconv.Itoa(cfg.SSH.Port)

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: ssh not found in PATH")
		fmt.Fprintln(os.Stderr, "Install OpenSSH or add it to your PATH.")
		os.Exit(1)
	}

	// exec replaces the current process with ssh.
	args := []string{"ssh", "localhost", "-p", port}
	fmt.Printf("Connecting to zpit server on port %s...\n", port)

	cmd := exec.Command(sshPath, args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ssh exited: %v\n", err)
		os.Exit(1)
	}
}

// runAutoServe starts an SSH server in-process, connects via SSH client,
// and shuts down the server when the client disconnects.
func runAutoServe(cfg *config.Config, logFile *os.File) {
	// Logs go to logFile only — stdout belongs to the SSH client subprocess.
	var logWriter io.Writer = io.Discard
	if logFile != nil {
		logWriter = logFile
	}
	logger := log.New(logWriter, "", log.LstdFlags)

	logger.Println("auto_serve: starting")

	appState := tui.NewAppState(cfg, clarifierAgentMD, reviewerAgentMD, taskRunnerMD, efficiencyMD, agentGuidelinesMD, codeConstructionPrinciplesMD, buildHookScripts(), logWriter)

	// Start SSH server (non-blocking — port is ready on return).
	handle, err := zssh.StartServerAsync(appState, cfg.SSH, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auto_serve: failed to start SSH server: %v\n", err)
		os.Exit(1)
	}
	logger.Println("auto_serve: SSH server ready")

	// Ensure server is shut down on all exit paths.
	defer func() {
		logger.Println("auto_serve: shutting down SSH server")
		if err := handle.Shutdown(); err != nil {
			logger.Printf("auto_serve: shutdown error: %v", err)
		}
		logger.Println("auto_serve: done")
	}()

	// Locate ssh client.
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		fmt.Fprintln(os.Stderr, "auto_serve: ssh not found in PATH")
		fmt.Fprintln(os.Stderr, "Install OpenSSH or add it to your PATH.")
		return // deferred shutdown will clean up the server
	}

	// Build SSH command with host-key verification disabled (connecting to our own server).
	port := strconv.Itoa(cfg.SSH.Port)
	nullFile := "/dev/null"
	if runtime.GOOS == "windows" {
		nullFile = "NUL"
	}
	logger.Printf("auto_serve: connecting to localhost:%s", port)

	cmd := exec.Command(sshPath, "localhost", "-p", port,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile="+nullFile,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Intercept signals so Ctrl+C goes to the SSH client, not our process.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Forward signals to SSH client subprocess.
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigCh:
			logger.Printf("auto_serve: received %s, terminating SSH client", sig)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		case <-done:
		}
	}()

	// Run SSH client (blocks until disconnect).
	clientErr := cmd.Run()

	// Stop intercepting signals.
	close(done)
	signal.Stop(sigCh)

	if clientErr != nil {
		logger.Printf("auto_serve: SSH client exited: %v", clientErr)
	} else {
		logger.Println("auto_serve: SSH client disconnected")
	}

	// Check if server encountered an error during the session (non-blocking).
	select {
	case err := <-handle.ErrCh():
		if err != nil {
			logger.Printf("auto_serve: server error during session: %v", err)
		}
	default:
		// Server still running — will be shut down by deferred Shutdown.
	}

	// Deferred shutdown handles the rest.
}

// runServeChannel starts the Channel MCP stdio server for cross-worktree agent communication.
// Reads ZPIT_BROKER_URL, ZPIT_PROJECT_ID, ZPIT_ISSUE_ID from environment.
func runServeChannel() {
	if err := mcp.RunFromEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// loadConfig loads and returns the Zpit config, handling first-run template creation.
func loadConfig() *config.Config {
	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if envPath := os.Getenv("ZPIT_CONFIG"); envPath != "" {
		cfgPath = envPath
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := config.WriteTemplate(cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created config template at: %s\n", cfgPath)
		fmt.Println("Please edit it and add your projects, then run zpit again.")
		os.Exit(0)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config at: %s\n", cfgPath)
		os.Exit(1)
	}

	locale.SetLanguage(cfg.Language)
	return cfg
}

// loadConfigAndLog loads config and opens the daily log file.
func loadConfigAndLog() (*config.Config, *os.File) {
	cfg := loadConfig()

	baseDir, _ := config.BaseDir()
	logDir := filepath.Join(baseDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	today := time.Now().Format("2006-01-02")
	logFile, err := os.OpenFile(
		filepath.Join(logDir, "zpit-"+today+".log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open log file: %v\n", err)
		logFile = nil
	}
	cleanOldLogs(logDir, 30)

	return cfg, logFile
}

// buildHookScripts returns the embedded hook scripts.
func buildHookScripts() worktree.HookScripts {
	return worktree.HookScripts{
		PathGuard:        pathGuardSH,
		BashFirewall:     bashFirewallSH,
		GitGuard:         gitGuardSH,
		EnvWrapper:       zpitEnvCMD,
		EnvWrapperPS1:    zpitEnvPS1,
		ExitWrapper:      zpitExitCMD,
		ExitWrapperPS1:   zpitExitPS1,
		NotifyPermission: notifyPermissionSH,
	}
}

// cleanOldLogs removes log files older than maxDays from the log directory.
func cleanOldLogs(dir string, maxDays int) {
	cutoff := time.Now().AddDate(0, 0, -maxDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "zpit-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateStr := strings.TrimPrefix(strings.TrimSuffix(name, ".log"), "zpit-")
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
