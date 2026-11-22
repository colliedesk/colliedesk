package hcanalytics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
)

// answering is a stub that answers each path with a fixed body and headers, and keeps the paths it got.
func answering(t *testing.T, header map[string]string, bodies map[string]string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		for key, value := range header {
			w.Header().Set(key, value)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bodies[r.URL.Path]))
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), paths...)
	}
}

func TestAGA4TestNamesWhatGoogleFoundWrong(t *testing.T) {
	ga4, paths := answering(t, nil, map[string]string{"/debug/mp/collect": `{"validationMessages":[{"fieldPath":"measurement_id",
		"description":"Measurement ID G-TEST1234 is not a web stream of this property.","validationCode":"VALUE_INVALID"}]}`})
	sender := hcanalytics.New(&memoryStore{}, hcanalytics.Options{GA4Base: ga4.URL, Origin: "https://desk.example.com",
		AllowDial: map[string]bool{strings.TrimPrefix(ga4.URL, "http://"): true}})
	result := sender.Test(context.Background(), ga4Target("w1"), readerAgent, "192.0.2.10")
	if result.OK || !strings.Contains(result.Message, "Measurement ID G-TEST1234 is not a web stream of this property.") {
		t.Fatalf("result = %+v, want Google's validation message", result)
	}
	if got := paths(); len(got) != 1 || got[0] != "/debug/mp/collect" {
		t.Fatalf("paths = %v, want only the validation endpoint after a refusal", got)
	}
}

func TestAValidGA4TestSendsOnePageViewAndSaysTheSecretIsNotChecked(t *testing.T) {
	ga4, paths := answering(t, nil, map[string]string{"/debug/mp/collect": `{"validationMessages":[]}`})
	sender := hcanalytics.New(&memoryStore{}, hcanalytics.Options{GA4Base: ga4.URL, Origin: "https://desk.example.com",
		AllowDial: map[string]bool{strings.TrimPrefix(ga4.URL, "http://"): true}})
	result := sender.Test(context.Background(), ga4Target("w1"), readerAgent, "192.0.2.10")
	if !result.OK || !strings.Contains(result.Message, "Realtime") || !strings.Contains(result.Message, "API secret") {
		t.Fatalf("result = %+v", result)
	}
	if got := paths(); len(got) != 2 || got[0] != "/debug/mp/collect" || got[1] != "/mp/collect" {
		t.Fatalf("paths = %v, want the validation endpoint then one page view", got)
	}
}

func TestAPlausibleTestSaysWhenTheBotFilterDroppedTheEvent(t *testing.T) {
	plausible, _ := answering(t, map[string]string{"X-Plausible-Dropped": "1"}, nil)
	sender := hcanalytics.New(&memoryStore{}, hcanalytics.Options{Origin: "https://desk.example.com",
		AllowDial: map[string]bool{strings.TrimPrefix(plausible.URL, "http://"): true}})
	target := hcanalytics.Target{WorkspaceID: "w2", Provider: hcanalytics.Plausible, Domain: "help.acme.example", BaseURL: plausible.URL}
	if result := sender.Test(context.Background(), target, readerAgent, "192.0.2.10"); result.OK || !strings.Contains(result.Message, "dropped") {
		t.Fatalf("result = %+v, want the drop named", result)
	}
}

func TestAPlausibleTestThatIsAcceptedSaysWhereToLook(t *testing.T) {
	plausible, paths := answering(t, nil, nil)
	sender := hcanalytics.New(&memoryStore{}, hcanalytics.Options{Origin: "https://desk.example.com",
		AllowDial: map[string]bool{strings.TrimPrefix(plausible.URL, "http://"): true}})
	target := hcanalytics.Target{WorkspaceID: "w2", Provider: hcanalytics.Plausible, Domain: "help.acme.example", BaseURL: plausible.URL}
	result := sender.Test(context.Background(), target, readerAgent, "192.0.2.10")
	if !result.OK || !strings.Contains(result.Message, "help.acme.example") {
		t.Fatalf("result = %+v", result)
	}
	if got := paths(); len(got) != 1 || got[0] != "/api/event" {
		t.Fatalf("paths = %v", got)
	}
}

func TestATestOfAPrivatePlausibleNamesTheAddress(t *testing.T) {
	plausible, paths := answering(t, nil, nil)
	sender := hcanalytics.New(&memoryStore{}, hcanalytics.Options{Origin: "https://desk.example.com"})
	target := hcanalytics.Target{WorkspaceID: "w2", Provider: hcanalytics.Plausible, Domain: "help.acme.example", BaseURL: plausible.URL}
	result := sender.Test(context.Background(), target, readerAgent, "192.0.2.10")
	if result.OK || result.Message != "Plausible is at 127.0.0.1, a private address; Colliedesk sends only to public addresses." {
		t.Fatalf("result = %+v", result)
	}
	if len(paths()) != 0 {
		t.Fatal("the private address was reached")
	}
}
