package email_test

import (
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
	env := map[string]string{"EMAIL_FROM_POLICY": "any", "MAIL_FROM_DOMAIN": "Mail.CollieDesk.example", "EMAIL_TRUSTED_AUTHSERV_ID": "mx.cloudflare.net"}
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
	env := map[string]string{"HOSTED_MAIL_DOMAIN": "CollieDesk.com", "MAIL_FROM_INBOUND_SECRET": secret}
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
