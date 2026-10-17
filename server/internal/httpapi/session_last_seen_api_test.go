package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

func sessionTimes(t *testing.T, api http.Handler, admin, label string) (lastSeen, expires time.Time) {
	t.Helper()
	var list struct {
		Items []struct {
			Label      string    `json:"label"`
			LastSeenAt time.Time `json:"last_seen_at"`
			ExpiresAt  time.Time `json:"expires_at"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, admin, nil), &list)
	for _, item := range list.Items {
		if item.Label == label {
			return item.LastSeenAt, item.ExpiresAt
		}
	}
	t.Fatalf("session %q not listed", label)
	return time.Time{}, time.Time{}
}

// One tool call of an agent authenticates about five times in a second. Writing the session row each
// time made them queue on one row, so the last use is recorded at most once a minute.
func TestSessionLastUseIsRecordedAtMostOncePerMinuteAndStillSlidesTheExpiry(t *testing.T) {
	api, clock, admin := lifetimeFixture(t)
	chatty := issueToken(t, api, admin, "Busy agent", "read").Token
	thirtyDays := 30 * 24 * time.Hour

	for _, offset := range []time.Duration{time.Second, 30 * time.Second, 59 * time.Second} {
		clock.set(tokenStart.Add(offset))
		if status := meStatus(t, api, chatty); status != http.StatusOK {
			t.Fatalf("use after %s: status=%d, want 200", offset, status)
		}
	}
	lastSeen, expires := sessionTimes(t, api, admin, "Busy agent")
	if !lastSeen.Equal(tokenStart) || !expires.Equal(tokenStart.Add(thirtyDays)) {
		t.Errorf("after uses within the first minute last_seen_at=%s expires_at=%s, want the creation values %s and +30 days (nothing written yet)",
			lastSeen, expires, tokenStart)
	}

	clock.set(tokenStart.Add(time.Minute))
	if status := meStatus(t, api, chatty); status != http.StatusOK {
		t.Fatalf("use after exactly a minute: status=%d, want 200", status)
	}
	lastSeen, expires = sessionTimes(t, api, admin, "Busy agent")
	if want := tokenStart.Add(time.Minute); !lastSeen.Equal(want) || !expires.Equal(want.Add(thirtyDays)) {
		t.Errorf("after a use a minute on last_seen_at=%s expires_at=%s, want %s and +30 days from it", lastSeen, expires, want)
	}

	// The expiry still follows the last recorded use: idle for 30 days from it, then refused.
	clock.set(tokenStart.Add(time.Minute + thirtyDays - time.Second))
	if status := meStatus(t, api, chatty); status != http.StatusOK {
		t.Errorf("just inside 30 days after the last recorded use: status=%d, want 200", status)
	}
}
