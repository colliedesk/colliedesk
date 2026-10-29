package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesSPAAndKeepsAPINamespace(t *testing.T) {
	api := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.NotFound(response, request)
	})
	handler := Handler(api, ConsoleSignIn{})

	spa := httptest.NewRecorder()
	handler.ServeHTTP(spa, httptest.NewRequest(http.MethodGet, "/conversations/123", nil))
	if spa.Code != http.StatusOK || !strings.Contains(spa.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("SPA fallback status=%d body=%s", spa.Code, spa.Body.String())
	}

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if apiResponse.Code != http.StatusNotFound {
		t.Fatalf("API status=%d, want 404", apiResponse.Code)
	}
}

func TestOperatorConsoleServesItsScreensWithItsPolicy(t *testing.T) {
	for base, paths := range map[string][]string{"/operator": {"/operator", "/operator/sign-in", "/operator/config/email"}, "": {"/", "/sign-in", "/history"}} {
		handler := OperatorHandler(base, ConsoleSignIn{})
		for _, path := range paths {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Header().Get("Content-Security-Policy") == "" || recorder.Header().Get("Referrer-Policy") != "no-referrer" || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s%s: headers %v", base, path, recorder.Header())
			}
			if recorder.Code == http.StatusOK && !strings.Contains(recorder.Body.String(), `name="colliedesk-operator-base" content="`+base+`"`) {
				t.Errorf("%s%s: the page does not name its base", base, path)
			}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, base+"/inbox", nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s/inbox: %d, want 404", base, recorder.Code)
		}
	}
}

// With cloud sign-in the console's sign-in page may load the identity provider's form, and only that page:
// every other screen keeps the console's own policy, and the page names the publishable key.
func TestOperatorConsoleOpensItsPolicyToCloudSignInOnTheSignInPageOnly(t *testing.T) {
	// "clerk.colliedesk.com$" in base64: the Frontend API's host.
	signIn := ConsoleSignIn{PublishableKey: "pk_live_Y2xlcmsuY29sbGllZGVzay5jb20k"}
	for base, rest := range map[string][]string{"/operator": {"/operator", "/operator/config/email"}, "": {"/", "/history", "/operators"}} {
		handler := OperatorHandler(base, signIn)
		page := httptest.NewRecorder()
		handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, base+"/sign-in", nil))
		policy := page.Header().Get("Content-Security-Policy")
		for _, part := range []string{"script-src 'self' https://clerk.colliedesk.com https://challenges.cloudflare.com https://*.protect.clerk.com;",
			"connect-src 'self' https://clerk.colliedesk.com https://*.protect.clerk.com:*;", "style-src 'self' 'unsafe-inline';",
			"img-src 'self' data: https://img.clerk.com;", "frame-src https://challenges.cloudflare.com https://*.protect.clerk.com;", "worker-src 'self' blob:;",
			"frame-ancestors 'none'"} {
			if !strings.Contains(policy, part) {
				t.Errorf("%s/sign-in: policy %q lacks %q", base, policy, part)
			}
		}
		if page.Code == http.StatusOK && !strings.Contains(page.Body.String(), `name="colliedesk-operator-clerk" content="`+signIn.PublishableKey+`"`) {
			t.Errorf("%s/sign-in: the page does not name the publishable key", base)
		}
		for _, path := range rest {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if got := recorder.Header().Get("Content-Security-Policy"); got != operatorCSP {
				t.Errorf("%s: policy %q, want the console's own", path, got)
			}
			// Signing out and an ended session lead to the cloud sign-in, so every screen names the key.
			if recorder.Code == http.StatusOK && !strings.Contains(recorder.Body.String(), `name="colliedesk-operator-clerk" content="`+signIn.PublishableKey+`"`) {
				t.Errorf("%s: the page does not name the publishable key", path)
			}
		}
	}
	// Without cloud sign-in, or with a key that names no host, the sign-in page keeps the console's policy.
	for _, key := range []string{"", "pk_live_bm90IGEgaG9zdA==", "sk_live_x"} {
		recorder := httptest.NewRecorder()
		OperatorHandler("", ConsoleSignIn{PublishableKey: key}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sign-in", nil))
		if got := recorder.Header().Get("Content-Security-Policy"); got != operatorCSP {
			t.Errorf("key %q: policy %q, want the console's own", key, got)
		}
	}
}

// The workspace app and its files carry the headers that stop another site from framing a signed-in agent's
// inbox (clickjacking) and stop a browser from sniffing a file into script; the API keeps its own headers.
func TestWorkspaceAppSendsFramingAndSniffingHeaders(t *testing.T) {
	api := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.NotFound(response, request)
	})
	handler := Handler(api, ConsoleSignIn{})
	for _, path := range []string{"/", "/conversations/123", "/settings/email"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		header := recorder.Header()
		if got := header.Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
			t.Errorf("%s: Content-Security-Policy %q, want frame-ancestors 'self'", path, got)
		}
		if got := header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
			t.Errorf("%s: X-Frame-Options %q, want SAMEORIGIN", path, got)
		}
		if got := header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options %q, want nosniff", path, got)
		}
		if got := header.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
			t.Errorf("%s: Referrer-Policy %q, want strict-origin-when-cross-origin", path, got)
		}
	}
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if got := apiResponse.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("API: Content-Security-Policy %q, want none from the web app", got)
	}
}
