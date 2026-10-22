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
		CustomCSS: "@media (max-width:600px){.masthead{border-bottom:2px solid #0b5cad}}",
	}
}

func TestBrandStyleSheetSetsTheThemeAndNothingForTheDefault(t *testing.T) {
	if sheet := brandCSS(domain.HelpCenterTheme{LogoAlt: "Acme", LinkbackURL: themeValue("https://acme.example/")}); sheet != nil {
		t.Fatalf("a theme without colors or fonts has a style sheet:\n%s", sheet)
	}
	sheet := string(brandCSS(brandedTheme()))
	for _, want := range []string{
		"--accent:#0b5cad;", "--accent-ink:#ffffff;", "--accent-hover:#0a5198;", "--link:#0b5cad;", "--ring:#0b5cad;", "--paper:#ffffff;", "--ink:#1f2328;",
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

// The default look's buttons keep their text readable under the pointer, in light and in dark: a
// brightness filter would take text near 4.5:1 below it.
func TestDefaultLookKeepsButtonTextReadableOnHover(t *testing.T) {
	style, err := files.ReadFile("static/site.css")
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(style)
	dark := sheet[strings.Index(sheet, "@media (prefers-color-scheme: dark)"):]
	for scheme, block := range map[string]string{"light": sheet[:strings.Index(sheet, "}")], "dark": dark[:strings.Index(dark, "}")]} {
		color := func(name string) domain.RGB {
			match := regexp.MustCompile(`--` + name + `: (#[0-9a-f]{6});`).FindStringSubmatch(block)
			if match == nil {
				t.Fatalf("%s: site.css sets no --%s color", scheme, name)
			}
			parsed, _ := domain.ParseHexColor(match[1])
			return parsed
		}
		ink := color("accent-ink")
		for _, background := range []string{"accent", "accent-hover"} {
			if ratio := domain.ContrastRatio(ink, color(background)); ratio < 4.5 {
				t.Errorf("%s: button text on --%s is %.2f:1, needs 4.5:1", scheme, background, ratio)
			}
		}
	}
	if strings.Contains(sheet, "filter: brightness") {
		t.Error("site.css still brightens a button on hover")
	}
}
