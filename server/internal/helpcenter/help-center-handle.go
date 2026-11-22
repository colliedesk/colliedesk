package helpcenter

import (
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// handlePath splits sub, a path below the primary help center's root, into what may be the handle
// of another help center and the path below that one's root: /<handle> or /<handle>/.... A first
// segment the site routes itself or reads as a language is never a handle
// (domain.CheckHelpCenterHandle), so those pages keep their meaning.
func handlePath(sub string) (handle, rest string, ok bool) {
	after, found := strings.CutPrefix(sub, "/")
	if !found {
		return "", "", false
	}
	handle, rest, _ = strings.Cut(after, "/")
	if domain.CheckHelpCenterHandle(handle) != nil {
		return "", "", false
	}
	if rest != "" {
		rest = "/" + rest
	}
	return handle, rest, true
}

// siteAddress is the public address of the help center site, without a trailing slash: its custom
// domain once one is active; else, for a help center other than the primary, /<handle> below the
// primary's address; else the primary's own address (SiteURL).
func (s *Site) siteAddress(site domain.HelpCenterSite) string {
	if site.Handle == "" || site.CustomHost != "" {
		return SiteURL(s.config.Origin, s.config.Domain, site.Slug, site.CustomHost)
	}
	return SiteURL(s.config.Origin, s.config.Domain, site.Slug, site.PrimaryHost) + "/" + site.Handle
}
