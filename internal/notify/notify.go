package notify

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/zac15987/zpit/internal/config"
)

// Notifier dispatches notifications respecting config and re-remind cooldown.
type Notifier struct {
	cfg          config.NotificationConfig
	logger       *log.Logger
	mu           sync.Mutex
	lastNotified map[string]time.Time // projectID → last notification time
	warning      string               // one-shot warning message (consumed by TUI)
}

// NewNotifier creates a Notifier with the given config.
func NewNotifier(cfg config.NotificationConfig, logger *log.Logger) *Notifier {
	n := &Notifier{
		cfg:          cfg,
		logger:       logger,
		lastNotified: make(map[string]time.Time),
	}

	// Validate sound_file at construction time: if set but file doesn't exist,
	// store a warning for the TUI to display once.
	if cfg.SoundFile != "" {
		if _, err := os.Stat(cfg.SoundFile); os.IsNotExist(err) {
			msg := fmt.Sprintf("sound file not found: %s", cfg.SoundFile)
			if logger != nil {
				logger.Print(msg)
			}
			n.warning = msg
		}
	}

	return n
}

// ConsumeWarning returns and clears the one-shot warning message.
// First call returns the warning; subsequent calls return empty string.
func (n *Notifier) ConsumeWarning() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	w := n.warning
	n.warning = ""
	return w
}

// NotifyWaiting sends notifications that an agent is waiting for input.
// Respects config toggles and re-remind cooldown.
// Returns true if notification was sent (not suppressed by cooldown).
func (n *Notifier) NotifyWaiting(projectID, projectName, questionText string) bool {
	if !n.ShouldNotify(projectID) {
		return false
	}

	n.mu.Lock()
	n.lastNotified[projectID] = time.Now()
	n.mu.Unlock()

	if n.cfg.WindowsToast {
		// Fire and forget — don't block TUI on notification delivery.
		go func() {
			if err := sendToast(projectName, questionText); err != nil {
				if n.logger != nil {
					n.logger.Printf("sendToast failed: key=%s err=%v", projectID, err)
				}
			}
		}()
	}

	if n.cfg.Sound {
		// Skip playSound if sound_file is set but doesn't exist (warning already stored).
		if n.cfg.SoundFile != "" {
			if _, err := os.Stat(n.cfg.SoundFile); os.IsNotExist(err) {
				return true
			}
		}
		go func() {
			if err := playSound(n.cfg.SoundFile); err != nil {
				if n.logger != nil {
					n.logger.Printf("playSound failed: key=%s err=%v", projectID, err)
				}
			}
		}()
	}

	return true
}

// ShouldNotify checks if enough time has passed since last notification.
func (n *Notifier) ShouldNotify(projectID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	last, ok := n.lastNotified[projectID]
	if !ok {
		return true
	}

	cooldown := time.Duration(n.cfg.ReRemindMinutes) * time.Minute
	return time.Since(last) >= cooldown
}

// UpdateConfig replaces the notification config with the given new config.
// Thread-safe: acquires internal mutex.
func (n *Notifier) UpdateConfig(cfg config.NotificationConfig) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg = cfg
}

// Reset clears the cooldown for a project (call when user responds).
func (n *Notifier) Reset(projectID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.lastNotified, projectID)
}
