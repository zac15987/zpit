package notify

import (
	"sync"
	"time"

	"github.com/zac15987/zpit/internal/config"
)

// Notifier dispatches notifications respecting config and re-remind cooldown.
type Notifier struct {
	cfg          config.NotificationConfig
	mu           sync.Mutex
	lastNotified map[string]time.Time // projectID → last notification time
}

// NewNotifier creates a Notifier with the given config.
func NewNotifier(cfg config.NotificationConfig) *Notifier {
	return &Notifier{
		cfg:          cfg,
		lastNotified: make(map[string]time.Time),
	}
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
		go sendToast(projectName, questionText)
	}

	if n.cfg.Sound {
		go playSound()
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

// Reset clears the cooldown for a project (call when user responds).
func (n *Notifier) Reset(projectID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.lastNotified, projectID)
}
