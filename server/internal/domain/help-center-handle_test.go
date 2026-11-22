package domain

import (
	"errors"
	"strings"
	"testing"
)

// A handle is the first path segment of a help center below the primary's root, so it can be
// neither a name the help center site routes nor anything the site would read as a language.
func TestHelpCenterHandleRefusesRouteNamesAndLanguageTags(t *testing.T) {
	for _, handle := range []string{"hardware", "store-eu", "x", "docs2", "a1", "help", "billing-and-plans",
		strings.Repeat("h", 40)} {
		if err := CheckHelpCenterHandle(handle); err != nil {
			t.Errorf("CheckHelpCenterHandle(%q) = %v, want nil", handle, err)
		}
	}
	for _, handle := range []string{
		// Routes of the help center site and of the channels below its root.
		"a", "b", "c", "search", "images", "media", "files", "_", "robots.txt", "sitemap.xml", "llms.txt",
		"support", "chat",
		// Shaped like a language tag the site reads as a locale segment.
		"vi", "en", "fra", "pt-br", "zh-hant", "eu-store",
		// Not shaped like a handle at all.
		"", "Hardware", "-hardware", "hard_ware", "hard/ware", strings.Repeat("h", 41),
	} {
		err := CheckHelpCenterHandle(handle)
		var invalid *InvalidKBError
		if !errors.As(err, &invalid) {
			t.Errorf("CheckHelpCenterHandle(%q) = %v, want an InvalidKBError", handle, err)
		}
	}
}
