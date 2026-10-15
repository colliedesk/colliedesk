package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/polar"
)

// A half-configured billing setup must stop the server before it listens: the instance would
// otherwise accept checkouts it cannot reconcile, or silently run without billing.
func TestServeRefusesPartialBillingConfiguration(t *testing.T) {
	t.Setenv("POLAR_ACCESS_TOKEN", "polar_oat_never-printed")
	t.Setenv("POLAR_WEBHOOK_SECRET", "whsec_never-printed")
	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), []string{"serve", "--listen", "127.0.0.1:0"}, io.Discard,
			"postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable", "http://localhost:8080")
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("serve started with partial billing configuration")
		}
		for _, want := range []string{"POLAR_PRODUCT_TEAM_MONTHLY", "POLAR_PRODUCT_TEAM_YEARLY", "POLAR_PRODUCT_FOUNDING"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not name %s", err, want)
			}
		}
		if strings.Contains(err.Error(), "never-printed") {
			t.Fatalf("error leaks a configured value: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not refuse partial billing configuration")
	}
}

// Billing locks a workspace after its trial. Until the web app can show that state, switching it on
// would leave customers with a broken inbox and no reason.
func TestBillingNeedsTheLockedScreen(t *testing.T) {
	enabled := polar.Config{AccessToken: "t", WebhookSecret: "s", Products: polar.Products{TeamMonthly: "a", TeamYearly: "b", Founding: "c"}}
	if err := requireLockedScreen(enabled, false); err == nil || !strings.Contains(err.Error(), "locked screen") {
		t.Fatalf("billing on without the locked screen: err=%v, want a refusal naming the locked screen", err)
	}
	if err := requireLockedScreen(enabled, true); err != nil {
		t.Fatalf("billing on with the locked screen: err=%v", err)
	}
	if err := requireLockedScreen(polar.Config{}, false); err != nil {
		t.Fatalf("billing off must always start: err=%v", err)
	}
}

func TestServeRefusesBillingWhileTheLockedScreenIsMissing(t *testing.T) {
	if lockedScreenShipped {
		t.Skip("the web app ships the locked screen")
	}
	for key, value := range map[string]string{"POLAR_ACCESS_TOKEN": "t", "POLAR_WEBHOOK_SECRET": "s",
		"POLAR_PRODUCT_TEAM_MONTHLY": "a", "POLAR_PRODUCT_TEAM_YEARLY": "b", "POLAR_PRODUCT_FOUNDING": "c"} {
		t.Setenv(key, value)
	}
	err := run(context.Background(), []string{"serve", "--listen", "127.0.0.1:0"}, io.Discard,
		"postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable", "http://localhost:8080")
	if err == nil || !strings.Contains(err.Error(), "locked screen") {
		t.Fatalf("serve with billing on and no locked screen: err=%v", err)
	}
}
