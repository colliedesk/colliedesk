package email

import (
	"fmt"
	"regexp"
	"strings"
)

// From policies decide which address replies are sent from.
const (
	// FromPolicyVerified sends as the support address only after its domain is verified by DNS;
	// otherwise as "<workspace> via CollieDesk" <reply+token@MAIL_FROM_DOMAIN>. Use it on a shared relay.
	FromPolicyVerified = "verified"
	// FromPolicyAny always sends as the support address. Use it when the operator owns the relay.
	FromPolicyAny = "any"
)

var hostnamePattern = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// Config is the server-wide email configuration.
type Config struct {
	SMTP              SMTPConfig
	FromPolicy        string
	MailFromDomain    string
	TrustedAuthServID string
}

// ConfigFromEnv reads SMTP_*, EMAIL_FROM_POLICY (verified), MAIL_FROM_DOMAIN and EMAIL_TRUSTED_AUTHSERV_ID.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	smtp, err := SMTPConfigFromEnv(getenv)
	if err != nil {
		return Config{}, err
	}
	config := Config{SMTP: smtp, FromPolicy: FromPolicyVerified, MailFromDomain: strings.ToLower(strings.TrimSpace(getenv("MAIL_FROM_DOMAIN"))),
		TrustedAuthServID: strings.TrimSpace(getenv("EMAIL_TRUSTED_AUTHSERV_ID"))}
	if value := strings.TrimSpace(getenv("EMAIL_FROM_POLICY")); value != "" {
		config.FromPolicy = value
	}
	if config.FromPolicy != FromPolicyVerified && config.FromPolicy != FromPolicyAny {
		return Config{}, fmt.Errorf("EMAIL_FROM_POLICY must be verified or any, got %q", config.FromPolicy)
	}
	if config.MailFromDomain != "" && !hostnamePattern.MatchString(config.MailFromDomain) {
		return Config{}, fmt.Errorf("MAIL_FROM_DOMAIN must be a domain name, got %q", config.MailFromDomain)
	}
	return config, nil
}
