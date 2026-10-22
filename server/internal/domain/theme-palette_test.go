package domain

import "testing"

func themeColors(primary, header, headerText, background, text, link, footer string) ThemeColors {
	value := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	return ThemeColors{Primary: value(primary), Header: value(header), HeaderText: value(headerText), Background: value(background),
		Text: value(text), Link: value(link), Footer: value(footer)}
}

func TestBrandPaletteDerivesInksThatReadOnTheirBackgrounds(t *testing.T) {
	cases := []struct {
		name   string
		colors ThemeColors
		want   map[string]string
	}{
		{
			// #0b5cad on white is 6.67:1, so white ink, and the primary serves as link hover and ring.
			name:   "light brand",
			colors: themeColors("#0b5cad", "#ffffff", "#16201c", "#ffffff", "#1f2328", "#0b5cad", "#f6f8fa"),
			// The button hover is the primary 12% towards black (white ink: 7.95:1, up from 6.67:1).
			want: map[string]string{"accent_ink": "#ffffff", "surface": "#ffffff", "footer_ink": "#16201c", "ring": "#0b5cad", "accent_hover": "#0a5198",
				"header_hover": "#0b5cad", "header_ring": "#0b5cad", "footer_hover": "#0b5cad", "footer_ring": "#0b5cad"},
		},
		{
			// #7c3aed is 3.29:1 on the #121212 page but 2.89:1 on the #1f1f1f cards, so the focus ring
			// takes the link color, 6.06:1 on the cards.
			name:   "dark page with a primary that fades on the cards",
			colors: themeColors("#7c3aed", "", "", "#121212", "#e8e8e8", "#a78bfa", ""),
			want:   map[string]string{"surface": "#1f1f1f", "ring": "#a78bfa"},
		},
		{
			// White on #2f74c0 is 4.79:1; a brighter hover (#337dcf) would drop it to 4.22:1, so the hover
			// darkens instead: #2966a9, 5.89:1.
			name:   "primary near the button text limit",
			colors: themeColors("#2f74c0", "", "", "", "", "", ""),
			want:   map[string]string{"accent_ink": "#ffffff", "accent_hover": "#2966a9"},
		},
		{
			// Black ink on a yellow primary: the hover lightens 12% towards white.
			name:   "yellow primary with black ink",
			colors: themeColors("#ffd400", "#ffffff", "#000000", "#2b2b2b", "#ffffff", "#ffd400", "#000000"),
			want:   map[string]string{"accent_ink": "#000000", "accent_hover": "#ffd91f"},
		},
		{
			// A header and footer in the primary color: links there turn the header's (or footer's) ink,
			// and the focus ring too, since the primary color vanishes on itself.
			name:   "primary header and footer",
			colors: themeColors("#0b5cad", "#0b5cad", "#ffffff", "#ffffff", "#1f2328", "#0b5cad", "#0b5cad"),
			want:   map[string]string{"footer_ink": "#ffffff", "header_hover": "#ffffff", "header_ring": "#ffffff", "footer_hover": "#ffffff", "footer_ring": "#ffffff"},
		},
		{
			// #767676 is the lightest gray that reads on white (4.54:1); anything lighter for muted text fails.
			name:   "muted falls back to the text color",
			colors: themeColors("", "", "", "#ffffff", "#767676", "#0b5cad", ""),
			want:   map[string]string{"muted": "#767676"},
		},
	}
	for _, c := range cases {
		palette := ThemeBrandPalette(c.colors)
		got := map[string]string{"accent_ink": palette.AccentInk.Hex(), "surface": palette.Surface.Hex(), "footer_ink": palette.FooterInk.Hex(),
			"header_hover": palette.HeaderHover.Hex(), "header_ring": palette.HeaderRing.Hex(), "footer_hover": palette.FooterHover.Hex(),
			"footer_ring": palette.FooterRing.Hex(), "muted": palette.Muted.Hex(), "ring": palette.Ring.Hex(), "accent_hover": palette.AccentHover.Hex()}
		for key, want := range c.want {
			if got[key] != want {
				t.Errorf("%s: %s = %s, want %s", c.name, key, got[key], want)
			}
		}
	}
}

// Whatever colors pass the save checks, every pair the pages show passes too: derived colors that
// would not read fall back to ones that do.
func TestBrandPaletteKeepsEveryShownPairReadable(t *testing.T) {
	for name, colors := range map[string]ThemeColors{
		"default":        {},
		"light brand":    themeColors("#0b5cad", "#ffffff", "#16201c", "#ffffff", "#1f2328", "#0b5cad", "#f6f8fa"),
		"dark header":    themeColors("#c2410c", "#111827", "#f9fafb", "#fffbeb", "#1c1917", "#9a3412", "#111827"),
		"gray text":      themeColors("", "", "", "#ffffff", "#767676", "#0b5cad", ""),
		"dark page":      themeColors("#8ab4f8", "#121212", "#e8e8e8", "#121212", "#e8e8e8", "#8ab4f8", "#1e1e1e"),
		"cream page":     themeColors("#14532d", "#14532d", "#ffffff", "#fdf6e3", "#3b3a36", "#14532d", "#eee8d5"),
		"yellow primary": themeColors("#ffd400", "#ffffff", "#000000", "#2b2b2b", "#ffffff", "#ffd400", "#000000"),
		"violet on dark": themeColors("#7c3aed", "", "", "#121212", "#e8e8e8", "#a78bfa", ""),
		"button limit":   themeColors("#2f74c0", "", "", "", "", "", ""),
	} {
		if _, fields := ThemeContrast(colors); len(fields) > 0 {
			t.Fatalf("%s does not pass the save checks: %v", name, fields)
		}
		p := ThemeBrandPalette(colors)
		for _, pair := range []struct {
			label           string
			ink, background RGB
			required        float64
		}{
			{"text on surface", p.Ink, p.Surface, 4.5}, {"link on surface", p.Link, p.Surface, 4.5},
			{"text on soft accent", p.Ink, p.AccentSoft, 4.5}, {"link on soft accent", p.Link, p.AccentSoft, 4.5},
			{"text on tab", p.Ink, p.Tab, 4.5}, {"link on tab", p.Link, p.Tab, 4.5},
			{"muted on paper", p.Muted, p.Paper, 4.5}, {"muted on surface", p.Muted, p.Surface, 4.5},
			{"ink on accent", p.AccentInk, p.Accent, 4.5}, {"ink on accent hover", p.AccentInk, p.AccentHover, 4.5},
			{"ring on paper", p.Ring, p.Paper, 3}, {"ring on surface", p.Ring, p.Surface, 3},
			{"ring on soft accent", p.Ring, p.AccentSoft, 3}, {"ring on tab", p.Ring, p.Tab, 3},
			{"header ink", p.HeaderInk, p.Header, 4.5}, {"header hover", p.HeaderHover, p.Header, 4.5}, {"header ring", p.HeaderRing, p.Header, 3},
			{"footer ink", p.FooterInk, p.Footer, 4.5}, {"footer hover", p.FooterHover, p.Footer, 4.5}, {"footer ring", p.FooterRing, p.Footer, 3},
		} {
			if !ContrastPasses(pair.ink, pair.background, pair.required) {
				t.Errorf("%s: %s %s on %s is %.2f:1, needs %v:1", name, pair.label, pair.ink.Hex(), pair.background.Hex(),
					ContrastRatio(pair.ink, pair.background), pair.required)
			}
		}
	}
}
