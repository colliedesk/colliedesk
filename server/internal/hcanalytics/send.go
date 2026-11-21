package hcanalytics

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/netguard"
	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

// guardedClient dials only public addresses, refuses redirects and bounds each request.
func guardedClient(allow map[string]bool, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: netguard.Control(allow)}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: timeout,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 4,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// serviceName is how messages name a provider.
func serviceName(provider string) string {
	if provider == GA4 {
		return "GA4"
	}
	return "Plausible"
}

// deliver sends one page view and returns "" when the service accepted it, or what went wrong.
func (s *Sender) deliver(ctx context.Context, item *delivery) string {
	request, err := s.pageViewRequest(ctx, item, "/mp/collect")
	if err != nil {
		return serviceName(item.target.Provider) + " could not be reached."
	}
	response, failure := s.do(request, item.target.Provider)
	if failure != "" {
		return failure
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
	return ""
}

// do runs a request and turns a failure into a message that never carries the request's URL,
// which holds the GA4 API secret.
func (s *Sender) do(request *http.Request, provider string) (*http.Response, string) {
	name := serviceName(provider)
	response, err := s.client.Do(request)
	if err != nil {
		var timeout interface{ Timeout() bool }
		switch {
		case errors.Is(err, netguard.ErrBlockedAddress):
			return nil, fmt.Sprintf("%s is at %s, a private address; Colliedesk sends only to public addresses.", name, blockedAddress(err))
		case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
			return nil, fmt.Sprintf("%s did not answer within %s.", name, duration(s.options.Timeout))
		}
		return nil, name + " could not be reached."
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		_ = response.Body.Close()
		return nil, fmt.Sprintf("%s answered HTTP %d.", name, response.StatusCode)
	}
	return response, ""
}

// blockedAddress is the address netguard refused, read back from its error so the message names it.
func blockedAddress(err error) string {
	text := err.Error()
	marker := netguard.ErrBlockedAddress.Error() + ": "
	if at := strings.LastIndex(text, marker); at >= 0 {
		if ip := net.ParseIP(strings.TrimSpace(text[at+len(marker):])); ip != nil {
			return ip.String()
		}
	}
	return "a private address"
}

func duration(d time.Duration) string {
	if d%time.Second == 0 {
		if d == time.Second {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", d/time.Second)
	}
	return d.String()
}

// pageViewRequest builds the request for one page view; path is GA4's endpoint (/mp/collect, or
// /debug/mp/collect for a test).
func (s *Sender) pageViewRequest(ctx context.Context, item *delivery, path string) (*http.Request, error) {
	event, target := item.event, item.target
	var address string
	var body any
	if target.Provider == GA4 {
		params := map[string]any{"page_location": event.Address, "page_title": event.Title, "language": event.Locale,
			"session_id": item.session, "engagement_time_msec": 1}
		if event.Referrer != "" {
			params["page_referrer"] = event.Referrer
		}
		if event.Kind == pageview.Search && event.Search != "" {
			params["search_term"] = event.Search
		}
		body = map[string]any{"client_id": item.clientID, "timestamp_micros": event.At.UnixMicro(),
			"events": []any{map[string]any{"name": "page_view", "params": params}}}
		query := url.Values{"measurement_id": {target.MeasurementID}, "api_secret": {target.APISecret}}
		address = strings.TrimSuffix(s.options.GA4Base, "/") + path + "?" + query.Encode()
	} else {
		fields := map[string]any{"name": "pageview", "url": event.Address, "domain": target.Domain}
		if event.Referrer != "" {
			fields["referrer"] = event.Referrer
		}
		body = fields
		address = strings.TrimSuffix(target.BaseURL, "/") + "/api/event"
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if target.Provider == Plausible {
		request.Header.Set("User-Agent", event.UserAgent)
		request.Header.Set("X-Forwarded-For", event.Client)
	}
	return request, nil
}
