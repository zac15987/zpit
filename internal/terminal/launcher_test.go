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
	args := BuildWindowsArgs("Test", "/path", "new_tab", []string{"-p", "do something"})
	want := []string{"new-tab", "-d", "/path", "--title", "Test", "--", "claude -p do something"}
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

func TestBuildClaudeCommand(t *testing.T) {
	if got := buildClaudeCommand(nil); got != "claude" {
		t.Errorf("nil args: got %q", got)
	}
	if got := buildClaudeCommand([]string{}); got != "claude" {
		t.Errorf("empty args: got %q", got)
	}
	if got := buildClaudeCommand([]string{"-p", "hello"}); got != "claude -p hello" {
		t.Errorf("with args: got %q", got)
	}
}
