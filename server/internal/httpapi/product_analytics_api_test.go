package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The hosted cloud counts how a workspace gets going: its email channel, its replies and its
// teammates. Each event is keyed by the workspace id and never carries an address or any text.
func TestWorkspaceActivationEventsAreRecorded(t *testing.T) {
	funnel := &recordingFunnel{}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Funnel = funnel })
	admin := createSession(t, api, service, "Activation Acme", "admin@activation-acme.example.com")

	if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "support@activation-acme.example.com"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("PUT email channel status=%d body=%s", response.Code, response.Body.String())
	}
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@customer.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer jam"})
	create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "Internal note."})
	create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Try turning it off and on."})
	if response := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "teammate@activation-acme.example.com", "role_ceiling": "agent"}, admin, nil); response.Code != http.StatusCreated {
		t.Fatalf("POST invite status=%d body=%s", response.Code, response.Body.String())
	}

	events := funnel.recorded()
	var names []string
	for _, event := range events {
		names = append(names, event.Event)
		if !uuidPattern.MatchString(event.Workspace) || event.Workspace != events[0].Workspace {
			t.Fatalf("event %+v: want the same workspace id on every event", event)
		}
		for key, value := range event.Properties {
			if text, ok := value.(string); ok && strings.ContainsAny(text, "@ ") {
				t.Fatalf("event %s property %s=%q: no address or text may leave the workspace", event.Event, key, text)
			}
		}
	}
	if got := strings.Join(names, ","); got != "email_channel_saved,reply_sent,teammate_invited" {
		t.Fatalf("events=%s, want email_channel_saved,reply_sent,teammate_invited (a note is not a reply)", got)
	}
	if by := events[1].Properties["by"]; by != "person" {
		t.Fatalf("reply_sent by=%v, want person", by)
	}
}

func TestConfigOffersProductAnalyticsUnlessTelemetryIsOff(t *testing.T) {
	type config struct {
		ProductAnalytics struct {
			Enabled bool    `json:"enabled"`
			Key     *string `json:"key"`
			Host    *string `json:"host"`
			Realm   *string `json:"realm"`
		} `json:"product_analytics"`
	}
	read := func(api http.Handler) config {
		response := request(t, api, http.MethodGet, "/api/v1/config", nil, "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET config status=%d", response.Code)
		}
		var got config
		decode(t, response, &got)
		return got
	}

	t.Run("telemetry off", func(t *testing.T) {
		api, _ := testAPI(t)
		if got := read(api); got.ProductAnalytics.Enabled || got.ProductAnalytics.Key != nil || got.ProductAnalytics.Host != nil || got.ProductAnalytics.Realm != nil {
			t.Fatalf("product_analytics=%+v, want disabled with no key", got.ProductAnalytics)
		}
	})
	t.Run("self-hosted", func(t *testing.T) {
		api, _, _, _ := testAPIWithConfig(t, func(c *httpapi.Config) {
			c.ProductAnalytics = &httpapi.ProductAnalytics{Key: "phc_cloudkey", Host: "https://us.i.posthog.com", Realm: "self-hosted"}
		})
		if got := read(api); !got.ProductAnalytics.Enabled || got.ProductAnalytics.Realm == nil || *got.ProductAnalytics.Realm != "self-hosted" {
			t.Fatalf("product_analytics=%+v, want enabled for realm self-hosted", got.ProductAnalytics)
		}
	})
	t.Run("cloud", func(t *testing.T) {
		api, _, _, _ := testAPIWithConfig(t, func(c *httpapi.Config) {
			c.ProductAnalytics = &httpapi.ProductAnalytics{Key: "phc_cloudkey", Host: "https://us.i.posthog.com", Realm: "cloud"}
		})
		got := read(api)
		if !got.ProductAnalytics.Enabled || got.ProductAnalytics.Key == nil || *got.ProductAnalytics.Key != "phc_cloudkey" || got.ProductAnalytics.Host == nil || *got.ProductAnalytics.Host != "https://us.i.posthog.com" || got.ProductAnalytics.Realm == nil || *got.ProductAnalytics.Realm != "cloud" {
			t.Fatalf("product_analytics=%+v, want enabled with the key and host", got.ProductAnalytics)
		}
	})
}
