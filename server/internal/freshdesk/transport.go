package freshdesk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/importkit"
)

const (
	defaultAPITimeout  = importkit.ResponseHeaderTimeout
	defaultIdleTimeout = 2 * time.Minute
	maxRedirects       = 5
)

type redirectGuardKey struct{}

// withRedirectGuard makes a download refuse any redirect to a URL the predicate does not allow.
func withRedirectGuard(ctx context.Context, allowed func(*url.URL) bool) context.Context {
	return context.WithValue(ctx, redirectGuardKey{}, allowed)
}

var errBlockedAddress = importkit.ErrBlockedAddress

func newTransport(hooks testHooks) *http.Transport {
	return importkit.NewTransport(hooks.rootCAs, hooks.allowDial)
}

// newHTTPClients returns the API client (short timeout, no redirect off the
// account host) and the download client (HTTPS-only redirects, idle deadline).
func newHTTPClients(base *url.URL, hooks testHooks) (*http.Client, *http.Client) {
	timeout := hooks.apiTimeout
	if timeout <= 0 {
		timeout = defaultAPITimeout
	}
	transport := newTransport(hooks)
	api := &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, base.Host) || len(via) >= maxRedirects {
			return fmt.Errorf("refusing redirect to %s", redact(req.URL.String()))
		}
		return nil
	}}
	files := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= maxRedirects {
			return fmt.Errorf("refusing redirect to %s", redact(req.URL.String()))
		}
		if allowed, ok := req.Context().Value(redirectGuardKey{}).(func(*url.URL) bool); ok && !allowed(req.URL) {
			return fmt.Errorf("refusing redirect to %s", redact(req.URL.String()))
		}
		return nil
	}}
	return api, files
}

// idleBody cancels a download when no bytes arrive for the idle duration.
type idleBody struct {
	body     io.ReadCloser
	cancel   context.CancelFunc
	timer    *time.Timer
	idle     time.Duration
	timedOut atomic.Bool
	once     sync.Once
}

func newIdleBody(body io.ReadCloser, cancel context.CancelFunc, idle time.Duration) *idleBody {
	b := &idleBody{body: body, cancel: cancel, idle: idle}
	b.timer = time.AfterFunc(idle, func() {
		b.timedOut.Store(true)
		cancel()
	})
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	if err != nil && err != io.EOF && b.timedOut.Load() {
		return n, fmt.Errorf("download stalled: no data for %s", b.idle)
	}
	return n, err
}

func (b *idleBody) Close() error {
	var err error
	b.once.Do(func() {
		b.timer.Stop()
		err = b.body.Close()
		b.cancel()
	})
	return err
}
