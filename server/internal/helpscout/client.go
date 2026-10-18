package helpscout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	errNotFound  = errors.New("not found in Help Scout")
	errForbidden = errors.New("forbidden by Help Scout")
	// errGone: an image host answered 404 or 410, so the file no longer exists.
	errGone = errors.New("no longer available from Help Scout")
)

const (
	maxThrottleRetries = 30
	maxServerRetries   = 4
	defaultRetryAfter  = 60 * time.Second
	apiTimeout         = 60 * time.Second
	userAgent          = "Colliedesk-Importer/0.2"
)

// client talks to the Help Scout Inbox API 2.0 with an OAuth2 client-credentials token. It asks for
// a new token when the API answers 401 (Help Scout's sign that a token expired), waits on 429 for
// X-RateLimit-Retry-After, pauses when X-RateLimit-Remaining-Minute reaches 0, retries 5xx and
// network errors, sends the token only over HTTPS to the API host, and never puts the secret or the
// token in an error. It is not safe for concurrent use.
type client struct {
	base             *url.URL
	appID, appSecret string
	api, images      *http.Client
	sleep            func(context.Context, time.Duration) error
	progress         *progress
	token            string
	pauseBeforeNext  time.Duration
	mu               sync.Mutex
	stats            RateLimitStats
}

func (c *client) wait(ctx context.Context, d time.Duration) error {
	if err := c.sleep(ctx, d); err != nil {
		return err
	}
	return ctx.Err()
}

// authenticate exchanges the app credentials for an access token.
func (c *client) authenticate(ctx context.Context) error {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.appID}, "client_secret": {c.appSecret}}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	err := c.call(ctx, http.MethodPost, c.endpoint("/v2/oauth2/token", nil), form, &token, false)
	var status *statusErr
	if errors.As(err, &status) && (status.code == http.StatusUnauthorized || status.code == http.StatusBadRequest || status.code == http.StatusForbidden) {
		return fmt.Errorf("Help Scout rejected the App ID or App Secret (HTTP %d); check them under Your Profile > My Apps", status.code)
	}
	if err != nil {
		return fmt.Errorf("get a Help Scout access token: %w", err)
	}
	if token.AccessToken == "" {
		return errors.New("the Help Scout token response has no access_token")
	}
	c.token = token.AccessToken
	return nil
}

// getJSON decodes the response of an authenticated GET into out.
func (c *client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	if c.token == "" {
		if err := c.authenticate(ctx); err != nil {
			return err
		}
	}
	err := c.call(ctx, http.MethodGet, c.endpoint(path, query), nil, out, true)
	var status *statusErr
	if !errors.As(err, &status) || status.code != http.StatusUnauthorized {
		return err
	}
	c.mu.Lock()
	c.stats.TokenRefreshes++
	c.mu.Unlock()
	c.progress.note("access token expired: requesting a new one")
	if err := c.authenticate(ctx); err != nil {
		return err
	}
	err = c.call(ctx, http.MethodGet, c.endpoint(path, query), nil, out, true)
	if errors.As(err, &status) && status.code == http.StatusUnauthorized {
		return fmt.Errorf("%w (a fresh access token was refused too)", err)
	}
	return err
}

func (c *client) endpoint(path string, query url.Values) string {
	return c.base.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()}).String()
}

// call sends one API request with retries. form makes it a POST without the bearer token.
func (c *client) call(ctx context.Context, method, target string, form url.Values, out any, withToken bool) error {
	serverRetries, throttleRetries := 0, 0
	for {
		if c.pauseBeforeNext > 0 {
			pause := c.pauseBeforeNext
			c.pauseBeforeNext = 0
			c.noteWait(pause, false)
			if err := c.wait(ctx, pause); err != nil {
				return err
			}
		}
		response, err := c.send(ctx, method, target, form, withToken)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if serverRetries < maxServerRetries {
				serverRetries++
				if err := c.wait(ctx, backoff(serverRetries)); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("%s %s: network error after %d retries", method, redact(target), serverRetries)
		}
		if response.Header.Get("X-RateLimit-Remaining-Minute") == "0" {
			c.pauseBeforeNext = retryAfter(response.Header)
		}
		switch {
		case response.StatusCode >= 200 && response.StatusCode < 300:
			defer response.Body.Close()
			if err := json.NewDecoder(io.LimitReader(response.Body, 256<<20)).Decode(out); err != nil {
				return fmt.Errorf("decode %s: %w", redact(target), err)
			}
			return nil
		case response.StatusCode == http.StatusTooManyRequests && throttleRetries < maxThrottleRetries:
			throttleRetries++
			drain(response)
			c.pauseBeforeNext = 0
			delay := retryAfter(response.Header)
			c.noteWait(delay, true)
			if err := c.wait(ctx, delay); err != nil {
				return err
			}
		case response.StatusCode >= 500 && serverRetries < maxServerRetries:
			serverRetries++
			drain(response)
			if err := c.wait(ctx, backoff(serverRetries)); err != nil {
				return err
			}
		default:
			drain(response)
			return &statusErr{code: response.StatusCode, target: method + " " + redact(target)}
		}
	}
}

func (c *client) send(ctx context.Context, method, target string, form url.Values, withToken bool) (*http.Response, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", redact(target), err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if withToken {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.api.Do(request)
}
