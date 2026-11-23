package domain

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
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

// PrimaryHelpCenterHandle is the word inbox search reads as the primary help center
// (help_center:primary). The primary has no handle of its own, so no other help center may take
// this one. It is not a route of the site, so the database does not refuse it.
const PrimaryHelpCenterHandle = "primary"

// IsHelpCenterHandle says a text is shaped like a help center's handle.
func IsHelpCenterHandle(text string) bool { return helpCenterHandleShape.MatchString(text) }

// CheckHelpCenterHandle refuses a handle that cannot name a help center at /<handle> below the
// primary's root: one not shaped like a handle, a route of the site, or a language tag. It also
// refuses the word search uses for the primary.
func CheckHelpCenterHandle(handle string) error {
	switch {
	case !helpCenterHandleShape.MatchString(handle):
		return invalidKB("A handle is 1 to %d lowercase letters, digits or hyphens, starting with a letter or digit, such as hardware.", MaxHelpCenterHandle)
	case handle == PrimaryHelpCenterHandle:
		return invalidKB("%q stands for the primary help center in inbox search (help_center:primary); choose another handle, such as %s-help.", handle, handle)
	case slices.Contains(ReservedHelpCenterHandles, handle):
		return invalidKB("%q is a page of the help center site; choose another handle, such as %s-help.", handle, handle)
	case languageSegmentShape.MatchString(handle):
		return invalidKB("%q reads as a language tag (like vi or pt-br) in a help center address; choose a handle of 4 or more letters, such as %s-help.", handle, handle)
	}
	return nil
}

// HelpCenterHandleFrom makes a handle from a help center's name: accents dropped, every run of other
// characters one hyphen, at most MaxHelpCenterHandle long. A name with no Latin letter or digit, such
// as サポート, gives "". The result is not checked: it may be reserved or read as a language tag.
func HelpCenterHandleFrom(name string) string {
	var out strings.Builder
	hyphen := false
	for _, char := range norm.NFD.String(strings.ToLower(name)) {
		if char == 'đ' {
			char = 'd'
		}
		switch {
		case unicode.Is(unicode.Mn, char):
			continue
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			if hyphen && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(char)
			hyphen = false
		default:
			hyphen = true
		}
	}
	return TrimHelpCenterHandle(out.String(), MaxHelpCenterHandle)
}

// TrimHelpCenterHandle cuts handle to at most length characters, without a hyphen at the end.
func TrimHelpCenterHandle(handle string, length int) string {
	if len(handle) > length {
		handle = handle[:length]
	}
	return strings.TrimRight(handle, "-")
}
