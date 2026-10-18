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
	"strings"
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

type redirectGuardKey struct{}

// withRedirectGuard makes a download refuse any redirect to a URL the predicate does not allow.
func withRedirectGuard(ctx context.Context, allowed func(*url.URL) bool) context.Context {
	return context.WithValue(ctx, redirectGuardKey{}, allowed)
}

var errBlockedAddress = errors.New("refusing to connect to a private, loopback or link-local address")

// reservedNetworks are special-purpose ranges that net.IP methods do not
// cover: CGNAT, benchmarking, IETF protocol assignments, the former class E,
// NAT64 (which can embed any IPv4 address, including private ones), 6to4,
// Teredo and documentation prefixes.
var reservedNetworks = func() []*net.IPNet {
	var networks []*net.IPNet
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32", "2001:db8::/32"} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}()

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
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, network := range reservedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
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
