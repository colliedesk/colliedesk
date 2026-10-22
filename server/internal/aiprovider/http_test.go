package aiprovider

import (
	"context"
	"net/http"
	"testing"
)

func TestGuardedClientRefusesPrivateAddresses(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	guarded, err := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1", HTTPClient: NewHTTPClient(HTTPOptions{Guarded: true})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guarded.Complete(context.Background(), draftRequest()); CodeOf(err) != ErrorUnavailable {
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
