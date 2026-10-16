package polar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// slotStatuses are the subscription states that hold a seat of a capped product.
var slotStatuses = []string{"active", "trialing", "past_due"}

// CountSlotHolders returns how many subscriptions of the product hold a slot: active, trialing or
// past due. It counts at Polar, so a buyer who came through a checkout link and has no workspace
// is counted too.
func (c *Client) CountSlotHolders(ctx context.Context, productID string) (int, error) {
	query := url.Values{"product_id": {productID}, "status": slotStatuses, "limit": {"1"}}
	var reply struct {
		Pagination *struct {
			TotalCount int `json:"total_count"`
		} `json:"pagination"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/subscriptions/?"+query.Encode(), nil, &reply); err != nil {
		return 0, fmt.Errorf("count polar subscriptions: %w", err)
	}
	if reply.Pagination == nil || reply.Pagination.TotalCount < 0 {
		return 0, errors.New("count polar subscriptions: reply has no valid total_count")
	}
	return reply.Pagination.TotalCount, nil
}

// ProductArchived reports whether the product is archived at Polar, which stops every checkout.
func (c *Client) ProductArchived(ctx context.Context, productID string) (bool, error) {
	var reply struct {
		IsArchived *bool `json:"is_archived"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/products/"+url.PathEscape(productID), nil, &reply); err != nil {
		return false, fmt.Errorf("read polar product: %w", err)
	}
	if reply.IsArchived == nil {
		return false, errors.New("read polar product: reply has no is_archived")
	}
	return *reply.IsArchived, nil
}

// ArchiveProduct archives the product at Polar. Archiving is not undone from here.
func (c *Client) ArchiveProduct(ctx context.Context, productID string) error {
	var reply map[string]any
	if err := c.do(ctx, http.MethodPatch, "/v1/products/"+url.PathEscape(productID), map[string]any{"is_archived": true}, &reply); err != nil {
		return fmt.Errorf("archive polar product: %w", err)
	}
	return nil
}
