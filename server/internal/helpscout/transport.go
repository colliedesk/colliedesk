package helpscout

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/importkit"
)

const maxRedirects = 5

// newClient builds the API client (no redirect off the API host) and the image client (HTTPS
// redirects to Help Scout image hosts only). Both dial public addresses only.
func newClient(base *url.URL, options Options, allowedImage func(*url.URL) bool) *client {
	transport := importkit.NewTransport(options.hooks.rootCAs, options.hooks.allowDial)
	api := &http.Client{Timeout: apiTimeout, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, base.Host) || len(via) >= maxRedirects {
			return fmt.Errorf("refusing redirect to %s", redact(req.URL.String()))
		}
		return nil
	}}
	images := &http.Client{Timeout: apiTimeout, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= maxRedirects || !allowedImage(req.URL) {
			return fmt.Errorf("refusing redirect to %s", redact(req.URL.String()))
		}
		return nil
	}}
	return &client{base: base, appID: options.AppID, appSecret: options.AppSecret, api: api, images: images, sleep: options.Sleep}
}

// fetchImage downloads one image without the access token and returns at most limit+1 bytes and
// the declared Content-Type. 404 and 410 are errGone.
func (c *client) fetchImage(ctx context.Context, target string, limit int64) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build image request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := c.images.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", fmt.Errorf("GET %s: %s", redact(target), networkReason(err))
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return nil, "", fmt.Errorf("GET %s: HTTP %d: %w", redact(target), response.StatusCode, errGone)
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, "", fmt.Errorf("GET %s: HTTP %d", redact(target), response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("read image: %w", err)
	}
	return data, response.Header.Get("Content-Type"), nil
}

func networkReason(err error) string {
	if strings.Contains(err.Error(), "refusing redirect") {
		return "redirect refused (not HTTPS, or not a Help Scout host)"
	}
	if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "Client.Timeout") {
		return "timeout"
	}
	return "connection failed"
}

// redact drops the query string, which may hold signatures.
func redact(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return "<invalid URL>"
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
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
