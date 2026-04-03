//go:build windows

package notify

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const soundTimeout = 5 * time.Second

func playSound(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), soundTimeout)
	defer cancel()

	if path != "" {
		// Custom sound file: use WPF MediaPlayer via PresentationCore.
		script := fmt.Sprintf(
			`Add-Type -AssemblyName PresentationCore; `+
				`$p = New-Object System.Windows.Media.MediaPlayer; `+
				`$p.Open([Uri]::new('%s')); `+
				`$p.Play(); `+
				`Start-Sleep -Milliseconds 3000; `+
				`$p.Close()`,
			path,
		)
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script)
		return cmd.Run()
	}

	// Default: system asterisk sound.
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		`[System.Media.SystemSounds]::Asterisk.Play()`)
	return cmd.Run()
}
