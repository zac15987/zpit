package tui

import (
	"io"
	"log"
	"sync"
	"time"

	"github.com/zac15987/zpit/internal/broker"
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
//
// Mutable fields protected by mu: activeTerminals, loops, lastLivenessCheck, lastPermissionCheck, lastSessionScan.
// Read-only fields (safe without locks): cfg, env, projects, clients, embedded MDs, hookScripts, wtManager.
type AppState struct {
	// mu protects mutable shared fields: activeTerminals, loops,
	// lastLivenessCheck, lastPermissionCheck, lastSessionScan.
	mu sync.RWMutex

	// subMu protects the subscribers map independently of mu,
	// allowing NotifyAll to be called while mu is held.
	subMu       sync.Mutex
	subscribers map[int]chan struct{}
	nextSubID   int

	cfg      *config.Config
	env      platform.Environment
	notifier *notify.Notifier
	logger   *log.Logger

	projects        []config.ProjectConfig
	activeTerminals map[string]*ActiveTerminal

	lastLivenessCheck   time.Time
	lastPermissionCheck time.Time
	lastSessionScan     time.Time

	clients map[string]tracker.TrackerClient

	loops     map[string]*loop.LoopState
	wtManager *worktree.Manager
	broker    *broker.Broker // read-only after init per project; nil when channel disabled

	channelEvents map[string][]broker.Event   // projectID → events (mutable, protected by mu)
	channelSubs   map[string]<-chan broker.Event // projectID → subscription channel (mutable, protected by mu)

	clarifierMD                  []byte
	reviewerMD                   []byte
	agentGuidelinesMD            []byte
	codeConstructionPrinciplesMD []byte
	hookScripts                  worktree.HookScripts
}

// Lock acquires the write lock for mutable shared state.
func (s *AppState) Lock() { s.mu.Lock() }

// Unlock releases the write lock.
func (s *AppState) Unlock() { s.mu.Unlock() }

// RLock acquires a read lock for mutable shared state.
func (s *AppState) RLock() { s.mu.RLock() }

// RUnlock releases the read lock.
func (s *AppState) RUnlock() { s.mu.RUnlock() }

// Subscribe registers a new subscriber for state change notifications.
// Returns the subscriber ID and a receive-only channel (buffered size 1).
// The caller should listen on the channel and call Unsubscribe when done.
func (s *AppState) Subscribe() (int, <-chan struct{}) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan struct{}, 1)
	s.subscribers[id] = ch
	s.logger.Printf("subscriber registered: id=%d", id)
	return id, ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (s *AppState) Unsubscribe(id int) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if ch, ok := s.subscribers[id]; ok {
		close(ch)
		delete(s.subscribers, id)
		s.logger.Printf("subscriber unregistered: id=%d", id)
	}
}

// NotifyAll performs non-blocking sends to all subscriber channels.
// The buffered channel (size 1) coalesces rapid state changes into a single
// notification — if a subscriber already has a pending signal, the send is skipped.
// Safe to call while mu (write lock) is held, since subMu is independent.
func (s *AppState) NotifyAll() {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// AppendChannelEvent stores a channel event for the given project and notifies subscribers.
// Acquires write lock and calls NotifyAll.
func (s *AppState) AppendChannelEvent(projectID string, event broker.Event) {
	s.mu.Lock()
	s.channelEvents[projectID] = append(s.channelEvents[projectID], event)
	s.NotifyAll()
	s.mu.Unlock()
}

// ChannelEvents returns a copy of channel events for the given project.
// Acquires read lock.
func (s *AppState) ChannelEvents(projectID string) []broker.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.channelEvents[projectID]
	if len(src) == 0 {
		return nil
	}
	cp := make([]broker.Event, len(src))
	copy(cp, src)
	return cp
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

	// Start broker if at least one project has channel_enabled = true.
	var b *broker.Broker
	channelNeeded := false
	for _, p := range cfg.Projects {
		if p.ChannelEnabled {
			channelNeeded = true
			break
		}
	}
	if channelNeeded {
		var err error
		b, err = broker.New(logger, cfg.BrokerPort)
		if err != nil {
			logger.Printf("broker: listen failed on port %d: %v", cfg.BrokerPort, err)
		} else {
			logger.Printf("broker: started on %s", b.Addr())
		}
	}

	return &AppState{
		cfg:                          cfg,
		env:                          platform.Detect(),
		notifier:                     notify.NewNotifier(cfg.Notification, logger),
		logger:                       logger,
		projects:                     cfg.Projects,
		activeTerminals:              make(map[string]*ActiveTerminal),
		clients:                      clients,
		loops:                        make(map[string]*loop.LoopState),
		wtManager:                    worktree.NewManager(cfg.Worktree),
		broker:                       b, // nil when no project has channel_enabled or listen failed
		subscribers:                  make(map[int]chan struct{}),
		channelEvents:                make(map[string][]broker.Event),
		channelSubs:                  make(map[string]<-chan broker.Event),
		clarifierMD:                  clarifierMD,
		reviewerMD:                   reviewerMD,
		agentGuidelinesMD:            agentGuidelinesMD,
		codeConstructionPrinciplesMD: codeConstructionPrinciplesMD,
		hookScripts:                  hookScripts,
	}
}
