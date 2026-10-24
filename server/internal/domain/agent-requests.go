package domain

import (
	"encoding/json"
	"errors"
	"time"
)

// Kinds and states of an agent request.
const (
	AgentRequestReplyDraft = "reply_draft"
	AgentRequestAction     = "action"

	AgentRequestPending   = "pending"
	AgentRequestApproved  = "approved"
	AgentRequestFailed    = "failed"
	AgentRequestRejected  = "rejected"
	AgentRequestCancelled = "cancelled"
	AgentRequestExpired   = "expired"
)

// AgentRequestStatuses lists every state, pending first.
var AgentRequestStatuses = []string{AgentRequestPending, AgentRequestApproved, AgentRequestFailed, AgentRequestRejected, AgentRequestCancelled, AgentRequestExpired}

const (
	// AgentRequestLifetime is how long a request waits for a person before it expires.
	AgentRequestLifetime = 7 * 24 * time.Hour
	// MaxPendingAgentRequests is how many requests one token may have waiting at once.
	MaxPendingAgentRequests = 50
	// MaxAgentRequestBody bounds the stored call and the stored answer, in bytes of JSON.
	MaxAgentRequestBody = 64 << 10
	// MaxAgentRequestsPerHour is how many requests one token may file in any rolling hour.
	MaxAgentRequestsPerHour = 100
	// AgentRequestCallRetention is how long a decided request keeps its call and answer; the audit
	// row (who, which route, the summary, the decision, the times) stays.
	AgentRequestCallRetention = 90 * 24 * time.Hour
)

// AgentRequestRateError means the token filed MaxAgentRequestsPerHour requests in the last hour.
type AgentRequestRateError struct{ RetryAfter time.Duration }

func (e *AgentRequestRateError) Error() string {
	return "this token filed 100 requests in the last hour; wait before filing more"
}

var (
	// ErrAgentRequestDecided means the request was approved, rejected, cancelled or expired already.
	ErrAgentRequestDecided = errors.New("this request was already decided")
	// ErrAgentRequestTokenRevoked means the token that asked was revoked, so its call is not run.
	ErrAgentRequestTokenRevoked = errors.New("the token that asked was revoked, so its request can only be rejected")
	// ErrTooManyAgentRequests means the token has MaxPendingAgentRequests waiting already.
	ErrTooManyAgentRequests = errors.New("this token has 50 requests waiting; wait until a person decides some, or cancel some")
	// ErrNotRequestOwner means a person tried to cancel a request of a token they do not own.
	ErrNotRequestOwner = errors.New("only the token that asked, or the person who owns it, can cancel a request; reject it instead")
)

// AgentRequest is a call an agent token made that waits for a person.
type AgentRequest struct {
	ID     string          `json:"id"`
	Kind   string          `json:"kind"`
	Status string          `json:"status"`
	Method string          `json:"method"`
	Route  string          `json:"route"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
	// EditedBody is the call a person edited before approving it (a reply draft); Body stays the agent's.
	EditedBody json.RawMessage `json:"edited_body"`
	// ApprovableBy is the lowest role whose person can run the call: agent or admin.
	ApprovableBy   string  `json:"approvable_by"`
	Summary        string  `json:"summary"`
	ConversationID *string `json:"conversation_id"`
	// SavedReply is the saved reply the request edits, deletes or restores, before and now; filled
	// only with the full detail.
	SavedReply   *AgentRequestSavedReply `json:"saved_reply"`
	RequestedBy  AgentRequester          `json:"requested_by"`
	DecidedBy    *AgentDecider           `json:"decided_by"`
	DecidedAt    *time.Time              `json:"decided_at"`
	Reason       *string                 `json:"reason"`
	ResultStatus *int                    `json:"result_status"`
	Result       json.RawMessage         `json:"result"`
	ResultNote   *string                 `json:"result_note"`
	CreatedAt    time.Time               `json:"created_at"`
	ExpiresAt    time.Time               `json:"expires_at"`
}

// AgentRequestSavedReply is the saved reply a request acts on: as the token saw it when asking, and as
// it is now (nil once it was deleted for good).
type AgentRequestSavedReply struct {
	ID      string           `json:"id"`
	Before  *SavedReplyText  `json:"before"`
	Current *SavedReplyState `json:"current"`
	// Restores is the version a restore request applies, as it was when the token asked.
	Restores *SavedReplyRestoredVersion `json:"restores"`
}

// SavedReplyRestoredVersion is the number, title and text of the version a request restores.
type SavedReplyRestoredVersion struct {
	Version int    `json:"version"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

// SavedReplyText is the title and text of a saved reply at one moment.
type SavedReplyText struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentRequester is the token that asked and the person who owns it. The label and tier are kept
// when the token is revoked or removed.
type AgentRequester struct {
	TokenID    *string `json:"token_id"`
	TokenLabel string  `json:"token_label"`
	TokenScope string  `json:"token_scope"`
	// TokenPermissions is what the token held when it asked; empty for a request older than permissions.
	TokenPermissions []string `json:"token_permissions"`
	TokenRevoked     bool     `json:"token_revoked"`
	AgentID          *string  `json:"agent_id"`
	AgentName        *string  `json:"agent_name"`
}

// AgentDecider is the person who approved, rejected or cancelled a request.
type AgentDecider struct {
	AgentID *string `json:"agent_id"`
	Name    string  `json:"name"`
}
