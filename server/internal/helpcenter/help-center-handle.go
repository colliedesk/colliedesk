package helpcenter

import (
	"regexp"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// handlePrefix is where a workspace's other help centers live below the primary's root: one letter,
// like /a/ and /c/, so it never takes a language segment, an article path or a channel type's route.
const handlePrefix = "/b/"

// handleShape is a help center's handle, as the database allows it.
var handleShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// handlePath splits sub, a path below the primary help center's root, into the handle of another
// help center and the path below that one's root: /b/<handle> or /b/<handle>/....
func handlePath(sub string) (handle, rest string, ok bool) {
	after, found := strings.CutPrefix(sub, handlePrefix)
	if !found {
		return "", "", false
	}
	handle, rest, _ = strings.Cut(after, "/")
	if !handleShape.MatchString(handle) {
		return "", "", false
	}
	if rest != "" {
		rest = "/" + rest
	}
	return handle, rest, true
}

// siteAddress is the public address of the help center site, without a trailing slash: its custom
// domain once one is active; else, for a help center other than the primary, /b/<handle> below the
// primary's address; else the primary's own address (SiteURL).
func (s *Site) siteAddress(site domain.HelpCenterSite) string {
	if site.Handle == "" || site.CustomHost != "" {
		return SiteURL(s.config.Origin, s.config.Domain, site.Slug, site.CustomHost)
	}
	return SiteURL(s.config.Origin, s.config.Domain, site.Slug, site.PrimaryHost) + strings.TrimSuffix(handlePrefix, "/") + "/" + site.Handle
}
