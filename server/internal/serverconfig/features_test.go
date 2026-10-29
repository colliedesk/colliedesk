package serverconfig_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// complete holds every setting each feature needs, so each one is set up.
var complete = map[string]string{
	"google.client_id": "id", "google.client_secret": "secret",
	"microsoft.client_id": "id", "microsoft.client_secret": "secret",
	"github.app_id": "1", "github.slug": "app", "github.client_id": "id", "github.private_key": "pem", "github.client_secret": "s", "github.webhook_secret": "w",
	"ai.server.provider": "anthropic", "ai.server.api_key": "key", "ai.server.models": "triage=m,draft=m",
	"hosted_mail.domain": "mail.example.com", "mail_from.inbound_secret": strings.Repeat("s", 32),
	"cloud_auth.issuer": "https://clerk.example.com", "cloud_auth.jwks_url": "https://clerk.example.com/jwks", "cloud_auth.authorized_parties": "https://app.example.com", "clerk.publishable_key": "pk",
	"polar.access_token": "token", "trusted_proxies": "10.0.0.0/8",
}

func snapshotOf(values map[string]string, ring bool) serverconfig.Snapshot {
	return serverconfig.NewSnapshot(1, values, nil, strings.Repeat("k", 32), nil, ring)
}

func stateOf(t *testing.T, snap serverconfig.Snapshot, name string) serverconfig.FeatureState {
	t.Helper()
	for _, state := range serverconfig.FeatureStates(snap) {
		if state.Name == name {
			return state
		}
	}
	t.Fatalf("no feature %s", name)
	return serverconfig.FeatureState{}
}

// An upgrade writes no switch: every feature its settings complete stays on, every other stays off.
func TestAFeatureWithoutItsSwitchFollowsItsSettings(t *testing.T) {
	full := snapshotOf(complete, true)
	for _, state := range serverconfig.FeatureStates(full) {
		if !state.Enabled || state.Requested != "unset" || len(state.Missing) != 0 {
			t.Fatalf("%s with every setting and no switch = %+v, want on", state.Name, state)
		}
	}
	for _, state := range serverconfig.FeatureStates(snapshotOf(nil, true)) {
		if state.Enabled || len(state.Missing) == 0 {
			t.Fatalf("%s with no setting = %+v, want off and what it misses", state.Name, state)
		}
	}
	if gmail := stateOf(t, snapshotOf(complete, false), "gmail"); gmail.Enabled || !slicesContain(gmail.Missing, "MAILBOX_TOKEN_KEYS") {
		t.Fatalf("gmail without a key ring = %+v", gmail)
	}
}

func TestASwitchTurnsOffOnlyItsFeature(t *testing.T) {
	values := map[string]string{"features.gmail": "false"}
	for key, value := range complete {
		values[key] = value
	}
	snap := snapshotOf(values, true)
	if gmail := stateOf(t, snap, "gmail"); gmail.Enabled || gmail.Requested != "off" || len(gmail.Missing) != 0 {
		t.Fatalf("gmail switched off = %+v", gmail)
	}
	if !serverconfig.FeatureOff(snap, "gmail") || serverconfig.FeatureOff(snap, "microsoft") {
		t.Fatal("FeatureOff names only what an operator switched off")
	}
	if microsoft := stateOf(t, snap, "microsoft"); !microsoft.Enabled {
		t.Fatalf("microsoft = %+v, want on", microsoft)
	}
}

// Sign-up needs cloud sign-in and billing on: switching either off takes sign-up with it.
func TestSignupNeedsCloudSignInAndBillingOn(t *testing.T) {
	values := map[string]string{"features.billing": "false"}
	for key, value := range complete {
		values[key] = value
	}
	signup := stateOf(t, snapshotOf(values, true), "signup")
	if signup.Enabled || !slicesContain(signup.Missing, "features.billing") {
		t.Fatalf("signup with billing off = %+v", signup)
	}
	if ai := stateOf(t, snapshotOf(map[string]string{"ai.server.provider": "openai_compatible", "ai.server.models": "m"}, true), "ai_server"); ai.Enabled || strings.Join(ai.Missing, ",") != "ai.server.base_url" {
		t.Fatalf("an OpenAI-compatible server without its address = %+v", ai)
	}
}

// Only a change that turns a switch on while its feature misses settings is refused; a change that makes a
// switched-on feature miss one is saved, and the feature reads as off.
func TestTurningOnAFeatureMissingSettingsIsRefused(t *testing.T) {
	empty := snapshotOf(nil, true)
	err := serverconfig.CheckSwitches(empty, empty.With(map[string]string{"features.gmail": "true"}, nil))
	var refused *serverconfig.SwitchError
	if !asSwitchError(err, &refused) || !strings.Contains(refused.Fields["features.gmail"], "google.client_id") {
		t.Fatalf("turning gmail on without its settings: %v", err)
	}
	withSettings := empty.With(map[string]string{"features.gmail": "true", "google.client_id": "id", "google.client_secret": "s"}, nil)
	if err := serverconfig.CheckSwitches(empty, withSettings); err != nil {
		t.Fatalf("turning gmail on with its settings in the same change: %v", err)
	}
	if err := serverconfig.CheckSwitches(withSettings, withSettings.With(nil, []string{"google.client_secret"})); err != nil {
		t.Fatalf("removing a setting of a switched-on feature: %v", err)
	}
	if err := serverconfig.CheckSwitches(empty, empty.With(map[string]string{"features.gmail": "false"}, nil)); err != nil {
		t.Fatalf("switching off is always allowed: %v", err)
	}
	if stateOf(t, withSettings.With(nil, []string{"google.client_secret"}), "gmail").Enabled {
		t.Fatal("a switched-on feature missing a setting reads as on")
	}
}

func slicesContain(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func asSwitchError(err error, target **serverconfig.SwitchError) bool {
	refused, ok := err.(*serverconfig.SwitchError)
	if ok {
		*target = refused
	}
	return ok
}
