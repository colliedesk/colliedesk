// Package export writes a whole workspace as one open, documented ZIP archive (see
// docs/user/export.md). Records are streamed one at a time so memory does not grow with the
// size of the workspace.
package export

import (
	"encoding/json"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// Agent is a person who signs in. Credentials, sessions and pairing links are never exported.
type Agent struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	DisplayName      string    `json:"display_name"`
	Role             string    `json:"role"`
	TimezoneOverride *string   `json:"timezone_override"`
	CreatedAt        time.Time `json:"created_at"`
	// DeactivatedAt is null for an active agent. A deactivated agent stays in the archive because
	// conversations and messages still name it.
	DeactivatedAt *time.Time `json:"deactivated_at"`
}

type Contact struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Organization *string   `json:"organization"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Tag struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

// SavedReply is a reply the team shares; its body may hold the variables {{customer.name}} and {{agent.name}}.
type SavedReply struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	CreatedByAgentID *string   `json:"created_by_agent_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// DeletedAt is set while the reply is in the trash; it is deleted for good 30 days later.
	DeletedAt        *time.Time `json:"deleted_at"`
	DeletedByAgentID *string    `json:"deleted_by_agent_id"`
}

// SavedReplyVersion is one version of a saved reply: what it said after a change, who made the change,
// through which agent token, and the agent request a person approved for it.
type SavedReplyVersion struct {
	SavedReplyID        string    `json:"saved_reply_id"`
	Version             int       `json:"version"`
	Kind                string    `json:"kind"`
	RestoredFromVersion *int      `json:"restored_from_version"`
	Title               string    `json:"title"`
	Body                string    `json:"body"`
	ChangedByAgentID    *string   `json:"changed_by_agent_id"`
	TokenID             *string   `json:"token_id"`
	TokenLabel          *string   `json:"token_label"`
	AgentRequestID      *string   `json:"agent_request_id"`
	CreatedAt           time.Time `json:"created_at"`
}

// AgentRequest is a call an agent token asked a person to approve, and what became of it. The answer
// of an approved call is not exported: credentials and links are never stored, and other answers are
// the records exported elsewhere. Body and EditedBody are null once 90 days passed after the decision.
type AgentRequest struct {
	ID                 string          `json:"id"`
	Kind               string          `json:"kind"`
	Status             string          `json:"status"`
	Method             string          `json:"method"`
	Route              string          `json:"route"`
	Path               string          `json:"path"`
	Body               json.RawMessage `json:"body"`
	EditedBody         json.RawMessage `json:"edited_body"`
	Summary            string          `json:"summary"`
	ConversationID     *string         `json:"conversation_id"`
	TokenID            *string         `json:"token_id"`
	TokenLabel         string          `json:"token_label"`
	TokenScope         string          `json:"token_scope"`
	RequestedByAgentID *string         `json:"requested_by_agent_id"`
	DecidedByAgentID   *string         `json:"decided_by_agent_id"`
	DecidedAt          *time.Time      `json:"decided_at"`
	Reason             *string         `json:"reason"`
	ResultStatus       *int32          `json:"result_status"`
	ResultNote         *string         `json:"result_note"`
	CreatedAt          time.Time       `json:"created_at"`
	ExpiresAt          time.Time       `json:"expires_at"`
}

// Attachment describes one file. Path is its location in the archive; StorageKey is where the
// server reads the bytes from and never leaves the server.
type Attachment struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
	Path        string    `json:"path"`
	StorageKey  string    `json:"-"`
}

type Message struct {
	ID             string                `json:"id"`
	ConversationID string                `json:"conversation_id"`
	AuthorAgentID  *string               `json:"author_agent_id"`
	AuthorName     *string               `json:"author_name"`
	Kind           string                `json:"kind"`
	Body           string                `json:"body"`
	BodyHTML       *string               `json:"body_html"`
	Origin         *domain.MessageOrigin `json:"origin"`
	EmailMessageID *string               `json:"email_message_id"`
	EmailMeta      json.RawMessage       `json:"email_meta"`
	// AgentRequestID names the agent request a person approved to send this reply.
	AgentRequestID *string `json:"agent_request_id"`
	// CC is the list a reply was copied to; null when it copied no one.
	CC          []string     `json:"cc"`
	CreatedAt   time.Time    `json:"created_at"`
	Attachments []Attachment `json:"attachments"`
}

// ConversationHead holds the fields of a conversation line that precede its messages.
type ConversationHead struct {
	ID         string                     `json:"id"`
	ContactID  string                     `json:"contact_id"`
	Subject    string                     `json:"subject"`
	Status     string                     `json:"status"`
	Priority   string                     `json:"priority"`
	AssigneeID *string                    `json:"assignee_id"`
	Origin     *domain.ConversationOrigin `json:"origin"`
	TagIDs     []string                   `json:"tag_ids"`
	CreatedAt  time.Time                  `json:"created_at"`
	UpdatedAt  time.Time                  `json:"updated_at"`
}

// Conversation is one line of conversations.ndjson with everything that hangs off it.
type Conversation struct {
	ConversationHead
	Messages    []Message          `json:"messages"`
	TimeEntries []domain.TimeEntry `json:"time_entries"`
}

// Sink receives the records of one workspace in archive order: Workspace, then Agent*, Contact*,
// Tag*, SavedReply*, SavedReplyVersion*, AgentRequest*, then per conversation BeginConversation, Message*, EndConversation. A conversation is
// streamed message by message because one thread can be larger than any sensible page. Writer is the Sink that builds the archive.
type Sink interface {
	Workspace(domain.Workspace) error
	Agent(Agent) error
	Contact(Contact) error
	Tag(Tag) error
	SavedReply(SavedReply) error
	SavedReplyVersion(SavedReplyVersion) error
	AgentRequest(AgentRequest) error
	BeginConversation(ConversationHead) error
	Message(Message) error
	EndConversation(timeEntries []domain.TimeEntry) error
}
