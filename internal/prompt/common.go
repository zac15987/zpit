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

// acSelfCheckExample returns a PASS/FAIL walk-through inserted after the AC
// self-check instructions in coding and revision workflows. mode "initial" is
// for first-pass self-check (coding); mode "revision" maps reviewer MUST FIX
// items onto AC verifications (revision). The walk-through uses a hypothetical
// log-format AC to show what verbatim quoting, file:line anchoring, strict-word
// checking, and character-by-character comparison actually look like — Sonnet
// reads the abstract rule "quote verbatim" but needs a concrete PASS/FAIL
// example to apply it consistently. Returned string starts with a blank line
// and ends with a blank line so it drops cleanly between workflow steps.
func acSelfCheckExample(mode string) string {
	if mode == "revision" {
		return `
   **Worked example** (hypothetical reviewer MUST FIX: ` + "\"🔴 AC-2 log format is wrong — missing `=` separator and `->` should be Unicode arrow\"" + `):

   PASS walk-through:
     Reviewer wording verbatim: ` + "\"AC-2 log format is wrong — missing `=` separator and `->` should be Unicode arrow\"" + `
     → Mapped AC: ` + "`AC-2: Log every lifecycle transition as [loop] slot=<key> state=<from>→<to>`" + `
     (a) Fix location: ` + "`internal/tui/loop_handler.go:147`" + ` — now ` + "`logger.Info(\"[loop] slot=%s state=%s→%s\", slot.Key, prev, next)`" + `
     (b) Re-read AC verbatim: fix matches the AC format character-for-character
     (c) Strict-word check: "every" → still satisfied (all 8 transitions use the new format; grep ` + "`logger.Info.*state=`" + ` returns 8 matches)
     (d) Character-by-character format check:
         AC format:  ` + "`[loop] slot=<key> state=<from>→<to>`" + `
         Actual out: ` + "`[loop] slot=zpit-42 state=coding→reviewing`" + `
         → ` + "`slot=`" + ` ✓   ` + "`→`" + ` (U+2192) ✓
     (f) Regression check: AC-1 (first-retry interval) untouched; no call site changed.
     Verdict: PASS

   FAIL walk-through (fix addresses literal reviewer wording but misses the AC):
     Reviewer wording: same as above.
     (a) Fix location: ` + "`internal/tui/loop_handler.go:147`" + ` — now ` + "`logger.Info(\"[loop] slot=%s state=%s->%s\", ...)`" + ` (fixed ` + "`=`" + ` but left ` + "`->`" + `)
     (d) Character-by-character format check:
         → ` + "`slot=`" + ` ✓
         → ` + "`→`" + ` (U+2192) vs ` + "`->`" + ` : MISMATCH — reviewer mentioned the arrow but fix only addressed the separator
     Verdict: FAIL — STOP, do not push. Post PR comment: "AC-2 arrow still ASCII after fix; need Unicode → (U+2192)."

   **Worked example #2 — cross-product trace for branching dispatch** (hypothetical reviewer MUST FIX: ` + "\"🔴 AC-5 dispatch surface drops valid input under one focus state\"" + `):

   PASS walk-through:
     Reviewer wording verbatim: ` + "\"AC-5 dispatch surface drops valid input under one focus state\"" + `
     → Mapped AC: ` + "`AC-5: dispatch surface S accepts every defined input under every defined context state`" + `
     (a) Fix location: ` + "`<path>:<line>`" + ` — gate the rule for the typed/discrete event class before falling through to alias-matched lookup, conditioned on the relevant context state
     (e) Cross-product table — context state × input axis (the surface has two competing matchers: an alias-bound rule and a literal pass-through rule):
         | context state | input ` + "`A`" + `         | input ` + "`B`" + `         | input ` + "`C`" + ` (alias) | input ` + "`D`" + ` (alias) |
         | ------------- | ----------------- | ----------------- | ------------------- | ------------------- |
         | state-1       | passes through ✓  | passes through ✓  | passes through ✓    | passes through ✓    |
         | state-2       | matches rule R1 ✓ | matches rule R1 ✓ | matches rule R2 ✓   | matches rule R2 ✓   |
         Each cell traced to ` + "`<path>:<line-range>`" + `; verified by replaying inputs for both context states.
     (f) Regression check: AC-3 (state-2 alias behaviour) untouched — the state-2 row of the table proves the prior behaviour still holds.
     Verdict: PASS

   FAIL walk-through (cross-product not enumerated):
     (a) Fix location: ` + "`<path>:<line>`" + ` — added a guard for the obvious dispatch path
     (e) Cross-product table not produced; self-trace only covered the headline path.
     Hidden cell: ` + "`(state-1, input C)`" + ` — still consumed by the alias-bound rule because the alias lookup runs unconditionally regardless of context state.
     Verdict: FAIL — STOP. Build the table; verify every cell, including inputs that share an identity with rule aliases.

`
	}
	return `
   **Worked example** (hypothetical AC ` + "`AC-2: Log every lifecycle transition as [loop] slot=<key> state=<from>→<to>`" + `):

   PASS walk-through:
     (a) AC text verbatim: ` + "`AC-2: Log every lifecycle transition as [loop] slot=<key> state=<from>→<to>`" + `
     (b) Concrete artifact: ` + "`internal/tui/loop_handler.go:147`" + ` — ` + "`logger.Info(\"[loop] slot=%s state=%s→%s\", slot.Key, prev, next)`" + `
     (c) Strict-word check: "every" → confirmed logger is called on ALL 8 state transitions (grep ` + "`logger.Info.*state=`" + ` returns 8 matches covering Creating/Launching/Coding/Reviewing/Merging/Cleaning/Done/Error)
     (d) Character-by-character format check:
         AC format:  ` + "`[loop] slot=<key> state=<from>→<to>`" + `
         Actual out: ` + "`[loop] slot=zpit-42 state=coding→reviewing`" + `
         → prefix ` + "`[loop] `" + ` ✓   ` + "`slot=`" + ` ✓   space separator ✓   arrow ` + "`→`" + ` (U+2192) ✓
     Verdict: PASS

   FAIL walk-through (same AC, weaker implementation):
     (b) Concrete artifact: ` + "`internal/tui/loop_handler.go:147`" + ` — ` + "`logger.Info(\"[loop] slot %s: %s -> %s\", ...)`" + `
     (d) Character-by-character format check:
         AC format:  ` + "`[loop] slot=<key> state=<from>→<to>`" + `
         Actual out: ` + "`[loop] slot zpit-42: coding -> reviewing`" + `
         → ` + "`slot=`" + ` vs ` + "`slot `" + ` : MISMATCH (missing ` + "`=`" + `)
         → ` + "`→`" + ` (U+2192) vs ` + "`->`" + ` : MISMATCH (ASCII substitute for Unicode arrow)
     Verdict: FAIL — STOP, do not commit. Post issue comment naming AC-2 as unmet.

   **Worked example #2 — coverage-claim sweep** (hypothetical AC ` + "`AC-3: All user-facing strings in the SCOPE files must route through the locale function. No hardcoded literals.`" + `):

   PASS walk-through:
     (a) AC text verbatim: ` + "`AC-3: All user-facing strings in the SCOPE files must route through the locale function. No hardcoded literals.`" + `
     (e) Coverage-claim sweep — strict-word "All" + "No" triggers enumeration:
         Grep candidate set across SCOPE files (regex ` + "`\"[^\"]+\"`" + ` over view_X / view_Y / handler_Z):
         | candidate                         | location          | verdict                         |
         | --------------------------------- | ----------------- | ------------------------------- |
         | "Loading..."                      | view_X:42         | locale.T(KeyLoading) ✓          |
         | "Error: %s"                       | view_X:67         | locale.T(KeyErrorFmt) ✓         |
         | "Saved"                           | view_Y:91         | locale.T(KeySaved) ✓            |
         | "submit"                          | handler_Z:38      | API token, NOT user-facing — skip ✓ |
         | ... (rest of the 14 candidates)   | ...               | all routed ✓                    |
         14/14 compliant.
     Verdict: PASS

   FAIL walk-through (one literal escapes the sweep):
     (e) Sweep table:
         | candidate            | location  | verdict                     |
         | -------------------- | --------- | --------------------------- |
         | "Loading..."         | view_X:42 | locale.T(KeyLoading) ✓      |
         | "  Importing..."     | view_X:67 | returned as-is, NOT routed ✗|
         | ... (rest compliant) | ...       | ...                         |
         13/14 compliant — one non-compliant literal at view_X:67.
     Verdict: FAIL — STOP, do not commit. Fix the offending literal and re-run the sweep.

`
}
