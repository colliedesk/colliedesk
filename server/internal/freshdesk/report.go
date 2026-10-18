package freshdesk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Report statuses. A partial import is FAILED: every missing item is listed.
const (
	StatusOK     = "OK"
	StatusFailed = "FAILED"
	StatusDryRun = "DRY_RUN"
	// StatusUnverified: nothing failed, but the expected totals could not be
	// confirmed from an independent source, so completeness is not proven.
	StatusUnverified = "UNVERIFIED"
)

type EntityCount struct {
	Source   int `json:"source"`
	Imported int `json:"imported"`
	Failed   int `json:"failed"`
}

type TicketCount struct {
	EntityCount
	DeletedAtSource []string `json:"deleted_at_source"`
	Trashed         []string `json:"trashed"`
	Spam            []string `json:"spam"`
	AbsentIDs       int      `json:"absent_ids"`
	Archived        int      `json:"archived"`
	HighestID       int64    `json:"highest_existing_id"`
	HighestProbedID int64    `json:"highest_probed_id"`
	ProbedIDs       int      `json:"probed_ids"`
	// UnresolvedIDs are ids below the highest existing ticket that answered 404 while the
	// archive endpoint was refused: each may be deleted or archived, so the customer must
	// check them in Freshdesk. Empty when the archive could be checked.
	UnresolvedIDs []string `json:"unresolved_ids"`
}

// RateLimitStats counts the waits the importer made for Freshdesk rate limits.
type RateLimitStats struct {
	// Pauses are one-minute waits after a response with X-Ratelimit-Remaining 0.
	Pauses int `json:"pauses_remaining_zero"`
	// Throttled counts HTTP 429 responses.
	Throttled   int     `json:"http_429"`
	WaitSeconds float64 `json:"wait_seconds"`
}

// Verification says how an expected total was established independently of
// the list endpoints the importer reads.
type Verification struct {
	Verified bool   `json:"verified"`
	Method   string `json:"method"`
	Detail   string `json:"detail"`
}

type Verifications struct {
	Tickets      Verification `json:"tickets"`
	Contacts     Verification `json:"contacts"`
	SavedReplies Verification `json:"saved_replies"`
}

// SavedReplyCount counts canned responses. A response with no text is not counted in Source: it is
// listed in EmptySkipped, so the counts stay comparable.
type SavedReplyCount struct {
	EntityCount
	Folders int `json:"folders"`
	// NotAvailable is true when the Freshdesk account has no canned responses to read (the folder
	// list answered 403 or 404). That is a fact about the account, not a failure of the import.
	NotAvailable bool     `json:"not_available"`
	EmptySkipped []string `json:"empty_skipped"`
	// PersonalNotImported are canned responses in personal folders, left out unless asked for.
	PersonalNotImported []string `json:"personal_not_imported"`
	// AttachmentsNotImported counts files on canned responses; saved replies carry none.
	AttachmentsNotImported   int      `json:"attachments_not_imported"`
	ResponsesWithAttachments []string `json:"responses_with_attachments"`
	// UnmappedPlaceholders maps a Freshdesk placeholder with no Colliedesk equivalent to the canned
	// responses holding it; it stays in the text as written.
	UnmappedPlaceholders map[string][]string `json:"unmapped_placeholders"`
}

type AttachmentCount struct {
	EntityCount
	SourceBytes        int64    `json:"source_bytes"`
	ImportedBytes      int64    `json:"imported_bytes"`
	ChecksumVerified   int      `json:"checksum_verified"`
	ChecksumMismatches []string `json:"checksum_mismatches"`
}

// InlineImageCount counts images in message HTML that Freshdesk hosts. Their size is unknown until they
// are downloaded, so there is no source byte total; Source is the number of distinct images found.
type InlineImageCount struct {
	EntityCount
	ImportedBytes      int64    `json:"imported_bytes"`
	ChecksumVerified   int      `json:"checksum_verified"`
	ChecksumMismatches []string `json:"checksum_mismatches"`
}

type CompanyCount struct {
	Source     int      `json:"source"`
	Attached   int      `json:"attached_to_contacts"`
	Unattached []string `json:"unattached"`
}

type AgentCount struct {
	Source    int      `json:"source"`
	Matched   int      `json:"matched"`
	Unmatched []string `json:"unmatched"`
}

type Failure struct {
	Entity      string `json:"entity"`
	FreshdeskID string `json:"freshdesk_id"`
	TicketID    string `json:"ticket_id,omitempty"`
	Error       string `json:"error"`
}

type Warning struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	FreshdeskIDs []string `json:"freshdesk_ids"`
}

// Report is the reconcile report of one import run: source counts versus
// what Colliedesk holds, so a customer can prove nothing was lost.
type Report struct {
	Status       string           `json:"status"`
	Domain       string           `json:"domain"`
	Workspace    string           `json:"workspace"`
	DryRun       bool             `json:"dry_run"`
	Since        *time.Time       `json:"since"`
	StartedAt    time.Time        `json:"started_at"`
	FinishedAt   time.Time        `json:"finished_at"`
	Contacts     EntityCount      `json:"contacts"`
	Companies    CompanyCount     `json:"companies"`
	Agents       AgentCount       `json:"agents"`
	Tickets      TicketCount      `json:"tickets"`
	Messages     EntityCount      `json:"messages"`
	Attachments  AttachmentCount  `json:"attachments"`
	InlineImages InlineImageCount `json:"inline_images"`
	TimeEntries  EntityCount      `json:"time_entries"`
	SavedReplies SavedReplyCount  `json:"saved_replies"`
	Verification Verifications    `json:"verification"`
	RateLimit    RateLimitStats   `json:"rate_limit"`
	Missing      []string         `json:"missing"`
	Failures     []Failure        `json:"failures"`
	Warnings     []Warning        `json:"warnings"`
	NotImported  []string         `json:"not_imported"`
	// TrialExtendedTo is the new trial end when this run extended the workspace's trial.
	TrialExtendedTo *time.Time `json:"trial_extended_to,omitempty"`
	// TrialExtensionError is set when the import finished but the trial could not be extended;
	// running the same import again retries it.
	TrialExtensionError string `json:"trial_extension_error,omitempty"`
	// StorageCap is set by a dry run on a workspace whose cap counts imports: whether the import would be refused.
	StorageCap *StorageCapCheck `json:"storage_cap,omitempty"`
}

// StorageCapCheck compares the attachment bytes an import needs with the room the workspace has under its plan.
type StorageCapCheck struct {
	// Plan is the plan whose cap is checked: founding, team or team25.
	Plan        string `json:"plan"`
	LimitBytes  int64  `json:"limit_bytes"`
	UsedBytes   int64  `json:"used_bytes"`
	NeededBytes int64  `json:"needed_bytes"`
	FreeBytes   int64  `json:"free_bytes"`
	WouldRefuse bool   `json:"would_refuse"`
	// SkippedTickets are tickets the sizing could not read (deleted or forbidden) and so did not count.
	SkippedTickets []string `json:"skipped_tickets,omitempty"`
}

// notImported lists Freshdesk data this importer does not carry over, so the
// report never implies more than it delivers.
var notImported = []string{
	"Custom ticket fields and custom contact fields",
	"Ticket group, product, company link, email_config_id, escalation flags, watchers, parent/child and merge links",
	"Contact phone numbers, other emails, addresses, job titles and social handles",
	"Companies other than their name (domains, notes, custom fields)",
	"Message structured_body, support_email, notified_to and edit history (last_edited_*)",
	"Satisfaction ratings, SLA policies, automations",
	"Files attached to canned responses (saved replies hold text only; the report lists the canned responses that had files)",
	"Solution (knowledge base) articles",
	"Images in message HTML that are not hosted by Freshdesk (other sites, tracking pixels) stay links; the original HTML is kept",
	"Tickets in Freshdesk Trash or Spam (listed by id in this report)",
	"Time entries of archived tickets (the Freshdesk API documents no endpoint for them)",
	"A re-run takes subject, status, priority and assignee from Freshdesk again and only adds tags; tags removed in Freshdesk stay",
}

func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// WriteFiles writes <prefix>.json and <prefix>.md into dir.
func (r *Report) WriteFiles(dir string) (string, string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", fmt.Errorf("create report directory: %w", err)
	}
	prefix := filepath.Join(dir, fmt.Sprintf("freshdesk-import-%s-%s", r.Workspace, r.StartedAt.UTC().Format("20060102T150405Z")))
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
