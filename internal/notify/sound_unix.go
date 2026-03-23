//go:build !windows

package notify

import "os/exec"

func playSound() error {
	// Best effort: try common Linux sound players.
	if path, err := exec.LookPath("paplay"); err == nil {
		return exec.Command(path, "/usr/share/sounds/freedesktop/stereo/message-new-instant.oga").Run()
	}
	if path, err := exec.LookPath("aplay"); err == nil {
		return exec.Command(path, "-q", "/usr/share/sounds/freedesktop/stereo/message-new-instant.wav").Run()
	}
	return nil
}
