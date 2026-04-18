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
