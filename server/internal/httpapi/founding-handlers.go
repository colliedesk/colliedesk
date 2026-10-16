package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const (
	foundingPath = "/api/v1/founding"
	// marketingOrigin may read the slot state from the browser.
	marketingOrigin = "https://colliedesk.com"
	// foundingRefreshTimeout bounds the three Polar calls of one refresh.
	foundingRefreshTimeout = 30 * time.Second
)

// foundingState is the last slot count read from Polar, kept in memory. It is unknown until the
// first refresh succeeds. Archived never goes back to false by itself: closing is one-way.
type foundingState struct {
	// refresh serializes refreshes so an older count never overwrites a newer one.
	refresh  sync.Mutex
	mu       sync.RWMutex
	known    bool
	taken    int
	archived bool
}

func (s *foundingState) set(taken int, archived bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.known, s.taken, s.archived = true, taken, archived || s.archived
}

func (s *foundingState) get() (taken int, known, closed bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.taken, s.known, s.known && (s.archived || s.taken >= domain.FoundingSlots)
}

// foundingClosed is true only once a count says so. While the count is unknown the app keeps
// offering Founding, because the archive at Polar is what stops a purchase.
func (a *API) foundingClosed() bool {
	_, _, closed := a.founding.get()
	return closed
}

func (a *API) isFoundingProduct(productID string) bool {
	return a.polar != nil && productID != "" && productID == a.config.Billing.Products.Founding
}

// checkoutPlans is the list of plans offered now: the configured ones, without Founding once closed.
func (a *API) checkoutPlans() []string {
	plans := a.config.Billing.CheckoutPlans()
	if a.foundingClosed() {
		plans = slices.DeleteFunc(plans, func(plan string) bool { return plan == "founding" })
	}
	return plans
}

// refreshFounding reads the slot holders and the archive flag from Polar, archives the product when
// the slots are used up, and stores the result. When the archive fails the count is still stored,
// so the app closes at once; the next Founding event or start tries the archive again. Two buyers
// who pay at the same moment at slot 9 can both get in; the cap is not a lock.
func (a *API) refreshFounding(ctx context.Context) error {
	if !a.isFoundingProduct(a.config.Billing.Products.Founding) {
		return nil
	}
	product := a.config.Billing.Products.Founding
	a.founding.refresh.Lock()
	defer a.founding.refresh.Unlock()
	archived, err := a.polar.ProductArchived(ctx, product)
	if err != nil {
		return err
	}
	taken, err := a.polar.CountSlotHolders(ctx, product)
	if err != nil {
		return err
	}
	if taken < domain.FoundingSlots || archived {
		a.founding.set(taken, archived)
		return nil
	}
	a.founding.set(taken, false)
	if err := a.polar.ArchiveProduct(ctx, product); err != nil {
		return err
	}
	a.founding.set(taken, true)
	slog.Info("founding closed: product archived", "product_id", product, "taken", taken, "slots", domain.FoundingSlots)
	return nil
}

// refreshFoundingAfterWebhook runs after the webhook answer is decided. A failure is logged and
// changes nothing for Polar, which would otherwise redeliver an event that was applied.
func (a *API) refreshFoundingAfterWebhook(parent context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), foundingRefreshTimeout)
	defer cancel()
	if err := a.refreshFounding(ctx); err != nil {
		slog.Error("founding slot refresh failed", "error", err)
	}
}

// getFounding tells the marketing site how many slots are left. It reads memory only.
func (a *API) getFounding(response http.ResponseWriter, request *http.Request) {
	taken, known, closed := a.founding.get()
	view := struct {
		Slots int  `json:"slots"`
		Taken *int `json:"taken"`
		Open  bool `json:"open"`
	}{Slots: domain.FoundingSlots, Open: !closed}
	if known {
		view.Taken = &taken
	}
	response.Header().Set("Cache-Control", "public, max-age=60")
	response.Header().Set("Access-Control-Allow-Origin", marketingOrigin)
	writeJSON(response, http.StatusOK, view)
}
