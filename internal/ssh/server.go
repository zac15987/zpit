package ssh

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	wishbt "github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/logging"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/tui"
)

const shutdownTimeout = 30 * time.Second

// ServerHandle provides a handle to a running SSH server for lifecycle management.
type ServerHandle struct {
	srv         *ssh.Server
	errCh       chan error
	logger      *log.Logger
	listenAddr  string   // actual listen address for banner printing
	hostKeyPath string   // host key path for banner printing
	authMethods []string // enabled auth method names for banner printing
}

// Wait blocks until the server encounters a fatal error or shuts down cleanly.
// Returns nil on clean shutdown.
func (h *ServerHandle) Wait() error {
	return <-h.errCh
}

// ErrCh returns the error channel for non-blocking reads.
// The channel receives at most one error, then closes.
func (h *ServerHandle) ErrCh() <-chan error {
	return h.errCh
}

// Shutdown gracefully shuts down the SSH server with the standard 30s timeout.
func (h *ServerHandle) Shutdown() error {
	h.logger.Println("ssh server: shutting down (30s timeout)")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("SSH server shutdown: %w", err)
	}
	h.logger.Println("ssh server: stopped")
	return nil
}

// StartServerAsync creates and starts the SSH server without blocking.
// It returns a ServerHandle once the server is actively listening,
// or an error if setup fails. The caller manages lifecycle via the handle.
func StartServerAsync(appState *tui.AppState, sshCfg config.SSHConfig, logger *log.Logger) (*ServerHandle, error) {
	logger.Println("ssh server: starting")

	if err := sshCfg.ResolveSSHPaths(); err != nil {
		return nil, fmt.Errorf("resolving SSH paths: %w", err)
	}

	// Ensure host key directory exists.
	if dir := hostKeyDir(sshCfg.HostKeyPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("creating host key directory: %w", err)
		}
	}

	// Build server options.
	addr := net.JoinHostPort(sshCfg.Host, strconv.Itoa(sshCfg.Port))
	opts := []ssh.Option{
		wish.WithAddress(addr),
		wish.WithHostKeyPath(sshCfg.HostKeyPath),
		wish.WithMiddleware(
			wishbt.Middleware(teaHandler(appState)),
			activeterm.Middleware(),
			logging.MiddlewareWithLogger(logger),
		),
	}

	// Configure authentication methods.
	ac := configureAuth(sshCfg, logger)
	if !ac.pubKey && !ac.password {
		return nil, errors.New("no SSH auth methods available: configure authorized_keys or password_env")
	}
	opts = ac.applyTo(opts)

	srv, err := wish.NewServer(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating SSH server: %w", err)
	}

	// Log startup info (logger only — no stdout).
	logStartupInfo(addr, sshCfg, ac, logger)

	// Run server-init (session scan, .gitignore, provider check).
	tui.RunServerInit(appState)
	logger.Println("ssh server: server-init complete")

	// Bind listener explicitly so the port is ready before we return.
	// Same pattern as broker.go:41-67.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh server: listen on %s: %w", addr, err)
	}
	logger.Printf("ssh server: listening on %s", ln.Addr().String())

	// Start serving in background.
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	return &ServerHandle{
		srv:         srv,
		errCh:       errCh,
		logger:      logger,
		listenAddr:  ln.Addr().String(),
		hostKeyPath: sshCfg.HostKeyPath,
		authMethods: authMethodNames(ac),
	}, nil
}

// StartServer creates and runs a Wish SSH server with the given AppState.
// It blocks until SIGINT/SIGTERM is received, then shuts down gracefully.
func StartServer(appState *tui.AppState, sshCfg config.SSHConfig, logger *log.Logger) error {
	handle, err := StartServerAsync(appState, sshCfg, logger)
	if err != nil {
		return err
	}

	// Print user-facing banner to stdout (only for headless "zpit serve" mode).
	printBanner(handle.listenAddr, handle.hostKeyPath, handle.authMethods)

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-handle.errCh:
		if err != nil {
			return fmt.Errorf("SSH server error: %w", err)
		}
	case sig := <-sigCh:
		logger.Printf("ssh server: received %s, shutting down", sig)
	}

	return handle.Shutdown()
}

// teaHandler returns a wish bubbletea.Handler that creates a new Model per SSH session.
func teaHandler(appState *tui.AppState) wishbt.Handler {
	return func(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
		model := tui.NewModelWithState(appState, true)
		return model, []tea.ProgramOption{
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		}
	}
}

// authConfig tracks which auth methods are enabled.
type authConfig struct {
	pubKey   bool
	password bool
	opts     []ssh.Option
}

func (a authConfig) applyTo(base []ssh.Option) []ssh.Option {
	return append(base, a.opts...)
}

// configureAuth sets up SSH authentication based on the SSHConfig.
// Returns the number of enabled auth methods.
func configureAuth(sshCfg config.SSHConfig, logger *log.Logger) authConfig {
	var ac authConfig

	// Public key auth via authorized_keys.
	akPath := sshCfg.AuthorizedKeysPath
	if _, err := os.Stat(akPath); err == nil {
		ac.opts = append(ac.opts, wish.WithAuthorizedKeys(akPath))
		ac.pubKey = true
		logger.Printf("ssh server: public key auth enabled (authorized_keys: %s)", akPath)
	} else {
		logger.Printf("ssh server: public key auth disabled (authorized_keys not found: %s)", akPath)
	}

	// Password auth via environment variable.
	if sshCfg.PasswordEnv != "" {
		password := os.Getenv(sshCfg.PasswordEnv)
		if password != "" {
			ac.opts = append(ac.opts, wish.WithPasswordAuth(
				func(_ ssh.Context, pass string) bool {
					return pass == password
				},
			))
			ac.password = true
			logger.Printf("ssh server: password auth enabled (env: %s)", sshCfg.PasswordEnv)
		} else {
			logger.Printf("ssh server: password auth disabled (env var %s is empty or unset)", sshCfg.PasswordEnv)
		}
	} else {
		logger.Println("ssh server: password auth disabled (password_env not configured)")
	}

	return ac
}

// logStartupInfo logs server configuration to the logger (no stdout).
func logStartupInfo(addr string, sshCfg config.SSHConfig, ac authConfig, logger *log.Logger) {
	logger.Printf("ssh server: listening on %s", addr)
	logger.Printf("ssh server: host key path: %s", sshCfg.HostKeyPath)
	logger.Printf("ssh server: auth methods: %v", authMethodNames(ac))
}

// authMethodNames returns the enabled auth method names from an authConfig.
func authMethodNames(ac authConfig) []string {
	var methods []string
	if ac.pubKey {
		methods = append(methods, "public_key")
	}
	if ac.password {
		methods = append(methods, "password")
	}
	return methods
}

// printBanner prints the user-facing startup banner to stdout.
// Called only by StartServer (headless "zpit serve" mode).
func printBanner(addr, hostKeyPath string, methods []string) {
	fmt.Printf("Zpit SSH server listening on %s\n", addr)
	fmt.Printf("  Host key: %s\n", hostKeyPath)
	fmt.Printf("  Auth: %v\n", methods)
	fmt.Println("  Press Ctrl+C to stop.")
}

// hostKeyDir returns the directory part of the host key path.
func hostKeyDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}
