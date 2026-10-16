package freshdesk

import (
	"context"
	"crypto/x509"
	"io"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

// Options configures one import run.
type Options struct {
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
	// Storage receives attachment bytes; not needed for a dry run.
	Storage attachments.Storage
	// Sleep waits between rate-limited requests; tests replace it.
	Sleep func(context.Context, time.Duration) error
	// MaxTicketPages is the list-endpoint page cap (Freshdesk: 300 pages).
	MaxTicketPages int
	// RateLimitPause is the wait after a response reports zero remaining calls.
	RateLimitPause time.Duration

	// Cloud is true on Colliedesk Cloud, the only place a trial exists. A self-hosted run still
	// moves the stored end (nothing reads it) but reports nothing about it.
	Cloud bool
	// PaidStorageBytes replaces the attachment cap of every plan that counts imports when above zero; tests
	// use a small one.
	PaidStorageBytes int64
	// Now is the clock of the trial extension; nil means time.Now.
	Now func() time.Time

	// Progress receives one-line progress messages (the CLI passes stderr); nil is silent.
	Progress io.Writer
	// ProgressInterval is the minimum time between counter lines; default 30 s.
	ProgressInterval time.Duration

	hooks testHooks
}

// testHooks are set only by tests (export_test.go).
type testHooks struct {
	rootCAs                 *x509.CertPool
	allowDial               map[string]bool
	apiTimeout, idleTimeout time.Duration
	probeAhead              int
}
