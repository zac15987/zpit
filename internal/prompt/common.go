package prompt

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/tracker"
)

// formatScope formats scope entries as "[action] path (reason)" lines.
func formatScope(scope []tracker.ScopeEntry) string {
	var b strings.Builder
	for _, s := range scope {
		fmt.Fprintf(&b, "[%s] %s", s.Action, s.Path)
		if s.Reason != "" {
			fmt.Fprintf(&b, " (%s)", s.Reason)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
