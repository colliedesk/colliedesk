package httpapi_test

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// The operator console translates what the server says by code (docs/internals/operator-console.md › Message
// codes): beside each English text the API sends its code and params, and a param never holds a secret.

type codedMessage struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params"`
}

type codedCheck struct {
	OK          bool                    `json:"ok"`
	Errors      map[string]string       `json:"errors"`
	FieldErrors map[string]codedMessage `json:"field_errors"`
	Test        struct {
		Name    string         `json:"name"`
		OK      bool           `json:"ok"`
		Message string         `json:"message"`
		Code    string         `json:"code"`
		Params  map[string]any `json:"params"`
	} `json:"test"`
	Guards []struct {
		Key           string         `json:"key"`
		Preview       string         `json:"preview"`
		PreviewCode   string         `json:"preview_code"`
		PreviewParams map[string]any `json:"preview_params"`
		Refused       string         `json:"refused"`
		RefusedCode   string         `json:"refused_code"`
		RefusedParams map[string]any `json:"refused_params"`
	} `json:"guards"`
}

type codedRefusal struct {
	Error struct {
		Code        string                  `json:"code"`
		Message     string                  `json:"message"`
		MessageCode string                  `json:"message_code"`
		Params      map[string]any          `json:"params"`
		Fields      map[string]string       `json:"fields"`
		FieldErrors map[string]codedMessage `json:"field_errors"`
	} `json:"error"`
}

// checkCoded posts a check and fails when the answer repeats canary, the secret the change carries.
func checkCoded(t *testing.T, e *operatorEnv, cookie *http.Cookie, body map[string]any, canary string) codedCheck {
	t.Helper()
	response := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", body, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("check: %d %s", response.Code, response.Body.String())
	}
	noSecretIn(t, response, canary)
	var answer codedCheck
	decode(t, response, &answer)
	return answer
}

func noSecretIn(t *testing.T, response *httptest.ResponseRecorder, canary string) {
	t.Helper()
	if canary != "" && strings.Contains(response.Body.String(), canary) {
		t.Fatalf("the answer repeats the secret: %s", response.Body.String())
	}
}

// fieldCode expects the message of key to carry code, under the same keys as errors.
func fieldCode(t *testing.T, answer codedCheck, key, code string) codedMessage {
	t.Helper()
	keys := slices.Sorted(maps.Keys(answer.Errors))
	coded := slices.Sorted(maps.Keys(answer.FieldErrors))
	for _, name := range coded {
		if !slices.Contains(keys, name) {
			t.Fatalf("field_errors has %s, errors does not: %v %v", name, keys, coded)
		}
	}
	if answer.FieldErrors[key].Code != code {
		t.Fatalf("%s: code %q, want %s (errors %v, field_errors %v)", key, answer.FieldErrors[key].Code, code, answer.Errors, answer.FieldErrors)
	}
	return answer.FieldErrors[key]
}

func TestTheServerAIProvidersErrorsAndTestsCarryCodes(t *testing.T) {
	fake := &aiprovider.Fake{}
	e := newOperatorEnvWith(t, false, func(snap serverconfig.Snapshot) error { _, err := ai.ServerProviderFromEnv(snap.Getenv); return err },
		func(config *httpapi.Config) {
			config.AI.Build = func(ai.Spec) (aiprovider.Provider, error) { return fake, nil }
		})
	cookie := e.signIn("ops@example.com")
	const key = "sk-ant-canary-never-echoed"
	secrets := map[string]any{"ai.server.api_key": key}

	bogus := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "bogus"}, "secrets": secrets}, key)
	fieldCode(t, bogus, "ai.server.provider", "ai_server_provider_invalid")

	// A message about several settings carries its code under each, with their names, not variables.
	half := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic"}}, "")
	missing := fieldCode(t, half, "ai.server.api_key", "ai_server_half_configured")
	if missing.Params["missing"] != "ai.server.api_key, ai.server.models" || half.FieldErrors["ai.server.models"].Code != "ai_server_half_configured" {
		t.Fatalf("half configured: %+v", half)
	}

	twice := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic", "ai.server.models": "triage=a,triage=b"}, "secrets": secrets}, key)
	if coded := fieldCode(t, twice, "ai.server.models", "ai_server_models_twice"); coded.Params["slot"] != "triage" {
		t.Fatalf("params: %+v", coded)
	}
	prices := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic", "ai.server.models": "triage=a",
		"ai.server.monthly_allowance": "seat=3"}, "secrets": secrets}, key)
	if coded := fieldCode(t, prices, "ai.server.monthly_allowance", "ai_server_allowance_unknown"); coded.Params["key"] != "seat" {
		t.Fatalf("params: %+v", coded)
	}

	// A save refused for the same reason carries the same codes.
	refused := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"ai.server.provider": "bogus"}})
	var body codedRefusal
	decode(t, refused, &body)
	if refused.Code != http.StatusUnprocessableEntity || body.Error.FieldErrors["ai.server.provider"].Code != "ai_server_provider_invalid" || body.Error.Fields["ai.server.provider"] == "" {
		t.Fatalf("save: %d %s", refused.Code, refused.Body.String())
	}

	// Live tests: the answer, the refusal (the provider's words as detail, never the key) and the models.
	ok := map[string]any{"set": map[string]any{"ai.server.provider": "anthropic", "ai.server.models": "triage=claude-haiku-5-5"}, "secrets": secrets, "test": "ai"}
	fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: "OK"}})
	if answer := checkCoded(t, e, cookie, ok, key); answer.Test.Code != "test_ai_ok" || answer.Test.Params["model"] != "claude-haiku-5-5" {
		t.Fatalf("ai test: %+v", answer.Test)
	}
	fake.Script(aiprovider.FakeReply{Err: errors.New("401 invalid x-api-key " + key)})
	if answer := checkCoded(t, e, cookie, ok, key); answer.Test.Code != "test_ai_refused" || !strings.Contains(answer.Test.Params["detail"].(string), "401") {
		t.Fatalf("ai refused: %+v", answer.Test)
	}
	if answer := checkCoded(t, e, cookie, map[string]any{"test": "ai_models"}, ""); answer.Test.Code != "test_ai_models_needs_provider" || answer.Test.Message != "Pick a provider first." {
		t.Fatalf("models without a provider: %+v", answer.Test)
	}
	fake.FakeModels = []string{"claude-haiku-5-5"}
	listed := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic"}, "secrets": secrets, "test": "ai_models"}, key)
	if listed.Test.Code != "test_ai_models_ok" || listed.Test.Params["count"] != float64(1) || listed.Test.Message != "The provider offers 1 model. Nothing was saved." {
		t.Fatalf("models: %+v", listed.Test)
	}
}

func TestSignUpAndNetworkErrorsAndTheSignInKeysTestCarryCodes(t *testing.T) {
	e := newOperatorEnvChecking(t, false, func(snap serverconfig.Snapshot) error {
		_, signups := httpapi.ParseInternalSignups(snap.Getenv("INTERNAL_SIGNUP_EMAILS"), snap.Getenv("INTERNAL_SIGNUP_NETWORKS"))
		_, proxies := httpapi.ParseTrustedProxies(snap.Getenv("TRUSTED_PROXIES"))
		_, domain := helpcenter.DomainFromEnv(snap.Getenv)
		return errors.Join(signups, proxies, domain)
	})
	cookie := e.signIn("ops@example.com")
	const canary = "relay-password-canary"
	answer := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"internal_signup.emails": "colliedesk.com",
		"trusted_proxies": "not-an-ip", "help_center.domain": "localhost"}, "secrets": map[string]any{"smtp.password": canary}}, canary)
	if coded := fieldCode(t, answer, "internal_signup.emails", "internal_signup_email_invalid"); coded.Params["entry"] != "colliedesk.com" {
		t.Fatalf("emails: %+v", coded)
	}
	// One parser serves two settings: its params name the setting, as its text does.
	networks := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"internal_signup.networks": "10.0.0.0/33"}, "secrets": map[string]any{"smtp.password": canary}}, canary)
	if coded := fieldCode(t, networks, "internal_signup.networks", "network_entry_invalid"); coded.Params["name"] != "internal_signup.networks" || coded.Params["entry"] != "10.0.0.0/33" {
		t.Fatalf("networks: %+v", coded)
	}
	if coded := fieldCode(t, answer, "trusted_proxies", "network_entry_invalid"); coded.Params["name"] != "trusted_proxies" {
		t.Fatalf("proxies: %+v", coded)
	}
	if coded := fieldCode(t, answer, "help_center.domain", "help_center_domain_invalid"); coded.Params["domain"] != "localhost" {
		t.Fatalf("domain: %+v", coded)
	}
	if jwks := checkCoded(t, e, cookie, map[string]any{"test": "jwks"}, ""); jwks.Test.Code != "test_jwks_needs_settings" || jwks.Test.Message != "Set the issuer and the JWKS address first." {
		t.Fatalf("jwks: %+v", jwks.Test)
	}
}

func TestEmailErrorsAndTheTestEmailCarryCodes(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	const password = "relay-password-canary"
	cases := []struct {
		set        map[string]any
		key, code  string
		param, val string
	}{
		{map[string]any{"smtp.port": "smtp"}, "smtp.port", "smtp_port_invalid", "value", "smtp"},
		{map[string]any{"smtp.port": "465"}, "smtp.port", "smtp_port_implicit_tls", "", ""},
		{map[string]any{"smtp.starttls": "maybe"}, "smtp.starttls", "smtp_starttls_invalid", "value", "maybe"},
		{map[string]any{"email.from_policy": "everyone"}, "email.from_policy", "email_from_policy_invalid", "value", "everyone"},
		{map[string]any{"mail_from.domain": "not a domain"}, "mail_from.domain", "mail_from_domain_invalid", "value", "not a domain"},
		{map[string]any{"hosted_mail.domain": "mail.example.com"}, "hosted_mail.domain", "hosted_mail_needs_inbound_secret", "", ""},
	}
	for _, c := range cases {
		answer := checkCoded(t, e, cookie, map[string]any{"set": c.set, "secrets": map[string]any{"smtp.password": password}}, password)
		if coded := fieldCode(t, answer, c.key, c.code); c.param != "" && coded.Params[c.param] != c.val {
			t.Fatalf("%s: %+v", c.code, coded)
		}
	}
	// A secret refused by its own rule names itself, never its value.
	short := checkCoded(t, e, cookie, map[string]any{"secrets": map[string]any{"mail_from.inbound_secret": "short-inbound-canary"}}, "short-inbound-canary")
	if coded := fieldCode(t, short, "mail_from.inbound_secret", "mail_from_inbound_secret_needs"); coded.Params["min"] != float64(32) {
		t.Fatalf("inbound secret: %+v", coded)
	}
	if test := checkCoded(t, e, cookie, map[string]any{"test": "smtp"}, ""); test.Test.Code != "test_smtp_needs_host" || test.Test.Message != "Set the relay host first." {
		t.Fatalf("smtp test: %+v", test.Test)
	}
}

func TestMailboxBillingAndLimitErrorsAndThePolarTestCarryCodes(t *testing.T) {
	e := newOperatorEnvChecking(t, false, func(snap serverconfig.Snapshot) error {
		_, tenant := microsoft.ConfigFromEnv(snap.Getenv, tokenseal.Keys{})
		_, billing := polar.ConfigFromEnv(snap.Getenv)
		return errors.Join(tenant, github.ConfigFromEnv(snap.Getenv).Check(), billing)
	})
	cookie := e.signIn("ops@example.com")
	const token = "polar-token-canary"
	answer := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"microsoft.tenant": "my tenant"},
		"secrets": map[string]any{"github.private_key": "not-a-key-canary", "polar.access_token": token}}, token)
	if coded := fieldCode(t, answer, "microsoft.tenant", "microsoft_tenant_invalid"); coded.Params["value"] != "my tenant" {
		t.Fatalf("tenant: %+v", coded)
	}
	fieldCode(t, answer, "github.private_key", "github_private_key_invalid")
	if strings.Contains(answer.FieldErrors["github.private_key"].Params["detail"].(string), "canary") {
		t.Fatalf("the key's value in params: %+v", answer.FieldErrors)
	}
	// Billing names its settings and its products as the console does, in the params too.
	billing := fieldCode(t, answer, "polar.webhook_secret", "polar_partly_configured")
	if missing := billing.Params["missing"].(string); !strings.Contains(missing, "polar.webhook_secret") || !strings.Contains(missing, "the team monthly product") || strings.Contains(missing, "POLAR_") {
		t.Fatalf("billing: %+v", billing)
	}
	if test := checkCoded(t, e, cookie, map[string]any{"test": "polar"}, ""); test.Test.Code != "test_polar_needs_settings" {
		t.Fatalf("polar test: %+v", test.Test)
	}
}

func TestGuardsSwitchesAndRefusedRequestsCarryCodes(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	const canary = "polar_oat_canary-never-echoed"

	// What a change does: the preview's code, with its numbers.
	billing := checkCoded(t, e, cookie, map[string]any{"secrets": map[string]any{"polar.access_token": canary}}, canary)
	if len(billing.Guards) != 1 || billing.Guards[0].PreviewCode != "guard_billing_on" || billing.Guards[0].PreviewParams["workspaces"] != float64(0) {
		t.Fatalf("billing on: %+v", billing.Guards)
	}
	proxies := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"trusted_proxies": "10.0.0.0/8"}}, "")
	if len(proxies.Guards) != 1 || proxies.Guards[0].PreviewCode != "guard_proxies_read" || proxies.Guards[0].PreviewParams["address"] == nil ||
		proxies.Guards[0].PreviewParams["trial"] != float64(10) || !strings.Contains(proxies.Guards[0].Preview, "It is tried for 10 minutes;") {
		t.Fatalf("proxies: %+v", proxies.Guards)
	}
	// Why it cannot be saved: the refusal's code, beside the error of its setting.
	nobody := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"problem_report.workspace": "nobody-here"}}, "")
	if coded := fieldCode(t, nobody, "problem_report.workspace", "guard_no_workspace"); coded.Params["slug"] != "nobody-here" {
		t.Fatalf("problem reports: %+v", coded)
	}
	// A switch turned on before its settings.
	gmail := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"features.gmail": "true"}}, "")
	if coded := fieldCode(t, gmail, "features.gmail", "feature_needs_settings"); coded.Params["missing"] != "google.client_id, google.client_secret" || coded.Params["feature"] != "gmail" {
		t.Fatalf("gmail: %+v", coded)
	}
	var switched codedRefusal
	refused := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"features.gmail": "true"}})
	decode(t, refused, &switched)
	if switched.Error.MessageCode != "feature_incomplete_refused" || switched.Error.FieldErrors["features.gmail"].Code != "feature_needs_settings" {
		t.Fatalf("gmail save: %s", refused.Body.String())
	}

	// A refused request: the message's code and each field's.
	var unknown codedRefusal
	decode(t, e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"no.such": "x"}}), &unknown)
	if unknown.Error.MessageCode != "unknown_setting" || unknown.Error.Message != "Unknown setting. Nothing was saved." || unknown.Error.FieldErrors["no.such"].Code != "field_unknown_setting" {
		t.Fatalf("unknown setting: %+v", unknown.Error)
	}
	var typed codedRefusal
	mismatch := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"email.from_policy": "any"}})
	noSecretIn(t, mismatch, canary)
	decode(t, mismatch, &typed)
	if typed.Error.Code != "confirm_mismatch" || typed.Error.MessageCode != "confirm_mismatch" || typed.Error.FieldErrors["email.from_policy"].Code != "field_type_phrase" ||
		typed.Error.FieldErrors["email.from_policy"].Params["phrase"] != "any" || !strings.Contains(mismatch.Body.String(), `"preview_code":"guard_from_policy_any"`) {
		t.Fatalf("phrase: %s", mismatch.Body.String())
	}
	var stale codedRefusal
	decode(t, e.patch(cookie, map[string]any{"base_version": e.version(cookie) + 5, "set": map[string]any{"smtp.host": "smtp.example.net"}}), &stale)
	if stale.Error.Code != "config_changed" || stale.Error.MessageCode != "config_changed" {
		t.Fatalf("stale: %+v", stale.Error)
	}
	var invite codedRefusal
	decode(t, operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/operators", map[string]any{"email": "nobody"}, cookie), &invite)
	if invite.Error.MessageCode != "send_operator_email" {
		t.Fatalf("invite: %+v", invite.Error)
	}
}
