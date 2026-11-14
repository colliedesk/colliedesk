package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/metaconversions"
)

// signUpFromLinkWithCookie is signUpFromLink from a browser that also sends cookie, as one that accepted
// cookies on colliedesk.com sends the Meta pixel's _fbp to app.colliedesk.com.
func (f signupFixture) signUpFromLinkWithCookie(t *testing.T, email string, attribution map[string]any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Meta Acme", "attribution": attribution},
		f.token(t, map[string]any{"email": email}), nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) Test/1.0")
	req.RemoteAddr = defaultPeer
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return serve(f.api, req)
}

func sentUserData(t *testing.T, request graphRequest) map[string]any {
	t.Helper()
	return request.Body["data"].([]any)[0].(map[string]any)["user_data"].(map[string]any)
}

func TestSignupFromAMetaAdWithThePixelCookieSendsItsBrowserID(t *testing.T) {
	graph := newFakeGraphAPI(t)
	f, worker := metaSignupFixture(t, graph)
	// The value fbevents.js set on .colliedesk.com in a real visit (2026-11-04).
	const browserID = "fb.1.1793783755161.861984240860650994"
	created := f.signUpFromLinkWithCookie(t, "fbp@meta-fbp.example.com",
		map[string]any{"fbclid": "test123", "click_id_seen_at": "2026-10-20T09:40:00Z"}, &http.Cookie{Name: "_fbp", Value: browserID})
	wantResponse(t, created, http.StatusCreated, "")
	workspaceID := f.meAfter(t, created).Workspace.ID
	if got := queryString(t, f.pool, `SELECT coalesce(fbp,'<null>') FROM meta_conversion_events WHERE workspace_id=$1`, workspaceID); got != browserID {
		t.Fatalf("stored fbp=%q, want %q", got, browserID)
	}

	runMetaWorker(t, worker)
	requests := graph.received()
	if len(requests) != 1 {
		t.Fatalf("Graph API requests=%d, want one StartTrial", len(requests))
	}
	want := map[string]any{
		"fbc": "fb.1.1792489200000.test123", "fbp": browserID,
		"client_ip_address": "203.0.113.7", "client_user_agent": "Mozilla/5.0 (Macintosh) Test/1.0",
	}
	if got := sentUserData(t, requests[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("user_data=%v\nwant %v", got, want)
	}
}

func TestAMissingOrMalformedPixelCookieIsLeftOutAndTheTrialStillReported(t *testing.T) {
	cases := map[string]*http.Cookie{
		"no cookie":              nil,
		"empty":                  {Name: "_fbp", Value: ""},
		"letters in the id":      {Name: "_fbp", Value: "fb.1.1793783755161.86198424abc"},
		"two-digit subdomain":    {Name: "_fbp", Value: "fb.12.1793783755161.861984240860650994"},
		"not fb":                 {Name: "_fbp", Value: "fx.1.1793783755161.861984240860650994"},
		"missing the random id":  {Name: "_fbp", Value: "fb.1.1793783755161"},
		"trailing junk":          {Name: "_fbp", Value: "fb.1.1793783755161.861984240860650994;x"},
		"longer than any real":   {Name: "_fbp", Value: "fb.1.1793783755161." + strings.Repeat("9", 80)},
		"an email in the cookie": {Name: "_fbp", Value: "ada@example.com"},
	}
	for name, cookie := range cases {
		t.Run(name, func(t *testing.T) {
			graph := newFakeGraphAPI(t)
			f, worker := metaSignupFixture(t, graph)
			created := f.signUpFromLinkWithCookie(t, "nofbp@meta-nofbp.example.com", map[string]any{"fbclid": "test123"}, cookie)
			wantResponse(t, created, http.StatusCreated, "")
			workspaceID := f.meAfter(t, created).Workspace.ID
			if got := queryString(t, f.pool, `SELECT coalesce(fbp,'<null>') FROM meta_conversion_events WHERE workspace_id=$1`, workspaceID); got != "<null>" {
				t.Fatalf("stored fbp=%q, want none", got)
			}
			runMetaWorker(t, worker)
			requests := graph.received()
			if len(requests) != 1 {
				t.Fatalf("Graph API requests=%d, want the StartTrial still sent", len(requests))
			}
			if userData := sentUserData(t, requests[0]); userData["fbp"] != nil || userData["fbc"] != "fb.1.1792490400000.test123" {
				t.Fatalf("user_data=%v, want fbc and no fbp", userData)
			}
		})
	}
}

func TestSubscribeCarriesTheBrowserIDOfItsStartTrialAndForgetsItOnceSent(t *testing.T) {
	graph := newFakeGraphAPI(t)
	f := newBillingFixtureWith(t, func(config *httpapi.Config) { config.MetaConversions = graph.config() })
	worker := metaconversions.NewWorker(database.New(f.pool), graph.config(), metaconversions.WorkerOptions{})
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO meta_conversion_events(workspace_id,event_name,event_time,fbc,fbp,client_ip,user_agent,event_source_url,next_attempt_at,attempts,sent_at)
		VALUES($1,'StartTrial',now()-interval '1 day','fb.1.1792489200000.paid1','fb.1.1792489100000.123456789','198.51.100.4','Mozilla/5.0 Paid','https://app.example/sign-in',now(),1,now()-interval '1 day')`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	paid := activeTeam(f.workspaceA, "sub_meta_fbp", time.Now())
	paid.Product, paid.Amount, paid.Currency = productFounding, 9900, "usd"
	f.mustDeliver(t, "msg_meta_fbp", paid.event("subscription.active"))

	runMetaWorker(t, worker)
	requests := graph.received()
	if len(requests) != 1 {
		t.Fatalf("Graph API requests=%d, want one Subscribe", len(requests))
	}
	want := map[string]any{
		"fbc": "fb.1.1792489200000.paid1", "fbp": "fb.1.1792489100000.123456789",
		"client_ip_address": "198.51.100.4", "client_user_agent": "Mozilla/5.0 Paid",
	}
	if got := sentUserData(t, requests[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("Subscribe user_data=%v\nwant %v", got, want)
	}
	// Subscribe is the last report: the browser ID goes with the address and the browser.
	if got := queryString(t, f.pool, `SELECT count(*) FROM meta_conversion_events WHERE workspace_id=$1 AND fbp IS NOT NULL`, f.workspaceA); got != "0" {
		t.Fatalf("rows still keeping fbp after Subscribe was sent=%s, want 0", got)
	}
}
