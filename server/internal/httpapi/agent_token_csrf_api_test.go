package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// cookieSession signs in with a pairing link and returns the session cookie a browser would hold.
func cookieSession(t *testing.T, api http.Handler, pairingURL string) *http.Cookie {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem",
		map[string]any{"token": linkToken(t, pairingURL), "session_type": "cookie", "label": "Browser"}, "", nil)
	cookies := response.Result().Cookies()
	if response.Code != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("cookie redeem status=%d cookies=%d body=%s", response.Code, len(cookies), response.Body.String())
	}
	return cookies[0]
}

// POST /auth/tokens turns a cookie into a portable bearer, so a page on another origin must not be
// able to make a signed-in browser mint one.
func TestCreatingATokenWithACookieSessionNeedsTheInstanceOrigin(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	_, pairingURL, err := service.CreateWorkspace(context.Background(), "Token Csrf", "admin@csrf.example.com")
	if err != nil {
		t.Fatal(err)
	}
	cookie := cookieSession(t, api, pairingURL)
	body := map[string]any{"label": "Planted", "scope": "send"}
	tokens := func() int {
		t.Helper()
		var count int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE scope IS NOT NULL`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	for name, origin := range map[string]string{"another origin": "https://evil.example", "no Origin header": ""} {
		req := newRequest(t, http.MethodPost, "/api/v1/auth/tokens", body, "", cookie)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		response := serve(api, req)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"csrf_rejected"`) {
			t.Fatalf("%s: status=%d body=%s, want 403 csrf_rejected", name, response.Code, response.Body.String())
		}
		if count := tokens(); count != 0 {
			t.Fatalf("%s: %d tokens exist after a refused request", name, count)
		}
	}

	req := newRequest(t, http.MethodPost, "/api/v1/auth/tokens", body, "", cookie)
	req.Header.Set("Origin", testOrigin)
	if response := serve(api, req); response.Code != http.StatusCreated {
		t.Fatalf("instance origin: status=%d body=%s, want 201", response.Code, response.Body.String())
	}
	if count := tokens(); count != 1 {
		t.Fatalf("%d tokens exist after one accepted request, want 1", count)
	}
}
