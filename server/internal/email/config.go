package email

import (
	"fmt"
	"regexp"
	"strings"
)

// From policies decide which address replies are sent from.
const (
	// FromPolicyVerified sends as the support address only after its domain is verified by DNS;
	// otherwise as "<workspace> via Colliedesk" <reply+token@MAIL_FROM_DOMAIN>. Use it on a shared relay.
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
	// PlatformInboundSecret authenticates the forwarder for MAIL_FROM_DOMAIN, which receives replies and
	// bounces addressed to reply+<token>@MAIL_FROM_DOMAIN for every workspace.
	PlatformInboundSecret string
	// HostedMailDomain turns on the hosted cloud address support@<workspace slug>.<HostedMailDomain>.
	// Empty (the self-host default) leaves it off. The same PlatformInboundSecret authenticates its forwarder.
	HostedMailDomain string
}

// ConfigFromEnv reads SMTP_*, EMAIL_FROM_POLICY (verified), MAIL_FROM_DOMAIN, MAIL_FROM_INBOUND_SECRET and
// EMAIL_TRUSTED_AUTHSERV_ID and HOSTED_MAIL_DOMAIN.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	smtp, err := SMTPConfigFromEnv(getenv)
	if err != nil {
		return Config{}, err
	}
	config := Config{SMTP: smtp, FromPolicy: FromPolicyVerified, MailFromDomain: strings.ToLower(strings.TrimSpace(getenv("MAIL_FROM_DOMAIN"))),
		TrustedAuthServID: strings.TrimSpace(getenv("EMAIL_TRUSTED_AUTHSERV_ID")), PlatformInboundSecret: strings.TrimSpace(getenv("MAIL_FROM_INBOUND_SECRET")),
		HostedMailDomain: strings.ToLower(strings.TrimSpace(getenv("HOSTED_MAIL_DOMAIN")))}
	if value := strings.TrimSpace(getenv("EMAIL_FROM_POLICY")); value != "" {
		config.FromPolicy = value
	}
	if config.FromPolicy != FromPolicyVerified && config.FromPolicy != FromPolicyAny {
		return Config{}, fmt.Errorf("EMAIL_FROM_POLICY must be verified or any, got %q", config.FromPolicy)
	}
	if config.MailFromDomain != "" && !hostnamePattern.MatchString(config.MailFromDomain) {
		return Config{}, fmt.Errorf("MAIL_FROM_DOMAIN must be a domain name, got %q", config.MailFromDomain)
	}
	if config.HostedMailDomain != "" && !hostnamePattern.MatchString(config.HostedMailDomain) {
		return Config{}, fmt.Errorf("HOSTED_MAIL_DOMAIN must be a domain name, got %q", config.HostedMailDomain)
	}
	if config.PlatformInboundSecret != "" && (len(config.PlatformInboundSecret) < 32 || (config.MailFromDomain == "" && config.HostedMailDomain == "")) {
		return Config{}, fmt.Errorf("MAIL_FROM_INBOUND_SECRET needs MAIL_FROM_DOMAIN or HOSTED_MAIL_DOMAIN and at least 32 characters")
	}
	if config.HostedMailDomain != "" && config.PlatformInboundSecret == "" {
		return Config{}, fmt.Errorf("HOSTED_MAIL_DOMAIN needs MAIL_FROM_INBOUND_SECRET for its forwarder")
	}
	return config, nil
}
