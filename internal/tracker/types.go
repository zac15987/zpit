package tracker

// Internal canonical issue statuses. Each provider maps these to its own labels/columns.
const (
	StatusPendingConfirm = "pending_confirm"
	StatusTodo           = "todo"
	StatusInProgress     = "in_progress"
	StatusAIReview       = "ai_review"
	StatusWaitingReview  = "waiting_review"
	StatusNeedsVerify    = "needs_verify"
	StatusDone           = "done"
)

type Issue struct {
	ID       string
	Title    string
	Status   string
	Priority string
	Labels   []string
	Body     string
}

type NewIssue struct {
	Title    string
	Body     string
	Priority string
	Labels   []string
}

type PR struct {
	ID    string
	URL   string
	Title string
}

type NewPR struct {
	Title        string
	Body         string
	SourceBranch string
	TargetBranch string
}

type PRStatus struct {
	ID    string
	State string // "open", "merged", "closed"
	URL   string
}

// PRInfo contains summary info for an open PR (used by Loop scan).
type PRInfo struct {
	ID     string // PR number as string
	Title  string
	Branch string // head ref, e.g. "feat/19-docs-readme-md"
	State  string // "open"
	URL    string
}

// LabelDef describes a label that Zpit requires in the tracker.
type LabelDef struct {
	Name  string
	Color string // hex with #, e.g. "#0075ca"
}

// RequiredLabels is the set of labels Zpit expects in every project's tracker.
var RequiredLabels = []LabelDef{
	{Name: "pending", Color: "#bfd4f2"},
	{Name: "todo", Color: "#0075ca"},
	{Name: "wip", Color: "#e4e669"},
	{Name: "review", Color: "#d876e3"},
	{Name: "ai-review", Color: "#0e8a16"},
	{Name: "needs-changes", Color: "#d93f0b"},
}

