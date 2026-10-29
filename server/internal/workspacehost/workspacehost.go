// Package workspacehost names a workspace's own host name, <slug>.<HELP_CENTER_DOMAIN>, where its
// desk runs (and its help center under /help), and carries the host a request arrived on to the code
// that must act only for that workspace. Without HELP_CENTER_DOMAIN every workspace stays on
// APP_ORIGIN.
package workspacehost

import (
	"context"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

type slugKey struct{}

// With marks ctx as a request that arrived on the host of the workspace with slug.
func With(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, slugKey{}, slug)
}

// Slug is the slug of the workspace whose host the request arrived on, or false on APP_ORIGIN (and on
// every host of a server without workspace hosts).
func Slug(ctx context.Context) (string, bool) {
	slug, ok := ctx.Value(slugKey{}).(string)
	return slug, ok && slug != ""
}

// Host is the workspace's own host name, <slug>.<domain>, or "" when the server has no domain or the
// slug cannot be a host name (a reserved name, for example).
func Host(domain, slug string) string {
	if domain == "" || email.ValidateHostedSlug(slug) != nil {
		return ""
	}
	return slug + "." + domain
}

// Origin is where the workspace's desk runs: https://<slug>.<domain> with the scheme and port of
// origin (APP_ORIGIN), or origin itself when the workspace has no host of its own.
func Origin(origin, domain, slug string) string {
	origin = strings.TrimRight(origin, "/")
	host := Host(domain, slug)
	if host == "" {
		return origin
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
	return scheme + "://" + host + port
}
