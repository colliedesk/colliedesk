package email_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

func TestValidateHostedSlug(t *testing.T) {
	// A name that only starts like a custom domain's CNAME target is an ordinary slug ("HC Labs").
	for _, slug := range []string{"acme", "acme-support", "a1", "x", strings.Repeat("a", 63), "hc-labs", "hc-0123456789abcdef0123456", "hc-0123456789abcdef012345678"} {
		if err := email.ValidateHostedSlug(slug); err != nil {
			t.Errorf("%q rejected: %v", slug, err)
		}
	}
	for name, slug := range map[string]string{
		"empty":         "",
		"uppercase":     "Acme",
		"leading dash":  "-acme",
		"trailing dash": "acme-",
		"underscore":    "acme_inc",
		"dot":           "a.b",
		"too long":      strings.Repeat("a", 64),
		"reserved app":  "app",
		"reserved www":  "www",
		"reserved mail": "mail",
		"reserved in":   "in",
		"reserved help": "help",
		// colliedesk.com has its own DNS records for these names (staging app, Clerk).
		"reserved staging":      "staging",
		"reserved clerk":        "clerk",
		"reserved clkmail":      "clkmail",
		"reserved help-staging": "help-staging",
		// The operator console's own host (OPERATOR_ORIGIN) is one of these on the hosted cloud.
		"reserved admin":    "admin",
		"reserved ops":      "ops",
		"reserved operator": "operator",
		"reserved console":  "console",
		"punycode":          "xn--acme",
		// hc-<24 hex digits> is the CNAME target of a custom help center domain's claim.
		"claim target": "hc-0123456789abcdef01234567",
	} {
		if err := email.ValidateHostedSlug(slug); err == nil {
			t.Errorf("%s (%q) accepted", name, slug)
		}
	}
}

func TestHostedAddress(t *testing.T) {
	if got := email.HostedAddress("acme", "colliedesk.com"); got != "support@acme.colliedesk.com" {
		t.Fatalf("address = %q", got)
	}
	if got := email.HostedAddress("acme", ""); got != "" {
		t.Fatalf("feature off must give no address, got %q", got)
	}
	if got := email.HostedAddress("app", "colliedesk.com"); got != "" {
		t.Fatalf("a reserved slug must give no address, got %q", got)
	}
}

func TestHostedSlug(t *testing.T) {
	for name, tc := range map[string]struct{ recipient, want string }{
		"support":        {"support@acme.colliedesk.com", "acme"},
		"any local part": {"billing@acme.colliedesk.com", "acme"},
		"plus address":   {"support+r-0123@acme.colliedesk.com", "acme"},
		"uppercase":      {"Support@ACME.Colliedesk.com", "acme"},
		"apex":           {"support@colliedesk.com", ""},
		"nested":         {"support@a.b.colliedesk.com", ""},
		"other domain":   {"support@acme.example.com", ""},
		"lookalike":      {"support@acme.evilcolliedesk.com", ""},
		"reserved":       {"support@app.colliedesk.com", ""},
		"mail sender":    {"support@send.colliedesk.com", ""},
		"return path":    {"support@rsend.colliedesk.com", ""},
		"resend":         {"support@resend.colliedesk.com", ""},
		"bounce host":    {"support@bounce.colliedesk.com", ""},
		"no at":          {"acme.colliedesk.com", ""},
	} {
		if got := email.HostedSlug(tc.recipient, "colliedesk.com"); got != tc.want {
			t.Errorf("%s: HostedSlug(%q) = %q, want %q", name, tc.recipient, got, tc.want)
		}
	}
	if got := email.HostedSlug("support@acme.colliedesk.com", ""); got != "" {
		t.Errorf("feature off: %q", got)
	}
}

func TestParseKeepsOnlyTheTopmostEnvelopeRecipient(t *testing.T) {
	raw := "X-Original-To: support@acme.colliedesk.com\r\nX-Original-To: support@victim.colliedesk.com\r\n" +
		"From: a@customer.example\r\nTo: support@victim.colliedesk.com\r\nSubject: s\r\nMessage-ID: <1@c.example>\r\n\r\nhi\r\n"
	parsed, err := email.Parse([]byte(raw), "")
	if err != nil || parsed.EnvelopeTo != "support@acme.colliedesk.com" {
		t.Fatalf("envelope = %q, %v", parsed.EnvelopeTo, err)
	}
}
