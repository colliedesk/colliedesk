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
	// Storage receives attachment bytes; not needed for a dry run.
	Storage attachments.Storage
	// Sleep waits between rate-limited requests; tests replace it.
	Sleep func(context.Context, time.Duration) error
	// MaxTicketPages is the list-endpoint page cap (Freshdesk: 300 pages).
	MaxTicketPages int
	// RateLimitPause is the wait after a response reports zero remaining calls.
	RateLimitPause time.Duration

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
