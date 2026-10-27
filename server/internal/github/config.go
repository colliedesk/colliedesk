// Package github is the Colliedesk GitHub App that Autofix works through: the operator's App settings
// here; the App and user tokens, the REST and GraphQL calls and the webhook signature come with the
// GitHub connection. Plain net/http against GitHub's documented endpoints; no SDK.
package github

import "strings"

// Config is the operator's own GitHub App. All six settings are needed; with
// any missing, Autofix is off on this server and GET /api/v1/config names what is missing.
type Config struct {
	AppID         string
	Slug          string
	PrivateKey    string
	ClientID      string
	ClientSecret  string
	WebhookSecret string
}

// Environment variables read by ConfigFromEnv.
const (
	EnvAppID         = "GITHUB_APP_ID"
	EnvSlug          = "GITHUB_APP_SLUG"
	EnvPrivateKey    = "GITHUB_APP_PRIVATE_KEY"
	EnvClientID      = "GITHUB_APP_CLIENT_ID"
	EnvClientSecret  = "GITHUB_APP_CLIENT_SECRET"
	EnvWebhookSecret = "GITHUB_APP_WEBHOOK_SECRET"
)

// ConfigFromEnv reads the six GITHUB_APP_* settings. Missing ones are not an error (Missing names them).
func ConfigFromEnv(getenv func(string) string) Config {
	return Config{
		AppID:         strings.TrimSpace(getenv(EnvAppID)),
		Slug:          strings.TrimSpace(getenv(EnvSlug)),
		PrivateKey:    strings.TrimSpace(getenv(EnvPrivateKey)),
		ClientID:      strings.TrimSpace(getenv(EnvClientID)),
		ClientSecret:  strings.TrimSpace(getenv(EnvClientSecret)),
		WebhookSecret: strings.TrimSpace(getenv(EnvWebhookSecret)),
	}
}

// Missing names the settings still needed before Autofix can be used, empty when none.
func (c Config) Missing() []string {
	var missing []string
	for _, setting := range []struct{ name, value string }{
		{EnvAppID, c.AppID}, {EnvSlug, c.Slug}, {EnvPrivateKey, c.PrivateKey},
		{EnvClientID, c.ClientID}, {EnvClientSecret, c.ClientSecret}, {EnvWebhookSecret, c.WebhookSecret},
	} {
		if setting.value == "" {
			missing = append(missing, setting.name)
		}
	}
	return missing
}

// Enabled reports whether Autofix can be used on this server.
func (c Config) Enabled() bool { return len(c.Missing()) == 0 }
