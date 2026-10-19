// Package importkit holds what every importer (Freshdesk, Help Scout) must do the same way: the
// egress guard, the workspace import lock, the trial extension, the storage cap error and the
// handling of images inside message HTML.
package importkit

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

// ResponseHeaderTimeout bounds the wait for response headers of one request.
const ResponseHeaderTimeout = 60 * time.Second

// NewTransport dials only public addresses, so a URL or redirect from the source system can never
// reach the host's own network or metadata service. rootCAs and allowDial are test seams: nil and
// empty in every release build.
func NewTransport(rootCAs *x509.CertPool, allowDial map[string]bool) *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: netguard.Control(allowDial)}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootCAs},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: ResponseHeaderTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   8,
		ForceAttemptHTTP2:     true,
	}
}
