package main

import (
	"context"
	"io"
	"net/netip"
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

// Billing locks a workspace after its trial. Until the web app can show that state, billing stays
// off; the server still starts, so an image and a config that disagree never take production down.
func TestBillingStaysOffWithoutTheLockedScreen(t *testing.T) {
	enabled := polar.Config{AccessToken: "t", WebhookSecret: "s", Products: polar.Products{TeamMonthly: "a", TeamYearly: "b", Founding: "c"}}
	got, warning := billingAtStart(enabled, false)
	if got.Enabled() || !strings.Contains(warning, "locked screen") {
		t.Fatalf("billing configured, no locked screen: enabled=%v warning=%q, want billing off and a warning naming the locked screen", got.Enabled(), warning)
	}
	if got, warning := billingAtStart(enabled, true); !got.Enabled() || warning != "" {
		t.Fatalf("billing configured with the locked screen: enabled=%v warning=%q, want billing on", got.Enabled(), warning)
	}
	if got, warning := billingAtStart(polar.Config{}, false); got.Enabled() || warning != "" {
		t.Fatalf("billing not configured: enabled=%v warning=%q", got.Enabled(), warning)
	}
}

func TestServeGetsPastBillingConfigurationAndStopsOnlyAtTheDatabase(t *testing.T) {
	for key, value := range map[string]string{"POLAR_ACCESS_TOKEN": "t", "POLAR_WEBHOOK_SECRET": "s",
		"POLAR_PRODUCT_TEAM_MONTHLY": "a", "POLAR_PRODUCT_TEAM_YEARLY": "b", "POLAR_PRODUCT_FOUNDING": "c"} {
		t.Setenv(key, value)
	}
	// The database below is unreachable, so serve stops there; it must not stop earlier on billing.
	err := run(context.Background(), []string{"serve", "--listen", "127.0.0.1:0"}, io.Discard,
		"postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable", "http://localhost:8080")
	if err == nil || strings.Contains(err.Error(), "locked screen") || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("serve must get past billing and stop only at the unreachable database: %v", err)
	}
}

// Sign-up exists only with cloud sign-in and billing. Without the hash key the trial records are
// unkeyed, and without a trusted proxy every client shares the proxy's address and the 4th
// sign-up of the day is refused for the whole platform; either mistake turns sign-up off, never the server.
func TestSignupNeedsItsHashKeyAndTrustedProxies(t *testing.T) {
	good := "0123456789abcdef0123456789abcdef"
	proxies := []netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")}
	cases := []struct {
		name           string
		cloud, billing bool
		proxies        []netip.Prefix
		key            string
		want           string // "" means sign-up stays on
	}{
		{"self-hosted needs neither", false, false, nil, "", ""},
		{"cloud sign-in alone is not sign-up", true, false, nil, "", ""},
		{"billing alone is not sign-up", false, true, nil, "", ""},
		{"sign-up with both", true, true, proxies, good, ""},
		{"no hash key", true, true, proxies, "", "SIGNUP_HASH_KEY"},
		{"short hash key", true, true, proxies, "too-short", "SIGNUP_HASH_KEY"},
		{"no trusted proxy", true, true, nil, good, "TRUSTED_PROXIES"},
	}
	for _, c := range cases {
		problem := signupProblem(c.cloud, c.billing, c.proxies, c.key)
		switch {
		case c.want == "" && problem != "":
			t.Errorf("%s: problem=%q, want sign-up on", c.name, problem)
		case c.want != "" && !strings.Contains(problem, c.want):
			t.Errorf("%s: problem=%q, want sign-up off naming %s", c.name, problem, c.want)
		case strings.Contains(problem, good):
			t.Errorf("%s: the message leaks the key", c.name)
		}
	}
}
