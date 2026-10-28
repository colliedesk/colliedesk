package domain

import (
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of the help center theme.
const (
	MaxThemeLogoAlt  = 200
	MaxThemeURL      = 2000
	MaxThemePhone    = 40
	themeInvalidCode = "validation_failed"
)

// ThemeFontStacks are the fonts a help center may use: stacks of fonts already on the reader's
// system, so nothing is downloaded and the page's Content-Security-Policy stays as it is.
// system-sans and classic-serif are the default text and heading fonts.
var ThemeFontStacks = map[string]string{
	"system-sans":        `system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans", "Helvetica Neue", Arial, sans-serif`,
	"humanist-sans":      `Seravek, "Gill Sans Nova", Ubuntu, Calibri, "DejaVu Sans", source-sans-pro, sans-serif`,
	"geometric-sans":     `Avenir, "Avenir Next", Montserrat, Corbel, "URW Gothic", source-sans-pro, sans-serif`,
	"classic-serif":      `"Iowan Old Style", "Charter", "Bitstream Charter", "Sitka Text", Cambria, "Palatino Linotype", Georgia, serif`,
	"transitional-serif": `Charter, "Bitstream Charter", "Sitka Text", Cambria, serif`,
	"rounded-sans":       `ui-rounded, "Hiragino Maru Gothic ProN", Quicksand, Comfortaa, Manjari, "Arial Rounded MT", Calibri, source-sans-pro, sans-serif`,
	"mono":               `ui-monospace, "SFMono-Regular", Menlo, Consolas, "Liberation Mono", monospace`,
}

// ThemeColors are the brand colors as lowercase #rrggbb; nil is the default.
type ThemeColors struct {
	Primary    *string `json:"primary"`
	Header     *string `json:"header"`
	HeaderText *string `json:"header_text"`
	Background *string `json:"background"`
	Text       *string `json:"text"`
	Link       *string `json:"link"`
	Footer     *string `json:"footer"`
}

// fields lists each color with its field name, in the order checks and messages use.
func (c *ThemeColors) fields() []struct {
	name  string
	value **string
} {
	return []struct {
		name  string
		value **string
	}{
		{"colors.primary", &c.Primary}, {"colors.header", &c.Header}, {"colors.header_text", &c.HeaderText},
		{"colors.background", &c.Background}, {"colors.text", &c.Text}, {"colors.link", &c.Link}, {"colors.footer", &c.Footer},
	}
}

// HelpCenterTheme is the brand of a help center. A nil field is the default.
type HelpCenterTheme struct {
	LogoMediaID    *string     `json:"logo_media_id"`
	FaviconMediaID *string     `json:"favicon_media_id"`
	LogoAlt        string      `json:"logo_alt"`
	LinkbackURL    *string     `json:"linkback_url"`
	ContactURL     *string     `json:"contact_url"`
	ContactPhone   *string     `json:"contact_phone"`
	Colors         ThemeColors `json:"colors"`
	BodyFont       *string     `json:"body_font"`
	HeadingFont    *string     `json:"heading_font"`
}

// HelpCenterThemeView is the theme as the API answers it: with the help center's content version and
// the contrast of every pair it shows.
type HelpCenterThemeView struct {
	HelpCenterTheme
	ContentVersion int64        `json:"content_version"`
	UpdatedAt      *time.Time   `json:"updated_at"`
	Checks         []ThemeCheck `json:"checks"`
}

// ThemeError is a theme that cannot be saved. Code is validation_failed (shape), media_not_found or
// contrast_too_low; Fields holds one message per field; Checks is set on contrast_too_low.
type ThemeError struct {
	Code   string
	Fields map[string]string
	Checks []ThemeCheck
}

func (e *ThemeError) Error() string { return "help center theme: " + e.Code }

var (
	themeMediaID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	themePhone   = regexp.MustCompile(`^[0-9 +\-()]+$`)
)

// CleanTheme trims the theme, turns empty values into defaults and lowercases colors. It refuses a
// field of the wrong shape with a ThemeError naming every bad field.
func CleanTheme(theme HelpCenterTheme) (HelpCenterTheme, error) {
	fields := map[string]string{}
	clean := func(name string, value **string, check func(string) string) {
		if *value == nil {
			return
		}
		trimmed := strings.TrimSpace(**value)
		if trimmed == "" {
			*value = nil
			return
		}
		if problem := check(trimmed); problem != "" {
			fields[name] = problem
			return
		}
		*value = &trimmed
	}
	mediaID := func(v string) string {
		if !themeMediaID.MatchString(v) {
			return "Send the id of an image of the media library."
		}
		return ""
	}
	clean("logo_media_id", &theme.LogoMediaID, mediaID)
	clean("favicon_media_id", &theme.FaviconMediaID, mediaID)
	theme.LogoAlt = strings.TrimSpace(theme.LogoAlt)
	if utf8.RuneCountInString(theme.LogoAlt) > MaxThemeLogoAlt {
		fields["logo_alt"] = "The logo's text holds at most 200 characters."
	}
	clean("linkback_url", &theme.LinkbackURL, func(v string) string { return checkThemeURL(v, "http", "https") })
	clean("contact_url", &theme.ContactURL, func(v string) string { return checkThemeURL(v, "https", "mailto") })
	clean("contact_phone", &theme.ContactPhone, func(v string) string {
		if utf8.RuneCountInString(v) > MaxThemePhone || !themePhone.MatchString(v) || strings.IndexFunc(v, func(r rune) bool { return r >= '0' && r <= '9' }) < 0 {
			return "A phone number holds digits, spaces and + - ( ), at most 40 characters."
		}
		return ""
	})
	for _, item := range theme.Colors.fields() {
		clean(item.name, item.value, func(v string) string {
			if _, ok := ParseHexColor(v); !ok {
				return "A color is written #rrggbb, such as #0b5cad."
			}
			return ""
		})
		if *item.value != nil {
			lower := strings.ToLower(**item.value)
			*item.value = &lower
		}
	}
	font := func(v string) string {
		if _, ok := ThemeFontStacks[v]; !ok {
			return "Choose one of the fonts: system-sans, humanist-sans, geometric-sans, classic-serif, transitional-serif, rounded-sans or mono."
		}
		return ""
	}
	clean("body_font", &theme.BodyFont, font)
	clean("heading_font", &theme.HeadingFont, font)
	if len(fields) > 0 {
		return HelpCenterTheme{}, &ThemeError{Code: themeInvalidCode, Fields: fields}
	}
	return theme, nil
}

// checkThemeURL accepts an absolute address of one of the schemes: a host for http and https, an
// email address for mailto.
func checkThemeURL(value string, schemes ...string) string {
	want := "an absolute " + strings.Join(schemes, " or ") + " address"
	if utf8.RuneCountInString(value) > MaxThemeURL {
		return "An address holds at most 2,000 characters."
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "Send " + want + "."
	}
	scheme := strings.ToLower(parsed.Scheme)
	allowed := false
	for _, s := range schemes {
		allowed = allowed || scheme == s
	}
	switch {
	case !allowed:
		return "Send " + want + "."
	case scheme == "mailto":
		if !plainMailAddress(parsed) {
			return "Write mailto: followed by one plain email address, such as mailto:help@example.com, with no name, ? or %."
		}
	case parsed.Host == "" || parsed.User != nil:
		return "Send " + want + " with a host name and no user name."
	}
	return ""
}

// plainMailAddress is true for a mailto that is one bare addr-spec: no display name or comment, no
// header fields (?bcc= would copy a stranger into every mail) and no fragment. Percent-encoding is
// refused rather than decoded, so the stored address is exactly the one shown and linked.
func plainMailAddress(parsed *url.URL) bool {
	address := parsed.Opaque
	if address == "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(address, "%,; <>()") {
		return false
	}
	found, err := mail.ParseAddress(address)
	return err == nil && found.Name == "" && found.Address == address
}
