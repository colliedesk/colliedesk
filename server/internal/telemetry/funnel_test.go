package telemetry_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/telemetry"
)

// capturedEvents is a stand-in for the PostHog capture endpoint.
type capturedEvents struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func newCaptureServer(t *testing.T) (*httptest.Server, *capturedEvents) {
	t.Helper()
	captured := &capturedEvents{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/i/v0/e/" {
			t.Errorf("path=%s, want /i/v0/e/", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body is not JSON: %s", raw)
		}
		captured.mu.Lock()
		captured.bodies = append(captured.bodies, body)
		captured.mu.Unlock()
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func cloudConfig(endpoint string) telemetry.Config {
	return telemetry.Config{Endpoint: endpoint, ProjectKey: "phc_test", Version: "0.1.0", Realm: "cloud", Domain: "app.colliedesk.com"}
}

func TestFunnelSendsCloudEventKeyedByWorkspace(t *testing.T) {
	server, captured := newCaptureServer(t)
	funnel := telemetry.NewFunnel(cloudConfig(server.URL), nil)

	err := funnel.Send(context.Background(), "subscription_activated", "0b6c2f1e-1111-4222-8333-944455556666", map[string]any{"plan": "founding"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(captured.bodies) != 1 {
		t.Fatalf("got %d events, want 1", len(captured.bodies))
	}
	body := captured.bodies[0]
	if body["event"] != "subscription_activated" || body["distinct_id"] != "0b6c2f1e-1111-4222-8333-944455556666" || body["api_key"] != "phc_test" {
		t.Fatalf("event=%v distinct_id=%v api_key=%v", body["event"], body["distinct_id"], body["api_key"])
	}
	properties, _ := body["properties"].(map[string]any)
	if properties["plan"] != "founding" || properties["realm"] != "cloud" || properties["version"] != "0.1.0" {
		t.Fatalf("properties=%v", properties)
	}
	if properties["$process_person_profile"] != false {
		t.Fatalf("$process_person_profile=%v, want false: a workspace is not a person", properties["$process_person_profile"])
	}
}

func TestFunnelSendsNothingOffTheCloud(t *testing.T) {
	cases := map[string]func(telemetry.Config) telemetry.Config{
		"self-hosted": func(c telemetry.Config) telemetry.Config { c.Realm = "self-hosted"; return c },
		"opted out":   func(c telemetry.Config) telemetry.Config { c.OffReason = "DISABLE_TELEMETRY is set"; return c },
		"no key":      func(c telemetry.Config) telemetry.Config { c.ProjectKey = ""; return c },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			server, captured := newCaptureServer(t)
			funnel := telemetry.NewFunnel(mutate(cloudConfig(server.URL)), nil)
			if err := funnel.Send(context.Background(), "trial_started", "0b6c2f1e-1111-4222-8333-944455556666", nil); err != nil {
				t.Fatalf("Send: %v", err)
			}
			if len(captured.bodies) != 0 {
				t.Fatalf("sent %d events, want none", len(captured.bodies))
			}
		})
	}
}

func TestFunnelReportsRejectedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	err := telemetry.NewFunnel(cloudConfig(server.URL), nil).Send(context.Background(), "trial_started", "0b6c2f1e-1111-4222-8333-944455556666", nil)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("err=%v, want PostHog answered 400", err)
	}
}
