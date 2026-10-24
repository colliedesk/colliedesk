// Package webform is the web form channel: a public form on a website opens a conversation for every
// submission. Its configuration is web_form_channels (the origins allowed to post); it sends nothing
// itself, its replies leave by email.
package webform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Type is the web form channel's type.
const Type channels.Type = domain.ChannelWebForm

// Tag marks every conversation the form opens.
const Tag = "web-form"

// NewConversationLimit caps the conversations the form opens per window, apart from email's
// ceiling, so form spam cannot block mail.
const NewConversationLimit = 50

// MaxOrigins is how many origins a form may list.
const MaxOrigins = 20

// Adapter is the web form channel type.
type Adapter struct{}

// New returns the web form channel type.
func New() *Adapter { return &Adapter{} }

func (*Adapter) Definition() channels.Definition {
	return channels.Definition{Type: Type, Singleton: true, Name: "Web form", Capabilities: []string{"form"}, ReplyTo: channels.IdentityEmail,
		RepliesVia: domain.ChannelEmail,
		// An unknown, off or locked form answers the same, so a slug cannot be probed.
		WhenOff: channels.OffRefuse,
		Defaults: channels.Settings{JoinPolicy: channels.JoinPolicy{Kind: channels.JoinNever}, DefaultTags: []string{Tag},
			RateLimits: channels.RateLimits{WindowSeconds: int(store.InboundRateWindow.Seconds()), NewConversations: NewConversationLimit, Scope: channels.ScopeChannel}}}
}

func (a *Adapter) Config() channels.ConfigPart     { return config{} }
func (a *Adapter) Inbound() channels.InboundPart   { return inbound{} }
func (a *Adapter) Outbound() channels.OutboundPart { return nil }

// ConfigValue is the web form's configuration as the API takes it.
type ConfigValue struct {
	AllowedOrigins *[]string `json:"allowed_origins,omitempty"`
}

// NormalizeOrigins lowercases, validates and de-duplicates allowed origins, keeping their order.
func NormalizeOrigins(values []string) ([]string, error) {
	origins := make([]string, 0, len(values))
	for _, value := range values {
		origin, ok := normalizeOrigin(value)
		if !ok {
			return nil, fmt.Errorf("Invalid origin %q: use an exact origin such as https://example.com, with no path, wildcard or credentials.", value)
		}
		if !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}
	if len(origins) > MaxOrigins {
		return nil, fmt.Errorf("At most %d origins are allowed.", MaxOrigins)
	}
	return origins, nil
}

func normalizeOrigin(value string) (string, bool) {
	if value == "" || strings.ContainsAny(value, "*\\ \t\r\n") {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.Opaque != "" || parsed.ForceQuery || strings.HasSuffix(value, "/") {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(parsed.Host), true
}

type config struct{}

func (config) Validate(_ context.Context, _ pgx.Tx, _ string, current *channels.Channel, patch json.RawMessage) (channels.ConfigChange, error) {
	var value ConfigValue
	if len(patch) > 0 {
		if err := json.Unmarshal(patch, &value); err != nil {
			return channels.ConfigChange{}, &channels.FieldError{Fields: map[string]string{"allowed_origins": "allowed_origins must be a list of origins."},
				Message: "allowed_origins must be a list of origins."}
		}
	}
	if value.AllowedOrigins == nil {
		if current == nil {
			value.AllowedOrigins = &[]string{}
		} else {
			return channels.ConfigChange{}, nil
		}
	}
	origins, err := NormalizeOrigins(*value.AllowedOrigins)
	if err != nil {
		return channels.ConfigChange{}, &channels.FieldError{Fields: map[string]string{"allowed_origins": err.Error()}, Message: err.Error()}
	}
	value.AllowedOrigins = &origins
	return channels.ConfigChange{Value: value, Detail: map[string]any{"allowed_origins": origins}}, nil
}

// Save writes the origins, and the channel's on or off for older images, which read it here.
func (config) Save(ctx context.Context, tx pgx.Tx, channel channels.Channel, change channels.ConfigChange) (channels.Event, error) {
	var origins []string
	if value, ok := change.Value.(ConfigValue); ok && value.AllowedOrigins != nil {
		origins = *value.AllowedOrigins
	}
	if _, err := tx.Exec(ctx, `INSERT INTO web_form_channels(workspace_id,enabled,allowed_origins,channel_id) VALUES($1,$2,COALESCE($3,'{}'::text[]),$4)
		ON CONFLICT(workspace_id) DO UPDATE SET enabled=EXCLUDED.enabled,allowed_origins=COALESCE($3,web_form_channels.allowed_origins),
			channel_id=EXCLUDED.channel_id,updated_at=now()`,
		channel.WorkspaceID, channel.Enabled, origins, channel.ID); err != nil {
		return channels.Event{}, fmt.Errorf("save web form: %w", err)
	}
	return channels.Event{Kind: "config_changed", Detail: change.Detail}, nil
}

func (config) Public(ctx context.Context, tx pgx.Tx, channel channels.Channel) (any, error) {
	form := domain.WebForm{Enabled: channel.Enabled, AllowedOrigins: []string{}}
	err := tx.QueryRow(ctx, `SELECT allowed_origins FROM web_form_channels WHERE workspace_id=$1`, channel.WorkspaceID).Scan(&form.AllowedOrigins)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var slug string
	if err := tx.QueryRow(ctx, `SELECT slug FROM workspaces WHERE id=$1`, channel.WorkspaceID).Scan(&slug); err != nil {
		return nil, err
	}
	form.SubmitPath = SubmitPathPrefix + slug
	return form, nil
}

// SubmitPathPrefix is where a website posts, followed by the workspace slug.
const SubmitPathPrefix = "/api/v1/public/forms/"

func (c config) Summary(ctx context.Context, tx pgx.Tx, channel channels.Channel) (string, error) {
	public, err := c.Public(ctx, tx, channel)
	if err != nil {
		return "", err
	}
	return strings.Join(public.(domain.WebForm).AllowedOrigins, ", "), nil
}

func (config) Credentials() []channels.CredentialSpec { return nil }

// ReasonNoOrigins: a form that is on but lists no origin cannot be posted from any page.
const ReasonNoOrigins = "no_origins"

func (c config) Status(ctx context.Context, tx pgx.Tx, channel channels.Channel) (channels.Status, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(cardinality(allowed_origins),0) FROM web_form_channels WHERE workspace_id=$1`, channel.WorkspaceID).Scan(&count); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return channels.Status{}, err
	}
	if count == 0 {
		return channels.Status{State: channels.StateNeedsAttention, ReasonCode: ReasonNoOrigins}, nil
	}
	return channels.Status{State: channels.StateWorking}, nil
}

func (config) Remove(ctx context.Context, tx pgx.Tx, channel channels.Channel) error {
	_, err := tx.Exec(ctx, `DELETE FROM web_form_channels WHERE workspace_id=$1`, channel.WorkspaceID)
	return err
}

func (config) Snapshot(context.Context, pgx.Tx, channels.Channel) (any, error) { return nil, nil }

// Submission is a validated submission of the public form.
type Submission struct {
	Email   string `json:"email"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

type inbound struct{}

func (inbound) Mode() channels.IngestMode { return channels.Sync }

// Parse makes a message of a submission. The sender typed the address and nothing checked it; the
// mark stays on the message when an agent removes the tag.
func (inbound) Parse(_ context.Context, _ channels.Channel, _ any, raw channels.Raw) ([]channels.Inbound, error) {
	var submission Submission
	if err := json.Unmarshal(raw.JSON, &submission); err != nil {
		return nil, fmt.Errorf("read web form submission: %w", err)
	}
	address := strings.ToLower(submission.Email)
	subject := "Web form"
	if submission.Source != "" {
		subject += ": " + submission.Source
	}
	text := submission.Message
	if text == "" {
		text = "(no message)"
	}
	sentAt := raw.ReceivedAt
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	return []channels.Inbound{{
		Sender:  channels.Sender{Identity: channels.Identity{Kind: channels.IdentityEmail, Value: submission.Email}, Email: address},
		Subject: subject, Text: text, SentAt: sentAt,
		EmailMeta: &domain.IncomingEmail{From: submission.Email, Authentication: "none", Via: domain.ViaWebForm, Attachments: []domain.EmailAttachment{}},
	}}, nil
}
