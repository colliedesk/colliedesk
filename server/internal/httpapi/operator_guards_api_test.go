package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/jackc/pgx/v5"
)

type guardAnswer struct {
	Error struct {
		Code   string
		Guards []struct{ Key, Preview, Phrase string }
	}
}

// What a dangerous change asks depends on what it does, the server computes the phrase, and a phrase never
// carries a secret's value, whichever refusal answers.
func TestDangerousChangesSayWhatTheyDoAndAskToType(t *testing.T) {
	e := newOperatorEnv(t, true)
	cookie, secret := e.enrolled("ops@example.com")
	e.staleCode()
	base := e.version(cookie)
	const canary = "polar_oat_canary-never-echoed"
	for _, body := range []map[string]any{
		{"base_version": base, "secrets": map[string]any{"polar.access_token": canary}},
		{"base_version": base, "secrets": map[string]any{"polar.access_token": canary}, "confirm": map[string]any{"polar.access_token": "wrong"}},
		{"base_version": base, "secrets": map[string]any{"polar.access_token": canary}, "confirm": map[string]any{"polar.access_token": "billing on"}},
	} {
		response := e.patch(cookie, body)
		if strings.Contains(response.Body.String(), canary) {
			t.Fatalf("a refusal echoed the secret: %s", response.Body.String())
		}
	}
	check := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", map[string]any{"secrets": map[string]any{"polar.access_token": canary}}, cookie)
	if strings.Contains(check.Body.String(), canary) || !strings.Contains(check.Body.String(), `"phrase":"billing on"`) {
		t.Fatalf("check: %s", check.Body.String())
	}

	// Allowing any sender: the preview says what it does, and the phrase saves it.
	refused := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"email.from_policy": "any"}})
	var answer guardAnswer
	_ = json.Unmarshal(refused.Body.Bytes(), &answer)
	if answer.Error.Code != "confirm_mismatch" || len(answer.Error.Guards) != 1 || answer.Error.Guards[0].Phrase != "any" || !strings.Contains(answer.Error.Guards[0].Preview, "pose as another") {
		t.Fatalf("any sender: %s", refused.Body.String())
	}
	saved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"email.from_policy": "any"}, "confirm": map[string]any{"email.from_policy": "any"}, "reauth_code": nextCode(secret)})
	if saved.Code != http.StatusOK {
		t.Fatalf("any sender typed: %d %s", saved.Code, saved.Body.String())
	}
	// Back to verified asks nothing to type.
	base = e.version(cookie)
	if back := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"email.from_policy": "verified"}}); back.Code != http.StatusOK {
		t.Fatalf("verified again: %d %s", back.Code, back.Body.String())
	}

	// A mail domain in use: the first one asks nothing, changing it asks to type the new one.
	base = e.version(cookie)
	if first := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"mail_from.domain": "mail.example.com"}, "secrets": map[string]any{"mail_from.inbound_secret": strings.Repeat("s", 32)}}); first.Code != http.StatusOK {
		t.Fatalf("first domain: %d %s", first.Code, first.Body.String())
	}
	base = e.version(cookie)
	moved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"mail_from.domain": "reply.example.com"}})
	_ = json.Unmarshal(moved.Body.Bytes(), &answer)
	if answer.Error.Code != "confirm_mismatch" || answer.Error.Guards[0].Phrase != "reply.example.com" || !strings.Contains(answer.Error.Guards[0].Preview, "mail.example.com") {
		t.Fatalf("moving the domain: %s", moved.Body.String())
	}
	if typed := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"mail_from.domain": "reply.example.com"}, "confirm": map[string]any{"mail_from.domain": "reply.example.com"}}); typed.Code != http.StatusOK {
		t.Fatalf("domain typed: %d %s", typed.Code, typed.Body.String())
	}

	// Private AI addresses: turning on asks, turning off does not.
	base = e.version(cookie)
	if on := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"ai.allow_private_base_urls": "1"}}); opsErrorCode(on) != "confirm_mismatch" {
		t.Fatalf("private AI on: %s", on.Body.String())
	}
	// A problem report workspace nobody has is refused.
	if nobody := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"problem_report.workspace": "nobody-here"}}); opsErrorCode(nobody) != "config_invalid" {
		t.Fatalf("unknown workspace: %s", nobody.Body.String())
	}
}

// D12 in the API: trusted proxies are tried; nothing else saves meanwhile; confirm keeps, revert brings back.
func TestTrialChangesAreConfirmedOrReverted(t *testing.T) {
	e := newOperatorEnv(t, true)
	cookie, secret := e.enrolled("ops@example.com")
	e.staleCode()
	base := e.version(cookie)
	tried := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"trusted_proxies": "10.0.0.0/8"}, "reauth_code": nextCode(secret)})
	var saved struct {
		Version        int64  `json:"version"`
		Applies        string `json:"applies"`
		TrialExpiresAt string `json:"trial_expires_at"`
	}
	decode(t, tried, &saved)
	if tried.Code != http.StatusOK || saved.Applies != "trial" || saved.TrialExpiresAt == "" {
		t.Fatalf("trial: %d %s", tried.Code, tried.Body.String())
	}
	if other := e.patch(cookie, map[string]any{"base_version": saved.Version, "set": map[string]any{"internal_signup.emails": "@example.com"}}); other.Code != http.StatusConflict || opsErrorCode(other) != "trial_pending" {
		t.Fatalf("a save during the trial: %d %s", other.Code, other.Body.String())
	}
	e.hot.SetTrial(saved.Version)
	defer e.hot.SetTrial(0)
	status := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/status", nil, cookie)
	var state struct {
		Trial *struct {
			Version  int64
			Keys     []string
			Evidence struct {
				YourIP    string `json:"your_ip"`
				ClientIPs int    `json:"client_ips"`
			}
		}
	}
	decode(t, status, &state)
	if state.Trial == nil || state.Trial.Version != saved.Version || state.Trial.Keys[0] != "trusted_proxies" || state.Trial.Evidence.ClientIPs < 1 || state.Trial.Evidence.YourIP == "" {
		t.Fatalf("status during the trial: %s", status.Body.String())
	}
	if wrong := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/trial/confirm", map[string]any{"version": saved.Version + 7}, cookie); opsErrorCode(wrong) != "no_trial" {
		t.Fatalf("confirming another version: %d %s", wrong.Code, wrong.Body.String())
	}
	if kept := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/trial/confirm", map[string]any{"version": saved.Version}, cookie); kept.Code != http.StatusCreated {
		t.Fatalf("confirm: %d %s", kept.Code, kept.Body.String())
	}
	second := e.patch(cookie, map[string]any{"base_version": saved.Version, "set": map[string]any{"trusted_proxies": "192.168.0.0/16"}})
	decode(t, second, &saved)
	if saved.Applies != "trial" {
		t.Fatalf("second trial: %s", second.Body.String())
	}
	if reverted := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/trial/revert", map[string]any{"version": saved.Version}, cookie); reverted.Code != http.StatusCreated {
		t.Fatalf("revert: %d %s", reverted.Code, reverted.Body.String())
	}
	config := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/config", nil, cookie)
	if !strings.Contains(config.Body.String(), `"value":"10.0.0.0/8"`) {
		t.Fatal("the revert did not bring back the confirmed proxies")
	}
}

// Products are add-only: listed once, never changed but for being on sale.
func TestProductsAreAddedAndTakenOffSaleNeverChanged(t *testing.T) {
	e := newOperatorEnv(t, true)
	cookie, secret := e.enrolled("ops@example.com")
	e.staleCode()
	add := func(body map[string]any) int {
		return operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/billing/products", body, cookie).Code
	}
	if code := add(map[string]any{"product_id": "prod_a", "plan": "team", "period": "monthly"}); code != http.StatusBadRequest {
		t.Fatalf("add without a code: %d", code)
	}
	if code := add(map[string]any{"product_id": "prod_a", "plan": "team", "period": "monthly", "reauth_code": nextCode(secret)}); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if code := add(map[string]any{"product_id": "prod_a", "plan": "solo", "period": "yearly"}); code != http.StatusConflict {
		t.Fatalf("add the same id again: %d", code)
	}
	if code := add(map[string]any{"product_id": "prod_b", "plan": "team", "period": "monthly"}); code != http.StatusCreated {
		t.Fatalf("add a second team monthly: %d", code)
	}
	var list struct {
		Items []struct {
			ProductID string `json:"product_id"`
			OnSale    bool   `json:"on_sale"`
		}
	}
	decode(t, operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/billing/products", nil, cookie), &list)
	if len(list.Items) != 2 || list.Items[0].OnSale || !list.Items[1].OnSale {
		t.Fatalf("one product of a plan and period is on sale: %+v", list.Items)
	}
	if back := operatorRequest(t, e.api, http.MethodPatch, "/api/v1/operator/billing/products/prod_a", map[string]any{"on_sale": true}, cookie); back.Code != http.StatusOK {
		t.Fatalf("back on sale: %d %s", back.Code, back.Body.String())
	}
	if missing := operatorRequest(t, e.api, http.MethodPatch, "/api/v1/operator/billing/products/prod_zzz", map[string]any{"on_sale": true}, cookie); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown product: %d", missing.Code)
	}
}

// A token cannot fill in the phrase for the approver: it is dropped when filed, and the approver types it.
func TestATokenCannotTypeTheApproversPhrase(t *testing.T) {
	e := newOperatorEnv(t, true)
	cookie, secret := e.enrolled("ops@example.com")
	token := e.token(cookie, auth.PermOperatorConfigWrite)
	e.staleCode()
	base := e.version(cookie)
	asked := request(t, e.api, http.MethodPatch, "/api/v1/operator/config", map[string]any{"base_version": base, "set": map[string]any{"email.from_policy": "any"},
		"confirm": map[string]any{"email.from_policy": "any"}, "reauth_code": "123456"}, token, nil)
	var filed struct {
		RequestID string `json:"request_id"`
	}
	decode(t, asked, &filed)
	if asked.Code != http.StatusAccepted {
		t.Fatalf("filed: %d %s", asked.Code, asked.Body.String())
	}
	shown := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/requests/"+filed.RequestID, nil, cookie)
	if strings.Contains(shown.Body.String(), "confirm") || strings.Contains(shown.Body.String(), "123456") {
		t.Fatalf("the stored request keeps what only the approver gives: %s", shown.Body.String())
	}
	approve := func(body map[string]any) (int, string) {
		req := newRequest(t, http.MethodPost, "/api/v1/agent-requests/"+filed.RequestID+"/approve", body, "", cookie)
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("X-Colliedesk-Realm", "operator")
		response := serve(e.api, req)
		return response.Code, response.Body.String()
	}
	code := nextCode(secret)
	if status, body := approve(map[string]any{"reauth_code": code}); status != http.StatusBadRequest || !strings.Contains(body, `"phrase":"any"`) {
		t.Fatalf("approve without the phrase: %d %s", status, body)
	}
	// The wrong phrase did not spend the code: the same code approves with the phrase.
	if status, body := approve(map[string]any{"reauth_code": code, "confirm": map[string]any{"email.from_policy": "any"}}); status != http.StatusOK || !strings.Contains(body, `"status":"approved"`) {
		t.Fatalf("approve with the phrase: %d %s", status, body)
	}
}

// Billing turns on in the console the way an operator does it: the products first, then the token and the
// webhook secret with the phrase, checked by the real billing parser; off again asks its own phrase.
func TestBillingTurnsOnAfterItsProductsAndOffAgain(t *testing.T) {
	e := newOperatorEnvChecking(t, true, func(snap serverconfig.Snapshot) error { _, err := polar.ConfigFromEnv(snap.Getenv); return err })
	cookie, secret := e.enrolled("ops@example.com")
	e.staleCode()
	for i, product := range []map[string]any{{"product_id": "prod_tm", "plan": "team", "period": "monthly"},
		{"product_id": "prod_ty", "plan": "team", "period": "yearly"}, {"product_id": "prod_f", "plan": "founding", "period": "single"}} {
		if i == 0 {
			product["reauth_code"] = nextCode(secret)
		}
		if added := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/billing/products", product, cookie); added.Code != http.StatusCreated {
			t.Fatalf("add %v: %d %s", product["product_id"], added.Code, added.Body.String())
		}
	}
	// Listed products alone are not billing: every other change still saves.
	base := e.version(cookie)
	if other := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"internal_signup.emails": "@example.com"}}); other.Code != http.StatusOK {
		t.Fatalf("a change with products listed and billing off: %d %s", other.Code, other.Body.String())
	}
	base = e.version(cookie)
	on := map[string]any{"base_version": base, "secrets": map[string]any{"polar.access_token": "polar_oat_test", "polar.webhook_secret": "whsec_test"}}
	if half := e.patch(cookie, map[string]any{"base_version": base, "secrets": map[string]any{"polar.access_token": "polar_oat_test"}, "confirm": map[string]any{"polar.access_token": "billing on"}}); opsErrorCode(half) != "config_invalid" {
		t.Fatalf("a token without its webhook secret: %d %s", half.Code, half.Body.String())
	}
	if untyped := e.patch(cookie, on); opsErrorCode(untyped) != "confirm_mismatch" {
		t.Fatalf("billing on untyped: %s", untyped.Body.String())
	}
	on["confirm"] = map[string]any{"polar.access_token": "billing on"}
	if saved := e.patch(cookie, on); saved.Code != http.StatusOK {
		t.Fatalf("billing on: %d %s", saved.Code, saved.Body.String())
	}
	base = e.version(cookie)
	// Rotating the token while billing stays on asks no phrase.
	if rotated := e.patch(cookie, map[string]any{"base_version": base, "secrets": map[string]any{"polar.access_token": "polar_oat_next"}}); rotated.Code != http.StatusOK {
		t.Fatalf("rotating the token: %d %s", rotated.Code, rotated.Body.String())
	}
	base = e.version(cookie)
	off := map[string]any{"base_version": base, "unset": []string{"polar.access_token", "polar.webhook_secret"}}
	if untyped := e.patch(cookie, off); opsErrorCode(untyped) != "confirm_mismatch" || !strings.Contains(untyped.Body.String(), `"phrase":"billing off"`) {
		t.Fatalf("billing off untyped: %s", untyped.Body.String())
	}
	off["confirm"] = map[string]any{"polar.access_token": "billing off"}
	if saved := e.patch(cookie, off); saved.Code != http.StatusOK {
		t.Fatalf("billing off: %d %s", saved.Code, saved.Body.String())
	}
}

// Turning deletion on with billing on shows how many workspaces are due and asks to type that number.
func TestRetentionOnAsksForTheNumberDue(t *testing.T) {
	e := newOperatorEnv(t, true)
	cookie, secret := e.enrolled("ops@example.com")
	e.staleCode()
	base := e.version(cookie)
	if saved := e.patch(cookie, map[string]any{"base_version": base, "secrets": map[string]any{"polar.access_token": "polar_oat_test"},
		"confirm": map[string]any{"polar.access_token": "billing on"}, "reauth_code": nextCode(secret)}); saved.Code != http.StatusOK {
		t.Fatalf("billing on: %d %s", saved.Code, saved.Body.String())
	}
	base = e.version(cookie)
	asked := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"retention.delete": "on"}})
	var answer guardAnswer
	_ = json.Unmarshal(asked.Body.Bytes(), &answer)
	if answer.Error.Code != "confirm_mismatch" || answer.Error.Guards[0].Phrase != "0" || !strings.Contains(answer.Error.Guards[0].Preview, "0 workspaces locked for 12 months are due now") {
		t.Fatalf("retention on: %s", asked.Body.String())
	}
	if saved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"retention.delete": "on"}, "confirm": map[string]any{"retention.delete": "0"}}); saved.Code != http.StatusOK {
		t.Fatalf("retention on typed: %d %s", saved.Code, saved.Body.String())
	}
	// Off again is at once and asks nothing.
	base = e.version(cookie)
	if off := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"retention.delete": "off"}}); off.Code != http.StatusOK {
		t.Fatalf("retention off: %d %s", off.Code, off.Body.String())
	}
}

// Before cloud sign-in changes, the console fetches the JWKS of the change once; nothing is saved.
func TestCheckFetchesTheSignInKeysOfAChange(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{jwk("key-1", &key.PublicKey)}})
	}))
	defer jwks.Close()
	set := map[string]any{"cloud_auth.issuer": "https://identity.example.com", "cloud_auth.jwks_url": jwks.URL,
		"cloud_auth.authorized_parties": testOrigin, "clerk.publishable_key": "pk_test_x"}
	check := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", map[string]any{"set": set, "test": "jwks"}, cookie)
	var answer struct {
		Test struct {
			OK      bool
			Message string
		}
		Guards []struct {
			Key, Phrase string
			Trial       bool
		}
	}
	decode(t, check, &answer)
	if !answer.Test.OK || !strings.Contains(answer.Test.Message, "1 keys") || len(answer.Guards) != 1 || answer.Guards[0].Phrase != "https://identity.example.com" || !answer.Guards[0].Trial {
		t.Fatalf("check: %s", check.Body.String())
	}
	if e.version(cookie) != 0 {
		t.Fatal("a check saved something")
	}
}

// A help center domain needs its wildcard DNS record before it is saved; while it is tried, the band shows
// what a visitor gets from a real help center under it.
func TestHelpCenterDomainNeedsItsWildcardAndIsFetchedOnTrial(t *testing.T) {
	wildcard := false
	fetched := ""
	e := newOperatorEnvWith(t, false, func(serverconfig.Snapshot) error { return nil }, func(config *httpapi.Config) {
		config.LookupCNAME = func(_ context.Context, host string) (string, error) {
			if wildcard && strings.HasSuffix(host, ".help.example.com") {
				return host, nil
			}
			return "", errors.New("no such host")
		}
		config.HelpCenterProbe = func(_ context.Context, url string) (int, error) { fetched = url; return http.StatusOK, nil }
	})
	cookie := e.signIn("ops@example.com")
	base := e.version(cookie)
	refused := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"help_center.domain": "help.example.com"}})
	if opsErrorCode(refused) != "config_invalid" || !strings.Contains(refused.Body.String(), "wildcard record *.help.example.com") {
		t.Fatalf("without the wildcard: %d %s", refused.Code, refused.Body.String())
	}
	wildcard = true
	tried := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"help_center.domain": "help.example.com"}})
	var saved struct {
		Version int64  `json:"version"`
		Applies string `json:"applies"`
	}
	decode(t, tried, &saved)
	if saved.Applies != "trial" {
		t.Fatalf("with the wildcard: %d %s", tried.Code, tried.Body.String())
	}
	if err := database.New(e.pool).Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO help_centers(workspace_id, enabled) SELECT id, true FROM workspaces ORDER BY created_at LIMIT 1
			ON CONFLICT (workspace_id) DO UPDATE SET enabled = true`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/status", nil, cookie)
	if !strings.Contains(status.Body.String(), `"help_center":{"ok":true,"status":200,"url":"https://`) || !strings.HasSuffix(fetched, ".help.example.com/") {
		t.Fatalf("trial evidence: %s (fetched %q)", status.Body.String(), fetched)
	}
}

// The AI check asks the change's provider once (one provider check), and never repeats its key.
func TestCheckAsksTheAIProviderOnce(t *testing.T) {
	fake := &aiprovider.Fake{}
	e := newOperatorEnvWith(t, false, func(serverconfig.Snapshot) error { return nil }, func(config *httpapi.Config) {
		config.AI.Build = func(ai.Spec) (aiprovider.Provider, error) { return fake, nil }
	})
	cookie := e.signIn("ops@example.com")
	const key = "sk-ant-canary-never-echoed"
	body := map[string]any{"set": map[string]any{"ai.server.provider": "anthropic", "ai.server.models": "triage=claude-haiku-5-5"},
		"secrets": map[string]any{"ai.server.api_key": key}, "test": "ai"}
	fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: "OK"}})
	ok := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", body, cookie)
	if !strings.Contains(ok.Body.String(), `"ok":true`) || !strings.Contains(ok.Body.String(), "claude-haiku-5-5") || len(fake.Requests()) != 1 || fake.Requests()[0].MaxOutput > 256 {
		t.Fatalf("ai check: %s (%d requests)", ok.Body.String(), len(fake.Requests()))
	}
	fake.Script(aiprovider.FakeReply{Err: errors.New("401 invalid x-api-key " + key)})
	refused := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", body, cookie)
	if strings.Contains(refused.Body.String(), key) || !strings.Contains(refused.Body.String(), `"name":"ai","ok":false`) {
		t.Fatalf("a refusal: %s", refused.Body.String())
	}
}

// Review ADR round 2: a live test never sends a stored secret somewhere new. A token runs no live test at
// all (it only reads); a person who moves an endpoint types its secret again for the test, as for a save.
func TestALiveTestNeverSendsAStoredSecretToANewAddress(t *testing.T) {
	connected := make(chan struct{}, 4)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connected <- struct{}{}
			_ = conn.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	base := e.version(cookie)
	if saved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "smtp.example.net", "smtp.username": "svc"},
		"secrets": map[string]any{"smtp.password": "stored-relay-password"}}); saved.Code != http.StatusOK {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	moved := map[string]any{"set": map[string]any{"smtp.host": "127.0.0.1", "smtp.port": port, "smtp.starttls": "false"}, "test": "smtp"}
	token := e.token(cookie)
	byToken := request(t, e.api, http.MethodPost, "/api/v1/operator/config/check", moved, token, nil)
	if byToken.Code != http.StatusForbidden || opsErrorCode(byToken) != "test_needs_a_person" {
		t.Fatalf("a token's live test: %d %s", byToken.Code, byToken.Body.String())
	}
	// Without a test, a token still checks a change.
	plain := request(t, e.api, http.MethodPost, "/api/v1/operator/config/check", map[string]any{"set": map[string]any{"internal_signup.emails": "@x.example"}}, token, nil)
	if plain.Code != http.StatusOK {
		t.Fatalf("a token's check: %d %s", plain.Code, plain.Body.String())
	}
	byPerson := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", moved, cookie)
	if !strings.Contains(byPerson.Body.String(), `"ok":false`) || !strings.Contains(byPerson.Body.String(), "smtp.password") {
		t.Fatalf("a person's test to a new relay without the password: %s", byPerson.Body.String())
	}
	select {
	case <-connected:
		t.Fatal("the server connected to the new relay with the stored password")
	default:
	}
}

// Review UI/UX 2: a change that does not build says why in the console's words, never a variable name.
func TestACheckThatFailsNamesNoVariable(t *testing.T) {
	e := newOperatorEnvChecking(t, false, func(snap serverconfig.Snapshot) error { _, err := polar.ConfigFromEnv(snap.Getenv); return err })
	cookie := e.signIn("ops@example.com")
	check := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check",
		map[string]any{"secrets": map[string]any{"polar.access_token": "tok", "polar.webhook_secret": "wh"}}, cookie)
	if !strings.Contains(check.Body.String(), "team monthly product") || strings.Contains(check.Body.String(), "POLAR_") {
		t.Fatalf("check: %s", check.Body.String())
	}
}

// Review smoke F3: removing a relay's password (and username) asks for no password again; moving the relay
// while the password stays does.
func TestRemovingASecretAsksNotToTypeItAgain(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	base := e.version(cookie)
	if saved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "smtp.example.net", "smtp.username": "svc"},
		"secrets": map[string]any{"smtp.password": "pw"}}); saved.Code != http.StatusOK {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	base = e.version(cookie)
	if removed := e.patch(cookie, map[string]any{"base_version": base, "unset": []string{"smtp.username", "smtp.password"}}); removed.Code != http.StatusOK {
		t.Fatalf("removing the password: %d %s", removed.Code, removed.Body.String())
	}
	base = e.version(cookie)
	if same := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "smtp.example.net", "smtp.port": "2525"}, "secrets": map[string]any{"smtp.password": "pw"}}); same.Code != http.StatusOK {
		t.Fatalf("save again: %d %s", same.Code, same.Body.String())
	}
	base = e.version(cookie)
	if moved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "other.example.net"}}); opsErrorCode(moved) != "reenter_secret" {
		t.Fatalf("moving the relay: %s", moved.Body.String())
	}
}

// Review smoke F1: a restore passes the guards of what it changes from the version that runs.
func TestARestorePassesTheGuardsOfWhatItChanges(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	if first := e.patch(cookie, map[string]any{"base_version": 0, "set": map[string]any{"internal_signup.emails": "@example.com"}}); first.Code != http.StatusOK {
		t.Fatalf("first: %s", first.Body.String())
	}
	before := e.version(cookie)
	if saved := e.patch(cookie, map[string]any{"base_version": before, "set": map[string]any{"trusted_proxies": "10.0.0.0/8"}}); saved.Code != http.StatusOK {
		t.Fatalf("proxies: %s", saved.Body.String())
	}
	proxies := e.version(cookie)
	if kept := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/trial/confirm", map[string]any{"version": proxies}, cookie); kept.Code != http.StatusCreated {
		t.Fatalf("confirm: %s", kept.Body.String())
	}
	if on := e.patch(cookie, map[string]any{"base_version": proxies, "secrets": map[string]any{"polar.access_token": "tok"}, "confirm": map[string]any{"polar.access_token": "billing on"}}); on.Code != http.StatusOK {
		t.Fatalf("billing on: %s", on.Body.String())
	}
	billing := e.version(cookie)
	restore := func(version int64, body map[string]any) *httptest.ResponseRecorder {
		body["base_version"] = billing
		return operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/versions/"+strconv.FormatInt(version, 10)+"/restore", body, cookie)
	}
	// Back to before billing: billing turns off, so the phrase is asked.
	untyped := restore(proxies, map[string]any{})
	if opsErrorCode(untyped) != "confirm_mismatch" || !strings.Contains(untyped.Body.String(), `"phrase":"billing off"`) {
		t.Fatalf("restore untyped: %d %s", untyped.Code, untyped.Body.String())
	}
	// Back to before the proxies too: also a trial.
	typed := restore(before, map[string]any{"confirm": map[string]any{"polar.access_token": "billing off"}})
	if typed.Code != http.StatusCreated || !strings.Contains(typed.Body.String(), `"applies":"trial"`) {
		t.Fatalf("restore typed: %d %s", typed.Code, typed.Body.String())
	}
}

// Review smoke F4: a request filed on an older version applies on the version that runs when it is approved.
func TestARequestOnAnOlderVersionStillApplies(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	token := e.token(cookie, auth.PermOperatorConfigWrite)
	asked := request(t, e.api, http.MethodPatch, "/api/v1/operator/config", map[string]any{"base_version": 0, "set": map[string]any{"internal_signup.emails": "@agent.example"}}, token, nil)
	var filed struct {
		RequestID string `json:"request_id"`
	}
	decode(t, asked, &filed)
	if saved := e.patch(cookie, map[string]any{"base_version": 0, "set": map[string]any{"internal_signup.networks": "10.0.0.0/8"}}); saved.Code != http.StatusOK {
		t.Fatalf("a save meanwhile: %s", saved.Body.String())
	}
	req := newRequest(t, http.MethodPost, "/api/v1/agent-requests/"+filed.RequestID+"/approve", map[string]any{}, "", cookie)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-Colliedesk-Realm", "operator")
	approved := serve(e.api, req)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"status":"approved"`) {
		t.Fatalf("approve on a newer version: %d %s", approved.Code, approved.Body.String())
	}
	config := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/config", nil, cookie).Body.String()
	if !strings.Contains(config, `"value":"@agent.example"`) || !strings.Contains(config, `"value":"10.0.0.0/8"`) {
		t.Fatal("the approved change did not apply on the newer version, or replaced it")
	}
}
