package google_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/google/googletest"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const redirectURI = "https://desk.example.com/api/v1/channels/email/gmail/callback"

func standIn(t *testing.T) (*googletest.Server, google.Config) {
	t.Helper()
	fake := googletest.New("client-1", "not-a-secret")
	server := fake.Start()
	t.Cleanup(server.Close)
	keys, _ := tokenseal.ParseKeys("k1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	return fake, google.Config{ClientID: "client-1", ClientSecret: "not-a-secret", Keys: keys, Base: server.URL}
}

func consent(t *testing.T, fake *googletest.Server, config google.Config, account string) (code string) {
	t.Helper()
	target, err := fake.Consent(config.AuthorizationURL(redirectURI, "state-1", "verifier-1"), account)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	if parsed.Query().Get("state") != "state-1" {
		t.Fatalf("state not returned: %s", target)
	}
	return parsed.Query().Get("code")
}

func TestConsentExchangeRefreshSendRevoke(t *testing.T) {
	fake, config := standIn(t)
	authURL := config.AuthorizationURL(redirectURI, "state-1", "verifier-1")
	for _, want := range []string{"access_type=offline", "prompt=consent", "code_challenge_method=S256", "gmail.send"} {
		if !strings.Contains(authURL, want) {
			t.Errorf("authorization URL lacks %s: %s", want, authURL)
		}
	}
	grant, err := config.Exchange(context.Background(), consent(t, fake, config, "Support@Acme.example"), "verifier-1", redirectURI)
	if err != nil || grant.Address != "support@acme.example" || grant.RefreshToken == "" {
		t.Fatalf("exchange: %+v %v", grant, err)
	}
	if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@b.example"), "wrong-verifier", redirectURI); !errors.Is(err, google.ErrRevoked) {
		t.Fatalf("wrong PKCE verifier: %v", err)
	}
	access, _, err := config.AccessToken(context.Background(), grant.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Send(context.Background(), access, []byte("From: support@acme.example\r\nTo: c@d.example\r\nMessage-ID: <x@acme.example>\r\n\r\nhi\r\n")); err != nil {
		t.Fatal(err)
	}
	if sent := fake.Messages(); len(sent) != 1 || sent[0].Account != "support@acme.example" || sent[0].MessageID != "x@acme.example" {
		t.Fatalf("sent: %+v", sent)
	}
	if err := config.Revoke(context.Background(), grant.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.AccessToken(context.Background(), grant.RefreshToken); !errors.Is(err, google.ErrRevoked) {
		t.Fatalf("refresh after revoke: %v", err)
	}
	if err := config.Revoke(context.Background(), grant.RefreshToken); err != nil {
		t.Fatalf("revoking twice must succeed: %v", err)
	}
}

func TestExchangeRefusesWhatCannotSend(t *testing.T) {
	fake, config := standIn(t)
	fake.Set(func(s *googletest.Server) { s.GrantScope = "openid email" })
	if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@b.example"), "verifier-1", redirectURI); !errors.Is(err, google.ErrSendNotGranted) {
		t.Fatalf("unticked gmail.send: %v", err)
	}
	fake.Set(func(s *googletest.Server) { s.GrantScope = ""; s.OmitRefreshToken = true })
	if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@b.example"), "verifier-1", redirectURI); !errors.Is(err, google.ErrNoRefreshToken) {
		t.Fatalf("no refresh token: %v", err)
	}
	// An ID token issued to another client is not ours to believe.
	fake.Set(func(s *googletest.Server) { s.OmitRefreshToken = false; s.IDTokenAudience = "client-2" })
	if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@b.example"), "verifier-1", redirectURI); !errors.Is(err, google.ErrNoAddress) {
		t.Fatalf("ID token for another audience: %v", err)
	}
}

func TestGmailErrorsAreClassified(t *testing.T) {
	fake, config := standIn(t)
	grant, err := config.Exchange(context.Background(), consent(t, fake, config, "a@b.example"), "verifier-1", redirectURI)
	if err != nil {
		t.Fatal(err)
	}
	access, _, _ := config.AccessToken(context.Background(), grant.RefreshToken)
	cases := []struct {
		failure           googletest.Failure
		temporary, broken bool
	}{
		{googletest.Failure{Status: http.StatusForbidden, Reason: "userRateLimitExceeded"}, true, false},
		{googletest.Failure{Status: http.StatusForbidden, Reason: "dailyLimitExceeded"}, true, false},
		{googletest.Failure{Status: http.StatusTooManyRequests, Reason: "rateLimitExceeded"}, true, false},
		{googletest.Failure{Status: http.StatusServiceUnavailable, Reason: "backendError"}, true, false},
		{googletest.Failure{Status: http.StatusForbidden, Reason: "insufficientPermissions"}, false, true},
		{googletest.Failure{Status: http.StatusBadRequest, Reason: "invalidArgument"}, false, false},
	}
	for _, item := range cases {
		fake.Set(func(s *googletest.Server) { s.SendFailures = []googletest.Failure{item.failure} })
		err := config.Send(context.Background(), access, []byte("To: c@d.example\r\n\r\nhi\r\n"))
		var apiErr *google.APIError
		if !errors.As(err, &apiErr) || apiErr.Temporary() != item.temporary || apiErr.ConnectionBroken() != item.broken {
			t.Errorf("%+v: err=%v temporary/broken want %v/%v", item.failure, err, item.temporary, item.broken)
		}
	}
	if err := config.Send(context.Background(), "expired", []byte("To: c@d.example\r\n\r\nhi\r\n")); !errors.Is(err, google.ErrUnauthorized) {
		t.Fatalf("401: %v", err)
	}
}

func TestConfigNamesWhatIsMissing(t *testing.T) {
	config, err := google.ConfigFromEnv(func(string) string { return "" })
	if err != nil || config.Enabled() || strings.Join(config.Missing(), ",") != "GOOGLE_OAUTH_CLIENT_ID,GOOGLE_OAUTH_CLIENT_SECRET,MAILBOX_TOKEN_KEYS" {
		t.Fatalf("empty env: %v %v", config.Missing(), err)
	}
	if _, err := google.ConfigFromEnv(func(key string) string {
		return map[string]string{"MAILBOX_TOKEN_KEYS": "nope"}[key]
	}); err == nil {
		t.Fatal("malformed key ring accepted")
	}
}
