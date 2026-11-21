// Package hcanalytics sends each help center page view a reader was served to the workspace's own
// Google Analytics 4 property or Plausible site, from the server, so the public pages carry no
// analytics script and their Content-Security-Policy stays the same
// (docs/internals/knowledge-base.md › Analytics).
package hcanalytics

import (
	"context"
	"time"
)

// The services a workspace can send its page views to.
const (
	GA4       = "ga4"
	Plausible = "plausible"
)

// DefaultPlausibleBase is Plausible's hosted service, used when a workspace names no base URL.
const DefaultPlausibleBase = "https://plausible.io"

// Target is where one workspace's page views go, with its secret opened.
type Target struct {
	WorkspaceID   string
	Provider      string
	MeasurementID string
	APISecret     string
	Domain        string
	BaseURL       string
	Enabled       bool
	// Version names the saved settings (their update time in microseconds), so a result of a send
	// to older settings never lands on newer ones.
	Version int64
	// Problem, when set, says why the store could not read this target, and the rest is empty.
	Problem string
}

// Result is what the sender learned about one workspace's sends since the last flush: the time of
// the last send a service accepted, or the last error and when it happened, for the settings of
// Version.
type Result struct {
	WorkspaceID string
	Version     int64
	SentAt      time.Time
	Error       string
	ErrorAt     time.Time
}

// Store reads the targets and records the results. HelpCenterAnalyticsTargets returns every
// enabled target of every workspace; HelpCenterAnalyticsTarget returns one workspace's saved target,
// enabled or not, or nil when it has none. Either may return a target with only its Problem set.
// RecordHelpCenterAnalyticsResults applies a result only while its Version is still the saved one.
type Store interface {
	HelpCenterAnalyticsTargets(ctx context.Context) ([]Target, error)
	HelpCenterAnalyticsTarget(ctx context.Context, workspaceID string) (*Target, error)
	RecordHelpCenterAnalyticsResults(ctx context.Context, results []Result) error
}

// Options tune a Sender; the zero value is the production setting.
type Options struct {
	Now func() time.Time
	// NewKey makes the key a GA4 client id is keyed with; a new one is made each UTC day.
	NewKey func() []byte
	// Origin is the app's public address, put before a page address that is only a path.
	Origin string
	// GA4Base is the Measurement Protocol host, https://www.google-analytics.com by default.
	GA4Base string
	// AllowDial lists "host:port" addresses that pass netguard; tests use it for a local stub.
	AllowDial map[string]bool
	// Timeout bounds one request, 5 seconds by default.
	Timeout time.Duration
	// RetryDelays are the waits before each retry of a failed send, 10 and 30 seconds by default; an
	// empty, non-nil list sends once.
	RetryDelays []time.Duration
	// QueueSize is how many events wait to be sent, 10,000 by default; a full queue drops the oldest.
	QueueSize int
	// Senders is how many sends run at once, 4 by default.
	Senders int
	// FlushEvery is how often Run records the results and reloads the targets, 30 seconds by default.
	FlushEvery time.Duration
	// PauseAfter failed sends in a row pause a workspace for PauseFor (5 and 5 minutes by default), so
	// one service that hangs never holds the senders every workspace shares.
	PauseAfter int
	PauseFor   time.Duration
}

func (o Options) withDefaults() Options {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.NewKey == nil {
		o.NewKey = randomKey
	}
	if o.GA4Base == "" {
		o.GA4Base = "https://www.google-analytics.com"
	}
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.RetryDelays == nil {
		o.RetryDelays = []time.Duration{10 * time.Second, 30 * time.Second}
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 10000
	}
	if o.Senders <= 0 {
		o.Senders = 4
	}
	if o.FlushEvery <= 0 {
		o.FlushEvery = 30 * time.Second
	}
	if o.PauseAfter <= 0 {
		o.PauseAfter = 5
	}
	if o.PauseFor <= 0 {
		o.PauseFor = 5 * time.Minute
	}
	return o
}
