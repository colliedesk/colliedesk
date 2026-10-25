package microsoft_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/microsoft/microsofttest"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const redirectURI = "https://desk.example.com/api/v1/channels/email/microsoft/callback"

func standIn(t *testing.T) (*microsofttest.Server, microsoft.Config) {
	t.Helper()
	fake := microsofttest.New("client-1", "not-a-secret")
	server := fake.Start()
	t.Cleanup(server.Close)
	keys, _ := tokenseal.ParseKeys("k1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	return fake, microsoft.Config{ClientID: "client-1", ClientSecret: "not-a-secret", Tenant: "common", Keys: keys, Base: server.URL}
}

func consent(t *testing.T, fake *microsofttest.Server, config microsoft.Config, account, kind string) (code string) {
	t.Helper()
	target, err := fake.Consent(config.AuthorizationURL(redirectURI, "state-1", "verifier-1", account), account, kind)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	if parsed.Query().Get("state") != "state-1" {
		t.Fatalf("state not returned: %s", target)
	}
	return parsed.Query().Get("code")
}

func TestAuthorizationURLAsksForSendingOnlyWithPKCEAtTheTenant(t *testing.T) {
	config := microsoft.Config{ClientID: "client-1", Tenant: "contoso.onmicrosoft.com"}
	parsed, err := url.Parse(config.AuthorizationURL(redirectURI, "state-1", "verifier-1", "support@contoso.example"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Host != "login.microsoftonline.com" || parsed.Path != "/contoso.onmicrosoft.com/oauth2/v2.0/authorize" {
		t.Errorf("authorization endpoint %s", parsed.String())
	}
	want := map[string]string{"client_id": "client-1", "response_type": "code", "response_mode": "query", "prompt": "select_account",
		"login_hint": "support@contoso.example", "code_challenge_method": "S256", "code_challenge": microsoft.Challenge("verifier-1"),
		"redirect_uri": redirectURI, "state": "state-1",
		"scope": "openid offline_access https://graph.microsoft.com/User.Read https://graph.microsoft.com/Mail.Send"}
	for key, value := range want {
		if query.Get(key) != value {
			t.Errorf("%s=%q, want %q", key, query.Get(key), value)
		}
	}
	if strings.Contains(query.Get("scope"), "Mail.Read") || strings.Contains(query.Get("scope"), "ReadWrite") {
		t.Errorf("scope asks to read mail: %s", query.Get("scope"))
	}
}

func TestAdminConsentURLNeverUsesCommon(t *testing.T) {
	for tenant, want := range map[string]string{"common": "/organizations/v2.0/adminconsent", "organizations": "/organizations/v2.0/adminconsent",
		"contoso.onmicrosoft.com": "/contoso.onmicrosoft.com/v2.0/adminconsent", "consumers": ""} {
		address := microsoft.Config{ClientID: "client-1", Tenant: tenant}.AdminConsentURL("https://desk.example.com/api/v1/channels/email/microsoft/admin-consent", "c1")
		if want == "" {
			if address != "" {
				t.Errorf("tenant %s: admin consent %s, want none for personal accounts", tenant, address)
			}
			continue
		}
		parsed, _ := url.Parse(address)
		if parsed.Path != want || parsed.Query().Get("client_id") != "client-1" || parsed.Query().Get("state") != "c1" ||
			parsed.Query().Get("scope") != "https://graph.microsoft.com/Mail.Send https://graph.microsoft.com/User.Read" {
			t.Errorf("tenant %s: admin consent %s", tenant, address)
		}
	}
}

func TestConfigFromEnvDefaultsToCommonAndRefusesAMalformedTenant(t *testing.T) {
	env := map[string]string{microsoft.EnvClientID: " id ", microsoft.EnvClientSecret: "secret"}
	config, err := microsoft.ConfigFromEnv(func(key string) string { return env[key] }, tokenseal.Keys{})
	if err != nil || config.Tenant != "common" || config.ClientID != "id" {
		t.Fatalf("config %+v %v", config, err)
	}
	if missing := config.Missing(); len(missing) != 1 || missing[0] != microsoft.EnvTokenKeys {
		t.Errorf("missing %v, want only the token keys", missing)
	}
	env[microsoft.EnvTenant] = "common/../x?y"
	if _, err := microsoft.ConfigFromEnv(func(key string) string { return env[key] }, tokenseal.Keys{}); err == nil {
		t.Error("a tenant that is not a path segment was accepted")
	}
}

func TestExchangeReadsTheMailboxFromGraph(t *testing.T) {
	fake, config := standIn(t)
	grant, err := config.Exchange(context.Background(), consent(t, fake, config, "Support@Acme.example", microsofttest.Work), "verifier-1", redirectURI)
	if err != nil || grant.Address != "support@acme.example" || grant.RefreshToken == "" || grant.AccountID == "" {
		t.Fatalf("work account: %+v %v", grant, err)
	}
	// A personal account's /me has no mail: its sign-in name is the address.
	grant, err = config.Exchange(context.Background(), consent(t, fake, config, "Help@Outlook.com", microsofttest.Personal), "verifier-1", redirectURI)
	if err != nil || grant.Address != "help@outlook.com" {
		t.Fatalf("personal account: %+v %v", grant, err)
	}
	fake.Set(func(s *microsofttest.Server) { s.NoMailbox = true })
	if _, err := config.Exchange(context.Background(), consent(t, fake, config, "nolicence@acme.example", microsofttest.Work), "verifier-1", redirectURI); !errors.Is(err, microsoft.ErrNoMailbox) {
		t.Fatalf("work account without a mailbox: %v, want ErrNoMailbox", err)
	}
	fake.Set(func(s *microsofttest.Server) { s.NoMailbox = false })
	if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@acme.example", microsofttest.Work), "wrong-verifier", redirectURI); !errors.Is(err, microsoft.ErrRevoked) {
		t.Fatalf("wrong PKCE verifier: %v", err)
	}
}

func TestExchangeRefusesAGrantWithoutSendingOrLastingAccess(t *testing.T) {
	fake, config := standIn(t)
	for _, scope := range []string{"openid offline_access User.Read", "openid%20offline_access%20https%3A%2F%2Fgraph.microsoft.com%2FUser.Read"} {
		fake.Set(func(s *microsofttest.Server) { s.GrantScope = scope })
		if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@acme.example", microsofttest.Work), "verifier-1", redirectURI); !errors.Is(err, microsoft.ErrSendNotGranted) {
			t.Errorf("scope %q: %v, want ErrSendNotGranted", scope, err)
		}
	}
	// Microsoft answers Mail.Send with or without the resource and in any case.
	for _, scope := range []string{"openid offline_access User.Read mail.send", "https://graph.microsoft.com/Mail.Send"} {
		fake.Set(func(s *microsofttest.Server) { s.GrantScope = scope })
		if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@acme.example", microsofttest.Work), "verifier-1", redirectURI); err != nil {
			t.Errorf("scope %q: %v", scope, err)
		}
	}
	fake.Set(func(s *microsofttest.Server) { s.GrantScope, s.OmitRefreshToken = "", true })
	if _, err := config.Exchange(context.Background(), consent(t, fake, config, "a@acme.example", microsofttest.Work), "verifier-1", redirectURI); !errors.Is(err, microsoft.ErrNoRefreshToken) {
		t.Errorf("no refresh token: %v", err)
	}
}

func TestAccessTokenRotatesAndClassifiesTokenErrors(t *testing.T) {
	fake, config := standIn(t)
	grant, err := config.Exchange(context.Background(), consent(t, fake, config, "a@acme.example", microsofttest.Work), "verifier-1", redirectURI)
	if err != nil {
		t.Fatal(err)
	}
	_, rotated, err := config.AccessToken(context.Background(), grant.RefreshToken)
	if err != nil || rotated == "" || rotated == grant.RefreshToken {
		t.Fatalf("refresh: rotated=%q %v, want a new refresh token every time", rotated, err)
	}
	// The old token keeps working (Microsoft does not revoke it on use).
	if _, _, err := config.AccessToken(context.Background(), grant.RefreshToken); err != nil {
		t.Fatalf("old refresh token: %v", err)
	}
	for _, failure := range []microsofttest.TokenFailure{
		{Status: 400, Error: "invalid_grant", Codes: []int{700082}},
		{Status: 400, Error: "interaction_required", Codes: []int{50076}},
	} {
		fake.Set(func(s *microsofttest.Server) { s.TokenFailures = []microsofttest.TokenFailure{failure} })
		if _, _, err := config.AccessToken(context.Background(), rotated); !errors.Is(err, microsoft.ErrRevoked) || errors.Is(err, microsoft.ErrClientConfig) {
			t.Errorf("%s: %v, want ErrRevoked", failure.Error, err)
		}
	}
	fake.Set(func(s *microsofttest.Server) {
		s.TokenFailures = []microsofttest.TokenFailure{{Status: 401, Error: "invalid_client", Codes: []int{7000222}}}
	})
	_, _, err = config.AccessToken(context.Background(), rotated)
	if !errors.Is(err, microsoft.ErrClientConfig) || errors.Is(err, microsoft.ErrRevoked) || !strings.Contains(err.Error(), "AADSTS7000222") {
		t.Errorf("expired client secret: %v, want ErrClientConfig naming the code", err)
	}
	fake.RevokeAll()
	if _, _, err := config.AccessToken(context.Background(), rotated); !errors.Is(err, microsoft.ErrRevoked) {
		t.Errorf("revoked: %v", err)
	}
}

func TestSendPostsBase64MIMEAndClassifiesGraphAnswers(t *testing.T) {
	fake, config := standIn(t)
	grant, err := config.Exchange(context.Background(), consent(t, fake, config, "support@acme.example", microsofttest.Work), "verifier-1", redirectURI)
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := config.AccessToken(context.Background(), grant.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("From: support@acme.example\r\nTo: c@d.example\r\nMessage-ID: <x@acme.example>\r\nIn-Reply-To: <in@d.example>\r\n\r\nhi\r\n")
	if err := config.Send(context.Background(), access, message); err != nil {
		t.Fatal(err)
	}
	if sent := fake.Messages(); len(sent) != 1 || sent[0].Account != "support@acme.example" || string(sent[0].Raw) != string(message) {
		t.Fatalf("sent %+v", sent)
	}
	if err := config.Send(context.Background(), "not-a-token", message); !errors.Is(err, microsoft.ErrUnauthorized) {
		t.Errorf("401: %v", err)
	}
	cases := []struct {
		failure                                  microsofttest.Failure
		temporary, tooLarge, mailbox, permission bool
		retryAfter                               time.Duration
	}{
		{failure: microsofttest.Failure{Status: 429, Code: "ApplicationThrottled", RetryAfter: 7}, temporary: true, retryAfter: 7 * time.Second},
		{failure: microsofttest.Failure{Status: 503, Code: "ServiceUnavailable"}, temporary: true},
		{failure: microsofttest.Failure{Status: 413, Code: "RequestEntityTooLarge"}, tooLarge: true},
		{failure: microsofttest.Failure{Status: 403, Code: "ErrorAccessDenied"}, permission: true},
		{failure: microsofttest.Failure{Status: 404, Code: "MailboxNotEnabledForRESTAPI"}, mailbox: true},
		{failure: microsofttest.Failure{Status: 401, Code: "MailboxNotEnabledForRESTAPI"}, mailbox: true},
	}
	for _, c := range cases {
		fake.Set(func(s *microsofttest.Server) { s.SendFailures = []microsofttest.Failure{c.failure} })
		err := config.Send(context.Background(), access, message)
		var apiErr *microsoft.APIError
		if !errors.As(err, &apiErr) {
			t.Errorf("%+v: %v, want an APIError", c.failure, err)
			continue
		}
		if apiErr.Temporary() != c.temporary || apiErr.TooLarge() != c.tooLarge || apiErr.MailboxUnavailable() != c.mailbox ||
			apiErr.PermissionDenied() != c.permission || apiErr.RetryAfter != c.retryAfter {
			t.Errorf("%+v: temporary=%v tooLarge=%v mailbox=%v permission=%v retryAfter=%v", c.failure, apiErr.Temporary(), apiErr.TooLarge(),
				apiErr.MailboxUnavailable(), apiErr.PermissionDenied(), apiErr.RetryAfter)
		}
	}
}

func TestGraphErrorPrefersAKnownInnerCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte(`{"error":{"code":"ErrorAccessDenied","message":"x","innerError":{"code":"MailboxNotEnabledForRESTAPI"}}}`))
	}))
	t.Cleanup(server.Close)
	err := microsoft.Config{Base: server.URL}.Send(context.Background(), "token", []byte("x"))
	var apiErr *microsoft.APIError
	if !errors.As(err, &apiErr) || !apiErr.MailboxUnavailable() || apiErr.PermissionDenied() {
		t.Fatalf("%v, want the inner mailbox code", err)
	}
}
