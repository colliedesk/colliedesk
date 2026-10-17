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
)

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
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	Method         string          `json:"method"`
	Route          string          `json:"route"`
	Path           string          `json:"path"`
	Body           json.RawMessage `json:"body"`
	Summary        string          `json:"summary"`
	ConversationID *string         `json:"conversation_id"`
	RequestedBy    AgentRequester  `json:"requested_by"`
	DecidedBy      *AgentDecider   `json:"decided_by"`
	DecidedAt      *time.Time      `json:"decided_at"`
	Reason         *string         `json:"reason"`
	ResultStatus   *int            `json:"result_status"`
	Result         json.RawMessage `json:"result"`
	ResultNote     *string         `json:"result_note"`
	CreatedAt      time.Time       `json:"created_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
}

// AgentRequester is the token that asked and the person who owns it. The label and tier are kept
// when the token is revoked or removed.
type AgentRequester struct {
	TokenID      *string `json:"token_id"`
	TokenLabel   string  `json:"token_label"`
	TokenScope   string  `json:"token_scope"`
	TokenRevoked bool    `json:"token_revoked"`
	AgentID      *string `json:"agent_id"`
	AgentName    *string `json:"agent_name"`
}

// AgentDecider is the person who approved, rejected or cancelled a request.
type AgentDecider struct {
	AgentID *string `json:"agent_id"`
	Name    string  `json:"name"`
}
