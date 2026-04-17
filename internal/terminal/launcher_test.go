package terminal

import (
	"reflect"
	"strings"
	"testing"
)

// --- Existing tests updated for new BuildWindowsArgs signature (profile="", shell="") ---

func TestBuildWindowsArgs_NewTab(t *testing.T) {
	args := BuildWindowsArgs("My Project", "D:/Projects/Foo", "new_tab", "", "", nil)
	want := []string{"new-tab", "-d", "D:/Projects/Foo", "--title", "My Project", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-exit.cmd", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_NewWindow(t *testing.T) {
	args := BuildWindowsArgs("My Project", "D:/Projects/Foo", "new_window", "", "", nil)
	want := []string{"-w", "new", "-d", "D:/Projects/Foo", "--title", "My Project", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-exit.cmd", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_WithExtraArgs(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "", "", []string{"--agent", "clarifier"})
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-env.cmd", "clarifier", "claude", "--agent", "clarifier"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_AgentModeWithInitMsg(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "", "", []string{"--agent", "coding", "init message"})
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-env.cmd", "coding", "claude", "--agent", "coding", "init message"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_NoAgentCleanExit(t *testing.T) {
	// Without --agent, clean exit wrapper should be used (not env wrapper)
	args := BuildWindowsArgs("Test", "/path", "new_tab", "", "", []string{"--resume"})
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-exit.cmd", "claude", "--resume"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

// --- Profile-related tests ---

func TestBuildWindowsArgs_WithProfile_NewTab(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "PowerShell 7", "pwsh", nil)
	want := []string{"new-tab", "-p", "PowerShell 7", "-d", "/path", "--title", "Test", "--",
		"pwsh", "-NoProfile", "-File", ".claude\\hooks\\zpit-exit.ps1", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_WithProfile_NewWindow(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_window", "PowerShell 7", "pwsh", nil)
	want := []string{"-w", "new", "-p", "PowerShell 7", "-d", "/path", "--title", "Test", "--",
		"pwsh", "-NoProfile", "-File", ".claude\\hooks\\zpit-exit.ps1", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_WithProfile_AgentPwsh(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "PowerShell 7", "pwsh", []string{"--agent", "coding"})
	want := []string{"new-tab", "-p", "PowerShell 7", "-d", "/path", "--title", "Test", "--",
		"pwsh", "-NoProfile", "-File", ".claude\\hooks\\zpit-env.ps1", "coding", "claude", "--agent", "coding"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_WithProfile_AgentPowershell(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "Windows PowerShell", "powershell", []string{"--agent", "coding"})
	want := []string{"new-tab", "-p", "Windows PowerShell", "-d", "/path", "--title", "Test", "--",
		"powershell", "-NoProfile", "-File", ".claude\\hooks\\zpit-env.ps1", "coding", "claude", "--agent", "coding"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_WithProfile_AgentCmd(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "Command Prompt", "cmd", []string{"--agent", "coding"})
	want := []string{"new-tab", "-p", "Command Prompt", "-d", "/path", "--title", "Test", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-env.cmd", "coding", "claude", "--agent", "coding"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_WithProfile_NoAgent(t *testing.T) {
	// Profile set but no agent mode — clean exit wrapper with pwsh, -p flag present
	args := BuildWindowsArgs("Test", "/path", "new_tab", "PowerShell 7", "pwsh", []string{"--resume"})
	want := []string{"new-tab", "-p", "PowerShell 7", "-d", "/path", "--title", "Test", "--",
		"pwsh", "-NoProfile", "-File", ".claude\\hooks\\zpit-exit.ps1", "claude", "--resume"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_EmptyProfile_IdenticalToCurrent(t *testing.T) {
	// AC-9: When profile is empty, behavior is identical to current.
	args := BuildWindowsArgs("Test", "/path", "new_tab", "", "", []string{"--agent", "coding"})
	// Should use cmd wrapper, no -p flag
	if args[0] != "new-tab" {
		t.Errorf("expected new-tab, got %s", args[0])
	}
	for _, a := range args {
		if a == "-p" {
			t.Error("should not have -p flag when profile is empty")
		}
	}
	// Should have cmd wrapper
	found := false
	for i, a := range args {
		if a == "cmd" && i+2 < len(args) && args[i+1] == "/c" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cmd /c wrapper in args: %v", args)
	}
}

// --- buildEnvWrapper tests ---

func TestBuildEnvWrapper(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{"cmd", []string{"cmd", "/c", ".claude\\hooks\\zpit-env.cmd"}},
		{"pwsh", []string{"pwsh", "-NoProfile", "-File", ".claude\\hooks\\zpit-env.ps1"}},
		{"powershell", []string{"powershell", "-NoProfile", "-File", ".claude\\hooks\\zpit-env.ps1"}},
		{"", []string{"cmd", "/c", ".claude\\hooks\\zpit-env.cmd"}},          // empty defaults to cmd
		{"unknown", []string{"cmd", "/c", ".claude\\hooks\\zpit-env.cmd"}},   // unknown defaults to cmd
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			got := buildEnvWrapper(tt.shell)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildEnvWrapper(%q) = %v, want %v", tt.shell, got, tt.want)
			}
		})
	}
}

// --- buildCleanExitWrapper tests ---

func TestBuildCleanExitWrapper(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{"cmd", []string{"cmd", "/c", ".claude\\hooks\\zpit-exit.cmd"}},
		{"pwsh", []string{"pwsh", "-NoProfile", "-File", ".claude\\hooks\\zpit-exit.ps1"}},
		{"powershell", []string{"powershell", "-NoProfile", "-File", ".claude\\hooks\\zpit-exit.ps1"}},
		{"", []string{"cmd", "/c", ".claude\\hooks\\zpit-exit.cmd"}},          // empty defaults to cmd
		{"unknown", []string{"cmd", "/c", ".claude\\hooks\\zpit-exit.cmd"}},   // unknown defaults to cmd
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			got := buildCleanExitWrapper(tt.shell)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildCleanExitWrapper(%q) = %v, want %v", tt.shell, got, tt.want)
			}
		})
	}
}

// --- Existing tmux and utility tests (unchanged) ---

func TestBuildTmuxArgs_NewWindow(t *testing.T) {
	args := BuildTmuxArgs("ase-inspection", "/mnt/d/Projects/ASE", "new_window", nil)
	want := []string{"new-window", "-n", "ase-inspection", "-c", "/mnt/d/Projects/ASE", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildTmuxArgs_NewPane(t *testing.T) {
	args := BuildTmuxArgs("ase-inspection", "/mnt/d/Projects/ASE", "new_pane", nil)
	want := []string{"split-window", "-h", "-c", "/mnt/d/Projects/ASE", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildTmuxArgs_AgentMode(t *testing.T) {
	args := BuildTmuxArgs("ase", "/mnt/d/Projects/ASE", "new_window", []string{"--agent", "clarifier"})
	want := []string{"new-window", "-n", "ase", "-c", "/mnt/d/Projects/ASE",
		"ZPIT_AGENT=1 ZPIT_AGENT_TYPE=clarifier claude --agent clarifier"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildTmuxArgs_NoAgentNoPrefix(t *testing.T) {
	args := BuildTmuxArgs("ase", "/mnt/d/Projects/ASE", "new_window", []string{"--resume"})
	want := []string{"new-window", "-n", "ase", "-c", "/mnt/d/Projects/ASE", "claude --resume"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildClaudeArgs(t *testing.T) {
	got := buildClaudeArgs(nil)
	if len(got) != 1 || got[0] != "claude" {
		t.Errorf("nil args: got %v", got)
	}
	got = buildClaudeArgs([]string{})
	if len(got) != 1 || got[0] != "claude" {
		t.Errorf("empty args: got %v", got)
	}
	got = buildClaudeArgs([]string{"--agent", "clarifier"})
	if len(got) != 3 || got[0] != "claude" || got[1] != "--agent" || got[2] != "clarifier" {
		t.Errorf("with args: got %v", got)
	}
}

func TestFilterChannelFlag(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		enabled bool
	}{
		{"nil", nil, nil, false},
		{"empty", []string{}, nil, false},
		{"no flag", []string{"--agent", "coding"}, []string{"--agent", "coding"}, false},
		{"with flag", []string{"--agent", "coding", "--channel-enabled"}, []string{"--agent", "coding"}, true},
		{"flag only", []string{"--channel-enabled"}, nil, true},
		{"flag in middle", []string{"--agent", "--channel-enabled", "coding"}, []string{"--agent", "coding"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered, enabled := filterChannelFlag(tt.args)
			if enabled != tt.enabled {
				t.Errorf("enabled: got %v, want %v", enabled, tt.enabled)
			}
			if !reflect.DeepEqual(filtered, tt.want) {
				t.Errorf("filtered: got %v, want %v", filtered, tt.want)
			}
		})
	}
}

func TestBuildClaudeArgs_ChannelEnabled(t *testing.T) {
	got := buildClaudeArgs([]string{"--agent", "coding", "--channel-enabled"})
	want := []string{"claude", "--agent", "coding", "--dangerously-load-development-channels", "server:zpit-channel"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildClaudeArgs_ChannelNotEnabled(t *testing.T) {
	got := buildClaudeArgs([]string{"--agent", "coding"})
	want := []string{"claude", "--agent", "coding"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildWindowsArgs_WithChannelEnabled(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "", "", []string{"--agent", "coding", "--channel-enabled"})
	// Should contain the channel flag and not --channel-enabled
	found := false
	for _, a := range args {
		if a == "--channel-enabled" {
			t.Error("--channel-enabled should be removed")
		}
		if a == "--dangerously-load-development-channels" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --dangerously-load-development-channels in args: %v", args)
	}
}

func TestBuildTmuxArgs_WithChannelEnabled(t *testing.T) {
	args := BuildTmuxArgs("proj", "/path", "new_window", []string{"--agent", "coding", "--channel-enabled"})
	// The command string should contain the channel flag.
	cmd := args[len(args)-1] // last element is the command string
	if strings.Contains(cmd, "--channel-enabled") {
		t.Error("--channel-enabled should be removed")
	}
	if !strings.Contains(cmd, "--dangerously-load-development-channels") {
		t.Errorf("expected --dangerously-load-development-channels in command: %s", cmd)
	}
	if !strings.Contains(cmd, "server:zpit-channel") {
		t.Errorf("expected server:zpit-channel in command: %s", cmd)
	}
}

func TestHasAgentFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"nil", nil, false},
		{"empty", []string{}, false},
		{"no agent", []string{"--resume"}, false},
		{"has agent", []string{"--agent", "clarifier"}, true},
		{"agent with init", []string{"--agent", "coding", "init msg"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAgentFlag(tt.args); got != tt.want {
				t.Errorf("hasAgentFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestNeedsAgentEnv(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"nil", nil, false},
		{"empty", []string{}, false},
		{"no agent", []string{"--resume"}, false},
		{"clarifier", []string{"--agent", "clarifier"}, true},
		{"coding", []string{"--agent", "coding"}, true},
		{"reviewer", []string{"--agent", "reviewer"}, true},
		{"efficiency skipped", []string{"--agent", "efficiency"}, false},
		{"efficiency with channel", []string{"--agent", "efficiency", "--channel-enabled"}, false},
		{"agent flag without value", []string{"--agent"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsAgentEnv(tt.args); got != tt.want {
				t.Errorf("needsAgentEnv(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestGetAgentRole(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"nil", nil, ""},
		{"empty", []string{}, ""},
		{"no agent", []string{"--resume"}, ""},
		{"clarifier", []string{"--agent", "clarifier"}, "clarifier"},
		{"coding with init", []string{"--agent", "coding", "init msg"}, "coding"},
		{"agent flag without value", []string{"--agent"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getAgentRole(tt.args); got != tt.want {
				t.Errorf("getAgentRole(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestBuildWindowsArgs_EfficiencyCleanExit(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", "", "", []string{"--agent", "efficiency"})
	// Efficiency agent should use clean exit wrapper (zpit-exit), NOT env wrapper (zpit-env)
	for _, a := range args {
		if a == ".claude\\hooks\\zpit-env.cmd" || a == ".claude\\hooks\\zpit-env.ps1" {
			t.Errorf("efficiency agent should not have env wrapper, got: %v", args)
		}
	}
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-exit.cmd", "claude", "--agent", "efficiency"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildTmuxArgs_EfficiencyNoPrefix(t *testing.T) {
	args := BuildTmuxArgs("proj", "/path", "new_window", []string{"--agent", "efficiency"})
	cmd := args[len(args)-1]
	if strings.HasPrefix(cmd, "ZPIT_AGENT=1") {
		t.Errorf("efficiency agent should not have ZPIT_AGENT=1 prefix, got: %s", cmd)
	}
	want := []string{"new-window", "-n", "proj", "-c", "/path", "claude --agent efficiency"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}
