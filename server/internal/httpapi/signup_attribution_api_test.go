package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestSignupStoresTheAdLinkItCameFromAndCountsItWithTheTrial(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })
	seen := signupStart.Add(-20 * time.Minute)
	created := f.signUpWith(t, "ada@attribution.example.com", defaultPeer, map[string]any{
		"workspace_name": "Attributed Acme", "source": "tiktok",
		"attribution": map[string]any{
			"utm_source": "tiktok", "utm_medium": "paid", "utm_campaign": "launch", "utm_content": "post-07", "utm_term": "helpdesk",
			"fbclid": "IwAR_x-1.y", "click_id_seen_at": seen.Format(time.RFC3339),
			// Dropped: a key that is not listed, and a listed key whose value is not a plain token.
			"gclid": "abc", "ttclid": "has space",
		},
	})
	wantResponse(t, created, http.StatusCreated, "")
	me := f.meAfter(t, created)
	got := queryString(t, f.pool, `SELECT concat_ws('/', utm_source, utm_medium, utm_campaign, utm_content, utm_term, COALESCE(ttclid,'no-ttclid'), fbclid,
		to_char(click_id_seen_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS')) FROM workspace_onboarding WHERE workspace_id=$1`, me.Workspace.ID)
	if want := "tiktok/paid/launch/post-07/helpdesk/no-ttclid/IwAR_x-1.y/2026-10-20T09:40:00"; got != want {
		t.Fatalf("stored attribution=%q, want %q", got, want)
	}
	events := funnel.named("trial_started")
	if len(events) != 1 {
		t.Fatalf("trial_started=%+v, want one", events)
	}
	properties := events[0].Properties
	want := map[string]string{"source": "tiktok", "utm_source": "tiktok", "utm_medium": "paid", "utm_campaign": "launch", "utm_content": "post-07", "utm_term": "helpdesk", "fbclid": "IwAR_x-1.y"}
	if len(properties) != len(want) {
		t.Fatalf("trial_started properties=%v, want exactly %v", properties, want)
	}
	for key, value := range want {
		if properties[key] != value {
			t.Fatalf("trial_started %s=%v, want %s (all: %v)", key, properties[key], value, properties)
		}
	}
}

func TestSignupWithoutAnAdLinkStoresNoAttribution(t *testing.T) {
	f := newSignupFixture(t, nil)
	created := f.signUpWith(t, "plain@attribution.example.com", defaultPeer, map[string]any{"workspace_name": "Plain Acme", "attribution": map[string]any{"utm_source": "a b"}})
	wantResponse(t, created, http.StatusCreated, "")
	me := f.meAfter(t, created)
	got := queryString(t, f.pool, `SELECT num_nonnulls(utm_source, utm_medium, utm_campaign, utm_content, utm_term, ttclid, fbclid, click_id_seen_at)::text FROM workspace_onboarding WHERE workspace_id=$1`, me.Workspace.ID)
	if got != "0" {
		t.Fatalf("attribution values stored=%s, want 0", got)
	}
}

func TestSignupRefusesAttributionThatIsNotText(t *testing.T) {
	f := newSignupFixture(t, nil)
	wantResponse(t, f.signUpWith(t, "typed@attribution.example.com", defaultPeer, map[string]any{
		"workspace_name": "Typed Acme", "attribution": map[string]any{"utm_source": 7},
	}), http.StatusBadRequest, "invalid_request")
	if got := f.count(t, "workspaces"); got != "0" {
		t.Fatalf("workspaces=%s after a refused signup, want 0", got)
	}
}

func TestFirstPaymentCarriesTheAdLinkOfTheSignup(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newBillingFixtureWith(t, func(config *httpapi.Config) { config.Funnel = funnel })
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id, source, utm_source, utm_content, fbclid, click_id_seen_at) VALUES($1,'tiktok','tiktok','post-07','IwAR_x',now())`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	f.mustDeliver(t, "msg_attributed_founding", f.foundingEvent(f.workspaceA, "sub_attributed_founding"))
	f.mustDeliver(t, "msg_unattributed_founding", f.foundingEvent(f.workspaceB, "sub_unattributed_founding"))

	events := funnel.named("subscription_activated")
	if len(events) != 2 {
		t.Fatalf("events=%+v, want two subscription_activated", events)
	}
	byWorkspace := map[string]map[string]any{}
	for _, event := range events {
		byWorkspace[event.Workspace] = event.Properties
	}
	attributed := byWorkspace[f.workspaceA]
	if len(attributed) != 4 || attributed["plan"] != "founding" || attributed["utm_source"] != "tiktok" || attributed["utm_content"] != "post-07" || attributed["fbclid"] != "IwAR_x" {
		t.Fatalf("workspace A properties=%v, want plan founding with utm_source, utm_content and fbclid of its signup", attributed)
	}
	if plain := byWorkspace[f.workspaceB]; len(plain) != 1 || plain["plan"] != "founding" {
		t.Fatalf("workspace B properties=%v, want only plan founding", plain)
	}
}
