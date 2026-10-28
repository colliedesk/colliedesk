// Package github is the Colliedesk GitHub App that Autofix works through: the operator's App settings
// here; the App and user tokens, the REST and GraphQL calls and the webhook signature come with the
// GitHub connection. Plain net/http against GitHub's documented endpoints; no SDK.
package github

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Config is the operator's own GitHub App. All six settings are needed; with
// any missing, Autofix is off on this server and GET /api/v1/config names what is missing.
type Config struct {
	AppID         string
	Slug          string
	PrivateKey    string
	ClientID      string
	ClientSecret  string
	WebhookSecret string
	// Base replaces github.com and api.github.com with one local server (tests and the e2e stand-in).
	// Empty is GitHub.
	Base string
	HTTP *http.Client
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

// SigningKey parses GITHUB_APP_PRIVATE_KEY: the PEM GitHub gives (PKCS#1 or PKCS#8), as it is, with its
// line breaks written as \n (one line in an env file), or base64-encoded.
func (c Config) SigningKey() (*rsa.PrivateKey, error) {
	text := strings.TrimSpace(c.PrivateKey)
	if text == "" {
		return nil, errors.New("not set")
	}
	if !strings.Contains(text, "-----BEGIN") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, errors.New("neither a PEM private key nor base64 of one")
		}
		text = string(decoded)
	}
	text = strings.ReplaceAll(text, `\n`, "\n")
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not an RSA private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA private key")
	}
	return key, nil
}

// Check reports a setting that is present but cannot work (a private key that does not parse), so the
// server refuses to start instead of failing at the first GitHub call. A server without the settings
// passes: Autofix is simply off.
func (c Config) Check() error {
	if c.PrivateKey == "" {
		return nil
	}
	if _, err := c.SigningKey(); err != nil {
		return fmt.Errorf("%s: %w", EnvPrivateKey, err)
	}
	return nil
}
