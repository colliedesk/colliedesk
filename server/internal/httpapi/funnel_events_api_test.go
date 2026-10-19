package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type funnelEvent struct {
	Event      string
	Workspace  string
	Properties map[string]any
}

// recordingFunnel stands in for PostHog: it keeps what the API recorded.
type recordingFunnel struct {
	mu     sync.Mutex
	events []funnelEvent
}

func (f *recordingFunnel) Record(event, workspaceID string, properties map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, funnelEvent{Event: event, Workspace: workspaceID, Properties: properties})
}

func (f *recordingFunnel) recorded() []funnelEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]funnelEvent(nil), f.events...)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestCloudSignupRecordsTrialStartedOnce(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })

	wantResponse(t, f.signUp(t, "owner@funnel-acme.example.com", "Funnel Acme", defaultPeer), http.StatusCreated, "")
	// The same mailbox cannot start a second trial, so nothing more is counted.
	wantResponse(t, f.signUp(t, "owner@funnel-acme.example.com", "Funnel Acme Two", defaultPeer), http.StatusConflict, "trial_already_used")

	events := funnel.recorded()
	if len(events) != 1 || events[0].Event != "trial_started" {
		t.Fatalf("events=%+v, want one trial_started", events)
	}
	if !uuidPattern.MatchString(events[0].Workspace) {
		t.Fatalf("workspace=%q, want the workspace id, never a slug, name or email", events[0].Workspace)
	}
	if len(events[0].Properties) != 0 {
		t.Fatalf("properties=%v, want none", events[0].Properties)
	}
}

func TestSubscriptionActiveWebhookRecordsActivationOnce(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newBillingFixtureWith(t, func(config *httpapi.Config) { config.Funnel = funnel })

	f.mustDeliver(t, "msg_funnel_founding", f.foundingEvent(f.workspaceA, "sub_funnel_founding"))
	// Polar retries a delivery with the same webhook id; it must not count a second payment.
	f.mustDeliver(t, "msg_funnel_founding", f.foundingEvent(f.workspaceA, "sub_funnel_founding"))
	// A later update of the same subscription is not a new activation.
	updated := activeTeam(f.workspaceA, "sub_funnel_founding", time.Now())
	updated.Product = productFounding
	f.mustDeliver(t, "msg_funnel_founding_update", updated.event("subscription.updated"))
	// Polar sends subscription.active again, under a new webhook id, when a past_due subscription
	// is paid; the workspace was already paying, so it is not a new conversion.
	recovered := activeTeam(f.workspaceA, "sub_funnel_founding", time.Now().Add(time.Minute))
	recovered.Product = productFounding
	f.mustDeliver(t, "msg_funnel_founding_recovered", recovered.event("subscription.active"))

	events := funnel.recorded()
	if len(events) != 1 {
		t.Fatalf("events=%+v, want exactly one subscription_activated", events)
	}
	got := events[0]
	if got.Event != "subscription_activated" || got.Workspace != f.workspaceA || got.Properties["plan"] != "founding" {
		t.Fatalf("event=%+v, want subscription_activated for workspace A with plan founding", got)
	}
}

func TestSubscriptionWithoutWorkspaceRecordsNothing(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newBillingFixtureWith(t, func(config *httpapi.Config) { config.Funnel = funnel })

	orphan := activeTeam("", "sub_funnel_orphan", time.Now())
	f.mustDeliver(t, "msg_funnel_orphan", orphan.event("subscription.active"))

	if events := funnel.recorded(); len(events) != 0 {
		t.Fatalf("events=%+v, want none for a payment that matches no workspace", events)
	}
}

// websiteCookie is the cookie colliedesk.com's PostHog sets on .colliedesk.com once a visitor
// accepts cookies; the app's requests carry it. Its value is URL-encoded JSON.
const websiteCookie = "ph_phc_zDJPtLCpaVyUQg2BbFoLMrzmfQBsfLg6vEJfsTtjyQ3N_posthog"

func (f signupFixture) signUpWithCookies(t *testing.T, email, name string, cookies ...*http.Cookie) {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": name}, f.token(t, map[string]any{"email": email}), nil)
	req.Header.Set("Origin", testOrigin)
	req.RemoteAddr = defaultPeer
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	wantResponse(t, serve(f.api, req), http.StatusCreated, "")
}

func TestCloudSignupLinksTrialToTheWebsiteVisitorWhoAcceptedCookies(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })

	value := url.QueryEscape(`{"distinct_id":"01a1524f-3bd0-7b2b-b2fd-edb4cf9852dc","$sesid":[1760846400000,"01a1524f-3bd0-7b2b-b2fd-edb4cf9852dd",1760846400000],"$epp":true}`)
	f.signUpWithCookies(t, "owner@visitor-acme.example.com", "Visitor Acme", &http.Cookie{Name: websiteCookie, Value: value})

	events := funnel.recorded()
	if len(events) != 1 || events[0].Properties["website_visitor_id"] != "01a1524f-3bd0-7b2b-b2fd-edb4cf9852dc" {
		t.Fatalf("events=%+v, want trial_started with website_visitor_id from the website cookie", events)
	}
	if len(events[0].Properties) != 1 {
		t.Fatalf("properties=%v, want only website_visitor_id", events[0].Properties)
	}
}

func TestCloudSignupIgnoresAWebsiteCookieWithoutAVisitorID(t *testing.T) {
	cases := map[string]*http.Cookie{
		"cookieless sentinel": {Name: websiteCookie, Value: url.QueryEscape(`{"distinct_id":"$posthog_cookieless"}`)},
		"not json":            {Name: websiteCookie, Value: "not-json"},
		"an email as id":      {Name: websiteCookie, Value: url.QueryEscape(`{"distinct_id":"ada@example.com"}`)},
		"another project":     {Name: "ph_phc_other_posthog", Value: url.QueryEscape(`{"distinct_id":"01a1524f-3bd0-7b2b-b2fd-edb4cf9852dc"}`)},
	}
	index := 0
	for name, cookie := range cases {
		t.Run(name, func(t *testing.T) {
			index++
			funnel := &recordingFunnel{}
			f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })
			f.signUpWithCookies(t, fmt.Sprintf("owner%d@cookie-acme.example.com", index), fmt.Sprintf("Cookie Acme %d", index), cookie)
			events := funnel.recorded()
			if len(events) != 1 || len(events[0].Properties) != 0 {
				t.Fatalf("events=%+v, want trial_started without properties", events)
			}
		})
	}
}
