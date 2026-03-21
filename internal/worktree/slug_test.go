package worktree

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		title  string
		maxLen int
		want   string
	}{
		{"EtherCAT reconnect backoff", 0, "ethercat-reconnect-backoff"},
		{"EtherCAT 斷線重連 backoff", 0, "ethercat-backoff"},
		{"foo_bar (baz) - qux!", 0, "foo-bar-baz-qux"},
		{"v2.0 release", 0, "v2-0-release"},
		{"", 0, ""},
		{"---", 0, ""},
		{"  hello  world  ", 0, "hello-world"},
		{"修正 Z 軸 Homing 邏輯", 0, "z-homing"},
		// Truncation
		{"this is a very long title that should be truncated at the max length boundary", 20, "this-is-a-very-long"},
		// Truncation trims trailing hyphen
		{"aaaa-bbbb-cccc-dddd", 10, "aaaa-bbbb"},
		// Numbers preserved
		{"issue 123 fix", 0, "issue-123-fix"},
	}

	for _, tt := range tests {
		got := Slugify(tt.title, tt.maxLen)
		if got != tt.want {
			t.Errorf("Slugify(%q, %d) = %q, want %q", tt.title, tt.maxLen, got, tt.want)
		}
	}
}
