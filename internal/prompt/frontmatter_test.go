package prompt

import "testing"

func TestFrontmatterField(t *testing.T) {
	tests := []struct {
		name  string
		md    string
		field string
		want  string
	}{
		{
			name:  "standard",
			md:    "---\nname: reviewer\ndisallowedTools: Edit\n---\nbody",
			field: "disallowedTools",
			want:  "Edit",
		},
		{
			name:  "multi value",
			md:    "---\nname: reviewer\ndisallowedTools: Write, Edit\n---\n",
			field: "disallowedTools",
			want:  "Write, Edit",
		},
		{
			name:  "field not present",
			md:    "---\nname: coding\ndescription: Coding agent\n---\n",
			field: "disallowedTools",
			want:  "",
		},
		{
			name:  "no frontmatter",
			md:    "just plain text",
			field: "disallowedTools",
			want:  "",
		},
		{
			name:  "malformed single marker",
			md:    "---\nname: reviewer\n",
			field: "name",
			want:  "",
		},
		{
			name:  "CRLF",
			md:    "---\r\nname: reviewer\r\ndisallowedTools: Edit\r\n---\r\nbody",
			field: "disallowedTools",
			want:  "Edit",
		},
		{
			name:  "extra whitespace around value",
			md:    "---\ndisallowedTools:  Edit \n---\n",
			field: "disallowedTools",
			want:  "Edit",
		},
		{
			name:  "empty value",
			md:    "---\ndisallowedTools:\n---\n",
			field: "disallowedTools",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FrontmatterField([]byte(tt.md), tt.field)
			if got != tt.want {
				t.Errorf("FrontmatterField(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}
