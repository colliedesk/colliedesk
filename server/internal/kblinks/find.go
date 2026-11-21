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

// Site is what names a workspace's help center in an address.
type Site struct {
	// Slug is the workspace's: <any host>/help/<slug>/... and <slug>.<domain>/help/... are its help center.
	Slug string
	// Hosts are its custom domains, the whole host being the help center.
	Hosts []string
}

// Links are the articles a text links to, each once, in the order they first appear.
type Links struct {
	// Numbers are article numbers from current addresses (/a/<number>-<slug>).
	Numbers []int64
	// Freshdesk are Freshdesk article ids from old portal addresses (/support/solutions/articles/<id>),
	// on any host: the id only names an article through the workspace's own import ledger.
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
		path := parsed.Path
		if below, ok := helpCenterPath(strings.ToLower(parsed.Hostname()), path, site); ok {
			if match := article.FindStringSubmatch(below); match != nil {
				if number, err := strconv.ParseInt(match[1], 10, 64); err == nil && !slices.Contains(links.Numbers, number) {
					links.Numbers = append(links.Numbers, number)
				}
				continue
			}
			path = below
		}
		if match := freshdesk.FindStringSubmatch(path); match != nil && !slices.Contains(links.Freshdesk, match[1]) {
			links.Freshdesk = append(links.Freshdesk, match[1])
		}
	}
	return links
}

// helpCenterPath is the path below the help center's address when host and path are on it.
func helpCenterPath(host, path string, site Site) (string, bool) {
	for _, custom := range site.Hosts {
		if strings.EqualFold(host, custom) {
			return path, true
		}
	}
	if site.Slug == "" {
		return "", false
	}
	if label, _, ok := strings.Cut(host, "."); ok && label == site.Slug {
		if below, ok := cutBase(path, "/help"); ok {
			return below, true
		}
	}
	return cutBase(path, "/help/"+site.Slug)
}

// cutBase is path below base, when path is base or starts with base and a slash.
func cutBase(path, base string) (string, bool) {
	below, ok := strings.CutPrefix(path, base)
	if !ok || (below != "" && below[0] != '/') {
		return "", false
	}
	return below, true
}
