package polar

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Subscription is the part of a Polar subscription the product reads.
type Subscription struct {
	ID                 string
	Status             string
	ProductID          string
	CustomerID         string
	ExternalCustomerID string
	// CustomerEmail is the buyer's address as Polar holds it; empty when the payload omits it.
	CustomerEmail     string
	CurrentPeriodEnd  *time.Time
	CancelAtPeriodEnd bool
	EndedAt           *time.Time
	// ModifiedAt orders events of one subscription; it falls back to the event timestamp.
	ModifiedAt time.Time
}

// Event is one webhook delivery. Subscription is set only for "subscription.*" events.
type Event struct {
	Type         string
	Subscription *Subscription
}

// ParseEvent reads a webhook body whose signature was already verified.
func ParseEvent(body []byte) (Event, error) {
	var envelope struct {
		Type      string          `json:"type"`
		Timestamp time.Time       `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Event{}, fmt.Errorf("decode webhook: %w", err)
	}
	if envelope.Type == "" {
		return Event{}, errors.New("decode webhook: no event type")
	}
	event := Event{Type: envelope.Type}
	if !strings.HasPrefix(envelope.Type, "subscription.") {
		return event, nil
	}
	var data struct {
		ID                string     `json:"id"`
		Status            string     `json:"status"`
		ProductID         string     `json:"product_id"`
		CustomerID        string     `json:"customer_id"`
		CurrentPeriodEnd  *time.Time `json:"current_period_end"`
		CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
		EndedAt           *time.Time `json:"ended_at"`
		ModifiedAt        *time.Time `json:"modified_at"`
		Customer          struct {
			ExternalID *string `json:"external_id"`
			Email      string  `json:"email"`
		} `json:"customer"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return Event{}, fmt.Errorf("decode subscription: %w", err)
	}
	if data.ID == "" || data.Status == "" || data.ProductID == "" {
		return Event{}, errors.New("decode subscription: id, status and product_id are required")
	}
	subscription := &Subscription{
		ID: data.ID, Status: data.Status, ProductID: data.ProductID, CustomerID: data.CustomerID,
		CurrentPeriodEnd: data.CurrentPeriodEnd, CancelAtPeriodEnd: data.CancelAtPeriodEnd, EndedAt: data.EndedAt,
		ModifiedAt: envelope.Timestamp,
	}
	if data.ModifiedAt != nil {
		subscription.ModifiedAt = *data.ModifiedAt
	}
	subscription.CustomerEmail = data.Customer.Email
	if data.Customer.ExternalID != nil {
		subscription.ExternalCustomerID = *data.Customer.ExternalID
	}
	if subscription.ModifiedAt.IsZero() {
		return Event{}, errors.New("decode subscription: modified_at or timestamp is required")
	}
	event.Subscription = subscription
	return event, nil
}
