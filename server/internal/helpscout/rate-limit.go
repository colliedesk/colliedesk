package helpscout

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type statusErr struct {
	code   int
	target string
}

func (e *statusErr) Error() string { return fmt.Sprintf("%s: HTTP %d", e.target, e.code) }

func (e *statusErr) Unwrap() error {
	switch e.code {
	case http.StatusNotFound:
		return errNotFound
	case http.StatusForbidden:
		return errForbidden
	}
	return nil
}

func (c *client) noteWait(d time.Duration, throttled bool) {
	cause := "X-RateLimit-Remaining-Minute 0"
	c.mu.Lock()
	if throttled {
		c.stats.Throttled++
		cause = "HTTP 429"
	} else {
		c.stats.Pauses++
	}
	c.stats.WaitSeconds += d.Seconds()
	c.mu.Unlock()
	c.progress.note(fmt.Sprintf("rate limit: waiting %s (%s)", d, cause))
}

func (c *client) rateLimitStats() RateLimitStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// retryAfter reads Help Scout's X-RateLimit-Retry-After, then the standard Retry-After.
func retryAfter(header http.Header) time.Duration {
	for _, name := range []string{"X-RateLimit-Retry-After", "Retry-After"} {
		if seconds, err := strconv.Atoi(strings.TrimSpace(header.Get(name))); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultRetryAfter
}
