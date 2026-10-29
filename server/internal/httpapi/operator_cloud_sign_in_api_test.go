package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

const operatorCloudOrigin = "https://ops.example.com"

// operatorCloudEnv is an operator console on its own host, with cloud sign-in configured for the app.
type operatorCloudEnv struct {
	*operatorEnv
	issuer string
	key    *rsa.PrivateKey
}

func newOperatorCloudEnv(t *testing.T, requireTOTP bool) *operatorCloudEnv {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{jwk("key-1", &key.PublicKey)}})
	}))
	t.Cleanup(jwks.Close)
	issuer := "https://identity.example.com"
	env := newOperatorEnvWith(t, requireTOTP, func(serverconfig.Snapshot) error { return nil }, func(config *httpapi.Config) {
		config.OperatorOrigin = operatorCloudOrigin
		config.CloudAuth = cloudauth.Config{Issuer: issuer, JWKSURL: jwks.URL, AuthorizedParties: []string{testOrigin}, PublishableKey: "pk_test_example"}
	})
	return &operatorCloudEnv{operatorEnv: env, issuer: issuer, key: key}
}

// identity is a session token of the identity provider, issued to the console's origin unless overridden.
func (e *operatorCloudEnv) identity(overrides map[string]any) string {
	e.t.Helper()
	now := time.Now()
	claims := map[string]any{"iss": e.issuer, "azp": operatorCloudOrigin, "exp": now.Add(time.Hour).Unix(), "nbf": now.Add(-time.Minute).Unix(),
		"email": "ops@example.com", "email_verified": true}
	for name, value := range overrides {
		claims[name] = value
	}
	return signJWT(e.t, e.key, "key-1", claims)
}

func (e *operatorCloudEnv) cloudSignIn(bearer, origin string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := newRequest(e.t, http.MethodPost, "/api/v1/operator/auth/cloud", nil, bearer, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response := serve(e.api, req)
	// Every answer, a step or a refusal, is the one contracts/openapi.yaml types.
	validateContractResponse(e.t, req, response)
	return response
}

func TestOperatorCloudSignInAsksForTheAuthenticatorThenSignsIn(t *testing.T) {
	// operator.require_totp is off, and still a cloud sign-in needs the authenticator: an inbox alone is
	// not an operator.
	e := newOperatorCloudEnv(t, false)
	ctx := context.Background()
	if added, err := e.service.Add(ctx, "Ops@Example.com", "cli", "tests"); err != nil || !added {
		t.Fatalf("add = %v, %v", added, err)
	}
	first := e.cloudSignIn(e.identity(map[string]any{"email": "OPS@EXAMPLE.COM"}), operatorCloudOrigin)
	var step struct {
		Step, Ticket, Key, Email string
	}
	decode(t, first, &step)
	if first.Code != http.StatusOK || step.Step != "enroll" || step.Ticket == "" || step.Key == "" || step.Email != "ops@example.com" ||
		len(first.Result().Cookies()) != 0 || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("first cloud sign-in: %d %s", first.Code, first.Body.String())
	}
	secret, err := auth.ParseTOTPKey(step.Key)
	if err != nil {
		t.Fatal(err)
	}
	done := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/totp", map[string]any{"ticket": step.Ticket, "code": auth.TOTPCode(secret, time.Now())}, "", nil)
	if done.Code != http.StatusOK || len(done.Result().Cookies()) != 1 {
		t.Fatalf("code: %d %s", done.Code, done.Body.String())
	}
	cookie := done.Result().Cookies()[0]
	me := serve(e.api, newRequest(t, http.MethodGet, "/api/v1/operator/auth/me", nil, "", cookie))
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"totp_enabled":true`) {
		t.Fatalf("me: %d %s", me.Code, me.Body.String())
	}
	var via string
	if err := e.pool.QueryRow(ctx, `SELECT detail->>'via' FROM operator_audit WHERE action='operator.sign_in' ORDER BY id DESC LIMIT 1`).Scan(&via); err != nil || via != "cloud" {
		t.Fatalf("sign-in audit via = %q, %v", via, err)
	}
	// Once the authenticator is set, a cloud sign-in asks for a code and never shows a key again.
	second := e.cloudSignIn(e.identity(nil), operatorCloudOrigin)
	step.Key = ""
	decode(t, second, &step)
	if second.Code != http.StatusOK || step.Step != "code" || step.Key != "" {
		t.Fatalf("second cloud sign-in: %d %s", second.Code, second.Body.String())
	}
}

func TestOperatorCloudSignInRefusesEveryOtherTokenTheSameWay(t *testing.T) {
	e := newOperatorCloudEnv(t, true)
	ctx := context.Background()
	for _, email := range []string{"ops@example.com", "gone@example.com"} {
		if _, err := e.service.Add(ctx, email, "cli", "tests"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.service.RevokeOperator(ctx, "gone@example.com", "cli", "tests"); err != nil {
		t.Fatal(err)
	}
	refusals := map[string]string{
		"not an operator":     e.identity(map[string]any{"email": "stranger@example.com"}),
		"revoked operator":    e.identity(map[string]any{"email": "gone@example.com"}),
		"issued to the app":   e.identity(map[string]any{"azp": testOrigin}),
		"another party":       e.identity(map[string]any{"azp": "https://other.example.com"}),
		"another issuer":      e.identity(map[string]any{"iss": "https://attacker.example.com"}),
		"expired":             e.identity(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}),
		"email not verified":  e.identity(map[string]any{"email_verified": false}),
		"a workspace session": e.workspace,
		"not a token at all":  "nonsense",
		"no token":            "",
	}
	var want string
	for name, bearer := range refusals {
		response := e.cloudSignIn(bearer, operatorCloudOrigin)
		if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
			t.Errorf("%s: %d %s, want 401", name, response.Code, response.Body.String())
			continue
		}
		// Every answer carries its own request id; the rest must be the same, byte for byte.
		body := regexp.MustCompile(`"request_id":"[^"]*"`).ReplaceAllString(response.Body.String(), "")
		if want == "" {
			want = body
		}
		if body != want {
			t.Errorf("%s answers %s, unlike %s: a refusal must not tell who is an operator", name, body, want)
		}
	}
	if !strings.Contains(want, `"code":"operator_sign_in_refused"`) {
		t.Fatalf("refusal = %s", want)
	}
	// A verified identity that is no operator leaves a line in the audit log; junk tokens leave none.
	var refused int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM operator_audit WHERE action='operator.sign_in_refused' AND detail->>'email' IN ('stranger@example.com','gone@example.com')`).Scan(&refused); err != nil || refused != 2 {
		t.Fatalf("refused sign-ins audited = %d, %v", refused, err)
	}
	// The console's own origin only: a page elsewhere cannot use a token it holds.
	if response := e.cloudSignIn(e.identity(nil), testOrigin); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_rejected") {
		t.Fatalf("from the app's origin: %d %s", response.Code, response.Body.String())
	}
	// A token issued to the console never signs in to the app.
	app := exchange(t, e.api, e.identity(map[string]any{"email": "admin@realm.example.com"}), "", testOrigin)
	if app.Code != http.StatusUnauthorized {
		t.Fatalf("console token on the app's exchange: %d %s", app.Code, app.Body.String())
	}
	// The same token is tried 5 times a minute at most.
	token := e.identity(map[string]any{"email": "stranger@example.com"})
	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = e.cloudSignIn(token, operatorCloudOrigin)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth try: %d", last.Code)
	}
}

func TestOperatorCloudSignInIsOffWithoutCloudSignIn(t *testing.T) {
	e := newOperatorEnv(t, true)
	req := newRequest(t, http.MethodPost, "/api/v1/operator/auth/cloud", nil, "token", nil)
	req.Header.Set("Origin", testOrigin)
	if response := serve(e.api, req); response.Code != http.StatusNotFound {
		t.Fatalf("without cloud sign-in: %d %s", response.Code, response.Body.String())
	}
}
