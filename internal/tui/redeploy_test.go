package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

func stubHookScripts() worktree.HookScripts {
	b := []byte("#!/usr/bin/env bash\nexit 0\n")
	return worktree.HookScripts{
		PathGuard: b, BashFirewall: b, PwshFirewall: b, GitGuard: b,
		EnvWrapper: b, EnvWrapperPS1: b, ExitWrapper: b, ExitWrapperPS1: b,
		NotifyPermission: b, WorktreeCreate: b,
	}
}

func projectAt(id, dir string) config.ProjectConfig {
	return config.ProjectConfig{ID: id, Name: "Project " + id, Path: config.ProjectPathConfig{Windows: dir, WSL: dir}}
}

// writeDeployed seeds n of the deployedFiles into dir (n = len → full deploy).
func writeDeployed(t *testing.T, dir string, n int) {
	t.Helper()
	for _, rel := range deployedFiles[:n] {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func makeRedeployTestModel(t *testing.T, projects ...config.ProjectConfig) Model {
	t.Helper()
	cfg := &config.Config{Projects: projects}
	b := []byte("# agent\n")
	state := NewAppState(cfg, b, b, b, b, nil, b, b, stubHookScripts(), io.Discard)
	return NewModel(state)
}

func TestRedeployTargets_SkipsNeverDeployed(t *testing.T) {
	full, partial, none := t.TempDir(), t.TempDir(), t.TempDir()
	writeDeployed(t, full, len(deployedFiles))
	writeDeployed(t, partial, 1)

	m := makeRedeployTestModel(t, projectAt("full", full), projectAt("partial", partial), projectAt("none", none))
	var ids []string
	for _, p := range m.redeployTargets() {
		ids = append(ids, p.ID)
	}
	if got := strings.Join(ids, ","); got != "full,partial" {
		t.Errorf("redeployTargets = %q, want %q", got, "full,partial")
	}
}

func TestBusyProjectNames(t *testing.T) {
	m := makeRedeployTestModel(t,
		projectAt("p1", t.TempDir()), projectAt("p2", t.TempDir()),
		projectAt("p3", t.TempDir()), projectAt("p4", t.TempDir()))
	m.state.activeTerminals["focus:p2:5#2"] = &ActiveTerminal{State: watcher.StateWorking}
	m.state.loops["p3"] = &loop.LoopState{Active: true}
	m.state.loops["p4"] = &loop.LoopState{Active: false} // stopped loop is not busy

	got := strings.Join(m.busyProjectNames(m.state.projects), ",")
	if got != "Project p2,Project p3" {
		t.Errorf("busyProjectNames = %q, want %q", got, "Project p2,Project p3")
	}
}

func TestRedeployAllProjectsCmd_RedeploysOnlyDeployedProjects(t *testing.T) {
	a, b, untouched := t.TempDir(), t.TempDir(), t.TempDir()
	writeDeployed(t, a, len(deployedFiles))
	writeDeployed(t, b, 2) // partial → gets completed

	m := makeRedeployTestModel(t, projectAt("a", a), projectAt("b", b), projectAt("c", untouched))
	m.state.loops["a"] = &loop.LoopState{Active: true}

	msg, ok := m.redeployAllProjectsCmd()().(StatusMsg)
	if !ok {
		t.Fatalf("expected StatusMsg")
	}
	for _, dir := range []string{a, b} {
		if s := deployStatus(dir); s != DeployFull {
			t.Errorf("%s: deployStatus = %v, want DeployFull", dir, s)
		}
	}
	if s := deployStatus(untouched); s != DeployNone {
		t.Errorf("never-deployed project was written to (status %v)", s)
	}
	if !strings.Contains(msg.Text, "2/2") {
		t.Errorf("status %q should report 2/2", msg.Text)
	}
	if !strings.Contains(msg.Text, "Project a") {
		t.Errorf("status %q should name the busy project", msg.Text)
	}
}

func TestRedeployProjectCmd_DeploysUndeployedProject(t *testing.T) {
	dir := t.TempDir()
	m := makeRedeployTestModel(t, projectAt("x", dir))

	if _, ok := m.redeployProjectCmd(m.state.projects[0])().(StatusMsg); !ok {
		t.Fatalf("expected StatusMsg")
	}
	if s := deployStatus(dir); s != DeployFull {
		t.Errorf("deployStatus = %v, want DeployFull", s)
	}
}
