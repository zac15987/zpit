package main

import (
	_ "embed"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/tui"
)

//go:embed agents/clarifier.md
var clarifierAgentMD []byte

//go:embed agents/reviewer.md
var reviewerAgentMD []byte

func main() {
	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if envPath := os.Getenv("ZPIT_CONFIG"); envPath != "" {
		cfgPath = envPath
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Expected config at: %s\n", cfgPath)
		fmt.Fprintf(os.Stderr, "Set ZPIT_CONFIG env var to override.\n")
		os.Exit(1)
	}

	if len(cfg.Projects) == 0 {
		fmt.Fprintf(os.Stderr, "No projects defined in %s\n", cfgPath)
		os.Exit(1)
	}

	p := tea.NewProgram(
		tui.NewModel(cfg, clarifierAgentMD, reviewerAgentMD),
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
