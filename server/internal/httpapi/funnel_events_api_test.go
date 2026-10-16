package httpapi_test

import (
	"net/http"
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
