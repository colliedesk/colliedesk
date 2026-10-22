package domain

import (
	"errors"
	"strings"
	"testing"
)

func color(hex string) *string { return &hex }

func themeError(t *testing.T, err error, code string) *ThemeError {
	t.Helper()
	var invalid *ThemeError
	if !errors.As(err, &invalid) || invalid.Code != code {
		t.Fatalf("error = %v, want a ThemeError %s", err, code)
	}
	return invalid
}

func checkOf(t *testing.T, checks []ThemeCheck, pair string) ThemeCheck {
	t.Helper()
	for _, check := range checks {
		if check.Pair == pair {
			return check
		}
	}
	t.Fatalf("no %s check in %+v", pair, checks)
	return ThemeCheck{}
}

func TestDefaultThemePassesEveryContrastCheck(t *testing.T) {
	_, checks, err := CheckTheme(HelpCenterTheme{})
	if err != nil {
		t.Fatalf("the default theme was refused: %v", err)
	}
	if len(checks) != 6 {
		t.Fatalf("checks = %+v, want six pairs", checks)
	}
	for _, check := range checks {
		if !check.Passes || check.Suggestion != nil {
			t.Errorf("default %s fails: %+v", check.Pair, check)
		}
	}
}

func TestCheckThemeRefusesLowTextContrastWithAPassingSuggestion(t *testing.T) {
	theme := HelpCenterTheme{Colors: ThemeColors{Text: color("#999999"), Background: color("#ffffff")}}
	_, _, err := CheckTheme(theme)
	invalid := themeError(t, err, "contrast_too_low")
	// #999999 on white is 2.85:1 (published WCAG value).
	text := checkOf(t, invalid.Checks, "text_on_background")
	if text.Passes || text.Ratio != 2.85 || text.Required != 4.5 || text.Field != "colors.text" || text.Suggestion == nil {
		t.Fatalf("text check = %+v", text)
	}
	suggestion, ok := ParseHexColor(*text.Suggestion)
	if !ok || !ContrastPasses(suggestion, RGB{255, 255, 255}, 4.5) {
		t.Fatalf("suggestion %v does not pass on white", *text.Suggestion)
	}
	want := "Text on the background is 2.85:1; it needs 4.5:1. " + *text.Suggestion + " passes."
	if invalid.Fields["colors.text"] != want || len(invalid.Fields) != 1 {
		t.Fatalf("fields = %v, want colors.text: %q", invalid.Fields, want)
	}
	theme.Colors.Text = text.Suggestion
	if _, _, err := CheckTheme(theme); err != nil {
		t.Fatalf("the suggested text color is refused: %v", err)
	}
}

func TestPrimarySuggestionPassesBothPairsOfThePrimaryColor(t *testing.T) {
	// Yellow carries black button text well, but the focus ring vanishes on white (WCAG 1.4.11).
	theme := HelpCenterTheme{Colors: ThemeColors{Primary: color("#ffff00"), Background: color("#ffffff")}}
	_, _, err := CheckTheme(theme)
	invalid := themeError(t, err, "contrast_too_low")
	if ink := checkOf(t, invalid.Checks, "ink_on_primary"); !ink.Passes || ink.Foreground != "#000000" {
		t.Fatalf("ink check = %+v, want black ink passing", ink)
	}
	ring := checkOf(t, invalid.Checks, "primary_on_background")
	if ring.Passes || ring.Required != 3 || ring.Suggestion == nil {
		t.Fatalf("primary check = %+v", ring)
	}
	if !strings.HasPrefix(invalid.Fields["colors.primary"], "The primary color against the background is 1.07:1; it needs 3:1.") {
		t.Fatalf("fields = %v", invalid.Fields)
	}
	theme.Colors.Primary = ring.Suggestion
	if _, _, err := CheckTheme(theme); err != nil {
		t.Fatalf("the suggested primary color is refused: %v", err)
	}
}

func TestFooterInkIsTheBetterOfTextAndHeaderText(t *testing.T) {
	dark := HelpCenterTheme{Colors: ThemeColors{Footer: color("#16201c")}}
	_, _, err := CheckTheme(dark)
	if invalid := themeError(t, err, "contrast_too_low"); invalid.Fields["colors.footer"] == "" {
		t.Fatalf("dark footer with dark text: fields = %v", invalid.Fields)
	}
	dark.Colors.HeaderText = color("#ffffff")
	dark.Colors.Header = color("#16201c")
	_, checks, err := CheckTheme(dark)
	if err != nil {
		t.Fatalf("dark footer with white header text refused: %v", err)
	}
	if footer := checkOf(t, checks, "footer_ink_on_footer"); footer.Foreground != "#ffffff" {
		t.Fatalf("footer ink = %+v, want the white header text", footer)
	}
}

func TestCheckThemeRefusesBadShapesFieldByField(t *testing.T) {
	bad := HelpCenterTheme{
		LogoMediaID:  color("not-an-id"),
		LogoAlt:      strings.Repeat("a", 201),
		LinkbackURL:  color("javascript:alert(1)"),
		ContactURL:   color("http://example.com/contact"),
		ContactPhone: color("call us"),
		Colors:       ThemeColors{Text: color("#fff")},
		BodyFont:     color("comic"),
	}
	_, _, err := CheckTheme(bad)
	invalid := themeError(t, err, "validation_failed")
	for _, field := range []string{"logo_media_id", "logo_alt", "linkback_url", "contact_url", "contact_phone", "colors.text", "body_font"} {
		if invalid.Fields[field] == "" {
			t.Errorf("no message for %s in %v", field, invalid.Fields)
		}
	}
}

func TestCheckThemeCleansWhatItKeeps(t *testing.T) {
	theme, _, err := CheckTheme(HelpCenterTheme{
		LogoAlt:      "  Acme  ",
		LinkbackURL:  color("https://acme.example/"),
		ContactURL:   color("mailto:help@acme.example"),
		ContactPhone: color("+1 (555) 010-2030"),
		Colors:       ThemeColors{Link: color("#0B5CAD"), Primary: color("")},
		HeadingFont:  color("transitional-serif"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if theme.LogoAlt != "Acme" || *theme.Colors.Link != "#0b5cad" || theme.Colors.Primary != nil || *theme.HeadingFont != "transitional-serif" {
		t.Fatalf("cleaned theme = %+v", theme)
	}
	if len(ThemeFontStacks) != 7 {
		t.Fatalf("font keys = %d, want 7", len(ThemeFontStacks))
	}
}
