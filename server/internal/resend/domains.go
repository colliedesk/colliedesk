// Package resend is the client of the Resend Domains API: add a sending domain, read the DNS records
// and status Resend reports for it, and ask Resend to check them.
package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBase is the production API address.
const DefaultBase = "https://api.resend.com"

// StatusVerified is the domain and record status of a domain whose DNS records Resend has checked.
const StatusVerified = "verified"

const (
	requestTimeout = 10 * time.Second
	maxBodyBytes   = 1 << 20
)

// Record is one DNS record to publish, with the status Resend last reported for it.
type Record struct {
	Record   string `json:"record"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Priority *int   `json:"priority"`
	Status   string `json:"status"`
}

// Domain is a sending domain in the Resend account.
type Domain struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Records []Record `json:"records"`
}

// Verified reports whether Resend says the domain, and so every record it needs, is verified.
func (d Domain) Verified() bool { return d.Status == StatusVerified }

// APIError is an error answer from Resend. Resend documents the body as {statusCode, name, message}.
type APIError struct {
	Status  int
	Name    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("resend: HTTP %d %s: %s", e.Status, e.Name, e.Message)
}

// Rejected reports an answer that refuses this domain (invalid, already registered, over a plan limit)
// rather than a server or credential problem. Retrying the same request will not help.
func (e *APIError) Rejected() bool {
	return e.Status == http.StatusBadRequest || e.Status == http.StatusForbidden || e.Status == http.StatusConflict || e.Status == http.StatusUnprocessableEntity
}

// Client calls the Resend Domains API. The zero HTTP client is replaced by one with a timeout.
type Client struct {
	APIKey string
	// Base is the API address without a trailing slash; empty uses DefaultBase.
	Base string
	HTTP *http.Client
}

// CreateDomain adds the domain to the account and returns it with the records to publish.
func (c *Client) CreateDomain(ctx context.Context, name string) (Domain, error) {
	var domain Domain
	err := c.do(ctx, http.MethodPost, "/domains", map[string]string{"name": name}, &domain)
	return domain, err
}

// GetDomain returns the domain with its current status and records.
func (c *Client) GetDomain(ctx context.Context, id string) (Domain, error) {
	var domain Domain
	err := c.do(ctx, http.MethodGet, "/domains/"+url.PathEscape(id), nil, &domain)
	return domain, err
}

// VerifyDomain asks Resend to check the domain's records now. The answer carries no status; read it
// with GetDomain.
func (c *Client) VerifyDomain(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/domains/"+url.PathEscape(id)+"/verify", nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("resend: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	base := strings.TrimRight(c.Base, "/")
	if base == "" {
		base = DefaultBase
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.APIKey)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		// The error text carries the URL, never the key.
		return fmt.Errorf("resend: %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("resend: read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		apiErr := &APIError{Status: response.StatusCode}
		var parsed struct {
			Name    string `json:"name"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &parsed) == nil {
			apiErr.Name, apiErr.Message = parsed.Name, parsed.Message
		}
		return apiErr
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("resend: decode response: %w", err)
	}
	return nil
}
