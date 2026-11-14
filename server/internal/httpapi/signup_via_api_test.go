package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// A sign-up whose sign-in returns to an AI app's consent page (/oauth/authorize) came through that app's
// connector; every other sign-up came from the web. Only the page path of this server counts: a query that
// mentions the page, a look-alike path or another host does not.
func TestCloudSignupSaysWhetherAnAIAppConnectorStartedIt(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })

	cases := []struct {
		next, want string
	}{
		{"/oauth/authorize?client_id=https%3A%2F%2Fclaude.ai%2Fclient.json&response_type=code&state=abc", "ai_app_connector"},
		{"/oauth/authorize", "ai_app_connector"},
		{testOrigin + "/oauth/authorize?client_id=abc", "ai_app_connector"},
		{"", "web"},
		{"/inbox", "web"},
		{"/inbox?next=/oauth/authorize", "web"},
		{"/oauth/authorize-not?client_id=abc", "web"},
		{"/oauth/authorize/extra", "web"},
		{"https://evil.example/oauth/authorize?client_id=abc", "web"},
		{"//evil.example/oauth/authorize?client_id=abc", "web"},
		{"/trial-offer?offer=abc", "web"},
	}
	for i, c := range cases {
		body := map[string]any{"workspace_name": fmt.Sprintf("Via Test %d", i)}
		if c.next != "" {
			body["next"] = c.next
		}
		peer := fmt.Sprintf("203.0.113.%d:40000", 100+i)
		wantResponse(t, f.signUpWith(t, fmt.Sprintf("owner%d@via.example.com", i), peer, body), http.StatusCreated, "")
	}

	events := funnel.named("trial_started")
	if len(events) != len(cases) {
		t.Fatalf("events=%+v, want one trial_started per sign-up", events)
	}
	for i, c := range cases {
		if got := events[i].Properties["signup_via"]; got != c.want {
			t.Errorf("next %q: signup_via=%#v, want %q (properties %v)", c.next, got, c.want, events[i].Properties)
		}
	}
}
