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
