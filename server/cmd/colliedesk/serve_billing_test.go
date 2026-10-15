package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
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
