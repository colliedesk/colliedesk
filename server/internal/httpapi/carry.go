package httpapi

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
)

// Carry is what a process keeps while its configuration is applied again (docs/internals/operator-console.md
// › Re-serve): a new API is built for the new configuration, and these go on from the old one, so a change
// never resets a rate limit, breaks an export link, loses a video upload's place or forgets an alert, and
// shutdown waits for every GitHub send of every configuration. One Carry serves every API of a process.
type Carry struct {
	exports                             *exportLimiter
	exportLinks                         *exportLinks
	webForms, webFormClients            *webFormLimiter
	domainChecks, providerChecks        *webFormLimiter
	cursorCalls, problemReports         *webFormLimiter
	testMessages                        *webFormLimiter
	helpCenterPages, helpCenterSearches *webFormLimiter
	helpCenterHostLookups               *webFormLimiter
	certificateNotes                    *webFormLimiter
	mailImageLimits                     *mailImageLimiter
	liveChecks                          *helpcenter.LiveChecks
	alerted                             *alertedSubscriptions
	kbUploads                           *kbUploadGate
	kbUploadStart                       *sync.Mutex
	dispatches                          *sync.WaitGroup
	founding                            *foundingState
	// Hot holds the settings read at each use; Store applies a change at once.
	Hot *Hot
}

// NewCarry makes the state a process's APIs share, sized by config as New would.
func NewCarry(config Config) *Carry {
	clock := config.Now
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Carry{
		exports: newExportLimiter(config.MaxConcurrentExports), exportLinks: newExportLinks(),
		webForms: newWebFormLimiter(webFormMaxPerWorkspace, webFormMaxKeys), webFormClients: newWebFormLimiter(webFormMaxPerAddress, webFormMaxKeys),
		domainChecks:    newWindowLimiter(tokenDomainChecksPerHour, webFormMaxKeys, time.Hour),
		providerChecks:  newWindowLimiter(1, webFormMaxKeys, domain.ProviderCheckMinInterval),
		cursorCalls:     newWindowLimiter(cursorCallsPerMinute, webFormMaxKeys, time.Minute),
		problemReports:  newWindowLimiter(problemReportsPerHour, webFormMaxKeys, time.Hour),
		testMessages:    newWindowLimiter(testMessagesPerHour, webFormMaxKeys, time.Hour),
		helpCenterPages: newWebFormLimiter(helpCenterPagesPerAddress, webFormMaxKeys), helpCenterSearches: newWebFormLimiter(helpCenterSearchesPerAddress, webFormMaxKeys),
		helpCenterHostLookups: newWebFormLimiter(helpCenterHostLookupsPerAddress, webFormMaxKeys), certificateNotes: newWindowLimiter(1, webFormMaxKeys, time.Hour),
		mailImageLimits: newMailImageLimiter(config.MailImageConcurrency, config.MailImageWorkspaceConcurrency, config.MailImageQueueWait),
		liveChecks:      helpcenter.NewLiveChecks(helpCenterLiveChecks, helpCenterLiveCheckBackoff, helpCenterLiveCheckHosts, clock),
		alerted:         &alertedSubscriptions{}, kbUploads: &kbUploadGate{}, kbUploadStart: &sync.Mutex{}, dispatches: &sync.WaitGroup{},
		founding: &foundingState{},
	}
}

// WaitForAutofixSends waits for the fixes being sent to GitHub by any API of the process.
func (c *Carry) WaitForAutofixSends() { c.dispatches.Wait() }

// HotSettings are the settings an API reads at each use instead of at New, so a change applies at once.
type HotSettings struct {
	AlertEmail             string
	InternalSignups        InternalSignups
	ProblemReportWorkspace string
	KBVideoMaxBytes        int64
	MailImageCacheBytes    int64
}

// Hot holds the current HotSettings of a process.
type Hot struct{ current atomic.Pointer[HotSettings] }

// NewHot holds settings.
func NewHot(settings HotSettings) *Hot {
	hot := &Hot{}
	hot.Store(settings)
	return hot
}

// Store applies settings to every request that starts after it.
func (h *Hot) Store(settings HotSettings) { h.current.Store(&settings) }

// Load returns the current settings.
func (h *Hot) Load() HotSettings { return *h.current.Load() }

// hot is what the API reads at each use: the process's Hot when there is one, else the Config it was
// built with (tests, the CLI).
func (a *API) hot() HotSettings {
	if a.config.Carry != nil && a.config.Carry.Hot != nil {
		return a.config.Carry.Hot.Load()
	}
	return HotSettings{AlertEmail: a.config.AlertEmail, InternalSignups: a.config.InternalSignups, ProblemReportWorkspace: a.config.ProblemReportWorkspace,
		KBVideoMaxBytes: a.config.KBVideoMaxBytes, MailImageCacheBytes: a.config.MailImageCacheBytes}
}
