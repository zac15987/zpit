package terminal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WTProfileResult holds the resolved shell type and any warning from WT profile lookup.
type WTProfileResult struct {
	Shell   string // "cmd", "pwsh", or "powershell"; empty on failure
	Warning string // non-empty if resolution failed (settings.json not found, profile not matched, etc.)
}

// wtProfile represents a single profile entry in WT settings.json.
type wtProfile struct {
	Name        string `json:"name"`
	CommandLine string `json:"commandline"`
	Source      string `json:"source"`
}

// wtSettings represents the relevant parts of WT settings.json.
type wtSettings struct {
	Profiles struct {
		List []wtProfile `json:"list"`
	} `json:"profiles"`
}

// wtSettingsPaths returns the candidate paths for WT settings.json in discovery order:
// 1. Store stable, 2. Store preview, 3. Unpackaged (Scoop/Chocolatey).
// Exported via function variable for testing.
var wtSettingsPaths = defaultWTSettingsPaths

func defaultWTSettingsPaths() []string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return nil
	}
	return []string{
		filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
		filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState", "settings.json"),
		filepath.Join(localAppData, "Microsoft", "Windows Terminal", "settings.json"),
	}
}

// ResolveWTProfile reads WT settings.json, finds the named profile, and detects the shell type.
// Returns a WTProfileResult with Shell set on success, or Warning set on failure.
func ResolveWTProfile(profileName string) WTProfileResult {
	if profileName == "" {
		return WTProfileResult{}
	}

	settings, settingsPath, err := findAndParseWTSettings()
	if err != nil {
		return WTProfileResult{
			Warning: fmt.Sprintf("WT settings.json not found: %s", err),
		}
	}

	profile, err := findProfileByName(settings, profileName)
	if err != nil {
		return WTProfileResult{
			Warning: fmt.Sprintf("WT profile %q not found in %s", profileName, settingsPath),
		}
	}

	shell := detectShell(profile)
	if shell == "" {
		return WTProfileResult{
			Warning: fmt.Sprintf("WT profile %q: unsupported shell (commandline=%q, source=%q), falling back to cmd",
				profileName, profile.CommandLine, profile.Source),
		}
	}

	return WTProfileResult{Shell: shell}
}

// findAndParseWTSettings tries each candidate path and returns the first successfully parsed settings.
func findAndParseWTSettings() (*wtSettings, string, error) {
	paths := wtSettingsPaths()
	if len(paths) == 0 {
		return nil, "", fmt.Errorf("LOCALAPPDATA not set")
	}

	var lastErr error
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			lastErr = err
			continue
		}
		var s wtSettings
		if err := json.Unmarshal(data, &s); err != nil {
			lastErr = fmt.Errorf("parsing %s: %w", p, err)
			continue
		}
		return &s, p, nil
	}
	return nil, "", fmt.Errorf("tried %d paths, last error: %w", len(paths), lastErr)
}

// findProfileByName searches profiles.list for an exact (case-sensitive) name match.
func findProfileByName(s *wtSettings, name string) (*wtProfile, error) {
	for i := range s.Profiles.List {
		if s.Profiles.List[i].Name == name {
			return &s.Profiles.List[i], nil
		}
	}
	return nil, fmt.Errorf("profile %q not found", name)
}

// detectShell determines the shell type from a WT profile's commandline and source fields.
// Rules (in order):
//  1. commandline contains "pwsh" → "pwsh"
//  2. commandline contains "powershell" (but not "pwsh") → "powershell"
//  3. source equals "Windows.Terminal.PowershellCore" → "pwsh"
//  4. commandline is non-empty (any other value) → "cmd"
//  5. source is non-empty but not PowershellCore → "" (unsupported, e.g. WSL)
//  6. both empty → "cmd" (default WT behavior)
func detectShell(p *wtProfile) string {
	cl := strings.ToLower(p.CommandLine)

	if cl != "" {
		if strings.Contains(cl, "pwsh") {
			return "pwsh"
		}
		if strings.Contains(cl, "powershell") {
			return "powershell"
		}
		// Any other non-empty commandline (cmd.exe, bash, etc.)
		// If it's something like wsl.exe or bash.exe, it's not supported for agent wrapper
		if strings.Contains(cl, "wsl") || strings.Contains(cl, "bash") ||
			strings.Contains(cl, "ubuntu") || strings.Contains(cl, "debian") {
			return "" // unsupported shell
		}
		return "cmd"
	}

	// No commandline specified — check source field.
	if p.Source == "Windows.Terminal.PowershellCore" {
		return "pwsh"
	}
	if p.Source != "" {
		// Other sources (e.g. Windows.Terminal.Wsl) are unsupported.
		return ""
	}

	// Both empty → default cmd behavior.
	return "cmd"
}
