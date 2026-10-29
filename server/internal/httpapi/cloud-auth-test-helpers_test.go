package httpapi_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cloudAuthFixture struct {
	api          http.Handler
	pool         *pgxpool.Pool
	service      *auth.Service
	issuer       string
	key          *rsa.PrivateKey
	kid          string
	keys         map[string]*rsa.PrivateKey
	jwks         *httptest.Server
	jwksRequests atomic.Int64
}

func newCloudAuthFixture(t *testing.T) *cloudAuthFixture {
	return newCloudAuthFixtureWithEmailClaim(t, "")
}

func newCloudAuthFixtureWithEmailClaim(t *testing.T, emailClaim string) *cloudAuthFixture {
	t.Helper()
	return newCloudAuthFixtureWith(t, emailClaim, nil)
}

// newCloudAuthFixtureWith is the fixture with the server config adjusted by mutate, for example to
// switch billing on or move the clock.
func newCloudAuthFixtureWith(t *testing.T, emailClaim string, mutate func(*httpapi.Config)) *cloudAuthFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &cloudAuthFixture{issuer: "https://identity.example.com", key: key, kid: "key-1", keys: map[string]*rsa.PrivateKey{"key-1": key}}
	fixture.jwks = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fixture.jwksRequests.Add(1)
		items := make([]map[string]string, 0, len(fixture.keys))
		for kid, privateKey := range fixture.keys {
			items = append(items, jwk(kid, &privateKey.PublicKey))
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": items})
	}))
	t.Cleanup(fixture.jwks.Close)
	pool := testPool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	fixture.pool = pool
	fixture.service = auth.New(pool, testOrigin)
	config := httpapi.Config{Origin: testOrigin, CloudAuth: cloudauth.Config{
		Issuer: fixture.issuer, JWKSURL: fixture.jwks.URL, AuthorizedParties: []string{testOrigin}, PublishableKey: "pk_test_example",
		EmailClaim: emailClaim,
	}}
	if mutate != nil {
		mutate(&config)
	}
	fixture.api = httpapi.New(pool, config)
	return fixture
}

func jwk(kid string, key *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func (fixture *cloudAuthFixture) token(t *testing.T, overrides map[string]any) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss": fixture.issuer, "azp": testOrigin, "exp": now.Add(time.Hour).Unix(), "nbf": now.Add(-time.Minute).Unix(),
		"email": "ADMIN@CLOUD.EXAMPLE.COM", "email_verified": true,
	}
	for key, value := range overrides {
		claims[key] = value
	}
	return signJWT(t, fixture.key, fixture.kid, claims)
}

// signJWT signs claims as an RS256 JWT with key under kid.
func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func exchange(t *testing.T, api http.Handler, token, slug, origin string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{}
	if slug != "" {
		body["workspace_slug"] = slug
	}
	request := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", body, token, nil)
	request.Header.Set("Origin", origin)
	return serve(api, request)
}

func sessionCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "colliedesk_session" {
			return cookie
		}
	}
	t.Fatal("cloud exchange did not set a Colliedesk session cookie")
	return nil
}
