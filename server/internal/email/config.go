package email

import (
	"regexp"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/configerr"
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
	// ResendWebhookSecret turns on POST /api/v1/webhooks/resend, which marks replies the relay could not
	// deliver. Relays such as Resend own the envelope sender, so their bounces never reach MAIL_FROM_DOMAIN.
	ResendWebhookSecret string
	// ResendAPIKey turns on domain verification through the Resend Domains API: the workspace publishes the
	// records Resend lists (DKIM, SPF) instead of a TXT record. Empty keeps the TXT check.
	ResendAPIKey string
	// ResendAPIBase replaces the Resend API address; tests point it at a local server. Not read from the environment.
	ResendAPIBase string
}

// ReceivesPlatformReplies is whether this server takes mail for MailFromDomain (its forwarder has
// MAIL_FROM_INBOUND_SECRET): only then may a reply ask for the customer's answer at reply+<token>@MailFromDomain.
func (c Config) ReceivesPlatformReplies() bool {
	return c.MailFromDomain != "" && c.PlatformInboundSecret != ""
}

// ConfigFromEnv reads SMTP_*, EMAIL_FROM_POLICY (verified), MAIL_FROM_DOMAIN, MAIL_FROM_INBOUND_SECRET and
// EMAIL_TRUSTED_AUTHSERV_ID, HOSTED_MAIL_DOMAIN, RESEND_WEBHOOK_SECRET and RESEND_API_KEY.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	smtp, err := SMTPConfigFromEnv(getenv)
	if err != nil {
		return Config{}, err
	}
	config := Config{SMTP: smtp, FromPolicy: FromPolicyVerified, MailFromDomain: strings.ToLower(strings.TrimSpace(getenv("MAIL_FROM_DOMAIN"))),
		TrustedAuthServID: strings.TrimSpace(getenv("EMAIL_TRUSTED_AUTHSERV_ID")), PlatformInboundSecret: strings.TrimSpace(getenv("MAIL_FROM_INBOUND_SECRET")),
		HostedMailDomain: strings.ToLower(strings.TrimSpace(getenv("HOSTED_MAIL_DOMAIN"))), ResendWebhookSecret: strings.TrimSpace(getenv("RESEND_WEBHOOK_SECRET")),
		ResendAPIKey: strings.TrimSpace(getenv("RESEND_API_KEY"))}
	if value := strings.TrimSpace(getenv("EMAIL_FROM_POLICY")); value != "" {
		config.FromPolicy = value
	}
	if config.FromPolicy != FromPolicyVerified && config.FromPolicy != FromPolicyAny {
		return Config{}, configerr.Say("email_from_policy_invalid", configerr.Params{"value": config.FromPolicy})
	}
	if config.MailFromDomain != "" && !hostnamePattern.MatchString(config.MailFromDomain) {
		return Config{}, configerr.Say("mail_from_domain_invalid", configerr.Params{"value": config.MailFromDomain})
	}
	if config.HostedMailDomain != "" && !hostnamePattern.MatchString(config.HostedMailDomain) {
		return Config{}, configerr.Say("hosted_mail_domain_invalid", configerr.Params{"value": config.HostedMailDomain})
	}
	if config.PlatformInboundSecret != "" && (len(config.PlatformInboundSecret) < 32 || (config.MailFromDomain == "" && config.HostedMailDomain == "")) {
		return Config{}, configerr.Say("mail_from_inbound_secret_needs", configerr.Params{"min": 32})
	}
	if config.HostedMailDomain != "" && config.PlatformInboundSecret == "" {
		return Config{}, configerr.Say("hosted_mail_needs_inbound_secret", nil)
	}
	if config.MailFromDomain != "" && HostedSlug("reply@"+config.MailFromDomain, config.HostedMailDomain) != "" {
		return Config{}, configerr.Say("mail_from_domain_slug", configerr.Params{"value": config.MailFromDomain})
	}
	if config.ResendWebhookSecret != "" && config.MailFromDomain == "" {
		// The webhook finds the reply by the reply+<token>@MAIL_FROM_DOMAIN address it was sent from.
		return Config{}, configerr.Say("resend_webhook_needs_mail_from", nil)
	}
	return config, nil
}
