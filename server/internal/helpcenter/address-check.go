package helpcenter

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

// MarkerHeader is the response header by which a help center served on a custom domain says whose it
// is (Marker), so the custom domain check can tell this workspace's help center from any other server
// that answers on the name.
const MarkerHeader = "Colliedesk-Help-Center"

// maxAddressHeaderBytes bounds the response headers of an address check; its body is never read.
const maxAddressHeaderBytes = 16 << 10

// Marker is the value of MarkerHeader on the pages of a workspace's help center served on host. Only
// this server knows the workspace id, so another server answering on the name cannot send it.
func Marker(workspaceID, host string) string {
	sum := sha256.Sum256([]byte("colliedesk help center\x00" + workspaceID + "\x00" + host))
	return hex.EncodeToString(sum[:16])
}

// NewAddressTransport is the transport of the custom domain check: it dials public addresses only, so a
// name pointed at the host's own network or a metadata service is refused, checks certificates as a
// browser does, sends no proxy credentials and reads at most maxAddressHeaderBytes of headers.
func NewAddressTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: netguard.Control(nil)}
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		MaxResponseHeaderBytes: maxAddressHeaderBytes,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      true,
	}
}

// AddressError says why a help center's address did not open with its help center. Its text is for the
// workspace: what is wrong and what to do, never what the network answered, which may name addresses
// the workspace must not see (Unwrap keeps that for the server's log).
type AddressError struct {
	message string
	cause   error
}

func (e *AddressError) Error() string { return e.message }
func (e *AddressError) Unwrap() error { return e.cause }

// CheckAddress opens address, the help center's address on its custom domain host (SiteURL), once
// through transport, as a visitor would: a GET without credentials whose redirect is never followed
// and whose body is never read. It reports nil when the answer carries the workspace's Marker. Behind
// TLS, the first such request is also what gets the reverse proxy to issue the domain's certificate.
func CheckAddress(ctx context.Context, transport http.RoundTripper, address, workspaceID, host string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/", nil)
	if err != nil {
		return &AddressError{message: address + " is not an address this server can open.", cause: err}
	}
	request.Header.Set("User-Agent", "Colliedesk help center check")
	// The trace runs on the transport's dialing goroutine, which may outlive RoundTrip.
	var handshakeFailed atomic.Bool
	request = request.WithContext(httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				handshakeFailed.Store(true)
			}
		},
	}))
	response, err := transport.RoundTrip(request)
	if err != nil {
		return addressFailure(address, host, err, handshakeFailed.Load())
	}
	_ = response.Body.Close()
	if response.Header.Get(MarkerHeader) != Marker(workspaceID, host) {
		return &AddressError{message: address + " answers, but not with this help center: its DNS, or a proxy in front of it such as a CDN, " +
			"sends visitors to another server. Point " + host + " at this server only.", cause: errors.New(response.Status)}
	}
	return nil
}

// addressFailure words a request that got no answer: no answer in time, a certificate the client
// refused or a TLS handshake that failed (handshakeFailed: a proxy with no certificate for the name
// answers the handshake with an alert), or no way to reach the address.
func addressFailure(address, host string, err error, handshakeFailed bool) error {
	var netErr net.Error
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var mismatch x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout():
		return &AddressError{message: address + " did not answer in time. Check again in a minute; if it keeps failing, " +
			"make sure " + host + " points at this server.", cause: err}
	case handshakeFailed || errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &mismatch) || errors.As(err, &invalid) || errors.As(err, &record):
		return &AddressError{message: address + " has no valid certificate yet, so browsers refuse it. One is requested on the first visit: " +
			"check again in a minute. If it still fails, the reverse proxy in front of this server cannot get a certificate for " + host + ".", cause: err}
	default:
		return &AddressError{message: address + " could not be reached. Make sure " + host + " points at this server and that nothing blocks visitors.", cause: err}
	}
}
