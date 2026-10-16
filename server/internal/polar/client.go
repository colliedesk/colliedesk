package polar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// ErrCustomerNotFound means Polar has no customer for the external id yet (nobody paid).
var ErrCustomerNotFound = errors.New("polar customer not found")

// ErrCustomerConflict means Polar refused to create a customer, in practice because another
// customer already uses the email. The caller may retry with a different address.
var ErrCustomerConflict = errors.New("polar customer conflicts with an existing one")

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

// CheckoutRequest creates a checkout for one product, bought by an existing Polar customer.
type CheckoutRequest struct {
	ProductID  string
	CustomerID string
	SuccessURL string
	// EmbedOrigin is the origin of the page that opens the checkout as an overlay. Polar refuses
	// to embed a checkout session created without it.
	EmbedOrigin string
}

// EnsureCustomer returns the Polar customer whose external id is externalID, creating it when
// there is none. The external id (the workspace id) is the identity: Polar keeps customers unique
// by email too, so the caller passes an email that is unique to the workspace. A second call for
// the same external id returns the same customer.
func (c *Client) EnsureCustomer(ctx context.Context, externalID, email, name string) (string, error) {
	var found struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/customers/external/"+url.PathEscape(externalID), nil, &found)
	var status *StatusError
	switch {
	case err == nil && found.ID != "":
		return found.ID, nil
	case err == nil:
		return "", errors.New("find polar customer: reply has no id")
	case !errors.As(err, &status) || status.Code != http.StatusNotFound:
		return "", fmt.Errorf("find polar customer: %w", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	body := map[string]any{"external_id": externalID, "email": email, "name": name}
	if err := c.do(ctx, http.MethodPost, "/v1/customers/", body, &created); err != nil {
		if errors.As(err, &status) && status.Code == http.StatusUnprocessableEntity {
			return "", fmt.Errorf("create polar customer: %w", ErrCustomerConflict)
		}
		return "", fmt.Errorf("create polar customer: %w", err)
	}
	if created.ID == "" {
		return "", errors.New("create polar customer: reply has no id")
	}
	return created.ID, nil
}

// CreateCheckout returns the hosted checkout URL.
func (c *Client) CreateCheckout(ctx context.Context, input CheckoutRequest) (string, error) {
	body := map[string]any{"products": []string{input.ProductID}, "customer_id": input.CustomerID}
	if input.SuccessURL != "" {
		body["success_url"] = input.SuccessURL
	}
	if input.EmbedOrigin != "" {
		body["embed_origin"] = input.EmbedOrigin
	}
	var reply struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/checkouts/", body, &reply); err != nil {
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
	if err := c.do(ctx, http.MethodPost, "/v1/customer-sessions/", body, &reply); err != nil {
		var status *StatusError
		// Polar answers 404 or 422 for an external id it has never seen.
		if errors.As(err, &status) && (status.Code == http.StatusNotFound || status.Code == http.StatusUnprocessableEntity) {
			return "", ErrCustomerNotFound
		}
		return "", fmt.Errorf("create polar portal session: %w", err)
	}
	if reply.URL == "" {
		return "", errors.New("create polar portal session: reply has no customer_portal_url")
	}
	return reply.URL, nil
}

// HasPaidSubscription reports whether Polar knows a subscription of the customer with this
// external id (the workspace id) that grants a paid plan at now, by the same rule as the webhook
// mirror. It reads the first 100 subscriptions, far more than a workspace holds.
func (c *Client) HasPaidSubscription(ctx context.Context, externalID string, now time.Time) (bool, error) {
	var reply struct {
		Items []struct {
			Status           string     `json:"status"`
			CurrentPeriodEnd *time.Time `json:"current_period_end"`
			EndedAt          *time.Time `json:"ended_at"`
		} `json:"items"`
	}
	path := "/v1/subscriptions/?limit=100&external_customer_id=" + url.QueryEscape(externalID)
	if err := c.do(ctx, http.MethodGet, path, nil, &reply); err != nil {
		return false, fmt.Errorf("list polar subscriptions: %w", err)
	}
	for _, item := range reply.Items {
		paid := domain.Subscription{Status: item.Status, CurrentPeriodEnd: item.CurrentPeriodEnd, EndedAt: item.EndedAt}
		if paid.Entitled(now) {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, target any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
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
