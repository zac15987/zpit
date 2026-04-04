package tui

import "testing"

func TestBaseProjectID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "plain project ID", input: "myproject", expected: "myproject"},
		{name: "multi-session suffix", input: "myproject#2", expected: "myproject"},
		{name: "multi-session suffix #3", input: "myproject#3", expected: "myproject"},
		{name: "focus format", input: "focus:myproject:19", expected: "myproject"},
		{name: "focus format with multi-session suffix", input: "focus:myproject:19#2", expected: "myproject"},
		{name: "empty string", input: "", expected: ""},
		{name: "focus prefix only", input: "focus:", expected: ""},
		{name: "focus with project only", input: "focus:myproject", expected: "myproject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := baseProjectID(tt.input)
			if got != tt.expected {
				t.Errorf("baseProjectID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
