package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
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

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := config.WriteTemplate(cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created config template at: %s\n", cfgPath)
		fmt.Println("Please edit it and add your projects, then run zpit again.")
		os.Exit(0)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config at: %s\n", cfgPath)
		os.Exit(1)
	}

	locale.SetLanguage(cfg.Language)

	if len(cfg.Projects) == 0 {
		fmt.Fprintf(os.Stderr, "No projects defined in %s\n", cfgPath)
		fmt.Fprintf(os.Stderr, "Please add at least one [[projects]] section, then run zpit again.\n")
		os.Exit(0)
	}

	// Open daily log file.
	baseDir, _ := config.BaseDir()
	logDir := filepath.Join(baseDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	today := time.Now().Format("2006-01-02")
	logFile, err := os.OpenFile(
		filepath.Join(logDir, "zpit-"+today+".log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open log file: %v\n", err)
	} else {
		defer logFile.Close()
	}
	cleanOldLogs(logDir, 30)

	p := tea.NewProgram(
		tui.NewModel(cfg, clarifierAgentMD, reviewerAgentMD, logFile),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

// cleanOldLogs removes log files older than maxDays from the log directory.
func cleanOldLogs(dir string, maxDays int) {
	cutoff := time.Now().AddDate(0, 0, -maxDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "zpit-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateStr := strings.TrimPrefix(strings.TrimSuffix(name, ".log"), "zpit-")
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
