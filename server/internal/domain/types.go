package domain

import "time"

type Workspace struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

type Agent struct {
	ID               string  `json:"id"`
	Email            string  `json:"email"`
	DisplayName      string  `json:"display_name"`
	Role             string  `json:"role"`
	TimezoneOverride *string `json:"timezone_override"`
}

type Session struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	AgentEmail string     `json:"agent_email"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

type Invite struct {
	ID             string     `json:"id"`
	Email          string     `json:"email"`
	RoleCeiling    string     `json:"role_ceiling"`
	CreatedByEmail string     `json:"created_by_email"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	UsedAt         *time.Time `json:"used_at"`
	RevokedAt      *time.Time `json:"revoked_at"`
}

type Contact struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Organization *string   `json:"organization"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ContactDetail struct {
	Contact
	Conversations []Conversation `json:"conversations"`
}

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Conversation struct {
	ID         string              `json:"id"`
	ContactID  string              `json:"contact_id"`
	Subject    string              `json:"subject"`
	Status     string              `json:"status"`
	Priority   string              `json:"priority"`
	AssigneeID *string             `json:"assignee_id"`
	Tags       []Tag               `json:"tags"`
	Origin     *ConversationOrigin `json:"origin"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

// ConversationOrigin records where an imported conversation came from.
type ConversationOrigin struct {
	System             string     `json:"system"`
	ExternalID         string     `json:"external_id"`
	Type               *string    `json:"type"`
	Source             *string    `json:"source"`
	DueAt              *time.Time `json:"due_at"`
	FirstResponseDueAt *time.Time `json:"first_response_due_at"`
	CC                 []string   `json:"cc"`
	ForwardedTo        []string   `json:"forwarded_to"`
}

// MessageOrigin records where an imported message came from and who it was addressed to.
type MessageOrigin struct {
	System     string   `json:"system"`
	ExternalID string   `json:"external_id"`
	Kind       string   `json:"kind"`
	From       *string  `json:"from"`
	To         []string `json:"to"`
	CC         []string `json:"cc"`
	BCC        []string `json:"bcc"`
}

type TimeEntry struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	AgentID        *string   `json:"agent_id"`
	AgentName      string    `json:"agent_name"`
	Minutes        int       `json:"minutes"`
	Note           string    `json:"note"`
	Billable       bool      `json:"billable"`
	ExecutedAt     time.Time `json:"executed_at"`
}

type Message struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversation_id"`
	AuthorAgentID  *string        `json:"author_agent_id"`
	AuthorName     *string        `json:"author_name"`
	Kind           string         `json:"kind"`
	Body           string         `json:"body"`
	BodyHTML       *string        `json:"body_html"`
	Origin         *MessageOrigin `json:"origin"`
	Attachments    []Attachment   `json:"attachments"`
	CreatedAt      time.Time      `json:"created_at"`
}

type Attachment struct {
	ID          string    `json:"id"`
	MessageID   string    `json:"message_id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
}

func UTC(value time.Time) time.Time { return value.UTC() }

func UTCOptional(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
