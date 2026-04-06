//go:build !windows

package notify

import (
	"context"
	"os/exec"
	"time"
)

const soundTimeout = 15 * time.Second

// playerSpec defines a command-line audio player with its arguments.
type playerSpec struct {
	bin  string
	args []string // path placeholder appended at call site
}

// customPlayers is the fallback chain for custom sound files on Linux.
var customPlayers = []playerSpec{
	{bin: "mpv", args: []string{"--no-video", "--really-quiet"}},
	{bin: "ffplay", args: []string{"-nodisp", "-autoexit", "-loglevel", "quiet"}},
	{bin: "paplay", args: nil},
	{bin: "aplay", args: []string{"-q"}},
}

func playSound(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), soundTimeout)
	defer cancel()

	if path != "" {
		// Custom sound file: try players in order.
		for _, p := range customPlayers {
			binPath, err := exec.LookPath(p.bin)
			if err != nil {
				continue
			}
			args := append(p.args, path)
			cmd := exec.CommandContext(ctx, binPath, args...)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		return nil // best effort — no player succeeded
	}

	// Default: system notification sound.
	if binPath, err := exec.LookPath("paplay"); err == nil {
		cmd := exec.CommandContext(ctx, binPath, "/usr/share/sounds/freedesktop/stereo/message-new-instant.oga")
		return cmd.Run()
	}
	if binPath, err := exec.LookPath("aplay"); err == nil {
		cmd := exec.CommandContext(ctx, binPath, "-q", "/usr/share/sounds/freedesktop/stereo/message-new-instant.wav")
		return cmd.Run()
	}
	return nil
}
