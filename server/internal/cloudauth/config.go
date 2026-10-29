package cloudauth

import (
	"encoding/base64"
	"regexp"
	"strings"
)

const defaultEmailClaim = "email"

type Config struct {
	Issuer            string
	JWKSURL           string
	AuthorizedParties []string
	PublishableKey    string
	EmailClaim        string
}

// ConfigFromEnv reads cloud sign-in's settings by their variable names (a configuration snapshot answers to
// them).
func ConfigFromEnv(getenv func(string) string) Config {
	return Config{
		Issuer:            getenv("CLOUD_AUTH_ISSUER"),
		JWKSURL:           getenv("CLOUD_AUTH_JWKS_URL"),
		AuthorizedParties: strings.Split(getenv("CLOUD_AUTH_AUTHORIZED_PARTIES"), ","),
		PublishableKey:    getenv("CLERK_PUBLISHABLE_KEY"),
		EmailClaim:        getenv("CLOUD_AUTH_EMAIL_CLAIM"),
	}
}

func (config Config) Enabled() bool {
	return config.Issuer != "" && config.JWKSURL != "" && config.PublishableKey != "" && len(config.authorizedParties()) > 0
}

func (config Config) emailClaim() string {
	if strings.TrimSpace(config.EmailClaim) == "" {
		return defaultEmailClaim
	}
	return strings.TrimSpace(config.EmailClaim)
}

func (config Config) authorizedParties() map[string]bool {
	parties := make(map[string]bool, len(config.AuthorizedParties))
	for _, party := range config.AuthorizedParties {
		if trimmed := strings.TrimSpace(party); trimmed != "" {
			parties[trimmed] = true
		}
	}
	return parties
}

// ForParty is the same identity provider accepting only tokens issued to party: the operator console checks
// its sign-ins with it, so a token of the app never opens the console and one of the console never the app.
func (config Config) ForParty(party string) Config {
	config.AuthorizedParties = []string{party}
	return config
}

var publishableKeyPattern = regexp.MustCompile(`^pk_(?:live|test)_([A-Za-z0-9+/=]+)$`)
var frontendHostPattern = regexp.MustCompile(`(?i)^[a-z0-9.-]+\.[a-z]{2,}$`)

// FrontendHost reads the host of the identity provider's Frontend API from a publishable key (pk_live_ or
// pk_test_ and base64 of "<host>$"), as the web app's clerkFrontendHost does; "" when the key names none.
func FrontendHost(publishableKey string) string {
	match := publishableKeyPattern.FindStringSubmatch(publishableKey)
	if match == nil {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(string(decoded), "$")
	if !frontendHostPattern.MatchString(host) {
		return ""
	}
	return host
}
