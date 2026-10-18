package email_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

func TestConfigFromEnvDefaultsToVerifiedPolicyAndValidates(t *testing.T) {
	config, err := email.ConfigFromEnv(func(string) string { return "" })
	if err != nil || config.FromPolicy != email.FromPolicyVerified || config.SMTP.Configured() || config.TrustedAuthServID != "" {
		t.Fatalf("defaults: %+v %v", config, err)
	}
	for name, env := range map[string]map[string]string{
		"policy": {"EMAIL_FROM_POLICY": "everyone"},
		"domain": {"MAIL_FROM_DOMAIN": "not a domain"},
	} {
		if _, err := email.ConfigFromEnv(func(key string) string { return env[key] }); err == nil {
			t.Errorf("invalid %s accepted", name)
		}
	}
	env := map[string]string{"EMAIL_FROM_POLICY": "any", "MAIL_FROM_DOMAIN": "Mail.Colliedesk.example", "EMAIL_TRUSTED_AUTHSERV_ID": "mx.cloudflare.net"}
	config, err = email.ConfigFromEnv(func(key string) string { return env[key] })
	if err != nil || config.FromPolicy != "any" || config.MailFromDomain != "mail.colliedesk.example" || config.TrustedAuthServID != "mx.cloudflare.net" {
		t.Fatalf("parsed: %+v %v", config, err)
	}
}

func TestConfigFromEnvReadsThePlatformInboundSecret(t *testing.T) {
	env := map[string]string{"MAIL_FROM_DOMAIN": "mail.example.com", "MAIL_FROM_INBOUND_SECRET": "0123456789abcdef0123456789abcdef"} // gitleaks:allow (test placeholder)
	config, err := email.ConfigFromEnv(func(key string) string { return env[key] })
	if err != nil || config.PlatformInboundSecret != env["MAIL_FROM_INBOUND_SECRET"] {
		t.Fatalf("platform secret not read: %+v %v", config, err)
	}
	for name, bad := range map[string]map[string]string{
		"short secret":     {"MAIL_FROM_DOMAIN": "mail.example.com", "MAIL_FROM_INBOUND_SECRET": "short"},
		"secret no domain": {"MAIL_FROM_INBOUND_SECRET": "0123456789abcdef0123456789abcdef"}, // gitleaks:allow (test placeholder)
	} {
		if _, err := email.ConfigFromEnv(func(key string) string { return bad[key] }); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestConfigFromEnvReadsTheHostedMailDomain(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef" // gitleaks:allow (test placeholder)
	env := map[string]string{"HOSTED_MAIL_DOMAIN": "Colliedesk.com", "MAIL_FROM_INBOUND_SECRET": secret}
	config, err := email.ConfigFromEnv(func(key string) string { return env[key] })
	if err != nil || config.HostedMailDomain != "colliedesk.com" || config.PlatformInboundSecret != secret {
		t.Fatalf("hosted domain not read: %+v %v", config, err)
	}
	if config, err = email.ConfigFromEnv(func(string) string { return "" }); err != nil || config.HostedMailDomain != "" {
		t.Fatalf("feature must default to off: %+v %v", config, err)
	}
	for name, bad := range map[string]map[string]string{
		"no secret":  {"HOSTED_MAIL_DOMAIN": "colliedesk.com"},
		"bad domain": {"HOSTED_MAIL_DOMAIN": "not a domain", "MAIL_FROM_INBOUND_SECRET": secret},
	} {
		if _, err := email.ConfigFromEnv(func(key string) string { return bad[key] }); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// MAIL_FROM_DOMAIN below HOSTED_MAIL_DOMAIN must not be a name a workspace can own as its slug.
func TestConfigFromEnvKeepsTheReplyDomainOutOfTheWorkspaceSlugs(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef" // gitleaks:allow (test placeholder)
	load := func(from string) error {
		env := map[string]string{"HOSTED_MAIL_DOMAIN": "colliedesk.com", "MAIL_FROM_DOMAIN": from, "MAIL_FROM_INBOUND_SECRET": secret}
		_, err := email.ConfigFromEnv(func(key string) string { return env[key] })
		return err
	}
	for _, ok := range []string{"mail.colliedesk.com", "bounce.colliedesk.com", "reply.mail.colliedesk.com", "colliedesk.com", "mail.example.org"} {
		if err := load(ok); err != nil {
			t.Errorf("MAIL_FROM_DOMAIN=%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"acme.colliedesk.com", "Desk.Colliedesk.com"} {
		if err := load(bad); err == nil || !strings.Contains(err.Error(), "MAIL_FROM_DOMAIN") {
			t.Errorf("MAIL_FROM_DOMAIN=%s accepted: %v", bad, err)
		}
	}
}

func TestConfigFromEnvReadsTheResendWebhookSecret(t *testing.T) {
	env := map[string]string{"MAIL_FROM_DOMAIN": "mail.example.com", "RESEND_WEBHOOK_SECRET": " whsec_c2VjcmV0 "} // gitleaks:allow (test placeholder)
	config, err := email.ConfigFromEnv(func(key string) string { return env[key] })
	if err != nil || config.ResendWebhookSecret != "whsec_c2VjcmV0" {
		t.Fatalf("resend webhook secret not read: %+v %v", config, err)
	}
	if config, err = email.ConfigFromEnv(func(string) string { return "" }); err != nil || config.ResendWebhookSecret != "" {
		t.Fatalf("webhook must default to off: %+v %v", config, err)
	}
	noDomain := map[string]string{"RESEND_WEBHOOK_SECRET": "whsec_c2VjcmV0"} // gitleaks:allow (test placeholder)
	if _, err := email.ConfigFromEnv(func(key string) string { return noDomain[key] }); err == nil || !strings.Contains(err.Error(), "MAIL_FROM_DOMAIN") {
		t.Fatalf("secret without MAIL_FROM_DOMAIN accepted: %v", err)
	}
}

func TestConfigFromEnvReadsTheResendAPIKey(t *testing.T) {
	env := map[string]string{"RESEND_API_KEY": " re_test_placeholder "} // gitleaks:allow (test placeholder)
	config, err := email.ConfigFromEnv(func(key string) string { return env[key] })
	if err != nil || config.ResendAPIKey != "re_test_placeholder" {
		t.Fatalf("resend api key not read: %+v %v", config, err)
	}
	if config, err = email.ConfigFromEnv(func(string) string { return "" }); err != nil || config.ResendAPIKey != "" {
		t.Fatalf("domain verification must default to the TXT record: %+v %v", config, err)
	}
}
