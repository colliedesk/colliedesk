package helpcenter

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The formatting subset of an article (underline, highlight, colour, alignment, indent and tables
// with merged or coloured cells) renders with the help center's own classes, never a style attribute.
// The editor preview runs the same cases (web/src/features/kb/markdown-preview.test.tsx).
func TestArticleFormattingRendersWithHelpCenterClasses(t *testing.T) {
	raw, err := os.ReadFile("../kbformat/testdata/render.json")
	if err != nil {
		t.Fatal(err)
	}
	var shared struct {
		Cases []struct{ Name, Markdown, HTML string }
	}
	if err := json.Unmarshal(raw, &shared); err != nil || len(shared.Cases) == 0 {
		t.Fatalf("read the cases: %v", err)
	}
	for _, c := range shared.Cases {
		t.Run(c.Name, func(t *testing.T) {
			out, err := renderArticle(c.Markdown, "")
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != c.HTML {
				t.Fatalf("rendered\n%s\nwant\n%s", out, c.HTML)
			}
		})
	}
}

// The page's policy stays exactly what it was before articles could hold formatting: colours and
// alignment come from the style sheet's classes, so no inline style is ever allowed.
func TestPagePolicyAllowsNoInlineStyle(t *testing.T) {
	want := "default-src 'none'; img-src 'self'; style-src 'self'; frame-src https://www.youtube-nocookie.com https://player.vimeo.com; media-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	if contentSecurityPolicy != want {
		t.Fatalf("policy %q, want %q", contentSecurityPolicy, want)
	}
}

// Every class the renderer writes has a rule in the style sheet, in light and in dark.
func TestStyleSheetStylesEveryFormattingClass(t *testing.T) {
	css, err := os.ReadFile("static/site.css")
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(css)
	dark := sheet[strings.Index(sheet, "@media (prefers-color-scheme: dark)"):]
	for _, class := range []string{"kb-align-center", "kb-align-right", "kb-indent-1", "kb-indent-4",
		"kb-color-accent", "kb-color-success", "kb-color-warning", "kb-color-danger", "kb-color-muted",
		"kb-bg-accent", "kb-bg-success", "kb-bg-warning", "kb-bg-danger", "kb-bg-muted"} {
		if !regexp.MustCompile(`\.` + class + `\b`).MatchString(sheet) {
			t.Errorf("site.css has no rule for .%s", class)
		}
	}
	for _, token := range []string{"--kb-success", "--kb-warning", "--kb-danger"} {
		if !strings.Contains(dark, token+":") {
			t.Errorf("the dark theme does not set %s", token)
		}
	}
}
