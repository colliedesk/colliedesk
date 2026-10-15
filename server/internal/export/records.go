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
	CreatedAt      time.Time             `json:"created_at"`
	Attachments    []Attachment          `json:"attachments"`
}

// Conversation is one line of conversations.ndjson with everything that hangs off it.
type Conversation struct {
	ID          string                     `json:"id"`
	ContactID   string                     `json:"contact_id"`
	Subject     string                     `json:"subject"`
	Status      string                     `json:"status"`
	Priority    string                     `json:"priority"`
	AssigneeID  *string                    `json:"assignee_id"`
	Origin      *domain.ConversationOrigin `json:"origin"`
	TagIDs      []string                   `json:"tag_ids"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
	Messages    []Message                  `json:"messages"`
	TimeEntries []domain.TimeEntry         `json:"time_entries"`
}

// Sink receives the records of one workspace in archive order: Workspace, then Agent*, Contact*,
// Tag*, Conversation*. Writer is the Sink that builds the archive.
type Sink interface {
	Workspace(domain.Workspace) error
	Agent(Agent) error
	Contact(Contact) error
	Tag(Tag) error
	Conversation(Conversation) error
}
