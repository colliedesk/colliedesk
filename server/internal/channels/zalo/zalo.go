// Package zalo is the Zalo Official Account channel (docs/user/zalo.md): a channel is one OA, reached
// through a Zalo app whose one webhook (POST /api/v1/channels/zalo/webhook) carries every OA the app is
// authorized for. Customers write to it one to one and in the groups it is in; replies go back the same
// way. Its own table is zalo_channels (the app, the team's Zalo user ids, the organization of each group);
// its secrets are the channel's credentials.
package zalo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Type is the Zalo channel's type.
const Type channels.Type = domain.ChannelZalo

// IdentityUser is the kind of a customer's Zalo user id (as the OA sees it), the address replies go to.
const IdentityUser = "zalo_user"

// Scopes: a customer writing to the OA alone, and the groups the OA is in.
const (
	ScopeDirect = "direct"
	ScopeGroup  = "group"
)

// Credentials the channel keeps sealed.
const (
	CredentialAppSecret    = "app_secret"
	CredentialOASecretKey  = "oa_secret_key"
	CredentialAccessToken  = "access_token"
	CredentialRefreshToken = "refresh_token"
)

// WebhookPath is the app's one webhook, below /api/v1.
const WebhookPath = "/channels/zalo/webhook"

// Status reasons the web app explains.
const (
	ReasonSecretMissing   = "zalo_secret_missing"
	ReasonTokenMissing    = "zalo_token_missing"
	ReasonSignatureFailed = "zalo_signature_failed"
)

// Limits of the configuration.
const (
	MaxStaff  = 200
	MaxGroups = 500
)

// Config is where the adapter reaches Zalo: the message API and the OAuth server. A test points them at
// a stand-in; nothing here calls Zalo unless a reply or a notice is sent.
type Config struct {
	MessageBase string
	OAuthBase   string
	HTTP        *http.Client
	Now         func() time.Time
	// Background runs what follows a delivery after it was answered (reading its sender's name); a test
	// runs it at once.
	Background func(func())
}

// Adapter is the Zalo channel type.
type Adapter struct {
	config Config
	// lookups bounds the name lookups running at once (maxLookups).
	lookups chan struct{}
}

// New returns the Zalo channel type.
func New(config Config) *Adapter {
	if config.MessageBase == "" {
		config.MessageBase = "https://openapi.zalo.me"
	}
	if config.OAuthBase == "" {
		config.OAuthBase = "https://oauth.zaloapp.com"
	}
	if config.HTTP == nil {
		config.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Background == nil {
		config.Background = func(f func()) { go f() }
	}
	return &Adapter{config: config, lookups: make(chan struct{}, maxLookups)}
}

func (a *Adapter) Definition() channels.Definition {
	return channels.Definition{Type: Type, Identified: true, Name: "Zalo OA", Guide: "zalo.md",
		Capabilities: []string{"threads", "attachments_in", "attachments_out"}, ReplyTo: IdentityUser, WhenOff: channels.OffHold,
		Scopes: []string{ScopeDirect, ScopeGroup}, IntakeScopes: []string{ScopeGroup}, SharedScopes: []string{ScopeGroup},
		FileLimits: channels.FileLimits{PerFile: 25 << 20, PerEvent: 50 << 20},
		Defaults: channels.Settings{JoinPolicy: channels.JoinPolicy{Kind: channels.JoinWithinDays, Days: 7},
			RateLimits: channels.RateLimits{WindowSeconds: 600, PerSender: 60, NewConversations: 200, Scope: channels.ScopeChannel},
			// A group opens a conversation only on a command; what its members say in between is collected
			// for the rules (or intake) to decide.
			Scopes: map[string]channels.Scope{
				ScopeDirect: {Mode: channels.ModeAll},
				ScopeGroup:  {Mode: channels.ModeCommands, Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}},
			}},
		Routes: []channels.Route{{Method: http.MethodPost, Path: WebhookPath, Public: true, Handler: a.webhook}}}
}

func (a *Adapter) Config() channels.ConfigPart     { return config{} }
func (a *Adapter) Inbound() channels.InboundPart   { return inbound{} }
func (a *Adapter) Outbound() channels.OutboundPart { return outbound{a} }

// ConfigValue is the channel's configuration as the API takes it.
type ConfigValue struct {
	OAID         *string            `json:"oa_id,omitempty"`
	AppID        *string            `json:"app_id,omitempty"`
	StaffUserIDs *[]string          `json:"staff_user_ids,omitempty"`
	Groups       *map[string]string `json:"groups,omitempty"`
}

// Public is the channel's configuration as the API shows it; never a secret.
type Public struct {
	OAID         string            `json:"oa_id"`
	AppID        string            `json:"app_id"`
	StaffUserIDs []string          `json:"staff_user_ids"`
	Groups       map[string]string `json:"groups"`
	// WebhookPath is the app's webhook below the server's /api/v1, for the Zalo app's settings.
	WebhookPath string `json:"webhook_path"`
	// Credentials say which secrets are set: each of the four, its last four characters and when it was
	// last written (a refresh too); never its value.
	Credentials []CredentialState `json:"credentials"`
}

// CredentialState is one secret as the API shows it.
type CredentialState struct {
	Name  string     `json:"name"`
	Hint  *string    `json:"hint"`
	SetAt *time.Time `json:"set_at"`
}

var zaloID = regexp.MustCompile(`^[0-9]{1,40}$`)

func fieldError(field, message string) error {
	return &channels.FieldError{Fields: map[string]string{field: message}, Message: "Check the Zalo channel."}
}

type config struct{}

func (config) Validate(_ context.Context, _ pgx.Tx, _ string, current *channels.Channel, patch json.RawMessage) (channels.ConfigChange, error) {
	var value ConfigValue
	if len(patch) > 0 {
		if err := json.Unmarshal(patch, &value); err != nil {
			return channels.ConfigChange{}, &channels.FieldError{Message: "The configuration must be oa_id, app_id, staff_user_ids and groups."}
		}
	}
	change := channels.ConfigChange{Detail: map[string]any{}}
	if current == nil && (value.OAID == nil || value.AppID == nil) {
		return channels.ConfigChange{}, fieldError("oa_id", "Enter the OA's id and the Zalo app's id.")
	}
	if value.OAID != nil {
		oa := strings.TrimSpace(*value.OAID)
		if !zaloID.MatchString(oa) {
			return channels.ConfigChange{}, fieldError("oa_id", "The OA id is the number Zalo gives the Official Account.")
		}
		value.OAID, change.ExternalAccount, change.Detail["oa_id"] = &oa, &oa, oa
	}
	if value.AppID != nil {
		app := strings.TrimSpace(*value.AppID)
		if !zaloID.MatchString(app) {
			return channels.ConfigChange{}, fieldError("app_id", "The app id is the number Zalo gives the app.")
		}
		value.AppID, change.Detail["app_id"] = &app, app
	}
	if value.StaffUserIDs != nil {
		staff := []string{}
		for _, id := range *value.StaffUserIDs {
			id = strings.TrimSpace(id)
			if !zaloID.MatchString(id) {
				return channels.ConfigChange{}, fieldError("staff_user_ids", "Each is a Zalo user id, a number.")
			}
			if !contains(staff, id) {
				staff = append(staff, id)
			}
		}
		if len(staff) > MaxStaff {
			return channels.ConfigChange{}, fieldError("staff_user_ids", fmt.Sprintf("At most %d team members.", MaxStaff))
		}
		value.StaffUserIDs, change.Detail["staff_user_ids"] = &staff, len(staff)
	}
	if value.Groups != nil {
		groups := map[string]string{}
		for id, organization := range *value.Groups {
			id, organization = strings.TrimSpace(id), strings.TrimSpace(organization)
			if id == "" || len(id) > 100 || strings.ContainsAny(id, " \t\r\n") || len([]rune(organization)) > 200 {
				return channels.ConfigChange{}, fieldError("groups", "Each group is its Zalo id and the organization it stands for (at most 200 characters).")
			}
			groups[id] = organization
		}
		if len(groups) > MaxGroups {
			return channels.ConfigChange{}, fieldError("groups", fmt.Sprintf("At most %d groups.", MaxGroups))
		}
		value.Groups, change.Detail["groups"] = &groups, len(groups)
	}
	if current != nil && len(change.Detail) == 0 {
		return channels.ConfigChange{}, nil
	}
	change.Value = value
	return change, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// Save writes what the change sets, keeping the rest.
func (config) Save(ctx context.Context, tx pgx.Tx, channel channels.Channel, change channels.ConfigChange) (channels.Event, error) {
	value, _ := change.Value.(ConfigValue)
	var groups []byte
	if value.Groups != nil {
		groups, _ = json.Marshal(*value.Groups)
	}
	if value.AppID == nil {
		// A change of the rest of the channel (its name, its settings) leaves its row as it is.
		if _, err := tx.Exec(ctx, `UPDATE zalo_channels SET staff_user_ids=COALESCE($3,staff_user_ids),groups=COALESCE($4::jsonb,groups),updated_at=now()
			WHERE workspace_id=$1 AND channel_id=$2`, channel.WorkspaceID, channel.ID, value.StaffUserIDs, groups); err != nil {
			return channels.Event{}, fmt.Errorf("save zalo channel: %w", err)
		}
		return channels.Event{Kind: "config_changed", Detail: change.Detail}, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO zalo_channels(workspace_id,channel_id,app_id,staff_user_ids,groups)
		VALUES($1,$2,$3,COALESCE($4,'{}'::text[]),COALESCE($5::jsonb,'{}'))
		ON CONFLICT(workspace_id,channel_id) DO UPDATE SET app_id=COALESCE($3,zalo_channels.app_id),
			staff_user_ids=COALESCE($4,zalo_channels.staff_user_ids),groups=COALESCE($5::jsonb,zalo_channels.groups),updated_at=now()`,
		channel.WorkspaceID, channel.ID, value.AppID, value.StaffUserIDs, groups); err != nil {
		return channels.Event{}, fmt.Errorf("save zalo channel: %w", err)
	}
	return channels.Event{Kind: "config_changed", Detail: change.Detail}, nil
}

// settings is the channel's row of zalo_channels.
type settings struct {
	AppID           string
	Staff           []string
	Groups          map[string]string
	SignatureFailed *time.Time
}

func readSettings(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (settings, error) {
	var s settings
	var groups []byte
	err := tx.QueryRow(ctx, `SELECT app_id,staff_user_ids,groups,signature_failed_at FROM zalo_channels WHERE workspace_id=$1 AND channel_id=$2`,
		workspaceID, channelID).Scan(&s.AppID, &s.Staff, &groups, &s.SignatureFailed)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings{Staff: []string{}, Groups: map[string]string{}}, nil
	}
	if err != nil {
		return s, fmt.Errorf("read zalo channel: %w", err)
	}
	s.Groups = map[string]string{}
	if len(groups) > 0 {
		if err := json.Unmarshal(groups, &s.Groups); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (config) Public(ctx context.Context, tx pgx.Tx, channel channels.Channel) (any, error) {
	s, err := readSettings(ctx, tx, channel.WorkspaceID, channel.ID)
	if err != nil {
		return nil, err
	}
	public := Public{OAID: channel.ExternalAccount, AppID: s.AppID, StaffUserIDs: s.Staff, Groups: s.Groups, WebhookPath: "/api/v1" + WebhookPath}
	stored := map[string]CredentialState{}
	rows, err := tx.Query(ctx, `SELECT name,hint,updated_at FROM channel_credentials WHERE workspace_id=$1 AND channel_id=$2`, channel.WorkspaceID, channel.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var state CredentialState
		var at time.Time
		if err := rows.Scan(&state.Name, &state.Hint, &at); err != nil {
			return nil, err
		}
		at = at.UTC()
		state.SetAt = &at
		stored[state.Name] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, spec := range (config{}).Credentials() {
		state, ok := stored[spec.Name]
		if !ok {
			state = CredentialState{Name: spec.Name}
		}
		public.Credentials = append(public.Credentials, state)
	}
	return public, nil
}

// Summary names the OA by the ends of its id, and how many groups stand for an organization.
func (config) Summary(ctx context.Context, tx pgx.Tx, channel channels.Channel) (string, error) {
	s, err := readSettings(ctx, tx, channel.WorkspaceID, channel.ID)
	if err != nil {
		return "", err
	}
	oa := channel.ExternalAccount
	if len(oa) > 10 {
		oa = oa[:4] + "…" + oa[len(oa)-4:]
	}
	linked := 0
	for _, organization := range s.Groups {
		if organization != "" {
			linked++
		}
	}
	unit := "groups"
	if linked == 1 {
		unit = "group"
	}
	return fmt.Sprintf("OA %s · %d %s linked", oa, linked, unit), nil
}

func (config) Credentials() []channels.CredentialSpec {
	return []channels.CredentialSpec{{Name: CredentialAppSecret, Label: "App secret key"}, {Name: CredentialOASecretKey, Label: "OA secret key"},
		{Name: CredentialAccessToken, Label: "Access token"}, {Name: CredentialRefreshToken, Label: "Refresh token"}}
}

// Status says what keeps the channel from working: a key that checks deliveries missing, a delivery whose
// signature did not match in the last day (it was not taken), or no token to send with.
func (config) Status(ctx context.Context, tx pgx.Tx, channel channels.Channel) (channels.Status, error) {
	s, err := readSettings(ctx, tx, channel.WorkspaceID, channel.ID)
	if err != nil {
		return channels.Status{}, err
	}
	names := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT name FROM channel_credentials WHERE workspace_id=$1 AND channel_id=$2`, channel.WorkspaceID, channel.ID)
	if err != nil {
		return channels.Status{}, err
	}
	stored, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return channels.Status{}, err
	}
	for _, name := range stored {
		names[name] = true
	}
	switch {
	case !names[CredentialOASecretKey]:
		return channels.Status{State: channels.StateNeedsAttention, ReasonCode: ReasonSecretMissing}, nil
	case s.SignatureFailed != nil && time.Since(*s.SignatureFailed) < 24*time.Hour:
		return channels.Status{State: channels.StateNeedsAttention, ReasonCode: ReasonSignatureFailed}, nil
	case !names[CredentialAccessToken] || !names[CredentialRefreshToken] || !names[CredentialAppSecret]:
		return channels.Status{State: channels.StateNeedsAttention, ReasonCode: ReasonTokenMissing}, nil
	}
	return channels.Status{State: channels.StateWorking}, nil
}

func (config) Remove(ctx context.Context, tx pgx.Tx, channel channels.Channel) error {
	// The names Zalo gave its customers go with it; their contacts keep theirs.
	if _, err := tx.Exec(ctx, `DELETE FROM zalo_users WHERE workspace_id=$1 AND channel_id=$2`, channel.WorkspaceID, channel.ID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM zalo_channels WHERE workspace_id=$1 AND channel_id=$2`, channel.WorkspaceID, channel.ID)
	return err
}

// snapshot is what parsing a delivery reads of the channel.
type snapshot struct {
	staff  map[string]bool
	groups map[string]string
	// names are the customers' names Zalo gave (zalo_users).
	names map[string]string
}

func (config) Snapshot(ctx context.Context, tx pgx.Tx, channel channels.Channel) (any, error) {
	s, err := readSettings(ctx, tx, channel.WorkspaceID, channel.ID)
	if err != nil {
		return nil, err
	}
	staff := map[string]bool{}
	for _, id := range s.Staff {
		staff[id] = true
	}
	rows, err := tx.Query(ctx, `SELECT user_id,name FROM zalo_users WHERE workspace_id=$1 AND channel_id=$2 AND name IS NOT NULL`, channel.WorkspaceID, channel.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var user, name string
		if err := rows.Scan(&user, &name); err != nil {
			return nil, err
		}
		names[user] = name
	}
	return snapshot{staff: staff, groups: s.Groups, names: names}, rows.Err()
}
