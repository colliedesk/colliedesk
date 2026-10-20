package helpcenter

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

// URL is the public address of a workspace's help center, without a trailing slash: its own host
// name <slug>.<domain> when the server has a help center domain and the slug can be a host name,
// else <origin>/help/<slug>. The host name keeps the scheme and port of the origin, so a local server
// works the same way. Every address the server hands out comes from here.
func URL(origin, domain, slug string) string {
	origin = strings.TrimRight(origin, "/")
	if domain == "" || email.ValidateHostedSlug(slug) != nil {
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
	return scheme + "://" + slug + "." + domain + port
}

// ArticleURL is the public address of an article: /a/<number>-<slug> below the help center. The
// number alone finds the article; a wrong or old slug redirects to this address.
func ArticleURL(origin, domain, slug string, number int64, articleSlug string) string {
	return URL(origin, domain, slug) + articlePath(number, articleSlug)
}

func articlePath(number int64, articleSlug string) string {
	return "/a/" + strconv.FormatInt(number, 10) + "-" + url.PathEscape(articleSlug)
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

// DomainFromEnv reads HELP_CENTER_DOMAIN: the domain under which each workspace's help center gets
// its own host name. Empty keeps every help center on <APP_ORIGIN>/help/<slug>.
func DomainFromEnv(getenv func(string) string) (string, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(getenv("HELP_CENTER_DOMAIN"))), ".")
	if domain != "" && (!email.ValidHostname(domain) || !strings.Contains(domain, ".")) {
		return "", fmt.Errorf("HELP_CENTER_DOMAIN must be a domain name such as help.example.com, got %q", domain)
	}
	return domain, nil
}
