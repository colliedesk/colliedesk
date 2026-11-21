// Package kblinks finds the help center articles a reply links to, so a sent reply can count as a use
// of each (docs/internals/knowledge-base.md › Views and uses).
package kblinks

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Site is what names a workspace's help center in an address: only its own hosts do, so a host that
// merely starts with the slug, or the help center path on another host, is not it.
type Site struct {
	// Slug is the workspace's: <AppHost>/help/<slug>/... is its help center.
	Slug string
	// AppHost is this server's host (APP_ORIGIN); empty matches nothing.
	AppHost string
	// SlugHost is the workspace's own host, <slug>.<HELP_CENTER_DOMAIN>, whose /help/... is its help
	// center; empty without a help center domain.
	SlugHost string
	// Hosts are its verified custom domains, the whole host being the help center.
	Hosts []string
	// FreshdeskHosts are the old Freshdesk portal hosts it imported from: an old article path counts
	// there, and on the help center's own hosts, never on any other host.
	FreshdeskHosts []string
}

// Links are the articles a text links to, each once, in the order they first appear.
type Links struct {
	// Numbers are article numbers from current addresses (/a/<number>-<slug>).
	Numbers []int64
	// Freshdesk are Freshdesk article ids from old portal addresses (/support/solutions/articles/<id>)
	// on the help center or an old portal host; the id names an article through the import ledger.
	Freshdesk []string
}

// maxLinks bounds the addresses read from one text.
const maxLinks = 50

var (
	address = regexp.MustCompile(`(?i)https?://[^\s<>"'()\[\]{}]+`)
	// article is a path below a help center's address: an optional language, then /a/<number>[-<slug>].
	article = regexp.MustCompile(`^(?:/[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*)?/a/([1-9][0-9]{0,17})(?:-[^/]*)?/?$`)
	// freshdesk is an old Freshdesk portal article path; Freshdesk ids never start with 0.
	freshdesk = regexp.MustCompile(`(?i)^(?:/[a-z]{2,3}(?:-[a-z0-9]{2,8})?)?/support/solutions/articles/([1-9][0-9]{0,18})(?:-[^/]*)?/?$`)
)

// Find reads the absolute http(s) addresses in text and returns the help center articles they name.
func Find(text string, site Site) Links {
	var links Links
	for _, raw := range address.FindAllString(text, maxLinks) {
		parsed, err := url.Parse(strings.TrimRight(raw, ".,;:!?*_~"))
		if err != nil {
			continue
		}
		host, path := parsed.Hostname(), parsed.Path
		if below, ok := helpCenterPath(host, path, site); ok {
			if match := article.FindStringSubmatch(below); match != nil {
				if number, err := strconv.ParseInt(match[1], 10, 64); err == nil && !slices.Contains(links.Numbers, number) {
					links.Numbers = append(links.Numbers, number)
				}
				continue
			}
			path = below
		} else if !hostIn(host, site.FreshdeskHosts) {
			continue
		}
		if match := freshdesk.FindStringSubmatch(path); match != nil && !slices.Contains(links.Freshdesk, match[1]) {
			links.Freshdesk = append(links.Freshdesk, match[1])
		}
	}
	return links
}

// helpCenterPath is the path below the help center's address when host and path are on it.
func helpCenterPath(host, path string, site Site) (string, bool) {
	switch {
	case hostIn(host, site.Hosts):
		return path, true
	case site.SlugHost != "" && strings.EqualFold(host, site.SlugHost):
		return cutBase(path, "/help")
	case site.Slug != "" && site.AppHost != "" && strings.EqualFold(host, site.AppHost):
		return cutBase(path, "/help/"+site.Slug)
	}
	return "", false
}

// hostIn reports whether host is one of hosts, ignoring case.
func hostIn(host string, hosts []string) bool {
	return slices.ContainsFunc(hosts, func(h string) bool { return h != "" && strings.EqualFold(host, h) })
}

// cutBase is path below base, when path is base or starts with base and a slash.
func cutBase(path, base string) (string, bool) {
	below, ok := strings.CutPrefix(path, base)
	if !ok || (below != "" && below[0] != '/') {
		return "", false
	}
	return below, true
}
