//go:build windows

package notify

import "os/exec"

func playSound() error {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`[System.Media.SystemSounds]::Asterisk.Play()`)
	return cmd.Run()
}
