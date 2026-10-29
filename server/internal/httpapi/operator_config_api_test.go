package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// enrolled signs an operator in with an authenticator and returns the cookie and the key.
func (e *operatorEnv) enrolled(email string) (*http.Cookie, []byte) {
	e.t.Helper()
	var step struct{ Step, Ticket, Key string }
	decode(e.t, request(e.t, e.api, http.MethodPost, "/api/v1/operator/auth/redeem", map[string]any{"token": e.link(email)}, "", nil), &step)
	secret, err := auth.ParseTOTPKey(step.Key)
	if err != nil {
		e.t.Fatalf("enroll: %+v %v", step, err)
	}
	signed := request(e.t, e.api, http.MethodPost, "/api/v1/operator/auth/totp", map[string]any{"ticket": step.Ticket, "code": auth.TOTPCode(secret, time.Now())}, "", nil)
	if signed.Code != http.StatusOK {
		e.t.Fatalf("totp: %d %s", signed.Code, signed.Body.String())
	}
	return signed.Result().Cookies()[0], secret
}

// nextCode is a code the operator has not used yet: the step after the current one.
func nextCode(secret []byte) string { return auth.TOTPCode(secret, time.Now().Add(30*time.Second)) }

// staleCode makes the code given at sign-in older than 10 minutes, so the next guarded change asks again.
func (e *operatorEnv) staleCode() {
	e.t.Helper()
	if err := database.New(e.pool).Privileged(context.Background(), database.PrivilegeOperator, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE operator_sessions SET reauth_at = now() - interval '1 hour'`)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *operatorEnv) version(cookie *http.Cookie) int64 {
	e.t.Helper()
	var config struct{ Version int64 }
	decode(e.t, operatorRequest(e.t, e.api, http.MethodGet, "/api/v1/operator/config", nil, cookie), &config)
	return config.Version
}

func (e *operatorEnv) patch(cookie *http.Cookie, body map[string]any) *httptest.ResponseRecorder {
	e.t.Helper()
	return operatorRequest(e.t, e.api, http.MethodPatch, "/api/v1/operator/config", body, cookie)
}

func opsErrorCode(response *httptest.ResponseRecorder) string {
	var body struct{ Error struct{ Code string } }
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	return body.Error.Code
}

// The server enforces every guard the console shows; nothing is saved on a refusal.
func TestOperatorConfigChangeEnforcesItsGuards(t *testing.T) {
	e := newOperatorEnv(t, true)
	cookie, secret := e.enrolled("ops@example.com")
	e.staleCode()
	base := e.version(cookie)
	for name, c := range map[string]struct {
		body map[string]any
		want string
	}{
		"secret in set":         {map[string]any{"base_version": base, "set": map[string]any{"smtp.password": "x"}}, "secret_in_set"},
		"trusted proxies":       {map[string]any{"base_version": base, "set": map[string]any{"trusted_proxies": "10.0.0.0/8"}}, "reauth_required"},
		"billing untyped":       {map[string]any{"base_version": base, "secrets": map[string]any{"polar.access_token": "tok"}}, "confirm_mismatch"},
		"authenticator off":     {map[string]any{"base_version": base, "set": map[string]any{"operator.require_totp": "false"}}, "setting_not_changeable"},
		"authenticator unset":   {map[string]any{"base_version": base, "unset": []string{"operator.require_totp"}}, "setting_not_changeable"},
		"any sender untyped":    {map[string]any{"base_version": base, "set": map[string]any{"email.from_policy": "any"}}, "confirm_mismatch"},
		"unknown":               {map[string]any{"base_version": base, "set": map[string]any{"smtp.hots": "x"}}, "unknown_setting"},
		"a secret without code": {map[string]any{"base_version": base, "secrets": map[string]any{"smtp.password": "pw"}}, "reauth_required"},
		"a wrong code":          {map[string]any{"base_version": base, "secrets": map[string]any{"smtp.password": "pw"}, "reauth_code": "000000"}, "reauth_code_invalid"},
		"does not build":        {map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "smtp.example.net", "smtp.port": "465"}}, "config_invalid"},
	} {
		response := e.patch(cookie, c.body)
		if got := opsErrorCode(response); got != c.want {
			t.Errorf("%s: %d %s, want %s", name, response.Code, response.Body.String(), c.want)
		}
	}
	if e.version(cookie) != base {
		t.Fatal("a refused change was saved")
	}
	// A relay with its password; then moving the relay asks for the password again.
	saved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "smtp.example.net"}, "secrets": map[string]any{"smtp.password": "pw"}, "reauth_code": nextCode(secret)})
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `"applies":"in_seconds"`) {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	base = e.version(cookie)
	if moved := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "other.example.net"}}); opsErrorCode(moved) != "reenter_secret" {
		t.Fatalf("moving the relay without the password: %s", moved.Body.String())
	}
	if stale := e.patch(cookie, map[string]any{"base_version": base - 1, "set": map[string]any{"internal_signup.emails": "@example.com"}}); stale.Code != http.StatusConflict {
		t.Fatalf("stale base: %d %s", stale.Code, stale.Body.String())
	}
	hot := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"internal_signup.emails": "@example.com"}})
	if hot.Code != http.StatusOK || !strings.Contains(hot.Body.String(), `"applies":"at_once"`) {
		t.Fatalf("a hot setting: %d %s", hot.Code, hot.Body.String())
	}
	// The configuration never shows a secret's value.
	config := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/config", nil, cookie)
	if strings.Contains(config.Body.String(), `"pw"`) || !strings.Contains(config.Body.String(), `"env_name":"SMTP_PASSWORD"`) {
		t.Fatalf("config shows a secret or no variable names: %s", config.Body.String()[:200])
	}
}

// A token asks; a person approves with their code and types the secret, which is used once and stored nowhere.
func TestOperatorRequestIsApprovedWithItsSecretAndStoresNoSecret(t *testing.T) {
	e := newOperatorEnv(t, true)
	cookie, secret := e.enrolled("ops@example.com")
	token := e.token(cookie, auth.PermOperatorConfigWrite)
	e.staleCode()
	base := e.version(cookie)
	asked := request(t, e.api, http.MethodPatch, "/api/v1/operator/config", map[string]any{"base_version": base, "set": map[string]any{"smtp.host": "smtp.example.net"},
		"secrets": map[string]any{"smtp.password": nil}, "note": "the relay moved"}, token, nil)
	if asked.Code != http.StatusAccepted {
		t.Fatalf("token change: %d %s", asked.Code, asked.Body.String())
	}
	var filed struct {
		RequestID string `json:"request_id"`
	}
	decode(t, asked, &filed)
	const canary = "approver-typed-canary-77"
	approve := func(body map[string]any, realm bool, credential *http.Cookie, bearer string) *httptest.ResponseRecorder {
		req := newRequest(t, http.MethodPost, "/api/v1/agent-requests/"+filed.RequestID+"/approve", body, bearer, credential)
		req.Header.Set("Origin", testOrigin)
		if realm {
			req.Header.Set("X-Colliedesk-Realm", "operator")
		}
		return serve(e.api, req)
	}
	if wrong := approve(map[string]any{"reauth_code": "000000", "secrets": map[string]any{"smtp.password": canary}}, true, cookie, ""); opsErrorCode(wrong) != "reauth_code_invalid" {
		t.Fatalf("approve with a wrong code: %s", wrong.Body.String())
	}
	approved := approve(map[string]any{"reauth_code": nextCode(secret), "secrets": map[string]any{"smtp.password": canary}}, true, cookie, "")
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"status":"approved"`) {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	if again := approve(map[string]any{"reauth_code": nextCode(secret)}, true, cookie, ""); again.Code != http.StatusConflict {
		t.Fatalf("a second approval: %d", again.Code)
	}
	var stored, linked int
	if err := database.New(e.pool).Privileged(context.Background(), database.PrivilegeOperator, func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT
			(SELECT count(*) FROM operator_requests WHERE coalesce(body::text,'') || coalesce(result::text,'') LIKE '%'||$1||'%') +
			(SELECT count(*) FROM operator_audit WHERE detail::text LIKE '%'||$1||'%') +
			(SELECT count(*) FROM server_config_values WHERE coalesce(value,'') LIKE '%'||$1||'%')`, canary).Scan(&stored); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM server_config_versions WHERE request_id::text=$1 AND source='request'`, filed.RequestID).Scan(&linked)
	}); err != nil {
		t.Fatal(err)
	}
	if stored != 0 || linked != 1 {
		t.Fatalf("secret stored %d times, versions linked to the request %d", stored, linked)
	}
	config := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/config", nil, cookie)
	if !strings.Contains(config.Body.String(), `"key":"smtp.password"`) || !strings.Contains(config.Body.String(), `"source":"request"`) {
		t.Fatalf("the approved change is not the running configuration: %s", config.Body.String()[:300])
	}
}

// An operator request is never seen or decided from a workspace, and an agent request never from the console.
func TestOperatorAndWorkspaceRequestsNeverCross(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	token := e.token(cookie, auth.PermOperatorConfigWrite)
	var operatorRequestID struct {
		RequestID string `json:"request_id"`
	}
	decode(t, request(t, e.api, http.MethodPatch, "/api/v1/operator/config", map[string]any{"base_version": 0, "set": map[string]any{"internal_signup.emails": "@example.com"}}, token, nil), &operatorRequestID)
	// A read token of the workspace asks for a change: an agent request.
	readToken := issueToken(t, e.api, e.workspace, "reader", "read").Token
	var agentRequest struct {
		RequestID string `json:"request_id"`
	}
	decode(t, request(t, e.api, http.MethodPost, "/api/v1/tags", map[string]any{"name": "crossing"}, readToken, nil), &agentRequest)
	decision := func(id, action string, realm bool, bearer string, credential *http.Cookie) int {
		req := newRequest(t, http.MethodPost, "/api/v1/agent-requests/"+id+"/"+action, map[string]any{}, bearer, credential)
		req.Header.Set("Origin", testOrigin)
		if realm {
			req.Header.Set("X-Colliedesk-Realm", "operator")
		}
		return serve(e.api, req).Code
	}
	for _, action := range []string{"approve", "reject"} {
		for name, c := range map[string]struct {
			got, want int
		}{
			"workspace admin on an operator request":   {decision(operatorRequestID.RequestID, action, false, e.workspace, nil), http.StatusNotFound},
			"realm header with a workspace credential": {decision(operatorRequestID.RequestID, action, true, e.workspace, nil), http.StatusUnauthorized},
			"operator token":                                    {decision(operatorRequestID.RequestID, action, true, token, nil), http.StatusForbidden},
			"operator session on an agent request":              {decision(agentRequest.RequestID, action, true, "", cookie), http.StatusNotFound},
			"operator session without the realm on its request": {decision(operatorRequestID.RequestID, action, false, "", cookie), http.StatusUnauthorized},
		} {
			if c.got != c.want {
				t.Errorf("%s %s: %d, want %d", action, name, c.got, c.want)
			}
		}
	}
	listed := request(t, e.api, http.MethodGet, "/api/v1/agent-requests", nil, e.workspace, nil)
	if strings.Contains(listed.Body.String(), operatorRequestID.RequestID) {
		t.Fatal("a workspace lists an operator request")
	}
	if operators := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/requests", nil, cookie); strings.Contains(operators.Body.String(), agentRequest.RequestID) {
		t.Fatal("the console lists an agent request")
	}
	// A token sees only its own requests; a person all.
	second := e.token(cookie)
	if own := request(t, e.api, http.MethodGet, "/api/v1/operator/requests/"+operatorRequestID.RequestID, nil, second, nil); own.Code != http.StatusNotFound {
		t.Fatalf("another token read the request: %d", own.Code)
	}
}

func TestOperatorInviteRevokeAndRestore(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	req := newRequest(t, http.MethodPost, "/api/v1/operator/operators", map[string]any{"email": "second@example.com"}, "", cookie)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Idempotency-Key", "invite-1")
	invited := serve(e.api, req)
	if invited.Code != http.StatusCreated || !strings.Contains(invited.Body.String(), "/operator/sign-in?token=") {
		t.Fatalf("invite: %d %s", invited.Code, invited.Body.String())
	}
	req = newRequest(t, http.MethodPost, "/api/v1/operator/operators", map[string]any{"email": "second@example.com"}, "", cookie)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Idempotency-Key", "invite-1")
	if repeat := serve(e.api, req); repeat.Code != http.StatusOK || strings.Contains(repeat.Body.String(), "token=") {
		t.Fatalf("repeated invite: %d %s", repeat.Code, repeat.Body.String())
	}
	second := e.signIn("second@example.com")
	var team struct {
		Operators []struct{ ID, Email string }
	}
	decode(t, operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/operators", nil, cookie), &team)
	var secondID string
	for _, operator := range team.Operators {
		if operator.Email == "second@example.com" {
			secondID = operator.ID
		}
	}
	if revoked := operatorRequest(t, e.api, http.MethodDelete, "/api/v1/operator/operators/"+secondID, nil, cookie); revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", revoked.Code, revoked.Body.String())
	}
	if after := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/auth/me", nil, second); after.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked operator's session still works: %d", after.Code)
	}
	// Two versions, then the first again as a third.
	first := e.patch(cookie, map[string]any{"base_version": 0, "set": map[string]any{"internal_signup.emails": "@one.example"}})
	var v1 struct{ Version int64 }
	decode(t, first, &v1)
	e.patch(cookie, map[string]any{"base_version": v1.Version, "set": map[string]any{"internal_signup.emails": "@two.example"}})
	restored := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/versions/"+strconv.FormatInt(v1.Version, 10)+"/restore",
		map[string]any{"base_version": v1.Version + 1}, cookie)
	if restored.Code != http.StatusCreated {
		t.Fatalf("restore: %d %s", restored.Code, restored.Body.String())
	}
	history := operatorRequest(t, e.api, http.MethodGet, "/api/v1/operator/config/versions", nil, cookie)
	if !strings.Contains(history.Body.String(), `"source":"restore"`) || !strings.Contains(history.Body.String(), `"before":"@two.example","key":"internal_signup.emails"`) && !strings.Contains(history.Body.String(), `"after":"@one.example"`) {
		t.Fatalf("history: %s", history.Body.String())
	}
}
