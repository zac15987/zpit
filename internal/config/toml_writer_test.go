package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateLines_FieldExists_InPlaceUpdate(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		"channel_enabled = false",
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		"channel_enabled = true",
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_FieldMissing_InsertAtBlockEnd(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"",
		"[[projects]]",
		`id = "proj-b"`,
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"channel_enabled = true",
		"",
		"[[projects]]",
		`id = "proj-b"`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_TrailingComment_Preserved(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		"channel_enabled = false  # toggle this",
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		"channel_enabled = true  # toggle this",
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_MultipleProjects_OnlyMatchingModified(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		"channel_enabled = false",
		"",
		"[[projects]]",
		`id = "proj-b"`,
		"channel_enabled = false",
		"",
		"[[projects]]",
		`id = "proj-c"`,
		"channel_enabled = false",
	}

	got, err := updateLines(input, "proj-b", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		"channel_enabled = false",
		"",
		"[[projects]]",
		`id = "proj-b"`,
		"channel_enabled = true",
		"",
		"[[projects]]",
		`id = "proj-c"`,
		"channel_enabled = false",
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_ChannelListen_StringSlice(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`channel_listen = ["_global"]`,
	}

	got, err := updateLines(input, "proj-a", "channel_listen", []string{"_global", "other-proj"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`channel_listen = ["_global", "other-proj"]`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_PreservesAllOtherContent(t *testing.T) {
	input := []string{
		"# Top-level comment",
		`language = "en"`,
		"",
		"[terminal]",
		`windows_mode = "new_tab"`,
		"",
		"# Project section",
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"channel_enabled = false",
		`tags = ["go", "bubbletea"]`,
		"",
		"[projects.path]",
		`windows = "D:/Projects/proj-a"`,
		"",
		"[[projects]]",
		`id = "proj-b"`,
		`name = "Project B"`,
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the channel_enabled line should change.
	want := []string{
		"# Top-level comment",
		`language = "en"`,
		"",
		"[terminal]",
		`windows_mode = "new_tab"`,
		"",
		"# Project section",
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"channel_enabled = true",
		`tags = ["go", "bubbletea"]`,
		"",
		"[projects.path]",
		`windows = "D:/Projects/proj-a"`,
		"",
		"[[projects]]",
		`id = "proj-b"`,
		`name = "Project B"`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_ProjectNotFound_ReturnsError(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		"channel_enabled = false",
	}

	_, err := updateLines(input, "nonexistent", "channel_enabled", true)
	if err == nil {
		t.Fatal("expected error for missing project, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention project ID, got: %v", err)
	}
}

func TestUpdateLines_UnsupportedField_ReturnsError(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
	}

	_, err := updateLines(input, "proj-a", "name", "new-name")
	if err == nil {
		t.Fatal("expected error for unsupported field, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported, got: %v", err)
	}
}

func TestUpdateLines_WrongValueType_ReturnsError(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
	}

	_, err := updateLines(input, "proj-a", "channel_enabled", "true")
	if err == nil {
		t.Fatal("expected error for wrong value type, got nil")
	}
	if !strings.Contains(err.Error(), "bool") {
		t.Errorf("error should mention bool, got: %v", err)
	}
}

func TestUpdateLines_InsertChannelListen_NewField(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
	}

	got, err := updateLines(input, "proj-a", "channel_listen", []string{"_global"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		`channel_listen = ["_global"]`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_EmptyChannelListen(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`channel_listen = ["_global"]`,
	}

	got, err := updateLines(input, "proj-a", "channel_listen", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`channel_listen = []`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_SingleQuotedID(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = 'proj-a'`,
		"channel_enabled = false",
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = 'proj-a'`,
		"channel_enabled = true",
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_ChannelListenWithTrailingComment(t *testing.T) {
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`channel_listen = ["_global"]  # subscribe to global`,
	}

	got, err := updateLines(input, "proj-a", "channel_listen", []string{"_global", "other"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`channel_listen = ["_global", "other"]  # subscribe to global`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_BlockWithSubTable(t *testing.T) {
	// [projects.path] sub-table is part of the same block.
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"channel_enabled = false",
		"",
		"[projects.path]",
		`windows = "D:/Projects/proj-a"`,
		"",
		"[[projects]]",
		`id = "proj-b"`,
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"channel_enabled = true",
		"",
		"[projects.path]",
		`windows = "D:/Projects/proj-a"`,
		"",
		"[[projects]]",
		`id = "proj-b"`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateLines_InsertBeforeSubTable(t *testing.T) {
	// When inserting a new field into a block that has a sub-table,
	// the field should be inserted in the main block area (before blank lines
	// that precede the sub-table section or next header).
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"",
		"[projects.path]",
		`windows = "D:/Projects/proj-a"`,
		"",
		"[[projects]]",
		`id = "proj-b"`,
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"channel_enabled = true",
		"",
		"[projects.path]",
		`windows = "D:/Projects/proj-a"`,
		"",
		"[[projects]]",
		`id = "proj-b"`,
	}
	assertLinesEqual(t, want, got)
}

func TestUpdateProjectField_FileRoundtrip(t *testing.T) {
	content := `[[projects]]
id = "proj-a"
channel_enabled = false

[[projects]]
id = "proj-b"
channel_enabled = true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := UpdateProjectField(path, "proj-a", "channel_enabled", true); err != nil {
		t.Fatalf("UpdateProjectField failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}

	got := string(data)
	want := `[[projects]]
id = "proj-a"
channel_enabled = true

[[projects]]
id = "proj-b"
channel_enabled = true
`
	if got != want {
		t.Errorf("file content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateLines_LastProjectAtEOF(t *testing.T) {
	// Project block is the last thing in the file, no trailing newline.
	input := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
	}

	got, err := updateLines(input, "proj-a", "channel_enabled", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"[[projects]]",
		`id = "proj-a"`,
		`name = "Project A"`,
		"channel_enabled = true",
	}
	assertLinesEqual(t, want, got)
}

// assertLinesEqual compares two string slices and reports differences.
func assertLinesEqual(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("line count: want %d, got %d\nwant:\n%s\ngot:\n%s",
			len(want), len(got),
			strings.Join(want, "\n"),
			strings.Join(got, "\n"))
		return
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("line %d:\n  want: %q\n  got:  %q", i, want[i], got[i])
		}
	}
}
