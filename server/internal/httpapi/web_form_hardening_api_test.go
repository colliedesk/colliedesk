package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestWebFormIgnoresFieldsItDoesNotKnow(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	// A site's form often has more inputs than the channel reads; they must not fail the submission.
	wantAccepted(t, f.submit(f.slugA, `{"email":"extra@example.com","name":"Ada","phone":"123","website":""}`, "", ""))
	if n := f.count(t, "conversations", f.workspaceA); n != "1" {
		t.Fatalf("conversations=%s, want 1", n)
	}
}

func TestWebFormJunkSlugFloodFromOneAddressDoesNotBlockOtherVisitors(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	limited := 0
	for i := 0; i < 60; i++ {
		if f.submit(fmt.Sprintf("junk-%d", i), `{"email":"flood@example.com"}`, "", "203.0.113.50:1").Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("60 posts to unknown slugs from one address were never limited")
	}
	wantAccepted(t, f.submit(f.slugA, `{"email":"real@example.com"}`, "", "198.51.100.77:1"))
}

func TestWebFormPreflightCountsTowardTheAddressCeiling(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)
	var last *httptest.ResponseRecorder
	for i := 0; i < 60; i++ {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/public/forms/"+f.slugA, nil)
		req.Header.Set("Origin", siteOrigin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.RemoteAddr = "203.0.113.60:1"
		last = serve(f.api, req)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("60th preflight status=%d, want 429", last.Code)
	}
}

func TestWebFormAnswersFromAnAllowedOriginAreReadableWhenLimitedOrRefused(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)
	for i := 0; i < 5; i++ {
		wantAccepted(t, f.submit(f.slugA, `{"email":"cors@example.com"}`, siteOrigin, "198.51.100.90:1"))
	}
	limited := f.submit(f.slugA, `{"email":"cors@example.com"}`, siteOrigin, "198.51.100.90:1")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Access-Control-Allow-Origin") != siteOrigin {
		t.Fatalf("429: status=%d headers=%v, want ACAO %q", limited.Code, limited.Header(), siteOrigin)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+f.slugA, strings.NewReader(`{"email":"a@example.com"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", siteOrigin)
	req.RemoteAddr = "198.51.100.91:1"
	wrong := serve(f.api, req)
	if wrong.Code != http.StatusUnsupportedMediaType || wrong.Header().Get("Access-Control-Allow-Origin") != siteOrigin {
		t.Fatalf("415: status=%d headers=%v, want ACAO %q", wrong.Code, wrong.Header(), siteOrigin)
	}
}

func TestWebFormClientAddressIgnoresThePortOfForwardedForEntries(t *testing.T) {
	f := newWebFormFixture(t, func(config *httpapi.Config) {
		config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}
	})
	f.enable(t, f.adminA)
	for _, format := range []string{"198.51.100.20:%d", "[2001:db8:1:1::5]:%d"} {
		for port := 4711; port < 4716; port++ {
			wantAccepted(t, forwardedSubmit(f, "172.16.0.2:1", fmt.Sprintf(format, port)))
		}
		if response := forwardedSubmit(f, "172.16.0.2:1", fmt.Sprintf(format, 5000)); response.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: sixth request status=%d, want 429: a new port is not a new client", format, response.Code)
		}
	}
	// Another client behind the same proxy still gets through; with the port unparsed both would count as the proxy.
	wantAccepted(t, forwardedSubmit(f, "172.16.0.2:1", "198.51.100.21:4711"))
}

func TestWebFormMarksTheMessageAsAnUnverifiedWebFormSender(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	wantAccepted(t, f.submit(f.slugA, `{"email":"stranger@example.com","message":"Hi"}`, "", ""))
	conversation := f.conversations(t, f.adminA)[0]
	type timeline struct {
		Items []struct {
			Email *struct {
				From           string `json:"from"`
				Authentication string `json:"authentication"`
				Via            string `json:"via"`
			} `json:"email"`
		} `json:"items"`
	}
	read := func() timeline {
		var messages timeline
		decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations/"+conversation.ID+"/messages", nil, f.adminA, nil), &messages)
		if len(messages.Items) != 1 || messages.Items[0].Email == nil {
			t.Fatalf("messages=%+v, want one with email metadata", messages.Items)
		}
		return messages
	}
	email := read().Items[0].Email
	if email.From != "stranger@example.com" || email.Authentication != "none" || email.Via != "web_form" {
		t.Fatalf("email=%+v, want from stranger@example.com, authentication none, via web_form", *email)
	}
	// An agent removing the tag does not remove the mark on the message.
	var tags struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/tags", nil, f.adminA, nil), &tags)
	for _, tag := range tags.Items {
		if tag.Name == "web-form" {
			if response := request(t, f.api, http.MethodDelete, "/api/v1/conversations/"+conversation.ID+"/tags/"+tag.ID, nil, f.adminA, nil); response.Code != http.StatusOK {
				t.Fatalf("remove tag status=%d", response.Code)
			}
		}
	}
	if kept := read().Items[0].Email; kept.Via != "web_form" {
		t.Fatalf("after removing the tag: %+v, want the web_form mark kept", *kept)
	}
}

func TestWebFormSpamCannotBlockInboundEmail(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	secret := connectEmail(t, f.api, f.adminA, "support@form-alpha.example.com")
	accepted := 0
	for i := 0; i < 80; i++ {
		response := f.submit(f.slugA, `{"email":"spam@example.com"}`, "", fmt.Sprintf("10.2.0.%d:1", i+1))
		if response.Code == http.StatusAccepted {
			accepted++
			continue
		}
		var body apiError
		decode(t, response, &body)
		if response.Code != http.StatusTooManyRequests || body.Error.Code != "rate_limited" {
			t.Fatalf("submission %d: status=%d body=%s", i, response.Code, response.Body.String())
		}
	}
	if accepted != 50 {
		t.Fatalf("accepted=%d, want the web form's own ceiling of 50 per 10 minutes", accepted)
	}
	result := inboundOK(t, f.api, secret, mail([]string{
		"From: customer@customer.example", "To: support@form-alpha.example.com", "Subject: Real question", "Message-ID: <real-after-spam@customer.example>",
	}, "Please help."))
	if result.Status != "created" {
		t.Fatalf("inbound email after form spam: %+v, want created", result)
	}
}

func TestTrialRepliesToWebFormConversationsAreRefused(t *testing.T) {
	f := newLimitsFixture(t, true)
	slug := workspaceSlug(t, f.api, f.adminA)
	body := map[string]any{"enabled": true, "allowed_origins": []string{}}
	if response := request(t, f.api, http.MethodPut, "/api/v1/channels/web-form", body, f.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", response.Code, response.Body.String())
	}
	wantAccepted(t, submitForm(f.api, slug, `{"email":"anyone@victim.example","message":"hello"}`, "", "192.0.2.9:1"))
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, f.adminA, nil), &page)
	// Anyone can submit any address, so the form must not turn the trial's mail relay into a way to write to it.
	wantStatus(t, f.reply(t, f.adminA, page.Items[0].ID), http.StatusForbidden, restrictedCode, "trial reply to a web form conversation")
	if n := f.outboxCount(t, f.workspaceA); n != "0" {
		t.Fatalf("email_outbox rows=%s, want 0", n)
	}
}
