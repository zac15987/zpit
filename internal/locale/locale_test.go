package locale

import (
	"strings"
	"testing"
)

func TestResponseInstruction_AlwaysEnglish(t *testing.T) {
	for _, lang := range []string{"en", "zh-TW", "zh-tw", "zh", "fr"} {
		SetLanguage(lang)
		got := ResponseInstruction()
		if got == "" {
			t.Errorf("lang=%q: ResponseInstruction() returned empty", lang)
		}
		if !strings.Contains(got, "English") {
			t.Errorf("lang=%q: instruction missing 'English' directive: %q", lang, got)
		}
		if strings.Contains(got, "Traditional Chinese") {
			t.Errorf("lang=%q: instruction still references Chinese: %q", lang, got)
		}
	}
}

func TestResponseInstruction_NonOverridable(t *testing.T) {
	SetLanguage("zh-TW")
	got := ResponseInstruction()
	if !strings.Contains(got, "non-negotiable") && !strings.Contains(got, "cannot be overridden") {
		t.Errorf("instruction should declare the rule as non-negotiable: %q", got)
	}
}

func TestClarifierResponseInstruction_EnglishMatchesStrict(t *testing.T) {
	SetLanguage("en")
	got := ClarifierResponseInstruction()
	want := ResponseInstruction()
	if got != want {
		t.Errorf("English clarifier instruction should equal strict ResponseInstruction(); got=%q want=%q", got, want)
	}
}

func TestClarifierResponseInstruction_ZhTW(t *testing.T) {
	SetLanguage("zh-TW")
	got := ClarifierResponseInstruction()
	if got == "" {
		t.Fatal("zh-TW clarifier instruction should not be empty")
	}
	// Dialogue side: must reference Traditional Chinese.
	if !strings.Contains(got, "Traditional Chinese") && !strings.Contains(got, "繁體中文") {
		t.Errorf("zh-TW clarifier instruction should permit Traditional Chinese in conversation: %q", got)
	}
	// Artifact side: Issue Spec must still be English.
	if !strings.Contains(got, "Issue Spec") {
		t.Errorf("zh-TW clarifier instruction should reference Issue Spec: %q", got)
	}
	if !strings.Contains(got, "English") {
		t.Errorf("zh-TW clarifier instruction should require English for Issue Spec: %q", got)
	}
	// Non-negotiable framing.
	if !strings.Contains(got, "non-negotiable") {
		t.Errorf("zh-TW clarifier instruction should declare the rule as non-negotiable: %q", got)
	}
}

func TestClarifierResponseInstruction_UnknownLocaleFallback(t *testing.T) {
	SetLanguage("fr")
	// Unknown locales fall back to strict English (same as ResponseInstruction).
	got := ClarifierResponseInstruction()
	if got != ResponseInstruction() {
		t.Errorf("Unknown locale should fall back to strict English; got=%q", got)
	}
}

func TestTStillLocalizes(t *testing.T) {
	// Sanity check: the TUI translation path is unaffected by the English-only
	// agent rule. T() should still switch based on SetLanguage.
	SetLanguage("en")
	enVal := T(KeyProjects)
	SetLanguage("zh-TW")
	zhVal := T(KeyProjects)
	if enVal == "" || zhVal == "" {
		t.Fatalf("KeyProjects translations missing: en=%q zh=%q", enVal, zhVal)
	}
	if enVal == zhVal {
		t.Errorf("TUI translations should differ between en/zh-TW, both returned %q", enVal)
	}
}
