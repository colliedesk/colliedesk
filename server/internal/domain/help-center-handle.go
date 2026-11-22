package domain

import (
	"regexp"
	"slices"
)

// MaxHelpCenterHandle is the longest handle of a help center.
const MaxHelpCenterHandle = 40

// helpCenterHandleShape is a help center's handle as the database stores it.
var helpCenterHandleShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// languageSegmentShape is a lowercase handle CheckLocale would take as a language tag: the help
// center site reads such a first path segment as a locale (vi, pt-br), so it can never name a help
// center, whether or not that language is shown today.
var languageSegmentShape = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{1,8})*$`)

// ReservedHelpCenterHandles are the first path segments the help center site routes below its root:
// its pages, files and styles, the Freshdesk paths it keeps (/support), the channel types' routes
// (the help chat at /chat), and b, the old place of other help centers. The database refuses them
// too (help_centers_handle_not_a_route); a new route is added to both.
var ReservedHelpCenterHandles = []string{
	"a", "b", "c", "search", "images", "media", "files", "_", "robots.txt", "sitemap.xml", "llms.txt",
	"support", "chat",
}

// CheckHelpCenterHandle refuses a handle that cannot name a help center at /<handle> below the
// primary's root: one not shaped like a handle, a route of the site, or a language tag.
func CheckHelpCenterHandle(handle string) error {
	switch {
	case !helpCenterHandleShape.MatchString(handle):
		return invalidKB("A handle is 1 to %d lowercase letters, digits or hyphens, starting with a letter or digit, such as hardware.", MaxHelpCenterHandle)
	case slices.Contains(ReservedHelpCenterHandles, handle):
		return invalidKB("%q is a page of the help center site; choose another handle, such as %s-help.", handle, handle)
	case languageSegmentShape.MatchString(handle):
		return invalidKB("%q reads as a language tag (like vi or pt-br) in a help center address; choose a handle of 4 or more letters, such as %s-help.", handle, handle)
	}
	return nil
}
