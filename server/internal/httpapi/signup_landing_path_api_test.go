package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestSignupStoresTheSitePageItCameFromAndCountsItWithTheTrial(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })
	created := f.signUpWith(t, "ada@landing.example.com", defaultPeer, map[string]any{
		"workspace_name": "Landed Acme", "source": "search",
		"attribution": map[string]any{"landing_path": "/vs/zendesk", "utm_source": "google"},
	})
	wantResponse(t, created, http.StatusCreated, "")
	me := f.meAfter(t, created)
	if got := queryString(t, f.pool, `SELECT concat_ws('/', utm_source, landing_path) FROM workspace_onboarding WHERE workspace_id=$1`, me.Workspace.ID); got != "google//vs/zendesk" {
		t.Fatalf("stored attribution=%q, want google//vs/zendesk", got)
	}
	events := funnel.named("trial_started")
	if len(events) != 1 {
		t.Fatalf("trial_started=%+v, want one", events)
	}
	properties := events[0].Properties
	if len(properties) != 3 || properties["source"] != "search" || properties["utm_source"] != "google" || properties["landing_path"] != "/vs/zendesk" {
		t.Fatalf("trial_started properties=%v, want source, utm_source and landing_path=/vs/zendesk", properties)
	}
}

func TestSignupDropsALandingPathThatIsNotAPlainSitePath(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })
	created := f.signUpWith(t, "eve@landing.example.com", defaultPeer, map[string]any{
		"workspace_name": "Unlanded Acme", "source": "search",
		"attribution": map[string]any{"landing_path": "https://evil.com/vs"},
	})
	wantResponse(t, created, http.StatusCreated, "")
	me := f.meAfter(t, created)
	if got := queryString(t, f.pool, `SELECT COALESCE(landing_path,'no-page') FROM workspace_onboarding WHERE workspace_id=$1`, me.Workspace.ID); got != "no-page" {
		t.Fatalf("stored landing path=%q, want none", got)
	}
	events := funnel.named("trial_started")
	if len(events) != 1 {
		t.Fatalf("trial_started=%+v, want one", events)
	}
	if properties := events[0].Properties; len(properties) != 1 || properties["source"] != "search" {
		t.Fatalf("trial_started properties=%v, want only source (no landing_path)", properties)
	}
}

func TestFirstPaymentCarriesTheSitePageOfTheSignup(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newBillingFixtureWith(t, func(config *httpapi.Config) { config.Funnel = funnel })
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id, source, landing_path) VALUES($1,'search','/vs/zendesk')`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	f.mustDeliver(t, "msg_landed_founding", f.foundingEvent(f.workspaceA, "sub_landed_founding"))

	events := funnel.named("subscription_activated")
	if len(events) != 1 {
		t.Fatalf("events=%+v, want one subscription_activated", events)
	}
	if properties := events[0].Properties; len(properties) != 2 || properties["plan"] != "founding" || properties["landing_path"] != "/vs/zendesk" {
		t.Fatalf("subscription_activated properties=%v, want plan founding and landing_path=/vs/zendesk", properties)
	}
}
