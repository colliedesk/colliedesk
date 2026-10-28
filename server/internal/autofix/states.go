// Package autofix holds the bridge version of Autofix: a person sends a conversation to a coding agent
// the workspace already uses, on a private GitHub repository, and follows it to a pull request, its
// merge and the release. This file is the life of one fix attempt: its states, the reasons it fails or
// is cancelled, and which moves each source may make. It knows nothing of storage or HTTP, so every rule
// is tested on its own; the store applies them under a row lock (store.ApplyFixObservation).
package autofix

import (
	"errors"
	"fmt"
	"slices"
)

// State is where a fix attempt is.
type State string

const (
	// Sending: the content is stored; it waits for the person's GitHub authorization (issue connectors)
	// or for the worker (Cursor).
	Sending State = "sending"
	// Sent: the vendor took it (the issue exists, the Cursor agent was created).
	Sent State = "sent"
	// Working: a sign that the agent runs (its first comment, the Copilot assignment, Cursor RUNNING).
	Working   State = "working"
	PROpen    State = "pr_open"
	Merged    State = "merged"
	Deployed  State = "deployed"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

// States lists every state, the happy path first.
var States = []State{Sending, Sent, Working, PROpen, Merged, Deployed, Failed, Cancelled}

// Valid reports whether s is a known state.
func (s State) Valid() bool { return slices.Contains(States, s) }

// InProgress states hold the conversation's one attempt in progress: a new one waits for it to end.
// The database holds the same list (fix_attempts_one_in_progress).
func (s State) InProgress() bool { return s == Sending || s == Sent || s == Working || s == PROpen }

// Ended states take a retry (a new attempt).
func (s State) Ended() bool { return s == Failed || s == Cancelled }

// rank orders the happy path; an automatic signal only moves forward on it.
func (s State) rank() int {
	return slices.Index([]State{Sending, Sent, Working, PROpen, Merged, Deployed}, s)
}

// Connector is the coding agent a fix goes to and how.
type Connector string

const (
	// ClaudeIssue: an issue created as the person, then an @claude comment for the customer's
	// claude-code-action.
	ClaudeIssue Connector = "claude_issue"
	// CopilotIssue: an issue created as the person, assigned to Copilot (or assigned on GitHub by hand).
	CopilotIssue Connector = "copilot_issue"
	// CursorAPI: a Cursor cloud agent created with the workspace's key, followed by polling.
	CursorAPI Connector = "cursor_api"
)

// Connectors lists every connector, in the order the settings show them.
var Connectors = []Connector{ClaudeIssue, CopilotIssue, CursorAPI}

// Valid reports whether c is a known connector.
func (c Connector) Valid() bool { return slices.Contains(Connectors, c) }

// FailedReasons are why an attempt failed; CancelledReasons why it was cancelled. The database checks
// the same lists (fix_attempts CHECK) and the contract names them (FixOutcomeReason).
var (
	FailedReasons = []string{
		"vendor_auth", "vendor_repo_access", "vendor_rejected", "provider_unavailable", "no_repo_access",
		"copilot_unavailable", "repo_public", "pr_closed", "agent_error", "timed_out", "interrupted",
	}
	CancelledReasons = []string{
		"cancelled_by_person", "authorization_abandoned", "authorization_denied", "repo_disconnected",
		"conversation_deleted", "issue_closed", "connector_disabled",
	}
)

// Reasons is every outcome reason.
func Reasons() []string { return append(slices.Clone(FailedReasons), CancelledReasons...) }

// ErrMove is a move the rules refuse; the caller answers 409.
var ErrMove = errors.New("this move is not allowed from the fix's state")

// ErrReason is a missing or wrong outcome reason for the target state.
var ErrReason = errors.New("the outcome reason does not fit the state")

// CheckReason checks reason against the target state: failed and cancelled need one of their own, every
// other state none.
func CheckReason(to State, reason string) error {
	switch to {
	case Failed:
		if !slices.Contains(FailedReasons, reason) {
			return fmt.Errorf("%w: failed needs one of %v, not %q", ErrReason, FailedReasons, reason)
		}
	case Cancelled:
		if !slices.Contains(CancelledReasons, reason) {
			return fmt.Errorf("%w: cancelled needs one of %v, not %q", ErrReason, CancelledReasons, reason)
		}
	default:
		if reason != "" {
			return fmt.Errorf("%w: %s takes no reason", ErrReason, to)
		}
	}
	return nil
}

// ActorKind is who made a move or sent a signal.
type ActorKind string

const (
	ActorPerson ActorKind = "person"
	ActorToken  ActorKind = "token"
	ActorGitHub ActorKind = "github"
	ActorCursor ActorKind = "cursor"
	ActorSystem ActorKind = "system"
)

// Actor names who made a move: a person or an agent token (their agent id and name), GitHub (the login
// that acted), Cursor or the server itself.
type Actor struct {
	Kind ActorKind
	ID   string
	Name string
}

// automatic is a signal from a vendor or the server, which only moves forward.
func (a Actor) automatic() bool { return a.Kind != ActorPerson && a.Kind != ActorToken }

// Current is what the rules need to know about an attempt.
type Current struct {
	State      State
	Reason     string
	HasPR      bool
	PRLinkedBy string // auto or person, with a pull request
	// AgentRan is true when a signal already showed the agent working; unlinking a pull request goes back
	// to that stop, not to the one before it.
	AgentRan bool
}

// Observation is a signal about an attempt from any source (a GitHub webhook, a Cursor poll, the
// reconcile pass, the worker's clock), normalized. State is where the source says the attempt is; the
// other fields are what it learned and are stored when the move is made.
type Observation struct {
	State  State
	Reason string
	// Kind names the event on the timeline; empty is state_changed.
	Kind        string
	IssueNumber int
	IssueNodeID string
	VendorRef   string
	VendorRunID string
	VendorURL   string
	Branch      string
	PRNumber    int
	PRURL       string
	MergeSHA    string
	MergedBy    string
	// Summary is a short line for the timeline (a vendor's message, cut); never a secret.
	Summary string
	// DeliveryID names the webhook delivery or poll that brought it.
	DeliveryID string
}

// Result says what an observation does.
type Result int

const (
	// Ignore: nothing to record (the same state again, or an old signal arriving late).
	Ignore Result = iota
	// Note: the timeline records the signal; the state stays (a signal the rules do not follow, such as
	// an issue closed after a person cancelled the fix).
	Note
	// Move: the state changes and the timeline records it.
	Move
)

// Decide applies the rules to an automatic observation (GitHub, Cursor, the server). It never refuses:
// a signal it does not follow is ignored or only noted. A wrong reason is a programming error.
func Decide(current Current, obs Observation, actor Actor) (Result, error) {
	if !actor.automatic() {
		return Ignore, fmt.Errorf("%w: a person's change goes through Act", ErrMove)
	}
	if !obs.State.Valid() {
		return Ignore, fmt.Errorf("%w: unknown state %q", ErrMove, obs.State)
	}
	if err := CheckReason(obs.State, obs.Reason); err != nil {
		return Ignore, err
	}
	from, to := current.State, obs.State
	if from == to {
		// An ended attempt keeps its reason; another reason for the same end is only recorded (the issue
		// closed after a person cancelled the fix in Colliedesk).
		if from.Ended() && obs.Reason != current.Reason {
			return Note, nil
		}
		return Ignore, nil
	}
	switch {
	case to == Deployed:
		// Only a person says a fix is released.
		return Note, nil
	case to == PROpen && obs.PRURL == "" && !current.HasPR:
		return Ignore, fmt.Errorf("%w: pr_open needs the pull request", ErrMove)
	case from.InProgress() && to.rank() > from.rank():
		return Move, nil
	case from.InProgress() && to.rank() >= 0:
		// An old signal arriving after a newer one (working after pr_open).
		return Ignore, nil
	case from == PROpen && to == Failed:
		// Only a pull request closed without merging fails a fix that has one.
		if obs.Reason == "pr_closed" {
			return Move, nil
		}
		return Note, nil
	case from == PROpen && to == Cancelled:
		// An issue closed while a pull request is open is not a cancellation: the pull request decides.
		if obs.Reason == "issue_closed" || beforeSending(obs.Reason) {
			return Note, nil
		}
		return Move, nil
	case from.InProgress() && to == Failed:
		return Move, nil
	case from.InProgress() && to == Cancelled:
		if beforeSending(obs.Reason) && from != Sending {
			return Note, nil
		}
		return Move, nil
	case from == Failed && current.Reason == "pr_closed" && (to == PROpen || to == Merged):
		// The pull request was reopened (and perhaps merged before its reopening reached us).
		return Move, nil
	case from == Cancelled && current.Reason == "issue_closed" && (to == Sent || to == PROpen || to == Merged):
		// The issue was reopened, or the agent's pull request came after someone closed the issue by hand.
		// The store keeps it noted when the conversation has a newer fix in progress.
		return Move, nil
	}
	// Merged, released, failed and cancelled stay; a person's cancel is never undone by a signal.
	return Note, nil
}

// beforeSending reasons only end a fix whose content has not left Colliedesk yet: the person's GitHub
// authorization was abandoned or denied, or an admin turned the agent off (withdrawing the consent). Once
// sent, they are only noted.
func beforeSending(reason string) bool {
	return reason == "authorization_abandoned" || reason == "authorization_denied" || reason == "connector_disabled"
}

// Action is something a person (or an agent token, within its permissions) does to an attempt.
type Action string

const (
	ActionCancel     Action = "cancel"
	ActionLinkPR     Action = "link_pr"
	ActionUnlinkPR   Action = "unlink_pr"
	ActionDeployed   Action = "mark_deployed"
	ActionUndeployed Action = "unmark_deployed"
)

// Act returns the state an action moves the attempt to, and its reason, or ErrMove.
func Act(action Action, current Current) (State, string, error) {
	from := current.State
	switch action {
	case ActionCancel:
		if from.InProgress() {
			return Cancelled, "cancelled_by_person", nil
		}
	case ActionLinkPR:
		if from == Sent || from == Working || (from == Failed && current.Reason == "pr_closed") {
			return PROpen, "", nil
		}
	case ActionUnlinkPR:
		if from == PROpen && current.PRLinkedBy == "person" {
			if current.AgentRan {
				return Working, "", nil
			}
			return Sent, "", nil
		}
	case ActionDeployed:
		if from == Merged {
			return Deployed, "", nil
		}
	case ActionUndeployed:
		if from == Deployed {
			return Merged, "", nil
		}
	default:
		return "", "", fmt.Errorf("%w: unknown action %q", ErrMove, action)
	}
	return "", "", fmt.Errorf("%w: %s from %s", ErrMove, action, from)
}

// CanRetry reports whether a new attempt may follow this one.
func CanRetry(s State) bool { return s.Ended() }
