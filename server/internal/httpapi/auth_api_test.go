package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestPasswordlessRedeemIsSingleUseAndCreatesSession(t *testing.T) {
	api, authService := testAPI(t)
	_, pairingURL, err := authService.CreateWorkspace(context.Background(), "Acme Support", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	token := linkToken(t, pairingURL)
	invalidDelivery := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
		"token": token, "session_type": "both",
	}, "", nil)
	if invalidDelivery.Code != http.StatusBadRequest {
		t.Fatalf("invalid session delivery status=%d, want 400", invalidDelivery.Code)
	}

	first := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
		"token": token, "label": "Firefox on Linux", "session_type": "cookie",
	}, "", nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first redeem status=%d body=%s", first.Code, first.Body.String())
	}
	var session struct {
		Token string `json:"token"`
		Agent struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"agent"`
	}
	decode(t, first, &session)
	if session.Token != "" || session.Agent.Email != "admin@example.com" || session.Agent.Role != "admin" {
		t.Fatalf("unexpected session: %+v", session)
	}
	if cookie := first.Result().Cookies(); len(cookie) != 1 || !cookie[0].HttpOnly || !cookie[0].Secure || cookie[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie does not meet contract: %+v", cookie)
	}

	second := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": token, "session_type": "cookie"}, "", nil)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second redeem status=%d, want 401", second.Code)
	}

	me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, "", first.Result().Cookies()[0])
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"timezone":"UTC"`) {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}
	if refreshed := me.Result().Cookies(); len(refreshed) != 1 || refreshed[0].MaxAge <= 0 {
		t.Fatalf("cookie session expiry did not slide: %+v", refreshed)
	}

	bearerURL, err := authService.Pair(context.Background(), "acme-support", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bearer := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
		"token": linkToken(t, bearerURL), "session_type": "bearer",
	}, "", nil)
	decode(t, bearer, &session)
	if session.Token == "" || len(bearer.Result().Cookies()) != 0 {
		t.Fatalf("bearer delivery exposed cookie or omitted token: body=%s cookies=%v", bearer.Body.String(), bearer.Result().Cookies())
	}
}

func TestInvalidBearerNeverFallsBackToCookieAndCookieWritesRequireOrigin(t *testing.T) {
	api, authService := testAPI(t)
	_, pairingURL, err := authService.CreateWorkspace(context.Background(), "Acme Support", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	redeem := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
		"token": linkToken(t, pairingURL), "session_type": "cookie",
	}, "", nil)
	cookie := redeem.Result().Cookies()[0]

	badBearer := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, "invalid-token", cookie)
	if badBearer.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer with valid cookie status=%d, want 401", badBearer.Code)
	}

	withoutOrigin := request(t, api, http.MethodPost, "/api/v1/auth/logout", nil, "", cookie)
	if withoutOrigin.Code != http.StatusForbidden {
		t.Fatalf("cookie write without Origin status=%d, want 403", withoutOrigin.Code)
	}

	withOrigin := requestWithOrigin(t, api, http.MethodPost, "/api/v1/auth/logout", cookie, testOrigin)
	if withOrigin.Code != http.StatusNoContent {
		t.Fatalf("cookie write with Origin status=%d body=%s", withOrigin.Code, withOrigin.Body.String())
	}
}
