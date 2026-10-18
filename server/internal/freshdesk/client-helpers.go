package freshdesk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// statusError classifies a failed response. A download refused with 400, 403
// or 410 is treated as an expired pre-signed URL, which the caller can refresh.
func statusError(status int, target string, download bool) error {
	switch {
	case status == http.StatusNotFound:
		return fmt.Errorf("GET %s: %w", redact(target), errNotFound)
	case download && status == http.StatusGone:
		return fmt.Errorf("GET %s: HTTP 410: %w: %w", redact(target), errGone, errURLExpired)
	case download && (status == http.StatusForbidden || status == http.StatusBadRequest):
		return fmt.Errorf("GET %s: HTTP %d: %w", redact(target), status, errURLExpired)
	case status == http.StatusUnauthorized:
		return fmt.Errorf("GET %s: HTTP 401: Freshdesk rejected the API key", redact(target))
	case status == http.StatusForbidden:
		return fmt.Errorf("GET %s: HTTP 403: %w", redact(target), errForbidden)
	default:
		return fmt.Errorf("GET %s: HTTP %d", redact(target), status)
	}
}

func networkReason(err error) string {
	if strings.Contains(err.Error(), "Client.Timeout") || strings.Contains(err.Error(), "timeout") {
		return "timeout"
	}
	return "connection failed"
}

// redact drops the query string, which holds signatures on pre-signed URLs.
func redact(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return "<invalid URL>"
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultRetryAfter
}

func backoff(attempt int) time.Duration {
	return time.Duration(1<<attempt) * time.Second
}

func drain(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
