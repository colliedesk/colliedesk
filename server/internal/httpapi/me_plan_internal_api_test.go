package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestMePlanSaysWhetherThePersonIsOnTheTeamRunningThisCloud(t *testing.T) {
	internal, err := httpapi.ParseInternalSignups("@billing-alpha.example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	f := newBillingFixtureWith(t, func(config *httpapi.Config) { config.InternalSignups = internal })

	type view struct {
		Plan *struct {
			Internal *bool `json:"internal"`
		} `json:"plan"`
	}
	read := func(token string) *bool {
		t.Helper()
		response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET /auth/me status=%d body=%s", response.Code, response.Body.String())
		}
		var got view
		decode(t, response, &got)
		if got.Plan == nil {
			t.Fatal("plan is null on a billed server")
		}
		return got.Plan.Internal
	}
	if got := read(f.adminA); got == nil || !*got {
		t.Fatalf("admin@billing-alpha.example.com (on the team list): internal=%v, want true", got)
	}
	if got := read(f.adminB); got == nil || *got {
		t.Fatalf("admin@billing-bravo.example.com (not on the list): internal=%v, want false", got)
	}
}
