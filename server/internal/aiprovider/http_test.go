package aiprovider

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestGuardedClientRefusesPrivateAddresses(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	guarded, err := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1", HTTPClient: NewHTTPClient(HTTPOptions{Guarded: true})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guarded.Complete(context.Background(), draftRequest()); CodeOf(err) != ErrorBlockedAddress {
		t.Fatalf("guarded call to loopback: %v", err)
	}
	if hits := len(fake.recorded(t)); hits != 0 {
		t.Fatalf("guarded client reached loopback %d times", hits)
	}

	allowed := NewHTTPClient(HTTPOptions{Guarded: true, AllowDial: map[string]bool{fake.server.Listener.Addr().String(): true}})
	provider, _ := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1", HTTPClient: allowed})
	if _, err := provider.Complete(context.Background(), draftRequest()); err != nil {
		t.Fatalf("allowed address: %v", err)
	}

	open := NewHTTPClient(HTTPOptions{})
	provider, _ = NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1", HTTPClient: open})
	if _, err := provider.Complete(context.Background(), draftRequest()); err != nil {
		t.Fatalf("unguarded client (operator's own network): %v", err)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	fake := newFakeAPI(t, http.StatusTemporaryRedirect, ``)
	fake.header.Set("Location", "http://169.254.169.254/latest/meta-data")
	provider, _ := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1", HTTPClient: NewHTTPClient(HTTPOptions{})})
	if _, err := provider.Complete(context.Background(), draftRequest()); CodeOf(err) != ErrorInvalidResponse {
		t.Fatalf("redirect: %v", err)
	}
}

func TestRetryAfterIsBounded(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"7": 7 * time.Second, "1e12": maxRetryAfter, "Inf": maxRetryAfter, "9223372037": maxRetryAfter,
		"NaN": 0, "-3": 0, "soon": 0, "": 0,
		time.Now().Add(1000 * time.Hour).UTC().Format(http.TimeFormat): maxRetryAfter,
	} {
		if got := retryAfter(http.Header{"Retry-After": []string{value}}); got != want {
			t.Fatalf("Retry-After %q = %v, want %v", value, got, want)
		}
	}
}
