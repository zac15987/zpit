package notify

import (
	"testing"
	"time"

	"github.com/zac15987/zpit/internal/config"
)

func newTestNotifier(reRemindMinutes int) *Notifier {
	return NewNotifier(config.NotificationConfig{
		TUIAlert:        true,
		WindowsToast:    false, // don't send real toasts in tests
		Sound:           false,
		ReRemindMinutes: reRemindMinutes,
	}, nil)
}

func TestShouldNotify_FirstTime(t *testing.T) {
	n := newTestNotifier(15)
	if !n.ShouldNotify("proj-1") {
		t.Error("first notification should be allowed")
	}
}

func TestShouldNotify_Cooldown(t *testing.T) {
	n := newTestNotifier(15)
	n.lastNotified["proj-1"] = time.Now().Add(-5 * time.Minute) // 5 min ago

	if n.ShouldNotify("proj-1") {
		t.Error("should be suppressed within cooldown period")
	}
}

func TestShouldNotify_CooldownExpired(t *testing.T) {
	n := newTestNotifier(15)
	n.lastNotified["proj-1"] = time.Now().Add(-20 * time.Minute) // 20 min ago

	if !n.ShouldNotify("proj-1") {
		t.Error("should be allowed after cooldown expires")
	}
}

func TestReset(t *testing.T) {
	n := newTestNotifier(15)
	n.lastNotified["proj-1"] = time.Now() // just notified

	if n.ShouldNotify("proj-1") {
		t.Error("should be suppressed right after notification")
	}

	n.Reset("proj-1")

	if !n.ShouldNotify("proj-1") {
		t.Error("should be allowed after reset")
	}
}

func TestNotifyWaiting_RespectsConfig(t *testing.T) {
	n := newTestNotifier(15)
	// With toast and sound disabled, it should still return true (notification "sent" for TUI).
	sent := n.NotifyWaiting("proj-1", "Test Project", "question?")
	if !sent {
		t.Error("NotifyWaiting should return true on first call")
	}

	// Second call within cooldown should be suppressed.
	sent = n.NotifyWaiting("proj-1", "Test Project", "another question?")
	if sent {
		t.Error("NotifyWaiting should return false within cooldown")
	}
}

func TestShouldNotify_DifferentProjects(t *testing.T) {
	n := newTestNotifier(15)
	n.lastNotified["proj-1"] = time.Now() // just notified proj-1

	if n.ShouldNotify("proj-1") {
		t.Error("proj-1 should be suppressed")
	}
	if !n.ShouldNotify("proj-2") {
		t.Error("proj-2 should be allowed (independent cooldown)")
	}
}

func TestSoundFile_PassedToPlaySound(t *testing.T) {
	// Verify that SoundFile is stored in config and accessible.
	cfg := config.NotificationConfig{
		Sound:     true,
		SoundFile: "/tmp/test-notify.wav",
	}
	n := NewNotifier(cfg, nil)

	if n.cfg.SoundFile != "/tmp/test-notify.wav" {
		t.Errorf("SoundFile not stored: got %q", n.cfg.SoundFile)
	}
}

func TestSoundFile_EmptyDefault(t *testing.T) {
	cfg := config.NotificationConfig{
		Sound: true,
	}
	n := NewNotifier(cfg, nil)

	if n.cfg.SoundFile != "" {
		t.Errorf("SoundFile should be empty by default: got %q", n.cfg.SoundFile)
	}
}

func TestSoundFile_NonExistent_SetsWarning(t *testing.T) {
	cfg := config.NotificationConfig{
		Sound:     true,
		SoundFile: "/nonexistent/path/sound.mp3",
	}
	n := NewNotifier(cfg, nil)

	if n.warning == "" {
		t.Error("warning should be set when sound_file doesn't exist")
	}
}

func TestConsumeWarning_FirstCallReturnsWarning(t *testing.T) {
	cfg := config.NotificationConfig{
		Sound:     true,
		SoundFile: "/nonexistent/path/sound.mp3",
	}
	n := NewNotifier(cfg, nil)

	w := n.ConsumeWarning()
	if w == "" {
		t.Error("first ConsumeWarning should return warning message")
	}
	if w != "sound file not found: /nonexistent/path/sound.mp3" {
		t.Errorf("unexpected warning: %q", w)
	}
}

func TestConsumeWarning_SecondCallReturnsEmpty(t *testing.T) {
	cfg := config.NotificationConfig{
		Sound:     true,
		SoundFile: "/nonexistent/path/sound.mp3",
	}
	n := NewNotifier(cfg, nil)

	_ = n.ConsumeWarning() // consume first

	w := n.ConsumeWarning()
	if w != "" {
		t.Errorf("second ConsumeWarning should be empty, got %q", w)
	}
}

func TestConsumeWarning_NoWarningWhenFileEmpty(t *testing.T) {
	cfg := config.NotificationConfig{
		Sound: true,
	}
	n := NewNotifier(cfg, nil)

	w := n.ConsumeWarning()
	if w != "" {
		t.Errorf("ConsumeWarning should be empty when SoundFile not set, got %q", w)
	}
}
