package email

import (
	"errors"
	"regexp"
	"strings"
)

// hostedLabel is a DNS label: lowercase letters, digits and inner hyphens, at most 63 characters.
var hostedLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// reservedSlugs are names a platform needs for its own hosts or that read as official mail senders.
// A workspace's slug is also its subdomain on the hosted cloud (mail address and help center host), so
// every one-label name that has its own DNS record under the hosted domain belongs here: such a record
// wins over the wildcard and would send the workspace's mail and help center elsewhere. Add the name
// here whenever such a record is added (colliedesk.com today: app, www, mx, mail, send, rsend, staging,
// and Clerk's clerk, accounts and clkmail). Reserving a name refuses it for a new or changed slug and
// gives it no hosted address or help center host; a workspace that already has it keeps signing in,
// its data, and its help center on the path address.
var reservedSlugs = map[string]bool{
	"abuse": true, "account": true, "accounts": true, "admin": true, "api": true, "app": true, "autoconfig": true,
	"autodiscover": true, "billing": true, "bounce": true, "bounces": true, "em": true, "resend": true, "rsend": true, "send": true, "blog": true, "cdn": true, "clerk": true, "clkmail": true, "dashboard": true, "dkim": true, "dmarc": true,
	"docs": true, "ftp": true, "help": true, "hostmaster": true, "imap": true, "in": true, "inbound": true, "login": true,
	"mail": true, "mx": true, "ns": true, "ns1": true, "ns2": true, "postmaster": true, "root": true, "security": true,
	"sales": true, "signin": true, "smtp": true, "spf": true, "staging": true, "static": true, "status": true, "support": true, "test": true,
	"webmail": true, "webmaster": true, "www": true,
}

// HelpCenterClaimLabel is the first label of a custom help center domain's CNAME target,
// hc-<claim>.<HELP_CENTER_DOMAIN>, where the claim is 24 random hex digits; the submatch is the claim.
// No slug may have this shape, so a claim's target is never a workspace's host name. Only the exact
// shape is reserved: a slug such as hc-labs stays an ordinary one.
var HelpCenterClaimLabel = regexp.MustCompile(`^hc-([0-9a-f]{24})$`)

// ValidateHostedSlug reports why a workspace slug cannot be the mail subdomain of a hosted address.
func ValidateHostedSlug(slug string) error {
	switch {
	case !hostedLabel.MatchString(slug):
		return errors.New("use 1 to 63 lowercase letters, digits or hyphens, starting and ending with a letter or digit")
	case strings.HasPrefix(slug, "xn--"):
		return errors.New("a slug cannot start with xn--")
	case reservedSlugs[slug], HelpCenterClaimLabel.MatchString(slug):
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
	return HostedLocalAddress("support", slug, hostedDomain)
}

// HostedLocalAddress is the forwarding address of one email channel of a workspace on the hosted cloud,
// <local>@<slug>.<hostedDomain>, or "" when the feature is off.
func HostedLocalAddress(local, slug, hostedDomain string) string {
	if hostedDomain == "" || ValidateHostedSlug(slug) != nil || local == "" {
		return ""
	}
	return local + "@" + slug + "." + hostedDomain
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

// ForDomain reports whether the address is exactly at domain (not a subdomain), ignoring case.
func ForDomain(address, domain string) bool {
	at := strings.LastIndex(address, "@")
	return at > 0 && domain != "" && strings.EqualFold(address[at+1:], domain)
}

// ValidHostname reports whether name is a domain name the server can be configured with.
func ValidHostname(name string) bool { return hostnamePattern.MatchString(name) }
