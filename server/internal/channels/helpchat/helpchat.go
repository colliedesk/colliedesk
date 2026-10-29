// Package helpchat is the help center's AI chat channel: end users ask on the help center's chat page,
// in the chat box of an article or in a widget on the workspace's own site, an AI answers from the
// published articles, and "Talk to a person" opens a conversation with the chat's transcript. Its
// configuration is help_chat_channels (what the chat says first, the questions it offers, what the
// admin adds to the AI's instructions, the sites that may embed it); it sends nothing itself, its
// replies leave by email. The AI part (turns, the articles it reads, limits, the chat's cookie) is the
// ai package's: this adapter holds the surface, the hand-off and where its routes mount.
package helpchat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/webform"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Type is the help chat channel's type.
const Type channels.Type = domain.ChannelHelpChat

// Tag marks every conversation a hand-off opens.
const Tag = "help-chat"

// Limits of the configuration, as help_chat_channels checks them.
const (
	MaxGreeting     = 300
	MaxSuggestions  = 4
	MaxSuggestion   = 120
	MaxInstructions = 2000
)

// NewConversationLimit caps the conversations hand-offs open per window, apart from email's ceiling.
const NewConversationLimit = 50

// PerAddress caps the hand-offs one address sends per window, whatever email it types.
const PerAddress = 5

// ThreadKeyPrefix names the chat a hand-off came from: a second hand-off of one chat joins its
// conversation.
const ThreadKeyPrefix = "chat-session:"

// Config is what the type needs of the server: the help center routes of the chat, built by the
// server around the ai package. Nil serves none.
type Config struct {
	HelpCenterRoutes    []channels.HelpCenterRoute
	HelpCenterAdditions func(kit channels.HelpCenterKit) func(ctx context.Context, at channels.HelpCenter) (channels.HelpCenterAdditions, error)
}

// Adapter is the help chat channel type.
type Adapter struct{ config Config }

// New returns the help chat channel type.
func New(config Config) *Adapter { return &Adapter{config: config} }

func (a *Adapter) Definition() channels.Definition {
	return channels.Definition{Type: Type, Name: "Help chat", Guide: "help-chat.md", Capabilities: []string{"help_center_chat", "site_chat", "handoff"},
		ReplyTo: channels.IdentityEmail, RepliesVia: domain.ChannelEmail,
		// An unknown, off or locked chat answers the same, so nothing can be probed.
		WhenOff: channels.OffRefuse,
		Defaults: channels.Settings{JoinPolicy: channels.JoinPolicy{Kind: channels.JoinAlwaysReopen}, DefaultTags: []string{Tag},
			RateLimits: channels.RateLimits{WindowSeconds: int(store.InboundRateWindow.Seconds()), PerSender: PerAddress, NewConversations: NewConversationLimit,
				Scope: channels.ScopeChannel}},
		HelpCenterRoutes: a.config.HelpCenterRoutes, HelpCenterAdditions: a.config.HelpCenterAdditions}
}

func (a *Adapter) Config() channels.ConfigPart     { return config{} }
func (a *Adapter) Inbound() channels.InboundPart   { return inbound{} }
func (a *Adapter) Outbound() channels.OutboundPart { return nil }

// ConfigValue is the help chat's configuration as the API takes it; a field left out is kept.
type ConfigValue struct {
	Greeting      *string   `json:"greeting,omitempty"`
	Suggestions   *[]string `json:"suggestions,omitempty"`
	Instructions  *string   `json:"instructions,omitempty"`
	WidgetOrigins *[]string `json:"widget_origins,omitempty"`
}

// Surface is a help chat channel's configuration as the chat and the API read it.
type Surface struct {
	ChannelID     string   `json:"channel_id"`
	Enabled       bool     `json:"enabled"`
	Greeting      string   `json:"greeting"`
	Suggestions   []string `json:"suggestions"`
	Instructions  string   `json:"instructions"`
	WidgetOrigins []string `json:"widget_origins"`
}

type config struct{}

func fieldError(field, message string) error {
	return &channels.FieldError{Fields: map[string]string{field: message}, Message: message}
}

// clean trims a text and refuses control characters other than line breaks and tabs.
func clean(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for _, r := range value {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return "", false
		}
	}
	return value, utf8.ValidString(value)
}

func (config) Validate(_ context.Context, _ pgx.Tx, _ string, current *channels.Channel, patch json.RawMessage) (channels.ConfigChange, error) {
	var value ConfigValue
	if len(patch) > 0 {
		if err := json.Unmarshal(patch, &value); err != nil {
			return channels.ConfigChange{}, &channels.FieldError{Message: "The configuration must be greeting, suggestions, instructions and widget_origins."}
		}
	}
	detail := map[string]any{}
	if value.Greeting != nil {
		greeting, ok := clean(*value.Greeting)
		if !ok || utf8.RuneCountInString(greeting) > MaxGreeting {
			return channels.ConfigChange{}, fieldError("greeting", fmt.Sprintf("Write a greeting of at most %d characters.", MaxGreeting))
		}
		value.Greeting, detail["greeting"] = &greeting, true
	}
	if value.Suggestions != nil {
		suggestions := []string{}
		for _, suggestion := range *value.Suggestions {
			suggestion, ok := clean(suggestion)
			if !ok || strings.ContainsAny(suggestion, "\n\t") || utf8.RuneCountInString(suggestion) > MaxSuggestion {
				return channels.ConfigChange{}, fieldError("suggestions", fmt.Sprintf("Each suggested question is one line of at most %d characters.", MaxSuggestion))
			}
			if suggestion != "" {
				suggestions = append(suggestions, suggestion)
			}
		}
		if len(suggestions) > MaxSuggestions {
			return channels.ConfigChange{}, fieldError("suggestions", fmt.Sprintf("At most %d suggested questions.", MaxSuggestions))
		}
		value.Suggestions, detail["suggestions"] = &suggestions, len(suggestions)
	}
	if value.Instructions != nil {
		instructions, ok := clean(*value.Instructions)
		if !ok || utf8.RuneCountInString(instructions) > MaxInstructions {
			return channels.ConfigChange{}, fieldError("instructions", fmt.Sprintf("Write instructions of at most %d characters.", MaxInstructions))
		}
		value.Instructions, detail["instructions"] = &instructions, true
	}
	if value.WidgetOrigins != nil {
		origins, err := webform.NormalizeOrigins(*value.WidgetOrigins)
		if err != nil {
			return channels.ConfigChange{}, fieldError("widget_origins", err.Error())
		}
		value.WidgetOrigins, detail["widget_origins"] = &origins, origins
	}
	if current != nil && len(detail) == 0 {
		return channels.ConfigChange{}, nil
	}
	return channels.ConfigChange{Value: value, Detail: detail}, nil
}

// Save writes what the change sets, keeping the rest.
func (config) Save(ctx context.Context, tx pgx.Tx, channel channels.Channel, change channels.ConfigChange) (channels.Event, error) {
	value, _ := change.Value.(ConfigValue)
	if _, err := tx.Exec(ctx, `INSERT INTO help_chat_channels(workspace_id,channel_id,greeting,suggestions,instructions,widget_origins)
		VALUES($1,$2,COALESCE($3,''),COALESCE($4,'{}'::text[]),COALESCE($5,''),COALESCE($6,'{}'::text[]))
		ON CONFLICT(workspace_id,channel_id) DO UPDATE SET greeting=COALESCE($3,help_chat_channels.greeting),
			suggestions=COALESCE($4,help_chat_channels.suggestions),instructions=COALESCE($5,help_chat_channels.instructions),
			widget_origins=COALESCE($6,help_chat_channels.widget_origins),updated_at=now()`,
		channel.WorkspaceID, channel.ID, value.Greeting, value.Suggestions, value.Instructions, value.WidgetOrigins); err != nil {
		return channels.Event{}, fmt.Errorf("save help chat: %w", err)
	}
	return channels.Event{Kind: "config_changed", Detail: change.Detail}, nil
}

// ReadSurface reads a channel's configuration in a transaction of its workspace.
func ReadSurface(ctx context.Context, tx pgx.Tx, channel channels.Channel) (Surface, error) {
	surface := Surface{ChannelID: channel.ID, Enabled: channel.Enabled, Suggestions: []string{}, WidgetOrigins: []string{}}
	err := tx.QueryRow(ctx, `SELECT greeting,suggestions,instructions,widget_origins FROM help_chat_channels WHERE workspace_id=$1 AND channel_id=$2`,
		channel.WorkspaceID, channel.ID).Scan(&surface.Greeting, &surface.Suggestions, &surface.Instructions, &surface.WidgetOrigins)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Surface{}, err
	}
	return surface, nil
}

func (config) Public(ctx context.Context, tx pgx.Tx, channel channels.Channel) (any, error) {
	return ReadSurface(ctx, tx, channel)
}

func (config) Summary(ctx context.Context, tx pgx.Tx, channel channels.Channel) (string, error) {
	surface, err := ReadSurface(ctx, tx, channel)
	if err != nil {
		return "", err
	}
	if len(surface.WidgetOrigins) == 0 {
		return "Help center", nil
	}
	return "Help center, " + strings.Join(surface.WidgetOrigins, ", "), nil
}

func (config) Credentials() []channels.CredentialSpec { return nil }

// ReasonAIChatOff: the channel is on but the workspace's AI chat feature is off or cannot run, so the
// help center shows no chat.
const ReasonAIChatOff = "ai_chat_off"

func (config) Status(ctx context.Context, tx pgx.Tx, channel channels.Channel) (channels.Status, error) {
	var on bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ai_slots WHERE workspace_id=$1 AND slot='chat' AND enabled)`, channel.WorkspaceID).Scan(&on); err != nil {
		return channels.Status{}, err
	}
	if !on {
		return channels.Status{State: channels.StateNeedsAttention, ReasonCode: ReasonAIChatOff}, nil
	}
	return channels.Status{State: channels.StateWorking}, nil
}

func (config) Remove(ctx context.Context, tx pgx.Tx, channel channels.Channel) error {
	_, err := tx.Exec(ctx, `DELETE FROM help_chat_channels WHERE workspace_id=$1 AND channel_id=$2`, channel.WorkspaceID, channel.ID)
	return err
}

func (config) Snapshot(context.Context, pgx.Tx, channels.Channel) (any, error) { return nil, nil }

// Handoff is an end user's "Talk to a person": who they are (typed, never verified), what they add,
// and the chat's transcript as the team reads it.
type Handoff struct {
	SessionID string `json:"session_id"`
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	Note      string `json:"note,omitempty"`
	// Transcript is the chat as text: each turn "Customer: …" and "AI: …" with its sources' links.
	Transcript string `json:"transcript"`
	// FirstQuestion names the conversation.
	FirstQuestion string `json:"first_question,omitempty"`
	// Address is a hash of where the request came from (the client's address): the rate limits count
	// it, so typing another email each time does not get past them.
	Address string `json:"address"`
	// Press names one press of the form's button; a repeat of it is the duplicate of what it stored.
	Press string `json:"press,omitempty"`
}

// AddressKey hashes a client's address for Handoff.Address: the journal never keeps the address itself.
func AddressKey(workspaceID, address string) string {
	sum := sha256.Sum256([]byte(workspaceID + "\x00" + address))
	return hex.EncodeToString(sum[:12])
}

type inbound struct{}

func (inbound) Mode() channels.IngestMode { return channels.Sync }

// Parse makes a message of a hand-off. The end user typed the address and nothing checked it.
func (inbound) Parse(_ context.Context, _ channels.Channel, _ any, raw channels.Raw) ([]channels.Inbound, error) {
	var handoff Handoff
	if err := json.Unmarshal(raw.JSON, &handoff); err != nil {
		return nil, fmt.Errorf("read help chat hand-off: %w", err)
	}
	address := strings.ToLower(strings.TrimSpace(handoff.Email))
	subject := "Help chat"
	if first := strings.Join(strings.Fields(handoff.FirstQuestion), " "); first != "" {
		if utf8.RuneCountInString(first) > 120 {
			first = string([]rune(first)[:119]) + "…"
		}
		subject += ": " + first
	}
	var text strings.Builder
	if note := strings.TrimSpace(handoff.Note); note != "" {
		text.WriteString(note + "\n\n")
	}
	text.WriteString("Chat with the AI on the help center:\n\n" + strings.TrimSpace(handoff.Transcript))
	sentAt := raw.ReceivedAt
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	externalID := ""
	if handoff.Press != "" {
		externalID = "press:" + handoff.Press
	}
	in := channels.Inbound{
		ExternalID: externalID,
		Sender:     channels.Sender{Identity: channels.Identity{Kind: channels.IdentityEmail, Value: address}, Name: strings.TrimSpace(handoff.Name), Email: address},
		Subject:    subject, Text: text.String(), SentAt: sentAt,
	}
	if handoff.SessionID != "" {
		key := ThreadKeyPrefix + handoff.SessionID
		in.Thread = channels.Thread{Keys: []string{key}, Record: []string{key}}
	}
	if handoff.Address != "" {
		in.RateKey = "address:" + handoff.Address
	}
	return []channels.Inbound{in}, nil
}
