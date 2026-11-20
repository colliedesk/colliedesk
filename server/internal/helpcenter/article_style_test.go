package helpcenter

import (
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// articleColors are the colors an article's callouts and highlighted code are written in.
var articleColors = []string{
	"callout-note", "callout-tip", "callout-important", "callout-warning", "callout-caution",
	"code-keyword", "code-string", "code-comment", "code-name", "code-number",
}

// Callout labels and highlighted code stay readable (4.5:1) on the page and on a code block, in light
// and in dark.
func TestCalloutAndCodeColorsAreReadableInLightAndDark(t *testing.T) {
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
		for _, name := range articleColors {
			for _, background := range []string{"surface", "paper"} {
				if ratio := domain.ContrastRatio(color(name), color(background)); ratio < 4.5 {
					t.Errorf("%s: --%s on --%s is %.2f:1, needs 4.5:1", scheme, name, background, ratio)
				}
			}
		}
	}
	for _, kind := range calloutTypes {
		if !strings.Contains(sheet, ".callout-"+kind+" {") {
			t.Errorf("site.css has no rule .callout-%s", kind)
		}
	}
}

// One click in a code block selects all of it, ready to copy.
func TestCodeBlocksSelectAsAWhole(t *testing.T) {
	style, err := files.ReadFile("static/site.css")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(style), ".prose pre code {")
	if start < 0 {
		t.Fatal("no rule .prose pre code")
	}
	rule := string(style[start:])
	if rule = rule[:strings.Index(rule, "}")]; !strings.Contains(rule, "user-select: all") {
		t.Errorf("%s} has no user-select: all", rule)
	}
}
