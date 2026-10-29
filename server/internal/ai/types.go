package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// MaxProviders is how many providers a workspace may add.
const MaxProviders = 5

// Slot sources.
const (
	SourceServer   = "server"
	SourceProvider = "provider"
	SourceNone     = "none"
)

// Why a slot that is on cannot run.
const (
	OffServerProviderMissing = "server_provider_missing"
	// OffServerProviderOff: the operator switched the server's provider off; the feature runs again once
	// it is on.
	OffServerProviderOff = "server_provider_off"
	OffModelNotOffered   = "model_not_offered"
)

// Provider is a provider a workspace added. It never carries the key, only its last four characters.
type Provider struct {
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
	// UsedBy lists the slots set to this provider, on or off.
	UsedBy []string `json:"used_by"`
}

// Server describes the server's provider without its key.
type Server struct {
	Kind   string              `json:"kind"`
	Label  string              `json:"label"`
	Models map[string][]string `json:"models"`
}

// Slot is one feature's setting. Active is whether it runs now: on, and its provider and model are
// still there. A slot that is on but cannot run says why in OffReason.
type Slot struct {
	Slot       string  `json:"slot"`
	Source     string  `json:"source"`
	ProviderID *string `json:"provider_id"`
	Model      *string `json:"model"`
	Effort     string  `json:"effort"`
	Enabled    bool    `json:"enabled"`
	// Automatic: triage suggestions apply by themselves (the AutoParts of them) as they come.
	Automatic bool     `json:"automatic"`
	AutoParts []string `json:"auto_parts"`
	// MonthlyCap is the chat slot's own monthly limit in micro-dollars; nil is none.
	MonthlyCap  *int64     `json:"monthly_cap_micro_usd"`
	Active      bool       `json:"active"`
	OffReason   *string    `json:"off_reason"`
	ConsentedBy *string    `json:"consented_by"`
	ConsentedAt *time.Time `json:"consented_at"`
	UpdatedBy   *string    `json:"updated_by"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

// Event is one change to the AI settings.
type Event struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	ActorAgentID  *string         `json:"actor_agent_id"`
	ActorName     *string         `json:"actor_name"`
	ViaToken      bool            `json:"via_token"`
	ProviderID    *string         `json:"provider_id"`
	ProviderLabel *string         `json:"provider_label"`
	Slot          *string         `json:"slot"`
	Detail        json.RawMessage `json:"detail"`
	CreatedAt     time.Time       `json:"created_at"`
}

// Settings is everything Settings → Built-in AI shows.
type Settings struct {
	Server    *Server    `json:"server"`
	Providers []Provider `json:"providers"`
	Slots     []Slot     `json:"slots"`
	// Events are the newest changes, newest first.
	Events []Event `json:"events"`
	// ProviderLimit is MaxProviders; CanStoreKeys is false while the server has no MAILBOX_TOKEN_KEYS.
	ProviderLimit        int  `json:"provider_limit"`
	CanStoreKeys         bool `json:"can_store_keys"`
	AllowPrivateBaseURLs bool `json:"allow_private_base_urls"`
	// Automatic is the workspace's automatic limits and today's use; Allowance what the server's
	// provider may still spend this month, null when the server sets no allowance.
	Automatic AutomaticLimits `json:"automatic"`
	Allowance *AllowanceView  `json:"allowance"`
	// AutoTriage counts the suggestions AI applied by itself in the last 30 days, and how many of those
	// a person undid.
	AutoTriage AutoTriageStats `json:"auto_triage"`
	// AutoDraftEstimate is what writing a draft for every customer message would have meant in the last
	// 30 days, shown before an admin turns it on.
	AutoDraftEstimate AutoDraftEstimate `json:"auto_draft_estimate"`
}

// AutoDraftEstimate: CustomerMessages came in the last 30 days (imports left out); DraftCostMicroUSD
// is the workspace's average draft on the server's provider in that time, or a typical one at the
// server's prices, null when there is no price to go by (a workspace's own key).
type AutoDraftEstimate struct {
	CustomerMessages  int    `json:"customer_messages"`
	DraftCostMicroUSD *int64 `json:"draft_cost_micro_usd"`
}

// AutoTriageStats is how automatic triage went in the last 30 days.
type AutoTriageStats struct {
	Applied int `json:"applied"`
	Undone  int `json:"undone"`
}

// AllowanceView is this month's allowance of the server's provider, in micro-dollars.
type AllowanceView struct {
	LimitMicroUSD int64     `json:"limit_micro_usd"`
	UsedMicroUSD  int64     `json:"used_micro_usd"`
	ResetsAt      time.Time `json:"resets_at"`
	// DraftsLeft is about how many reply drafts what is left pays for, at the workspace's own average
	// cost of a draft over 30 days or, before its first, a typical one; nil when no price is known.
	DraftsLeft *int64 `json:"drafts_left"`
}

// Actor is who changes the settings: the agent, and the token's session when a token did.
type Actor struct {
	AgentID string
	TokenID string
}

var (
	// ErrNotFound means no provider with that id in this workspace.
	ErrNotFound = errors.New("no AI provider with that id in this workspace")
	// ErrKeysMissing means the server cannot seal a key: MAILBOX_TOKEN_KEYS is not set.
	ErrKeysMissing = errors.New("this server cannot store API keys: MAILBOX_TOKEN_KEYS is not set")
	// ErrLimit means the workspace already has MaxProviders providers.
	ErrLimit = fmt.Errorf("a workspace can add at most %d AI providers", MaxProviders)
	// ErrLabelTaken means another provider of the workspace has that name.
	ErrLabelTaken = errors.New("another AI provider of this workspace has that name")
	// ErrNoServerProvider means the server has no provider (or none for that slot).
	ErrNoServerProvider = errors.New("this server has no AI provider for that feature")
	// ErrServerProviderOff means the operator switched the server's provider off.
	ErrServerProviderOff = errors.New("the server's AI provider is turned off on this server")
)

// InvalidError is a request the service refuses; Message says what to send instead.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// InUseError refuses removing a model a slot uses.
type InUseError struct{ Model, Slot string }

func (e *InUseError) Error() string {
	return fmt.Sprintf("the model %s is used by the %s feature; pick another model there first", e.Model, e.Slot)
}

// CheckError is a provider check that failed: nothing was saved. Code is the provider error code.
// CheckError is a failed check; Model names the model it asked for, empty when no model was asked
// (listing a provider's models, a refused address).
type CheckError struct {
	Code  aiprovider.ErrorCode
	Model string
}

func (e *CheckError) Error() string { return "the provider check failed: " + string(e.Code) }
