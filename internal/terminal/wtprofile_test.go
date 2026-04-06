package terminal

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestSettingsJSON creates a temporary WT settings.json and overrides wtSettingsPaths.
// Returns a cleanup function that restores the original.
func setupTestSettingsJSON(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	original := wtSettingsPaths
	wtSettingsPaths = func() []string { return []string{path} }
	t.Cleanup(func() { wtSettingsPaths = original })
	return path
}

func TestResolveWTProfile_EmptyName(t *testing.T) {
	result := ResolveWTProfile("")
	if result.Shell != "" || result.Warning != "" {
		t.Errorf("empty name should return zero result, got shell=%q warning=%q", result.Shell, result.Warning)
	}
}

func TestResolveWTProfile_SettingsNotFound(t *testing.T) {
	original := wtSettingsPaths
	wtSettingsPaths = func() []string { return []string{"/nonexistent/settings.json"} }
	defer func() { wtSettingsPaths = original }()

	result := ResolveWTProfile("PowerShell 7")
	if result.Shell != "" {
		t.Errorf("should return empty shell, got %q", result.Shell)
	}
	if result.Warning == "" {
		t.Error("should return warning when settings.json not found")
	}
}

func TestResolveWTProfile_ProfileNotFound(t *testing.T) {
	setupTestSettingsJSON(t, `{
		"profiles": {
			"list": [
				{"name": "Command Prompt", "commandline": "cmd.exe"}
			]
		}
	}`)

	result := ResolveWTProfile("NonExistent Profile")
	if result.Shell != "" {
		t.Errorf("should return empty shell, got %q", result.Shell)
	}
	if result.Warning == "" {
		t.Error("should return warning when profile not found")
	}
}

func TestDetectShell_Pwsh(t *testing.T) {
	tests := []struct {
		name    string
		profile wtProfile
		want    string
	}{
		{
			name:    "pwsh.exe in commandline",
			profile: wtProfile{Name: "PowerShell 7", CommandLine: "C:\\Program Files\\PowerShell\\7\\pwsh.exe"},
			want:    "pwsh",
		},
		{
			name:    "pwsh in commandline (no path)",
			profile: wtProfile{Name: "PS7", CommandLine: "pwsh.exe -NoLogo"},
			want:    "pwsh",
		},
		{
			name:    "pwsh via source field only",
			profile: wtProfile{Name: "PS7", Source: "Windows.Terminal.PowershellCore"},
			want:    "pwsh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectShell(&tt.profile)
			if got != tt.want {
				t.Errorf("detectShell() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectShell_Powershell(t *testing.T) {
	tests := []struct {
		name    string
		profile wtProfile
		want    string
	}{
		{
			name:    "powershell.exe in commandline",
			profile: wtProfile{Name: "Windows PowerShell", CommandLine: "powershell.exe"},
			want:    "powershell",
		},
		{
			name:    "powershell with full path",
			profile: wtProfile{Name: "PS5", CommandLine: "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe"},
			want:    "powershell",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectShell(&tt.profile)
			if got != tt.want {
				t.Errorf("detectShell() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectShell_Cmd(t *testing.T) {
	tests := []struct {
		name    string
		profile wtProfile
		want    string
	}{
		{
			name:    "cmd.exe explicit",
			profile: wtProfile{Name: "Command Prompt", CommandLine: "cmd.exe"},
			want:    "cmd",
		},
		{
			name:    "both fields empty (default WT behavior)",
			profile: wtProfile{Name: "Default"},
			want:    "cmd",
		},
		{
			name:    "cmd.exe with path",
			profile: wtProfile{Name: "CMD", CommandLine: "C:\\Windows\\System32\\cmd.exe"},
			want:    "cmd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectShell(&tt.profile)
			if got != tt.want {
				t.Errorf("detectShell() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectShell_Unsupported(t *testing.T) {
	tests := []struct {
		name    string
		profile wtProfile
	}{
		{
			name:    "WSL commandline",
			profile: wtProfile{Name: "Ubuntu", CommandLine: "wsl.exe -d Ubuntu"},
		},
		{
			name:    "bash commandline",
			profile: wtProfile{Name: "Git Bash", CommandLine: "C:\\Git\\bin\\bash.exe"},
		},
		{
			name:    "WSL source only",
			profile: wtProfile{Name: "Ubuntu", Source: "Windows.Terminal.Wsl"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectShell(&tt.profile)
			if got != "" {
				t.Errorf("detectShell() = %q, want empty (unsupported)", got)
			}
		})
	}
}

func TestResolveWTProfile_PwshFound(t *testing.T) {
	setupTestSettingsJSON(t, `{
		"profiles": {
			"list": [
				{"name": "Command Prompt", "commandline": "cmd.exe"},
				{"name": "PowerShell 7", "commandline": "C:\\Program Files\\PowerShell\\7\\pwsh.exe"}
			]
		}
	}`)

	result := ResolveWTProfile("PowerShell 7")
	if result.Shell != "pwsh" {
		t.Errorf("shell = %q, want pwsh", result.Shell)
	}
	if result.Warning != "" {
		t.Errorf("unexpected warning: %s", result.Warning)
	}
}

func TestResolveWTProfile_PowershellFound(t *testing.T) {
	setupTestSettingsJSON(t, `{
		"profiles": {
			"list": [
				{"name": "Windows PowerShell", "commandline": "powershell.exe"}
			]
		}
	}`)

	result := ResolveWTProfile("Windows PowerShell")
	if result.Shell != "powershell" {
		t.Errorf("shell = %q, want powershell", result.Shell)
	}
}

func TestResolveWTProfile_CmdFound(t *testing.T) {
	setupTestSettingsJSON(t, `{
		"profiles": {
			"list": [
				{"name": "Command Prompt", "commandline": "cmd.exe"}
			]
		}
	}`)

	result := ResolveWTProfile("Command Prompt")
	if result.Shell != "cmd" {
		t.Errorf("shell = %q, want cmd", result.Shell)
	}
}

func TestResolveWTProfile_UnsupportedShell(t *testing.T) {
	setupTestSettingsJSON(t, `{
		"profiles": {
			"list": [
				{"name": "Ubuntu", "commandline": "wsl.exe -d Ubuntu"}
			]
		}
	}`)

	result := ResolveWTProfile("Ubuntu")
	if result.Shell != "" {
		t.Errorf("shell = %q, want empty for unsupported shell", result.Shell)
	}
	if result.Warning == "" {
		t.Error("should return warning for unsupported shell")
	}
}

func TestResolveWTProfile_SourcePwshCore(t *testing.T) {
	setupTestSettingsJSON(t, `{
		"profiles": {
			"list": [
				{"name": "PowerShell", "source": "Windows.Terminal.PowershellCore"}
			]
		}
	}`)

	result := ResolveWTProfile("PowerShell")
	if result.Shell != "pwsh" {
		t.Errorf("shell = %q, want pwsh", result.Shell)
	}
}

func TestResolveWTProfile_CaseSensitive(t *testing.T) {
	setupTestSettingsJSON(t, `{
		"profiles": {
			"list": [
				{"name": "PowerShell 7", "commandline": "pwsh.exe"}
			]
		}
	}`)

	// Exact match works
	result := ResolveWTProfile("PowerShell 7")
	if result.Shell != "pwsh" {
		t.Errorf("exact match: shell = %q, want pwsh", result.Shell)
	}

	// Case mismatch fails
	result = ResolveWTProfile("powershell 7")
	if result.Shell != "" {
		t.Errorf("case mismatch should fail, got shell=%q", result.Shell)
	}
	if result.Warning == "" {
		t.Error("case mismatch should produce warning")
	}
}

func TestFindAndParseWTSettings_MultiplePaths(t *testing.T) {
	dir := t.TempDir()
	// First path doesn't exist, second does.
	goodPath := filepath.Join(dir, "good", "settings.json")
	os.MkdirAll(filepath.Dir(goodPath), 0o755)
	os.WriteFile(goodPath, []byte(`{"profiles":{"list":[{"name":"Test","commandline":"cmd.exe"}]}}`), 0o644)

	original := wtSettingsPaths
	wtSettingsPaths = func() []string {
		return []string{
			filepath.Join(dir, "bad", "settings.json"), // doesn't exist
			goodPath,
		}
	}
	defer func() { wtSettingsPaths = original }()

	s, path, err := findAndParseWTSettings()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != goodPath {
		t.Errorf("path = %q, want %q", path, goodPath)
	}
	if len(s.Profiles.List) != 1 {
		t.Errorf("profiles count = %d, want 1", len(s.Profiles.List))
	}
}

func TestFindAndParseWTSettings_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "settings.json")
	os.WriteFile(badPath, []byte(`{invalid json}`), 0o644)

	original := wtSettingsPaths
	wtSettingsPaths = func() []string { return []string{badPath} }
	defer func() { wtSettingsPaths = original }()

	_, _, err := findAndParseWTSettings()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFindAndParseWTSettings_NoLOCALAPPDATA(t *testing.T) {
	original := wtSettingsPaths
	wtSettingsPaths = func() []string { return nil }
	defer func() { wtSettingsPaths = original }()

	_, _, err := findAndParseWTSettings()
	if err == nil {
		t.Error("expected error when no paths available")
	}
}
