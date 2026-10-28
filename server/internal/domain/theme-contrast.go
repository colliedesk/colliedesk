package domain

import (
	"fmt"
	"math"
	"strconv"
)

// ThemeCheck is the contrast of one color pair the help center shows. Field is the color to change
// when it fails; Suggestion, set only then, is the nearest color for Field that passes every pair of
// that field.
type ThemeCheck struct {
	Pair       string  `json:"pair"`
	Field      string  `json:"field"`
	Foreground string  `json:"foreground"`
	Background string  `json:"background"`
	Ratio      float64 `json:"ratio"`
	Required   float64 `json:"required"`
	Passes     bool    `json:"passes"`
	Suggestion *string `json:"suggestion"`
}

// themePalette is the effective light palette: the theme's colors with the defaults filled in.
type themePalette struct{ primary, header, headerText, background, text, link, footer RGB }

// The defaults are the light colors of today's pages (helpcenter/static/site.css).
var defaultThemePalette = themePalette{
	primary: RGB{0x17, 0x67, 0x5a}, header: RGB{0xff, 0xff, 0xff}, headerText: RGB{0x16, 0x20, 0x1c},
	background: RGB{0xf5, 0xf7, 0xf5}, text: RGB{0x16, 0x20, 0x1c}, link: RGB{0x17, 0x67, 0x5a}, footer: RGB{0xff, 0xff, 0xff},
}

func paletteOf(colors ThemeColors) themePalette {
	palette := defaultThemePalette
	for _, item := range []struct {
		value  *string
		target *RGB
	}{
		{colors.Primary, &palette.primary}, {colors.Header, &palette.header}, {colors.HeaderText, &palette.headerText},
		{colors.Background, &palette.background}, {colors.Text, &palette.text}, {colors.Link, &palette.link}, {colors.Footer, &palette.footer},
	} {
		if item.value != nil {
			if parsed, ok := ParseHexColor(*item.value); ok {
				*item.target = parsed
			}
		}
	}
	return palette
}

// fieldColor is the palette entry a field names.
func (p *themePalette) fieldColor(field string) *RGB {
	switch field {
	case "colors.primary":
		return &p.primary
	case "colors.header_text":
		return &p.headerText
	case "colors.text":
		return &p.text
	case "colors.link":
		return &p.link
	case "colors.footer":
		return &p.footer
	}
	return nil
}

// betterInk is whichever of a and b stands out more on background.
func betterInk(background, a, b RGB) RGB {
	if ContrastRatio(b, background) > ContrastRatio(a, background) {
		return b
	}
	return a
}

type contrastRule struct {
	pair, field, label string
	required           float64
	colors             func(themePalette) (foreground, background RGB)
}

// contrastRules are WCAG AA: 4.5:1 for text (1.4.3) and 3:1 for the primary color, the focus ring and
// buttons, against the page (1.4.11).
var contrastRules = []contrastRule{
	{"text_on_background", "colors.text", "Text on the background", 4.5, func(p themePalette) (RGB, RGB) { return p.text, p.background }},
	{"link_on_background", "colors.link", "Links on the background", 4.5, func(p themePalette) (RGB, RGB) { return p.link, p.background }},
	{"header_text_on_header", "colors.header_text", "Header text on the header", 4.5, func(p themePalette) (RGB, RGB) { return p.headerText, p.header }},
	{"ink_on_primary", "colors.primary", "Button text on the primary color", 4.5, func(p themePalette) (RGB, RGB) {
		return betterInk(p.primary, RGB{255, 255, 255}, RGB{}), p.primary
	}},
	{"footer_ink_on_footer", "colors.footer", "Footer text on the footer", 4.5, func(p themePalette) (RGB, RGB) {
		return betterInk(p.footer, p.text, p.headerText), p.footer
	}},
	{"primary_on_background", "colors.primary", "The primary color against the background", 3, func(p themePalette) (RGB, RGB) { return p.primary, p.background }},
}

// ThemeContrast checks every pair of the effective palette and returns the checks and, for each
// failing field, the message of its first failing pair with the nearest color that passes them all.
func ThemeContrast(colors ThemeColors) ([]ThemeCheck, map[string]string) {
	palette := paletteOf(colors)
	checks := make([]ThemeCheck, len(contrastRules))
	fields := map[string]string{}
	suggestions := map[string]*string{}
	for i, rule := range contrastRules {
		foreground, background := rule.colors(palette)
		ratio := ContrastRatio(foreground, background)
		check := ThemeCheck{Pair: rule.pair, Field: rule.field, Foreground: foreground.Hex(), Background: background.Hex(),
			Ratio: displayRatio(ratio, rule.required), Required: rule.required, Passes: ratio >= rule.required}
		if !check.Passes {
			suggestion, done := suggestions[rule.field]
			if !done {
				suggestion = suggestFor(palette, rule.field)
				suggestions[rule.field] = suggestion
			}
			check.Suggestion = suggestion
			if _, named := fields[rule.field]; !named {
				fields[rule.field] = contrastMessage(rule, check)
			}
		}
		checks[i] = check
	}
	return checks, fields
}

// suggestFor finds the nearest color for field that passes every rule of that field, the rest of the
// palette unchanged.
func suggestFor(palette themePalette, field string) *string {
	start := palette.fieldColor(field)
	if start == nil {
		return nil
	}
	found, ok := NearestPassingColor(*start, func(candidate RGB) bool {
		trial := palette
		*trial.fieldColor(field) = candidate
		for _, rule := range contrastRules {
			if rule.field == field {
				if foreground, background := rule.colors(trial); !ContrastPasses(foreground, background, rule.required) {
					return false
				}
			}
		}
		return true
	})
	if !ok {
		return nil
	}
	hex := found.Hex()
	return &hex
}

func contrastMessage(rule contrastRule, check ThemeCheck) string {
	message := fmt.Sprintf("%s is %s:1; it needs %s:1.", rule.label, formatRatio(check.Ratio), formatRatio(check.Required))
	if check.Suggestion != nil {
		message += " " + *check.Suggestion + " passes."
	}
	return message
}

// displayRatio rounds to two decimals, but never up onto the required ratio of a pair that fails.
func displayRatio(ratio, required float64) float64 {
	rounded := math.Round(ratio*100) / 100
	if ratio < required && rounded >= required {
		return math.Floor(ratio*100) / 100
	}
	return rounded
}

func formatRatio(ratio float64) string { return strconv.FormatFloat(ratio, 'f', -1, 64) }

// CheckTheme cleans the theme (CleanTheme) and checks its contrast. A pair below WCAG AA is a
// ThemeError contrast_too_low with every check and a message per failing color.
func CheckTheme(theme HelpCenterTheme) (HelpCenterTheme, []ThemeCheck, error) {
	cleaned, err := CleanTheme(theme)
	if err != nil {
		return HelpCenterTheme{}, nil, err
	}
	checks, fields := ThemeContrast(cleaned.Colors)
	if len(fields) > 0 {
		return HelpCenterTheme{}, checks, &ThemeError{Code: "contrast_too_low", Fields: fields, Checks: checks}
	}
	return cleaned, checks, nil
}
