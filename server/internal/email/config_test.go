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
