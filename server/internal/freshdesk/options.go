package freshdesk

import (
	"context"
	"crypto/x509"
	"io"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

// Mode chooses what one run covers.
type Mode int

const (
	// ModeFull is the whole account, checked against independent totals: the only run whose report
	// can be OK. It is the zero value, so the CLI runs it.
	ModeFull Mode = iota
	// ModeRecent imports the tickets updated since Options.Since, newest first, and leaves everything
	// account-wide to a full run that follows: the knowledge base, the contact and saved-reply lists,
	// the id probe and the totals. Its report is interim, never OK. The in-app import runs it first so
	// a team can work on recent tickets while the rest arrives.
	ModeRecent
)

// Options configures one import run.
type Options struct {
	Mode          Mode
	WorkspaceSlug string
	// BaseURL is the account root, for example https://acme.freshdesk.com.
	BaseURL string
	APIKey  string
	// Since limits tickets to those updated at or after this instant.
	Since *time.Time
	// LastTicketID is the highest ticket number the operator sees in
	// Freshdesk; the id probe always covers at least this far.
	LastTicketID int64
	DryRun       bool
	// IncludePersonalCannedResponses also imports canned responses from personal folders. They become
	// shared with the whole workspace, so the default leaves them out and lists their ids.
	IncludePersonalCannedResponses bool
	// HelpCenterPortal is the Freshdesk portal whose published articles stay published, on an account
	// with several portals (CLI --help-center-portal). Zero publishes none of them there.
	HelpCenterPortal int64
	// Storage receives attachment bytes; not needed for a dry run.
	Storage attachments.Storage
	// Sleep waits between rate-limited requests; tests replace it.
	Sleep func(context.Context, time.Duration) error
	// MaxTicketPages is the list-endpoint page cap (Freshdesk: 300 pages).
	MaxTicketPages int
	// RateLimitPause is the wait after a response reports zero remaining calls.
	RateLimitPause time.Duration
	// ReserveRateLimit leaves part of the account's API limit to the customer's other integrations:
	// the run pauses once X-Ratelimit-Remaining falls to max(10, 20 % of X-Ratelimit-Total) instead of
	// zero. The in-app import sets it; the CLI, watched by an operator, does not.
	ReserveRateLimit bool

	// Cloud is true on Colliedesk Cloud, the only place a trial exists. A self-hosted run still
	// moves the stored end (nothing reads it) but reports nothing about it.
	Cloud bool
	// PaidStorageBytes replaces the attachment cap of every plan that counts imports when above zero; tests
	// use a small one.
	PaidStorageBytes int64
	// PlansOnSale are the plans the server sells; a storage refusal names the next one with more storage.
	PlansOnSale []string
	// Now is the clock of the trial extension; nil means time.Now.
	Now func() time.Time

	// Progress receives one-line progress messages (the CLI passes stderr); nil is silent.
	Progress io.Writer
	// ProgressInterval is the minimum time between counter lines; default 30 s.
	ProgressInterval time.Duration
	// OnProgress receives every phase start, counter and rate-limit wait, unthrottled and outside any
	// lock of the run. The id probe calls it from several goroutines at once.
	OnProgress func(ProgressEvent)

	hooks testHooks
}

// testHooks are set only by tests (export_test.go).
type testHooks struct {
	rootCAs   *x509.CertPool
	allowDial map[string]bool
	// imageHosts are hosts (host:port) the fake servers answer on; the importer treats them as Freshdesk-hosted.
	imageHosts              map[string]bool
	apiTimeout, idleTimeout time.Duration
	probeAhead              int
}
