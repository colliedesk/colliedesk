package autofix

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StaleAfter is how long a sent fix may show no sign of its agent before the screen flags it (it keeps
// its state: nothing changed, the person checks the workflow or the vendor).
const StaleAfter = 30 * time.Minute

// Errors the store and the API share.
var (
	ErrFixNotFound   = errors.New("no fix of this workspace has that id")
	ErrFixInProgress = errors.New("this conversation already has a fix in progress; cancel it or wait for it to end")
	ErrFixChanged    = errors.New("this fix changed since you read it")
	ErrBadPRURL      = errors.New("the pull request address is not a pull request of the fix's repository")
)

// Fix is one fix attempt as the API shows it. SentBody and Events are only on one fix (FixDetail).
type Fix struct {
	ID                string     `json:"id"`
	ConversationID    string     `json:"conversation_id"`
	AttemptNo         int        `json:"attempt_no"`
	PreviousAttemptID *string    `json:"previous_attempt_id"`
	RepoID            *string    `json:"repo_id"`
	RepoFullName      string     `json:"repo_full_name"`
	Connector         Connector  `json:"connector"`
	State             State      `json:"state"`
	OutcomeReason     *string    `json:"outcome_reason"`
	StateChangedAt    time.Time  `json:"state_changed_at"`
	RowVersion        int        `json:"row_version"`
	Stale             bool       `json:"stale"`
	Group             string     `json:"group"`
	SentTitle         string     `json:"sent_title"`
	SentMessageIDs    []string   `json:"sent_message_ids"`
	SentNotes         bool       `json:"sent_notes"`
	CreatedByKind     string     `json:"created_by_kind"`
	CreatedByAgentID  *string    `json:"created_by_agent_id"`
	CreatedByName     string     `json:"created_by_name"`
	ApprovedRequestID *string    `json:"approved_request_id"`
	GitHubLogin       *string    `json:"github_login"`
	IssueNumber       *int       `json:"issue_number"`
	IssueURL          *string    `json:"issue_url"`
	VendorURL         *string    `json:"vendor_url"`
	Branch            *string    `json:"branch"`
	PRNumber          *int       `json:"pr_number"`
	PRURL             *string    `json:"pr_url"`
	PRLinkedBy        *string    `json:"pr_linked_by"`
	MergeSHA          *string    `json:"merge_sha"`
	MergedAt          *time.Time `json:"merged_at"`
	MergedByLogin     *string    `json:"merged_by_login"`
	DeployedAt        *time.Time `json:"deployed_at"`
	DeployedByName    *string    `json:"deployed_by_name"`
	CreatedAt         time.Time  `json:"created_at"`
}

// FixDetail is one fix with what was sent and its timeline.
type FixDetail struct {
	Fix
	SentBody string  `json:"sent_body"`
	Events   []Event `json:"events"`
}

// Event is one line of a fix's timeline.
type Event struct {
	ID        int64          `json:"id"`
	Kind      string         `json:"kind"`
	FromState *State         `json:"from_state"`
	ToState   *State         `json:"to_state"`
	ActorKind ActorKind      `json:"actor_kind"`
	ActorName *string        `json:"actor_name"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}

// Groups of the Fixes screen. needs_person: the conversation's latest fix failed, was merged and waits
// for its release, or was sent and stayed silent; running: in progress; done: everything else
// (released, cancelled, or followed by a newer attempt).
const (
	GroupNeedsPerson = "needs_person"
	GroupRunning     = "running"
	GroupDone        = "done"
)

// Groups lists them in the screen's order.
var Groups = []string{GroupNeedsPerson, GroupRunning, GroupDone}

// Status is the count the navigation rail needs: whether Autofix is set up (a repository connected)
// and how many fixes wait for a person or run.
type Status struct {
	ConnectedRepos int `json:"connected_repos"`
	NeedsPerson    int `json:"needs_person"`
	Running        int `json:"running"`
}

// IssueURL is the address of an issue of a GitHub repository.
func IssueURL(repoFullName string, number int) string {
	return "https://github.com/" + repoFullName + "/issues/" + strconv.Itoa(number)
}

var pullPath = regexp.MustCompile(`^/([A-Za-z0-9_.-]{1,100})/([A-Za-z0-9_.-]{1,100})/pull/([1-9][0-9]{0,9})/?$`)

// ParsePRURL checks that address is a pull request of the repository (GitHub names are not case
// sensitive) and returns its number and its address written the canonical way.
func ParsePRURL(repoFullName, address string) (int, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return 0, "", ErrBadPRURL
	}
	match := pullPath.FindStringSubmatch(parsed.Path)
	if match == nil || !strings.EqualFold(match[1]+"/"+match[2], repoFullName) {
		return 0, "", ErrBadPRURL
	}
	number, err := strconv.Atoi(match[3])
	if err != nil || number > 1<<31-1 {
		return 0, "", ErrBadPRURL
	}
	return number, fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, number), nil
}
