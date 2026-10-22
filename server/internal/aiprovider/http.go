package aiprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

// maxRetryAfter caps a provider's retry-after: a base URL a workspace admin typed can answer anything.
const maxRetryAfter = time.Hour

// maxResponseBytes caps what an adapter reads from a provider. A complete answer is a few tens of
// kilobytes; anything larger is not an answer.
const maxResponseBytes = 8 << 20

// HTTPOptions builds the HTTP client an adapter uses.
type HTTPOptions struct {
	// Guarded dials only public addresses (netguard), for a base URL a workspace admin typed.
	// The server provider and an operator-allowed private URL are not guarded.
	Guarded bool
	// AllowDial and RootCAs are test seams: empty in every release build.
	AllowDial map[string]bool
	RootCAs   *x509.CertPool
}

// NewHTTPClient returns a client without a response-header timeout: a non-streaming completion
// sends its headers only when the whole answer is ready, so the request context is the only bound.
// Redirects are refused; a provider API does not redirect a POST.
func NewHTTPClient(options HTTPOptions) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if options.Guarded {
		dialer.Control = netguard.Control(options.AllowDial)
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: options.RootCAs},
		TLSHandshakeTimeout: 15 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 8,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// statusError maps an HTTP status to an Error. An adapter that can tell a billing 429 from a rate
// limit by the provider's error body corrects the code afterwards.
func statusError(status int, header http.Header, requestID string) *Error {
	err := &Error{Status: status, RequestID: requestID}
	switch {
	case status == http.StatusUnauthorized:
		err.Code = ErrorAuth
	case status == http.StatusPaymentRequired:
		err.Code = ErrorQuotaExhausted
	case status == http.StatusForbidden:
		err.Code = ErrorPermission
	case status == http.StatusNotFound:
		err.Code = ErrorModelNotFound
	case status == http.StatusRequestEntityTooLarge:
		err.Code = ErrorTooLarge
	case status == http.StatusTooManyRequests:
		err.Code = ErrorRateLimited
		err.RetryAfter = retryAfter(header)
	case status == 529:
		err.Code = ErrorOverloaded
		err.RetryAfter = retryAfter(header)
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		err.Code = ErrorTimeout
	case status >= 500:
		err.Code = ErrorUnavailable
		err.RetryAfter = retryAfter(header)
	case status >= 400:
		err.Code = ErrorBadRequest
	default:
		err.Code = ErrorInvalidResponse
	}
	return err
}

// retryAfter reads a retry-after header in seconds or as an HTTP date, at most maxRetryAfter; zero
// when absent or unreadable.
func retryAfter(header http.Header) time.Duration {
	value := header.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		switch {
		case math.IsNaN(seconds) || seconds <= 0:
			return 0
		case seconds >= maxRetryAfter.Seconds():
			return maxRetryAfter
		}
		return time.Duration(seconds * float64(time.Second))
	}
	if at, err := http.ParseTime(value); err == nil {
		if wait := time.Until(at); wait > 0 {
			return min(wait, maxRetryAfter)
		}
	}
	return 0
}

// transportError maps an error with no HTTP response: a dial netguard refused, a timeout (the
// context's own error decides) or anything else. The original error is dropped because a URL error
// can carry the URL.
func transportError(ctx context.Context, err error) *Error {
	if errors.Is(err, netguard.ErrBlockedAddress) {
		return &Error{Code: ErrorBlockedAddress}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: ErrorTimeout}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &Error{Code: ErrorTimeout}
	}
	return &Error{Code: ErrorUnavailable}
}

// parseBaseURL accepts an http or https URL without credentials, query or fragment: userinfo would
// be sent as Basic auth next to the key.
func parseBaseURL(value string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" {
		return nil, errors.New("aiprovider: the base URL must be an http or https URL")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("aiprovider: the base URL must not carry credentials, a query or a fragment")
	}
	return base, nil
}

// schemaJSON returns text when the request asked for a schema and text is a JSON object.
func schemaJSON(request Request, text string) string {
	if request.Schema == nil {
		return ""
	}
	var object map[string]any
	if json.Unmarshal([]byte(text), &object) != nil {
		return ""
	}
	return text
}

// checkSchema is the structured output a Check asks for, so a model or endpoint that cannot do
// structured output fails at Check time rather than at the first triage.
var checkSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"ok": map[string]any{"type": "boolean"}},
	"required":             []string{"ok"},
	"additionalProperties": false,
}

func checkRequest(model string) Request {
	return Request{
		Model:     model,
		Effort:    EffortLow,
		System:    "This is a connection check. Answer with ok set to true.",
		Messages:  []Message{{Role: RoleUser, Text: "Check."}},
		MaxOutput: 256,
		Schema:    checkSchema,
	}
}
