package loop

import "time"

const (
	PollInterval      = 30 * time.Second // how often to poll tracker for todo issues
	PRPollInterval    = 60 * time.Second // how often to poll for PR merge
	LivenessInterval  = 5 * time.Second  // how often to check agent PID liveness
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
	SlotWaitingPRMerge
	SlotCleaningUp
	SlotDone
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
	case SlotWaitingPRMerge:
		return "waiting PR merge"
	case SlotCleaningUp:
		return "cleaning up"
	case SlotDone:
		return "done"
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
	BranchName   string
	WorktreePath string
	State        SlotState
	Error        error
	SessionPID   int
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
