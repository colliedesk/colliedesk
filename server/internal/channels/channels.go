// Package channels is the frame every channel type plugs into (docs/internals/channels.md). A channel
// is one way customers reach a workspace; its type is an Adapter of exactly three parts: its
// configuration, the parser that turns what arrives into Inbound messages, and the sender that hands
// a reply to the outside. Everything else is shared and knows no type: the ingest pipeline (the only
// writer of conversations, contacts and messages from a channel), the outbox, the journal of raw
// events, and the API, MCP and web pages.
//
// This package holds only the types and the registry, so the store, the pipeline and every adapter
// can import it. Adding a type is one adapter package and one line in channels/registry.
package channels

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// Type names a channel type, as channel_types.type and conversations.channel hold it.
type Type string

// Channel is one channel row as the frame and the adapters see it.
type Channel struct {
	ID, WorkspaceID string
	Type            Type
	Name            string
	Enabled         bool
	// Settings are the channel's effective rules: its type's defaults with what the channel stores over them.
	Settings        Settings
	ExternalAccount string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DisabledAt      *time.Time
	DeletedAt       *time.Time
}

// Live is whether the channel was not removed.
func (c Channel) Live() bool { return c.DeletedAt == nil }

// Join policies: when a message whose thread matches a conversation joins it.
const (
	// JoinAlwaysReopen joins the matching conversation and reopens it when resolved or closed.
	JoinAlwaysReopen = "always_reopen"
	// JoinWithinDays joins a resolved or closed conversation only within Days of its last change;
	// later a new conversation starts.
	JoinWithinDays = "within_days"
	// JoinNever opens a new conversation for every message.
	JoinNever = "never"
)

// JoinPolicy says when a message joins the conversation its thread matches.
type JoinPolicy struct {
	Kind string `json:"kind"`
	Days int    `json:"days,omitempty"`
}

// Rate limit scopes for new conversations.
const (
	ScopeWorkspace = "workspace"
	ScopeChannel   = "channel"
)

// RateLimits bound what one channel stores per window. Zero means no limit.
type RateLimits struct {
	WindowSeconds int `json:"window_seconds"`
	// PerSender caps the messages one sender (Inbound.Sender.Identity) stores on the channel per window.
	PerSender int `json:"per_sender"`
	// NewConversations caps the conversations opened per window: by every channel of the workspace
	// (ScopeWorkspace) or by this channel (ScopeChannel).
	NewConversations int    `json:"new_conversations"`
	Scope            string `json:"scope"`
}

// Window is the rate limit window.
func (r RateLimits) Window() time.Duration { return time.Duration(r.WindowSeconds) * time.Second }

// Command actions.
const (
	// ActionOpen opens a conversation with the command's tags and priority (the default).
	ActionOpen = "open"
	// ActionCancel throws away what is being collected.
	ActionCancel = "cancel"
	// ActionReopen adds the message to the conversation the customer names (Intent.Ref).
	ActionReopen = "reopen"
)

// Command is a command customers type at the start of a message, as a workspace sets it up: its
// phrases come only from the channel's settings, never from a list built into the server.
type Command struct {
	Action   string   `json:"action,omitempty"`
	Phrases  []string `json:"phrases,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Priority string   `json:"priority,omitempty"`
}

// Notices are the messages a channel sends the customer by itself; an empty text is off.
type Notices struct {
	// Received answers the first message of a new conversation.
	Received string `json:"received,omitempty"`
}

// Settings are a channel's rules for what arrives and what leaves. They never hold a secret.
type Settings struct {
	JoinPolicy        JoinPolicy         `json:"join_policy"`
	DefaultTags       []string           `json:"default_tags,omitempty"`
	DefaultPriority   string             `json:"default_priority,omitempty"`
	DefaultAssigneeID string             `json:"default_assignee_id,omitempty"`
	RateLimits        RateLimits         `json:"rate_limits"`
	Commands          map[string]Command `json:"commands,omitempty"`
	Notices           Notices            `json:"notices,omitempty"`
	// Review holds a collected message that named no command for a person to decide (hold_reason review).
	Review bool `json:"review,omitempty"`
	// Scopes change these rules for one kind of conversation of the type (a chat's one-to-one messages
	// and its groups); the keys are the type's Definition.Scopes.
	Scopes map[string]Scope `json:"scopes,omitempty"`
}

// Scope modes.
const (
	// ModeAll makes every message of the scope a customer message (the default).
	ModeAll = "all"
	// ModeCommands opens a conversation only for a message that names a command; others join an
	// open conversation or what is being collected, or are archived.
	ModeCommands = "commands"
)

// Gather is a scope's collecting window, in seconds.
type Gather struct {
	QuietSeconds int `json:"quiet_seconds"`
	MaxSeconds   int `json:"max_seconds"`
}

// Scope is the rules of one scope; a field left out keeps the channel's.
type Scope struct {
	Mode       string      `json:"mode,omitempty"`
	JoinPolicy *JoinPolicy `json:"join_policy,omitempty"`
	Gather     *Gather     `json:"gather,omitempty"`
	Review     *bool       `json:"review,omitempty"`
}

// ForScope returns the rules of a scope: the channel's with what the scope sets over them.
func (s Settings) ForScope(scope string) (Settings, Scope) {
	rules, ok := s.Scopes[scope]
	if !ok || scope == "" {
		return s, Scope{Mode: ModeAll}
	}
	if rules.Mode == "" {
		rules.Mode = ModeAll
	}
	merged := s
	if rules.JoinPolicy != nil {
		merged.JoinPolicy = *rules.JoinPolicy
	}
	if rules.Review != nil {
		merged.Review = *rules.Review
	}
	return merged, rules
}

// Merge returns s with every field override sets replacing it; override is JSON a channel stores.
func (s Settings) Merge(override json.RawMessage) (Settings, error) {
	if len(override) == 0 {
		return s, nil
	}
	merged := s
	if err := json.Unmarshal(override, &merged); err != nil {
		return Settings{}, err
	}
	return merged, nil
}

// When a channel is off, a type either keeps what arrives for a person to open later (the default)
// or refuses it the way it refuses an unknown address.
const (
	OffHold   = "hold"
	OffRefuse = "refuse"
)

// Definition is what a type declares about itself. It is data: the frame reads it, never the type's name.
type Definition struct {
	Type Type
	// Identified says the type's messages carry the channel's id of them, so the same message
	// delivered twice is stored once. A form post has none: two posts are two messages.
	Identified bool
	// Singleton allows at most one live channel of the type per workspace (channel_types.singleton).
	Singleton bool
	// Name is the name a new channel of the type gets when none is given.
	Name string
	// Capabilities are fixed codes the web app turns into words: what the channel can carry.
	Capabilities []string
	// FileLimits bound the files the frame fetches for the type's events (MediaSource).
	FileLimits FileLimits
	// Scopes are the keys of Settings.Scopes the type gives its messages (Inbound.Scope).
	Scopes []string
	// RepliesVia names the type that carries this type's replies when it sends none itself.
	RepliesVia Type
	// CarriesOtherReplies makes the type carry the replies of conversations that came through no
	// channel that sends (one a team member started, an import, a problem report).
	CarriesOtherReplies bool
	// ReplyTo is the identity kind a reply is addressed to (IdentityEmail, or a kind of the type's
	// contact identities). A customer without one gets no delivery on the channel.
	ReplyTo string
	// WhenOff is OffHold or OffRefuse.
	WhenOff  string
	Defaults Settings
	// Routes are the type's own HTTP entry points (a webhook) and admin actions, mounted by the API.
	Routes []Route
	// Tools are the MCP tools of the type's own admin actions.
	Tools []ToolSpec
}

// Route is one HTTP route of a type, mounted under /api/v1 with its token tier.
type Route struct {
	Method, Path string
	// Public routes take no session (a webhook authenticates itself); others need a signed-in admin.
	Public bool
	// Permission names the token permission the route needs; empty for a public route.
	Permission string
	// AlwaysAsk makes an agent token's call a request a person approves.
	AlwaysAsk bool
	Handler   func(kit RouteKit) http.HandlerFunc
}

// RouteKit is what the API gives a type's route.
type RouteKit interface {
	// Receive runs raw events of a channel through the pipeline.
	Receive(ctx context.Context, channel Channel, raws []Raw) ([]IngestResult, error)
	// FindChannel finds a live channel of the route's type by its external account.
	FindChannel(ctx context.Context, externalAccount string) (Channel, bool, error)
	// WriteError writes the API's error body.
	WriteError(w http.ResponseWriter, status int, code, message string)
}

// ToolSpec is an MCP tool of a type's own action, in neutral terms the MCP server translates.
type ToolSpec struct {
	Name, Description string
	// Method and Path name the Route the tool calls.
	Method, Path string
	InputSchema  json.RawMessage
}

// Adapter is a channel type: exactly three parts and its definition.
type Adapter interface {
	Definition() Definition
	Config() ConfigPart
	Inbound() InboundPart
	// Outbound is nil when the type sends nothing (Definition.RepliesVia names who does).
	Outbound() OutboundPart
}

// Status states a channel can be in. off and not_connected are the frame's; an adapter reports
// working or needs_attention with a reason code.
const (
	StateWorking        = "working"
	StateOff            = "off"
	StateNeedsAttention = "needs_attention"
	StateNotConnected   = "not_connected"
)

// Status is a channel's state with a fixed reason code the web app explains.
type Status struct {
	State      string `json:"state"`
	ReasonCode string `json:"reason_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

// ConfigChange is a validated change of a channel's own configuration, to be saved by the same adapter.
type ConfigChange struct {
	Value any
	// ExternalAccount, when not nil, replaces the channel's external account.
	ExternalAccount *string
	// Detail is recorded in the channel's history; never a secret.
	Detail map[string]any
}

// Event is what a saved change adds to the channel's history.
type Event struct {
	Kind   string
	Detail map[string]any
}

// FieldError is a validation error of one configuration field.
type FieldError struct {
	Fields  map[string]string
	Message string
	// Code is the API error code; empty is invalid_request.
	Code string
}

func (e *FieldError) Error() string { return e.Message }

// CredentialSpec names a secret the type keeps sealed in channel_credentials.
type CredentialSpec struct {
	Name, Label string
}

// ConfigPart is the type's configuration: its own table, validated and saved by the frame's one
// Configure path, whichever route asked.
type ConfigPart interface {
	// Validate reads a patch (the type's configuration schema in contracts/openapi.yaml). current is
	// nil when the channel is being created.
	Validate(ctx context.Context, tx pgx.Tx, workspaceID string, current *Channel, patch json.RawMessage) (ConfigChange, error)
	// Save writes the change to the type's table for the channel, created or not.
	Save(ctx context.Context, tx pgx.Tx, channel Channel, change ConfigChange) (Event, error)
	// Public is the configuration the API shows; never a secret.
	Public(ctx context.Context, tx pgx.Tx, channel Channel) (any, error)
	// Summary is the one line under the channel's name (an address, an origin).
	Summary(ctx context.Context, tx pgx.Tx, channel Channel) (string, error)
	Credentials() []CredentialSpec
	// Status is working or needs_attention; the frame reports off and not_connected itself.
	Status(ctx context.Context, tx pgx.Tx, channel Channel) (Status, error)
	// Remove deletes the type's own rows of a channel being removed.
	Remove(ctx context.Context, tx pgx.Tx, channel Channel) error
	// Snapshot reads what Parse needs of the type's own tables (a map of chat groups), read-only and
	// never a secret; nil when it needs nothing.
	Snapshot(ctx context.Context, tx pgx.Tx, channel Channel) (any, error)
}
