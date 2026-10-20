package helpscout

import (
	"context"
	"crypto/x509"
	"io"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

// DefaultBaseURL is the Help Scout Inbox API 2.0 root.
const DefaultBaseURL = "https://api.helpscout.net"

// Options configures one import run.
type Options struct {
	WorkspaceSlug string
	// BaseURL is the API root; empty means DefaultBaseURL.
	BaseURL string
	// AppID and AppSecret are the OAuth2 client credentials of a Help Scout app (My Apps).
	AppID, AppSecret string
	// MailboxID selects the Help Scout inbox to import. Zero is accepted only when the account has
	// exactly one inbox.
	MailboxID int64
	// Since limits conversations to those modified after this instant.
	Since  *time.Time
	DryRun bool
	// Storage receives attachment bytes; not needed for a dry run.
	Storage attachments.Storage
	// Sleep waits between rate-limited requests; tests replace it.
	Sleep func(context.Context, time.Duration) error

	// Cloud is true on Colliedesk Cloud, the only place a trial and a storage cap exist.
	Cloud bool
	// PaidStorageBytes replaces the attachment cap of every plan that counts imports when above zero.
	PaidStorageBytes int64
	// Now is the clock of the trial extension; nil means time.Now.
	Now func() time.Time

	// Progress receives one-line progress messages (the CLI passes stderr); nil is silent.
	Progress io.Writer

	hooks testHooks
}

// testHooks are set only by tests (export_test.go) and end-to-end builds.
type testHooks struct {
	rootCAs   *x509.CertPool
	allowDial map[string]bool
	// imageHosts are hosts (host:port) the importer treats as Help Scout image hosts.
	imageHosts map[string]bool
}
