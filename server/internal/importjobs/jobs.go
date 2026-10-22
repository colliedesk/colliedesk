// Package importjobs runs imports inside the server: a person or an AI agent starts one through the
// API, a worker in `collie serve` runs it in the background, and its state, progress and report live
// in the import_jobs table. The rules are in docs/internals/freshdesk-import.md ("In-app import").
package importjobs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// Job states.
const (
	StatePreviewing = "previewing"
	StatePreviewed  = "previewed"
	StateQueued     = "queued"
	StateRunning    = "running"
	StateRetryWait  = "retry_wait"
	StateWaitingKey = "waiting_key"
	StateFinished   = "finished"
	StateFailed     = "failed"
	StateCancelled  = "cancelled"
	StateDiscarded  = "discarded"
)

// Job phases: what a worker runs next.
const (
	PhasePreview = "preview"
	PhaseRecent  = "recent"
	PhaseFull    = "full"
	PhaseUndo    = "undo"
)

// Job kinds.
const (
	KindImport = "import"
	KindUndo   = "undo"
)

// Error codes of a job (error_code).
const (
	CodeKeyRejected      = "freshdesk_key_rejected"
	CodeNeedsAdminKey    = "freshdesk_needs_admin_key"
	CodePlanNoAPI        = "freshdesk_plan_no_api"
	CodeUnavailable      = "freshdesk_unavailable"
	CodeRunningElsewhere = "import_running_elsewhere"
	CodeStorageCap       = "storage_cap"
	CodeKnowledgeBase    = "knowledge_base_limit"
	CodeKeyUnavailable   = "key_unavailable"
	CodeNoProgress       = "no_progress"
	CodeRetryingFailures = "retrying_failed_items"
	CodeImportError      = "import_error"
)

// DefaultRecentDays is how far back the recent pass reaches; Options.RecentDays changes it (0 skips it).
const DefaultRecentDays = 90

// MaxRecentDays bounds Options.RecentDays.
const MaxRecentDays = 365

// PreviewTTL is how long a preview nobody started keeps its key.
const PreviewTTL = time.Hour

var (
	ErrNotFound = errors.New("import not found")
	// ErrActive means the workspace already has an import or undo queued, running or waiting.
	ErrActive = errors.New("an import or undo is already running in this workspace")
	// ErrState means the job is not in a state that allows the action.
	ErrState = errors.New("the import is not in a state that allows this")
	// ErrOptions means the options are out of range.
	ErrOptions = errors.New("recent_days must be between 0 and 365")
	// errKeyLost means the key is neither sealed in the row nor in this process's memory.
	errKeyLost = errors.New("this server no longer holds the Freshdesk API key")
)

// Options are what a person chooses for an import.
type Options struct {
	// RecentDays is the window of the recent pass; nil means DefaultRecentDays, 0 skips the pass.
	RecentDays                     *int  `json:"recent_days,omitempty"`
	IncludePersonalCannedResponses bool  `json:"include_personal_canned_responses"`
	HelpCenterPortal               int64 `json:"help_center_portal,omitempty"`
}

func (o Options) recentDays() int {
	if o.RecentDays == nil {
		return DefaultRecentDays
	}
	return *o.RecentDays
}

func (o Options) valid() bool {
	days := o.recentDays()
	return days >= 0 && days <= MaxRecentDays && o.HelpCenterPortal >= 0
}

// Progress is the last progress a worker saved.
type Progress struct {
	// Step is the importer's step, for example "importing tickets".
	Step  string `json:"step"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// RateLimitUntil is set while the run waits for the Freshdesk rate limit.
	RateLimitUntil *time.Time `json:"rate_limit_until,omitempty"`
	APICalls       int        `json:"api_calls"`
	// ETASeconds estimates the rest of the step from the last five minutes; nil when unknown.
	ETASeconds *int      `json:"eta_seconds,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Job is what the API shows of an import; it never holds the key.
type Job struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Source        string          `json:"source"`
	Domain        string          `json:"domain"`
	AccountSource *string         `json:"account_source"`
	UndoOf        *string         `json:"undo_of"`
	State         string          `json:"state"`
	Phase         string          `json:"phase"`
	Options       Options         `json:"options"`
	Preview       json.RawMessage `json:"preview"`
	Progress      *Progress       `json:"progress"`
	Status        *string         `json:"status"`
	ErrorCode     *string         `json:"error_code"`
	ErrorMessage  *string         `json:"error_message"`
	HasKey        bool            `json:"has_key"`
	// HasInterimReport is true once the recent pass ended.
	HasInterimReport bool       `json:"has_interim_report"`
	HasReport        bool       `json:"has_report"`
	CancelRequested  bool       `json:"cancel_requested"`
	NextRunAt        *time.Time `json:"next_run_at"`
	CreatedBy        *string    `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Keys keeps the Freshdesk API keys of jobs: sealed with the server's key ring (MAILBOX_TOKEN_KEYS)
// when it has one, otherwise only in this process's memory, so a key is never stored in clear.
// One Keys is shared by the API and the worker of a process.
type Keys struct {
	ring     tokenseal.Keys
	instance string
	mu       sync.Mutex
	memory   map[string]string
}

// NewKeys returns the key store of this process.
func NewKeys(ring tokenseal.Keys) *Keys {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	return &Keys{ring: ring, instance: hex.EncodeToString(raw), memory: map[string]string{}}
}

// Sealed reports whether keys survive a restart.
func (k *Keys) Sealed() bool { return k.ring.Configured() }

// additionalData binds a sealed key to its workspace and job: copied to another row it does not open.
func additionalData(workspaceID, jobID string) string {
	return "freshdesk-import|" + workspaceID + "|" + jobID
}

// seal returns the sealed key and its key id, or (nil, nil) without a ring: the caller then keeps the
// key in memory once the job row holds it, and never before, so a refused call cannot replace the key
// of a job.
func (k *Keys) seal(workspaceID, jobID, apiKey string) ([]byte, *string, error) {
	if !k.ring.Configured() {
		return nil, nil, nil
	}
	id, sealed, err := k.ring.Seal([]byte(apiKey), additionalData(workspaceID, jobID))
	if err != nil {
		return nil, nil, err
	}
	return sealed, &id, nil
}

func (k *Keys) open(workspaceID, jobID string, sealed []byte, keyID *string) (string, error) {
	if sealed != nil && keyID != nil {
		plain, err := k.ring.Open(*keyID, sealed, additionalData(workspaceID, jobID))
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if key, ok := k.memory[jobID]; ok {
		return key, nil
	}
	return "", errKeyLost
}

// keep holds the key of a job in memory when the server has no ring.
func (k *Keys) keep(jobID, apiKey string) {
	if k.ring.Configured() {
		return
	}
	k.mu.Lock()
	k.memory[jobID] = apiKey
	k.mu.Unlock()
}

// held reports whether this process keeps the job's key in memory.
func (k *Keys) held(jobID string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, ok := k.memory[jobID]
	return ok
}

// forget drops the in-memory copy of a job's key.
func (k *Keys) forget(jobIDs ...string) {
	k.mu.Lock()
	for _, id := range jobIDs {
		delete(k.memory, id)
	}
	k.mu.Unlock()
}
