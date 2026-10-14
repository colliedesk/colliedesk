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
	"time"
)

var (
	errNotFound   = errors.New("not found in Freshdesk")
	errURLExpired = errors.New("attachment URL expired")
	linkNext      = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)
)

const (
	maxThrottleRetries = 30
	maxServerRetries   = 4
	defaultRetryAfter  = 60 * time.Second
)

// client talks to the Freshdesk API v2. It honours X-Ratelimit-Remaining and
// 429 Retry-After, retries 5xx and network errors, and never puts the API key
// in an error, a log line or a request to a host other than the account's.
type client struct {
	base   *url.URL
	apiKey string
	http   *http.Client
	sleep  func(context.Context, time.Duration) error
	pause  time.Duration
	// exhausted is set when the last response reported no remaining calls.
	exhausted bool
}

func (c *client) wait(ctx context.Context, d time.Duration) error {
	if err := c.sleep(ctx, d); err != nil {
		return err
	}
	return ctx.Err()
}

// getJSON decodes the response of a GET into out and returns the next page URL.
func (c *client) getJSON(ctx context.Context, target string, out any) (string, error) {
	response, err := c.do(ctx, target, true, false)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return "", fmt.Errorf("decode %s: %w", redact(target), err)
	}
	next := ""
	if match := linkNext.FindStringSubmatch(response.Header.Get("Link")); match != nil {
		next = match[1]
	}
	return next, nil
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

// download opens an attachment URL. Credentials are sent only to the account host;
// pre-signed storage URLs must not receive the API key.
func (c *client) download(ctx context.Context, target string) (io.ReadCloser, int64, error) {
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, 0, fmt.Errorf("invalid attachment URL")
	}
	response, err := c.do(ctx, target, parsed.Host == c.base.Host, true)
	if err != nil {
		return nil, 0, err
	}
	return response.Body, response.ContentLength, nil
}

func (c *client) do(ctx context.Context, target string, withKey, download bool) (*http.Response, error) {
	serverRetries, throttleRetries := 0, 0
	for {
		if c.exhausted && withKey {
			if err := c.wait(ctx, c.pause); err != nil {
				return nil, err
			}
			c.exhausted = false
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("build request for %s: %w", redact(target), err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "CollieDesk-Importer/0.2")
		if withKey {
			request.SetBasicAuth(c.apiKey, "X")
		}
		response, err := c.http.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if serverRetries < maxServerRetries {
				serverRetries++
				if err := c.wait(ctx, backoff(serverRetries)); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("GET %s: network error after %d retries", redact(target), serverRetries)
		}
		if withKey {
			if remaining, err := strconv.Atoi(response.Header.Get("X-Ratelimit-Remaining")); err == nil && remaining <= 0 {
				c.exhausted = true
			}
		}
		switch {
		case response.StatusCode >= 200 && response.StatusCode < 300:
			return response, nil
		case response.StatusCode == http.StatusTooManyRequests && throttleRetries < maxThrottleRetries:
			throttleRetries++
			drain(response)
			c.exhausted = false
			if err := c.wait(ctx, retryAfter(response.Header.Get("Retry-After"))); err != nil {
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
			return nil, statusError(response.StatusCode, target, download)
		}
	}
}

// statusError classifies a failed response. A download refused with 400, 403
// or 410 is treated as an expired pre-signed URL, which the caller can refresh.
func statusError(status int, target string, download bool) error {
	switch {
	case status == http.StatusNotFound:
		return fmt.Errorf("GET %s: %w", redact(target), errNotFound)
	case download && (status == http.StatusForbidden || status == http.StatusBadRequest || status == http.StatusGone):
		return fmt.Errorf("GET %s: HTTP %d: %w", redact(target), status, errURLExpired)
	case status == http.StatusUnauthorized:
		return fmt.Errorf("GET %s: HTTP 401: Freshdesk rejected the API key", redact(target))
	default:
		return fmt.Errorf("GET %s: HTTP %d", redact(target), status)
	}
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
