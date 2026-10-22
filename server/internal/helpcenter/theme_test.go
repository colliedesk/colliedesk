package helpcenter

import (
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func themeValue(value string) *string { return &value }

func brandedTheme() domain.HelpCenterTheme {
	return domain.HelpCenterTheme{
		Colors: domain.ThemeColors{Primary: themeValue("#0b5cad"), Header: themeValue("#ffffff"), HeaderText: themeValue("#16201c"), Background: themeValue("#ffffff"),
			Text: themeValue("#1f2328"), Link: themeValue("#0b5cad"), Footer: themeValue("#f6f8fa")},
		BodyFont: themeValue("humanist-sans"), HeadingFont: themeValue("transitional-serif"),
	}
}

func TestBrandStyleSheetSetsTheThemeAndNothingForTheDefault(t *testing.T) {
	if sheet := brandCSS(domain.HelpCenterTheme{LogoAlt: "Acme", LinkbackURL: themeValue("https://acme.example/")}); sheet != nil {
		t.Fatalf("a theme without colors or fonts has a style sheet:\n%s", sheet)
	}
	sheet := string(brandCSS(brandedTheme()))
	for _, want := range []string{
		"--accent:#0b5cad;", "--accent-ink:#ffffff;", "--link:#0b5cad;", "--paper:#ffffff;", "--ink:#1f2328;",
		"--header:#ffffff;", "--header-ink:#16201c;", "--footer:#f6f8fa;", "--footer-ink:#16201c;",
		`--font-body:Seravek, "Gill Sans Nova", Ubuntu, Calibri, "DejaVu Sans", source-sans-pro, sans-serif;`,
		`--font-head:Charter, "Bitstream Charter", "Sitka Text", Cambria, serif;`,
		// Brand colors are chosen for a light page: the automatic dark palette is off.
		"color-scheme:light;",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("brand style sheet lacks %q:\n%s", want, sheet)
		}
	}
	fontsOnly := string(brandCSS(domain.HelpCenterTheme{HeadingFont: themeValue("mono")}))
	if !strings.Contains(fontsOnly, `--font-head:ui-monospace,`) || strings.Contains(fontsOnly, "--accent") || strings.Contains(fontsOnly, "color-scheme") {
		t.Errorf("a theme of fonts only sets colors or the color scheme:\n%s", fontsOnly)
	}
}

func TestBrandStyleSheetIsNamedByItsContent(t *testing.T) {
	theme := brandedTheme()
	name := brandSheetName(brandCSS(theme))
	if !regexp.MustCompile(`^brand-[0-9a-f]{12}\.css$`).MatchString(name) || name != brandSheetName(brandCSS(brandedTheme())) {
		t.Fatalf("brand style sheet name %q", name)
	}
	theme.Colors.Link = themeValue("#0a4f94")
	if brandSheetName(brandCSS(theme)) == name {
		t.Fatal("another link color kept the style sheet's name")
	}
}
