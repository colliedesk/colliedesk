package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCloudAuthDisabled(t *testing.T) {
	api, _ := testAPI(t)
	config := request(t, api, http.MethodGet, "/api/v1/config", nil, "", nil)
	if config.Code != http.StatusOK || config.Body.String() != "{\"billing\":{\"enabled\":false},\"cloud_sign_in\":{\"enabled\":false,\"publishable_key\":null},\"limits\":{\"outgoing_attachments_bytes\":10485760,\"outgoing_attachments_files\":10},\"signup\":{\"enabled\":false},\"version\":\"dev\"}\n" {
		t.Fatalf("config status=%d body=%s", config.Code, config.Body.String())
	}
	exchanged := exchange(t, api, "not-a-token", "", testOrigin)
	if exchanged.Code != http.StatusNotFound {
		t.Fatalf("exchange status=%d body=%s", exchanged.Code, exchanged.Body.String())
	}
}

func TestCloudAuthValidTokenCreatesOrdinarySession(t *testing.T) {
	fixture := newCloudAuthFixture(t)
	_, _, err := fixture.service.CreateWorkspace(context.Background(), "Cloud", "admin@cloud.example.com")
	if err != nil {
		t.Fatal(err)
	}
	config := request(t, fixture.api, http.MethodGet, "/api/v1/config", nil, "", nil)
	if config.Code != http.StatusOK || config.Body.String() != "{\"billing\":{\"enabled\":false},\"cloud_sign_in\":{\"enabled\":true,\"publishable_key\":\"pk_test_example\"},\"limits\":{\"outgoing_attachments_bytes\":10485760,\"outgoing_attachments_files\":10},\"signup\":{\"enabled\":false},\"version\":\"dev\"}\n" {
		t.Fatalf("config status=%d body=%s", config.Code, config.Body.String())
	}
	response := exchange(t, fixture.api, fixture.token(t, nil), "", testOrigin)
	if response.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", response.Code, response.Body.String())
	}
	me := request(t, fixture.api, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookie(t, response))
	if me.Code != http.StatusOK {
		t.Fatalf("session status=%d body=%s", me.Code, me.Body.String())
	}
}

func TestCloudAuthUsesConfiguredEmailClaim(t *testing.T) {
	fixture := newCloudAuthFixtureWithEmailClaim(t, "primary_email")
	_, _, err := fixture.service.CreateWorkspace(context.Background(), "Cloud", "admin@cloud.example.com")
	if err != nil {
		t.Fatal(err)
	}
	response := exchange(t, fixture.api, fixture.token(t, map[string]any{
		"email": nil, "primary_email": "admin@cloud.example.com",
	}), "", testOrigin)
	if response.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCloudAuthRejectsInvalidTokens(t *testing.T) {
	fixture := newCloudAuthFixture(t)
	_, _, err := fixture.service.CreateWorkspace(context.Background(), "Cloud", "admin@cloud.example.com")
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	originalKey := fixture.key
	cases := map[string]func() string{
		"wrong signature": func() string {
			fixture.key = otherKey
			defer func() { fixture.key = originalKey }()
			return fixture.token(t, nil)
		},
		"expired":          func() string { return fixture.token(t, map[string]any{"exp": time.Now().Add(-2 * time.Minute).Unix()}) },
		"wrong issuer":     func() string { return fixture.token(t, map[string]any{"iss": "https://wrong.example.com"}) },
		"wrong azp":        func() string { return fixture.token(t, map[string]any{"azp": "https://evil.example.com"}) },
		"missing nbf":      func() string { return fixture.token(t, map[string]any{"nbf": nil}) },
		"unverified email": func() string { return fixture.token(t, map[string]any{"email_verified": false}) },
	}
	for name, buildToken := range cases {
		t.Run(name, func(t *testing.T) {
			response := exchange(t, fixture.api, buildToken(), "", testOrigin)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCloudAuthUnknownEmailIsForbidden(t *testing.T) {
	fixture := newCloudAuthFixture(t)
	response := exchange(t, fixture.api, fixture.token(t, map[string]any{"email": "missing@example.com"}), "", testOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCloudAuthRequiresWorkspaceChoice(t *testing.T) {
	fixture := newCloudAuthFixture(t)
	for _, name := range []string{"Alpha", "Beta"} {
		if _, _, err := fixture.service.CreateWorkspace(context.Background(), name, "admin@cloud.example.com"); err != nil {
			t.Fatal(err)
		}
	}
	choices := exchange(t, fixture.api, fixture.token(t, nil), "", testOrigin)
	if choices.Code != http.StatusConflict || choices.Body.String() != "{\"workspaces\":[{\"slug\":\"alpha\",\"name\":\"Alpha\"},{\"slug\":\"beta\",\"name\":\"Beta\"}]}\n" {
		t.Fatalf("choices status=%d body=%s", choices.Code, choices.Body.String())
	}
	selected := exchange(t, fixture.api, fixture.token(t, nil), "beta", testOrigin)
	if selected.Code != http.StatusOK {
		t.Fatalf("selected status=%d body=%s", selected.Code, selected.Body.String())
	}
	me := request(t, fixture.api, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookie(t, selected))
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"slug":"beta"`) {
		t.Fatalf("selected session status=%d body=%s", me.Code, me.Body.String())
	}
}

func TestCloudAuthCannotCrossWorkspaces(t *testing.T) {
	fixture := newCloudAuthFixture(t)
	if _, _, err := fixture.service.CreateWorkspace(context.Background(), "Alpha", "admin@cloud.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.service.CreateWorkspace(context.Background(), "Beta", "other@example.com"); err != nil {
		t.Fatal(err)
	}
	response := exchange(t, fixture.api, fixture.token(t, nil), "beta", testOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCloudAuthEnforcesOriginAndRefreshesUnknownKey(t *testing.T) {
	fixture := newCloudAuthFixture(t)
	if _, _, err := fixture.service.CreateWorkspace(context.Background(), "Cloud", "admin@cloud.example.com"); err != nil {
		t.Fatal(err)
	}
	if response := exchange(t, fixture.api, fixture.token(t, nil), "", "https://evil.example.com"); response.Code != http.StatusForbidden {
		t.Fatalf("wrong origin status=%d body=%s", response.Code, response.Body.String())
	}
	if response := exchange(t, fixture.api, fixture.token(t, nil), "", testOrigin); response.Code != http.StatusOK {
		t.Fatalf("initial key status=%d body=%s", response.Code, response.Body.String())
	}
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fixture.key, fixture.kid = newKey, "key-2"
	fixture.keys[fixture.kid] = newKey
	if response := exchange(t, fixture.api, fixture.token(t, nil), "", testOrigin); response.Code != http.StatusOK {
		t.Fatalf("rotated key status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCloudAuthThrottlesUnknownKeyRefresh(t *testing.T) {
	fixture := newCloudAuthFixture(t)
	if _, _, err := fixture.service.CreateWorkspace(context.Background(), "Cloud", "admin@cloud.example.com"); err != nil {
		t.Fatal(err)
	}
	if response := exchange(t, fixture.api, fixture.token(t, nil), "", testOrigin); response.Code != http.StatusOK {
		t.Fatalf("initial exchange status=%d body=%s", response.Code, response.Body.String())
	}
	for _, kid := range []string{"missing-1", "missing-2", "missing-3"} {
		fixture.kid = kid
		if response := exchange(t, fixture.api, fixture.token(t, nil), "", testOrigin); response.Code != http.StatusUnauthorized {
			t.Fatalf("unknown kid %s status=%d body=%s", kid, response.Code, response.Body.String())
		}
	}
	if requests := fixture.jwksRequests.Load(); requests != 2 {
		t.Fatalf("JWKS requests=%d, want 2 (initial load plus one unknown-kid refresh)", requests)
	}
}
