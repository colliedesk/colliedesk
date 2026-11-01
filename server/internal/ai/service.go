package ai

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/netguard"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// defaultCheckTimeout bounds a provider check, which a person waits for in Settings: well under
	// what a reverse proxy or an MCP client allows a request.
	defaultCheckTimeout = 20 * time.Second
	// checksPerMinute caps the provider checks of one workspace (adding, changing a key or a URL,
	// or the Check button): each is a billed call.
	checksPerMinute = 10
	eventsShown     = 50
	maxKeyLength    = 500
)

// ErrCheckRateLimited means the workspace ran checksPerMinute checks in the last minute.
var ErrCheckRateLimited = fmt.Errorf("at most %d provider checks a minute; wait a moment", checksPerMinute)

// Spec is what the adapter for one provider is built from.
type Spec struct {
	Kind    aiprovider.Kind
	BaseURL string
	APIKey  string
	// Server is the operator's provider: its network is the operator's and is not guarded.
	Server bool
}

// Config configures the service.
type Config struct {
	// Server is the operator's provider, nil when the server has none.
	Server *ServerProvider
	// ServerOff is true when the operator switched the server's provider off (features.ai_server): Server
	// is nil, and the features set to it say so until it is on again.
	ServerOff bool
	// Keys seal workspace keys (MAILBOX_TOKEN_KEYS); without one no key can be stored.
	Keys tokenseal.Keys
	// AllowPrivateBaseURLs lets a workspace provider use a private or loopback address and plain
	// http (AI_ALLOW_PRIVATE_BASE_URLS=1). An operator's decision; never set on Colliedesk Cloud.
	AllowPrivateBaseURLs bool
	// Build makes the adapter for a provider; nil uses the real adapters. Tests return a fake.
	Build func(Spec) (aiprovider.Provider, error)
	// CheckTimeout bounds one check; zero is 20 seconds.
	CheckTimeout time.Duration
	// ChatAnswerTimeout bounds one chat answer; zero is 60 seconds.
	ChatAnswerTimeout time.Duration
	// PlanLimits is true on the hosted cloud, where a trial and a lock apply: the allowance reads
	// the plan, and the worker cancels the tasks of a locked workspace.
	PlanLimits bool
	// ChatGate counts the chat answers streaming in this process; the services built for each new
	// configuration share it (NewChatGate). Nil makes one for this service alone.
	ChatGate *ChatGate
}

// Service reads and changes a workspace's AI settings. Every call names the workspace; a provider of
// another workspace is ErrNotFound.
type Service struct {
	db     *database.DB
	config Config
	now    func() time.Time
	checks checkLimiter
	// wakeup tells the worker a task was queued, so it does not wait for its next tick.
	wakeup chan struct{}
	// chats bounds the chat answers streaming at once (chat.go).
	chats *ChatGate
}

// NewService uses db (request pool).
func NewService(db *database.DB, config Config) *Service {
	if config.CheckTimeout <= 0 {
		config.CheckTimeout = defaultCheckTimeout
	}
	chats := config.ChatGate
	if chats == nil {
		chats = NewChatGate()
	}
	return &Service{db: db, config: config, now: time.Now, checks: checkLimiter{seen: map[string][]time.Time{}}, wakeup: make(chan struct{}, 1), chats: chats}
}

// SetClock replaces the clock; tests use it.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// ServerConfigured reports whether the server has a provider.
func (s *Service) ServerConfigured() bool { return s.config.Server != nil }

func (s *Service) build(spec Spec) (aiprovider.Provider, error) {
	if s.config.Build != nil {
		return s.config.Build(spec)
	}
	client := aiprovider.NewHTTPClient(aiprovider.HTTPOptions{Guarded: !spec.Server && !s.config.AllowPrivateBaseURLs})
	switch spec.Kind {
	case aiprovider.KindAnthropic:
		return aiprovider.NewAnthropic(aiprovider.AnthropicConfig{APIKey: spec.APIKey, BaseURL: spec.BaseURL, HTTPClient: client})
	case aiprovider.KindOpenAICompatible:
		return aiprovider.NewOpenAICompatible(aiprovider.OpenAIConfig{APIKey: spec.APIKey, BaseURL: spec.BaseURL, HTTPClient: client})
	}
	return nil, fmt.Errorf("unknown AI provider kind %q", spec.Kind)
}

// checkLimiter counts the checks of each workspace in the last minute. The server runs as one
// process, so memory is enough.
type checkLimiter struct {
	mu   sync.Mutex
	seen map[string][]time.Time
}

func (l *checkLimiter) allow(workspaceID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.seen[workspaceID][:0]
	for _, at := range l.seen[workspaceID] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent) >= checksPerMinute {
		l.seen[workspaceID] = recent
		return false
	}
	l.seen[workspaceID] = append(recent, now)
	return true
}

// check runs one metered check of a provider with the first model, bounded by CheckTimeout.
func (s *Service) check(ctx context.Context, call Call, spec Spec, model string) error {
	if !s.checks.allow(call.WorkspaceID, s.now()) {
		return ErrCheckRateLimited
	}
	provider, err := s.build(spec)
	if err != nil {
		return invalid("%s", err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.CheckTimeout)
	defer cancel()
	_, err = s.meteredCheck(ctx, call, provider, model)
	var providerErr *aiprovider.Error
	if errors.As(err, &providerErr) {
		return &CheckError{Code: providerErr.Code, Model: model}
	}
	return err
}

// workspaceSpec checks what a person typed for a provider of their own: the kind, its address and
// its key. Adding a provider and listing its models refuse the same input the same way.
func (s *Service) workspaceSpec(kindName, baseURLText, keyText string) (Spec, error) {
	key := strings.TrimSpace(keyText)
	if len(key) > maxKeyLength {
		return Spec{}, invalid("The API key is longer than %d characters.", maxKeyLength)
	}
	kind := aiprovider.Kind(kindName)
	var baseURL string
	switch kind {
	case aiprovider.KindAnthropic:
		if strings.TrimSpace(baseURLText) != "" {
			return Spec{}, invalid("An Anthropic provider always uses the Claude API; leave the base URL empty.")
		}
		if key == "" {
			return Spec{}, invalid("Paste an Anthropic API key (console.anthropic.com, API keys).")
		}
	case aiprovider.KindOpenAICompatible:
		var err error
		if baseURL, err = s.cleanBaseURL(baseURLText); err != nil {
			return Spec{}, err
		}
		if key == "" && !s.config.AllowPrivateBaseURLs {
			return Spec{}, invalid("Paste the provider's API key. A provider without a key is only for a server on the operator's own network.")
		}
	default:
		return Spec{}, invalid("The kind is anthropic or openai_compatible.")
	}
	return Spec{Kind: kind, BaseURL: baseURL, APIKey: key}, nil
}

// ModelsInput is a provider not saved yet, whose models a person wants to pick from.
type ModelsInput struct {
	Kind    string
	BaseURL string
	APIKey  string
}

// ListModels asks a provider which models its key may use, before anything is saved, so a person
// picks a model instead of typing its id. Nothing is stored and nothing is metered (listing costs no
// tokens); it counts against the workspace's checks a minute, since it calls out with a typed key.
func (s *Service) ListModels(ctx context.Context, workspaceID string, input ModelsInput) ([]string, error) {
	spec, err := s.workspaceSpec(input.Kind, input.BaseURL, input.APIKey)
	if err != nil {
		return nil, err
	}
	if !s.checks.allow(workspaceID, s.now()) {
		return nil, ErrCheckRateLimited
	}
	provider, err := s.build(spec)
	if err != nil {
		return nil, invalid("%s", err.Error())
	}
	lister, ok := provider.(aiprovider.ModelLister)
	if !ok {
		return nil, &CheckError{Code: aiprovider.ErrorBadRequest}
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.CheckTimeout)
	defer cancel()
	models, err := lister.Models(ctx)
	var providerErr *aiprovider.Error
	if errors.As(err, &providerErr) {
		return nil, &CheckError{Code: providerErr.Code}
	}
	if err != nil {
		return nil, err
	}
	if models == nil {
		models = []string{}
	}
	return models, nil
}

// ProviderInput adds a provider.
type ProviderInput struct {
	Kind    string
	Label   string
	BaseURL string
	APIKey  string
	Models  []string
}

// ProviderChange changes a provider; nil fields stay. The base URL never changes: the stored key and
// the features' data would go to a new address without anyone entering the key or consenting again.
type ProviderChange struct {
	Label  *string
	Models *[]string
}

// SlotInput sets one feature: where it runs (server, provider or none), the model, effort, on or off.
type SlotInput struct {
	Source     string
	ProviderID string
	Model      string
	Effort     string
	Enabled    bool
	// DefaultLevel, Automatic (the older switch, mapped onto DefaultLevel) and AutoParts are kept as
	// they are when nil.
	DefaultLevel *string
	Automatic    *bool
	AutoParts    []string
	// MonthlyCap is the chat slot's most a calendar month (UTC) may spend, in micro-dollars, kept as it
	// is unless MonthlyCapSet; nil with MonthlyCapSet removes it.
	MonthlyCap    *int64
	MonthlyCapSet bool
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func sealContext(workspaceID, providerID string) string {
	return "ai-provider:" + workspaceID + ":" + providerID
}

func keyHint(key string) string {
	runes := []rune(key)
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

func cleanLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" || len([]rune(label)) > 80 {
		return "", invalid("Give the provider a name of 1 to 80 characters.")
	}
	return label, nil
}

func cleanModels(models []string) ([]string, error) {
	var clean []string
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if len(model) > 200 {
			return nil, invalid("A model name has at most 200 characters.")
		}
		if !slices.Contains(clean, model) {
			clean = append(clean, model)
		}
	}
	if len(clean) == 0 || len(clean) > 20 {
		return nil, invalid("List 1 to 20 models; the check uses the first.")
	}
	return clean, nil
}

// cleanBaseURL checks a base URL a workspace admin typed. Without the operator's permission it must
// be https and must not name a private or loopback address; netguard also refuses such an address
// when a public name resolves to one.
func (s *Service) cleanBaseURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(value) > 500 {
		return "", invalid("The base URL is the provider's API address with its version path, for example https://openrouter.ai/api/v1.")
	}
	if s.config.AllowPrivateBaseURLs {
		return value, nil
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if (ip != nil && netguard.Blocked(ip)) || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return "", &CheckError{Code: aiprovider.ErrorBlockedAddress}
	}
	if parsed.Scheme != "https" {
		return "", invalid("Use an https address: a key must not cross the network in clear.")
	}
	return value, nil
}

func lockProviders(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-providers:"+workspaceID)
	return err
}

func roomForProvider(ctx context.Context, tx pgx.Tx, workspaceID, label, exceptID string) error {
	var count int
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT count(*), coalesce(bool_or(label=$2 AND id::text<>$3),false) FROM ai_providers WHERE workspace_id=$1`,
		workspaceID, label, exceptID).Scan(&count, &taken); err != nil {
		return err
	}
	switch {
	case taken:
		return ErrLabelTaken
	case exceptID == "" && count >= MaxProviders:
		return ErrLimit
	}
	return nil
}

func addEvent(ctx context.Context, tx pgx.Tx, workspaceID, kind string, actor Actor, providerID, providerLabel, slot string, detail any, at time.Time) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ai_settings_events(workspace_id,kind,actor_agent_id,actor_token_id,provider_id,provider_label,slot,detail,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, workspaceID, kind, nullable(actor.AgentID), nullable(actor.TokenID), nullable(providerID),
		nullable(providerLabel), nullable(slot), encoded, at)
	return err
}

// AddProvider checks a new provider with its first model and saves it only when the check passes.
// The key is sealed; nothing about a failed check is saved but its usage row.
func (s *Service) AddProvider(ctx context.Context, workspaceID string, actor Actor, input ProviderInput) (Provider, error) {
	label, err := cleanLabel(input.Label)
	if err != nil {
		return Provider{}, err
	}
	models, err := cleanModels(input.Models)
	if err != nil {
		return Provider{}, err
	}
	spec, err := s.workspaceSpec(input.Kind, input.BaseURL, input.APIKey)
	if err != nil {
		return Provider{}, err
	}
	kind, baseURL, key := spec.Kind, spec.BaseURL, spec.APIKey
	if key != "" && !s.config.Keys.Configured() {
		return Provider{}, ErrKeysMissing
	}
	// A cheap refusal before spending a check; the insert below checks again under the lock.
	if err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error { return roomForProvider(ctx, tx, workspaceID, label, "") }); err != nil {
		return Provider{}, err
	}
	id := newID()
	call := Call{WorkspaceID: workspaceID, BilledTo: BilledToWorkspaceKey, ProviderID: id, Kind: kind, Label: label, Actor: actor}
	if err := s.check(ctx, call, Spec{Kind: kind, BaseURL: baseURL, APIKey: key}, models[0]); err != nil {
		return Provider{}, err
	}
	var sealed []byte
	var sealedID, hint any
	authKind := "none"
	if key != "" {
		keyID, ciphertext, err := s.config.Keys.Seal([]byte(key), sealContext(workspaceID, id))
		if err != nil {
			return Provider{}, fmt.Errorf("seal the AI provider key: %w", err)
		}
		sealed, sealedID, hint, authKind = ciphertext, keyID, keyHint(key), "api_key"
	}
	now := s.now().UTC()
	var provider Provider
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := lockProviders(ctx, tx, workspaceID); err != nil {
			return err
		}
		if err := roomForProvider(ctx, tx, workspaceID, label, ""); err != nil {
			return err
		}
		provider, err = scanProvider(tx.QueryRow(ctx, `INSERT INTO ai_providers(id,workspace_id,kind,label,base_url,auth_kind,sealed_key,sealed_key_id,key_hint,models,
				check_status,checked_at,created_by,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'ok',$11,$12,$11,$11) RETURNING `+providerColumns,
			id, workspaceID, string(kind), label, nullable(baseURL), authKind, sealed, sealedID, hint, models, now, nullable(actor.AgentID)))
		if err != nil {
			return err
		}
		detail := map[string]any{"kind": string(kind), "models": models}
		if hint != nil {
			detail["key_hint"] = hint
		}
		if baseURL != "" {
			detail["base_url"] = baseURL
		}
		return addEvent(ctx, tx, workspaceID, "provider_added", actor, id, label, "", detail, now)
	})
	return provider, uniqueLabel(err)
}

func uniqueLabel(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrLabelTaken
	}
	return err
}

const providerColumns = `id::text,kind,label,base_url,auth_kind,key_hint,models,check_status,check_error_code,checked_at,created_by::text,created_at,updated_at`

func scanProvider(row pgx.Row) (Provider, error) {
	var p Provider
	err := row.Scan(&p.ID, &p.Kind, &p.Label, &p.BaseURL, &p.AuthKind, &p.KeyHint, &p.Models, &p.CheckStatus, &p.CheckErrorCode, &p.CheckedAt,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Provider{}, ErrNotFound
	}
	p.CheckedAt, p.CreatedAt, p.UpdatedAt = p.CheckedAt.UTC(), p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	p.UsedBy = []string{}
	return p, err
}

// stored is a provider row with what opens its key.
type stored struct {
	Provider
	sealed   []byte
	sealedID *string
}

func loadProvider(ctx context.Context, tx pgx.Tx, workspaceID, id string, lock bool) (stored, error) {
	if !isUUID(id) {
		return stored{}, ErrNotFound
	}
	query := `SELECT ` + providerColumns + `,sealed_key,sealed_key_id FROM ai_providers WHERE workspace_id=$1 AND id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	var row stored
	var p = &row.Provider
	err := tx.QueryRow(ctx, query, workspaceID, id).Scan(&p.ID, &p.Kind, &p.Label, &p.BaseURL, &p.AuthKind, &p.KeyHint, &p.Models, &p.CheckStatus,
		&p.CheckErrorCode, &p.CheckedAt, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &row.sealed, &row.sealedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return stored{}, ErrNotFound
	}
	p.CheckedAt, p.CreatedAt, p.UpdatedAt = p.CheckedAt.UTC(), p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	p.UsedBy = []string{}
	return row, err
}

func isUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F'):
			return false
		}
	}
	return true
}

func (s *Service) openKey(workspaceID string, row stored) (string, error) {
	if row.sealed == nil || row.sealedID == nil {
		return "", nil
	}
	plain, err := s.config.Keys.Open(*row.sealedID, row.sealed, sealContext(workspaceID, row.ID))
	if err != nil {
		return "", fmt.Errorf("open the AI provider key: %w", err)
	}
	return string(plain), nil
}

func (s *Service) spec(workspaceID string, row stored) (Spec, error) {
	key, err := s.openKey(workspaceID, row)
	if err != nil {
		return Spec{}, err
	}
	spec := Spec{Kind: aiprovider.Kind(row.Kind), APIKey: key}
	if row.BaseURL != nil {
		spec.BaseURL = *row.BaseURL
	}
	return spec, nil
}

func (s *Service) read(ctx context.Context, workspaceID, id string) (stored, error) {
	var row stored
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		row, err = loadProvider(ctx, tx, workspaceID, id, false)
		return err
	})
	return row, err
}

// UpdateProvider renames a provider or changes its models. A new first model is checked before it is
// saved; a model a feature uses cannot be removed.
func (s *Service) UpdateProvider(ctx context.Context, workspaceID string, actor Actor, id string, change ProviderChange) (Provider, error) {
	row, err := s.read(ctx, workspaceID, id)
	if err != nil {
		return Provider{}, err
	}
	label, models := row.Label, row.Models
	if change.Label != nil {
		if label, err = cleanLabel(*change.Label); err != nil {
			return Provider{}, err
		}
	}
	if change.Models != nil {
		if models, err = cleanModels(*change.Models); err != nil {
			return Provider{}, err
		}
	}
	if models[0] != row.Models[0] {
		spec, err := s.spec(workspaceID, row)
		if err != nil {
			return Provider{}, err
		}
		call := Call{WorkspaceID: workspaceID, BilledTo: BilledToWorkspaceKey, ProviderID: id, Kind: spec.Kind, Label: label, Actor: actor}
		if err := s.check(ctx, call, spec, models[0]); err != nil {
			return Provider{}, err
		}
	}
	now := s.now().UTC()
	var provider Provider
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		current, err := loadProvider(ctx, tx, workspaceID, id, true)
		if err != nil {
			return err
		}
		if err := roomForProvider(ctx, tx, workspaceID, label, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT slot,model FROM ai_slots WHERE workspace_id=$1 AND provider_id=$2 AND model IS NOT NULL ORDER BY slot`, workspaceID, id)
		if err != nil {
			return err
		}
		type use struct{ slot, model string }
		uses, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (use, error) {
			var u use
			return u, row.Scan(&u.slot, &u.model)
		})
		if err != nil {
			return err
		}
		for _, u := range uses {
			if !slices.Contains(models, u.model) {
				return &InUseError{Model: u.model, Slot: u.slot}
			}
		}
		checked := ""
		if models[0] != current.Models[0] {
			checked = `,check_status='ok',check_error_code=NULL,checked_at=$5`
		}
		provider, err = scanProvider(tx.QueryRow(ctx, `UPDATE ai_providers SET label=$3,models=$4,updated_at=$5`+checked+`
			WHERE workspace_id=$1 AND id=$2 RETURNING `+providerColumns, workspaceID, id, label, models, now))
		if err != nil {
			return err
		}
		detail := map[string]any{}
		if label != current.Label {
			detail["label"] = map[string]any{"from": current.Label, "to": label}
		}
		if !slices.Equal(models, current.Models) {
			detail["models"] = map[string]any{"from": current.Models, "to": models}
		}
		if len(detail) == 0 {
			return nil
		}
		return addEvent(ctx, tx, workspaceID, "provider_changed", actor, id, label, "", detail, now)
	})
	if err != nil {
		return Provider{}, uniqueLabel(err)
	}
	return s.withUses(ctx, workspaceID, provider)
}

// SetKey replaces a provider's key after the new key passes a check; a failed check changes nothing.
func (s *Service) SetKey(ctx context.Context, workspaceID string, actor Actor, id, key string) (Provider, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > maxKeyLength {
		return Provider{}, invalid("Paste the new API key.")
	}
	if !s.config.Keys.Configured() {
		return Provider{}, ErrKeysMissing
	}
	row, err := s.read(ctx, workspaceID, id)
	if err != nil {
		return Provider{}, err
	}
	spec := Spec{Kind: aiprovider.Kind(row.Kind), APIKey: key}
	if row.BaseURL != nil {
		spec.BaseURL = *row.BaseURL
	}
	call := Call{WorkspaceID: workspaceID, BilledTo: BilledToWorkspaceKey, ProviderID: id, Kind: spec.Kind, Label: row.Label, Actor: actor}
	if err := s.check(ctx, call, spec, row.Models[0]); err != nil {
		return Provider{}, err
	}
	keyID, sealed, err := s.config.Keys.Seal([]byte(key), sealContext(workspaceID, id))
	if err != nil {
		return Provider{}, fmt.Errorf("seal the AI provider key: %w", err)
	}
	now := s.now().UTC()
	var provider Provider
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		current, err := loadProvider(ctx, tx, workspaceID, id, true)
		if err != nil {
			return err
		}
		provider, err = scanProvider(tx.QueryRow(ctx, `UPDATE ai_providers SET auth_kind='api_key',sealed_key=$3,sealed_key_id=$4,key_hint=$5,
				check_status='ok',check_error_code=NULL,checked_at=$6,updated_at=$6
			WHERE workspace_id=$1 AND id=$2 RETURNING `+providerColumns, workspaceID, id, sealed, keyID, keyHint(key), now))
		if err != nil {
			return err
		}
		detail := map[string]any{"to_hint": keyHint(key)}
		if current.KeyHint != nil {
			detail["from_hint"] = *current.KeyHint
		}
		return addEvent(ctx, tx, workspaceID, "provider_key_changed", actor, id, current.Label, "", detail, now)
	})
	if err != nil {
		return Provider{}, err
	}
	return s.withUses(ctx, workspaceID, provider)
}

// CheckProvider runs a check of a saved provider and records the result: a failing provider stays
// saved and says why.
func (s *Service) CheckProvider(ctx context.Context, workspaceID string, actor Actor, id string) (Provider, error) {
	row, err := s.read(ctx, workspaceID, id)
	if err != nil {
		return Provider{}, err
	}
	spec, err := s.spec(workspaceID, row)
	if err != nil {
		return Provider{}, err
	}
	call := Call{WorkspaceID: workspaceID, BilledTo: BilledToWorkspaceKey, ProviderID: id, Kind: spec.Kind, Label: row.Label, Actor: actor}
	checkErr := s.check(ctx, call, spec, row.Models[0])
	var failed *CheckError
	if checkErr != nil && !errors.As(checkErr, &failed) {
		return Provider{}, checkErr
	}
	now := s.now().UTC()
	var provider Provider
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var code any
		status := "ok"
		if failed != nil {
			status, code = "failing", string(failed.Code)
		}
		var err error
		provider, err = scanProvider(tx.QueryRow(ctx, `UPDATE ai_providers SET check_status=$3,check_error_code=$4,checked_at=$5
			WHERE workspace_id=$1 AND id=$2 RETURNING `+providerColumns, workspaceID, id, status, code, now))
		return err
	})
	if err != nil {
		return Provider{}, err
	}
	return s.withUses(ctx, workspaceID, provider)
}

// RemoveProvider erases a provider and its key. The features that used it turn off and stay off:
// switching them to another provider would change who receives the workspace's data.
func (s *Service) RemoveProvider(ctx context.Context, workspaceID string, actor Actor, id string) ([]string, error) {
	now := s.now().UTC()
	var turnedOff []string
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		// The same lock as SetSlot and AddProvider: a feature being set to this provider meanwhile sees it gone.
		if err := lockProviders(ctx, tx, workspaceID); err != nil {
			return err
		}
		row, err := loadProvider(ctx, tx, workspaceID, id, true)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT slot,enabled,model FROM ai_slots WHERE workspace_id=$1 AND provider_id=$2 ORDER BY slot FOR UPDATE`, workspaceID, id)
		if err != nil {
			return err
		}
		type use struct {
			slot    string
			enabled bool
			model   *string
		}
		uses, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (use, error) {
			var u use
			return u, row.Scan(&u.slot, &u.enabled, &u.model)
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_slots SET provider_id=NULL,model=NULL,enabled=false,default_level='off',updated_by=$3,updated_at=$4
			WHERE workspace_id=$1 AND provider_id=$2`, workspaceID, id, nullable(actor.AgentID), now); err != nil {
			return err
		}
		for _, u := range uses {
			turnedOff = append(turnedOff, u.slot)
			from := map[string]any{"source": SourceProvider, "provider_label": row.Label, "enabled": u.enabled}
			if u.model != nil {
				from["model"] = *u.model
			}
			if err := addEvent(ctx, tx, workspaceID, "slot_changed", actor, id, row.Label, u.slot,
				map[string]any{"from": from, "to": map[string]any{"source": SourceNone, "enabled": false}, "reason": "provider_removed"}, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ai_providers WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
			return err
		}
		detail := map[string]any{"turned_off": turnedOff}
		if row.KeyHint != nil {
			detail["key_hint"] = *row.KeyHint
		}
		return addEvent(ctx, tx, workspaceID, "provider_removed", actor, id, row.Label, "", detail, now)
	})
	if turnedOff == nil {
		turnedOff = []string{}
	}
	return turnedOff, err
}

// PingServer sends one provider check to a server provider (the operator console's check before a change is
// saved) with the first model of the first slot that has one. It returns the model asked.
func (s *Service) PingServer(ctx context.Context, server *ServerProvider) (string, error) {
	if server == nil {
		return "", errors.New("the server has no AI provider")
	}
	model := ""
	for _, slot := range Slots {
		if models := server.ModelsFor(slot); len(models) > 0 {
			model = models[0]
			break
		}
	}
	if model == "" {
		return "", errors.New("the server AI provider offers no model")
	}
	provider, err := s.build(Spec{Kind: server.Kind, BaseURL: server.BaseURL, APIKey: server.APIKey, Server: true})
	if err != nil {
		return model, err
	}
	return model, operatorCheck(ctx, provider, model)
}
