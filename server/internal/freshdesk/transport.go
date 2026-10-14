package freshdesk

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultAPITimeout  = 60 * time.Second
	defaultIdleTimeout = 2 * time.Minute
	maxRedirects       = 5
)

var errBlockedAddress = errors.New("refusing to connect to a private, loopback or link-local address")

// carrierGradeNAT is 100.64.0.0/10, which net.IP.IsPrivate does not cover.
var carrierGradeNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// newTransport dials only public addresses, so a URL or redirect from the
// source system can never reach the host's own network or metadata service.
func newTransport(hooks testHooks) *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: guardAddress(hooks.allowDial)}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: hooks.rootCAs},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: defaultAPITimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   8,
		ForceAttemptHTTP2:     true,
	}
}

func guardAddress(allow map[string]bool) func(string, string, syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allow[address] {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", errBlockedAddress, address)
		}
		ip := net.ParseIP(host)
		if ip == nil || blockedIP(ip) {
			return fmt.Errorf("%w: %s", errBlockedAddress, host)
		}
		return nil
	}
}

func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		carrierGradeNAT.Contains(ip) || (ip.To4() != nil && ip.To4()[0] == 0)
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
		if req.URL.Scheme != "https" || req.URL.Host != base.Host || len(via) >= maxRedirects {
			return fmt.Errorf("refusing redirect to %s", redact(req.URL.String()))
		}
		return nil
	}}
	files := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= maxRedirects {
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
