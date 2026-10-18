package freshdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	errNotFound   = errors.New("not found in Freshdesk")
	errForbidden  = errors.New("forbidden by Freshdesk")
	errURLExpired = errors.New("attachment URL expired")
	linkNext      = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)
)

const (
	maxThrottleRetries = 30
	maxServerRetries   = 4
	defaultRetryAfter  = 60 * time.Second
)

// client talks to the Freshdesk API v2. It honours X-Ratelimit-Remaining and
// 429 Retry-After, retries 5xx and network errors, sends the API key only over
// HTTPS to the account host, and never puts the key in an error or log line.
// It is safe for concurrent use.
type client struct {
	base   *url.URL
	apiKey string
	api    *http.Client
	files  *http.Client
	idle   time.Duration
	sleep  func(context.Context, time.Duration) error
	pause  time.Duration

	progress *progress

	mu    sync.Mutex
	stats RateLimitStats
	// exhausted is set when the last response reported no remaining calls.
	exhausted bool
}

func newClient(base *url.URL, apiKey string, options Options) *client {
	api, files := newHTTPClients(base, options.hooks)
	idle := options.hooks.idleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	return &client{base: base, apiKey: apiKey, api: api, files: files, idle: idle, sleep: options.Sleep, pause: options.RateLimitPause}
}

func (c *client) wait(ctx context.Context, d time.Duration) error {
	if err := c.sleep(ctx, d); err != nil {
		return err
	}
	return ctx.Err()
}

func (c *client) onAccountHost(target *url.URL) bool {
	return target.Scheme == "https" && target.Host == c.base.Host
}

// getJSON decodes the response of a GET into out and returns the next page URL.
func (c *client) getJSON(ctx context.Context, target string, out any) (string, error) {
	response, err := c.do(ctx, target, true)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return "", fmt.Errorf("decode %s: %w", redact(target), err)
	}
	match := linkNext.FindStringSubmatch(response.Header.Get("Link"))
	if match == nil {
		return "", nil
	}
	next, err := url.Parse(match[1])
	if err != nil || !c.onAccountHost(next) {
		return "", fmt.Errorf("Freshdesk returned a next page link to another host (%s); stopping", redact(match[1]))
	}
	return next.String(), nil
}

// listAll walks every page of a list endpoint.
func listAll[T any](ctx context.Context, c *client, target string, each func(T) error) error {
	for target != "" {
		var page []T
		next, err := c.getJSON(ctx, target, &page)
		if err != nil {
			return err
		}
		for _, item := range page {
			if err := each(item); err != nil {
				return err
			}
		}
		target = next
	}
	return nil
}

// download opens an attachment URL. The body fails when no bytes arrive for
// the idle timeout; the caller must close it. It also returns the announced
// length (-1 when unknown) and the Content-Type header.
func (c *client) download(ctx context.Context, target string) (io.ReadCloser, int64, string, error) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, 0, "", errors.New("attachment URL is not an HTTPS URL")
	}
	downloadCtx, cancel := context.WithCancel(ctx)
	response, err := c.do(downloadCtx, target, false)
	if err != nil {
		cancel()
		return nil, 0, "", err
	}
	return newIdleBody(response.Body, cancel, c.idle), response.ContentLength, response.Header.Get("Content-Type"), nil
}

func (c *client) do(ctx context.Context, target string, apiCall bool) (*http.Response, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	withKey := c.onAccountHost(parsed)
	if apiCall && !withKey {
		return nil, fmt.Errorf("refusing to send the API key to %s", redact(target))
	}
	httpClient := c.files
	if apiCall {
		httpClient = c.api
	}
	serverRetries, throttleRetries := 0, 0
	for {
		if err := c.respectExhausted(ctx, withKey); err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("build request for %s: %w", redact(target), err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "Colliedesk-Importer/0.2")
		if withKey {
			request.SetBasicAuth(c.apiKey, "X")
		}
		response, err := httpClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, errBlockedAddress) {
				return nil, fmt.Errorf("GET %s: %w", redact(target), errBlockedAddress)
			}
			if strings.Contains(err.Error(), "refusing redirect") {
				return nil, fmt.Errorf("GET %s: redirect refused (not HTTPS, off the account host, or too many)", redact(target))
			}
			if serverRetries < maxServerRetries {
				serverRetries++
				if err := c.wait(ctx, backoff(serverRetries)); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("GET %s: network error after %d retries (%s)", redact(target), serverRetries, networkReason(err))
		}
		if withKey {
			c.noteRemaining(response.Header.Get("X-Ratelimit-Remaining"))
		}
		switch {
		case response.StatusCode >= 200 && response.StatusCode < 300:
			return response, nil
		case response.StatusCode == http.StatusTooManyRequests && throttleRetries < maxThrottleRetries:
			throttleRetries++
			drain(response)
			c.setExhausted(false)
			delay := retryAfter(response.Header.Get("Retry-After"))
			c.noteWait(delay, true)
			if err := c.wait(ctx, delay); err != nil {
				return nil, err
			}
		case response.StatusCode >= 500 && serverRetries < maxServerRetries:
			serverRetries++
			drain(response)
			if err := c.wait(ctx, backoff(serverRetries)); err != nil {
				return nil, err
			}
		default:
			drain(response)
			return nil, statusError(response.StatusCode, target, !apiCall)
		}
	}
}

func (c *client) respectExhausted(ctx context.Context, withKey bool) error {
	c.mu.Lock()
	exhausted := c.exhausted && withKey
	c.exhausted = c.exhausted && !withKey
	c.mu.Unlock()
	if exhausted {
		c.noteWait(c.pause, false)
		return c.wait(ctx, c.pause)
	}
	return nil
}

// noteWait counts a rate-limit wait and announces it before the client sleeps.
func (c *client) noteWait(d time.Duration, throttled bool) {
	cause := "X-Ratelimit-Remaining 0"
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

func (c *client) noteRemaining(header string) {
	if remaining, err := strconv.Atoi(header); err == nil && remaining <= 0 {
		c.setExhausted(true)
	}
}

func (c *client) setExhausted(value bool) {
	c.mu.Lock()
	c.exhausted = value
	c.mu.Unlock()
}
