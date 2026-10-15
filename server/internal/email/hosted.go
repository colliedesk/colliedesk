package email

import (
	"errors"
	"regexp"
	"strings"
)

// hostedLabel is a DNS label: lowercase letters, digits and inner hyphens, at most 63 characters.
var hostedLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// reservedSlugs are names a platform needs for its own hosts or that read as official mail senders.
var reservedSlugs = map[string]bool{
	"abuse": true, "account": true, "accounts": true, "admin": true, "api": true, "app": true, "autoconfig": true,
	"autodiscover": true, "billing": true, "bounce": true, "bounces": true, "em": true, "resend": true, "rsend": true, "send": true, "blog": true, "cdn": true, "dashboard": true, "dkim": true, "dmarc": true,
	"docs": true, "ftp": true, "help": true, "hostmaster": true, "imap": true, "in": true, "inbound": true, "login": true,
	"mail": true, "mx": true, "ns": true, "ns1": true, "ns2": true, "postmaster": true, "root": true, "security": true,
	"sales": true, "signin": true, "smtp": true, "spf": true, "static": true, "status": true, "support": true, "test": true,
	"webmail": true, "webmaster": true, "www": true,
}

// ValidateHostedSlug reports why a workspace slug cannot be the mail subdomain of a hosted address.
func ValidateHostedSlug(slug string) error {
	switch {
	case !hostedLabel.MatchString(slug):
		return errors.New("use 1 to 63 lowercase letters, digits or hyphens, starting and ending with a letter or digit")
	case strings.HasPrefix(slug, "xn--"):
		return errors.New("a slug cannot start with xn--")
	case reservedSlugs[slug]:
		return errors.New("this name is reserved")
	}
	return nil
}

// HostedAddress is the forwarding address of a workspace on the hosted cloud, or "" when the feature
// is off (empty hostedDomain) or the slug cannot be a mail subdomain.
func HostedAddress(slug, hostedDomain string) string {
	if hostedDomain == "" || ValidateHostedSlug(slug) != nil {
		return ""
	}
	return "support@" + slug + "." + hostedDomain
}

// HostedSlug returns the workspace slug of a recipient <local>@<slug>.<hostedDomain>, or "" when the
// recipient is not exactly one valid label below the hosted domain.
func HostedSlug(recipient, hostedDomain string) string {
	if hostedDomain == "" {
		return ""
	}
	at := strings.LastIndex(recipient, "@")
	if at <= 0 {
		return ""
	}
	slug, ok := strings.CutSuffix(strings.ToLower(recipient[at+1:]), "."+strings.ToLower(hostedDomain))
	if !ok || ValidateHostedSlug(slug) != nil {
		return ""
	}
	return slug
}

// UnderDomain reports whether the address belongs to domain or any subdomain of it.
func UnderDomain(address, domain string) bool {
	at := strings.LastIndex(address, "@")
	if at <= 0 || domain == "" {
		return false
	}
	host, domain := strings.ToLower(address[at+1:]), strings.ToLower(domain)
	return host == domain || strings.HasSuffix(host, "."+domain)
}
