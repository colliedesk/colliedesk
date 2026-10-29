package domain

import (
	"regexp"
	"time"
)

// searchConsoleToken is what a verification token may hold: letters, digits, "-" and "_". Google's are
// 43 characters of that alphabet and Bing's 32 hexadecimal digits; no quote, angle bracket or space can
// reach the <meta> tag the token is served in.
var searchConsoleToken = regexp.MustCompile(`^[A-Za-z0-9_-]{16,100}$`)

// SearchConsole is the verification tokens of a workspace's help center: the content of the
// google-site-verification and msvalidate.01 <meta> tags. An empty string is none.
type SearchConsole struct {
	Google string
	Bing   string
}

// SearchConsoleView is the tokens as the API answers them, with the help center's content version.
type SearchConsoleView struct {
	Google         *string    `json:"google_site_verification"`
	Bing           *string    `json:"bing_site_verification"`
	ContentVersion int64      `json:"content_version"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

// SearchConsoleChange is a save of the tokens: a nil field stays as it is, a pointer to "" removes it.
type SearchConsoleChange struct {
	Google *string
	Bing   *string
}

// ValidSearchConsoleToken reports whether token may be stored and served as a verification token.
func ValidSearchConsoleToken(token string) bool { return searchConsoleToken.MatchString(token) }

// CheckSearchConsole refuses a change that names a token of the wrong shape, with a ThemeError
// validation_failed that says which field.
func CheckSearchConsole(change SearchConsoleChange) error {
	fields := map[string]string{}
	for _, item := range []struct {
		field string
		token *string
	}{{"google_site_verification", change.Google}, {"bing_site_verification", change.Bing}} {
		if item.token != nil && *item.token != "" && !ValidSearchConsoleToken(*item.token) {
			fields[item.field] = "Paste only the content value of the meta tag: 16 to 100 letters, digits, - or _."
		}
	}
	if len(fields) > 0 {
		return &ThemeError{Code: "validation_failed", Fields: fields}
	}
	return nil
}
