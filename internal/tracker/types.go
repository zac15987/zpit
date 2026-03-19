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

// IssueTracker is the abstract interface for all issue tracker providers.
type IssueTracker interface {
	ListIssues(project string, status string) ([]Issue, error)
	GetIssue(project string, id string) (*Issue, error)
	CreateIssue(project string, issue NewIssue) (*Issue, error)
	UpdateStatus(project string, id string, status string) error
	AddComment(project string, id string, comment string) error
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

// GitHost is the abstract interface for all git hosting providers.
type GitHost interface {
	CreatePR(repo string, pr NewPR) (*PR, error)
	GetPRStatus(repo string, id string) (*PRStatus, error)
}
