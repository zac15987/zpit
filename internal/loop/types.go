package loop

import "time"

const (
	DefaultPollSeconds   = 15 // how often to poll tracker for todo issues (seconds)
	DefaultPRPollSeconds = 30 // how often to poll for PR merge (seconds)
	LivenessInterval       = 5 * time.Second  // how often to check agent PID liveness
	DefaultMaxReviewRounds = 3                // max coding↔review cycles before human intervention
)

// Verdict constants for review result detection.
const (
	VerdictApproved     = "approved"
	VerdictNeedsChanges = "needs_changes"
	VerdictUnknown      = "unknown"
)

// SlotState represents the pipeline state of a single issue in the loop.
type SlotState int

const (
	SlotCreatingWorktree SlotState = iota
	SlotWritingAgent
	SlotLaunchingCoder
	SlotCoding
	SlotLaunchingReviewer
	SlotReviewing
	SlotCheckingReview
	SlotWaitingPRMerge
	SlotCleaningUp
	SlotDone
	SlotNeedsHuman
	SlotError
)

func (s SlotState) String() string {
	switch s {
	case SlotCreatingWorktree:
		return "building worktree"
	case SlotWritingAgent:
		return "preparing agent"
	case SlotLaunchingCoder:
		return "launching coder"
	case SlotCoding:
		return "coding"
	case SlotLaunchingReviewer:
		return "launching reviewer"
	case SlotReviewing:
		return "reviewing"
	case SlotCheckingReview:
		return "checking review"
	case SlotWaitingPRMerge:
		return "waiting PR merge"
	case SlotCleaningUp:
		return "cleaning up"
	case SlotDone:
		return "done"
	case SlotNeedsHuman:
		return "needs human"
	case SlotError:
		return "error"
	default:
		return "unknown"
	}
}

// Slot tracks a single issue through the automation pipeline.
type Slot struct {
	ProjectID    string
	IssueID      string
	IssueTitle   string
	BranchName   string    // feature branch name, e.g. "feat/ISSUE-ID-slug"
	BaseBranch   string    // PR target branch, resolved from Issue Spec or project config
	WorktreePath string
	State        SlotState
	ReviewRound  int // 0-based; incremented on each NEEDS CHANGES retry
	Error        error
	SessionPID   int
	LaunchedAt   int64 // unix timestamp captured just before agent launch
}

// LoopState tracks per-project loop status.
type LoopState struct {
	Active bool
	Slots  map[string]*Slot
}

// SlotKey returns the map key for a slot.
func SlotKey(projectID, issueID string) string {
	return projectID + ":" + issueID
}
