package polar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrCustomerNotFound means Polar has no customer for the external id yet (nobody paid).
var ErrCustomerNotFound = errors.New("polar customer not found")

// StatusError is a non-2xx answer from Polar. The body is dropped on purpose: it can echo input.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("polar answered HTTP %d", e.Code) }

// Client calls the Polar API with an organization access token. Errors carry the HTTP status but
// never the token or the response body.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

func NewClient(config Config) *Client {
	return &Client{token: config.AccessToken, baseURL: config.APIURL, http: &http.Client{Timeout: 10 * time.Second}}
}

// CheckoutRequest creates a checkout for one product. ExternalCustomerID is how Polar events come
// back to the workspace, so the server sets it and never the browser.
type CheckoutRequest struct {
	ProductID          string
	ExternalCustomerID string
	CustomerEmail      string
	SuccessURL         string
}

// CreateCheckout returns the hosted checkout URL.
func (c *Client) CreateCheckout(ctx context.Context, input CheckoutRequest) (string, error) {
	body := map[string]any{"products": []string{input.ProductID}, "external_customer_id": input.ExternalCustomerID}
	if input.CustomerEmail != "" {
		body["customer_email"] = input.CustomerEmail
	}
	if input.SuccessURL != "" {
		body["success_url"] = input.SuccessURL
	}
	var reply struct {
		URL string `json:"url"`
	}
	if err := c.post(ctx, "/v1/checkouts/", body, &reply); err != nil {
		return "", fmt.Errorf("create polar checkout: %w", err)
	}
	if reply.URL == "" {
		return "", errors.New("create polar checkout: reply has no url")
	}
	return reply.URL, nil
}

// CreatePortalSession returns a short-lived customer portal URL where the customer cancels or
// changes the card.
func (c *Client) CreatePortalSession(ctx context.Context, externalCustomerID, returnURL string) (string, error) {
	body := map[string]any{"external_customer_id": externalCustomerID}
	if returnURL != "" {
		body["return_url"] = returnURL
	}
	var reply struct {
		URL string `json:"customer_portal_url"`
	}
	if err := c.post(ctx, "/v1/customer-sessions/", body, &reply); err != nil {
		var status *StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			return "", ErrCustomerNotFound
		}
		return "", fmt.Errorf("create polar portal session: %w", err)
	}
	if reply.URL == "" {
		return "", errors.New("create polar portal session: reply has no customer_portal_url")
	}
	return reply.URL, nil
}

func (c *Client) post(ctx context.Context, path string, body, target any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		// The token travels in a header, so the transport error (which names the URL) holds no secret.
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return &StatusError{Code: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("decode reply: %w", err)
	}
	return nil
}
