// Package serverconfig keeps the server's configuration in the database instead of the environment
// (docs/internals/operator-console.md). The environment holds only what opens the database and serves the
// first request (Bootstrap); every other setting is a key here, saved as versions an operator restores.
package serverconfig

import (
	"slices"
	"strings"
)

// A Guard is a check a change must pass before it is saved (docs/self-host/operator-console.md).
type Guard string

const (
	// GuardReauth asks for the operator's authenticator code again.
	GuardReauth Guard = "reauth"
	// GuardReenterSecret asks for the secret this endpoint is bound to again (Key.Reenter names it).
	GuardReenterSecret Guard = "reenter_secret"
	// GuardPreview shows what the change does, with numbers, before it is saved.
	GuardPreview Guard = "preview"
	// GuardTypeToConfirm asks the operator to type a phrase.
	GuardTypeToConfirm Guard = "type_to_confirm"
	// GuardTrial applies the change for 10 minutes and reverts it unless the operator confirms.
	GuardTrial Guard = "trial"
)

// Console groups: one page of the operator console each.
const (
	GroupAI        = "ai"
	GroupEmail     = "email"
	GroupMailboxes = "mailboxes"
	GroupBilling   = "billing"
	GroupSignups   = "signups"
	GroupSecurity  = "security"
	GroupLimits    = "limits"
	GroupOperator  = "operator"
)

// Key is one setting. Name is its stable dotted name (the database, the API, the console); Env is the
// environment variable it replaced, read only by the first import and by the leftover warning.
type Key struct {
	Name    string
	Env     string
	Secret  bool
	Hot     bool   // takes effect at once; the others restart the server's workers
	Default string // what an unset key means, for people; empty means off or none
	Group   string
	Import  string // the import group: a group is imported from the environment whole or not at all
	Guards  []Guard
	Reenter string // the secret GuardReenterSecret asks for
}

// keys lists every setting that moved from the environment into the database.
var keys = []Key{
	{Name: "ai.server.provider", Env: "AI_SERVER_PROVIDER", Group: GroupAI, Import: "ai-server"},
	{Name: "ai.server.api_key", Env: "AI_SERVER_API_KEY", Secret: true, Group: GroupAI, Import: "ai-server", Guards: []Guard{GuardReauth}},
	{Name: "ai.server.base_url", Env: "AI_SERVER_BASE_URL", Group: GroupAI, Import: "ai-server", Guards: []Guard{GuardReauth, GuardReenterSecret}, Reenter: "ai.server.api_key"},
	{Name: "ai.server.models", Env: "AI_SERVER_MODELS", Group: GroupAI, Import: "ai-server"},
	{Name: "ai.server.label", Env: "AI_SERVER_LABEL", Default: "Server provider", Group: GroupAI, Import: "ai-server"},
	{Name: "ai.server.prices", Env: "AI_SERVER_PRICES", Hot: true, Group: GroupAI, Import: "ai-server"},
	{Name: "ai.server.monthly_allowance", Env: "AI_SERVER_MONTHLY_ALLOWANCE", Hot: true, Group: GroupAI, Import: "ai-server"},

	{Name: "smtp.host", Env: "SMTP_HOST", Group: GroupEmail, Import: "email", Guards: []Guard{GuardReauth, GuardReenterSecret}, Reenter: "smtp.password"},
	{Name: "smtp.port", Env: "SMTP_PORT", Default: "587", Group: GroupEmail, Import: "email", Guards: []Guard{GuardReauth, GuardReenterSecret}, Reenter: "smtp.password"},
	{Name: "smtp.username", Env: "SMTP_USERNAME", Group: GroupEmail, Import: "email"},
	{Name: "smtp.password", Env: "SMTP_PASSWORD", Secret: true, Group: GroupEmail, Import: "email", Guards: []Guard{GuardReauth}},
	{Name: "smtp.starttls", Env: "SMTP_STARTTLS", Default: "true", Group: GroupEmail, Import: "email", Guards: []Guard{GuardReauth, GuardReenterSecret}, Reenter: "smtp.password"},
	{Name: "email.from_policy", Env: "EMAIL_FROM_POLICY", Default: "verified", Group: GroupEmail, Import: "email"},
	{Name: "mail_from.domain", Env: "MAIL_FROM_DOMAIN", Group: GroupEmail, Import: "email"},
	{Name: "mail_from.inbound_secret", Env: "MAIL_FROM_INBOUND_SECRET", Secret: true, Group: GroupEmail, Import: "email", Guards: []Guard{GuardReauth}},
	{Name: "email.trusted_authserv_id", Env: "EMAIL_TRUSTED_AUTHSERV_ID", Group: GroupEmail, Import: "email"},
	{Name: "hosted_mail.domain", Env: "HOSTED_MAIL_DOMAIN", Group: GroupEmail, Import: "email"},
	{Name: "resend.webhook_secret", Env: "RESEND_WEBHOOK_SECRET", Secret: true, Group: GroupEmail, Import: "email", Guards: []Guard{GuardReauth}},
	{Name: "resend.api_key", Env: "RESEND_API_KEY", Secret: true, Group: GroupEmail, Import: "email", Guards: []Guard{GuardReauth}},

	{Name: "google.client_id", Env: "GOOGLE_OAUTH_CLIENT_ID", Group: GroupMailboxes, Import: "google"},
	{Name: "google.client_secret", Env: "GOOGLE_OAUTH_CLIENT_SECRET", Secret: true, Group: GroupMailboxes, Import: "google", Guards: []Guard{GuardReauth}},
	{Name: "microsoft.client_id", Env: "MICROSOFT_OAUTH_CLIENT_ID", Group: GroupMailboxes, Import: "microsoft"},
	{Name: "microsoft.client_secret", Env: "MICROSOFT_OAUTH_CLIENT_SECRET", Secret: true, Group: GroupMailboxes, Import: "microsoft", Guards: []Guard{GuardReauth}},
	{Name: "microsoft.tenant", Env: "MICROSOFT_OAUTH_TENANT", Default: "common", Group: GroupMailboxes, Import: "microsoft"},
	{Name: "github.app_id", Env: "GITHUB_APP_ID", Group: GroupMailboxes, Import: "github"},
	{Name: "github.slug", Env: "GITHUB_APP_SLUG", Group: GroupMailboxes, Import: "github"},
	{Name: "github.client_id", Env: "GITHUB_APP_CLIENT_ID", Group: GroupMailboxes, Import: "github"},
	{Name: "github.private_key", Env: "GITHUB_APP_PRIVATE_KEY", Secret: true, Group: GroupMailboxes, Import: "github", Guards: []Guard{GuardReauth}},
	{Name: "github.client_secret", Env: "GITHUB_APP_CLIENT_SECRET", Secret: true, Group: GroupMailboxes, Import: "github", Guards: []Guard{GuardReauth}},
	{Name: "github.webhook_secret", Env: "GITHUB_APP_WEBHOOK_SECRET", Secret: true, Group: GroupMailboxes, Import: "github", Guards: []Guard{GuardReauth}},

	{Name: "polar.access_token", Env: "POLAR_ACCESS_TOKEN", Secret: true, Group: GroupBilling, Import: "polar", Guards: []Guard{GuardReauth, GuardPreview, GuardTypeToConfirm}},
	{Name: "polar.webhook_secret", Env: "POLAR_WEBHOOK_SECRET", Secret: true, Group: GroupBilling, Import: "polar", Guards: []Guard{GuardReauth}},
	{Name: "polar.api_url", Env: "POLAR_API_URL", Default: "https://api.polar.sh", Group: GroupBilling, Import: "polar", Guards: []Guard{GuardReauth, GuardReenterSecret}, Reenter: "polar.access_token"},
	{Name: "billing.alert_email", Env: "BILLING_ALERT_EMAIL", Hot: true, Group: GroupBilling, Import: "billing-alert"},
	{Name: "retention.delete", Env: "RETENTION_DELETE", Group: GroupBilling, Import: "retention", Guards: []Guard{GuardReauth, GuardPreview, GuardTypeToConfirm}},
	{Name: "retention.max_deletions_per_day", Env: "RETENTION_MAX_DELETIONS_PER_DAY", Default: "5", Group: GroupBilling, Import: "retention", Guards: []Guard{GuardReauth}},

	{Name: "internal_signup.emails", Env: "INTERNAL_SIGNUP_EMAILS", Hot: true, Group: GroupSignups, Import: "internal-signup"},
	{Name: "internal_signup.networks", Env: "INTERNAL_SIGNUP_NETWORKS", Hot: true, Group: GroupSignups, Import: "internal-signup"},

	// Unset trusts no proxy, as the unset variable did. The installer saves InstallerTrustedProxies.
	{Name: "trusted_proxies", Env: "TRUSTED_PROXIES", Group: GroupSecurity, Import: "trusted-proxies", Guards: []Guard{GuardReauth, GuardPreview, GuardTrial}},
	{Name: "cloud_auth.issuer", Env: "CLOUD_AUTH_ISSUER", Group: GroupSecurity, Import: "cloud-auth", Guards: []Guard{GuardReauth, GuardTypeToConfirm, GuardTrial}},
	{Name: "cloud_auth.jwks_url", Env: "CLOUD_AUTH_JWKS_URL", Group: GroupSecurity, Import: "cloud-auth", Guards: []Guard{GuardReauth, GuardTypeToConfirm, GuardTrial}},
	{Name: "cloud_auth.authorized_parties", Env: "CLOUD_AUTH_AUTHORIZED_PARTIES", Group: GroupSecurity, Import: "cloud-auth", Guards: []Guard{GuardReauth, GuardTypeToConfirm, GuardTrial}},
	{Name: "cloud_auth.email_claim", Env: "CLOUD_AUTH_EMAIL_CLAIM", Default: "email", Group: GroupSecurity, Import: "cloud-auth", Guards: []Guard{GuardReauth, GuardTypeToConfirm, GuardTrial}},
	{Name: "clerk.publishable_key", Env: "CLERK_PUBLISHABLE_KEY", Group: GroupSecurity, Import: "cloud-auth", Guards: []Guard{GuardReauth, GuardTypeToConfirm, GuardTrial}},
	{Name: "help_center.domain", Env: "HELP_CENTER_DOMAIN", Group: GroupSecurity, Import: "help-center", Guards: []Guard{GuardReauth, GuardPreview, GuardTrial}},
	{Name: "ai.allow_private_base_urls", Env: "AI_ALLOW_PRIVATE_BASE_URLS", Group: GroupSecurity, Import: "ai-private"},

	{Name: "mail_image_cache.max_bytes", Env: "MAIL_IMAGE_CACHE_BYTES", Default: "1073741824", Hot: true, Group: GroupLimits, Import: "limits"},
	{Name: "kb_video.max_bytes", Env: "KB_VIDEO_MAX_BYTES", Default: "209715200", Hot: true, Group: GroupLimits, Import: "limits", Guards: []Guard{GuardPreview}},
	{Name: "problem_report.workspace", Env: "PROBLEM_REPORT_WORKSPACE", Hot: true, Group: GroupLimits, Import: "problem-report", Guards: []Guard{GuardPreview}},
	// The environment can still turn the usage report off (DISABLE_TELEMETRY, DO_NOT_TRACK), never on.
	{Name: "telemetry.enabled", Default: "true", Group: GroupLimits},

	// Turning it off is for the CLI only, and refused on the hosted cloud.
	{Name: "operator.require_totp", Default: "false", Hot: true, Group: GroupOperator},
}

// InstallerTrustedProxies is what the installer saves for trusted_proxies, as its compose file passed it
// before the setting moved: loopback and the Docker bridge range, where a reverse proxy on the same host
// connects from. The server itself trusts no proxy until one is set.
const InstallerTrustedProxies = "127.0.0.0/8,::1/128,172.16.0.0/12"

// Product is one Polar product the server sells or once sold (polar_products); Env is the variable that
// held the product sold for its plan and period before the table.
type Product struct {
	Env    string
	Plan   string
	Period string
}

// ProductEnvs are the nine POLAR_PRODUCT_* variables, imported as rows of polar_products (group polar).
var ProductEnvs = []Product{
	{Env: "POLAR_PRODUCT_TEAM_MONTHLY", Plan: "team", Period: "monthly"},
	{Env: "POLAR_PRODUCT_TEAM_YEARLY", Plan: "team", Period: "yearly"},
	{Env: "POLAR_PRODUCT_FOUNDING", Plan: "founding", Period: "single"},
	{Env: "POLAR_PRODUCT_SOLO_MONTHLY", Plan: "solo", Period: "monthly"},
	{Env: "POLAR_PRODUCT_SOLO_YEARLY", Plan: "solo", Period: "yearly"},
	{Env: "POLAR_PRODUCT_SMALL_MONTHLY", Plan: "small", Period: "monthly"},
	{Env: "POLAR_PRODUCT_SMALL_YEARLY", Plan: "small", Period: "yearly"},
	{Env: "POLAR_PRODUCT_TEAM25_MONTHLY", Plan: "team25", Period: "monthly"},
	{Env: "POLAR_PRODUCT_TEAM25_YEARLY", Plan: "team25", Period: "yearly"},
}

// SignupHashKeyEnv held the key of the sign-up record hashes. It moved into server_secrets, never to be
// changed (changing it forgets who had a trial): imported once, else generated.
const SignupHashKeyEnv = "SIGNUP_HASH_KEY"

// SignupHashKeySecret is its name in server_secrets.
const SignupHashKeySecret = "signup-hash-key"

// Bootstrap is what stays in the environment: what opens the database, what the CLI prints links with
// before any server runs, where it listens and stores files, and the ring that seals the secrets here.
var Bootstrap = []string{"DATABASE_URL", "DATABASE_REQUEST_URL", "APP_ORIGIN", "OPERATOR_ORIGIN", "LISTEN_ADDR", "ATTACHMENTS_DIR", "MAILBOX_TOKEN_KEYS"}

// TelemetryOffEnvs only ever turn the usage report off.
var TelemetryOffEnvs = []string{"DISABLE_TELEMETRY", "DO_NOT_TRACK"}

// DeadEnvs are variables no release reads; a leftover one is harmless and the doctor says so.
var DeadEnvs = []string{"APP_SECRET"}

// Keys returns every key, in the order of the console.
func Keys() []Key { return slices.Clone(keys) }

// Lookup finds a key by its dotted name or by the environment variable it replaced.
func Lookup(name string) (Key, bool) {
	name = strings.TrimSpace(name)
	for _, key := range keys {
		if key.Name == name || (key.Env != "" && key.Env == name) {
			return key, true
		}
	}
	return Key{}, false
}

// ImportGroups lists the import groups in the order the first start imports them.
func ImportGroups() []string {
	var groups []string
	for _, key := range keys {
		if key.Import != "" && !slices.Contains(groups, key.Import) {
			groups = append(groups, key.Import)
		}
	}
	return groups
}

// GroupKeys lists the keys of one import group.
func GroupKeys(group string) []Key {
	var found []Key
	for _, key := range keys {
		if key.Import == group {
			found = append(found, key)
		}
	}
	return found
}

// MovedEnvs lists every variable whose setting now lives in the database: the keys', the products', and the
// sign-up hash key.
func MovedEnvs() []string {
	var names []string
	for _, key := range keys {
		if key.Env != "" {
			names = append(names, key.Env)
		}
	}
	for _, product := range ProductEnvs {
		names = append(names, product.Env)
	}
	return append(names, SignupHashKeyEnv)
}
