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
	ID   string `json:"id"`
	Name string `json:"name"`
	// Email is null for a customer known only on a channel (identities).
	Email        *string `json:"email"`
	Organization *string `json:"organization"`
	// MergedIntoID is the contact this one was merged into; its conversations are there.
	MergedIntoID *string   `json:"merged_into_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// Identities are who the customer is on a channel (channel_id, kind, value, display_name).
	Identities []domain.ContactIdentity `json:"identities"`
}

// Channel is one channel of the workspace, removed ones included. Its secrets are never exported.
type Channel struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Name            string          `json:"name"`
	Enabled         bool            `json:"enabled"`
	Settings        json.RawMessage `json:"settings"`
	Config          any             `json:"config"`
	ExternalAccount *string         `json:"external_account"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DisabledAt      *time.Time      `json:"disabled_at"`
	DeletedAt       *time.Time      `json:"deleted_at"`
	// IsDefault marks its type's default channel.
	IsDefault bool `json:"is_default"`
}

// ChannelAILevel is the level a channel sets for an AI feature on one of its scopes (” is the
// whole channel), with intake's options there.
type ChannelAILevel struct {
	ChannelID string          `json:"channel_id"`
	Scope     string          `json:"scope"`
	Slot      string          `json:"slot"`
	Level     string          `json:"level"`
	Options   json.RawMessage `json:"options"`
	UpdatedBy *string         `json:"updated_by"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// AISourceConsent is an admin's consent that intake may send a kind of context (images,
// contact_profile, external:<name>) besides the conversation's text.
type AISourceConsent struct {
	SourceClass string    `json:"source_class"`
	ConsentedBy *string   `json:"consented_by"`
	ConsentedAt time.Time `json:"consented_at"`
}

// ChannelEvent is one change to a channel (channel_events).
type ChannelEvent struct {
	ID           string          `json:"id"`
	ChannelID    string          `json:"channel_id"`
	ChannelType  string          `json:"channel_type"`
	ChannelName  string          `json:"channel_name"`
	Kind         string          `json:"kind"`
	ActorAgentID *string         `json:"actor_agent_id"`
	ActorTokenID *string         `json:"actor_token_id"`
	Detail       json.RawMessage `json:"detail"`
	CreatedAt    time.Time       `json:"created_at"`
}

// ChannelInboundEvent is a raw event a channel received that is not a message: held, collected,
// failed, dropped, or kept without a conversation (a group's chatter). Its raw payload is here, or in
// the archive at raw_path; its fetched files are in the archive too.
type ChannelInboundEvent struct {
	ID              string          `json:"id"`
	ChannelID       string          `json:"channel_id"`
	ExternalEventID *string         `json:"external_event_id"`
	State           string          `json:"state"`
	Raw             json.RawMessage `json:"raw"`
	RawPath         *string         `json:"raw_path"`
	HoldReason      *string         `json:"hold_reason"`
	DropReason      *string         `json:"drop_reason"`
	Error           *string         `json:"error"`
	// DecisionBasis and DecisionRule are what the channel's rules decided (match, default, person, and
	// the rule's name); null for what the frame handled before them or what is still being collected.
	DecisionBasis *string              `json:"decision_basis"`
	DecisionRule  *string              `json:"decision_rule"`
	ReceivedAt    time.Time            `json:"received_at"`
	ProcessedAt   *time.Time           `json:"processed_at"`
	Files         []ChannelInboundFile `json:"files"`
	RawKey        string               `json:"-"`
	RawSize       int64                `json:"-"`
}

// ChannelInboundFile is a file fetched for an event, or why it could not be.
type ChannelInboundFile struct {
	Name        string  `json:"name"`
	ContentType string  `json:"content_type"`
	Status      string  `json:"status"`
	Size        *int64  `json:"size"`
	SHA256      *string `json:"sha256"`
	Error       *string `json:"error"`
	Path        *string `json:"path"`
	StorageKey  string  `json:"-"`
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
	SEOTitle         string          `json:"seo_title"`
	SEODescription   string          `json:"seo_description"`
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
	// AIExcludedAt is set while AI may not read the article (in any language), by AIExcludedByAgentID.
	AIExcludedAt        *time.Time `json:"ai_excluded_at"`
	AIExcludedByAgentID *string    `json:"ai_excluded_by_agent_id"`
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
	SEOTitle            string    `json:"seo_title"`
	SEODescription      string    `json:"seo_description"`
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
	// GoogleSiteVerification and BingSiteVerification are the Search Console tokens (null when none).
	GoogleSiteVerification *string `json:"google_site_verification"`
	BingSiteVerification   *string `json:"bing_site_verification"`
	// CustomDomain is the help center's custom host name (null when none), CustomDomainStatus pending or
	// active, CustomDomainVerifiedAt when its DNS check first passed. The proof token is not exported.
	CustomDomain           *string    `json:"custom_domain"`
	CustomDomainStatus     *string    `json:"custom_domain_status"`
	CustomDomainVerifiedAt *time.Time `json:"custom_domain_verified_at"`
}

// AgentRequest is a call an agent token asked a person to approve, and what became of it. The answer
// of an approved call is not exported: credentials and links are never stored, and other answers are
// the records exported elsewhere. Body, EditedBody and SavedReplyBefore are null once 90 days passed
// after the decision.
type AgentRequest struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	Method         string          `json:"method"`
	Route          string          `json:"route"`
	Path           string          `json:"path"`
	Body           json.RawMessage `json:"body"`
	EditedBody     json.RawMessage `json:"edited_body"`
	Summary        string          `json:"summary"`
	ConversationID *string         `json:"conversation_id"`
	TokenID        *string         `json:"token_id"`
	TokenLabel     string          `json:"token_label"`
	TokenScope     string          `json:"token_scope"`
	// TokenPermissions is what the token held when it asked (resource:action); empty before permissions.
	TokenPermissions   []string   `json:"token_permissions"`
	RequestedByAgentID *string    `json:"requested_by_agent_id"`
	DecidedByAgentID   *string    `json:"decided_by_agent_id"`
	DecidedAt          *time.Time `json:"decided_at"`
	Reason             *string    `json:"reason"`
	ResultStatus       *int32     `json:"result_status"`
	ResultNote         *string    `json:"result_note"`
	CreatedAt          time.Time  `json:"created_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
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

// AIProvider is a provider the workspace added for its AI features. It never holds the key: a
// restored workspace enters its keys again.
type AIProvider struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Label          string    `json:"label"`
	BaseURL        *string   `json:"base_url"`
	AuthKind       string    `json:"auth_kind"`
	KeyHint        *string   `json:"key_hint"`
	Models         []string  `json:"models"`
	CheckStatus    string    `json:"check_status"`
	CheckErrorCode *string   `json:"check_error_code"`
	CheckedAt      time.Time `json:"checked_at"`
	CreatedBy      *string   `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// AISlot is one AI feature's setting: where it runs, the model, on or off, and who consented.
type AISlot struct {
	Slot       string  `json:"slot"`
	UsesServer bool    `json:"uses_server"`
	ProviderID *string `json:"provider_id"`
	Model      *string `json:"model"`
	Effort     string  `json:"effort"`
	Enabled    bool    `json:"enabled"`
	// DefaultLevel is what the feature does by itself where no channel says otherwise: off, suggest,
	// auto. Automatic is the older switch it replaced, read from it (triage auto, drafts suggest).
	DefaultLevel string     `json:"default_level"`
	Automatic    bool       `json:"automatic"`
	AutoParts    []string   `json:"auto_parts"`
	ConsentedBy  *string    `json:"consented_by"`
	ConsentedAt  *time.Time `json:"consented_at"`
	UpdatedBy    *string    `json:"updated_by"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// MonthlyCapMicroUSD is the AI chat feature's own monthly spending limit; null is none.
	MonthlyCapMicroUSD *int64 `json:"monthly_cap_micro_usd"`
}

// AIChatSession is one end user's chat with the help center's AI and its transcript; never the
// cookie's token or its hash. Texts are null once they expired (90 days after the last turn).
type AIChatSession struct {
	ID             string       `json:"id"`
	ChannelID      string       `json:"channel_id"`
	Surface        string       `json:"surface"`
	Status         string       `json:"status"`
	ConversationID *string      `json:"conversation_id"`
	HandedOffAt    *time.Time   `json:"handed_off_at"`
	HistoryCutAt   *time.Time   `json:"history_cut_at"`
	CreatedAt      time.Time    `json:"created_at"`
	LastTurnAt     time.Time    `json:"last_turn_at"`
	EndedAt        *time.Time   `json:"ended_at"`
	Turns          []AIChatTurn `json:"turns"`
}

// AIChatTurn is one question and its answer, with the articles it cited as they were and its feedback.
type AIChatTurn struct {
	ID                  string          `json:"id"`
	Seq                 int             `json:"seq"`
	Question            *string         `json:"question"`
	Answer              *string         `json:"answer"`
	ViewingKBArticleID  *string         `json:"viewing_kb_article_id"`
	Status              string          `json:"status"`
	ErrorCode           *string         `json:"error_code"`
	Model               *string         `json:"model"`
	TTFTMS              *int            `json:"ttft_ms"`
	InputTokens         *int64          `json:"input_tokens"`
	CacheReadTokens     *int64          `json:"cache_read_tokens"`
	CacheCreationTokens *int64          `json:"cache_creation_tokens"`
	OutputTokens        *int64          `json:"output_tokens"`
	CreatedAt           time.Time       `json:"created_at"`
	FinishedAt          *time.Time      `json:"finished_at"`
	Sources             []AIChatSource  `json:"sources"`
	Feedback            *AIChatFeedback `json:"feedback"`
}

// AIChatSource is an article an answer cited.
type AIChatSource struct {
	Ordinal        int     `json:"ordinal"`
	KBArticleID    *string `json:"kb_article_id"`
	ArticleNumber  int64   `json:"article_number"`
	ArticleVersion int     `json:"article_version"`
	Title          *string `json:"title"`
	Folder         *string `json:"folder"`
}

// AIChatFeedback is how the end user rated an answer.
type AIChatFeedback struct {
	Rating    string    `json:"rating"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AISettingsEvent is one change to the AI settings.
type AISettingsEvent struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	ActorAgentID  *string         `json:"actor_agent_id"`
	ActorTokenID  *string         `json:"actor_token_id"`
	ProviderID    *string         `json:"provider_id"`
	ProviderLabel *string         `json:"provider_label"`
	Slot          *string         `json:"slot"`
	Detail        json.RawMessage `json:"detail"`
	CreatedAt     time.Time       `json:"created_at"`
}

// AIUsage is one call to an AI provider. No prompt and no answer is ever stored.
type AIUsage struct {
	ID                string          `json:"id"`
	Feature           string          `json:"feature"`
	BilledTo          string          `json:"billed_to"`
	ProviderID        *string         `json:"provider_id"`
	ProviderKind      string          `json:"provider_kind"`
	ProviderLabel     string          `json:"provider_label"`
	Model             string          `json:"model"`
	Status            string          `json:"status"`
	ErrorCode         *string         `json:"error_code"`
	StopReason        *string         `json:"stop_reason"`
	ServedModel       *string         `json:"served_model"`
	InputTokens       *int64          `json:"input_tokens"`
	OutputTokens      *int64          `json:"output_tokens"`
	CacheReadTokens   *int64          `json:"cache_read_tokens"`
	CacheWriteTokens  *int64          `json:"cache_write_tokens"`
	Hops              json.RawMessage `json:"hops"`
	ProviderRequestID *string         `json:"provider_request_id"`
	ActorAgentID      *string         `json:"actor_agent_id"`
	ActorTokenID      *string         `json:"actor_token_id"`
	StartedAt         time.Time       `json:"started_at"`
	FinishedAt        *time.Time      `json:"finished_at"`
	LatencyMS         *int            `json:"latency_ms"`
	TaskID            *string         `json:"task_id"`
	Automatic         bool            `json:"automatic"`
	CostMicroUSD      *int64          `json:"cost_micro_usd"`
	// ReservedMicroUSD is the most the call could cost, held against the allowance while it ran.
	ReservedMicroUSD *int64 `json:"reserved_micro_usd"`
}

// AIDraft is one AI reply draft and how it ended. Its text and prompt are empty 90 days after it ended.
type AIDraft struct {
	ID                 string          `json:"id"`
	ConversationID     string          `json:"conversation_id"`
	TaskID             *string         `json:"task_id"`
	RequestedByAgentID *string         `json:"requested_by_agent_id"`
	RequestedByTokenID *string         `json:"requested_by_token_id"`
	Prompt             string          `json:"prompt"`
	Status             string          `json:"status"`
	Text               *string         `json:"text"`
	MissingInfo        *string         `json:"missing_info"`
	Sources            json.RawMessage `json:"sources"`
	Warnings           json.RawMessage `json:"warnings"`
	Model              *string         `json:"model"`
	Outcome            *string         `json:"outcome"`
	Similarity         *float64        `json:"similarity"`
	SentMessageID      *string         `json:"sent_message_id"`
	Automatic          bool            `json:"automatic"`
	AnswersMessageID   *string         `json:"answers_message_id"`
	ReplyNeeded        *bool           `json:"reply_needed"`
	CreatedAt          time.Time       `json:"created_at"`
	ReadyAt            *time.Time      `json:"ready_at"`
	DecidedAt          *time.Time      `json:"decided_at"`
	DecidedByAgentID   *string         `json:"decided_by_agent_id"`
	TextClearedAt      *time.Time      `json:"text_cleared_at"`
}

// ResultsBaseline is the baseline the results page measures against (docs/user/results.md): the import
// it comes from, when that import's first full run finished, its reconcile status (null for an import
// made before results existed) and the numbers as stored.
type ResultsBaseline struct {
	Source       string          `json:"source"`
	AnchorAt     time.Time       `json:"anchor_at"`
	ImportStatus *string         `json:"import_status"`
	ComputedAt   *time.Time      `json:"computed_at"`
	Numbers      json.RawMessage `json:"numbers"`
}

// AutofixInstallation is a GitHub App installation the workspace connected for Autofix.
type AutofixInstallation struct {
	ID                  string    `json:"id"`
	InstallationID      int64     `json:"installation_id"`
	AccountLogin        string    `json:"account_login"`
	AccountType         string    `json:"account_type"`
	RepositorySelection string    `json:"repository_selection"`
	Status              string    `json:"status"`
	ConnectedByAgentID  *string   `json:"connected_by_agent_id"`
	ConnectedByName     string    `json:"connected_by_name"`
	ConnectedAt         time.Time `json:"connected_at"`
}

// AutofixRepo is a repository connected for fixes.
type AutofixRepo struct {
	ID                  string     `json:"id"`
	InstallationID      *int64     `json:"installation_id"`
	GitHubRepoID        int64      `json:"github_repo_id"`
	FullName            string     `json:"full_name"`
	Visibility          string     `json:"visibility"`
	VisibilityCheckedAt time.Time  `json:"visibility_checked_at"`
	DefaultBranch       string     `json:"default_branch"`
	IncludeNotes        bool       `json:"include_notes"`
	DefaultConnector    *string    `json:"default_connector"`
	PausedReason        *string    `json:"paused_reason"`
	PausedAt            *time.Time `json:"paused_at"`
	ConnectedByAgentID  *string    `json:"connected_by_agent_id"`
	ConnectedByName     string     `json:"connected_by_name"`
	ConnectedAt         time.Time  `json:"connected_at"`
}

// AutofixConnector is a coding agent's settings and the consent given for it; never its key, only whether
// one is set, by whom and when.
type AutofixConnector struct {
	Kind                 string     `json:"kind"`
	Enabled              bool       `json:"enabled"`
	Mode                 *string    `json:"mode"`
	ConsentAgentID       *string    `json:"consent_agent_id"`
	ConsentName          *string    `json:"consent_name"`
	ConsentAt            *time.Time `json:"consent_at"`
	ConsentVersion       *string    `json:"consent_version"`
	CredentialSet        bool       `json:"credential_set"`
	CredentialSetByAgent *string    `json:"credential_set_by_agent_id"`
	CredentialSetByName  *string    `json:"credential_set_by_name"`
	CredentialSetAt      *time.Time `json:"credential_set_at"`
	LastCheckAt          *time.Time `json:"last_check_at"`
	LastCheckResult      *string    `json:"last_check_result"`
	LastCheckMessage     *string    `json:"last_check_message"`
}

// Fix is one fix attempt: exactly what was sent, where, by whom, and where it went.
type Fix struct {
	ID                string     `json:"id"`
	ConversationID    string     `json:"conversation_id"`
	AttemptNo         int        `json:"attempt_no"`
	PreviousAttemptID *string    `json:"previous_attempt_id"`
	RepoID            *string    `json:"repo_id"`
	RepoFullName      string     `json:"repo_full_name"`
	Connector         string     `json:"connector"`
	State             string     `json:"state"`
	OutcomeReason     *string    `json:"outcome_reason"`
	StateChangedAt    time.Time  `json:"state_changed_at"`
	SentTitle         string     `json:"sent_title"`
	SentBody          string     `json:"sent_body"`
	SentMessageIDs    []string   `json:"sent_message_ids"`
	SentNotes         bool       `json:"sent_notes"`
	CreatedByKind     string     `json:"created_by_kind"`
	CreatedByAgentID  *string    `json:"created_by_agent_id"`
	CreatedByName     string     `json:"created_by_name"`
	CreatedByTokenID  *string    `json:"created_by_token_id"`
	ApprovedRequestID *string    `json:"approved_request_id"`
	GitHubLogin       *string    `json:"github_login"`
	CopilotAssignment *string    `json:"copilot_assignment"`
	IssueNumber       *int       `json:"issue_number"`
	VendorRef         *string    `json:"vendor_ref"`
	VendorURL         *string    `json:"vendor_url"`
	Branch            *string    `json:"branch"`
	PRNumber          *int       `json:"pr_number"`
	PRURL             *string    `json:"pr_url"`
	PRLinkedBy        *string    `json:"pr_linked_by"`
	MergeSHA          *string    `json:"merge_sha"`
	MergedAt          *time.Time `json:"merged_at"`
	MergedByLogin     *string    `json:"merged_by_login"`
	DeployedAt        *time.Time `json:"deployed_at"`
	DeployedByAgentID *string    `json:"deployed_by_agent_id"`
	DeployedByName    *string    `json:"deployed_by_name"`
	CreatedAt         time.Time  `json:"created_at"`
}

// FixEvent is one line of a fix's timeline.
type FixEvent struct {
	ID        int64           `json:"id"`
	FixID     string          `json:"fix_id"`
	Kind      string          `json:"kind"`
	FromState *string         `json:"from_state"`
	ToState   *string         `json:"to_state"`
	ActorKind string          `json:"actor_kind"`
	ActorID   *string         `json:"actor_id"`
	ActorName *string         `json:"actor_name"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// AITriage is one AI triage suggestion and what a person did with it.
type AITriage struct {
	ID                 string          `json:"id"`
	ConversationID     string          `json:"conversation_id"`
	TaskID             *string         `json:"task_id"`
	Automatic          bool            `json:"automatic"`
	RequestedByAgentID *string         `json:"requested_by_agent_id"`
	RequestedByTokenID *string         `json:"requested_by_token_id"`
	Status             string          `json:"status"`
	TagIDs             []string        `json:"tag_ids"`
	Priority           *string         `json:"priority"`
	AssigneeAgentID    *string         `json:"assignee_agent_id"`
	Reason             *string         `json:"reason"`
	Sources            json.RawMessage `json:"sources"`
	Base               json.RawMessage `json:"base"`
	Stale              json.RawMessage `json:"stale"`
	Model              *string         `json:"model"`
	Outcome            *string         `json:"outcome"`
	Applied            json.RawMessage `json:"applied"`
	AutoAppliedAt      *time.Time      `json:"auto_applied_at"`
	AutoApplied        json.RawMessage `json:"auto_applied"`
	Skipped            json.RawMessage `json:"skipped"`
	UndoneParts        json.RawMessage `json:"undone_parts"`
	Kept               json.RawMessage `json:"kept"`
	UndoneAt           *time.Time      `json:"undone_at"`
	UndoneByAgentID    *string         `json:"undone_by_agent_id"`
	CreatedAt          time.Time       `json:"created_at"`
	ReadyAt            *time.Time      `json:"ready_at"`
	DecidedAt          *time.Time      `json:"decided_at"`
	DecidedByAgentID   *string         `json:"decided_by_agent_id"`
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
	// AIDraftID names the AI draft the reply was written from.
	AIDraftID *string `json:"ai_draft_id"`
	// CC is the list a reply was copied to; null when it copied no one.
	CC []string `json:"cc"`
	// ChannelID is the channel that carried it, ExternalID its id there, ChannelMeta what the channel knows of it.
	ChannelID   *string         `json:"channel_id"`
	ExternalID  *string         `json:"external_id"`
	ChannelMeta json.RawMessage `json:"channel_meta"`
	// DecisionBasis and DecisionRule say why a message from a channel is in this conversation: what the
	// channel's rules decided (match, default, person; new_thread, thread_key…). Null for the others.
	DecisionBasis *string      `json:"decision_basis"`
	DecisionRule  *string      `json:"decision_rule"`
	CreatedAt     time.Time    `json:"created_at"`
	Attachments   []Attachment `json:"attachments"`
}

// ConversationHead holds the fields of a conversation line that precede its messages.
type ConversationHead struct {
	ID         string  `json:"id"`
	ContactID  string  `json:"contact_id"`
	Subject    string  `json:"subject"`
	Status     string  `json:"status"`
	Priority   string  `json:"priority"`
	AssigneeID *string `json:"assignee_id"`
	Channel    string  `json:"channel"`
	ChannelID  *string `json:"channel_id"`
	// ChannelScope is the scope of its channel's type the first message came in (a chat's group).
	ChannelScope *string `json:"channel_scope"`
	// ReplyChannelID is the channel its replies were pinned to when its own channel cannot send.
	ReplyChannelID *string                    `json:"reply_channel_id"`
	Origin         *domain.ConversationOrigin `json:"origin"`
	TagIDs         []string                   `json:"tag_ids"`
	CreatedAt      time.Time                  `json:"created_at"`
	UpdatedAt      time.Time                  `json:"updated_at"`
}

// StatusChange is one change of a conversation's status (conversation_status_changes). Source is
// colliedesk or import; ActorAgentID is the person who changed it, when known.
type StatusChange struct {
	ID           string    `json:"id"`
	FromStatus   string    `json:"from_status"`
	ToStatus     string    `json:"to_status"`
	ChangedAt    time.Time `json:"changed_at"`
	ActorAgentID *string   `json:"actor_agent_id"`
	Source       string    `json:"source"`
}

// Conversation is one line of conversations.ndjson with everything that hangs off it.
type Conversation struct {
	ConversationHead
	Messages      []Message          `json:"messages"`
	TimeEntries   []domain.TimeEntry `json:"time_entries"`
	StatusChanges []StatusChange     `json:"status_changes"`
}

// Sink receives the records of one workspace in archive order: Workspace, then Agent*, Contact*,
// ContactMerge*, Tag*, SavedReply*, SavedReplyVersion*, SavedView*, AgentRequest*, ImportJob*, ImportPerson*, AIProvider*, AISlot*, AISettingsEvent*, AISourceConsent*, AIUsage*, AIDraft*, AITriage*, AIChatSession*, ResultsBaseline*, Channel*, ChannelEvent*, ChannelAILevel*, ChannelInboundEvent*, then per conversation BeginConversation, Message*, EndConversation. A conversation is
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
	AIProvider(AIProvider) error
	AISlot(AISlot) error
	AISettingsEvent(AISettingsEvent) error
	AISourceConsent(AISourceConsent) error
	AIUsage(AIUsage) error
	AIDraft(AIDraft) error
	AITriage(AITriage) error
	AIChatSession(AIChatSession) error
	ResultsBaseline(ResultsBaseline) error
	AutofixInstallation(AutofixInstallation) error
	AutofixRepo(AutofixRepo) error
	AutofixConnector(AutofixConnector) error
	Fix(Fix) error
	FixEvent(FixEvent) error
	Channel(Channel) error
	ChannelEvent(ChannelEvent) error
	ChannelAILevel(ChannelAILevel) error
	ChannelInboundEvent(ChannelInboundEvent) error
	BeginConversation(ConversationHead) error
	Message(Message) error
	EndConversation(timeEntries []domain.TimeEntry, statusChanges []StatusChange) error
}
