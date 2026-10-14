package cloudauth

import "strings"

const defaultEmailClaim = "email"

type Config struct {
	Issuer            string
	JWKSURL           string
	AuthorizedParties []string
	PublishableKey    string
	EmailClaim        string
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
