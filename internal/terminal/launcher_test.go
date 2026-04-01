package terminal

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildWindowsArgs_NewTab(t *testing.T) {
	args := BuildWindowsArgs("My Project", "D:/Projects/Foo", "new_tab", nil)
	want := []string{"new-tab", "-d", "D:/Projects/Foo", "--title", "My Project", "--", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_NewWindow(t *testing.T) {
	args := BuildWindowsArgs("My Project", "D:/Projects/Foo", "new_window", nil)
	want := []string{"-w", "new", "-d", "D:/Projects/Foo", "--title", "My Project", "--", "claude"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_WithExtraArgs(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", []string{"--agent", "clarifier"})
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-env.cmd", "claude", "--agent", "clarifier"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_AgentModeWithInitMsg(t *testing.T) {
	args := BuildWindowsArgs("Test", "/path", "new_tab", []string{"--agent", "coding", "init message"})
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--",
		"cmd", "/c", ".claude\\hooks\\zpit-env.cmd", "claude", "--agent", "coding", "init message"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

func TestBuildWindowsArgs_NoAgentNoWrapper(t *testing.T) {
	// Without --agent, no wrapper should be added
	args := BuildWindowsArgs("Test", "/path", "new_tab", []string{"--resume"})
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--", "claude", "--resume"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got %v, want %v", args, want)
	}
}

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
		"ZPIT_AGENT=1 claude --agent clarifier"}
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
	args := BuildWindowsArgs("Test", "/path", "new_tab", []string{"--agent", "coding", "--channel-enabled"})
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
