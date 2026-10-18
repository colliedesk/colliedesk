package helpscout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Report statuses, the same as the Freshdesk importer's. A partial import is FAILED.
const (
	StatusOK     = "OK"
	StatusFailed = "FAILED"
	StatusDryRun = "DRY_RUN"
	// StatusUnverified: nothing failed, but the Help Scout totals could not confirm completeness.
	StatusUnverified = "UNVERIFIED"
)

type EntityCount struct {
	Source   int `json:"source"`
	Imported int `json:"imported"`
	Failed   int `json:"failed"`
}

// ConversationCount lists, by Help Scout conversation id, what was deliberately not imported.
type ConversationCount struct {
	EntityCount
	Spam            []string `json:"spam"`
	DeletedAtSource []string `json:"deleted_at_source"`
	Drafts          []string `json:"drafts"`
}

// MessageCount counts threads that became timeline messages. Draft threads (never sent) and line
// items (Help Scout's record of a status or assignee change) are not messages.
type MessageCount struct {
	EntityCount
	DraftThreads []string `json:"draft_threads_not_imported"`
	LineItems    int      `json:"line_items_not_imported"`
}

type AttachmentCount struct {
	EntityCount
	ImportedBytes      int64    `json:"imported_bytes"`
	ChecksumVerified   int      `json:"checksum_verified"`
	ChecksumMismatches []string `json:"checksum_mismatches"`
	// Virus lists attachments Help Scout marked as a virus; they are not downloaded.
	Virus []string `json:"virus_not_imported"`
}

// InlineImageCount counts images in thread HTML that Help Scout hosts. Gone images (404/410) are in
// Source but are not failures: a rerun cannot bring them back.
type InlineImageCount struct {
	EntityCount
	Gone               int         `json:"gone"`
	GoneImages         []GoneImage `json:"gone_images"`
	ImportedBytes      int64       `json:"imported_bytes"`
	ChecksumVerified   int         `json:"checksum_verified"`
	ChecksumMismatches []string    `json:"checksum_mismatches"`
}

type GoneImage struct {
	ConversationID string `json:"conversation_id"`
	ThreadID       string `json:"thread_id"`
	Detail         string `json:"detail"`
}

type AgentCount struct {
	Source    int      `json:"source"`
	Matched   int      `json:"matched"`
	Unmatched []string `json:"unmatched"`
}

type Verification struct {
	Verified bool   `json:"verified"`
	Method   string `json:"method"`
	Detail   string `json:"detail"`
}

type Verifications struct {
	Conversations Verification `json:"conversations"`
}

// RateLimitStats counts the waits the importer made for Help Scout's rate limit and the access
// tokens it had to request again.
type RateLimitStats struct {
	Pauses         int     `json:"pauses_remaining_zero"`
	Throttled      int     `json:"http_429"`
	WaitSeconds    float64 `json:"wait_seconds"`
	TokenRefreshes int     `json:"token_refreshes"`
}

type Failure struct {
	Entity         string `json:"entity"`
	HelpScoutID    string `json:"helpscout_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Error          string `json:"error"`
}

type Warning struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	HelpScoutIDs []string `json:"helpscout_ids"`
}

type Mailbox struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// StorageCapCheck compares the attachment bytes an import needs with the room under the plan's cap.
type StorageCapCheck struct {
	Plan        string `json:"plan"`
	LimitBytes  int64  `json:"limit_bytes"`
	UsedBytes   int64  `json:"used_bytes"`
	NeededBytes int64  `json:"needed_bytes"`
	FreeBytes   int64  `json:"free_bytes"`
	WouldRefuse bool   `json:"would_refuse"`
}

// Report is the reconcile report of one run: Help Scout counts against what Colliedesk holds.
type Report struct {
	Status              string            `json:"status"`
	Source              string            `json:"source"`
	Mailbox             Mailbox           `json:"mailbox"`
	Workspace           string            `json:"workspace"`
	DryRun              bool              `json:"dry_run"`
	Since               *time.Time        `json:"since"`
	StartedAt           time.Time         `json:"started_at"`
	FinishedAt          time.Time         `json:"finished_at"`
	Contacts            EntityCount       `json:"contacts"`
	Agents              AgentCount        `json:"agents"`
	Conversations       ConversationCount `json:"conversations"`
	Messages            MessageCount      `json:"messages"`
	Attachments         AttachmentCount   `json:"attachments"`
	InlineImages        InlineImageCount  `json:"inline_images"`
	Tags                EntityCount       `json:"tags"`
	Verification        Verifications     `json:"verification"`
	RateLimit           RateLimitStats    `json:"rate_limit"`
	Missing             []string          `json:"missing"`
	Failures            []Failure         `json:"failures"`
	Warnings            []Warning         `json:"warnings"`
	NotImported         []string          `json:"not_imported"`
	TrialExtendedTo     *time.Time        `json:"trial_extended_to,omitempty"`
	TrialExtensionError string            `json:"trial_extension_error,omitempty"`
	StorageCap          *StorageCapCheck  `json:"storage_cap,omitempty"`
}

func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// WriteFiles writes <prefix>.json and <prefix>.md into dir.
func (r *Report) WriteFiles(dir string) (string, string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", fmt.Errorf("create report directory: %w", err)
	}
	prefix := filepath.Join(dir, fmt.Sprintf("helpscout-import-%s-%s", r.Workspace, r.StartedAt.UTC().Format("20060102T150405Z")))
	data, err := r.JSON()
	if err != nil {
		return "", "", fmt.Errorf("encode report: %w", err)
	}
	if err := os.WriteFile(prefix+".json", append(data, '\n'), 0o640); err != nil {
		return "", "", fmt.Errorf("write report: %w", err)
	}
	if err := os.WriteFile(prefix+".md", []byte(r.Markdown()), 0o640); err != nil {
		return "", "", fmt.Errorf("write report: %w", err)
	}
	return prefix + ".json", prefix + ".md", nil
}
