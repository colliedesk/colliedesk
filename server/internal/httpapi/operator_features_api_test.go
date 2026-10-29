package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type operatorFeature struct {
	Name      string   `json:"name"`
	SwitchKey string   `json:"switch_key"`
	Requested string   `json:"requested"`
	Enabled   bool     `json:"enabled"`
	Missing   []string `json:"missing"`
}

func (e *operatorEnv) feature(cookie *http.Cookie, name string) operatorFeature {
	e.t.Helper()
	req := newRequest(e.t, http.MethodGet, "/api/v1/operator/config", nil, "", cookie)
	req.Header.Set("Origin", testOrigin)
	response := serve(e.api, req)
	validateContractResponse(e.t, req, response)
	var config struct{ Features []operatorFeature }
	decode(e.t, response, &config)
	for _, feature := range config.Features {
		if feature.Name == name {
			return feature
		}
	}
	e.t.Fatalf("no feature %s in %s", name, response.Body.String())
	return operatorFeature{}
}

// A switch cannot be turned on before its feature's settings are complete; once on, a setting removed is
// saved and the feature reads as off, with a warning that names what it misses.
func TestAFeatureSwitchTurnsOnOnlyWithItsSettings(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	if gmail := e.feature(cookie, "gmail"); gmail.Enabled || gmail.Requested != "unset" || strings.Join(gmail.Missing, ",") != "google.client_id,google.client_secret" {
		t.Fatalf("gmail on a new server = %+v", gmail)
	}
	refused := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"features.gmail": "true"}})
	if refused.Code != http.StatusUnprocessableEntity || opsErrorCode(refused) != "config_invalid" || !strings.Contains(refused.Body.String(), `"features.gmail"`) ||
		!strings.Contains(refused.Body.String(), "google.client_id") {
		t.Fatalf("turning gmail on without its settings: %d %s", refused.Code, refused.Body.String())
	}
	check := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", map[string]any{"set": map[string]any{"features.gmail": "true"}}, cookie)
	if !strings.Contains(check.Body.String(), `"ok":false`) || !strings.Contains(check.Body.String(), "features.gmail") {
		t.Fatalf("the check of the same change: %s", check.Body.String())
	}
	saved := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"features.gmail": "true", "google.client_id": "client"},
		"secrets": map[string]any{"google.client_secret": "secret"}})
	if saved.Code != http.StatusOK {
		t.Fatalf("turning gmail on with its settings: %d %s", saved.Code, saved.Body.String())
	}
	if gmail := e.feature(cookie, "gmail"); !gmail.Enabled || gmail.Requested != "on" {
		t.Fatalf("gmail on = %+v", gmail)
	}
	if removed := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "unset": []string{"google.client_id"}}); removed.Code != http.StatusOK {
		t.Fatalf("removing a setting of a feature that is on: %d %s", removed.Code, removed.Body.String())
	}
	if gmail := e.feature(cookie, "gmail"); gmail.Enabled || strings.Join(gmail.Missing, ",") != "google.client_id" {
		t.Fatalf("gmail missing its client = %+v", gmail)
	}
	req := newRequest(t, http.MethodGet, "/api/v1/operator/status", nil, "", cookie)
	req.Header.Set("Origin", testOrigin)
	status := serve(e.api, req)
	validateContractResponse(t, req, status)
	if !strings.Contains(status.Body.String(), `"code":"feature_incomplete"`) || !strings.Contains(status.Body.String(), `"key":"features.gmail"`) {
		t.Fatalf("status does not warn of the incomplete feature: %s", status.Body.String())
	}
	if off := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"features.gmail": "false"}}); off.Code != http.StatusOK {
		t.Fatalf("switching off is always allowed: %d %s", off.Code, off.Body.String())
	}
}

// Switching cloud sign-in off asks for its phrase, says the console still opens, and is a trial.
func TestSwitchingOffCloudSignInAsksForItsPhraseAndIsATrial(t *testing.T) {
	e := newOperatorEnv(t, false)
	cookie := e.signIn("ops@example.com")
	base := e.version(cookie)
	on := e.patch(cookie, map[string]any{"base_version": base, "set": map[string]any{"cloud_auth.issuer": "https://id.example.com", "cloud_auth.jwks_url": "https://id.example.com/jwks",
		"cloud_auth.authorized_parties": testOrigin, "clerk.publishable_key": "pk_test"}, "confirm": map[string]any{"cloud_auth.issuer": "https://id.example.com"}})
	if on.Code != http.StatusOK {
		t.Fatalf("set cloud sign-in: %d %s", on.Code, on.Body.String())
	}
	var trial struct{ Version int64 }
	decode(t, on, &trial)
	confirmTrial := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/trial/confirm", map[string]any{"version": trial.Version}, cookie)
	if confirmTrial.Code/100 != 2 {
		t.Fatalf("confirm the trial: %d %s", confirmTrial.Code, confirmTrial.Body.String())
	}
	untyped := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"features.cloud_sign_in": "false"}})
	if opsErrorCode(untyped) != "confirm_mismatch" || !strings.Contains(untyped.Body.String(), "one-time link") {
		t.Fatalf("cloud sign-in off untyped: %s", untyped.Body.String())
	}
	off := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"features.cloud_sign_in": "false"}, "confirm": map[string]any{"features.cloud_sign_in": "cloud sign-in off"}})
	if off.Code != http.StatusOK || !strings.Contains(off.Body.String(), `"applies":"trial"`) {
		t.Fatalf("cloud sign-in off: %d %s", off.Code, off.Body.String())
	}
	if feature := e.feature(cookie, "cloud_sign_in"); feature.Enabled || feature.Requested != "off" {
		t.Fatalf("cloud sign-in switched off = %+v", feature)
	}
}
