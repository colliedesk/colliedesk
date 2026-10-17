package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// Written out by hand: a token created at tokenStart passes one year later at tokenCap.
var (
	tokenStart = time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)
	tokenCap   = time.Date(2027, time.October, 15, 12, 0, 0, 0, time.UTC)
)

func lifetimeFixture(t *testing.T) (http.Handler, *testClock, string) {
	t.Helper()
	clock := &testClock{now: tokenStart}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = clock.Now })
	service = service.WithClock(clock.Now)
	return api, clock, createSession(t, api, service, "Token Lifetime", "admin@lifetime.example.com")
}

func meStatus(t *testing.T, api http.Handler, token string) int {
	t.Helper()
	return request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil).Code
}

func TestIdleAgentTokenExpiresThirtyDaysAfterItsLastUse(t *testing.T) {
	api, clock, admin := lifetimeFixture(t)
	idle := issueToken(t, api, admin, "Idle", "read").Token
	day := 24 * time.Hour
	for _, use := range []time.Duration{29 * day, 58 * day} {
		clock.set(tokenStart.Add(use))
		if status := meStatus(t, api, idle); status != http.StatusOK {
			t.Fatalf("use %s after creation: status=%d, want 200 (each use slides the expiry)", use, status)
		}
	}
	clock.set(tokenStart.Add(88*day + time.Second))
	if status := meStatus(t, api, idle); status != http.StatusUnauthorized {
		t.Fatalf("30 days and a second after the last use: status=%d, want 401", status)
	}
}

func TestAgentTokenUsedEveryDayStillEndsOneYearAfterCreation(t *testing.T) {
	api, clock, admin := lifetimeFixture(t)
	busy := issueToken(t, api, admin, "Busy", "send").Token
	for day := 1; day <= 364; day++ {
		clock.set(tokenStart.Add(time.Duration(day) * 24 * time.Hour))
		if status := meStatus(t, api, busy); status != http.StatusOK {
			t.Fatalf("day %d of daily use: status=%d, want 200", day, status)
		}
		if day%29 == 0 { // the person's own session keeps sliding as well
			if status := meStatus(t, api, admin); status != http.StatusOK {
				t.Fatalf("day %d: browser session status=%d, want 200", day, status)
			}
		}
	}

	clock.set(tokenCap.Add(-time.Second))
	if status := meStatus(t, api, busy); status != http.StatusOK {
		t.Fatalf("one second before the first anniversary: status=%d, want 200", status)
	}
	if got := sessionExpiry(t, api, admin, "Busy"); !got.Equal(tokenCap) {
		t.Fatalf("expires_at of the token = %s, want the cap %s, not 30 days after the last use", got, tokenCap)
	}
	clock.set(tokenCap.Add(time.Second))
	if status := meStatus(t, api, busy); status != http.StatusUnauthorized {
		t.Fatalf("one second after the first anniversary, used the day before: status=%d, want 401", status)
	}
}

func TestBrowserSessionHasNoAbsoluteCap(t *testing.T) {
	api, clock, admin := lifetimeFixture(t)
	for day := 29; day <= 29*14; day += 29 {
		clock.set(tokenStart.Add(time.Duration(day) * 24 * time.Hour))
		if status := meStatus(t, api, admin); status != http.StatusOK {
			t.Fatalf("browser session on day %d: status=%d, want 200", day, status)
		}
	}
}

func sessionExpiry(t *testing.T, api http.Handler, admin, label string) time.Time {
	t.Helper()
	var list struct {
		Items []struct {
			Label     string    `json:"label"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, admin, nil), &list)
	for _, item := range list.Items {
		if item.Label == label {
			return item.ExpiresAt
		}
	}
	t.Fatalf("session %q not listed", label)
	return time.Time{}
}
