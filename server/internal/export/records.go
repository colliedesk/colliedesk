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
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	Organization *string `json:"organization"`
	// MergedIntoID is the contact this one was merged into; its conversations are there.
	MergedIntoID *string   `json:"merged_into_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ContactMerge is one duplicate merged into a contact, undone or not, with the conversations it moved.
type ContactMerge struct {
	ID              string     `json:"id"`
	ContactID       string     `json:"contact_id"`
	MergedContactID string     `json:"merged_contact_id"`
	ConversationIDs []string   `json:"conversation_ids"`
	MergedByAgentID *string    `json:"merged_by_agent_id"`
	TokenID         *string    `json:"token_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UndoneAt        *time.Time `json:"undone_at"`
	UndoneByAgentID *string    `json:"undone_by_agent_id"`
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

// SavedView is a named set of inbox filters, personal (owner_agent_id set) or shared, also in the trash.
type SavedView struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Scope            string          `json:"scope"`
	OwnerAgentID     *string         `json:"owner_agent_id"`
	Filters          json.RawMessage `json:"filters"`
	CreatedByAgentID *string         `json:"created_by_agent_id"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        *time.Time      `json:"deleted_at"`
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

// KBCollection is a knowledge base collection, also in the trash. SourceMeta is what an import read
// that has no column of its own.
type KBCollection struct {
	ID               string          `json:"id"`
	ParentID         *string         `json:"parent_id"`
	Name             string          `json:"name"`
	Slug             string          `json:"slug"`
	Description      string          `json:"description"`
	Position         int             `json:"position"`
	SourceMeta       json.RawMessage `json:"source_meta"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        *time.Time      `json:"deleted_at"`
	DeletedByAgentID *string         `json:"deleted_by_agent_id"`
}

// KBArticle is a knowledge base article in Markdown, also in the trash. SourceHTML is the original HTML
// an import read; ImageIDs are the images its versions link (files under kb-images/).
type KBArticle struct {
	ID               string          `json:"id"`
	CollectionID     string          `json:"collection_id"`
	Position         int             `json:"position"`
	Number           int64           `json:"number"`
	Title            string          `json:"title"`
	Slug             string          `json:"slug"`
	BodyMD           string          `json:"body_md"`
	Status           string          `json:"status"`
	PublishedAt      *time.Time      `json:"published_at"`
	AuthorAgentID    *string         `json:"author_agent_id"`
	SourceMeta       json.RawMessage `json:"source_meta"`
	SourceHTML       *string         `json:"source_html"`
	ImageIDs         []string        `json:"image_ids"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        *time.Time      `json:"deleted_at"`
	DeletedByAgentID *string         `json:"deleted_by_agent_id"`
}

// KBArticleVersion is one version of an article: what it said after a change and who made it.
type KBArticleVersion struct {
	ArticleID           string    `json:"article_id"`
	Version             int       `json:"version"`
	Kind                string    `json:"kind"`
	RestoredFromVersion *int      `json:"restored_from_version"`
	Title               string    `json:"title"`
	Slug                string    `json:"slug"`
	BodyMD              string    `json:"body_md"`
	Status              string    `json:"status"`
	CollectionID        *string   `json:"collection_id"`
	ChangedByAgentID    *string   `json:"changed_by_agent_id"`
	TokenID             *string   `json:"token_id"`
	TokenLabel          *string   `json:"token_label"`
	AgentRequestID      *string   `json:"agent_request_id"`
	CreatedAt           time.Time `json:"created_at"`
}

// KBArticleTranslation is a translation of an article an import brought in.
type KBArticleTranslation struct {
	ArticleID  string          `json:"article_id"`
	Locale     string          `json:"locale"`
	Title      string          `json:"title"`
	BodyMD     string          `json:"body_md"`
	SourceMeta json.RawMessage `json:"source_meta"`
	SourceHTML *string         `json:"source_html"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// KBImage is an image of the knowledge base library; its bytes are the file at Path. Name and Alt are
// what the library shows; Width and Height the size shown (null until the server read them); Kept
// keeps it from the purge; DeletedAt marks it in the library's trash. StorageKey stays out of the
// archive, and so do thumbnails, which the server makes again.
type KBImage struct {
	ID               string     `json:"id"`
	ContentType      string     `json:"content_type"`
	Size             int64      `json:"size"`
	SHA256           string     `json:"sha256"`
	Path             string     `json:"path"`
	Name             string     `json:"name"`
	Alt              string     `json:"alt"`
	Width            *int       `json:"width"`
	Height           *int       `json:"height"`
	Kept             bool       `json:"kept"`
	CreatedByAgentID *string    `json:"created_by_agent_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	UpdatedByAgentID *string    `json:"updated_by_agent_id"`
	DeletedAt        *time.Time `json:"deleted_at"`
	DeletedByAgentID *string    `json:"deleted_by_agent_id"`
	StorageKey       string     `json:"-"`
}

// KBVideo is a video of the knowledge base media library; its bytes are the file at Path and its
// poster, if it has one, the file at PosterPath. DurationMS is null when the file did not say.
type KBVideo struct {
	ID                string     `json:"id"`
	ContentType       string     `json:"content_type"`
	Size              int64      `json:"size"`
	SHA256            string     `json:"sha256"`
	Path              string     `json:"path"`
	PosterPath        *string    `json:"poster_path"`
	Name              string     `json:"name"`
	Alt               string     `json:"alt"`
	Width             *int       `json:"width"`
	Height            *int       `json:"height"`
	DurationMS        *int64     `json:"duration_ms"`
	Codecs            []string   `json:"codecs"`
	Kept              bool       `json:"kept"`
	CreatedByAgentID  *string    `json:"created_by_agent_id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	UpdatedByAgentID  *string    `json:"updated_by_agent_id"`
	DeletedAt         *time.Time `json:"deleted_at"`
	DeletedByAgentID  *string    `json:"deleted_by_agent_id"`
	StorageKey        string     `json:"-"`
	PosterKey         string     `json:"-"`
	PosterSize        int64      `json:"-"`
	PosterContentType string     `json:"-"`
}

// KBMediaReplacement is one replacement of a library item in the articles that showed it: the articles
// that got a version naming ToMediaID instead of FromMediaID (and the number of that version), the
// translations rewritten with their text before and after, and when it was undone, if it was.
type KBMediaReplacement struct {
	ID                string          `json:"id"`
	FromMediaID       string          `json:"from_media_id"`
	ToMediaID         string          `json:"to_media_id"`
	ArticleIDs        []string        `json:"article_ids"`
	ArticleVersions   []int           `json:"article_versions"`
	Translations      json.RawMessage `json:"translations"`
	SkippedArticleIDs []string        `json:"skipped_article_ids"`
	ReplacedByAgentID *string         `json:"replaced_by_agent_id"`
	CreatedAt         time.Time       `json:"created_at"`
	UndoneAt          *time.Time      `json:"undone_at"`
	UndoneByAgentID   *string         `json:"undone_by_agent_id"`
}

// HelpCenter is the public help center's settings, its theme and the knowledge base counters. A null
// theme field is the default look; the logo and favicon are kb-images.ndjson ids.
type HelpCenter struct {
	Enabled           bool      `json:"enabled"`
	Title             string    `json:"title"`
	Locale            string    `json:"locale"`
	ContentVersion    int64     `json:"content_version"`
	LastArticleNumber int64     `json:"last_article_number"`
	UpdatedAt         time.Time `json:"updated_at"`
	LogoMediaID       *string   `json:"logo_media_id"`
	FaviconMediaID    *string   `json:"favicon_media_id"`
	LogoAlt           string    `json:"logo_alt"`
	LinkbackURL       *string   `json:"linkback_url"`
	ContactURL        *string   `json:"contact_url"`
	ContactPhone      *string   `json:"contact_phone"`
	ColorPrimary      *string   `json:"color_primary"`
	ColorHeader       *string   `json:"color_header"`
	ColorHeaderText   *string   `json:"color_header_text"`
	ColorBackground   *string   `json:"color_background"`
	ColorText         *string   `json:"color_text"`
	ColorLink         *string   `json:"color_link"`
	ColorFooter       *string   `json:"color_footer"`
	BodyFont          *string   `json:"body_font"`
	HeadingFont       *string   `json:"heading_font"`
	CustomCSS         string    `json:"custom_css"`
}

// AgentRequest is a call an agent token asked a person to approve, and what became of it. The answer
// of an approved call is not exported: credentials and links are never stored, and other answers are
// the records exported elsewhere. Body, EditedBody and SavedReplyBefore are null once 90 days passed
// after the decision.
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
	// SavedReplyID and SavedReplyBefore name the saved reply a request acts on and keep it as the token
	// saw it (with the version a restore applies).
	SavedReplyID     *string         `json:"saved_reply_id"`
	SavedReplyBefore json.RawMessage `json:"saved_reply_before"`
}

// ImportJob is an import from another helpdesk run inside the app, or its undo. It never holds the
// API key of the other helpdesk; the preview, progress and reports are as the job saved them.
type ImportJob struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Source        string          `json:"source"`
	Domain        string          `json:"domain"`
	AccountSource *string         `json:"account_source"`
	UndoOf        *string         `json:"undo_of"`
	State         string          `json:"state"`
	Phase         string          `json:"phase"`
	Options       json.RawMessage `json:"options"`
	Preview       json.RawMessage `json:"preview"`
	InterimReport json.RawMessage `json:"interim_report"`
	Report        json.RawMessage `json:"report"`
	Status        *string         `json:"status"`
	ErrorCode     *string         `json:"error_code"`
	ErrorMessage  *string         `json:"error_message"`
	CreatedBy     *string         `json:"created_by"`
	CreatedAt     time.Time       `json:"created_at"`
	StartedAt     *time.Time      `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

// ImportPerson is an agent of an imported Freshdesk account and the agent of the workspace it is
// linked to (the author of their imported replies).
type ImportPerson struct {
	Source         string    `json:"source"`
	ExternalID     string    `json:"external_id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	Active         bool      `json:"active"`
	LinkedAgentID  *string   `json:"linked_agent_id"`
	LinkedByEmail  bool      `json:"linked_by_email"`
	ChosenByPerson bool      `json:"chosen_by_person"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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
	Channel    string                     `json:"channel"`
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
// ContactMerge*, Tag*, SavedReply*, SavedReplyVersion*, SavedView*, AgentRequest*, ImportJob*, ImportPerson*, then per conversation BeginConversation, Message*, EndConversation. A conversation is
// streamed message by message because one thread can be larger than any sensible page. Writer is the Sink that builds the archive.
type Sink interface {
	Workspace(domain.Workspace) error
	Agent(Agent) error
	Contact(Contact) error
	ContactMerge(ContactMerge) error
	Tag(Tag) error
	SavedReply(SavedReply) error
	SavedReplyVersion(SavedReplyVersion) error
	SavedView(SavedView) error
	KBCollection(KBCollection) error
	KBArticle(KBArticle) error
	KBArticleVersion(KBArticleVersion) error
	KBArticleTranslation(KBArticleTranslation) error
	KBImage(KBImage) error
	KBVideo(KBVideo) error
	KBMediaReplacement(KBMediaReplacement) error
	HelpCenter(HelpCenter) error
	AgentRequest(AgentRequest) error
	ImportJob(ImportJob) error
	ImportPerson(ImportPerson) error
	BeginConversation(ConversationHead) error
	Message(Message) error
	EndConversation(timeEntries []domain.TimeEntry) error
}
