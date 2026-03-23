package terminal

import (
	"reflect"
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
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--", "claude", "--agent", "clarifier"}
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
