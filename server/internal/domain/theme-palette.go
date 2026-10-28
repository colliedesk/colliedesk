package domain

import "math"

// BrandPalette is every color the help center's pages use for a theme: the theme's colors with the
// defaults filled in, and the colors derived from them. A derived color that would not read on what
// it sits on falls back to one the save checks already proved readable, so it never refuses a save.
type BrandPalette struct {
	// Paper is the page, Surface the cards and fields on it, Ink the text, Muted secondary text and
	// Rule the hairlines.
	Paper, Surface, Ink, Muted, Rule RGB
	// Accent is the primary color (buttons) and AccentInk the text on it, AccentHover a button under
	// the pointer; AccentSoft and Tab are pale tints of it behind text.
	Accent, AccentInk, AccentHover, AccentSoft, Tab RGB
	Link                                            RGB
	// Ring is the focus ring on the page and its cards.
	Ring RGB
	// Header and Footer carry their own ink, link hover color and focus ring color.
	Header, HeaderInk, HeaderHover, HeaderRing RGB
	Footer, FooterInk, FooterHover, FooterRing RGB
}

const (
	// textContrast is WCAG 1.4.3 (AA) for text; ringContrast is 1.4.11 for the focus ring.
	textContrast = 4.5
	ringContrast = 3
)

// ThemeBrandPalette derives the pages' colors from the theme's colors (CheckTheme has passed them).
func ThemeBrandPalette(colors ThemeColors) BrandPalette {
	p := paletteOf(colors)
	white, black := RGB{255, 255, 255}, RGB{}
	reads := func(background RGB, inks ...RGB) bool {
		for _, ink := range inks {
			if !ContrastPasses(ink, background, textContrast) {
				return false
			}
		}
		return true
	}
	// Cards stand out from a light page in white, from a dark one a shade towards the text.
	surface := white
	if RelativeLuminance(p.background) < RelativeLuminance(p.text) {
		surface = mixColor(p.background, p.text, 0.06)
	}
	if !reads(surface, p.text, p.link) {
		surface = p.background
	}
	muted := mixColor(p.text, p.background, 0.3)
	if !reads(p.background, muted) || !reads(surface, muted) {
		muted = p.text
	}
	tint := func(amount float64) RGB {
		if tinted := mixColor(p.primary, surface, amount); reads(tinted, p.text, p.link) {
			return tinted
		}
		return surface
	}
	// The ink, hover and ring of a band: the page's link color and primary color where they read on
	// it, the band's own ink otherwise.
	band := func(background, ink RGB) (hover, ring RGB) {
		hover, ring = ink, ink
		if ContrastPasses(p.link, background, textContrast) {
			hover = p.link
		}
		if ContrastPasses(p.primary, background, ringContrast) {
			ring = p.primary
		}
		return hover, ring
	}
	footerInk := betterInk(p.footer, p.text, p.headerText)
	palette := BrandPalette{
		Paper: p.background, Surface: surface, Ink: p.text, Muted: muted, Rule: mixColor(p.text, p.background, 0.85),
		Accent: p.primary, AccentInk: betterInk(p.primary, white, black), AccentSoft: tint(0.88), Tab: tint(0.94), Link: p.link,
		Header: p.header, HeaderInk: p.headerText, Footer: p.footer, FooterInk: footerInk,
	}
	// The page's focus ring is drawn on the page, the cards and the tints: the primary color where it
	// stands out 3:1 on all of them, else the link color, which reads 4.5:1 on each (the save checks it
	// on the page; the surface and tints fall back until it does).
	palette.Ring = p.link
	if ContrastPasses(p.primary, p.background, ringContrast) && ContrastPasses(p.primary, surface, ringContrast) &&
		ContrastPasses(p.primary, palette.AccentSoft, ringContrast) && ContrastPasses(p.primary, palette.Tab, ringContrast) {
		palette.Ring = p.primary
	}
	// A button's hover moves the primary away from its ink, so the ink only gains contrast.
	away := black
	if palette.AccentInk == black {
		away = white
	}
	palette.AccentHover = mixColor(p.primary, away, 0.12)
	palette.HeaderHover, palette.HeaderRing = band(p.header, p.headerText)
	palette.FooterHover, palette.FooterRing = band(p.footer, footerInk)
	return palette
}

// mixColor moves a towards b by amount (0 is a, 1 is b), channel by channel.
func mixColor(a, b RGB, amount float64) RGB {
	channel := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*amount))
	}
	return RGB{channel(a.R, b.R), channel(a.G, b.G), channel(a.B, b.B)}
}
