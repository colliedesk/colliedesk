package helpcenter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// brandCSS is the second style sheet of a themed help center: the custom properties of site.css, then
// the admin's custom CSS byte for byte, so the page's Content-Security-Policy (style-src 'self', no
// inline style) stays as it is. The properties come from validated #rrggbb colors and the fixed font
// stacks; the custom CSS passed CheckCustomCSS before it was saved, and its rules come last so they
// can override the theme. A theme that sets no color, no font and no custom CSS has none (nil), so its
// pages are unchanged.
//
// Brand colors are chosen for a light page, so with any color set the sheet sets every color and
// color-scheme: light. It loads after site.css, so its :root wins over site.css's dark palette too.
func brandCSS(theme domain.HelpCenterTheme) []byte {
	var sheet bytes.Buffer
	if colorsSet(theme.Colors) {
		p := domain.ThemeBrandPalette(theme.Colors)
		for _, item := range []struct {
			name  string
			color domain.RGB
		}{
			{"paper", p.Paper}, {"surface", p.Surface}, {"ink", p.Ink}, {"muted", p.Muted}, {"rule", p.Rule},
			{"accent", p.Accent}, {"accent-ink", p.AccentInk}, {"accent-hover", p.AccentHover}, {"accent-soft", p.AccentSoft}, {"tab", p.Tab},
			{"link", p.Link}, {"ring", p.Ring},
			{"header", p.Header}, {"header-ink", p.HeaderInk}, {"header-hover", p.HeaderHover}, {"header-ring", p.HeaderRing},
			{"footer", p.Footer}, {"footer-ink", p.FooterInk}, {"footer-hover", p.FooterHover}, {"footer-ring", p.FooterRing},
		} {
			fmt.Fprintf(&sheet, "--%s:%s;", item.name, item.color.Hex())
		}
		ink := p.Ink
		fmt.Fprintf(&sheet, "--thumb:rgba(%d, %d, %d, 0.22);--thumb-hover:rgba(%d, %d, %d, 0.42);color-scheme:light;", ink.R, ink.G, ink.B, ink.R, ink.G, ink.B)
	}
	for _, font := range []struct {
		name string
		key  *string
	}{{"font-body", theme.BodyFont}, {"font-head", theme.HeadingFont}} {
		if font.key == nil {
			continue
		}
		if stack, ok := domain.ThemeFontStacks[*font.key]; ok {
			fmt.Fprintf(&sheet, "--%s:%s;", font.name, stack)
		}
	}
	var out []byte
	if sheet.Len() > 0 {
		out = []byte(":root{" + sheet.String() + "}\n")
	}
	if theme.CustomCSS != "" {
		out = append(out, theme.CustomCSS...)
	}
	return out
}

func colorsSet(colors domain.ThemeColors) bool {
	for _, color := range []*string{colors.Primary, colors.Header, colors.HeaderText, colors.Background, colors.Text, colors.Link, colors.Footer} {
		if color != nil {
			return true
		}
	}
	return false
}

// brandSheetName holds the sheet's hash, like site.css's, so it may be cached for good.
func brandSheetName(sheet []byte) string {
	sum := sha256.Sum256(sheet)
	return "brand-" + hex.EncodeToString(sum[:6]) + ".css"
}

// brand is the theme of one help center as its pages show it.
type brand struct {
	theme                 domain.HelpCenterTheme
	logoWidth, logoHeight int
	sheet                 []byte
	sheetName             string
	// lightOnly is true with any brand color: the pages leave the visitor's dark setting aside.
	lightOnly bool
	// key changes with anything of the theme a page shows, for the ETag.
	key string
}

// pageImage is an image of the page with its size, so the page does not shift as it loads.
type pageImage struct {
	URL, Alt      string
	Width, Height int
}

// phoneLink is a phone number and its tel: address, built from the number's digits alone (html/template
// would refuse the tel: scheme otherwise).
type phoneLink struct {
	Number string
	URL    template.URL
}

func newBrand(site domain.HelpCenterSite) brand {
	theme := site.Theme
	b := brand{theme: theme, logoWidth: site.LogoWidth, logoHeight: site.LogoHeight, sheet: brandCSS(theme), lightOnly: colorsSet(theme.Colors)}
	if b.sheet != nil {
		b.sheetName = brandSheetName(b.sheet)
	}
	b.key = strings.Join([]string{b.sheetName, orEmpty(theme.LogoMediaID), fmt.Sprint(site.LogoWidth, "x", site.LogoHeight), orEmpty(theme.FaviconMediaID),
		theme.LogoAlt, orEmpty(theme.LinkbackURL), orEmpty(theme.ContactURL), orEmpty(theme.ContactPhone)}, "\x00")
	return b
}

func orEmpty(field *string) string {
	if field == nil {
		return ""
	}
	return *field
}

// fill sets the brand's part of a page of view v.
func (b brand) fill(data *page, v view) {
	theme := b.theme
	data.LightOnly = b.lightOnly
	if b.sheetName != "" {
		data.BrandStyle = v.base + stylePath + b.sheetName
	}
	if theme.LogoMediaID != nil {
		alt := theme.LogoAlt
		if alt == "" {
			alt = data.SiteTitle
		}
		data.Logo = &pageImage{URL: v.base + stylePath + logoPath + *theme.LogoMediaID, Alt: alt, Width: b.logoWidth, Height: b.logoHeight}
	}
	if theme.FaviconMediaID != nil {
		data.Favicon = v.base + stylePath + iconPath + *theme.FaviconMediaID
	}
	if theme.LinkbackURL != nil {
		data.Website = &link{Name: websiteName(*theme.LinkbackURL), URL: *theme.LinkbackURL}
	}
	if contact := theme.ContactURL; contact != nil {
		data.Contact = &link{Name: "Contact us", URL: *contact}
		if len(*contact) > len(mailto) && strings.EqualFold((*contact)[:len(mailto)], mailto) {
			data.Contact.Name = (*contact)[len(mailto):]
		}
	}
	if theme.ContactPhone != nil {
		data.Phone = &phoneLink{Number: *theme.ContactPhone, URL: template.URL(phoneURL(*theme.ContactPhone))}
	}
}

const mailto = "mailto:"

// websiteName is what the masthead's website link says: the host name, without www.
func websiteName(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "Website"
	}
	return strings.TrimPrefix(parsed.Hostname(), "www.")
}

// phoneURL is the tel: address of a phone number of digits, spaces and + - ( ): its digits and a
// leading +.
func phoneURL(phone string) string {
	var digits strings.Builder
	for index, r := range phone {
		if r >= '0' && r <= '9' || r == '+' && index == strings.IndexAny(phone, "+0123456789") {
			digits.WriteRune(r)
		}
	}
	return "tel:" + digits.String()
}
