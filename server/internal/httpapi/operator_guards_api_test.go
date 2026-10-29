package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
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
	if code, body := approve(map[string]any{"reauth_code": nextCode(secret)}); code != http.StatusBadRequest || !strings.Contains(body, `"phrase":"any"`) {
		t.Fatalf("approve without the phrase: %d %s", code, body)
	}
	if code, body := approve(map[string]any{"confirm": map[string]any{"email.from_policy": "any"}}); code != http.StatusOK || !strings.Contains(body, `"status":"approved"`) {
		t.Fatalf("approve with the phrase: %d %s", code, body)
	}
}
