package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const operatorRing = "k1:b3BlcmF0b3ItdGVzdC1rZXktMzItYnl0ZXMhISEhISE="

type operatorEnv struct {
	t         *testing.T
	api       http.Handler
	pool      *pgxpool.Pool
	service   *auth.OperatorService
	hot       *httpapi.Hot
	workspace string // a workspace admin's bearer
}

func newOperatorEnv(t *testing.T, requireTOTP bool) *operatorEnv {
	t.Helper()
	keys, err := tokenseal.ParseKeys(operatorRing)
	if err != nil {
		t.Fatal(err)
	}
	hot := httpapi.NewHot(httpapi.HotSettings{RequireTOTP: requireTOTP})
	api, workspaces, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Google.Keys = keys
		carry := httpapi.NewCarry(*config)
		carry.Hot = hot
		config.Carry = carry
		// serve builds with every parser (cmd/collie buildConfig); the email parser is enough here.
		config.CheckConfig = func(snap serverconfig.Snapshot) error { _, err := email.ConfigFromEnv(snap.Getenv); return err }
	})
	bearer := createSession(t, api, workspaces, "Operator Realm", "admin@realm.example.com")
	return &operatorEnv{t: t, api: api, pool: pool, hot: hot, workspace: bearer,
		service: auth.NewOperatorService(database.New(pool), keys, testOrigin+"/operator", nil)}
}

func (e *operatorEnv) link(email string) string {
	e.t.Helper()
	link, err := e.service.Pair(context.Background(), email, false, "tests")
	if err != nil {
		e.t.Fatal(err)
	}
	return link[strings.Index(link, "token=")+len("token="):]
}

// signIn redeems a link without an authenticator and returns the operator cookie.
func (e *operatorEnv) signIn(email string) *http.Cookie {
	e.t.Helper()
	response := request(e.t, e.api, http.MethodPost, "/api/v1/operator/auth/redeem", map[string]any{"token": e.link(email)}, "", nil)
	if response.Code != http.StatusOK {
		e.t.Fatalf("redeem: %d %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "__Host-colliedesk_operator" {
			return cookie
		}
	}
	e.t.Fatalf("no operator cookie in %v", response.Result().Cookies())
	return nil
}

func (e *operatorEnv) token(cookie *http.Cookie, permissions ...string) string {
	e.t.Helper()
	response := operatorRequest(e.t, e.api, http.MethodPost, "/api/v1/operator/tokens", map[string]any{"label": "an AI client", "permissions": permissions}, cookie)
	if response.Code != http.StatusCreated {
		e.t.Fatalf("create token: %d %s", response.Code, response.Body.String())
	}
	var made struct{ Token string }
	_ = json.Unmarshal(response.Body.Bytes(), &made)
	return made.Token
}

// operatorRequest sends with the operator cookie and the console's Origin.
func operatorRequest(t *testing.T, api http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, path, body, "", cookie)
	req.Header.Set("Origin", testOrigin)
	return serve(api, req)
}

func operatorRoutes() []httpapi.Route {
	var routes []httpapi.Route
	for _, route := range httpapi.Routes() {
		if route.Permission == auth.PermissionOperator {
			routes = append(routes, route)
		}
	}
	return routes
}

func concrete(pattern string) string {
	return strings.NewReplacer("{id}", "00000000-0000-4000-8000-000000000000").Replace(pattern)
}

// No workspace credential opens an operator route, and no operator credential opens a workspace route.
func TestOperatorRealmIsSeparate(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@realm.example.com")
	token := e.token(cookie)
	if len(operatorRoutes()) == 0 {
		t.Fatal("no operator route")
	}
	workspaceCookie := &http.Cookie{Name: "colliedesk_session", Value: e.workspace}
	for _, route := range operatorRoutes() {
		for name, response := range map[string]*httptest.ResponseRecorder{
			"workspace bearer": request(t, e.api, route.Method, concrete(route.Pattern), map[string]any{}, e.workspace, nil),
			"workspace cookie": operatorRequest(t, e.api, route.Method, concrete(route.Pattern), map[string]any{}, workspaceCookie),
		} {
			if response.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with a %s: %d, want 401", route.Method, route.Pattern, name, response.Code)
			}
		}
	}
	operatorCookieOnWorkspace := &http.Cookie{Name: "colliedesk_session", Value: cookie.Value}
	for _, route := range httpapi.Routes() {
		if route.Permission == auth.PermissionOperator || route.Permission == auth.PermissionPublic || route.Permission == auth.PermissionChannelInbound {
			continue
		}
		for name, response := range map[string]*httptest.ResponseRecorder{
			"operator token":                       request(t, e.api, route.Method, concrete(route.Pattern), map[string]any{}, token, nil),
			"operator session as a bearer":         request(t, e.api, route.Method, concrete(route.Pattern), map[string]any{}, cookie.Value, nil),
			"operator session in the app's cookie": operatorRequest(t, e.api, route.Method, concrete(route.Pattern), map[string]any{}, operatorCookieOnWorkspace),
			"operator cookie":                      operatorRequest(t, e.api, route.Method, concrete(route.Pattern), map[string]any{}, cookie),
		} {
			// 404: a route this server does not serve (billing off) refuses everyone the same way.
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusNotFound {
				t.Errorf("%s %s with an %s: %d, want 401", route.Method, route.Pattern, name, response.Code)
			}
		}
	}
	// The operator's own credentials work where they belong.
	if response := request(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, token, nil); response.Code != http.StatusOK {
		t.Fatalf("operator token on me: %d", response.Code)
	}
	if response := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, cookie); response.Code != http.StatusOK {
		t.Fatalf("operator cookie on me: %d", response.Code)
	}
	// A browser session is not a bearer, and a token is not a cookie.
	if response := request(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, cookie.Value, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("operator session as a bearer: %d", response.Code)
	}
	if response := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, &http.Cookie{Name: cookie.Name, Value: token}); response.Code != http.StatusUnauthorized {
		t.Fatalf("operator token as a cookie: %d", response.Code)
	}
}

func TestOperatorSignInWithoutAnAuthenticatorAndSignOut(t *testing.T) {
	e := newOperatorEnv(t, false)
	token := e.link("Ops@Example.com")
	response := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/redeem", map[string]any{"token": token}, "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"step":"done"`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("redeem: %d %s", response.Code, response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != "__Host-colliedesk_operator" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("cookie = %+v", cookie)
	}
	if again := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/redeem", map[string]any{"token": token}, "", nil); again.Code != http.StatusConflict {
		t.Fatalf("a link works once: %d", again.Code)
	}
	me := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, cookie)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"email":"ops@example.com"`) || !strings.Contains(me.Body.String(), `"path_mode":true`) {
		t.Fatalf("me: %d %s", me.Code, me.Body.String())
	}
	// A write with the cookie from another origin is refused.
	noOrigin := serve(e.api, newRequest(t, http.MethodPost, "/api/v1/operator/auth/logout", nil, "", cookie))
	if noOrigin.Code != http.StatusForbidden || !strings.Contains(noOrigin.Body.String(), "csrf_rejected") {
		t.Fatalf("logout without Origin: %d %s", noOrigin.Code, noOrigin.Body.String())
	}
	if out := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/auth/logout", nil, cookie); out.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", out.Code)
	}
	if after := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, cookie); after.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", after.Code)
	}
}

func TestOperatorAuthenticatorEnrollsSignsInAndLocks(t *testing.T) {
	e := newOperatorEnv(t, true)
	redeemed := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/redeem", map[string]any{"token": e.link("ops@example.com")}, "", nil)
	var step struct {
		Step, Ticket, Key, OTPAuthURL string
		QR                            *string `json:"qr_svg"`
	}
	decode(t, redeemed, &step)
	if step.Step != "enroll" || step.Ticket == "" || step.Key == "" || step.QR == nil || !strings.HasPrefix(*step.QR, "<svg") || len(redeemed.Result().Cookies()) != 0 {
		t.Fatalf("redeem with an authenticator required: %s", redeemed.Body.String())
	}
	secret, err := auth.ParseTOTPKey(step.Key)
	if err != nil {
		t.Fatal(err)
	}
	wrong := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/totp", map[string]any{"ticket": step.Ticket, "code": "000000"}, "", nil)
	if auth.TOTPCode(secret, time.Now()) == "000000" {
		t.Skip("the current code happens to be 000000")
	}
	if wrong.Code != http.StatusBadRequest || !strings.Contains(wrong.Body.String(), `"tries_left":9`) {
		t.Fatalf("wrong code: %d %s", wrong.Code, wrong.Body.String())
	}
	code := auth.TOTPCode(secret, time.Now())
	right := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/totp", map[string]any{"ticket": step.Ticket, "code": code}, "", nil)
	if right.Code != http.StatusOK || len(right.Result().Cookies()) != 1 {
		t.Fatalf("right code: %d %s", right.Code, right.Body.String())
	}
	// The next sign-in asks for a code, not the key again; the code just used does not work twice.
	second := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/redeem", map[string]any{"token": e.link("ops@example.com")}, "", nil)
	step.Key, step.QR = "", nil
	decode(t, second, &step)
	if step.Step != "code" || step.Key != "" {
		t.Fatalf("second sign-in: %s", second.Body.String())
	}
	if reused := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/totp", map[string]any{"ticket": step.Ticket, "code": code}, "", nil); reused.Code != http.StatusBadRequest {
		t.Fatalf("a used code: %d %s", reused.Code, reused.Body.String())
	}
	// Ten wrong codes lock sign-in, and say until when. The limiter allows 5 tries a minute per ticket,
	// so each wrong code comes from its own ticket.
	var locked *httptest.ResponseRecorder
	for i := 0; i < 10; i++ {
		fresh := request(t, e.api, http.MethodPost, "/api/v1/operator/auth/redeem", map[string]any{"token": e.link("ops@example.com")}, "", nil)
		decode(t, fresh, &step)
		locked = request(t, e.api, http.MethodPost, "/api/v1/operator/auth/totp", map[string]any{"ticket": step.Ticket, "code": "999999"}, "", nil)
	}
	if locked.Code != http.StatusLocked || !strings.Contains(locked.Body.String(), "locked_until") {
		t.Fatalf("after ten wrong codes: %d %s", locked.Code, locked.Body.String())
	}
}

func TestOperatorTokensReadAndAskForChanges(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	token := e.token(cookie, auth.PermOperatorManage)
	if !strings.HasPrefix(token, "cdop_") {
		t.Fatalf("token %q", token)
	}
	me := request(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, token, nil)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"kind":"token"`) {
		t.Fatalf("me with the token: %d %s", me.Code, me.Body.String())
	}
	asked := request(t, e.api, http.MethodPost, "/api/v1/operator/tokens", map[string]any{"label": "another", "note": "for the nightly check"}, token, nil)
	if asked.Code != http.StatusAccepted || !strings.Contains(asked.Body.String(), `"status":"pending"`) {
		t.Fatalf("a token creating a token: %d %s", asked.Code, asked.Body.String())
	}
	secret := request(t, e.api, http.MethodPost, "/api/v1/operator/tokens", map[string]any{"label": "x", "secrets": map[string]any{"smtp.password": "hunter2"}}, token, nil)
	if secret.Code != http.StatusBadRequest || !strings.Contains(secret.Body.String(), "secret_in_request") {
		t.Fatalf("a token sending a secret: %d %s", secret.Code, secret.Body.String())
	}
	var stored int
	if err := database.New(e.pool).Privileged(context.Background(), database.PrivilegeOperator, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM operator_requests WHERE body::text LIKE '%hunter2%'`).Scan(&stored)
	}); err != nil || stored != 0 {
		t.Fatalf("a secret was stored with a request: %d %v", stored, err)
	}
	// The same Idempotency-Key answers the first token's session without the token.
	req := newRequest(t, http.MethodPost, "/api/v1/operator/tokens", map[string]any{"label": "keyed"}, "", cookie)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Idempotency-Key", "press-1")
	first := serve(e.api, req)
	req = newRequest(t, http.MethodPost, "/api/v1/operator/tokens", map[string]any{"label": "keyed"}, "", cookie)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Idempotency-Key", "press-1")
	repeat := serve(e.api, req)
	if first.Code != http.StatusCreated || repeat.Code != http.StatusOK || strings.Contains(repeat.Body.String(), "cdop_") {
		t.Fatalf("repeat: %d then %d %s", first.Code, repeat.Code, repeat.Body.String())
	}
}
