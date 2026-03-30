package tui

import (
	"io"
	"log"
	"time"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/notify"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/worktree"
)

// AppState holds shared application state common across all connected clients.
// Multiple tea.Program instances (local TUI + SSH remote) share a single *AppState pointer,
// ensuring active terminals, loop progress, and agent states remain consistent.
type AppState struct {
	cfg      *config.Config
	env      platform.Environment
	notifier *notify.Notifier
	logger   *log.Logger

	projects        []config.ProjectConfig
	activeTerminals map[string]*ActiveTerminal

	lastLivenessCheck   time.Time
	lastPermissionCheck time.Time

	clients map[string]tracker.TrackerClient

	loops     map[string]*loop.LoopState
	wtManager *worktree.Manager

	clarifierMD                  []byte
	reviewerMD                   []byte
	agentGuidelinesMD            []byte
	codeConstructionPrinciplesMD []byte
	hookScripts                  worktree.HookScripts
}

// NewAppState creates and initializes a new AppState. logWriter may be nil (uses io.Discard).
// Initializes all maps and sets defaults equivalent to the former NewModel logic.
func NewAppState(
	cfg *config.Config,
	clarifierMD, reviewerMD, agentGuidelinesMD, codeConstructionPrinciplesMD []byte,
	hookScripts worktree.HookScripts,
	logWriter io.Writer,
) *AppState {
	if logWriter == nil {
		logWriter = io.Discard
	}
	logger := log.New(logWriter, "", log.LstdFlags)
	logger.Println("zpit started")

	clients := make(map[string]tracker.TrackerClient)
	for name, provider := range cfg.Providers.Tracker {
		client, err := tracker.NewClient(provider.Type, provider.URL, provider.TokenEnv)
		if err != nil {
			logger.Printf("tracker client %q init failed: %v", name, err)
			continue
		}
		clients[name] = client
	}

	return &AppState{
		cfg:                          cfg,
		env:                          platform.Detect(),
		notifier:                     notify.NewNotifier(cfg.Notification),
		logger:                       logger,
		projects:                     cfg.Projects,
		activeTerminals:              make(map[string]*ActiveTerminal),
		clients:                      clients,
		loops:                        make(map[string]*loop.LoopState),
		wtManager:                    worktree.NewManager(cfg.Worktree),
		clarifierMD:                  clarifierMD,
		reviewerMD:                   reviewerMD,
		agentGuidelinesMD:            agentGuidelinesMD,
		codeConstructionPrinciplesMD: codeConstructionPrinciplesMD,
		hookScripts:                  hookScripts,
	}
}
