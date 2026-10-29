package helpcenter

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

// SiteURL is the public address of a workspace's help center, without a trailing slash: its active
// custom domain when custom is set; else /help on the workspace's own host name <slug>.<domain> when
// the server has a help center domain and the slug can be a host name (the root of that host is the
// way into the workspace's desk); else <origin>/help/<slug>. A host name keeps the scheme and port of
// the origin, so a local server works the same way. Every address the server hands out comes from
// here.
func SiteURL(origin, domain, slug, custom string) string {
	origin = strings.TrimRight(origin, "/")
	host, base := custom, ""
	if host == "" {
		host, base = SlugHost(domain, slug), workspaceHostBase
	}
	if host == "" {
		return origin + "/help/" + slug
	}
	scheme, port := "https", ""
	if parsed, err := url.Parse(origin); err == nil {
		if parsed.Scheme != "" {
			scheme = parsed.Scheme
		}
		if parsed.Port() != "" {
			port = ":" + parsed.Port()
		}
	}
	return scheme + "://" + host + port + base
}

// URL is SiteURL for a workspace without a custom domain.
func URL(origin, domain, slug string) string { return SiteURL(origin, domain, slug, "") }

// ArticleURL is the public address of an article: /a/<number>-<slug> below the help center's address
// (SiteURL). The number alone finds the article; a wrong or old slug redirects to this address.
func ArticleURL(origin, domain, slug, custom string, number int64, articleSlug string) string {
	return SiteURL(origin, domain, slug, custom) + ArticlePath(number, articleSlug)
}

// ArticlePath is an article's path below its help center's address.
func ArticlePath(number int64, articleSlug string) string {
	return "/a/" + strconv.FormatInt(number, 10) + "-" + url.PathEscape(articleSlug)
}

// CollectionPath is a collection's path below its help center's address: /c/ and the slugs of the
// collections from the top down to it (a slug is unique only among siblings).
func CollectionPath(slugs []string) string {
	escaped := make([]string, len(slugs))
	for index, slug := range slugs {
		escaped[index] = url.PathEscape(slug)
	}
	return "/c/" + strings.Join(escaped, "/")
}

// hostSlug returns the workspace slug of a request host <slug>.<domain>, or false when the host is
// not exactly one valid label below the help center domain, or is the server's own host.
func hostSlug(host, domain, originHost string) (string, bool) {
	if domain == "" {
		return "", false
	}
	host = strings.ToLower(host)
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	host = strings.TrimSuffix(host, ".")
	if host == originHost {
		return "", false
	}
	slug, found := strings.CutSuffix(host, "."+domain)
	if !found || email.ValidateHostedSlug(slug) != nil {
		return "", false
	}
	return slug, true
}

// DomainSlug returns the slug of a host name the reverse proxy asks a certificate for, or false when
// it is not exactly one label below domain that can be a workspace's host name.
func DomainSlug(name, domain string) (string, bool) {
	return hostSlug(name, domain, "")
}

// DomainClaim returns the claim of a host name the reverse proxy asks a certificate for when it is a
// claim's CNAME target hc-<claim>.<domain>, or false.
func DomainClaim(name, domain string) (string, bool) {
	return claimOfHost(requestHost(name), domain)
}

// DomainFromEnv reads HELP_CENTER_DOMAIN: the domain under which each workspace's help center gets
// its own host name. Empty keeps every help center on <APP_ORIGIN>/help/<slug>.
func DomainFromEnv(getenv func(string) string) (string, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(getenv("HELP_CENTER_DOMAIN"))), ".")
	if domain != "" && (!email.ValidHostname(domain) || !strings.Contains(domain, ".")) {
		return "", fmt.Errorf("HELP_CENTER_DOMAIN must be a domain name such as help.example.com, got %q", domain)
	}
	return domain, nil
}
