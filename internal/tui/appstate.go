package tui

import (
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
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
// Mutable fields protected by mu: activeTerminals, loops, channelEvents, channelSubs, lastLivenessCheck, lastPermissionCheck, lastSessionScan.
// Read-only fields (safe without locks): cfg, env, projects, clients, embedded MDs, hookScripts, wtManager.
type AppState struct {
	// mu protects mutable shared fields: activeTerminals, loops,
	// channelEvents, channelSubs, lastLivenessCheck, lastPermissionCheck, lastSessionScan.
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
	taskRunnerMD                 []byte
	efficiencyMD                 []byte
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
	clarifierMD, reviewerMD, taskRunnerMD, efficiencyMD, agentGuidelinesMD, codeConstructionPrinciplesMD []byte,
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
		taskRunnerMD:                 taskRunnerMD,
		efficiencyMD:                 efficiencyMD,
		agentGuidelinesMD:            agentGuidelinesMD,
		codeConstructionPrinciplesMD: codeConstructionPrinciplesMD,
		hookScripts:                  hookScripts,
	}
}

// ApplyConfig applies a new configuration to all subsystems.
// Updates cfg pointer, calls sub-component UpdateConfig methods,
// manages channel subscriptions, and performs broker lazy start.
// Acquires write lock. Caller must NOT hold any locks.
// Returns a list of tea.Cmd to execute after the call (channel subscribe/unsubscribe).
func (s *AppState) ApplyConfig(newCfg *config.Config, diff config.ConfigDiff) []tea.Cmd {
	s.logger.Printf("config: ApplyConfig entry hot=%v restart=%v", diff.HotReload, diff.RestartRequired)

	// Apply locale change outside lock (locale has its own sync).
	for _, field := range diff.HotReload {
		if field == "language" {
			locale.SetLanguage(newCfg.Language)
			break
		}
	}

	// Update sub-components that have their own thread-safety.
	for _, field := range diff.HotReload {
		switch field {
		case "notification":
			s.notifier.UpdateConfig(newCfg.Notification)
		}
	}

	s.mu.Lock()
	oldCfg := s.cfg
	s.cfg = newCfg
	s.projects = newCfg.Projects

	// Update worktree manager config (safe under our lock since
	// no concurrent worktree operations can run while we hold it).
	for _, field := range diff.HotReload {
		switch field {
		case "worktree":
			s.wtManager.UpdateConfig(newCfg.Worktree)
		}
	}

	// Collect channel subscribe/unsubscribe actions.
	var cmds []tea.Cmd

	// Handle channel config changes: compare old vs new per-project.
	for _, field := range diff.HotReload {
		if field != "channel" {
			continue
		}

		oldProjectMap := make(map[string]config.ProjectConfig)
		for _, p := range oldCfg.Projects {
			oldProjectMap[p.ID] = p
		}

		for _, newProj := range newCfg.Projects {
			oldProj, existed := oldProjectMap[newProj.ID]

			// Channel toggled ON: may need broker lazy start + subscribe.
			if newProj.ChannelEnabled && (!existed || !oldProj.ChannelEnabled) {
				// Broker lazy start if needed.
				if s.broker == nil {
					b, err := broker.New(s.logger, newCfg.BrokerPort)
					if err != nil {
						s.logger.Printf("broker: lazy start failed on port %d: %v", newCfg.BrokerPort, err)
						// Revert channel_enabled in the config since broker failed.
						for i := range s.projects {
							if s.projects[i].ID == newProj.ID {
								s.projects[i].ChannelEnabled = false
								break
							}
						}
						continue
					}
					s.broker = b
					s.logger.Printf("broker: lazy started on %s", b.Addr())
				}
				// Subscribe if not already subscribed.
				if _, subscribed := s.channelSubs[newProj.ID]; !subscribed {
					projectID := newProj.ID
					bus := s.broker.Events()
					ch := bus.Subscribe(projectID)
					s.channelSubs[projectID] = ch
					s.logger.Printf("channel: subscribed to EventBus for project=%s (config reload)", projectID)
					cmds = append(cmds, func() tea.Msg {
						event, ok := <-ch
						if !ok {
							return ChannelSubscribedMsg{ProjectID: projectID, Err: errChannelClosed}
						}
						return ChannelEventMsg{ProjectID: projectID, Event: event}
					})
				}
				// Subscribe listen projects.
				for _, lp := range newProj.ChannelListen {
					if _, subscribed := s.channelSubs[lp]; !subscribed {
						listenProject := lp
						bus := s.broker.Events()
						ch := bus.Subscribe(listenProject)
						s.channelSubs[listenProject] = ch
						s.logger.Printf("channel: subscribed to EventBus for project=%s (listen, config reload)", listenProject)
						cmds = append(cmds, func() tea.Msg {
							event, ok := <-ch
							if !ok {
								return ChannelSubscribedMsg{ProjectID: listenProject, Err: errChannelClosed}
							}
							return ChannelEventMsg{ProjectID: listenProject, Event: event}
						})
					}
				}
			}

			// Channel toggled OFF: unsubscribe.
			if !newProj.ChannelEnabled && existed && oldProj.ChannelEnabled {
				if ch, subscribed := s.channelSubs[newProj.ID]; subscribed {
					delete(s.channelSubs, newProj.ID)
					if s.broker != nil {
						s.broker.Events().Unsubscribe(newProj.ID, ch)
					}
					s.logger.Printf("channel: unsubscribed from EventBus for project=%s (config reload)", newProj.ID)
				}
			}

			// channel_listen changes: subscribe new, unsubscribe removed.
			if existed && newProj.ChannelEnabled {
				oldListenSet := make(map[string]bool)
				for _, lp := range oldProj.ChannelListen {
					oldListenSet[lp] = true
				}
				newListenSet := make(map[string]bool)
				for _, lp := range newProj.ChannelListen {
					newListenSet[lp] = true
				}

				// New listens to subscribe.
				for _, lp := range newProj.ChannelListen {
					if !oldListenSet[lp] {
						if _, subscribed := s.channelSubs[lp]; !subscribed && s.broker != nil {
							listenProject := lp
							bus := s.broker.Events()
							ch := bus.Subscribe(listenProject)
							s.channelSubs[listenProject] = ch
							s.logger.Printf("channel: subscribed to EventBus for project=%s (listen change)", listenProject)
							cmds = append(cmds, func() tea.Msg {
								event, ok := <-ch
								if !ok {
									return ChannelSubscribedMsg{ProjectID: listenProject, Err: errChannelClosed}
								}
								return ChannelEventMsg{ProjectID: listenProject, Event: event}
							})
						}
					}
				}

				// Old listens to unsubscribe.
				for _, lp := range oldProj.ChannelListen {
					if !newListenSet[lp] {
						if ch, subscribed := s.channelSubs[lp]; subscribed {
							delete(s.channelSubs, lp)
							if s.broker != nil {
								s.broker.Events().Unsubscribe(lp, ch)
							}
							s.logger.Printf("channel: unsubscribed from EventBus for project=%s (listen removed)", lp)
						}
					}
				}
			}
		}
		break // only process "channel" once
	}

	s.NotifyAll()
	s.mu.Unlock()

	s.logger.Printf("config: ApplyConfig exit cmds=%d", len(cmds))
	return cmds
}

// ToggleChannel toggles channel_enabled for a specific project.
// If enabling and broker is nil, performs lazy broker start.
// Returns (newEnabled bool, subscribeCmds []tea.Cmd, err error).
// Acquires write lock. Caller must NOT hold any locks.
func (s *AppState) ToggleChannel(projectID string) (bool, []tea.Cmd, error) {
	s.logger.Printf("channel: ToggleChannel entry project=%s", projectID)
	s.mu.Lock()

	// Find project index.
	projIdx := -1
	for i, p := range s.projects {
		if p.ID == projectID {
			projIdx = i
			break
		}
	}
	if projIdx == -1 {
		s.mu.Unlock()
		return false, nil, fmt.Errorf("project not found: %s", projectID)
	}

	newEnabled := !s.projects[projIdx].ChannelEnabled
	var cmds []tea.Cmd

	if newEnabled {
		// Lazy start broker if needed.
		if s.broker == nil {
			b, err := broker.New(s.logger, s.cfg.BrokerPort)
			if err != nil {
				s.mu.Unlock()
				return false, nil, fmt.Errorf("broker start failed: %w", err)
			}
			s.broker = b
			s.logger.Printf("broker: lazy started on %s", b.Addr())
		}

		// Subscribe to own project.
		if _, subscribed := s.channelSubs[projectID]; !subscribed {
			bus := s.broker.Events()
			ch := bus.Subscribe(projectID)
			s.channelSubs[projectID] = ch
			s.logger.Printf("channel: subscribed project=%s (toggle on)", projectID)
			cmds = append(cmds, func() tea.Msg {
				event, ok := <-ch
				if !ok {
					return ChannelSubscribedMsg{ProjectID: projectID, Err: errChannelClosed}
				}
				return ChannelEventMsg{ProjectID: projectID, Event: event}
			})
		}

		// Subscribe to listen projects.
		for _, lp := range s.projects[projIdx].ChannelListen {
			if _, subscribed := s.channelSubs[lp]; !subscribed {
				listenProject := lp
				bus := s.broker.Events()
				ch := bus.Subscribe(listenProject)
				s.channelSubs[listenProject] = ch
				s.logger.Printf("channel: subscribed project=%s (listen, toggle on)", listenProject)
				cmds = append(cmds, func() tea.Msg {
					event, ok := <-ch
					if !ok {
						return ChannelSubscribedMsg{ProjectID: listenProject, Err: errChannelClosed}
					}
					return ChannelEventMsg{ProjectID: listenProject, Event: event}
				})
			}
		}
	} else {
		// Unsubscribe from own project.
		if ch, subscribed := s.channelSubs[projectID]; subscribed {
			delete(s.channelSubs, projectID)
			if s.broker != nil {
				s.broker.Events().Unsubscribe(projectID, ch)
			}
			s.logger.Printf("channel: unsubscribed project=%s (toggle off)", projectID)
		}
	}

	// Update the project config.
	s.projects[projIdx].ChannelEnabled = newEnabled
	s.cfg.Projects[projIdx].ChannelEnabled = newEnabled
	s.NotifyAll()
	s.mu.Unlock()

	s.logger.Printf("channel: ToggleChannel exit project=%s enabled=%v cmds=%d", projectID, newEnabled, len(cmds))
	return newEnabled, cmds, nil
}

// UpdateChannelListen updates channel_listen for a specific project.
// Compares old vs new listen sets and manages EventBus subscriptions accordingly:
// new items are subscribed, removed items are unsubscribed.
// Returns tea.Cmds for newly subscribed channels.
// Acquires write lock. Caller must NOT hold any locks.
func (s *AppState) UpdateChannelListen(projectID string, newListen []string) []tea.Cmd {
	s.logger.Printf("channel: UpdateChannelListen entry project=%s newListen=%v", projectID, newListen)

	s.mu.Lock()

	// Find project and get old listen list.
	projIdx := -1
	var oldListen []string
	var channelEnabled bool
	for i, p := range s.projects {
		if p.ID == projectID {
			projIdx = i
			oldListen = p.ChannelListen
			channelEnabled = p.ChannelEnabled
			break
		}
	}
	if projIdx == -1 {
		s.mu.Unlock()
		s.logger.Printf("channel: UpdateChannelListen project not found: %s", projectID)
		return nil
	}

	// Update in-memory config.
	s.projects[projIdx].ChannelListen = newListen
	s.cfg.Projects[projIdx].ChannelListen = newListen

	var cmds []tea.Cmd

	// Only manage subscriptions if channel is enabled and broker is available.
	if channelEnabled && s.broker != nil {
		oldSet := make(map[string]bool, len(oldListen))
		for _, lp := range oldListen {
			oldSet[lp] = true
		}
		newSet := make(map[string]bool, len(newListen))
		for _, lp := range newListen {
			newSet[lp] = true
		}

		// Subscribe new listen items.
		for _, lp := range newListen {
			if !oldSet[lp] {
				if _, subscribed := s.channelSubs[lp]; !subscribed {
					listenProject := lp
					bus := s.broker.Events()
					ch := bus.Subscribe(listenProject)
					s.channelSubs[listenProject] = ch
					s.logger.Printf("channel: subscribed project=%s (listen added)", listenProject)
					cmds = append(cmds, func() tea.Msg {
						event, ok := <-ch
						if !ok {
							return ChannelSubscribedMsg{ProjectID: listenProject, Err: errChannelClosed}
						}
						return ChannelEventMsg{ProjectID: listenProject, Event: event}
					})
				}
			}
		}

		// Unsubscribe removed listen items.
		for _, lp := range oldListen {
			if !newSet[lp] {
				if ch, subscribed := s.channelSubs[lp]; subscribed {
					delete(s.channelSubs, lp)
					s.broker.Events().Unsubscribe(lp, ch)
					s.logger.Printf("channel: unsubscribed project=%s (listen removed)", lp)
				}
			}
		}
	}

	s.NotifyAll()
	s.mu.Unlock()

	s.logger.Printf("channel: UpdateChannelListen exit project=%s cmds=%d", projectID, len(cmds))
	return cmds
}
