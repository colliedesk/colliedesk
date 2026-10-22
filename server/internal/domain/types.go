package domain

import (
	"errors"
	"time"
)

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

// Session is a sign-in. Scope is the tier of an agent token (read, draft or send); nil for a
// person's full session.
type Session struct {
	ID         string  `json:"id"`
	AgentID    string  `json:"agent_id"`
	AgentEmail string  `json:"agent_email"`
	Label      string  `json:"label"`
	Scope      *string `json:"scope"`
	// Current marks the session that made the request; it is not stored.
	Current    bool       `json:"current"`
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
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	Organization *string `json:"organization"`
	// MergedIntoID is the contact this one was merged into; its conversations and new mail are there.
	MergedIntoID *string   `json:"merged_into_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ContactDetail struct {
	Contact
	// ConversationCount counts the contact's history, which is read a page at a time from the
	// conversation list (an unpaged list cost one query per conversation).
	ConversationCount int      `json:"conversation_count"`
	MergedInto        *Contact `json:"merged_into"`
	// Merges are the contacts merged into this one and not undone, newest first.
	Merges []ContactMerge `json:"merges"`
}

// ContactMerge records one duplicate merged into a contact and exactly which conversations moved.
type ContactMerge struct {
	ID              string     `json:"id"`
	ContactID       string     `json:"contact_id"`
	MergedContact   Contact    `json:"merged_contact"`
	ConversationIDs []string   `json:"conversation_ids"`
	MergedByAgentID *string    `json:"merged_by_agent_id"`
	MergedAt        time.Time  `json:"merged_at"`
	UndoneAt        *time.Time `json:"undone_at"`
}

// ContactMergeError refuses a merge, or a new conversation, that the merge rules do not allow; the
// text says which rule.
type ContactMergeError struct{ Reason string }

func (e *ContactMergeError) Error() string { return e.Reason }

// ContactExistsError refuses a new contact whose address is already a contact of the workspace. Use is
// the contact to work with: the one that address belongs to, or the one it was merged into.
type ContactExistsError struct{ Use Contact }

func (e *ContactExistsError) Error() string {
	return e.Use.Name + " (" + e.Use.Email + ") is already a customer with this address. Find them by name or email instead."
}

// ErrMergeUndone means the merge was undone already.
var ErrMergeUndone = errors.New("this merge was undone already")

// ErrEmailChannelMissing means a new conversation would be stored but never emailed.
var ErrEmailChannelMissing = errors.New("connect the email channel in Settings before starting a conversation by email")

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Conversation struct {
	ID        string `json:"id"`
	ContactID string `json:"contact_id"`
	// ContactName saves the reply box a second request for the customer's name.
	ContactName string  `json:"contact_name"`
	Subject     string  `json:"subject"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	AssigneeID  *string `json:"assignee_id"`
	Tags        []Tag   `json:"tags"`
	// Channel is how the conversation started: one of the Channel* values.
	Channel   string              `json:"channel"`
	Origin    *ConversationOrigin `json:"origin"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	// LatestReplyNumber is the number of the newest reply (0 before the first): a reply sent with an
	// older seen_reply_number is refused with ReplyConflictError.
	LatestReplyNumber int `json:"latest_reply_number"`
}

// How a conversation started (conversations.channel).
const (
	ChannelEmail   = "email"
	ChannelWebForm = "web_form"
	// ChannelImported marks a conversation brought over from another helpdesk; Origin.Source keeps its own channel.
	ChannelImported = "imported"
	// ChannelAgent marks a conversation a team member started. An older image that does not know the column also writes it.
	ChannelAgent         = "agent"
	ChannelProblemReport = "problem_report"
)

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

// MessageToken names the agent token a message was written with. AgentName is the display name of
// the agent the token acts as, or their email when they have none.
type MessageToken struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Scope     string `json:"scope"`
	AgentName string `json:"agent_name"`
}

type Message struct {
	ID             string        `json:"id"`
	ConversationID string        `json:"conversation_id"`
	AuthorAgentID  *string       `json:"author_agent_id"`
	AuthorName     *string       `json:"author_name"`
	AgentToken     *MessageToken `json:"agent_token"`
	// AgentRequestID names the agent request a person approved to send this message (a reply draft).
	AgentRequestID *string        `json:"agent_request_id"`
	Kind           string         `json:"kind"`
	Body           string         `json:"body"`
	BodyHTML       *string        `json:"body_html"`
	Origin         *MessageOrigin `json:"origin"`
	Attachments    []Attachment   `json:"attachments"`
	// CC lists the addresses an outgoing reply was copied to; empty for every other message.
	CC        []string         `json:"cc"`
	CreatedAt time.Time        `json:"created_at"`
	Delivery  *MessageDelivery `json:"delivery"`
	Email     *IncomingEmail   `json:"email"`
	// ReplyNumber numbers the replies of a conversation in the order they were stored, 1 first; nil for
	// a note or a customer message.
	ReplyNumber *int `json:"reply_number"`
}

// ReplyConflictError refuses a reply whose sender had seen replies up to a number older than the
// conversation's latest: Newer are the replies after it, oldest first by time.
type ReplyConflictError struct {
	Latest int
	Newer  []Message
}

func (e *ReplyConflictError) Error() string {
	return "someone replied to this conversation after you started writing"
}

type Attachment struct {
	ID          string `json:"id"`
	MessageID   string `json:"message_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	// Inline is true when the bytes are embedded in the message's HTML, which already shows the image.
	Inline    bool      `json:"inline"`
	CreatedAt time.Time `json:"created_at"`
}

// MessageDelivery is the email delivery state of an outgoing reply.
type MessageDelivery struct {
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError *string    `json:"last_error"`
	SentAt    *time.Time `json:"sent_at"`
}

// Doors an inbound message can come through.
const (
	ViaPlatform  = "platform"
	ViaWorkspace = "workspace"
	// ViaWebForm marks a message typed into the public web form: the address was never verified.
	ViaWebForm = "web_form"
	// ViaProblemReport marks a report an agent filed with "Report a problem": the sender is a signed-in agent.
	ViaProblemReport = "problem_report"
)

// IncomingEmail is the envelope of a message that arrived by email.
type IncomingEmail struct {
	From           string `json:"from"`
	Authentication string `json:"authentication"`
	// CC are the other addresses on the Cc line, without our own; mail stored before they were kept has none.
	CC []string `json:"cc,omitempty"`
	// Via is how the message arrived: ViaPlatform, ViaWorkspace or ViaWebForm. Mail stored before this field
	// existed has none.
	Via         string            `json:"via,omitempty"`
	Attachments []EmailAttachment `json:"attachments"`
	// OriginalAttachmentID is the stored, gzip-compressed raw message (an attachment of this message).
	OriginalAttachmentID *string `json:"original_attachment_id"`
}

// EmailAttachment names an attachment seen in an inbound message; stored bytes are in Message.Attachments.
type EmailAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type EmailChannel struct {
	SupportAddress string `json:"support_address"`
	// SenderName is the name customers see in the From line of every reply the channel sends.
	SenderName       string     `json:"sender_name"`
	SecretSet        bool       `json:"secret_set"`
	SecretRotatedAt  *time.Time `json:"secret_rotated_at"`
	Domain           string     `json:"domain"`
	DomainVerifiedAt *time.Time `json:"domain_verified_at"`
	DNSRecordName    string     `json:"dns_record_name"`
	DNSRecordValue   string     `json:"dns_record_value"`
	// DNSRecords are the records the email provider lists for the domain; empty when verification is by TXT
	// record or the provider has not created the domain yet.
	DNSRecords []DNSRecord `json:"dns_records"`
	// DNSRecordsExpiredAt is set when the provider domain stayed unverified too long and was released; verifying again clears it.
	DNSRecordsExpiredAt *time.Time `json:"dns_records_expired_at"`
	// ProviderDomainID is the provider's id for the domain; it never leaves the server.
	ProviderDomainID string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// DNSRecord is one record the email provider asks the workspace to publish, with its last reported status.
type DNSRecord struct {
	Record   string `json:"record"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Priority *int   `json:"priority"`
	Status   string `json:"status"`
}

// WebForm is the workspace's public website form channel. Off until an admin turns it on.
type WebForm struct {
	Enabled bool `json:"enabled"`
	// AllowedOrigins are the exact origins (scheme://host[:port]) whose pages may call the form.
	AllowedOrigins []string `json:"allowed_origins"`
	// SubmitPath is the path a website posts to; the host is the instance's own.
	SubmitPath string `json:"submit_path"`
}

func UTC(value time.Time) time.Time { return value.UTC() }

func UTCOptional(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
