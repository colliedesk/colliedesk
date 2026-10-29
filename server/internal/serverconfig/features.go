package serverconfig

import (
	"fmt"
	"sort"
	"strings"
)

// A Feature is something workspaces see that only works once its settings are complete. Its switch
// (features.<name>) is unset by default, which means "on once its settings are complete": an upgrade writes
// nothing and changes nothing. "false" turns it off without touching its settings or any workspace's data;
// "true" may only be saved while its settings are complete (CheckSwitches). Settings that later go missing
// turn it off by themselves; the console says what is missing (docs/internals/operator-console.md).
type Feature struct {
	Name  string
	Label string
	// missing names what the feature still needs in snap: dotted keys, a bootstrap variable, or another
	// feature's switch.
	missing func(snap Snapshot) []string
}

// Feature names.
const (
	FeatureGmail       = "gmail"
	FeatureMicrosoft   = "microsoft"
	FeatureAutofix     = "autofix"
	FeatureAIServer    = "ai_server"
	FeatureHostedMail  = "hosted_mail"
	FeatureCloudSignIn = "cloud_sign_in"
	FeatureBilling     = "billing"
	FeatureSignup      = "signup"
)

// minSignupKey is the shortest sign-up hash key serve accepts (cmd/collie minSignupKeyBytes).
const minSignupKey = 32

// SwitchKey is the setting that turns a feature on or off.
func SwitchKey(name string) string { return "features." + name }

func unsetOf(snap Snapshot, names ...string) []string {
	var missing []string
	for _, name := range names {
		if value, _ := snap.Get(name); strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func withRing(snap Snapshot, missing []string) []string {
	if !snap.Ring() {
		missing = append(missing, "MAILBOX_TOKEN_KEYS")
	}
	return missing
}

var features = []Feature{
	{Name: FeatureGmail, Label: "Gmail mailboxes", missing: func(snap Snapshot) []string {
		return withRing(snap, unsetOf(snap, "google.client_id", "google.client_secret"))
	}},
	{Name: FeatureMicrosoft, Label: "Microsoft 365 mailboxes", missing: func(snap Snapshot) []string {
		return withRing(snap, unsetOf(snap, "microsoft.client_id", "microsoft.client_secret"))
	}},
	{Name: FeatureAutofix, Label: "Autofix (GitHub App)", missing: func(snap Snapshot) []string {
		return unsetOf(snap, "github.app_id", "github.slug", "github.client_id", "github.private_key", "github.client_secret", "github.webhook_secret")
	}},
	{Name: FeatureAIServer, Label: "The server's AI provider", missing: func(snap Snapshot) []string {
		missing := unsetOf(snap, "ai.server.provider")
		switch provider, _ := snap.Get("ai.server.provider"); strings.TrimSpace(provider) {
		case "openai_compatible":
			missing = append(missing, unsetOf(snap, "ai.server.base_url")...)
		default:
			missing = append(missing, unsetOf(snap, "ai.server.api_key")...)
		}
		return append(missing, unsetOf(snap, "ai.server.models")...)
	}},
	{Name: FeatureHostedMail, Label: "Hosted support addresses", missing: func(snap Snapshot) []string {
		return unsetOf(snap, "hosted_mail.domain", "mail_from.inbound_secret")
	}},
	{Name: FeatureCloudSignIn, Label: "Cloud sign-in", missing: func(snap Snapshot) []string {
		return unsetOf(snap, "cloud_auth.issuer", "cloud_auth.jwks_url", "cloud_auth.authorized_parties", "clerk.publishable_key")
	}},
	{Name: FeatureBilling, Label: "Billing (Polar)", missing: func(snap Snapshot) []string {
		return unsetOf(snap, "polar.access_token")
	}},
}

// Sign-up reads the state of cloud sign-in and billing, so it is added once the list exists.
func init() {
	features = append(features, Feature{Name: FeatureSignup, Label: "Cloud sign-up", missing: func(snap Snapshot) []string {
		var missing []string
		for _, needed := range []string{FeatureCloudSignIn, FeatureBilling} {
			if state := featureState(snap, needed); !state.Enabled {
				if state.Requested == "off" {
					missing = append(missing, SwitchKey(needed))
				} else {
					missing = append(missing, state.Missing...)
				}
			}
		}
		if len(snap.Getenv(SignupHashKeyEnv)) < minSignupKey {
			missing = append(missing, SignupHashKeyEnv)
		}
		return append(missing, unsetOf(snap, "trusted_proxies")...)
	}})
}

// Features lists every feature with a switch, in the order of the console.
func Features() []Feature { return append([]Feature(nil), features...) }

// FeatureState is a feature as a snapshot runs it.
type FeatureState struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	SwitchKey string   `json:"switch_key"`
	Requested string   `json:"requested"` // on, off or unset
	Enabled   bool     `json:"enabled"`
	Missing   []string `json:"missing"`
}

func requested(snap Snapshot, name string) string {
	value, ok := snap.Get(SwitchKey(name))
	switch strings.TrimSpace(value) {
	case "false":
		return "off"
	case "true":
		return "on"
	}
	if ok {
		return "on"
	}
	return "unset"
}

func featureState(snap Snapshot, name string) FeatureState {
	for _, feature := range features {
		if feature.Name != name {
			continue
		}
		missing := feature.missing(snap)
		if missing == nil {
			missing = []string{}
		}
		state := FeatureState{Name: name, Label: feature.Label, SwitchKey: SwitchKey(name), Requested: requested(snap, name), Missing: missing}
		state.Enabled = state.Requested != "off" && len(missing) == 0
		return state
	}
	return FeatureState{Name: name, Missing: []string{}}
}

// FeatureStates says, for every feature, whether snap runs it and what it misses.
func FeatureStates(snap Snapshot) []FeatureState {
	states := make([]FeatureState, 0, len(features))
	for _, feature := range features {
		states = append(states, featureState(snap, feature.Name))
	}
	return states
}

// FeatureOff reports whether an operator switched the feature off: its settings may be complete, and
// what it holds (connections, subscriptions, queued replies) waits for it to be on again.
func FeatureOff(snap Snapshot, name string) bool { return requested(snap, name) == "off" }

// SwitchedOff lists the features switched off, for the API (httpapi.Config.SwitchedOff).
func SwitchedOff(snap Snapshot) map[string]bool {
	off := map[string]bool{}
	for _, feature := range features {
		if FeatureOff(snap, feature.Name) {
			off[feature.Name] = true
		}
	}
	return off
}

// SwitchError refuses a change that turns a feature on while it misses settings; Fields maps each switch
// to what it misses.
type SwitchError struct{ Fields map[string]string }

func (e *SwitchError) Error() string {
	names := make([]string, 0, len(e.Fields))
	for name := range e.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+": "+e.Fields[name])
	}
	return strings.Join(parts, "; ")
}

// CheckSwitches refuses next when it turns a switch on (from off or unset) while its feature misses
// settings. A change that only makes a switched-on feature miss a setting is not refused: the feature reads
// as off until the setting is back. Restores and imports are not checked (the effective state covers them).
func CheckSwitches(current, next Snapshot) error {
	fields := map[string]string{}
	for _, feature := range features {
		if requested(next, feature.Name) != "on" || requested(current, feature.Name) == "on" {
			continue
		}
		if missing := feature.missing(next); len(missing) > 0 {
			fields[SwitchKey(feature.Name)] = fmt.Sprintf("%s cannot be turned on yet: set %s first", feature.Label, strings.Join(missing, ", "))
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return &SwitchError{Fields: fields}
}
