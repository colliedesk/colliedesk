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
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	ID         string    `json:"id"`
	ContactID  string    `json:"contact_id"`
	Subject    string    `json:"subject"`
	Status     string    `json:"status"`
	Priority   string    `json:"priority"`
	AssigneeID *string   `json:"assignee_id"`
	Tags       []Tag     `json:"tags"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Message struct {
	ID             string           `json:"id"`
	ConversationID string           `json:"conversation_id"`
	AuthorAgentID  *string          `json:"author_agent_id"`
	Kind           string           `json:"kind"`
	Body           string           `json:"body"`
	CreatedAt      time.Time        `json:"created_at"`
	Delivery       *MessageDelivery `json:"delivery"`
	Email          *IncomingEmail   `json:"email"`
}

// MessageDelivery is the email delivery state of an outgoing reply.
type MessageDelivery struct {
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError *string    `json:"last_error"`
	SentAt    *time.Time `json:"sent_at"`
}

// IncomingEmail is the envelope of a message that arrived by email.
type IncomingEmail struct {
	From           string            `json:"from"`
	Authentication string            `json:"authentication"`
	Attachments    []EmailAttachment `json:"attachments"`
}

// EmailAttachment records an attachment seen in an inbound message; its content is not stored.
type EmailAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type EmailChannel struct {
	SupportAddress   string     `json:"support_address"`
	SecretSet        bool       `json:"secret_set"`
	SecretRotatedAt  *time.Time `json:"secret_rotated_at"`
	Domain           string     `json:"domain"`
	DomainVerifiedAt *time.Time `json:"domain_verified_at"`
	DNSRecordName    string     `json:"dns_record_name"`
	DNSRecordValue   string     `json:"dns_record_value"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func UTC(value time.Time) time.Time { return value.UTC() }

func UTCOptional(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
