package tui

import (
	"strings"
	"testing"
)

func TestInjectFrontmatterModel_EmptyModel_Unchanged(t *testing.T) {
	md := []byte("---\nname: task-runner\n---\nbody")
	got := injectFrontmatterModel(md, "")
	if string(got) != string(md) {
		t.Errorf("empty model should return md unchanged; got %q", got)
	}
}

func TestInjectFrontmatterModel_NoFrontmatter_Unchanged(t *testing.T) {
	md := []byte("just some body text, no frontmatter")
	got := injectFrontmatterModel(md, "sonnet")
	if string(got) != string(md) {
		t.Errorf("missing frontmatter should return md unchanged; got %q", got)
	}
}

func TestInjectFrontmatterModel_MalformedFrontmatter_Unchanged(t *testing.T) {
	// Only one `---` delimiter (unclosed frontmatter).
	md := []byte("---\nname: task-runner\nbody without closing delimiter")
	got := injectFrontmatterModel(md, "sonnet")
	if string(got) != string(md) {
		t.Errorf("malformed frontmatter should return md unchanged; got %q", got)
	}
}

func TestInjectFrontmatterModel_InsertsWhenAbsent(t *testing.T) {
	md := []byte("---\nname: task-runner\ndescription: desc\ntools: Read, Write\n---\nbody here")
	got := string(injectFrontmatterModel(md, "sonnet"))

	if !strings.Contains(got, "model: sonnet") {
		t.Errorf("expected model line to be inserted; got %q", got)
	}
	// Preserve other fields.
	if !strings.Contains(got, "name: task-runner") {
		t.Errorf("name field missing after injection; got %q", got)
	}
	if !strings.Contains(got, "description: desc") {
		t.Errorf("description field missing after injection; got %q", got)
	}
	if !strings.Contains(got, "tools: Read, Write") {
		t.Errorf("tools field missing after injection; got %q", got)
	}
	// Body should be preserved.
	if !strings.Contains(got, "body here") {
		t.Errorf("body missing after injection; got %q", got)
	}
	// model line must sit inside the frontmatter block (before the closing ---).
	closingIdx := strings.Index(got[4:], "---") // skip opening "---\n"
	if closingIdx < 0 {
		t.Fatalf("closing delimiter missing; got %q", got)
	}
	frontmatter := got[:4+closingIdx]
	if !strings.Contains(frontmatter, "model: sonnet") {
		t.Errorf("model line not inside frontmatter block; got %q", got)
	}
}

func TestInjectFrontmatterModel_OverwritesExisting(t *testing.T) {
	md := []byte("---\nname: task-runner\nmodel: opus\ntools: Read\n---\nbody")
	got := string(injectFrontmatterModel(md, "sonnet"))

	if !strings.Contains(got, "model: sonnet") {
		t.Errorf("expected model to be overwritten to sonnet; got %q", got)
	}
	if strings.Contains(got, "model: opus") {
		t.Errorf("old model value should be replaced; got %q", got)
	}
	// Only one model line.
	if strings.Count(got, "model:") != 1 {
		t.Errorf("expected exactly one model line; got %d in %q", strings.Count(got, "model:"), got)
	}
}

func TestInjectFrontmatterModel_PreservesCRLF(t *testing.T) {
	md := []byte("---\r\nname: task-runner\r\ntools: Read\r\n---\r\nbody\r\n")
	got := string(injectFrontmatterModel(md, "sonnet"))

	if !strings.Contains(got, "\r\n") {
		t.Errorf("CRLF line endings should be preserved; got %q", got)
	}
	// Must NOT contain bare LF lines (all were CRLF in input).
	lfOnly := strings.ReplaceAll(got, "\r\n", "")
	if strings.Contains(lfOnly, "\n") {
		t.Errorf("output contains bare LF after CRLF normalization; got %q", got)
	}
	if !strings.Contains(got, "model: sonnet") {
		t.Errorf("model line missing in CRLF input; got %q", got)
	}
}

func TestInjectFrontmatterModel_AcceptsFullModelID(t *testing.T) {
	md := []byte("---\nname: task-runner\n---\nbody")
	got := string(injectFrontmatterModel(md, "claude-sonnet-4-6-20250929"))

	if !strings.Contains(got, "model: claude-sonnet-4-6-20250929") {
		t.Errorf("expected full model ID to be preserved verbatim; got %q", got)
	}
}

func TestInjectFrontmatterModel_AcceptsBracketSuffix(t *testing.T) {
	// zpit's "1M context" suffix syntax.
	md := []byte("---\nname: task-runner\n---\nbody")
	got := string(injectFrontmatterModel(md, "opus[1m]"))

	if !strings.Contains(got, "model: opus[1m]") {
		t.Errorf("expected bracket-suffix model to be preserved; got %q", got)
	}
}
